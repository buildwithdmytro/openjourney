package postgres

import (
	"context"
	"encoding/json"
	"os"
	"testing"

	"github.com/buildwithdmytro/openjourney/internal/domain"
)

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
