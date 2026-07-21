package tools

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/buildwithdmytro/openjourney/internal/domain"
)

type testStore struct{ previewedWith domain.Principal }

func (s *testStore) ListEventSchemas(context.Context, domain.Principal) ([]domain.EventSchema, error) {
	return []domain.EventSchema{{EventType: "signup"}}, nil
}
func (s *testStore) PreviewSegment(_ context.Context, p domain.Principal, _ string) (int, map[string]int, error) {
	s.previewedWith = p
	return 3, map[string]int{"profiles": 3}, nil
}
func (s *testStore) CampaignReport(context.Context, domain.Principal, string, domain.ReportQuery) (domain.CampaignReport, error) {
	return domain.CampaignReport{}, nil
}
func (s *testStore) JourneyReport(context.Context, domain.Principal, string, domain.ReportQuery) (domain.JourneyReport, error) {
	return domain.JourneyReport{}, nil
}
func (s *testStore) ExperimentReport(context.Context, domain.Principal, string, domain.ReportQuery) (domain.ExperimentReport, error) {
	return domain.ExperimentReport{}, nil
}

type testRecorder struct{ calls []ToolCall }

func (r *testRecorder) RecordToolCall(_ context.Context, call ToolCall) error {
	r.calls = append(r.calls, call)
	return nil
}

func TestRunnerDerivesAIActorAndIntersectsScopes(t *testing.T) {
	store := &testStore{}
	recorder := &testRecorder{}
	runner := NewRunner(store, recorder)
	for _, tool := range ReadOnlyTools() {
		if err := runner.Register(tool); err != nil {
			t.Fatalf("register %s: %v", tool.Definition().Name, err)
		}
	}
	output, err := runner.Call(context.Background(), domain.Principal{
		TenantID: "tenant", WorkspaceID: "workspace", UserID: "user", ActorType: "user",
		Scopes: []string{"segments:read", "reports:read"},
	}, "segment.preview", json.RawMessage(`{"segment_id":"segment-1"}`))
	if err != nil {
		t.Fatalf("call: %v", err)
	}
	if string(output) != `{"count":3,"per_leg":{"profiles":3}}` {
		t.Fatalf("unexpected output: %s", output)
	}
	if store.previewedWith.ActorType != "ai_agent" || len(store.previewedWith.Scopes) != 1 || store.previewedWith.Scopes[0] != "segments:read" {
		t.Fatalf("unexpected derived principal: %+v", store.previewedWith)
	}
	if len(recorder.calls) != 1 || recorder.calls[0].PolicyDecision != "allowed" {
		t.Fatalf("unexpected activity: %+v", recorder.calls)
	}
}

func TestRunnerDeniesOutOfScopeAndRecords(t *testing.T) {
	recorder := &testRecorder{}
	runner := NewRunner(&testStore{}, recorder)
	if err := runner.Register(reportReadTool{}); err != nil {
		t.Fatal(err)
	}
	_, err := runner.Call(context.Background(), domain.Principal{ActorType: "user", Scopes: []string{"segments:read"}}, "report.read", json.RawMessage(`{"report_type":"campaign","resource_id":"c1"}`))
	if !errors.Is(err, ErrDenied) {
		t.Fatalf("expected denied error, got %v", err)
	}
	if len(recorder.calls) != 1 || recorder.calls[0].PolicyDecision != "denied_scope" || recorder.calls[0].Actor.ActorType != "ai_agent" {
		t.Fatalf("denied call was not recorded as an AI actor: %+v", recorder.calls)
	}
}
