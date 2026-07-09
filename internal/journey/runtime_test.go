package journey

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/buildwithdmytro/openjourney/internal/domain"
	"github.com/buildwithdmytro/openjourney/internal/ports"
)

type mockStore struct {
	ports.Store
	runs            map[string]domain.JourneyRun
	steps           map[string]domain.JourneyStep
	versions        map[string]domain.JourneyVersion
	transitions     []domain.JourneyTransition
	intents         []domain.JourneyMessageIntent
	profile         *domain.Profile
	quietHoursStart *int
	quietHoursEnd   *int
	defaultTimezone string
}

func (m *mockStore) IsProfileInSegment(ctx context.Context, p domain.Principal, segmentID string, profileID string) (bool, error) {
	return true, nil
}

func (m *mockStore) UpdateProfileAttributes(ctx context.Context, p domain.Principal, profileID string, attrs map[string]any) error {
	return nil
}

func (m *mockStore) EvaluateAudience(ctx context.Context, p domain.Principal, profileID string, dsl json.RawMessage) (bool, error) {
	return true, nil
}

func (m *mockStore) GetProfile(ctx context.Context, p domain.Principal, profileID string) (domain.Profile, []domain.Consent, error) {
	if m.profile != nil {
		return *m.profile, nil, nil
	}
	return domain.Profile{
		ID:         profileID,
		Attributes: json.RawMessage(`{"email":"test@example.com"}`),
	}, nil, nil
}

func (m *mockStore) GetProfileByID(ctx context.Context, tenantID, appID, profileID string) (domain.Profile, error) {
	if m.profile != nil {
		return *m.profile, nil
	}
	return domain.Profile{
		ID:         profileID,
		Attributes: json.RawMessage(`{"email":"test@example.com"}`),
	}, nil
}

func (m *mockStore) GetProfileByIDSystem(ctx context.Context, tenantID, workspaceID, profileID string) (domain.Profile, error) {
	if m.profile != nil {
		return *m.profile, nil
	}
	return domain.Profile{
		ID:         profileID,
		Attributes: json.RawMessage(`{"email":"test@example.com"}`),
	}, nil
}

func (m *mockStore) AcceptEvents(ctx context.Context, p domain.Principal, events []domain.Event) ([]string, error) {
	return nil, nil
}

func newMockStore() *mockStore {
	return &mockStore{
		runs:        make(map[string]domain.JourneyRun),
		steps:       make(map[string]domain.JourneyStep),
		versions:    make(map[string]domain.JourneyVersion),
		transitions: make([]domain.JourneyTransition, 0),
		intents:     make([]domain.JourneyMessageIntent, 0),
	}
}

func (m *mockStore) ClaimJourneyStep(ctx context.Context) (domain.JourneyStep, bool, error) {
	for _, step := range m.steps {
		if step.Status == "pending" {
			step.Status = "processing"
			m.steps[step.ID] = step
			return step, true, nil
		}
	}
	return domain.JourneyStep{}, false, nil
}

func (m *mockStore) GetJourneyRunSystem(ctx context.Context, tenantID, runID string) (domain.JourneyRun, error) {
	run, ok := m.runs[runID]
	if !ok {
		return domain.JourneyRun{}, errors.New("not found")
	}
	return run, nil
}

func (m *mockStore) GetJourneyVersion(ctx context.Context, tenantID, versionID string) (domain.JourneyVersion, error) {
	ver, ok := m.versions[versionID]
	if !ok {
		return domain.JourneyVersion{}, errors.New("not found")
	}
	return ver, nil
}

func (m *mockStore) FailJourneyStep(ctx context.Context, stepID string, errMsg string) error {
	step, ok := m.steps[stepID]
	if !ok {
		return errors.New("not found")
	}
	step.Status = "failed"
	step.ErrorMessage = &errMsg
	m.steps[stepID] = step
	return nil
}

