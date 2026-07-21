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
