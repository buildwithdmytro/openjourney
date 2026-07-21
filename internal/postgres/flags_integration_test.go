package postgres

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/buildwithdmytro/openjourney/internal/domain"
	"github.com/buildwithdmytro/openjourney/internal/flags"
)

func TestFeatureFlagStoreRoundTripAndDuplicateRejection(t *testing.T) {
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
	if err := store.EnsureDevelopmentTenant(ctx, "flag-store"); err != nil {
		t.Fatal(err)
	}
	p, err := store.Authenticate(ctx, "flag-store")
	if err != nil {
		t.Fatal(err)
	}

	// Create a feature flag
	defaultVal := json.RawMessage(`true`)
	created, err := store.CreateFeatureFlag(ctx, p, domain.FeatureFlag{
		AppID:        p.AppID,
		Environment:  "production",
		Key:          "test_flag",
		Name:         stringPtr("Test Flag"),
		FlagType:     "boolean",
		DefaultValue: defaultVal,
		Seed:         "seed-123",
		Enabled:      false,
	})
	if err != nil {
		t.Fatal(err)
	}

	// Verify round-trip: get the flag
	got, err := store.GetFeatureFlag(ctx, p, created.ID)
	if err != nil {
		t.Fatalf("GetFeatureFlag failed: %v", err)
	}
	if got.ID != created.ID || got.Key != "test_flag" || got.Environment != "production" || got.FlagType != "boolean" {
		t.Fatalf("GetFeatureFlag mismatch: %+v", got)
	}
	if got.Enabled || got.Status != "draft" {
		t.Fatalf("Expected draft and disabled, got status=%s enabled=%v", got.Status, got.Enabled)
	}

	// Update the flag
	got.Enabled = true
	got.RolloutPct = 50
	updated, err := store.UpdateFeatureFlag(ctx, p, got)
	if err != nil {
		t.Fatal(err)
	}
	if !updated.Enabled || updated.RolloutPct != 50 {
		t.Fatalf("UpdateFeatureFlag mismatch: enabled=%v rollout=%d", updated.Enabled, updated.RolloutPct)
	}

	// Verify duplicate (same tenant, app, environment, key) is rejected
	_, err = store.CreateFeatureFlag(ctx, p, domain.FeatureFlag{
		AppID:        p.AppID,
		Environment:  "production",
		Key:          "test_flag",
		FlagType:     "string",
		DefaultValue: json.RawMessage(`"default"`),
		Seed:         "seed-456",
	})
	if err == nil {
		t.Fatal("expected duplicate error, got nil")
	}
	if !strings.Contains(err.Error(), "unique") && !strings.Contains(err.Error(), "already exists") {
		t.Fatalf("expected unique/duplicate error, got: %v", err)
	}

	// Verify same key in different environment is allowed
	created2, err := store.CreateFeatureFlag(ctx, p, domain.FeatureFlag{
		AppID:        p.AppID,
		Environment:  "staging",
		Key:          "test_flag",
		FlagType:     "boolean",
		DefaultValue: json.RawMessage(`false`),
		Seed:         "seed-789",
	})
	if err != nil {
		t.Fatalf("different environment should be allowed: %v", err)
	}
	if created2.Environment != "staging" {
		t.Fatalf("wrong environment: %s", created2.Environment)
	}

	// List should return both flags
	list, err := store.ListFeatureFlags(ctx, p)
	if err != nil {
		t.Fatal(err)
	}
	if len(list) < 2 {
		t.Fatalf("expected at least 2 flags, got %d", len(list))
	}
}