func (m *mockStore) AdvanceRunTx(ctx context.Context, runID string, run domain.JourneyRun, stepID string, nextStep *domain.JourneyStep, trans domain.JourneyTransition, messageIntent *domain.JourneyMessageIntent) error {
	m.runs[runID] = run
	step := m.steps[stepID]
	step.Status = "completed"
	m.steps[stepID] = step

	if nextStep != nil {
		nextStep.ID = "step-2"
		m.steps[nextStep.ID] = *nextStep
	}
	m.transitions = append(m.transitions, trans)
	if messageIntent != nil {
		m.intents = append(m.intents, *messageIntent)
	}
	return nil
}

func (m *mockStore) ClaimJourneyMessageIntent(ctx context.Context, workerID string) (domain.JourneyMessageIntent, bool, error) {
	for i, intent := range m.intents {
		if intent.Status == "pending" {
			intent.Status = "processing"
			intent.Attempts++
			m.intents[i] = intent
			return intent, true, nil
		}
	}
	return domain.JourneyMessageIntent{}, false, nil
}

func (m *mockStore) UpdateJourneyMessageIntent(ctx context.Context, intent domain.JourneyMessageIntent) error {
	for i, existing := range m.intents {
		if existing.ID == intent.ID {
			m.intents[i] = intent
			return nil
		}
	}
	m.intents = append(m.intents, intent)
	return nil
}


func TestTickNextSkeleton(t *testing.T) {
	store := newMockStore()
	clock := NewFakeClock(time.Now())
	deps := Deps{Clock: clock}

	// 1. Setup a simple entry->exit journey version
	graphJSON := []byte(`{
		"entry_node_id": "n1",
		"nodes": [
			{"id": "n1", "type": "entry", "config": {}},
			{"id": "n2", "type": "exit", "config": {}}
		],
		"edges": [
			{"from": "n1", "to": "n2"}
		]
	}`)

	versionID := "ver-1"
	store.versions[versionID] = domain.JourneyVersion{
		ID:    versionID,
		Graph: graphJSON,
	}

	// 2. Setup run and step for the entry node
	runID := "run-1"
	store.runs[runID] = domain.JourneyRun{
		ID:               runID,
		TenantID:         "tenant-1",
		WorkspaceID:      "workspace-1",
		JourneyID:        "journey-1",
		JourneyVersionID: versionID,
		ProfileID:        "profile-1",
		Status:           "active",
		CurrentNodeID:    "n1",
	}

	stepID := "step-1"
	store.steps[stepID] = domain.JourneyStep{
		ID:       stepID,
		RunID:    runID,
		TenantID: "tenant-1",
		NodeID:   "n1",
		Kind:     "advance",
		Status:   "pending",
	}

	// 3. Process the entry step (should advance to exit step)
	processed, err := TickNext(context.Background(), store, deps)
	if err != nil {
		t.Fatalf("TickNext failed: %v", err)
	}
	if !processed {
		t.Fatalf("expected step to be processed")
	}

	// Verify run state is updated to n2 (exit node), and a new step for n2 is pending
	run := store.runs[runID]
	if run.CurrentNodeID != "n2" {
		t.Errorf("expected current node to be n2, got %s", run.CurrentNodeID)
	}
	if run.Status != "active" {
		t.Errorf("expected status to be active, got %s", run.Status)
	}

	// Step 1 should be completed
	if store.steps["step-1"].Status != "completed" {
		t.Errorf("expected step-1 to be completed")
	}

	// Step 2 (next step) should be pending for node n2
	step2, ok := store.steps["step-2"]
	if !ok {
		t.Fatalf("expected step-2 to be inserted")
	}
	if step2.NodeID != "n2" || step2.Status != "pending" {
		t.Errorf("unexpected step2: %+v", step2)
	}

	// 4. Tick again to process the exit step (should complete the run)
	processed2, err := TickNext(context.Background(), store, deps)
	if err != nil {
		t.Fatalf("TickNext failed on step 2: %v", err)
	}
	if !processed2 {
		t.Fatalf("expected step 2 to be processed")
	}

	run = store.runs[runID]
	if run.Status != "completed" {
		t.Errorf("expected run status to be completed, got %s", run.Status)
	}
	if run.CompletedAt == nil {
		t.Errorf("expected completed_at to be set")
	}
	if store.steps["step-2"].Status != "completed" {
		t.Errorf("expected step-2 to be completed")
	}
}
