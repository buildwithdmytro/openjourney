package operations

import (
	"context"
	"os"
	"strings"
	"testing"

	"github.com/buildwithdmytro/openjourney/internal/postgres"
	"github.com/buildwithdmytro/openjourney/internal/projector"
)

type integrationImportBlobs struct{ objects map[string][]byte }

func (b *integrationImportBlobs) Put(_ context.Context, key string, data []byte, _ string) error {
	if b.objects == nil {
		b.objects = map[string][]byte{}
	}
	b.objects[key] = append([]byte(nil), data...)
	return nil
}
func (b *integrationImportBlobs) Get(_ context.Context, key string) ([]byte, error) {
	return append([]byte(nil), b.objects[key]...), nil
}
func (b *integrationImportBlobs) Delete(context.Context, string) error { return nil }

func TestImportReplaysAreIdempotentThroughProjection(t *testing.T) {
	databaseURL := os.Getenv("OPENJOURNEY_TEST_DATABASE_URL")
	if databaseURL == "" {
		t.Skip("OPENJOURNEY_TEST_DATABASE_URL is not configured")
	}
	ctx := context.Background()
	store, err := postgres.Open(ctx, databaseURL)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	if err := store.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	tenantKey := "import-idempotency-" + strings.ReplaceAll(t.Name(), "/", "-")
	if err := store.EnsureDevelopmentTenant(ctx, tenantKey); err != nil {
		t.Fatal(err)
	}
	p, err := store.Authenticate(ctx, tenantKey)
	if err != nil {
		t.Fatal(err)
	}
	p.AppID, err = store.GetFirstAppID(ctx, p.TenantID, p.WorkspaceID)
	if err != nil {
		t.Fatal(err)
	}
	blobs := &integrationImportBlobs{}
	key := "imports/" + p.TenantID + "/profiles-idempotent.csv"
	if err := blobs.Put(ctx, key, []byte("email,name\na@example.com,Alice\n"), "text/csv"); err != nil {
		t.Fatal(err)
	}
	request, err := store.CreateImportRequest(ctx, p, "profiles", key, []byte(`{"email":"email","name":"name"}`), p.AppID)
	if err != nil {
		t.Fatal(err)
	}
	if err := executeImport(ctx, store, blobs, request.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := projector.Drain(ctx, store, 10, false); err != nil {
		t.Fatal(err)
	}
	companyRequest, err := store.CreateImportRequest(ctx, p, "companies", key, []byte(`{"email":"email","name":"name"}`), p.AppID)
	if err != nil {
		t.Fatal(err)
	}
	if err := executeImport(ctx, store, blobs, companyRequest.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := projector.Drain(ctx, store, 10, false); err != nil {
		t.Fatal(err)
	}
	if err := executeImport(ctx, store, blobs, companyRequest.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := projector.Drain(ctx, store, 10, false); err != nil {
		t.Fatal(err)
	}
	if err := executeImport(ctx, store, blobs, request.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := projector.Drain(ctx, store, 10, false); err != nil {
		t.Fatal(err)
	}
	var profiles, events int
	if err := store.Pool().QueryRow(ctx, `SELECT count(*) FROM profiles WHERE tenant_id=$1 AND external_id=$2`, p.TenantID, "a@example.com").Scan(&profiles); err != nil {
		t.Fatal(err)
	}
	if err := store.Pool().QueryRow(ctx, `SELECT count(*) FROM accepted_events WHERE tenant_id=$1 AND app_id=$2 AND idempotency_key=$3`, p.TenantID, p.AppID, "profiles.import:"+key+":1").Scan(&events); err != nil {
		t.Fatal(err)
	}
	if profiles != 1 || events != 1 {
		t.Fatalf("replayed import created profiles=%d events=%d, want one each", profiles, events)
	}
	var companies, companyEvents int
	if err := store.Pool().QueryRow(ctx, `SELECT count(*) FROM companies WHERE tenant_id=$1 AND external_id=$2`, p.TenantID, "a@example.com").Scan(&companies); err != nil {
		t.Fatal(err)
	}
	if err := store.Pool().QueryRow(ctx, `SELECT count(*) FROM accepted_events WHERE tenant_id=$1 AND app_id=$2 AND idempotency_key=$3`, p.TenantID, p.AppID, "companies.import:"+key+":1").Scan(&companyEvents); err != nil {
		t.Fatal(err)
	}
	if companies != 1 || companyEvents != 1 {
		t.Fatalf("replayed company import created companies=%d events=%d, want one each", companies, companyEvents)
	}
	var resultRef string
	if err := store.Pool().QueryRow(ctx, `SELECT result_ref FROM import_requests WHERE id=$1 AND status='complete'`, request.ID).Scan(&resultRef); err != nil {
		t.Fatal(err)
	}
	if resultRef == "" || len(blobs.objects[resultRef]) == 0 {
		t.Fatalf("missing per-row result report: ref=%q", resultRef)
	}
}
