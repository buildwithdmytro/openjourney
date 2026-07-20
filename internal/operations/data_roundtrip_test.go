package operations

import (
	"context"
	"encoding/json"
	"io"
	"strings"
	"testing"

	"github.com/buildwithdmytro/openjourney/internal/connector"
	"github.com/buildwithdmytro/openjourney/internal/domain"
)

type roundTripObjects map[string]string

func (o roundTripObjects) List(context.Context, string) ([]string, error) {
	return []string{"profiles.csv"}, nil
}

func (o roundTripObjects) Open(_ context.Context, key string) (io.ReadCloser, error) {
	return io.NopCloser(strings.NewReader(o[key])), nil
}

type roundTripStore struct {
	*sourceStore
	profiles map[string]connector.Row
}

func (s *roundTripStore) AcceptEvents(ctx context.Context, p domain.Principal, events []domain.Event) ([]string, error) {
	ids, err := s.sourceStore.AcceptEvents(ctx, p, events)
	if err != nil {
		return nil, err
	}
	for _, event := range events {
		alreadyProjected := false
		for _, id := range ids {
			if id == event.IdempotencyKey {
				alreadyProjected = true
				break
			}
		}
		if !alreadyProjected {
			continue
		}
		var payload struct {
			Attributes map[string]any `json:"attributes"`
		}
		if err := json.Unmarshal(event.Payload, &payload); err != nil {
			return nil, err
		}
		row := connector.Row{"external_id": event.ExternalID}
		for key, value := range payload.Attributes {
			row[key] = value
		}
		s.profiles[event.ExternalID] = row
	}
	return ids, nil
}

func (s *roundTripStore) MaterializeConnectorRows(context.Context, domain.Principal, json.RawMessage, []string) ([]connector.Row, error) {
	rows := make([]connector.Row, 0, len(s.profiles))
	for _, row := range s.profiles {
		rows = append(rows, row)
	}
	return rows, nil
}

func TestConnectorDataRoundTripE2E(t *testing.T) {
	s3 := connector.NewS3DriverWithClient(roundTripObjects{
		"profiles.csv": "email,plan\na@example.com,pro\n",
	})
	fakeSink := connector.NewFakeDriver()
	registry := connector.NewRegistry(map[string]connector.ConnectorDriver{
		"s3":   s3,
		"fake": fakeSink,
	}, fakeSink)

	sourceMapping, _ := json.Marshal(map[string]any{
		"event_type":  "profile.updated",
		"external_id": "email",
		"attributes":  map[string]any{"plan": "plan"},
	})
	store := &roundTripStore{
		sourceStore: &sourceStore{
			pipeline: domain.ConnectorPipeline{ID: "pipeline", TenantID: "tenant", WorkspaceID: "workspace", AppID: "app", ConnectorExtensionID: "extension", CurrentVersionID: stringPtr("version")},
			version:  domain.ConnectorPipelineVersion{ID: "version", PipelineID: "pipeline", Version: 1, Mapping: sourceMapping},
			config:   domain.ExtensionConfig{Config: json.RawMessage(`{"driver":"s3","max_rows":100}`)},
		},
		profiles: map[string]connector.Row{},
	}
	blobs := &sourceBlobStore{values: map[string][]byte{}}
	sourceJob := warehouseSyncInput{TenantID: "tenant", WorkspaceID: "workspace", AppID: "app", PipelineID: "pipeline"}
	if err := executeWarehouseSyncWithRegistry(context.Background(), store, blobs, sourceJob, registry); err != nil {
		t.Fatal(err)
	}
	if err := executeWarehouseSyncWithRegistry(context.Background(), store, blobs, sourceJob, registry); err != nil {
		t.Fatal(err)
	}
	if len(store.events) != 1 || len(store.profiles) != 1 {
		t.Fatalf("source replay duplicated data: events=%d profiles=%d", len(store.events), len(store.profiles))
	}

	sinkMapping, _ := json.Marshal(map[string]any{
		"audience_dsl": map[string]any{"type": "profile_attribute", "field": "plan", "operator": "equals", "value": "pro"},
		"fields":       []string{"external_id", "plan"},
	})
	store.version.Mapping = sinkMapping
	store.config.Config = json.RawMessage(`{"driver":"fake","upsert_key":"external_id"}`)
	sinkJob := reverseETLInput{TenantID: "tenant", WorkspaceID: "workspace", AppID: "app", PipelineID: "pipeline"}
	if err := executeReverseETLWithRegistry(context.Background(), store, nil, sinkJob, registry); err != nil {
		t.Fatal(err)
	}
	if err := executeReverseETLWithRegistry(context.Background(), store, nil, sinkJob, registry); err != nil {
		t.Fatal(err)
	}
	if len(store.events) != 1 || len(store.profiles) != 1 || len(fakeSink.Writes) != 1 {
		t.Fatalf("replay duplicated data: events=%d profiles=%d sink_rows=%d", len(store.events), len(store.profiles), len(fakeSink.Writes))
	}
}
