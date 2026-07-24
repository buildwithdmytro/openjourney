package postgres

import (
	"context"
	"os"
	"testing"

	"github.com/buildwithdmytro/openjourney/internal/domain"
)

func TestBoundedProfileResolutionAndShortLinkList(t *testing.T) {
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
	const profileCount = 1001
	if _, err := store.pool.Exec(ctx, `INSERT INTO profiles(id, tenant_id, workspace_id, app_id, external_id)
		SELECT md5($1 || g::text)::uuid, $2, $3, $4, 'bounded-profile-' || g::text
		FROM generate_series(1, $5) AS g`, tenantID, p.TenantID, p.WorkspaceID, p.AppID, profileCount); err != nil {
		t.Fatalf("insert profiles: %v", err)
	}
	lastProfileID := "ffffffff-ffff-ffff-ffff-ffffffffffff"
	if _, err := store.pool.Exec(ctx, `INSERT INTO profiles(id, tenant_id, workspace_id, app_id, external_id)
		VALUES($1, $2, $3, $4, 'bounded-profile-last')`, lastProfileID, p.TenantID, p.WorkspaceID, p.AppID); err != nil {
		t.Fatalf("insert last profile: %v", err)
	}
	seg, err := store.CreateSegment(ctx, p, domain.Segment{Name: "bounded segment"})
	if err != nil {
		t.Fatalf("create segment: %v", err)
	}
	if _, err := store.pool.Exec(ctx, `INSERT INTO segment_members(segment_id, profile_id, tenant_id, membership)
		VALUES($1, $2, $3, 'include')`, seg.ID, lastProfileID, p.TenantID); err != nil {
		t.Fatalf("insert segment member: %v", err)
	}
	ids, err := store.ResolveSegment(ctx, p, seg.ID)
	if err != nil {
		t.Fatalf("resolve segment: %v", err)
	}
	if len(ids) != 1 || ids[0] != lastProfileID {
		t.Fatalf("expected paginated resolution to include %s, got %v", lastProfileID, ids)
	}

	if _, err := store.pool.Exec(ctx, `INSERT INTO short_links(tenant_id, workspace_id, slug, destination_url)
		SELECT $1, $2, 'bounded-link-' || g::text, 'https://example.com/' || g::text
		FROM generate_series(1, 1001) AS g`, p.TenantID, p.WorkspaceID); err != nil {
		t.Fatalf("insert short links: %v", err)
	}
	links, err := store.ListShortLinks(ctx, p)
	if err != nil {
		t.Fatalf("list short links: %v", err)
	}
	if len(links) != shortLinkListLimit {
		t.Fatalf("expected short-link list limit %d, got %d", shortLinkListLimit, len(links))
	}

	_, _ = store.pool.Exec(ctx, "DELETE FROM tenants WHERE id=$1", tenantID)
}
