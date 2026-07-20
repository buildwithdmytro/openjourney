package postgres

import (
	"context"
	"encoding/json"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/buildwithdmytro/openjourney/internal/domain"
)

func TestConnectorIdentityWritersRemainEventSourced(t *testing.T) {
	// Connector paths must only emit events or use the read-only projection
	// port. Keep this guard close to the identity E2E so a future connector
	// cannot silently add a second profiles/identity_* write path.
	protectedWrite := regexp.MustCompile(`(?is)\b(?:INSERT\s+INTO|UPDATE|DELETE\s+FROM)\s+(?:profiles|identity_aliases|identity_merges|identity_namespaces)\b`)
	files := []string{
		"internal/httpapi/connectors.go",
		"internal/operations/operations.go",
		"internal/postgres/connectors.go",
	}
	for _, name := range files {
		path := filepath.Join("..", "..", name)
		source, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("read %s: %v", name, err)
		}
		if name != "internal/operations/operations.go" {
			if protectedWrite.Match(source) {
				t.Fatalf("connector path %s contains a direct protected-table write", name)
			}
			continue
		}

		fset := token.NewFileSet()
		file, err := parser.ParseFile(fset, name, source, 0)
		if err != nil {
			t.Fatalf("parse %s: %v", name, err)
		}
		for _, declaration := range file.Decls {
			function, ok := declaration.(*ast.FuncDecl)
			if !ok || function.Body == nil ||
				!(strings.HasPrefix(function.Name.Name, "executeReverseETL") ||
					strings.HasPrefix(function.Name.Name, "executeWarehouseSync")) {
				continue
			}
			start := fset.Position(function.Body.Pos()).Offset
			end := fset.Position(function.Body.End()).Offset
			if protectedWrite.Match(source[start:end]) {
				t.Fatalf("connector executor %s contains a direct protected-table write", function.Name.Name)
			}
		}
	}
}

func TestIdentityMergeWinnerIsIndependentOfNamespaceArrivalOrder(t *testing.T) {
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
	store.SetBlobStore(&memoryBlobs{objects: map[string][]byte{}})
	if err := store.Migrate(ctx); err != nil {
		t.Fatal(err)
	}

	mergeWinner := func(reverseArrival bool) string {
		t.Helper()
		p, _ := setupTestTenant(t, ctx, store)
		if _, err := store.pool.Exec(ctx, `INSERT INTO identity_namespaces
			(tenant_id,app_id,namespace,priority) VALUES
			($1,$2,'user_id',10),($1,$2,'email',20)
			ON CONFLICT (tenant_id,app_id,namespace) DO UPDATE SET priority=EXCLUDED.priority`, p.TenantID, p.AppID); err != nil {
			t.Fatal(err)
		}
		project := func(e domain.Event) {
			t.Helper()
			if _, err := store.AcceptEvents(ctx, p, []domain.Event{e}); err != nil {
				t.Fatal(err)
			}
			accepted, found, err := store.ClaimProjectionJob(ctx)
			if err != nil || !found {
				t.Fatalf("claim projection job found=%v err=%v", found, err)
			}
			if err := store.ProjectEvent(ctx, accepted); err != nil {
				t.Fatal(err)
			}
		}
		aliases := []domain.Event{
			event("identity.alias", "email-profile", "arrival-email", `{"namespace":"email","value":"order-independent@example.test"}`),
			event("identity.alias", "user-profile", "arrival-user", `{"namespace":"user_id","value":"order-independent-user"}`),
		}
		if reverseArrival {
			aliases[0], aliases[1] = aliases[1], aliases[0]
		}
		for _, alias := range aliases {
			project(alias)
		}
		project(event("profile.updated", "conflict-profile", "arrival-conflict", `{"identities":{"email":"order-independent@example.test","user_id":"order-independent-user"},"attributes":{"merged":true}}`))
		var winner string
		if err := store.pool.QueryRow(ctx, `SELECT external_id FROM profiles
			WHERE tenant_id=$1 AND app_id=$2 AND merged_into IS NULL
			AND external_id IN ('email-profile','user-profile')`, p.TenantID, p.AppID).Scan(&winner); err != nil {
			t.Fatal(err)
		}
		return winner
	}

	if first, second := mergeWinner(false), mergeWinner(true); first != "user-profile" || second != first {
		t.Fatalf("namespace arrival order changed deterministic winner: first=%q second=%q", first, second)
	}
}