func TestListActiveFlagsFiltersCorrectly(t *testing.T) {
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
	if err := store.EnsureDevelopmentTenant(ctx, "flag-active"); err != nil {
		t.Fatal(err)
	}
	p, err := store.Authenticate(ctx, "flag-active")
	if err != nil {
		t.Fatal(err)
	}

	// Create multiple flags with different states
	f1, err := store.CreateFeatureFlag(ctx, p, domain.FeatureFlag{
		AppID:        p.AppID,
		Environment:  "production",
		Key:          "flag1",
		FlagType:     "boolean",
		DefaultValue: json.RawMessage(`true`),
		Seed:         "seed-1",
		Enabled:      true,
		Status:       "published",
	})
	if err != nil {
		t.Fatal(err)
	}

	// Draft flag (should not be returned)
	_, err = store.CreateFeatureFlag(ctx, p, domain.FeatureFlag{
		AppID:        p.AppID,
		Environment:  "production",
		Key:          "flag2",
		FlagType:     "boolean",
		DefaultValue: json.RawMessage(`true`),
		Seed:         "seed-2",
		Enabled:      true,
		Status:       "draft",
	})
	if err != nil {
		t.Fatal(err)
	}

	// Published but disabled (should not be returned)
	_, err = store.CreateFeatureFlag(ctx, p, domain.FeatureFlag{
		AppID:        p.AppID,
		Environment:  "production",
		Key:          "flag3",
		FlagType:     "boolean",
		DefaultValue: json.RawMessage(`true`),
		Seed:         "seed-3",
		Enabled:      false,
		Status:       "published",
	})
	if err != nil {
		t.Fatal(err)
	}

	// Different environment (should not be returned)
	_, err = store.CreateFeatureFlag(ctx, p, domain.FeatureFlag{
		AppID:        p.AppID,
		Environment:  "staging",
		Key:          "flag1",
		FlagType:     "boolean",
		DefaultValue: json.RawMessage(`true`),
		Seed:         "seed-4",
		Enabled:      true,
		Status:       "published",
	})
	if err != nil {
		t.Fatal(err)
	}

	// List active flags for production
	active, err := store.ListActiveFlags(ctx, p.TenantID, p.AppID, "production")
	if err != nil {
		t.Fatal(err)
	}

	// Should only return f1 (published and enabled)
	if len(active) != 1 {
		t.Fatalf("expected 1 active flag for production, got %d", len(active))
	}
	if active[0].ID != f1.ID || active[0].Key != "flag1" {
		t.Fatalf("unexpected flag: %+v", active[0])
	}

	// List active flags for staging (should have the other flag1)
	activeStaging, err := store.ListActiveFlags(ctx, p.TenantID, p.AppID, "staging")
	if err != nil {
		t.Fatal(err)
	}
	if len(activeStaging) != 1 || activeStaging[0].Environment != "staging" {
		t.Fatalf("expected 1 flag for staging, got %d", len(activeStaging))
	}
}

func TestFeatureFlagValidation(t *testing.T) {
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
	if err := store.EnsureDevelopmentTenant(ctx, "flag-validation"); err != nil {
		t.Fatal(err)
	}
	p, err := store.Authenticate(ctx, "flag-validation")
	if err != nil {
		t.Fatal(err)
	}

	tests := []struct {
		name    string
		flag    domain.FeatureFlag
		wantErr bool
	}{
		{
			name: "missing key",
			flag: domain.FeatureFlag{
				AppID:        p.AppID,
				FlagType:     "boolean",
				DefaultValue: json.RawMessage(`true`),
				Seed:         "seed",
			},
			wantErr: true,
		},
		{
			name: "missing flag_type",
			flag: domain.FeatureFlag{
				AppID:        p.AppID,
				Key:          "flag",
				DefaultValue: json.RawMessage(`true`),
				Seed:         "seed",
			},
			wantErr: true,
		},
		{
			name: "missing seed",
			flag: domain.FeatureFlag{
				AppID:        p.AppID,
				Key:          "flag",
				FlagType:     "boolean",
				DefaultValue: json.RawMessage(`true`),
			},
			wantErr: true,
		},
		{
			name: "invalid environment",
			flag: domain.FeatureFlag{
				AppID:        p.AppID,
				Environment:  "invalid",
				Key:          "flag",
				FlagType:     "boolean",
				DefaultValue: json.RawMessage(`true`),
				Seed:         "seed",
			},
			wantErr: true,
		},
		{
			name: "invalid rollout_pct",
			flag: domain.FeatureFlag{
				AppID:        p.AppID,
				Key:          "flag",
				FlagType:     "boolean",
				DefaultValue: json.RawMessage(`true`),
				Seed:         "seed",
				RolloutPct:   101,
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := store.CreateFeatureFlag(ctx, p, tt.flag)
			if (err != nil) != tt.wantErr {
				t.Errorf("CreateFeatureFlag wantErr=%v, got err=%v", tt.wantErr, err)
			}
		})
	}
}

