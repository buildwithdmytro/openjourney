package postgres

import (
	"context"
	"encoding/json"
	"os"
	"testing"

	"github.com/buildwithdmytro/openjourney/internal/domain"
)

func TestCompanyMembershipAndAudienceResolution(t *testing.T) {
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
	p, tenantID := setupTestTenant(t, ctx, store)
	profileID := testUUID(tenantID + "-company-profile")
	if _, err := store.pool.Exec(ctx, `INSERT INTO profiles(id,tenant_id,workspace_id,app_id,external_id,attributes) VALUES($1,$2,$3,$4,'company-member-1','{}')`, profileID, p.TenantID, p.WorkspaceID, p.AppID); err != nil {
		t.Fatal(err)
	}
	company, err := store.CreateCompany(ctx, p, domain.Company{Name: "Acme", ExternalID: "acme", Attributes: json.RawMessage(`{"industry":"software"}`)}, []domain.CompanyMember{{ProfileID: profileID, Role: "buyer"}})
	if err != nil {
		t.Fatal(err)
	}
	if len(company.Members) != 1 || company.Members[0].ProfileID != profileID {
		t.Fatalf("company members=%+v", company.Members)
	}
	segment, err := store.CreateSegment(ctx, p, domain.Segment{Name: "Acme audience", DSL: json.RawMessage(`{"type":"company","field":"industry","operator":"equals","value":"software"}`)})
	if err != nil {
		t.Fatal(err)
	}
	ids, err := store.ResolveSegment(ctx, p, segment.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(ids) != 1 || ids[0] != "company-member-1" {
		t.Fatalf("resolved ids=%v", ids)
	}
}
