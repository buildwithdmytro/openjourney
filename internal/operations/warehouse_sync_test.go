package operations

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/buildwithdmytro/openjourney/internal/connector"
	"github.com/buildwithdmytro/openjourney/internal/domain"
)

type sourceBlobStore struct{ values map[string][]byte }

func (s *sourceBlobStore) Put(_ context.Context, key string, value []byte, _ string) error {
	s.values[key] = value
	return nil
}
func (s *sourceBlobStore) Get(context.Context, string) ([]byte, error) { return nil, nil }
func (s *sourceBlobStore) Delete(context.Context, string) error        { return nil }

type sourceStore struct {
	pipeline  domain.ConnectorPipeline
	version   domain.ConnectorPipelineVersion
	config    domain.ExtensionConfig
	events    []domain.Event
	runs      []domain.ConnectorRun
	acceptErr error
}

func (s *sourceStore) GetConnectorPipeline(context.Context, domain.Principal, string) (domain.ConnectorPipeline, error) {
	return s.pipeline, nil
}
func (s *sourceStore) GetConnectorPipelineVersion(context.Context, domain.Principal, string) (domain.ConnectorPipelineVersion, error) {
	return s.version, nil
}
func (s *sourceStore) GetExtensionConfig(context.Context, domain.Principal, string) (domain.ExtensionConfig, error) {
	return s.config, nil
}
func (s *sourceStore) RecordConnectorRun(_ context.Context, run domain.ConnectorRun) error {
	s.runs = append(s.runs, run)
	return nil
}
func (s *sourceStore) AcceptEvents(_ context.Context, _ domain.Principal, events []domain.Event) ([]string, error) {
	if s.acceptErr != nil {
		return nil, s.acceptErr
	}
	ids := make([]string, 0, len(events))
	for _, event := range events {
		seen := false
		for _, old := range s.events {
			if old.IdempotencyKey == event.IdempotencyKey {
				seen = true
				break
			}
		}
		if !seen {
			s.events = append(s.events, event)
			ids = append(ids, event.IdempotencyKey)
		}
	}
	return ids, nil
}

type committingDriver struct {
	*connector.FakeDriver
	commits int
}

func (d *committingDriver) Commit(context.Context, []connector.Row) error {
	d.commits++
	return nil
}

func TestWarehouseSyncIsEventSourcedAndIdempotent(t *testing.T) {
	fake := connector.NewFakeDriver()
	fake.Rows = []connector.Row{{"email": "a@example.com", "name": "A"}, {"name": "rejected"}}
	registry := connector.NewRegistry(map[string]connector.ConnectorDriver{"fake": fake}, fake)
	mapping, _ := json.Marshal(map[string]any{"event_type": "profile.updated", "external_id": "email", "attributes": map[string]any{"name": "name"}})
	config, _ := json.Marshal(map[string]any{"driver": "fake"})
	store := &sourceStore{
		pipeline: domain.ConnectorPipeline{ID: "pipeline", TenantID: "tenant", WorkspaceID: "workspace", AppID: "app", ConnectorExtensionID: "extension", CurrentVersionID: stringPtr("version")},
		version:  domain.ConnectorPipelineVersion{ID: "version", PipelineID: "pipeline", Version: 3, Mapping: mapping},
		config:   domain.ExtensionConfig{Config: config},
	}
	blobs := &sourceBlobStore{values: map[string][]byte{}}
	job := warehouseSyncInput{TenantID: "tenant", WorkspaceID: "workspace", PipelineID: "pipeline", Cursor: "object.csv:0"}
	if err := executeWarehouseSyncWithRegistry(context.Background(), store, blobs, job, registry); err != nil {
		t.Fatal(err)
	}
	if err := executeWarehouseSyncWithRegistry(context.Background(), store, blobs, job, registry); err != nil {
		t.Fatal(err)
	}
	if len(store.events) != 1 || len(store.runs) != 4 {
		t.Fatalf("events=%d runs=%d", len(store.events), len(store.runs))
	}
	if store.runs[1].RowsRejected != 1 || store.runs[1].RejectBlobKey == "" {
		t.Fatalf("missing rejection audit: %#v", store.runs[1])
	}
	if len(blobs.values) != 1 {
		t.Fatalf("quarantine blobs=%d", len(blobs.values))
	}
	if store.events[0].IdempotencyKey != "extension:3:object.csv:0:0" {
		t.Fatalf("idempotency key=%q", store.events[0].IdempotencyKey)
	}
}

func TestWarehouseSyncCommitsStreamOnlyAfterAcceptEvents(t *testing.T) {
	driver := &committingDriver{FakeDriver: &connector.FakeDriver{Rows: []connector.Row{{"email": "a@example.com"}}}}
	registry := connector.NewRegistry(map[string]connector.ConnectorDriver{"kafka": driver}, driver)
	mapping, _ := json.Marshal(map[string]any{"event_type": "profile.updated", "external_id": "email"})
	config, _ := json.Marshal(map[string]any{"driver": "kafka"})
	store := &sourceStore{
		pipeline: domain.ConnectorPipeline{ID: "pipeline", AppID: "app", ConnectorExtensionID: "extension", CurrentVersionID: stringPtr("version")},
		version:  domain.ConnectorPipelineVersion{ID: "version", Version: 1, Mapping: mapping},
		config:   domain.ExtensionConfig{Config: config},
	}
	if err := executeWarehouseSyncWithRegistry(context.Background(), store, &sourceBlobStore{values: map[string][]byte{}}, warehouseSyncInput{PipelineID: "pipeline"}, registry); err != nil {
		t.Fatal(err)
	}
	if driver.commits != 1 {
		t.Fatalf("commits=%d, want one commit after acceptance", driver.commits)
	}
	driver.commits = 0
	store.acceptErr = context.DeadlineExceeded
	if err := executeWarehouseSyncWithRegistry(context.Background(), store, &sourceBlobStore{values: map[string][]byte{}}, warehouseSyncInput{PipelineID: "pipeline"}, registry); err != nil {
		t.Fatal(err)
	}
	if driver.commits != 0 {
		t.Fatal("stream records were committed after AcceptEvents failed")
	}
}

func stringPtr(v string) *string { return &v }
