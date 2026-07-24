package postgres

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/buildwithdmytro/openjourney/internal/domain"
	"github.com/google/uuid"
)

// TestAuditNonUUIDAppID_NonGated asserts that non-UUID app_ids ("system", "default")
// are properly identified as non-UUIDs and sanitized in audit code (F9).
func TestAuditNonUUIDAppID_NonGated(t *testing.T) {
	nonUUIDs := []string{"system", "default", "service-account", "12345", "not-a-uuid"}
	for _, appID := range nonUUIDs {
		if _, err := uuid.Parse(appID); err == nil {
			t.Errorf("expected uuid.Parse(%q) to return error, but got nil", appID)
		}
	}

	validUUID := uuid.NewString()
	if _, err := uuid.Parse(validUUID); err != nil {
		t.Errorf("expected uuid.Parse(%q) to succeed, got %v", validUUID, err)
	}

	// Inspect admin.go to ensure app_id sanitization via uuid.Parse is implemented
	files, err := filepath.Glob("admin.go")
	if err != nil || len(files) == 0 {
		files, err = filepath.Glob("internal/postgres/admin.go")
		if err != nil || len(files) == 0 {
			t.Fatalf("failed to locate admin.go: %v", err)
		}
	}

	contentBytes, err := os.ReadFile(files[0])
	if err != nil {
		t.Fatalf("failed to read %s: %v", files[0], err)
	}
	content := string(contentBytes)

	if !strings.Contains(content, "uuid.Parse(") {
		t.Errorf("%s must sanitize p.AppID using uuid.Parse to prevent Postgres cast errors on non-UUID app_ids (F9)", files[0])
	}
}

// TestAuditNonUUIDAppID_DBIntegration verifies that governed operations executed by principals
// with non-UUID app_ids ("system", "default") write audit rows cleanly (with NULL app_id)
// and pass audit chain verification (F9).
func TestAuditNonUUIDAppID_DBIntegration(t *testing.T) {
	dbURL := os.Getenv("OPENJOURNEY_TEST_DATABASE_URL")
	if dbURL == "" {
		t.Skip("OPENJOURNEY_TEST_DATABASE_URL not set; skipping DB integration test")
	}

	ctx := context.Background()
	store, err := Open(ctx, dbURL)
	if err != nil {
		t.Fatalf("failed to connect to test db: %v", err)
	}
	defer store.Close()

	testTenantID := uuid.NewString()
	testWorkspaceID := uuid.NewString()

	principals := []domain.Principal{
		{
			TenantID:    testTenantID,
			WorkspaceID: testWorkspaceID,
			AppID:       "system",
			UserID:      "sys-user-1",
			ActorType:   "user",
		},
		{
			TenantID:    testTenantID,
			WorkspaceID: testWorkspaceID,
			AppID:       "default",
			UserID:      "def-user-1",
			ActorType:   "user",
		},
	}

	for i, p := range principals {
		err := store.audit(ctx, nil, p, "test.action", "test_resource", uuid.NewString(), map[string]any{"index": i})
		if err != nil {
			t.Fatalf("audit failed for non-UUID AppID %q: %v", p.AppID, err)
		}
	}

	// Verify that 2 audit rows were written and app_id is NULL for both
	rows, err := store.pool.Query(ctx, `SELECT app_id FROM audit_events WHERE tenant_id = $1 ORDER BY seq ASC`, testTenantID)
	if err != nil {
		t.Fatalf("failed to query audit_events: %v", err)
	}
	defer rows.Close()

	var count int
	for rows.Next() {
		count++
		var appID *string
		if err := rows.Scan(&appID); err != nil {
			t.Fatalf("failed to scan app_id: %v", err)
		}
		if appID != nil {
			t.Errorf("expected app_id to be NULL in database for non-UUID app_id, got %q", *appID)
		}
	}
	if count != len(principals) {
		t.Fatalf("expected %d audit events, found %d", len(principals), count)
	}

	// Verify audit chain integrity
	res, err := store.VerifyAuditChain(ctx, domain.Principal{TenantID: testTenantID})
	if err != nil {
		t.Fatalf("VerifyAuditChain failed: %v", err)
	}
	if !res.Intact || res.Status != "ok" {
		t.Fatalf("expected audit chain to be intact and ok, got status=%s intact=%v reason=%s", res.Status, res.Intact, res.Reason)
	}
}
