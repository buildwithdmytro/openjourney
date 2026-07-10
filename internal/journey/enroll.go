package journey

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/buildwithdmytro/openjourney/internal/domain"
	"github.com/buildwithdmytro/openjourney/internal/ports"
)

func EnrollScheduledDue(ctx context.Context, store ports.Store, clock Clock) error {
	versions, err := store.ListActiveScheduledJourneyVersions(ctx)
	if err != nil {
		return fmt.Errorf("list active scheduled versions: %w", err)
	}

	now := clock.Now().UTC()
	for _, v := range versions {
		schedule := ""
		if v.EntrySchedule != nil {
			schedule = *v.EntrySchedule
		}
		if !isScheduledDue(schedule, now) {
			continue
		}

		slotTime := now.Truncate(time.Minute)
		slotStr := slotTime.Format("2006-01-02-15-04")
		entryKey := "sched:" + v.ID + ":" + slotStr

		if v.EntrySegmentID == nil {
			continue
		}
		p := domain.Principal{
			TenantID:    v.TenantID,
			WorkspaceID: v.WorkspaceID,
			Scopes:      []string{"*"},
		}
		profileIDs, err := store.ResolveSegment(ctx, p, *v.EntrySegmentID)
		if err != nil {
			return fmt.Errorf("resolve segment: %w", err)
		}

		graphObj, err := ParseGraph(v.Graph)
		if err != nil {
			return fmt.Errorf("parse version graph: %w", err)
		}
		entryNodeID := graphObj.EntryNodeID
		if entryNodeID == "" {
			continue
		}

		for _, profileID := range profileIDs {
			runs, err := store.GetJourneyRunsForProfile(ctx, v.TenantID, v.ID, profileID)
			if err != nil {
				return fmt.Errorf("get journey runs: %w", err)
			}

			var reentrySeq int
			if len(runs) == 0 {
				reentrySeq = 0
			} else {
				if v.ReentryPolicy == "once" {
					continue
				}
				if v.ReentryPolicy == "after_exit" {
					isActiveOrWaiting := false
					for _, r := range runs {
						if r.Status == "active" || r.Status == "waiting" {
							isActiveOrWaiting = true
							break
						}
					}
					if isActiveOrWaiting {
						continue
					}
				}
				reentrySeq = runs[0].ReentrySequence + 1
				if reentrySeq > v.MaxReentries {
					continue
				}
			}

			run := domain.JourneyRun{
				TenantID:         v.TenantID,
				WorkspaceID:      v.WorkspaceID,
				JourneyID:        v.JourneyID,
				JourneyVersionID: v.ID,
				ProfileID:        profileID,
				EntryKey:         entryKey,
				ReentrySequence:  reentrySeq,
				Status:           "active",
				CurrentNodeID:    entryNodeID,
				State:            json.RawMessage("{}"),
			}

			inserted, err := store.CreateJourneyRun(ctx, run)
			if err != nil {
				return fmt.Errorf("create journey run: %w", err)
			}
			if !inserted {
				continue
			}

			runsAfterInsert, err := store.GetJourneyRunsForProfile(ctx, v.TenantID, v.ID, profileID)
			if err != nil {
				return fmt.Errorf("get runs after insert: %w", err)
			}
			if len(runsAfterInsert) == 0 {
				continue
			}
			newRunID := runsAfterInsert[0].ID

			step := domain.JourneyStep{
				RunID:       newRunID,
				TenantID:    v.TenantID,
				NodeID:      entryNodeID,
				Kind:        "advance",
				Status:      "pending",
				AvailableAt: now,
			}
			if err := store.InsertJourneyStep(ctx, step); err != nil {
				return fmt.Errorf("insert journey step: %w", err)
			}
		}
	}
	return nil
}