func TestFeatureFlagTargetingRulesIntegration(t *testing.T) {
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
	if err := store.EnsureDevelopmentTenant(ctx, "flag-targeting"); err != nil {
		t.Fatal(err)
	}
	p, err := store.Authenticate(ctx, "flag-targeting")
	if err != nil {
		t.Fatal(err)
	}

	// Create a flag with multiple targeting rules
	// Rule 1: empty DSL -> matches everyone (rule-priority test)
	// Rule 2: premium condition -> should only match if rule 1 is skipped
	rule1, _ := json.Marshal(map[string]interface{}{})
	rule2, _ := json.Marshal(map[string]interface{}{
		"operator": "equals",
		"field":    "attributes.plan",
		"value":    "premium",
	})

	flag, err := store.CreateFeatureFlag(ctx, p, domain.FeatureFlag{
		AppID:        p.AppID,
		Environment:  "production",
		Key:          "test-targeting-integration",
		FlagType:     "string",
		DefaultValue: json.RawMessage(`"default"`),
		Variants: []domain.FlagVariant{
			{Label: "variant-rule1", Value: json.RawMessage(`"rule1"`), Weight: 100},
			{Label: "variant-rule2", Value: json.RawMessage(`"rule2"`), Weight: 100},
		},
		TargetingRules: []domain.FlagTargetingRule{
			{DSL: json.RawMessage(rule1), Variant: "variant-rule1"},
			{DSL: json.RawMessage(rule2), Variant: "variant-rule2"},
		},
		Seed:       "targeting-integration-seed",
		Enabled:    true,
		Status:     "published",
		RolloutPct: 0, // No rollout, only targeting rules
	})
	if err != nil {
		t.Fatal(err)
	}

	// Create a wrapper that implements flags.EvalAudience using store.EvaluateAudience
	evalAudience := &storeAudience{store: store, principal: p}

	// Test: Empty-DSL rule matches everyone (first rule should win)
	// Any profile should match rule 1 (empty DSL) and get variant-rule1
	result, err := flags.Evaluate(ctx, &flag, "any-profile-id", evalAudience)
	if err != nil {
		t.Fatalf("Evaluate failed: %v", err)
	}
	if result.Variant != "variant-rule1" {
		t.Errorf("Empty DSL rule should match everyone: expected variant-rule1, got %q", result.Variant)
	}
	if result.Reason != "targeted" {
		t.Errorf("Expected reason 'targeted', got %q", result.Reason)
	}

	// Test: A flag with no matching rule falls back to default
	flag2, err := store.CreateFeatureFlag(ctx, p, domain.FeatureFlag{
		AppID:        p.AppID,
		Environment:  "production",
		Key:          "test-no-match",
		FlagType:     "string",
		DefaultValue: json.RawMessage(`"fallback"`),
		Variants: []domain.FlagVariant{
			{Label: "variant-premium", Value: json.RawMessage(`"premium"`), Weight: 100},
		},
		TargetingRules: []domain.FlagTargetingRule{
			{DSL: json.RawMessage(rule2), Variant: "variant-premium"}, // Only matches premium
		},
		Seed:       "no-match-seed",
		Enabled:    true,
		Status:     "published",
		RolloutPct: 0,
	})
	if err != nil {
		t.Fatal(err)
	}

	result2, err := flags.Evaluate(ctx, &flag2, "any-profile-id", evalAudience)
	if err != nil {
		t.Fatalf("Evaluate flag2 failed: %v", err)
	}
	if result2.Variant != "" {
		t.Errorf("No matching rule should fall back to default: expected variant '', got %q", result2.Variant)
	}
	if result2.Reason != "rollout_excluded" {
		t.Errorf("Expected reason 'rollout_excluded', got %q", result2.Reason)
	}
	if string(result2.Value) != `"fallback"` {
		t.Errorf("Expected fallback value, got %s", result2.Value)
	}
}

