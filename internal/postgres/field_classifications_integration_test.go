package postgres

import (
	"context"
	"os"
	"testing"

	"github.com/buildwithdmytro/openjourney/internal/domain"
)

func TestFieldClassificationCRUD_11_5_1(t *testing.T) {
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
	key := "field-classification-crud-11-5-1"
	if err := store.EnsureDevelopmentTenant(ctx, key); err != nil {
		t.Fatal(err)
	}
	p, err := store.Authenticate(ctx, key)
	if err != nil {
		t.Fatal(err)
	}
	created, err := store.CreateFieldClassification(ctx, p, domain.FieldClassification{
		EntityType: "profile", FieldPath: "email", Classification: "confidential", SendToModel: "redact",
	})
	if err != nil {
		t.Fatal(err)
	}
	if created.ID == "" || created.TenantID != p.TenantID || created.WorkspaceID != p.WorkspaceID {
		t.Fatalf("created classification missing scope: %+v", created)
	}
	fetched, err := store.GetFieldClassification(ctx, p, created.ID)
	if err != nil || fetched.FieldPath != "email" || fetched.SendToModel != "redact" {
		t.Fatalf("get classification=%+v err=%v", fetched, err)
	}
	fetched.Classification = "restricted"
	fetched.SendToModel = "deny"
	updated, err := store.UpdateFieldClassification(ctx, p, fetched)
	if err != nil || updated.Classification != "restricted" || updated.SendToModel != "deny" {
		t.Fatalf("update classification=%+v err=%v", updated, err)
	}
	items, err := store.ListFieldClassifications(ctx, p, "profile")
	if err != nil || len(items) != 1 || items[0].ID != created.ID {
		t.Fatalf("list classifications=%+v err=%v", items, err)
	}
	if err := store.DeleteFieldClassification(ctx, p, created.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := store.GetFieldClassification(ctx, p, created.ID); err != ErrNotFound {
		t.Fatalf("expected not found after delete, got %v", err)
	}
}
