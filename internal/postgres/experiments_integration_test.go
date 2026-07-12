package postgres

import (
	"context"
	"errors"
	"os"
	"testing"

	"github.com/buildwithdmytro/openjourney/internal/domain"
)

func TestExperimentStoreIsolationAssignmentAndSeedGuard(t *testing.T) {
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
	if _, err := store.pool.Exec(ctx, "TRUNCATE tenants CASCADE"); err != nil {
		t.Fatal(err)
	}
	if err := store.EnsureDevelopmentTenant(ctx, "experiment-store"); err != nil {
		t.Fatal(err)
	}
	p1, err := store.Authenticate(ctx, "experiment-store")
	if err != nil {
		t.Fatal(err)
	}

	var workspace2, app2 string
	if err := store.pool.QueryRow(ctx, `INSERT INTO workspaces (tenant_id,name) VALUES ($1,'Other') RETURNING id`, p1.TenantID).Scan(&workspace2); err != nil {
		t.Fatal(err)
	}
	if err := store.pool.QueryRow(ctx, `INSERT INTO applications (tenant_id,workspace_id,name) VALUES ($1,$2,'Other') RETURNING id`, p1.TenantID, workspace2).Scan(&app2); err != nil {
		t.Fatal(err)
	}
	p2 := p1
	p2.WorkspaceID, p2.AppID = workspace2, app2

	created, err := store.CreateExperiment(ctx, p1, domain.Experiment{
		Name: "Subject line", SubjectType: "campaign", Seed: "fixed-seed",
		Variants: []domain.ExperimentVariant{{Label: "control", Weight: 50, IsControl: true}, {Label: "a", Weight: 50}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(created.Variants) != 2 || created.Status != "draft" || created.Method != "frequentist" {
		t.Fatalf("created experiment = %+v", created)
	}
	got, err := store.GetExperiment(ctx, p1, created.ID)
	if err != nil || len(got.Variants) != 2 {
		t.Fatalf("GetExperiment = %+v, %v", got, err)
	}
	if _, err := store.GetExperiment(ctx, p2, created.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("cross-workspace get err = %v", err)
	}
	list, err := store.ListExperiments(ctx, p2)
	if err != nil || len(list) != 0 {
		t.Fatalf("cross-workspace list = %+v, %v", list, err)
	}
	created.Name = "Updated"
	created.Status = "running"
	created.Variants = []domain.ExperimentVariant{{Label: "control", Weight: 40, IsControl: true}, {Label: "a", Weight: 60}}
	running, err := store.UpdateExperiment(ctx, p1, created)
	if err != nil || running.Name != "Updated" {
		t.Fatalf("UpdateExperiment = %+v, %v", running, err)
	}
	created.Seed = "changed-seed"
	if _, err := store.UpdateExperiment(ctx, p1, created); !errors.Is(err, ErrNotFound) {
		t.Fatalf("running seed mutation err = %v", err)
	}
	if got, err = store.GetExperiment(ctx, p1, created.ID); err != nil || got.Seed != "fixed-seed" {
		t.Fatalf("seed after rejected mutation = %q, %v", got.Seed, err)
	}

	var profileID string
	if err := store.pool.QueryRow(ctx, `INSERT INTO profiles (tenant_id,workspace_id,app_id,external_id) VALUES ($1,$2,$3,'assignment-profile') RETURNING id`, p1.TenantID, p1.WorkspaceID, p1.AppID).Scan(&profileID); err != nil {
		t.Fatal(err)
	}
	first, err := store.AssignExperiment(ctx, p1, created.ID, profileID, "control")
	if err != nil {
		t.Fatal(err)
	}
	second, err := store.AssignExperiment(ctx, p1, created.ID, profileID, "a")
	if err != nil {
		t.Fatal(err)
	}
	if first.Variant != "control" || second.Variant != first.Variant || !second.AssignedAt.Equal(first.AssignedAt) {
		t.Fatalf("assignment was not stable: first=%+v second=%+v", first, second)
	}
	if _, err := store.AssignExperiment(ctx, p2, created.ID, profileID, "a"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("cross-workspace assignment err = %v", err)
	}
}
