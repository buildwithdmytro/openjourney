package journey

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"
	"time"

	"github.com/buildwithdmytro/openjourney/internal/domain"
)

type enrollMockStore struct {
	mockStore
	resolvedSegment   []string
	scheduledVersions []domain.JourneyVersion
	journey           domain.Journey
	version           domain.JourneyVersion
}

func (e *enrollMockStore) GetJourney(context.Context, domain.Principal, string) (domain.Journey, error) {
	return e.journey, nil
}
func (e *enrollMockStore) GetJourneyVersion(context.Context, string, string) (domain.JourneyVersion, error) {
	return e.version, nil
}

func (e *enrollMockStore) ListActiveScheduledJourneyVersions(ctx context.Context) ([]domain.JourneyVersion, error) {
	return e.scheduledVersions, nil
}

func (e *enrollMockStore) ResolveSegment(ctx context.Context, p domain.Principal, segmentID string) ([]string, error) {
	return e.resolvedSegment, nil
}

func (e *enrollMockStore) GetJourneyRunsForProfile(ctx context.Context, tenantID, versionID, profileID string) ([]domain.JourneyRun, error) {
	var out []domain.JourneyRun
	for _, r := range e.runs {
		if r.JourneyVersionID == versionID && r.ProfileID == profileID {
			out = append(out, r)
		}
	}
	for i := 0; i < len(out); i++ {
		for j := i + 1; j < len(out); j++ {
			if out[i].ReentrySequence < out[j].ReentrySequence {
				out[i], out[j] = out[j], out[i]
			}
		}
	}
	return out, nil
}

func (e *enrollMockStore) CreateJourneyRun(ctx context.Context, run domain.JourneyRun) (bool, error) {
	for _, r := range e.runs {
		if r.JourneyVersionID == run.JourneyVersionID && r.ProfileID == run.ProfileID && r.EntryKey == run.EntryKey && r.ReentrySequence == run.ReentrySequence {
			return false, nil
		}
	}
	run.ID = fmt.Sprintf("run-%d", len(e.runs)+1)
	e.runs[run.ID] = run
	return true, nil
}

func (e *enrollMockStore) EnrollJourneyRun(ctx context.Context, run domain.JourneyRun, step domain.JourneyStep) (string, bool, error) {
	inserted, err := e.CreateJourneyRun(ctx, run)
	if err != nil || !inserted {
		return "", inserted, err
	}
	for id, stored := range e.runs {
		if stored.JourneyVersionID == run.JourneyVersionID && stored.ProfileID == run.ProfileID && stored.EntryKey == run.EntryKey {
			step.RunID = id
			if err := e.InsertJourneyStep(ctx, step); err != nil {
				delete(e.runs, id)
				return "", false, err
			}
			return id, true, nil
		}
	}
	return "", false, fmt.Errorf("inserted run not found")
}

func (e *enrollMockStore) InsertJourneyStep(ctx context.Context, step domain.JourneyStep) error {
	step.ID = fmt.Sprintf("step-%d", len(e.steps)+1)
	e.steps[step.ID] = step
	return nil
}

func TestIsScheduledDue(t *testing.T) {
	tests := []struct {
		schedule string
		now      time.Time
		expected bool
	}{
		{"", time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC), true},
		{"* * * * *", time.Date(2026, 1, 1, 12, 3, 0, 0, time.UTC), true},
		{"*/5 * * * *", time.Date(2026, 1, 1, 12, 5, 0, 0, time.UTC), true},
		{"*/5 * * * *", time.Date(2026, 1, 1, 12, 7, 0, 0, time.UTC), false},
		{"0 * * * *", time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC), true},
		{"0 * * * *", time.Date(2026, 1, 1, 12, 5, 0, 0, time.UTC), false},
		{"invalid schedule", time.Date(2026, 1, 1, 12, 5, 0, 0, time.UTC), false},
		{"5 12 * * *", time.Date(2026, 1, 1, 12, 5, 0, 0, time.UTC), false},
		{"*/0 * * * *", time.Date(2026, 1, 1, 12, 5, 0, 0, time.UTC), false},
	}

	for _, tc := range tests {
		res := isScheduledDue(tc.schedule, tc.now)
		if res != tc.expected {
			t.Errorf("isScheduledDue(%q, %v) expected %v, got %v", tc.schedule, tc.now, tc.expected, res)
		}
	}
}

