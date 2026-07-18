// Package stages applies lifecycle-stage rules using the event pipeline.
package stages

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"time"

	"github.com/buildwithdmytro/openjourney/internal/domain"
)

type Store interface {
	ListStageRuleScopes(context.Context) ([][2]string, error)
	ListStageRules(context.Context, string, string) ([]domain.StageRule, error)
	ResolveSegment(context.Context, domain.Principal, string) ([]string, error)
	GetProfileByIDSystem(context.Context, string, string, string) (domain.Profile, error)
	GetFirstAppID(context.Context, string, string) (string, error)
	AcceptEvents(context.Context, domain.Principal, []domain.Event) ([]string, error)
}

func ApplyAll(ctx context.Context, store Store) (int, error) {
	scopes, err := store.ListStageRuleScopes(ctx)
	if err != nil {
		return 0, err
	}
	total := 0
	for _, scope := range scopes {
		changed, err := Apply(ctx, store, scope[0], scope[1])
		if err != nil {
			return total, err
		}
		total += changed
	}
	return total, nil
}

// Apply evaluates enabled rules in priority order and emits the stage change
// through AcceptEvents. The profile projector remains the only profile writer.
func Apply(ctx context.Context, store Store, tenantID, workspaceID string) (int, error) {
	rules, err := store.ListStageRules(ctx, tenantID, workspaceID)
	if err != nil {
		return 0, err
	}
	sort.SliceStable(rules, func(i, j int) bool {
		if rules[i].Priority != rules[j].Priority {
			return rules[i].Priority > rules[j].Priority
		}
		return rules[i].ID < rules[j].ID
	})
	appID, err := store.GetFirstAppID(ctx, tenantID, workspaceID)
	if err != nil {
		return 0, err
	}
	p := domain.Principal{TenantID: tenantID, WorkspaceID: workspaceID, AppID: appID, ActorType: "system", Scopes: []string{"*"}}

	assigned := make(map[string]domain.StageRule)
	for _, rule := range rules {
		if !rule.Enabled || rule.SegmentID == "" {
			continue
		}
		profileIDs, err := store.ResolveSegment(ctx, p, rule.SegmentID)
		if err != nil {
			return 0, fmt.Errorf("resolve stage rule %s: %w", rule.ID, err)
		}
		for _, profileID := range profileIDs {
			if _, exists := assigned[profileID]; !exists {
				assigned[profileID] = rule
			}
		}
	}

	changed := 0
	for profileID, rule := range assigned {
		profile, err := store.GetProfileByIDSystem(ctx, tenantID, workspaceID, profileID)
		if err != nil {
			return 0, err
		}
		var attrs map[string]any
		if len(profile.Attributes) > 0 {
			_ = json.Unmarshal(profile.Attributes, &attrs)
		}
		if attrs != nil && attrs["stage"] == rule.Stage {
			continue
		}
		now := time.Now().UTC()
		stagePayload, _ := json.Marshal(map[string]any{"profile_id": profileID, "stage": rule.Stage, "rule_id": rule.ID})
		profilePayload, _ := json.Marshal(map[string]any{"attributes": map[string]any{"stage": rule.Stage}})
		_, err = store.AcceptEvents(ctx, p, []domain.Event{
			{Type: "stage.changed", SchemaVersion: 1, ExternalID: profile.ExternalID, AnonymousID: profile.AnonymousID,
				IdempotencyKey: "stage:" + rule.ID + ":" + profileID, OccurredAt: now, Source: "stage_rules", Payload: stagePayload},
			{Type: "profile.updated", SchemaVersion: 1, ExternalID: profile.ExternalID, AnonymousID: profile.AnonymousID,
				IdempotencyKey: "stage-profile:" + rule.ID + ":" + profileID, OccurredAt: now, Source: "stage_rules", Payload: profilePayload},
		})
		if err != nil {
			return 0, err
		}
		changed++
	}
	return changed, nil
}
