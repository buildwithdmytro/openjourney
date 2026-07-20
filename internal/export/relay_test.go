package export

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/buildwithdmytro/openjourney/internal/connector"
	"github.com/buildwithdmytro/openjourney/internal/domain"
)

type relayStore struct {
	event     domain.OutboxEvent
	claimed   bool
	completed int
	failed    int
	pipeline  domain.ConnectorPipeline
	version   domain.ConnectorPipelineVersion
	config    domain.ExtensionConfig
}

func (s *relayStore) ClaimExportOutboxEvent(context.Context) (domain.OutboxEvent, bool, error) {
	if s.claimed {
		return s.event, true, nil
	}
	s.claimed = true
	return s.event, true, nil
}
func (s *relayStore) CompleteOutboxEvent(context.Context, string) error    { s.completed++; return nil }
func (s *relayStore) FailOutboxEvent(context.Context, string, error) error { s.failed++; return nil }
func (s *relayStore) GetConnectorPipeline(context.Context, domain.Principal, string) (domain.ConnectorPipeline, error) {
	return s.pipeline, nil
}
func (s *relayStore) GetConnectorPipelineVersion(context.Context, domain.Principal, string) (domain.ConnectorPipelineVersion, error) {
	return s.version, nil
}
func (s *relayStore) GetExtensionConfig(context.Context, domain.Principal, string) (domain.ExtensionConfig, error) {
	return s.config, nil
}

func TestRelayUsesOutboxAtLeastOnceWithSinkUpsert(t *testing.T) {
	pipelineID, versionID := "pipeline-1", "version-1"
	payload, _ := json.Marshal(map[string]any{
		"event_id": "event-1", "tenant_id": "tenant-1", "workspace_id": "workspace-1", "app_id": "app-1",
		"export_pipeline_ids": []string{pipelineID}, "payload": map[string]any{"value": "hello"},
	})
	store := &relayStore{
		event:    domain.OutboxEvent{ID: "outbox-1", TenantID: "tenant-1", Topic: "exports.events.v1", EventID: "event-1", Payload: payload},
		pipeline: domain.ConnectorPipeline{ID: pipelineID, CurrentVersionID: &versionID, ConnectorExtensionID: "extension-1"},
		version:  domain.ConnectorPipelineVersion{ID: versionID, Mapping: json.RawMessage(`{"upsert_key":"event_id"}`)},
		config:   domain.ExtensionConfig{Config: json.RawMessage(`{"driver":"fake","upsert_key":"event_id"}`)},
	}
	fake := connector.NewFakeDriver()
	registry := connector.NewRegistry(map[string]connector.ConnectorDriver{"fake": fake}, fake)
	if _, err := DrainWithRegistry(context.Background(), store, registry, 1, false); err != nil {
		t.Fatal(err)
	}
	// A redelivery of the same outbox record is safe: FakeDriver's upsert key
	// replaces the existing row instead of appending a duplicate.
	store.claimed = false
	if _, err := DrainWithRegistry(context.Background(), store, registry, 1, false); err != nil {
		t.Fatal(err)
	}
	if len(fake.Writes) != 1 {
		t.Fatalf("expected one sink row after redelivery, got %d", len(fake.Writes))
	}
	if store.failed != 0 || store.completed != 2 {
		t.Fatalf("unexpected relay accounting: failed=%d completed=%d", store.failed, store.completed)
	}
}
