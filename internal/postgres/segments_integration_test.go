package postgres

import (
	"context"
	"encoding/json"
	"os"
	"testing"
	"time"

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

	tenantID := "tenant-seg-test-" + time.Now().Format("20060102-150405")
	p := domain.Principal{TenantID: tenantID, WorkspaceID: "workspace-1", AppID: "app-1"}

	_, err = store.pool.Exec(ctx, `INSERT INTO tenants(id, name) VALUES($1, 'Test Tenant')`, tenantID)
	if err != nil {
		t.Fatalf("insert tenant: %v", err)
	}
	_, err = store.pool.Exec(ctx, `INSERT INTO workspaces(id, tenant_id, name) VALUES($1, $2, 'Test Workspace')`, p.WorkspaceID, tenantID)
	if err != nil {
		t.Fatalf("insert workspace: %v", err)
	}
	_, err = store.pool.Exec(ctx, `INSERT INTO applications(id, tenant_id, workspace_id, name) VALUES($1, $2, $3, 'Test App')`, p.AppID, tenantID, p.WorkspaceID)
	if err != nil {
		t.Fatalf("insert application: %v", err)
	}

	_, err = store.pool.Exec(ctx, `INSERT INTO profiles(id, tenant_id, workspace_id, app_id, external_id, attributes)
		VALUES('p-1', $1, $2, $3, 'ext-1', '{"country":"US","age":25}'),
		      ('p-2', $1, $2, $3, 'ext-2', '{"country":"CA","age":30}')`, tenantID, p.WorkspaceID, p.AppID)
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
	if len(ids) != 1 || ids[0] != "p-1" {
		t.Errorf("expected ['p-1'], got %v", ids)
	}

	_, _ = store.pool.Exec(ctx, "DELETE FROM tenants WHERE id=$1", tenantID)
}