func isScheduledDue(schedule string, now time.Time) bool {
	if schedule == "" {
		return true
	}
	fields := strings.Fields(schedule)
	if len(fields) < 5 {
		return true
	}
	minField := fields[0]
	if minField == "*" {
		return true
	}
	if strings.HasPrefix(minField, "*/") {
		var interval int
		if _, err := fmt.Sscanf(minField, "*/%d", &interval); err == nil && interval > 0 {
			return now.Minute()%interval == 0
		}
	}
	var exact int
	if _, err := fmt.Sscanf(minField, "%d", &exact); err == nil {
		return now.Minute() == exact
	}
	return true
}

func Backfill(ctx context.Context, store ports.Store, p domain.Principal, journeyID string, segmentID string, approverUserID string) (int, error) {
	if approverUserID == "" {
		return 0, fmt.Errorf("approver user id is required")
	}
	j, err := store.GetJourney(ctx, p, journeyID)
	if err != nil {
		return 0, err
	}
	if j.CurrentVersionID == nil {
		return 0, fmt.Errorf("journey is not published")
	}

	v, err := store.GetJourneyVersion(ctx, p.TenantID, *j.CurrentVersionID)
	if err != nil {
		return 0, err
	}

	profileIDs, err := store.ResolveSegment(ctx, p, segmentID)
	if err != nil {
		return 0, fmt.Errorf("resolve segment: %w", err)
	}

	graphObj, err := ParseGraph(v.Graph)
	if err != nil {
		return 0, fmt.Errorf("parse version graph: %w", err)
	}
	entryNodeID := graphObj.EntryNodeID
	if entryNodeID == "" {
		return 0, fmt.Errorf("entry node not found in graph")
	}

	now := time.Now().UTC()
	enrolledCount := 0

	for _, profileID := range profileIDs {
		runs, err := store.GetJourneyRunsForProfile(ctx, v.TenantID, v.ID, profileID)
		if err != nil {
			return 0, fmt.Errorf("get journey runs: %w", err)
		}

		var reentrySeq int
		if len(runs) == 0 {
			reentrySeq = 0
		} else {
			if v.ReentryPolicy == "once" {
				continue
			}
			if v.ReentryPolicy == "after_exit" {
				isActiveOrWaiting := false
				for _, r := range runs {
					if r.Status == "active" || r.Status == "waiting" {
						isActiveOrWaiting = true
						break
					}
				}
				if isActiveOrWaiting {
					continue
				}
			}
			reentrySeq = runs[0].ReentrySequence + 1
			if reentrySeq > v.MaxReentries {
				continue
			}
		}

		entryKey := fmt.Sprintf("backfill:%s:%d", segmentID, now.UnixNano())

		run := domain.JourneyRun{
			TenantID:         v.TenantID,
			WorkspaceID:      v.WorkspaceID,
			JourneyID:        v.JourneyID,
			JourneyVersionID: v.ID,
			ProfileID:        profileID,
			EntryKey:         entryKey,
			ReentrySequence:  reentrySeq,
			Status:           "active",
			CurrentNodeID:    entryNodeID,
			State:            json.RawMessage("{}"),
		}

		inserted, err := store.CreateJourneyRun(ctx, run)
		if err != nil {
			return 0, fmt.Errorf("create journey run: %w", err)
		}
		if !inserted {
			continue
		}

		runsAfterInsert, err := store.GetJourneyRunsForProfile(ctx, v.TenantID, v.ID, profileID)
		if err != nil {
			return 0, fmt.Errorf("get runs after insert: %w", err)
		}
		if len(runsAfterInsert) == 0 {
			continue
		}
		newRunID := runsAfterInsert[0].ID

		step := domain.JourneyStep{
			RunID:       newRunID,
			TenantID:    v.TenantID,
			NodeID:      entryNodeID,
			Kind:        "advance",
			Status:      "pending",
			AvailableAt: now,
		}
		if err := store.InsertJourneyStep(ctx, step); err != nil {
			return 0, fmt.Errorf("insert journey step: %w", err)
		}
		enrolledCount++
	}

	return enrolledCount, nil
}