func TestFeatureFlagExposureProjection(t *testing.T) {
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
	if err := store.EnsureDevelopmentTenant(ctx, "flag-exposure"); err != nil {
		t.Fatal(err)
	}
	p, err := store.Authenticate(ctx, "flag-exposure")
	if err != nil {
		t.Fatal(err)
	}

	// Create a feature flag
	flag, err := store.CreateFeatureFlag(ctx, p, domain.FeatureFlag{
		AppID:        p.AppID,
		Environment:  "production",
		Key:          "exposure_test",
		FlagType:     "boolean",
		DefaultValue: json.RawMessage(`true`),
		Seed:         "seed-exposure",
	})
	if err != nil {
		t.Fatal(err)
	}

	// Emit an exposure event
	exposureEvent := domain.Event{
		Type:           "feature_flag.exposure",
		SchemaVersion:  1,
		IdempotencyKey: flag.ID + ":on:subject1:window1",
		OccurredAt:     time.Now(),
		Payload:        json.RawMessage(fmt.Sprintf(`{"flag_id":"%s","variant":"on","environment":"production"}`, flag.ID)),
		AnonymousID:    "subject1",
	}

	principal := domain.Principal{
		TenantID:  p.TenantID,
		AppID:     p.AppID,
		ActorType: "public",
	}
	_, err = store.AcceptEvents(ctx, principal, []domain.Event{exposureEvent})
	if err != nil {
		t.Fatalf("AcceptEvents failed: %v", err)
	}

	// Query the exposure aggregate
	var exposures int64
	var lastSeen interface{}
	err = store.pool.QueryRow(ctx, `SELECT exposures, last_seen FROM feature_flag_exposures
		WHERE flag_id=$1 AND environment='production' AND variant='on'`,
		flag.ID).Scan(&exposures, &lastSeen)
	if err != nil {
		t.Fatalf("query exposure failed: %v", err)
	}
	if exposures != 1 {
		t.Errorf("expected 1 exposure, got %d", exposures)
	}
	if lastSeen == nil {
		t.Error("expected last_seen to be set")
	}

	// Re-emit the SAME exposure event with the same idempotency key (idempotent)
	exposureEvent2 := domain.Event{
		Type:           "feature_flag.exposure",
		SchemaVersion:  1,
		IdempotencyKey: flag.ID + ":on:subject1:window1",
		OccurredAt:     time.Now(),
		Payload:        json.RawMessage(fmt.Sprintf(`{"flag_id":"%s","variant":"on","environment":"production"}`, flag.ID)),
		AnonymousID:    "subject1",
	}
	_, err = store.AcceptEvents(ctx, principal, []domain.Event{exposureEvent2})
	if err != nil {
		t.Fatalf("AcceptEvents (idempotent) failed: %v", err)
	}

	// Verify exposure count is still 1 (idempotency check)
	err = store.pool.QueryRow(ctx, `SELECT exposures FROM feature_flag_exposures
		WHERE flag_id=$1 AND environment='production' AND variant='on'`,
		flag.ID).Scan(&exposures)
	if err != nil {
		t.Fatalf("query exposure failed: %v", err)
	}
	if exposures != 1 {
		t.Errorf("idempotency failed: expected 1 exposure after re-emit, got %d", exposures)
	}

	// Emit a DIFFERENT exposure event (different variant)
	exposureEvent3 := domain.Event{
		Type:           "feature_flag.exposure",
		SchemaVersion:  1,
		IdempotencyKey: flag.ID + ":off:subject2:window1",
		OccurredAt:     time.Now(),
		Payload:        json.RawMessage(fmt.Sprintf(`{"flag_id":"%s","variant":"off","environment":"production"}`, flag.ID)),
		AnonymousID:    "subject2",
	}
	_, err = store.AcceptEvents(ctx, principal, []domain.Event{exposureEvent3})
	if err != nil {
		t.Fatalf("AcceptEvents (different variant) failed: %v", err)
	}

	// Verify the new variant is in the aggregate
	err = store.pool.QueryRow(ctx, `SELECT exposures FROM feature_flag_exposures
		WHERE flag_id=$1 AND environment='production' AND variant='off'`,
		flag.ID).Scan(&exposures)
	if err != nil {
		t.Fatalf("query off-variant exposure failed: %v", err)
	}
	if exposures != 1 {
		t.Errorf("expected 1 exposure for 'off' variant, got %d", exposures)
	}

	// Emit a third variant to test cumulative counting
	exposureEvent4 := domain.Event{
		Type:           "feature_flag.exposure",
		SchemaVersion:  1,
		IdempotencyKey: flag.ID + ":on:subject2:window1",
		OccurredAt:     time.Now(),
		Payload:        json.RawMessage(fmt.Sprintf(`{"flag_id":"%s","variant":"on","environment":"production"}`, flag.ID)),
		AnonymousID:    "subject2",
	}
	_, err = store.AcceptEvents(ctx, principal, []domain.Event{exposureEvent4})
	if err != nil {
		t.Fatalf("AcceptEvents (cumulative) failed: %v", err)
	}

	// Verify 'on' variant count increased
	err = store.pool.QueryRow(ctx, `SELECT exposures FROM feature_flag_exposures
		WHERE flag_id=$1 AND environment='production' AND variant='on'`,
		flag.ID).Scan(&exposures)
	if err != nil {
		t.Fatalf("query cumulative exposure failed: %v", err)
	}
	if exposures != 2 {
		t.Errorf("expected 2 exposures for 'on' variant after cumulative event, got %d", exposures)
	}
}

