package postgres

import (
	"context"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/buildwithdmytro/openjourney/internal/domain"
)

// determinism_integration_test.go — Milestone 10.7.4
//
// DB-gated integration test proving determinism:
// Once a subject/profile is assigned a variant for an experiment,
// they get the same variant regardless of channel or subsequent send attempts.

func TestExperimentDeterminismAcrossChannels(t *testing.T) {
	databaseURL := os.Getenv("OPENJOURNEY_TEST_DATABASE_URL")
	if databaseURL == "" {
		t.Skip("OPENJOURNEY_TEST_DATABASE_URL is not configured")
	}
	ctx := context.Background()
	store, err := Open(ctx, databaseURL)
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer store.Close()

	key := fmt.Sprintf("determinism-test-%d", time.Now().UnixNano())
	if err := store.EnsureDevelopmentTenant(ctx, key); err != nil {
		t.Fatalf("ensure tenant: %v", err)
	}
	p, err := store.Authenticate(ctx, key)
	if err != nil {
		t.Fatalf("authenticate: %v", err)
	}
	appID, err := store.GetFirstAppID(ctx, p.TenantID, p.WorkspaceID)
	if err != nil {
		t.Fatalf("get first app ID: %v", err)
	}
	p.AppID = appID

	// Create test profile (subject)
	profileID := ""
	err = store.pool.QueryRow(ctx,
		`INSERT INTO profiles (tenant_id, workspace_id, app_id, external_id)
		 VALUES ($1, $2, $3, 'det-profile') RETURNING id`,
		p.TenantID, p.WorkspaceID, p.AppID,
	).Scan(&profileID)
	if err != nil {
		t.Fatalf("create test profile: %v", err)
	}

	// Create template for variants
	tmpl, err := store.CreateTemplate(ctx, p, domain.Template{
		Name:            "det-temp",
		Channel:         "email",
		SubjectTemplate: ptrStr("Subject"),
		HTMLTemplate:    ptrStr("Body"),
	})
	if err != nil {
		t.Fatalf("create test template: %v", err)
	}

	// Create an experiment
	exp, err := store.CreateExperiment(ctx, p, domain.Experiment{
		Name:        "Determinism Test Experiment",
		SubjectType: "campaign",
		Status:      "running",
		Method:      "frequentist",
		Seed:        "det-seed-1",
		HoldoutPct:  0,
		Variants: []domain.ExperimentVariant{
			{Label: "control", Weight: 50, IsControl: true, TemplateID: &tmpl.ID},
			{Label: "treatment", Weight: 50, IsControl: false, TemplateID: &tmpl.ID},
		},
	})
	if err != nil {
		t.Fatalf("CreateExperiment: %v", err)
	}

	// 1. Assign the profile to 'treatment' variant first (e.g. simulating a send via Email)
	assignment1, err := store.AssignExperiment(ctx, p, exp.ID, profileID, "treatment")
	if err != nil {
		t.Fatalf("first Assignment: %v", err)
	}
	if assignment1.Variant != "treatment" {
		t.Errorf("expected variant to be 'treatment', got %q", assignment1.Variant)
	}

	// 2. Try assigning the same profile to 'control' variant (e.g. simulating a send via SMS or Push)
	assignment2, err := store.AssignExperiment(ctx, p, exp.ID, profileID, "control")
	if err != nil {
		t.Fatalf("second Assignment: %v", err)
	}

	// The returned variant must still be 'treatment' (determinstic lock-in)
	if assignment2.Variant != "treatment" {
		t.Errorf("determinism failed: expected variant to be locked to 'treatment', but got %q", assignment2.Variant)
	}
}
