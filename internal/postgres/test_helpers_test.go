package postgres

import (
	"context"
	"crypto/sha256"
	"fmt"
	"testing"
	"time"

	"github.com/buildwithdmytro/openjourney/internal/domain"
)

// testUUID returns a valid, deterministic UUID v4 string derived from a key.
func testUUID(key string) string {
	sum := sha256.Sum256([]byte(key))
	sum[6] = (sum[6] & 0x0f) | 0x40
	sum[8] = (sum[8] & 0x3f) | 0x80
	return fmt.Sprintf("%x-%x-%x-%x-%x", sum[0:4], sum[4:6], sum[6:8], sum[8:10], sum[10:16])
}

// setupTestTenant sets up a new tenant with a default workspace, app, and quotas,
// returning the principal and tenant ID.
func setupTestTenant(t *testing.T, ctx context.Context, store *Store) (domain.Principal, string) {
	tenantKey := fmt.Sprintf("%s-%d", t.Name(), time.Now().UnixNano())
	if err := store.EnsureDevelopmentTenant(ctx, tenantKey); err != nil {
		t.Fatalf("ensure tenant: %v", err)
	}
	p, err := store.Authenticate(ctx, tenantKey)
	if err != nil {
		t.Fatalf("authenticate: %v", err)
	}
	appID, err := store.GetFirstAppID(ctx, p.TenantID, p.WorkspaceID)
	if err != nil {
		t.Fatalf("get app: %v", err)
	}
	p.AppID = appID
	return p, p.TenantID
}