func TestEnrollScheduledDue(t *testing.T) {
	clock := NewFakeClock(time.Date(2026, 1, 1, 12, 5, 0, 0, time.UTC))
	segmentID := "seg-1"
	store := &enrollMockStore{
		mockStore:       *newMockStore(),
		resolvedSegment: []string{"profile-1", "profile-2"},
		scheduledVersions: []domain.JourneyVersion{
			{
				ID:             "ver-1",
				JourneyID:      "j-1",
				TenantID:       "tenant-1",
				WorkspaceID:    "ws-1",
				EntryKind:      "scheduled",
				EntrySegmentID: &segmentID,
				EntrySchedule:  stringPtr("*/5 * * * *"),
				ReentryPolicy:  "once",
				Status:         "active",
				Graph:          json.RawMessage(`{"entry_node_id":"n1","nodes":[],"edges":[]}`),
			},
		},
	}
	store.profile = &domain.Profile{ID: "profile", ExternalID: "customer-scheduled"}

	err := EnrollScheduledDue(context.Background(), store, clock)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(store.runs) != 2 {
		t.Errorf("expected 2 runs, got %d", len(store.runs))
	}
	if len(store.steps) != 2 {
		t.Errorf("expected 2 steps, got %d", len(store.steps))
	}
	for _, run := range store.runs {
		if run.SubjectExternalID == nil || *run.SubjectExternalID != "customer-scheduled" {
			t.Fatalf("scheduled run missing subject external id: %+v", run.SubjectExternalID)
		}
	}

	err = EnrollScheduledDue(context.Background(), store, clock)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(store.runs) != 2 {
		t.Errorf("expected 2 runs after re-run, got %d", len(store.runs))
	}

	clock.Advance(5 * time.Minute)
	err = EnrollScheduledDue(context.Background(), store, clock)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(store.runs) != 2 {
		t.Errorf("expected still 2 runs (reentry policy once), got %d", len(store.runs))
	}
}

func TestBackfillSetsSubjectExternalID(t *testing.T) {
	versionID := "ver-backfill"
	store := &enrollMockStore{
		mockStore: *newMockStore(), resolvedSegment: []string{"profile-1"},
		journey: domain.Journey{ID: "j-1", CurrentVersionID: &versionID},
		version: domain.JourneyVersion{ID: versionID, TenantID: "tenant-1", WorkspaceID: "ws-1", JourneyID: "j-1", ReentryPolicy: "once", Graph: json.RawMessage(`{"entry_node_id":"n1","nodes":[],"edges":[]}`)},
	}
	store.profile = &domain.Profile{ID: "profile-1", ExternalID: "customer-backfill"}
	count, err := Backfill(context.Background(), store, domain.Principal{TenantID: "tenant-1", WorkspaceID: "ws-1"}, "j-1", "seg-1", "user-1")
	if err != nil || count != 1 {
		t.Fatalf("count=%d err=%v", count, err)
	}
	for _, run := range store.runs {
		if run.SubjectExternalID == nil || *run.SubjectExternalID != "customer-backfill" {
			t.Fatalf("missing backfill external id: %+v", run)
		}
	}
}

func TestEnrollScheduledAlwaysOncePerFiring(t *testing.T) {
	clock := NewFakeClock(time.Date(2026, 1, 1, 12, 5, 0, 0, time.UTC))
	segmentID := "seg-1"
	store := &enrollMockStore{
		mockStore:       *newMockStore(),
		resolvedSegment: []string{"profile-1"},
		scheduledVersions: []domain.JourneyVersion{{
			ID:             "ver-always",
			JourneyID:      "j-always",
			TenantID:       "tenant-1",
			WorkspaceID:    "ws-1",
			EntryKind:      "scheduled",
			EntrySegmentID: &segmentID,
			EntrySchedule:  stringPtr("*/5 * * * *"),
			ReentryPolicy:  "always",
			MaxReentries:   10,
			Status:         "active",
			Graph:          json.RawMessage(`{"entry_node_id":"n1","nodes":[],"edges":[]}`),
		}},
	}

	for range 20 { // Simulate the worker's busy-loop retries within one due minute.
		if err := EnrollScheduledDue(context.Background(), store, clock); err != nil {
			t.Fatalf("enroll scheduled firing: %v", err)
		}
	}
	if len(store.runs) != 1 {
		t.Fatalf("same firing enrolled %d runs, want 1", len(store.runs))
	}

	clock.Advance(5 * time.Minute)
	if err := EnrollScheduledDue(context.Background(), store, clock); err != nil {
		t.Fatalf("enroll next scheduled firing: %v", err)
	}
	if len(store.runs) != 2 {
		t.Fatalf("two distinct firings enrolled %d runs, want 2", len(store.runs))
	}
}

func stringPtr(s string) *string {
	return &s
}