func TestNamespacedIdentityResolutionPreEnsureProfileAndRetroAssociates(t *testing.T) {
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
	store.SetBlobStore(&memoryBlobs{objects: map[string][]byte{}})
	if err := store.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	p, _ := setupTestTenant(t, ctx, store)
	if _, err := store.pool.Exec(ctx, `INSERT INTO identity_namespaces
		(tenant_id,app_id,namespace,priority) VALUES
		($1,$2,'user_id',10),($1,$2,'email',20),($1,$2,'phone',30)
		ON CONFLICT (tenant_id,app_id,namespace) DO NOTHING`, p.TenantID, p.AppID); err != nil {
		t.Fatal(err)
	}

	acceptAndProject := func(events ...domain.Event) {
		t.Helper()
		ids, err := store.AcceptEvents(ctx, p, events)
		if err != nil {
			t.Fatal(err)
		}
		for range ids {
			accepted, found, err := store.ClaimProjectionJob(ctx)
			if err != nil || !found {
				t.Fatalf("claim projection job found=%v err=%v", found, err)
			}
			if err := store.ProjectEvent(ctx, accepted); err != nil {
				t.Fatal(err)
			}
		}
	}

	acceptAndProject(event("identity.alias", "subject-a", "identity-email", `{"namespace":"email","value":"Person@Example.test"}`))
	acceptAndProject(event("identity.alias", "subject-a", "identity-phone", `{"namespace":"phone","value":"+1 555 0100"}`))

	// The email and phone aliases resolve a new event to the original profile
	// before ensureProfile can create a profile for its different external ID.
	acceptAndProject(event("profile.updated", "unrelated-external-id", "resolved-by-email", `{"attributes":{"resolved":true},"identities":{"email":"person@example.test"}}`))
	var liveProfiles int
	if err := store.pool.QueryRow(ctx, `SELECT count(*) FROM profiles
		WHERE tenant_id=$1 AND app_id=$2 AND merged_into IS NULL`, p.TenantID, p.AppID).Scan(&liveProfiles); err != nil {
		t.Fatal(err)
	}
	if liveProfiles != 1 {
		t.Fatalf("namespaced resolution created an extra profile: got %d", liveProfiles)
	}

	preIdentify := event("profile.updated", "", "pre-identification", `{"attributes":{"before_identify":true}}`)
	preIdentify.AnonymousID = "anonymous-subject"
	acceptAndProject(preIdentify)
	identify := event("identity.alias", "known-subject", "identify-email", `{"namespace":"email","value":"retro@example.test"}`)
	identify.AnonymousID = "anonymous-subject"
	acceptAndProject(identify)
	acceptAndProject(event("profile.updated", "another-external-id", "retro-resolved", `{"attributes":{"after_identify":true},"email":"retro@example.test"}`))

	var profileID, mergedInto string
	if err := store.pool.QueryRow(ctx, `SELECT id, COALESCE(merged_into::text,'') FROM profiles
		WHERE tenant_id=$1 AND app_id=$2 AND external_id='known-subject'`, p.TenantID, p.AppID).
		Scan(&profileID, &mergedInto); err != nil {
		t.Fatal(err)
	}
	if mergedInto != "" {
		t.Fatalf("identified profile unexpectedly tombstoned: %s", mergedInto)
	}
	var resolvedAttribute bool
	var attributes []byte
	if err := store.pool.QueryRow(ctx, `SELECT attributes FROM profiles WHERE id=$1`, profileID).Scan(&attributes); err != nil {
		t.Fatal(err)
	}
	var decoded map[string]any
	if err := json.Unmarshal(attributes, &decoded); err != nil {
		t.Fatal(err)
	}
	resolvedAttribute, _ = decoded["after_identify"].(bool)
	if !resolvedAttribute {
		t.Fatalf("post-identification event was not retro-associated: %s", attributes)
	}
}

