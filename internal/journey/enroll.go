package journey

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
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
			profile, err := store.GetProfileByIDSystem(ctx, v.TenantID, v.WorkspaceID, profileID)
			if err != nil {
				return fmt.Errorf("get scheduled profile: %w", err)
			}
			runs, err := store.GetJourneyRunsForProfile(ctx, v.TenantID, v.ID, profileID)
			if err != nil {
				return fmt.Errorf("get journey runs: %w", err)
			}

			var reentrySeq int
			if v.ReentryPolicy == "always" {
				// The schedule slot is the firing identity. Deriving the sequence from it
				// makes retries of the same firing hit the enrollment unique key instead
				// of observing the just-created run and choosing a new sequence.
				reentrySeq = int(slotTime.Unix() / 60)
				if len(runs) > v.MaxReentries {
					continue
				}
			} else if len(runs) == 0 {
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

			externalID := profile.ExternalID
			run := domain.JourneyRun{
				TenantID:          v.TenantID,
				WorkspaceID:       v.WorkspaceID,
				JourneyID:         v.JourneyID,
				JourneyVersionID:  v.ID,
				ProfileID:         profileID,
				SubjectExternalID: &externalID,
				EntryKey:          entryKey,
				ReentrySequence:   reentrySeq,
				Status:            "active",
				CurrentNodeID:     entryNodeID,
				State:             json.RawMessage("{}"),
			}

			step := domain.JourneyStep{TenantID: v.TenantID, NodeID: entryNodeID, Kind: "advance", Status: "pending", AvailableAt: now}
			_, inserted, err := store.EnrollJourneyRun(ctx, run, step)
			if err != nil {
				return fmt.Errorf("enroll journey run: %w", err)
			}
			if !inserted {
				continue
			}
		}
	}
	return nil
}

func isScheduledDue(schedule string, now time.Time) bool {
	fields, err := validateSchedule(schedule)
	if err != nil {
		return false
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
	exact, _ := strconv.Atoi(minField)
	return now.Minute() == exact
}

func validateSchedule(schedule string) ([]string, error) {
	if strings.TrimSpace(schedule) == "" {
		return []string{"*", "*", "*", "*", "*"}, nil
	}
	fields := strings.Fields(schedule)
	if len(fields) != 5 {
		return nil, errors.New("schedule must have exactly five fields")
	}
	for _, field := range fields[1:] {
		if field != "*" {
			return nil, errors.New("schedule only supports wildcard hour, day, month, and weekday fields")
		}
	}
	minute := fields[0]
	if minute == "*" {
		return fields, nil
	}
	if strings.HasPrefix(minute, "*/") {
		interval, err := strconv.Atoi(strings.TrimPrefix(minute, "*/"))
		if err != nil || interval < 1 || interval > 59 {
			return nil, errors.New("schedule minute interval must be between 1 and 59")
		}
		return fields, nil
	}
	exact, err := strconv.Atoi(minute)
	if err != nil || exact < 0 || exact > 59 {
		return nil, errors.New("schedule minute must be *, */1-59, or 0-59")
	}
	return fields, nil
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
		profile, err := store.GetProfileByIDSystem(ctx, v.TenantID, v.WorkspaceID, profileID)
		if err != nil {
			return 0, fmt.Errorf("get backfill profile: %w", err)
		}
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

		externalID := profile.ExternalID
		run := domain.JourneyRun{
			TenantID:          v.TenantID,
			WorkspaceID:       v.WorkspaceID,
			JourneyID:         v.JourneyID,
			JourneyVersionID:  v.ID,
			ProfileID:         profileID,
			SubjectExternalID: &externalID,
			EntryKey:          entryKey,
			ReentrySequence:   reentrySeq,
			Status:            "active",
			CurrentNodeID:     entryNodeID,
			State:             json.RawMessage("{}"),
		}

		step := domain.JourneyStep{TenantID: v.TenantID, NodeID: entryNodeID, Kind: "advance", Status: "pending", AvailableAt: now}
		_, inserted, err := store.EnrollJourneyRun(ctx, run, step)
		if err != nil {
			return 0, fmt.Errorf("create journey run: %w", err)
		}
		if !inserted {
			continue
		}

		enrolledCount++
	}

	return enrolledCount, nil
}