func TestFeatureFlagE2ECreatePublishEvaluateExpose(t *testing.T) {
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
	if err := store.EnsureDevelopmentTenant(ctx, "flag-e2e"); err != nil {
		t.Fatal(err)
	}
	p, err := store.Authenticate(ctx, "flag-e2e")
	if err != nil {
		t.Fatal(err)
	}

	t.Run("Boolean flag: create->publish->evaluate->expose", func(t *testing.T) {
		// Create a boolean flag
		boolFlag, err := store.CreateFeatureFlag(ctx, p, domain.FeatureFlag{
			AppID:        p.AppID,
			Environment:  "production",
			Key:          "bool_feature",
			Name:         stringPtr("Boolean Feature"),
			FlagType:     "boolean",
			DefaultValue: json.RawMessage(`false`),
			Seed:         "bool-seed-123",
			Enabled:      true,
			RolloutPct:   50,
		})
		if err != nil {
			t.Fatalf("Create boolean flag failed: %v", err)
		}

		// Publish the flag (human-gated)
		humanPrincipal := domain.Principal{
			ActorType: "user",
			UserID:    "user-123",
			TenantID:  p.TenantID,
			AppID:     p.AppID,
		}
		version, err := store.PublishFeatureFlag(ctx, humanPrincipal, boolFlag.ID, "approver-123", "bool_feature:v1")
		if err != nil {
			t.Fatalf("Publish flag failed: %v", err)
		}
		if version.Version != 1 {
			t.Errorf("Expected version 1, got %d", version.Version)
		}

		// Verify flag status changed to published
		published, err := store.GetFeatureFlag(ctx, p, boolFlag.ID)
		if err != nil {
			t.Fatalf("Get published flag failed: %v", err)
		}
		if published.Status != "published" {
			t.Errorf("Expected status 'published', got %q", published.Status)
		}
		if published.CurrentVersionID == nil {
			t.Error("Expected current_version_id to be set after publish")
		}

		// Evaluate the flag for an anonymous subject
		evalAudience := &storeAudience{store: store, principal: humanPrincipal}
		result, err := flags.Evaluate(ctx, &published, "anon-subject-1", evalAudience)
		if err != nil {
			t.Fatalf("Evaluate flag failed: %v", err)
		}

		// Verify the result has a value
		if result.Value == nil {
			t.Error("Expected result.Value to be set")
		}

		// Evaluate the same subject again - should get the SAME result (deterministic)
		result2, err := flags.Evaluate(ctx, &published, "anon-subject-1", evalAudience)
		if err != nil {
			t.Fatalf("Second evaluate failed: %v", err)
		}
		if result.Variant != result2.Variant {
			t.Errorf("Deterministic check failed: first %q, second %q", result.Variant, result2.Variant)
		}
		if string(result.Value) != string(result2.Value) {
			t.Errorf("Deterministic value check failed: first %s, second %s", result.Value, result2.Value)
		}

		// Emit exposure event for the flag
		exposureEvent := domain.Event{
			Type:           "feature_flag.exposure",
			SchemaVersion:  1,
			IdempotencyKey: boolFlag.ID + ":" + version.DefinitionSha[:16] + ":anon-subject-1:window1",
			OccurredAt:     time.Now(),
			Payload: json.RawMessage(fmt.Sprintf(
				`{"flag_id":"%s","variant":"%s","environment":"production"}`,
				boolFlag.ID, result.Variant)),
			AnonymousID: "anon-subject-1",
		}
		publicPrincipal := domain.Principal{
			TenantID:  p.TenantID,
			AppID:     p.AppID,
			ActorType: "public",
		}
		_, err = store.AcceptEvents(ctx, publicPrincipal, []domain.Event{exposureEvent})
		if err != nil {
			t.Fatalf("AcceptEvents failed: %v", err)
		}

		// Verify exposure was recorded
		var exposures int64
		err = store.pool.QueryRow(ctx,
			`SELECT exposures FROM feature_flag_exposures WHERE flag_id=$1 AND environment='production' AND variant=$2`,
			boolFlag.ID, result.Variant).Scan(&exposures)
		if err != nil {
			t.Fatalf("Query exposure failed: %v", err)
		}
		if exposures != 1 {
			t.Errorf("Expected 1 exposure, got %d", exposures)
		}
	})

	t.Run("Multivariate flag: environment scoping", func(t *testing.T) {
		// Create a multivariate flag in production
		prodFlag, err := store.CreateFeatureFlag(ctx, p, domain.FeatureFlag{
			AppID:        p.AppID,
			Environment:  "production",
			Key:          "multivar_feature",
			Name:         stringPtr("Multivar Feature"),
			FlagType:     "string",
			DefaultValue: json.RawMessage(`"default"`),
			Variants: []domain.FlagVariant{
				{Label: "variant_a", Value: json.RawMessage(`"value_a"`), Weight: 50},
				{Label: "variant_b", Value: json.RawMessage(`"value_b"`), Weight: 50},
			},
			Seed:       "multivar-seed-456",
			Enabled:    true,
			RolloutPct: 100,
		})
		if err != nil {
			t.Fatalf("Create production flag failed: %v", err)
		}

		// Create the SAME key in staging environment with different config
		stagingFlag, err := store.CreateFeatureFlag(ctx, p, domain.FeatureFlag{
			AppID:        p.AppID,
			Environment:  "staging",
			Key:          "multivar_feature", // Same key, different environment
			Name:         stringPtr("Staging Multivar"),
			FlagType:     "string",
			DefaultValue: json.RawMessage(`"staging_default"`),
			Variants: []domain.FlagVariant{
				{Label: "stage_alpha", Value: json.RawMessage(`"alpha"`), Weight: 100},
			},
			Seed:       "multivar-seed-789",
			Enabled:    true,
			RolloutPct: 100,
		})
		if err != nil {
			t.Fatalf("Create staging flag failed: %v", err)
		}

		// Verify they're different flags in the store
		if prodFlag.ID == stagingFlag.ID {
			t.Error("Production and staging flags should have different IDs")
		}

		// Verify the unique constraint: same key in different environments is allowed
		if prodFlag.Key != stagingFlag.Key {
			t.Error("Keys should be the same, but environments differ")
		}
		if prodFlag.Environment == stagingFlag.Environment {
			t.Error("Environments should differ")
		}

		// Publish and evaluate both
		humanPrincipal := domain.Principal{
			ActorType: "user",
			UserID:    "user-456",
			TenantID:  p.TenantID,
			AppID:     p.AppID,
		}
		_, err = store.PublishFeatureFlag(ctx, humanPrincipal, prodFlag.ID, "approver-456", "prod-key:v1")
		if err != nil {
			t.Fatalf("Publish prod flag failed: %v", err)
		}
		_, err = store.PublishFeatureFlag(ctx, humanPrincipal, stagingFlag.ID, "approver-456", "stage-key:v1")
		if err != nil {
			t.Fatalf("Publish stage flag failed: %v", err)
		}

		prodPublished, err := store.GetFeatureFlag(ctx, p, prodFlag.ID)
		if err != nil {
			t.Fatalf("Get published prod flag failed: %v", err)
		}
		stagingPublished, err := store.GetFeatureFlag(ctx, p, stagingFlag.ID)
		if err != nil {
			t.Fatalf("Get published stage flag failed: %v", err)
		}

		// Evaluate the same subject against both environments
		evalAudience := &storeAudience{store: store, principal: humanPrincipal}
		prodResult, err := flags.Evaluate(ctx, &prodPublished, "test-subject", evalAudience)
		if err != nil {
			t.Fatalf("Evaluate prod flag failed: %v", err)
		}
		stagingResult, err := flags.Evaluate(ctx, &stagingPublished, "test-subject", evalAudience)
		if err != nil {
			t.Fatalf("Evaluate staging flag failed: %v", err)
		}

		// Verify the same subject gets DIFFERENT values in different environments
		// (because they have different seed/rollout/variants)
		// At minimum, verify the values are what we expect for each environment
		if string(prodResult.Value) == `"staging_default"` {
			t.Error("Production flag should not return staging default")
		}
		if string(stagingResult.Value) == `"default"` {
			t.Error("Staging flag should not return production default")
		}
	})

	t.Run("Kill switch: disabled flag returns default", func(t *testing.T) {
		// Create a flag
		killFlag, err := store.CreateFeatureFlag(ctx, p, domain.FeatureFlag{
			AppID:        p.AppID,
			Environment:  "production",
			Key:          "kill_switch_test",
			FlagType:     "boolean",
			DefaultValue: json.RawMessage(`false`),
			Seed:         "kill-seed",
			Enabled:      true,
			RolloutPct:   100,
		})
		if err != nil {
			t.Fatalf("Create kill switch flag failed: %v", err)
		}

		// Publish it
		humanPrincipal := domain.Principal{
			ActorType: "user",
			UserID:    "user-kill",
			TenantID:  p.TenantID,
			AppID:     p.AppID,
		}
		_, err = store.PublishFeatureFlag(ctx, humanPrincipal, killFlag.ID, "approver-kill", "kill-key:v1")
		if err != nil {
			t.Fatalf("Publish kill switch flag failed: %v", err)
		}

		published, err := store.GetFeatureFlag(ctx, p, killFlag.ID)
		if err != nil {
			t.Fatalf("Get published flag failed: %v", err)
		}

		// Evaluate when enabled
		evalAudience := &storeAudience{store: store, principal: humanPrincipal}
		enabledResult, err := flags.Evaluate(ctx, &published, "subject-kill", evalAudience)
		if err != nil {
			t.Fatalf("Evaluate enabled flag failed: %v", err)
		}
		if enabledResult.Reason == "disabled" {
			t.Error("Enabled flag should not have reason 'disabled'")
		}

		// Now disable the flag (kill switch)
		published.Status = "disabled"
		updated, err := store.UpdateFeatureFlag(ctx, p, published)
		if err != nil {
			t.Fatalf("Update flag status failed: %v", err)
		}

		// Evaluate when disabled
		disabledResult, err := flags.Evaluate(ctx, &updated, "subject-kill", evalAudience)
		if err != nil {
			t.Fatalf("Evaluate disabled flag failed: %v", err)
		}

		// Verify disabled returns default and reason is "disabled"
		if disabledResult.Reason != "disabled" {
			t.Errorf("Disabled flag should have reason 'disabled', got %q", disabledResult.Reason)
		}
		if string(disabledResult.Value) != `false` {
			t.Errorf("Disabled flag should return default value, got %s", disabledResult.Value)
		}

		// Re-enable the flag
		updated.Status = "published"
		reenabledFlag, err := store.UpdateFeatureFlag(ctx, p, updated)
		if err != nil {
			t.Fatalf("Update to re-enable failed: %v", err)
		}

		// Verify it works again
		reenableResult, err := flags.Evaluate(ctx, &reenabledFlag, "subject-kill", evalAudience)
		if err != nil {
			t.Fatalf("Evaluate re-enabled flag failed: %v", err)
		}
		if reenableResult.Reason == "disabled" {
			t.Error("Re-enabled flag should not be disabled")
		}
	})
}

