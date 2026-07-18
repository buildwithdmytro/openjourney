package stages

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/buildwithdmytro/openjourney/internal/domain"
)

type fakeStore struct {
	rules    []domain.StageRule
	sets     map[string][]string
	profiles map[string]domain.Profile
	events   []domain.Event
}

func (f *fakeStore) ListStageRuleScopes(context.Context) ([][2]string, error) {
	return [][2]string{{"t", "w"}}, nil
}
func (f *fakeStore) ListStageRules(context.Context, string, string) ([]domain.StageRule, error) {
	return f.rules, nil
}
func (f *fakeStore) ResolveSegment(_ context.Context, _ domain.Principal, id string) ([]string, error) {
	return f.sets[id], nil
}
func (f *fakeStore) GetProfileByIDSystem(_ context.Context, _, _, id string) (domain.Profile, error) {
	return f.profiles[id], nil
}
func (f *fakeStore) GetFirstAppID(context.Context, string, string) (string, error) { return "app", nil }
func (f *fakeStore) AcceptEvents(_ context.Context, _ domain.Principal, events []domain.Event) ([]string, error) {
	f.events = append(f.events, events...)
	return []string{"1", "2"}, nil
}

func TestApplyUsesPriorityAndEmitsProfileUpdate(t *testing.T) {
	f := &fakeStore{
		rules: []domain.StageRule{{ID: "low", Stage: "lead", SegmentID: "seg-low", Priority: 1, Enabled: true}, {ID: "high", Stage: "mql", SegmentID: "seg-high", Priority: 2, Enabled: true}},
		sets:  map[string][]string{"seg-low": {"p"}, "seg-high": {"p"}}, profiles: map[string]domain.Profile{"p": {ID: "p", ExternalID: "external-p", Attributes: json.RawMessage(`{"stage":"lead"}`)}},
	}
	changed, err := Apply(context.Background(), f, "t", "w")
	if err != nil || changed != 1 {
		t.Fatalf("changed=%d err=%v", changed, err)
	}
	if len(f.events) != 2 || f.events[0].Type != "stage.changed" || f.events[1].Type != "profile.updated" {
		t.Fatalf("events=%+v", f.events)
	}
	if f.events[0].IdempotencyKey != "stage:high:p" {
		t.Fatalf("unexpected idempotency key %q", f.events[0].IdempotencyKey)
	}
}

func TestApplySkipsExistingStage(t *testing.T) {
	f := &fakeStore{rules: []domain.StageRule{{ID: "r", Stage: "lead", SegmentID: "s", Enabled: true}}, sets: map[string][]string{"s": {"p"}}, profiles: map[string]domain.Profile{"p": {ID: "p", ExternalID: "p", Attributes: json.RawMessage(`{"stage":"lead"}`)}}}
	changed, err := Apply(context.Background(), f, "t", "w")
	if err != nil || changed != 0 || len(f.events) != 0 {
		t.Fatalf("changed=%d events=%d err=%v", changed, len(f.events), err)
	}
}
