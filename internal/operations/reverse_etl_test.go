package operations

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/buildwithdmytro/openjourney/internal/connector"
	"github.com/buildwithdmytro/openjourney/internal/domain"
)

func (s *sourceStore) MaterializeConnectorRows(context.Context, domain.Principal, json.RawMessage, []string) ([]connector.Row, error) {
	return []connector.Row{{"external_id": "profile-1", "plan": "pro"}}, nil
}

func TestReverseETLIsIdempotentAndReadOnly(t *testing.T) {
	fake := connector.NewFakeDriver()
	registry := connector.NewRegistry(map[string]connector.ConnectorDriver{"fake": fake}, fake)
	mapping, _ := json.Marshal(map[string]any{
		"audience_dsl": map[string]any{"type": "profile_attribute", "field": "plan", "operator": "equals", "value": "pro"},
		"fields":       []string{"external_id", "plan"},
	})
	config, _ := json.Marshal(map[string]any{"driver": "fake", "upsert_key": "external_id"})
	store := &sourceStore{
		pipeline: domain.ConnectorPipeline{ID: "pipeline", TenantID: "tenant", WorkspaceID: "workspace", AppID: "app", ConnectorExtensionID: "extension", CurrentVersionID: stringPtr("version")},
		version:  domain.ConnectorPipelineVersion{ID: "version", PipelineID: "pipeline", Version: 1, Mapping: mapping},
		config:   domain.ExtensionConfig{Config: config},
	}
	job := reverseETLInput{TenantID: "tenant", WorkspaceID: "workspace", AppID: "app", PipelineID: "pipeline"}
	if err := executeReverseETLWithRegistry(context.Background(), store, nil, job, registry); err != nil {
		t.Fatal(err)
	}
	if err := executeReverseETLWithRegistry(context.Background(), store, nil, job, registry); err != nil {
		t.Fatal(err)
	}
	if len(fake.Writes) != 1 {
		t.Fatalf("sink rows=%d, want one idempotent upsert", len(fake.Writes))
	}
	if len(store.events) != 0 {
		t.Fatalf("reverse-ETL mutated source events: %d", len(store.events))
	}
	if len(store.runs) != 4 || store.runs[1].Status != "succeeded" || store.runs[3].Status != "succeeded" {
		t.Fatalf("unexpected connector runs: %#v", store.runs)
	}
}