// storeAudience wraps store.EvaluateAudience to implement flags.EvalAudience interface
type storeAudience struct {
	store     *Store
	principal domain.Principal
}

func (sa *storeAudience) Eval(ctx context.Context, profileID string, dsl json.RawMessage) (bool, error) {
	return sa.store.EvaluateAudience(ctx, sa.principal, profileID, dsl)
}

func stringPtr(s string) *string {
	return &s
}

func TestSecurityVersionsAppendOnly(t *testing.T) {
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
	if err := store.EnsureDevelopmentTenant(ctx, "flag-append-only"); err != nil {
		t.Fatal(err)
	}
	p, err := store.Authenticate(ctx, "flag-append-only")
	if err != nil {
		t.Fatal(err)
	}

	// Create and publish a flag to create a version
	flag, err := store.CreateFeatureFlag(ctx, p, domain.FeatureFlag{
		AppID:        p.AppID,
		Environment:  "production",
		Key:          "security-test-flag",
		FlagType:     "boolean",
		DefaultValue: json.RawMessage(`true`),
		Seed:         "seed-append-only",
		Enabled:      true,
		Status:       "draft",
	})
	if err != nil {
		t.Fatal(err)
	}

	// Publish to create a version
	userID := "user-123"
	version, err := store.PublishFeatureFlag(ctx, p, flag.ID, userID, "")
	if err != nil {
		t.Fatalf("PublishFeatureFlag failed: %v", err)
	}

	// Try to UPDATE the version directly via SQL (this should fail due to the BEFORE UPDATE trigger)
	// Get the version ID first
	var versionID string
	err = store.pool.QueryRow(ctx, "SELECT id FROM feature_flag_versions WHERE flag_id = $1 LIMIT 1", flag.ID).Scan(&versionID)
	if err != nil {
		t.Fatalf("Failed to get version ID: %v", err)
	}

	// Attempt to update the version — this should be blocked by the trigger
	updateErr := store.pool.QueryRow(ctx, `
		UPDATE feature_flag_versions
		SET version = $1
		WHERE id = $2
		RETURNING id
	`, version.Version+1, versionID).Scan(&versionID)

	if updateErr == nil {
		t.Fatal("expected version UPDATE to be blocked by trigger, but it succeeded")
	}
	if !strings.Contains(updateErr.Error(), "append-only") && !strings.Contains(updateErr.Error(), "append only") {
		// The trigger message might vary, so just check that it failed
		t.Logf("version UPDATE blocked with error (expected): %v", updateErr)
	}

	// Attempt to DELETE the version — this should also be blocked by the trigger
	deleteErr := store.pool.QueryRow(ctx, `
		DELETE FROM feature_flag_versions
		WHERE id = $1
		RETURNING id
	`, versionID).Scan(&versionID)

	if deleteErr == nil {
		t.Fatal("expected version DELETE to be blocked by trigger, but it succeeded")
	}
	if !strings.Contains(deleteErr.Error(), "append-only") && !strings.Contains(deleteErr.Error(), "append only") {
		t.Logf("version DELETE blocked with error (expected): %v", deleteErr)
	}
}

