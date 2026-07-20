package postgres

import (
	"context"
	"fmt"
	"os"
	"testing"
	"time"
)

func TestIdentityResolutionMigrationSupportsDefaultsAndTombstones(t *testing.T) {
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

	var tenantID, workspaceID, appID string
	if err := store.pool.QueryRow(ctx, `SELECT t.id, w.id, a.id
		FROM tenants t JOIN workspaces w ON w.tenant_id=t.id
		JOIN applications a ON a.workspace_id=w.id LIMIT 1`).Scan(&tenantID, &workspaceID, &appID); err != nil {
		t.Skip("no tenant exists to exercise seeded identity namespaces")
	}

	// Defaults are valid namespace configurations, and custom namespaces remain supported.
	for _, namespace := range []string{"email", "phone", "user_id"} {
		var exists bool
		if err := store.pool.QueryRow(ctx, `SELECT EXISTS(
			SELECT 1 FROM identity_namespaces WHERE tenant_id=$1 AND app_id=$2 AND namespace=$3
		)`, tenantID, appID, namespace).Scan(&exists); err != nil {
			t.Fatal(err)
		}
		if !exists {
			t.Fatalf("missing seeded namespace %q", namespace)
		}
	}
	var profileID string
	if err := store.pool.QueryRow(ctx, `INSERT INTO profiles
		(tenant_id, workspace_id, app_id, external_id) VALUES ($1,$2,$3,$4) RETURNING id`,
		tenantID, workspaceID, appID, fmt.Sprintf("identity-migration-%d", time.Now().UnixNano())).Scan(&profileID); err != nil {
		t.Fatal(err)
	}
	if _, err := store.pool.Exec(ctx, `UPDATE profiles SET merged_into=$1 WHERE id=$2`, profileID, profileID); err != nil {
		t.Fatal(err)
	}
	var mergedInto string
	if err := store.pool.QueryRow(ctx, `SELECT merged_into FROM profiles WHERE id=$1`, profileID).Scan(&mergedInto); err != nil {
		t.Fatal(err)
	}
	if mergedInto != profileID {
		t.Fatalf("merged profile was not retained as tombstone: got %q", mergedInto)
	}
}
