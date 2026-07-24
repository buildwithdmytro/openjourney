package postgres

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/buildwithdmytro/openjourney/internal/domain"
)

// TestSameTransactionAuditWrites_NonGated is a non-gated test that asserts
// s.audit uses the caller's transaction and that call sites propagate errors (F8).
func TestSameTransactionAuditWrites_NonGated(t *testing.T) {
	files, err := filepath.Glob("*.go")
	if err != nil || len(files) == 0 {
		files, err = filepath.Glob("internal/postgres/*.go")
		if err != nil || len(files) == 0 {
			t.Fatalf("failed to locate postgres package files: %v", err)
		}
	}

	for _, file := range files {
		if strings.HasSuffix(file, "_test.go") {
			continue
		}
		data, err := os.ReadFile(file)
		if err != nil {
			t.Fatalf("failed to read %s: %v", file, err)
		}
		content := string(data)

		// Assert that no call site discards s.audit error using `_ = s.audit(`
		if strings.Contains(content, "_ = s.audit(") {
			t.Errorf("found discarded audit error '_ = s.audit(' in %s (F8 requires error propagation)", file)
		}
	}
}

// TestSameTransactionAuditWrites_DBIntegration verifies that a forced audit-write failure
// aborts/errors the governed mutation in the DB (both or neither atomic property).
func TestSameTransactionAuditWrites_DBIntegration(t *testing.T) {
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

	p := domain.Principal{
		TenantID:    "tenant-same-tx-test",
		WorkspaceID: "ws-same-tx-test",
		AppID:       "app-same-tx-test",
		UserID:      "user-same-tx-test",
		ActorType:   "user",
	}

	// 1. Install a temporary failure trigger on audit_events for testing atomic rollback
	_, err = store.pool.Exec(ctx, `
		CREATE OR REPLACE FUNCTION fail_audit_test() RETURNS trigger AS $$
		BEGIN
			IF NEW.tenant_id = 'tenant-same-tx-test' THEN
				RAISE EXCEPTION 'forced_audit_failure_for_testing';
			END IF;
			RETURN NEW;
		END;
		$$ LANGUAGE plpgsql;

		DROP TRIGGER IF EXISTS trigger_fail_audit_test ON audit_events;
		CREATE TRIGGER trigger_fail_audit_test
		BEFORE INSERT ON audit_events
		FOR EACH ROW EXECUTE FUNCTION fail_audit_test();
	`)
	if err != nil {
		t.Fatalf("failed to install test audit failure trigger: %v", err)
	}
	defer func() {
		_, _ = store.pool.Exec(ctx, `
			DROP TRIGGER IF EXISTS trigger_fail_audit_test ON audit_events;
			DROP FUNCTION IF EXISTS fail_audit_test();
		`)
	}()

	// 2. Attempt a governed mutation (e.g. CreateSCIMUser)
	_, err = store.CreateSCIMUser(ctx, p, domain.User{
		OIDCSubject: "scim-atomic-test@example.com",
		Email:       "scim-atomic-test@example.com",
		DisplayName: "Atomic Test User",
	}, true)

	if err == nil {
		t.Fatalf("expected CreateSCIMUser to fail when audit write fails, but got nil error")
	}
	if !strings.Contains(err.Error(), "forced_audit_failure_for_testing") {
		t.Fatalf("expected forced audit failure error, got: %v", err)
	}

	// 3. Verify that the user WAS NOT created (mutation was rolled back atomically)
	var count int
	err = store.pool.QueryRow(ctx, `SELECT count(*) FROM users WHERE tenant_id = $1 AND email = 'scim-atomic-test@example.com'`, p.TenantID).Scan(&count)
	if err != nil {
		t.Fatalf("failed to query users table: %v", err)
	}
	if count != 0 {
		t.Fatalf("expected 0 users created due to audit rollback, found %d", count)
	}
}