func TestIdentityMergeIsDeterministicAndReversibleBySnapshot(t *testing.T) {
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
	blobs := &memoryBlobs{objects: map[string][]byte{}}
	store.SetBlobStore(blobs)
	if err := store.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	p, _ := setupTestTenant(t, ctx, store)
	_, err = store.pool.Exec(ctx, `INSERT INTO identity_namespaces
		(tenant_id,app_id,namespace,priority) VALUES
		($1,$2,'user_id',10),($1,$2,'email',20)
		ON CONFLICT (tenant_id,app_id,namespace) DO UPDATE SET priority=EXCLUDED.priority`, p.TenantID, p.AppID)
	if err != nil {
		t.Fatal(err)
	}
	project := func(e domain.Event) {
		t.Helper()
		if _, err := store.AcceptEvents(ctx, p, []domain.Event{e}); err != nil {
			t.Fatal(err)
		}
		accepted, found, err := store.ClaimProjectionJob(ctx)
		if err != nil || !found {
			t.Fatalf("claim projection job found=%v err=%v", found, err)
		}
		if err := store.ProjectEvent(ctx, accepted); err != nil {
			t.Fatal(err)
		}
	}
	project(event("identity.alias", "email-profile", "merge-email", `{"namespace":"email","value":"merge@example.test"}`))
	project(event("identity.alias", "user-profile", "merge-user", `{"namespace":"user_id","value":"merge-user"}`))
	// The map order is intentionally user_id then email; namespace priority, not
	// JSON/arrival order, must select user-profile as the winner.
	project(event("profile.updated", "new-external-id", "merge-conflict", `{"identities":{"user_id":"merge-user","email":"merge@example.test"},"attributes":{"merged":true}}`))

	var sourceID, targetID, mergedInto string
	if err := store.pool.QueryRow(ctx, `SELECT source_profile_id, target_profile_id FROM identity_merges
		WHERE tenant_id=$1 AND app_id=$2 ORDER BY merged_at DESC LIMIT 1`, p.TenantID, p.AppID).Scan(&sourceID, &targetID); err != nil {
		t.Fatal(err)
	}
	if err := store.pool.QueryRow(ctx, `SELECT COALESCE(merged_into::text,'') FROM profiles WHERE id=$1`, sourceID).Scan(&mergedInto); err != nil {
		t.Fatal(err)
	}
	if mergedInto != targetID {
		t.Fatalf("merge loser was not tombstoned into deterministic winner: got %q want %q", mergedInto, targetID)
	}
	var reversalRef string
	if err := store.pool.QueryRow(ctx, `SELECT reversal_ref FROM identity_merges WHERE source_profile_id=$1`, sourceID).Scan(&reversalRef); err != nil {
		t.Fatal(err)
	}
	if reversalRef == "" {
		t.Fatal("merge did not record reversal blob reference")
	}
	blobs.mu.Lock()
	_, exists := blobs.objects[reversalRef]
	blobs.mu.Unlock()
	if !exists {
		t.Fatalf("reversal blob %q was not written", reversalRef)
	}

	// The command restores the tombstoned source and its original alias edge.
	project(event("identity.unmerge", sourceID, "unmerge", `{"source_profile_id":"`+sourceID+`"}`))
	var restoredInto string
	if err := store.pool.QueryRow(ctx, `SELECT COALESCE(merged_into::text,'') FROM profiles WHERE id=$1`, sourceID).Scan(&restoredInto); err != nil {
		t.Fatal(err)
	}
	if restoredInto != "" {
		t.Fatalf("unmerge did not restore source profile: merged_into=%q", restoredInto)
	}
	var aliasOwner string
	if err := store.pool.QueryRow(ctx, `SELECT profile_id FROM identity_aliases
		WHERE tenant_id=$1 AND app_id=$2 AND namespace='email' AND value='merge@example.test'`, p.TenantID, p.AppID).Scan(&aliasOwner); err != nil {
		t.Fatal(err)
	}
	if aliasOwner != sourceID {
		t.Fatalf("unmerge did not restore source alias: got %q want %q", aliasOwner, sourceID)
	}

	// A fresh merge command after unmerge selects the same winner, independent
	// of the order in which the two namespace keys are supplied.
	project(event("profile.updated", "merge-after-unmerge", "merge-again", `{"identities":{"email":"merge@example.test","user_id":"merge-user"},"attributes":{"merged_again":true}}`))
	var rematchedTarget string
	if err := store.pool.QueryRow(ctx, `SELECT target_profile_id FROM identity_merges
		WHERE source_profile_id=$1 AND undone_at IS NULL ORDER BY merged_at DESC LIMIT 1`, sourceID).Scan(&rematchedTarget); err != nil {
		t.Fatal(err)
	}
	if rematchedTarget != targetID {
		t.Fatalf("merge after unmerge was not deterministic: got %q want %q", rematchedTarget, targetID)
	}
}