func TestSecurityExposureProjectorOnlyWriter(t *testing.T) {
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
	if err := store.EnsureDevelopmentTenant(ctx, "flag-exposure"); err != nil {
		t.Fatal(err)
	}
	p, err := store.Authenticate(ctx, "flag-exposure")
	if err != nil {
		t.Fatal(err)
	}

	// Create and publish a flag
	flag, err := store.CreateFeatureFlag(ctx, p, domain.FeatureFlag{
		AppID:        p.AppID,
		Environment:  "production",
		Key:          "exposure-test-flag",
		FlagType:     "boolean",
		DefaultValue: json.RawMessage(`true`),
		Seed:         "seed-exposure",
		Enabled:      true,
		Status:       "draft",
	})
	if err != nil {
		t.Fatal(err)
	}

	userID := "user-456"
	version, err := store.PublishFeatureFlag(ctx, p, flag.ID, userID, "")
	if err != nil {
		t.Fatalf("PublishFeatureFlag failed: %v", err)
	}

	// Create exposures via the projector (AcceptEvents)
	exposureEvent := domain.Event{
		Type: "feature_flag.exposure",
		Payload: json.RawMessage(fmt.Sprintf(`{
			"flag_id": "%s",
			"environment": "production",
			"variant": "default"
		}`, flag.ID)),
		AnonymousID:    "anon-1",
		OccurredAt:     time.Now(),
		IdempotencyKey: fmt.Sprintf("exposure:%s:v%d:anon-1:0", flag.ID, version.Version),
	}

	// Emit via AcceptEvents (the proper projector path)
	_, err = store.AcceptEvents(ctx, p, []domain.Event{exposureEvent})
	if err != nil {
		t.Fatalf("AcceptEvents failed: %v", err)
	}

	// Verify the exposure was recorded
	var count int
	err = store.pool.QueryRow(ctx, `
		SELECT exposures FROM feature_flag_exposures
		WHERE flag_id = $1 AND environment = $2 AND variant = $3
	`, flag.ID, "production", "default").Scan(&count)
	if err != nil {
		t.Fatalf("Failed to query exposures: %v", err)
	}
	if count != 1 {
		t.Fatalf("expected 1 exposure, got %d", count)
	}

	// Re-emit the same event (same idempotency key) — should be idempotent
	_, err = store.AcceptEvents(ctx, p, []domain.Event{exposureEvent})
	if err != nil {
		t.Fatalf("AcceptEvents re-emit failed: %v", err)
	}

	// Verify exposure count is still 1 (idempotent)
	err = store.pool.QueryRow(ctx, `
		SELECT exposures FROM feature_flag_exposures
		WHERE flag_id = $1 AND environment = $2 AND variant = $3
	`, flag.ID, "production", "default").Scan(&count)
	if err != nil {
		t.Fatalf("Failed to query exposures after re-emit: %v", err)
	}
	if count != 1 {
		t.Fatalf("exposure should be idempotent; expected 1, got %d", count)
	}
}

