package postgres

import (
	"context"
	"encoding/json"
	"os"
	"testing"

	"github.com/buildwithdmytro/openjourney/internal/domain"
	"github.com/buildwithdmytro/openjourney/internal/journey"
)

func TestMakerCheckerPoliciesAndEnforcement(t *testing.T) {
	databaseURL := os.Getenv("OPENJOURNEY_TEST_DATABASE_URL")
	if databaseURL == "" {
		t.Skip("OPENJOURNEY_TEST_DATABASE_URL is not configured")
	}

	ctx := context.Background()
	store, err := Open(ctx, databaseURL)
	if err != nil {
		t.Fatalf("Open failed: %v", err)
	}
	defer store.Close()
	if err := store.Migrate(ctx); err != nil {
		t.Fatalf("Migrate failed: %v", err)
	}

	pUser1, tenantID := setupTestTenant(t, ctx, store)
	pUser1.UserID = "00000000-0000-0000-0000-000000000091"
	pUser1.ActorType = "user"
	pUser1.Scopes = []string{"*"}

	pUser2 := domain.Principal{
		TenantID:    tenantID,
		WorkspaceID: pUser1.WorkspaceID,
		UserID:      "00000000-0000-0000-0000-000000000092",
		ActorType:   "user",
		Scopes:      []string{"*"},
	}

	pAPIKey := domain.Principal{
		TenantID:    tenantID,
		WorkspaceID: pUser1.WorkspaceID,
		KeyID:       "00000000-0000-0000-0000-000000000093",
		ActorType:   "api_key",
		Scopes:      []string{"*"},
	}

	t.Run("Policy CRUD and isolation", func(t *testing.T) {
		policy, err := store.SetMakerCheckerPolicy(ctx, pUser1, "journeys", true)
		if err != nil {
			t.Fatalf("SetMakerCheckerPolicy failed: %v", err)
		}
		if !policy.RequireChecker || policy.ResourceType != "journeys" {
			t.Fatalf("unexpected policy output: %+v", policy)
		}

		enabled, err := store.GetMakerCheckerPolicy(ctx, pUser1, "journeys")
		if err != nil || !enabled {
			t.Fatalf("expected GetMakerCheckerPolicy true, got %v, err=%v", enabled, err)
		}

		policies, err := store.ListMakerCheckerPolicies(ctx, pUser1)
		if err != nil || len(policies) == 0 {
			t.Fatalf("ListMakerCheckerPolicies failed: %v", err)
		}
	})

	t.Run("Self-approval blocked when maker-checker policy required", func(t *testing.T) {
		_, err := store.SetMakerCheckerPolicy(ctx, pUser1, "journeys", true)
		if err != nil {
			t.Fatalf("SetMakerCheckerPolicy failed: %v", err)
		}

		g := journey.Graph{
			Nodes: []journey.Node{
				{ID: "start", Type: journey.NodeTypeEntry, Config: json.RawMessage(`{"entry_kind":"scheduled"}`)},
			},
		}
		gBytes, _ := json.Marshal(g)

		created, err := store.CreateJourney(ctx, pUser1, domain.Journey{
			Name:  "Maker Checker Test Journey",
			Graph: gBytes,
		})
		if err != nil {
			t.Fatalf("CreateJourney failed: %v", err)
		}

		// Non-human actor must fail human gate
		_, err = store.PublishJourney(ctx, pAPIKey, created.ID, pAPIKey.KeyID, "manifest-1")
		if err == nil {
			t.Fatalf("expected error for non-human actor publishing")
		}

		// User1 (creator) publishing own draft must fail self-approval
		_, err = store.PublishJourney(ctx, pUser1, created.ID, pUser1.UserID, "manifest-1")
		if err == nil || err != ErrSelfApproval {
			t.Fatalf("expected ErrSelfApproval for creator publishing own draft, got %v", err)
		}

		// User2 (distinct authorized user) publishing draft must succeed
		version, err := store.PublishJourney(ctx, pUser2, created.ID, pUser2.UserID, "manifest-1")
		if err != nil {
			t.Fatalf("PublishJourney by distinct user failed: %v", err)
		}
		if version.Version != 1 {
			t.Fatalf("expected version 1, got %d", version.Version)
		}

		// Verify audit log has event
		events, err := store.ListAuditEvents(ctx, pUser2, 10)

		if err != nil {
			t.Fatalf("ListAuditEvents failed: %v", err)
		}
		found := false
		for _, e := range events {
			if e.ResourceType == "journey" && e.ResourceID == created.ID && e.Action == "journey.publish" {
				found = true
				if e.ActorID != pUser2.UserID {
					t.Fatalf("expected audit actor_id %q, got %q", pUser2.UserID, e.ActorID)
				}
			}
		}
		if !found {
			t.Fatalf("audit event for journey.publish not found")
		}
	})

	t.Run("Self-approval allowed when maker-checker policy disabled", func(t *testing.T) {
		_, err := store.SetMakerCheckerPolicy(ctx, pUser1, "flags", false)
		if err != nil {
			t.Fatalf("SetMakerCheckerPolicy failed: %v", err)
		}

		flagName := "MC Test Flag"
		flag, err := store.CreateFeatureFlag(ctx, pUser1, domain.FeatureFlag{
			Key:         "test_flag_mc",
			Name:        &flagName,
			Environment: "production",
			Enabled:     true,
		})


		if err != nil {
			t.Fatalf("CreateFeatureFlag failed: %v", err)
		}

		// User1 publishing own flag draft when policy disabled -> succeeds
		_, err = store.PublishFeatureFlag(ctx, pUser1, flag.ID, pUser1.UserID, "manifest-flag-1")
		if err != nil {
			t.Fatalf("expected success for creator when policy disabled, got: %v", err)
		}
	})

	t.Run("Co-author self-approval blocked and unknown creator fails closed", func(t *testing.T) {
		pUser3 := domain.Principal{
			TenantID:    tenantID,
			WorkspaceID: pUser1.WorkspaceID,
			UserID:      "00000000-0000-0000-0000-000000000094",
			ActorType:   "user",
			Scopes:      []string{"*"},
		}

		_, err := store.SetMakerCheckerPolicy(ctx, pUser1, "journeys", true)
		if err != nil {
			t.Fatalf("SetMakerCheckerPolicy failed: %v", err)
		}

		// 1. Unknown creator (fake resourceID with no audit events) -> CheckMakerChecker fails closed with ErrSelfApproval
		err = store.CheckMakerChecker(ctx, pUser1, "journeys", "unknown-journey-id", "")
		if err != ErrSelfApproval {
			t.Fatalf("expected ErrSelfApproval for unknown creator, got %v", err)
		}

		// 2. Co-author multi-actor test:
		// User1 creates journey -> audit event created (actor = User1)
		g := journey.Graph{
			Nodes: []journey.Node{
				{ID: "start", Type: journey.NodeTypeEntry, Config: json.RawMessage(`{"entry_kind":"scheduled"}`)},
			},
		}
		gBytes, _ := json.Marshal(g)
		j, err := store.CreateJourney(ctx, pUser1, domain.Journey{
			Name:  "Multi Actor Test Journey",
			Graph: gBytes,
		})
		if err != nil {
			t.Fatalf("CreateJourney failed: %v", err)
		}

		jUpdated := j
		jUpdated.Name = "Multi Actor Test Journey Updated"
		_, err = store.UpdateJourney(ctx, pUser2, jUpdated)
		if err != nil {
			t.Fatalf("UpdateJourney failed: %v", err)
		}

		// User2 (co-author who edited last) must be BLOCKED from publishing
		_, err = store.PublishJourney(ctx, pUser2, j.ID, pUser2.UserID, "manifest-coauthor")
		if err != ErrSelfApproval {
			t.Fatalf("expected ErrSelfApproval for co-author publishing, got %v", err)
		}

		// User1 (original creator) must also be BLOCKED from publishing
		_, err = store.PublishJourney(ctx, pUser1, j.ID, pUser1.UserID, "manifest-creator")
		if err != ErrSelfApproval {
			t.Fatalf("expected ErrSelfApproval for creator publishing after co-author edit, got %v", err)
		}

		// User3 (distinct authorized user) publishing MUST succeed
		_, err = store.PublishJourney(ctx, pUser3, j.ID, pUser3.UserID, "manifest-distinct")
		if err != nil {
			t.Fatalf("expected PublishJourney by distinct User3 to succeed, got %v", err)
		}
	})
}

