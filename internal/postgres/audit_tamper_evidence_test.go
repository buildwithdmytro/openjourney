package postgres

import (
	"context"
	"fmt"
	"os"
	"sync"
	"testing"
)

func TestAuditAppendOnlyAndHashChain(t *testing.T) {
	databaseURL := os.Getenv("OPENJOURNEY_TEST_DATABASE_URL")
	if databaseURL == "" {
		t.Skip("OPENJOURNEY_TEST_DATABASE_URL is not configured")
	}

	ctx := context.Background()
	store, err := Open(ctx, databaseURL)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer store.Close()

	if err := store.Migrate(ctx); err != nil {
		t.Fatalf("Migrate: %v", err)
	}

	p, _ := setupTestTenant(t, ctx, store)

	// Emit multiple audit events
	err1 := store.audit(ctx, p, "role.create", "role", "role-1", map[string]any{"name": "Admin"})
	err2 := store.audit(ctx, p, "user.create", "user", "user-1", map[string]any{"email": "admin@example.com"})
	err3 := store.audit(ctx, p, "journey.publish", "journey", "journey-1", map[string]any{"version": 1})

	if err1 != nil || err2 != nil || err3 != nil {
		t.Fatalf("audit failed: %v %v %v", err1, err2, err3)
	}

	// 1. Verify ListAuditEvents returns seq, prev_hash, row_hash
	events, err := store.ListAuditEvents(ctx, p, 10)
	if err != nil {
		t.Fatalf("ListAuditEvents: %v", err)
	}
	if len(events) < 3 {
		t.Fatalf("expected at least 3 events, got %d", len(events))
	}

	// 2. Verify VerifyAuditChain reports ok
	result, err := store.VerifyAuditChain(ctx, p)
	if err != nil {
		t.Fatalf("VerifyAuditChain: %v", err)
	}
	if !result.Intact || result.Status != "ok" {
		t.Fatalf("expected chain intact ok, got %+v", result)
	}

	// 3. Verify UPDATE and DELETE are rejected by trigger
	_, err = store.pool.Exec(ctx, `UPDATE audit_events SET action = 'tampered' WHERE tenant_id = $1`, p.TenantID)
	if err == nil {
		t.Fatal("expected UPDATE on audit_events to be rejected by trigger, but it succeeded")
	}

	_, err = store.pool.Exec(ctx, `DELETE FROM audit_events WHERE tenant_id = $1`, p.TenantID)
	if err == nil {
		t.Fatal("expected DELETE on audit_events to be rejected by trigger, but it succeeded")
	}

	// 4. Test tampering detection (disable trigger temporarily, edit row, re-enable trigger)
	_, err = store.pool.Exec(ctx, `ALTER TABLE audit_events DISABLE TRIGGER audit_events_no_update`)
	if err != nil {
		t.Fatalf("disable trigger: %v", err)
	}
	defer func() {
		_, _ = store.pool.Exec(ctx, `ALTER TABLE audit_events ENABLE TRIGGER audit_events_no_update`)
	}()

	// Modify action of second event
	var targetID string
	err = store.pool.QueryRow(ctx, `SELECT id FROM audit_events WHERE tenant_id = $1 AND seq = 2`, p.TenantID).Scan(&targetID)
	if err != nil {
		t.Fatalf("find event seq 2: %v", err)
	}

	_, err = store.pool.Exec(ctx, `UPDATE audit_events SET action = 'tampered_action' WHERE id = $1`, targetID)
	if err != nil {
		t.Fatalf("tamper edit: %v", err)
	}

	_, _ = store.pool.Exec(ctx, `ALTER TABLE audit_events ENABLE TRIGGER audit_events_no_update`)

	// Re-verify audit chain, expecting tampering detection
	tamperedResult, err := store.VerifyAuditChain(ctx, p)
	if err != nil {
		t.Fatalf("VerifyAuditChain post-tampering: %v", err)
	}
	if tamperedResult.Intact || tamperedResult.Status != "tampered" {
		t.Fatalf("expected tampering to be detected, got %+v", tamperedResult)
	}
	if tamperedResult.FirstBrokenSeq == nil || *tamperedResult.FirstBrokenSeq != 2 {
		t.Fatalf("expected broken seq 2, got %+v", tamperedResult)
	}
}

func TestAuditConcurrentWrites(t *testing.T) {
	databaseURL := os.Getenv("OPENJOURNEY_TEST_DATABASE_URL")
	if databaseURL == "" {
		t.Skip("OPENJOURNEY_TEST_DATABASE_URL is not configured")
	}

	ctx := context.Background()
	store, err := Open(ctx, databaseURL)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer store.Close()

	if err := store.Migrate(ctx); err != nil {
		t.Fatalf("Migrate: %v", err)
	}

	p, _ := setupTestTenant(t, ctx, store)

	const goroutines = 10
	const writesPerGoroutine = 5

	var wg sync.WaitGroup
	wg.Add(goroutines)

	for i := 0; i < goroutines; i++ {
		go func(workerID int) {
			defer wg.Done()
			for j := 0; j < writesPerGoroutine; j++ {
				action := fmt.Sprintf("action.%d.%d", workerID, j)
				err := store.audit(ctx, p, action, "resource", fmt.Sprintf("res-%d-%d", workerID, j), map[string]any{"worker": workerID})
				if err != nil {
					t.Errorf("worker %d audit error: %v", workerID, err)
					return
				}
			}
		}(i)
	}

	wg.Wait()

	// Verify chain is intact after concurrent writes
	result, err := store.VerifyAuditChain(ctx, p)
	if err != nil {
		t.Fatalf("VerifyAuditChain: %v", err)
	}
	if !result.Intact || result.Status != "ok" {
		t.Fatalf("expected chain intact ok after concurrent writes, got %+v", result)
	}
	if result.TotalEvents < int64(goroutines*writesPerGoroutine) {
		t.Fatalf("expected at least %d events, got %d", goroutines*writesPerGoroutine, result.TotalEvents)
	}
}
