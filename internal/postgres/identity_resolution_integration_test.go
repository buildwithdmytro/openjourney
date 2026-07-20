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