func TestIdentityMergeHardeningMultiWayAndReversibility(t *testing.T) {
	// Tests M10 hardening finding 1: multi-way merges now record every loser,
	// every loser is reversible independently, and trigger guards against updates/deletes.
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
	blobs := &memoryBlobs{objects: map[string][]byte{}}
	store.SetBlobStore(blobs)
	if err := store.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	p, _ := setupTestTenant(t, ctx, store)
	_, err = store.pool.Exec(ctx, `INSERT INTO identity_namespaces
		(tenant_id,app_id,namespace,priority) VALUES
		($1,$2,'user_id',10),($1,$2,'email',20),($1,$2,'phone',30)
		ON CONFLICT (tenant_id,app_id,namespace) DO UPDATE SET priority=EXCLUDED.priority`, p.TenantID, p.AppID)
	if err != nil {
		t.Fatal(err)
	}

	project := func(e domain.Event) {
		t.Helper()
		if _, err := store.AcceptEvents(ctx, p, []domain.Event{e}); err != nil {
			t.Fatal(err)
		}
		accepted, found, err := store.ClaimProjectionJob(ctx)
		if err != nil || !found {
			t.Fatalf("claim projection job found=%v err=%v", found, err)
		}
		if err := store.ProjectEvent(ctx, accepted); err != nil {
			t.Fatal(err)
		}
	}

	// Create three profiles identified by different namespaces
	project(event("identity.alias", "profile-a", "alias-a", `{"namespace":"email","value":"shared@example.test"}`))
	project(event("identity.alias", "profile-b", "alias-b", `{"namespace":"user_id","value":"shared-user"}`))
	project(event("identity.alias", "profile-c", "alias-c", `{"namespace":"phone","value":"+1234567890"}`))

	// One event ties all three together via their namespaces (a 3-way merge)
	// Winner is deterministic by namespace priority (lowest value wins), so profile-a wins
	mergeEventID := "three-way-merge"
	project(event("profile.updated", "winner-external-id", mergeEventID, `{
		"identities":{
			"email":"shared@example.test",
			"user_id":"shared-user",
			"phone":"+1234567890"
		}
	}`))

	// With the hardening fix: one event stitching 3 profiles should write 2 merge rows
	// (one for each loser profile-b and profile-c), each with its own reversal_ref
	var mergeCount int
	if err := store.pool.QueryRow(ctx, `SELECT COUNT(*) FROM identity_merges
		WHERE tenant_id=$1 AND app_id=$2 AND source_event_id=$3`,
		p.TenantID, p.AppID, mergeEventID).Scan(&mergeCount); err != nil {
		t.Fatal(err)
	}
	if mergeCount != 2 {
		t.Fatalf("multi-way merge did not create 2 rows (one per loser): got %d", mergeCount)
	}

	// Both losers must be reversible (have reversal_ref)
	var reversals int
	if err := store.pool.QueryRow(ctx, `SELECT COUNT(*) FROM identity_merges
		WHERE tenant_id=$1 AND app_id=$2 AND source_event_id=$3 AND reversal_ref != ''`,
		p.TenantID, p.AppID, mergeEventID).Scan(&reversals); err != nil {
		t.Fatal(err)
	}
	if reversals != 2 {
		t.Fatalf("not all losers in multi-way merge are reversible: got %d with reversal_ref", reversals)
	}

	// Unmerge one loser (profile-b)
	var profileB string
	if err := store.pool.QueryRow(ctx, `SELECT id FROM profiles
		WHERE tenant_id=$1 AND app_id=$2 AND external_id='profile-b' LIMIT 1`,
		p.TenantID, p.AppID).Scan(&profileB); err != nil {
		t.Fatal(err)
	}
	project(event("identity.unmerge", profileB, "unmerge-b", `{"source_profile_id":"`+profileB+`"}`))

	var profileBMergedInto string
	if err := store.pool.QueryRow(ctx, `SELECT COALESCE(merged_into::text,'') FROM profiles WHERE id=$1`, profileB).Scan(&profileBMergedInto); err != nil {
		t.Fatal(err)
	}
	if profileBMergedInto != "" {
		t.Fatalf("unmerge did not restore profile-b: merged_into=%q", profileBMergedInto)
	}

	// Verify the merge was marked done
	var profileBUndone int
	if err := store.pool.QueryRow(ctx, `SELECT COUNT(*) FROM identity_merges
		WHERE source_profile_id=$1 AND undone_at IS NOT NULL`, profileB).Scan(&profileBUndone); err != nil {
		t.Fatal(err)
	}
	if profileBUndone == 0 {
		t.Fatal("unmerge did not mark the merge row as undone")
	}

	// Trigger test: attempt to UPDATE a non-undone_at field on identity_merges (should fail)
	var mergeID string
	if err := store.pool.QueryRow(ctx, `SELECT id FROM identity_merges
		WHERE source_event_id=$1 AND undone_at IS NULL LIMIT 1`,
		mergeEventID).Scan(&mergeID); err != nil {
		t.Fatal(err)
	}
	_, err = store.pool.Exec(ctx, `UPDATE identity_merges SET policy_version='v2' WHERE id=$1`, mergeID)
	if err == nil {
		t.Fatal("trigger should prevent UPDATE to policy_version")
	}
	if !strings.Contains(err.Error(), "append-only") {
		t.Fatalf("expected append-only error, got: %v", err)
	}

	// Trigger test: attempt to DELETE without erasure GUC (should fail)
	_, err = store.pool.Exec(ctx, `DELETE FROM identity_merges WHERE id=$1`, mergeID)
	if err == nil {
		t.Fatal("trigger should prevent DELETE without erasure GUC")
	}
	if !strings.Contains(err.Error(), "erasure") {
		t.Fatalf("expected erasure error, got: %v", err)
	}

	// Erasure path test: SET LOCAL openjourney.erasure='on' should allow DELETE
	tx, err := store.pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := tx.Exec(ctx, "SET LOCAL openjourney.erasure='on'"); err != nil {
		tx.Rollback(ctx)
		t.Fatal(err)
	}
	if _, err := tx.Exec(ctx, `DELETE FROM identity_merges WHERE id=$1`, mergeID); err != nil {
		tx.Rollback(ctx)
		t.Fatalf("DELETE should succeed with erasure GUC: %v", err)
	}
	tx.Rollback(ctx)
	t.Logf("✓ multi-way merge writes %d rows, both reversible", mergeCount)
	t.Logf("✓ unmerge reverses the live merge")
	t.Logf("✓ trigger guards UPDATE and DELETE outside erasure")
}
