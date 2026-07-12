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
		mockStore: *newMockStore(),
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
