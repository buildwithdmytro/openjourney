package audience

import (
	"context"
	"testing"
)

type mockEvaluatorStore struct {
	profileMatched bool
	consentMatched bool
	chMatched      bool
	extID          string
}

func (m *mockEvaluatorStore) QueryProfileMatches(ctx context.Context, sql string, args []any) (bool, error) {
	return m.profileMatched, nil
}

func (m *mockEvaluatorStore) QueryConsentMatches(ctx context.Context, sql string, args []any) (bool, error) {
	return m.consentMatched, nil
}

func (m *mockEvaluatorStore) QueryClickHouseMatches(ctx context.Context, sql string, args []any) (bool, error) {
	return m.chMatched, nil
}

func (m *mockEvaluatorStore) GetProfileExternalID(ctx context.Context, tenantID, workspaceID, profileID string) (string, error) {
	return m.extID, nil
}

func TestMatchesProfileAttribute(t *testing.T) {
	store := &mockEvaluatorStore{profileMatched: true}
	node := &ProfileAttribute{
		Field:    "country",
		Operator: "equals",
		Value:    "US",
	}

	matched, err := Matches(context.Background(), store, "tenant-1", "workspace-1", "app-1", "profile-1", node)
	if err != nil {
		t.Fatalf("Matches failed: %v", err)
	}
	if !matched {
		t.Errorf("expected matched to be true")
	}

	store.profileMatched = false
	matched, err = Matches(context.Background(), store, "tenant-1", "workspace-1", "app-1", "profile-1", node)
	if err != nil {
		t.Fatalf("Matches failed: %v", err)
	}
	if matched {
		t.Errorf("expected matched to be false")
	}
}

func TestMatchesConsent(t *testing.T) {
	store := &mockEvaluatorStore{consentMatched: true}
	node := &Consent{
		Channel: "email",
		Topic:   "marketing",
		State:   "allow",
	}

	matched, err := Matches(context.Background(), store, "tenant-1", "workspace-1", "app-1", "profile-1", node)
	if err != nil {
		t.Fatalf("Matches failed: %v", err)
	}
	if !matched {
		t.Errorf("expected matched to be true")
	}
}

func TestMatchesEventHistory(t *testing.T) {
	store := &mockEvaluatorStore{chMatched: true, extID: "user-1"}
	node := &EventHistory{
		EventType:      "signup",
		TimeWindowDays: 30,
		MinCount:       1,
		Operator:       "has_occurred",
	}

	matched, err := Matches(context.Background(), store, "tenant-1", "workspace-1", "app-1", "profile-1", node)
	if err != nil {
		t.Fatalf("Matches failed: %v", err)
	}
	if !matched {
		t.Errorf("expected matched to be true")
	}
}

func TestMatchesScore(t *testing.T) {
	store := &mockEvaluatorStore{profileMatched: true}
	node := &Score{
		Model:     "model-1",
		ScoreName: "purchase_propensity",
		Operator:  "greater_than",
		Value:     0.85,
	}

	matched, err := Matches(context.Background(), store, "tenant-1", "workspace-1", "app-1", "profile-1", node)
	if err != nil {
		t.Fatalf("Matches failed: %v", err)
	}
	if !matched {
		t.Errorf("expected matched to be true")
	}
}