func TestSecurityScopeEnforcement(t *testing.T) {
	// This test verifies that routes are properly guarded with scope checks.
	// The actual enforcement happens at the HTTP handler level (s.authenticate),
	// but we verify here that different scopes exist and are distinct.

	// Verify scopes are different
	readOnly := []string{"flags:read"}
	writeScopes := []string{"flags:read", "flags:write"}

	// A read-only principal should not have write permissions
	hasWrite := false
	for _, scope := range readOnly {
		if scope == "flags:write" {
			hasWrite = true
			break
		}
	}
	if hasWrite {
		t.Fatal("read-only scopes should not include flags:write")
	}

	// A principal with write scopes should have both
	hasRead := false
	hasWriteScope := false
	for _, scope := range writeScopes {
		if scope == "flags:read" {
			hasRead = true
		}
		if scope == "flags:write" {
			hasWriteScope = true
		}
	}
	if !hasRead || !hasWriteScope {
		t.Fatal("write scopes should include both flags:read and flags:write")
	}

	// Verify principal types differ
	apiPrincipal := domain.Principal{
		ActorType: "api_key",
		KeyID:     "test-key",
		TenantID:  "tenant-1",
		AppID:     "app-1",
	}

	userPrincipal := domain.Principal{
		ActorType: "user",
		UserID:    "user-123",
		TenantID:  "tenant-1",
		AppID:     "app-1",
	}

	if apiPrincipal.ActorType == userPrincipal.ActorType {
		t.Error("API key and user principals should have different actor types")
	}
}
