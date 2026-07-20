package postgres

import (
	"context"
	"fmt"
	"os"
	"testing"
	"time"
)

func TestDeviceTokensCRUDIntegration(t *testing.T) {
	databaseURL := os.Getenv("OPENJOURNEY_TEST_DATABASE_URL")
	if databaseURL == "" {
		t.Skip("OPENJOURNEY_TEST_DATABASE_URL is not configured")
	}

	ctx := context.Background()
	store, err := Open(ctx, databaseURL)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	if err := store.Migrate(ctx); err != nil {
		t.Fatal(err)
	}

	// Setup: Need tenant, workspaces, and profiles to insert device tokens.
	tenantKey := fmt.Sprintf("tokens-test-%d", time.Now().UnixNano())
	if err := store.EnsureDevelopmentTenant(ctx, tenantKey); err != nil {
		t.Fatal(err)
	}
	p1, err := store.Authenticate(ctx, tenantKey)
	if err != nil {
		t.Fatal(err)
	}

	tenantID := p1.TenantID
	w1ID := p1.WorkspaceID
	appID := p1.AppID

	// Create workspace 2 for isolation test
	var w2ID string
	err = store.pool.QueryRow(ctx, "INSERT INTO workspaces (tenant_id, name) VALUES ($1, 'Workspace 2') RETURNING id", tenantID).Scan(&w2ID)
	if err != nil {
		t.Fatal(err)
	}

	// Create application 2 in workspace 2
	var app2ID string
	err = store.pool.QueryRow(ctx, "INSERT INTO applications (tenant_id, workspace_id, name) VALUES ($1, $2, 'App 2') RETURNING id", tenantID, w2ID).Scan(&app2ID)
	if err != nil {
		t.Fatal(err)
	}

	// Create profile in workspace 1
	var prof1 string
	err = store.pool.QueryRow(ctx, `INSERT INTO profiles (tenant_id, workspace_id, app_id, external_id) VALUES ($1, $2, $3, 'user-1') RETURNING id`,
		tenantID, w1ID, appID).Scan(&prof1)
	if err != nil {
		t.Fatal(err)
	}

	// Create profile in workspace 2
	var prof2 string
	err = store.pool.QueryRow(ctx, `INSERT INTO profiles (tenant_id, workspace_id, app_id, external_id) VALUES ($1, $2, $3, 'user-1') RETURNING id`,
		tenantID, w2ID, app2ID).Scan(&prof2)
	if err != nil {
		t.Fatal(err)
	}

	tokenVal := "token-fcm-12345"

	// 1. Register device token
	t.Run("register device token (upsert)", func(t *testing.T) {
		tok, err := store.RegisterDeviceToken(ctx, tenantID, w1ID, appID, prof1, "ios", "fcm", tokenVal)
		if err != nil {
			t.Fatal(err)
		}
		if tok.Status != "active" {
			t.Errorf("expected status to be active, got %q", tok.Status)
		}
		if tok.Token != tokenVal {
			t.Errorf("expected token to be %q, got %q", tokenVal, tok.Token)
		}

		// Re-register: should upsert and update last_seen_at
		tok2, err := store.RegisterDeviceToken(ctx, tenantID, w1ID, appID, prof1, "ios", "fcm", tokenVal)
		if err != nil {
			t.Fatal(err)
		}
		if tok2.ID != tok.ID {
			t.Errorf("expected same ID after re-registration, got %q vs %q", tok.ID, tok2.ID)
		}
	})

	// 2. Workspace isolation
	t.Run("workspace isolation", func(t *testing.T) {
		// Register in workspace 2 with same token (which is unique on tenant_id, app_id, token)
		// This should update/upsert the row to point to workspace 2 and profile 2!
		tok, err := store.RegisterDeviceToken(ctx, tenantID, w2ID, appID, prof2, "android", "fcm", tokenVal)
		if err != nil {
			t.Fatal(err)
		}

		// List active device tokens in workspace 1: should be empty now
		list1, err := store.ListActiveDeviceTokens(ctx, tenantID, w1ID, prof1)
		if err != nil {
			t.Fatal(err)
		}
		if len(list1) != 0 {
			t.Errorf("expected 0 active tokens in workspace 1, got %d", len(list1))
		}

		// List active device tokens in workspace 2: should have 1 active token
		list2, err := store.ListActiveDeviceTokens(ctx, tenantID, w2ID, prof2)
		if err != nil {
			t.Fatal(err)
		}
		if len(list2) != 1 {
			t.Fatalf("expected 1 active token in workspace 2, got %d", len(list2))
		}
		if list2[0].ID != tok.ID {
			t.Errorf("expected token ID %q, got %q", tok.ID, list2[0].ID)
		}
		if list2[0].Platform != "android" {
			t.Errorf("expected platform to be android, got %q", list2[0].Platform)
		}
	})

	// 3. Retire device token
	t.Run("retire device token", func(t *testing.T) {
		err := store.RetireDeviceToken(ctx, tenantID, appID, tokenVal)
		if err != nil {
			t.Fatal(err)
		}

		// List active: should be empty
		list2, err := store.ListActiveDeviceTokens(ctx, tenantID, w2ID, prof2)
		if err != nil {
			t.Fatal(err)
		}
		if len(list2) != 0 {
			t.Errorf("expected 0 active tokens after retirement, got %d", len(list2))
		}

		// List all by profile: should still contain the retired token
		listAll, err := store.ListDeviceTokensByProfile(ctx, tenantID, w2ID, prof2)
		if err != nil {
			t.Fatal(err)
		}
		if len(listAll) != 1 {
			t.Fatalf("expected 1 total token by profile, got %d", len(listAll))
		}
		if listAll[0].Status != "retired" {
			t.Errorf("expected token status to be retired, got %q", listAll[0].Status)
		}

		// Re-register: should reactivate to 'active'
		tok, err := store.RegisterDeviceToken(ctx, tenantID, w2ID, appID, prof2, "android", "fcm", tokenVal)
		if err != nil {
			t.Fatal(err)
		}
		if tok.Status != "active" {
			t.Errorf("expected reactivated token to be active, got %q", tok.Status)
		}
	})

	// 4. Web push subscription registration
	t.Run("web push subscription registration", func(t *testing.T) {
		webTokenVal := "https://push.example.com/subscription/web-xyz"
		tok, err := store.RegisterDeviceToken(ctx, tenantID, w1ID, appID, prof1, "web", "webpush", webTokenVal)
		if err != nil {
			t.Fatal(err)
		}
		if tok.Platform != "web" {
			t.Errorf("expected platform to be web, got %q", tok.Platform)
		}
		if tok.Provider != "webpush" {
			t.Errorf("expected provider to be webpush, got %q", tok.Provider)
		}
		if tok.Token != webTokenVal {
			t.Errorf("expected token (subscription URL) to be %q, got %q", webTokenVal, tok.Token)
		}
		if tok.Status != "active" {
			t.Errorf("expected status to be active, got %q", tok.Status)
		}

		// Verify it appears in the active tokens list
		list, err := store.ListActiveDeviceTokens(ctx, tenantID, w1ID, prof1)
		if err != nil {
			t.Fatal(err)
		}
		found := false
		for _, dt := range list {
			if dt.ID == tok.ID && dt.Platform == "web" && dt.Provider == "webpush" {
				found = true
				break
			}
		}
		if !found {
			t.Error("web push subscription not found in active tokens list")
		}

		// Verify retirement works for web tokens
		err = store.RetireDeviceToken(ctx, tenantID, appID, webTokenVal)
		if err != nil {
			t.Fatal(err)
		}
		listAfter, err := store.ListActiveDeviceTokens(ctx, tenantID, w1ID, prof1)
		if err != nil {
			t.Fatal(err)
		}
		for _, dt := range listAfter {
			if dt.ID == tok.ID {
				t.Errorf("expected web push token to be retired, but found it in active list")
			}
		}
	})
}
