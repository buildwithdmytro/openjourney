package postgres

import (
	"context"
	"encoding/json"
	"os"
	"testing"

	"github.com/buildwithdmytro/openjourney/internal/domain"
)

func TestSegmentsResolution(t *testing.T) {
	databaseURL := os.Getenv("OPENJOURNEY_TEST_DATABASE_URL")
	if databaseURL == "" {
		t.Skip("OPENJOURNEY_TEST_DATABASE_URL is not configured")
	}
	ctx := context.Background()
	store, err := Open(ctx, databaseURL)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer store.Close()

	p, tenantID := setupTestTenant(t, ctx, store)

	p1ID := testUUID(tenantID + "-p-1")
	p2ID := testUUID(tenantID + "-p-2")

	_, err = store.pool.Exec(ctx, `INSERT INTO profiles(id, tenant_id, workspace_id, app_id, external_id, attributes)
		VALUES($4, $1, $2, $3, 'ext-1', '{"country":"US","age":25}'),
		      ($5, $1, $2, $3, 'ext-2', '{"country":"CA","age":30}')`, tenantID, p.WorkspaceID, p.AppID, p1ID, p2ID)
	if err != nil {
		t.Fatalf("insert profiles: %v", err)
	}

	seg, err := store.CreateSegment(ctx, p, domain.Segment{
		Name: "US Users",
		DSL: json.RawMessage(`{
			"type": "profile_attribute",
			"field": "country",
			"operator": "equals",
			"value": "US"
		}`),
	})
	if err != nil {
		t.Fatalf("create segment: %v", err)
	}

	count, perLeg, err := store.PreviewSegment(ctx, p, seg.ID)
	if err != nil {
		t.Fatalf("preview segment: %v", err)
	}
	if count != 1 {
		t.Errorf("expected count 1, got %d", count)
	}
	if perLeg["profile_attributes"] != 1 {
		t.Errorf("expected profile attributes count 1, got %v", perLeg)
	}

	ids, err := store.ResolveSegment(ctx, p, seg.ID)
	if err != nil {
		t.Fatalf("resolve segment: %v", err)
	}
	if len(ids) != 1 || ids[0] != p1ID {
		t.Errorf("expected [%q], got %v", p1ID, ids)
	}

	_, _ = store.pool.Exec(ctx, "DELETE FROM tenants WHERE id=$1", tenantID)
}
