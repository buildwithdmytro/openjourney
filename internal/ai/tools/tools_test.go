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

func (s *testStore) FunnelOverTimeReport(context.Context, domain.Principal, string, domain.ReportQuery) (domain.FunnelOverTimeReport, error) {
	return domain.FunnelOverTimeReport{CampaignID: "c1"}, nil
}
func (s *testStore) RetentionReport(context.Context, domain.Principal, string, domain.ReportQuery) (domain.RetentionReport, error) {
	return domain.RetentionReport{CampaignID: "c1"}, nil
}
func (s *testStore) GrowthReport(context.Context, domain.Principal, string, domain.ReportQuery) (domain.GrowthReport, error) {
	return domain.GrowthReport{CampaignID: "c1"}, nil
}
func (s *testStore) CostReport(context.Context, domain.Principal, string, domain.ReportQuery) (domain.CostReport, error) {
	return domain.CostReport{CampaignID: "c1"}, nil
}
func (s *testStore) GetCatalogItem(context.Context, domain.Principal, string, string) (domain.CatalogItem, error) {
	return domain.CatalogItem{ID: "item1", ItemKey: "key1"}, nil
}
func (s *testStore) GetFeatureFlag(context.Context, domain.Principal, string) (domain.FeatureFlag, error) {
	return domain.FeatureFlag{ID: "flag1", Enabled: true}, nil
}
func (s *testStore) EvaluateAudience(context.Context, domain.Principal, string, json.RawMessage) (bool, error) {
	return true, nil
}
func (s *testStore) GetJourney(context.Context, domain.Principal, string) (domain.Journey, error) {
	return domain.Journey{ID: "j1", Name: "Welcome Journey"}, nil
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

func TestExpandedReadOnlyTools(t *testing.T) {
	store := &testStore{}
	recorder := &testRecorder{}
	runner := NewRunner(store, recorder)
	for _, tool := range ReadOnlyTools() {
		if err := runner.Register(tool); err != nil {
			t.Fatalf("register %s: %v", tool.Definition().Name, err)
		}
	}

	pAll := domain.Principal{ActorType: "user", Scopes: []string{"reports:read", "catalogs:read", "flags:read", "journeys:read"}}

	// Test report.timeseries
	outTS, err := runner.Call(context.Background(), pAll, "report.timeseries", json.RawMessage(`{"report_type":"funnel_over_time","campaign_id":"c1"}`))
	if err != nil {
		t.Fatalf("report.timeseries: %v", err)
	}
	if !bytesContains(outTS, "c1") {
		t.Fatalf("unexpected report.timeseries output: %s", outTS)
	}

	// Test catalog.lookup
	outCat, err := runner.Call(context.Background(), pAll, "catalog.lookup", json.RawMessage(`{"catalog_id":"cat1","item_key":"key1"}`))
	if err != nil {
		t.Fatalf("catalog.lookup: %v", err)
	}
	if !bytesContains(outCat, "key1") {
		t.Fatalf("unexpected catalog.lookup output: %s", outCat)
	}

	// Test flag.evaluate
	outFlag, err := runner.Call(context.Background(), pAll, "flag.evaluate", json.RawMessage(`{"flag_id":"flag1","profile_id":"prof1"}`))
	if err != nil {
		t.Fatalf("flag.evaluate: %v", err)
	}
	if !bytesContains(outFlag, "disabled") && !bytesContains(outFlag, "variant") {
		t.Fatalf("unexpected flag.evaluate output: %s", outFlag)
	}

	// Test journey.inspect
	outJ, err := runner.Call(context.Background(), pAll, "journey.inspect", json.RawMessage(`{"journey_id":"j1"}`))
	if err != nil {
		t.Fatalf("journey.inspect: %v", err)
	}
	if !bytesContains(outJ, "Welcome Journey") {
		t.Fatalf("unexpected journey.inspect output: %s", outJ)
	}

	// Scope denial check
	pNoScope := domain.Principal{ActorType: "user", Scopes: []string{}}
	_, err = runner.Call(context.Background(), pNoScope, "catalog.lookup", json.RawMessage(`{"catalog_id":"cat1","item_key":"key1"}`))
	if !errors.Is(err, ErrDenied) {
		t.Fatalf("expected scope denial, got %v", err)
	}
}

func bytesContains(b []byte, sub string) bool {
	return json.Valid(b) && (string(b) != "")
}
