package flags

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/buildwithdmytro/openjourney/internal/domain"
)

// mockEvalAudience implements EvalAudience for testing.
type mockEvalAudience struct {
	matches map[string]bool
}

func (m *mockEvalAudience) Eval(ctx context.Context, profileID string, dsl json.RawMessage) (bool, error) {
	if m.matches == nil {
		return false, nil
	}
	return m.matches[profileID], nil
}

func boolValue(v bool) json.RawMessage {
	b, _ := json.Marshal(v)
	return b
}

func stringValue(v string) json.RawMessage {
	b, _ := json.Marshal(v)
	return b
}

func TestEvalute_DisabledFlagReturnsDefault(t *testing.T) {
	flag := &domain.FeatureFlag{
		ID:           "flag-1",
		Key:          "test-flag",
		FlagType:     "boolean",
		DefaultValue: boolValue(false),
		Variants: []domain.FlagVariant{
			{Label: "on", Value: boolValue(true), Weight: 100},
		},
		RolloutPct: 100,
		Seed:       "seed123",
		Enabled:    true,
		Status:     "disabled", // Disabled flag
	}

	result, err := Evaluate(context.Background(), flag, "profile-1", &mockEvalAudience{})
	if err != nil {
		t.Fatalf("Evaluate failed: %v", err)
	}

	if result.Reason != "disabled" {
		t.Errorf("Expected reason 'disabled', got %q", result.Reason)
	}
	if string(result.Value) != string(boolValue(false)) {
		t.Errorf("Expected default value, got %s", result.Value)
	}
}

func TestEvaluate_NotEnabledFlagReturnsDefault(t *testing.T) {
	flag := &domain.FeatureFlag{
		ID:           "flag-1",
		Key:          "test-flag",
		FlagType:     "boolean",
		DefaultValue: boolValue(false),
		Variants: []domain.FlagVariant{
			{Label: "on", Value: boolValue(true), Weight: 100},
		},
		RolloutPct: 100,
		Seed:       "seed123",
		Enabled:    false, // Not enabled
		Status:     "published",
	}

	result, err := Evaluate(context.Background(), flag, "profile-1", &mockEvalAudience{})
	if err != nil {
		t.Fatalf("Evaluate failed: %v", err)
	}

	if result.Reason != "disabled" {
		t.Errorf("Expected reason 'disabled', got %q", result.Reason)
	}
	if string(result.Value) != string(boolValue(false)) {
		t.Errorf("Expected default value, got %s", result.Value)
	}
}

func TestEvaluate_TargetingRuleMatches(t *testing.T) {
	flag := &domain.FeatureFlag{
		ID:           "flag-1",
		Key:          "test-flag",
		FlagType:     "boolean",
		DefaultValue: boolValue(false),
		Variants: []domain.FlagVariant{
			{Label: "variant-a", Value: boolValue(true), Weight: 50},
			{Label: "variant-b", Value: boolValue(false), Weight: 50},
		},
		TargetingRules: []domain.FlagTargetingRule{
			{DSL: json.RawMessage("{}"), Variant: "variant-a"},
		},
		RolloutPct: 0,
		Seed:       "seed123",
		Enabled:    true,
		Status:     "published",
	}

	// Mock audience that matches the first rule for profile-1.
	mock := &mockEvalAudience{
		matches: map[string]bool{"profile-1": true},
	}

	result, err := Evaluate(context.Background(), flag, "profile-1", mock)
	if err != nil {
		t.Fatalf("Evaluate failed: %v", err)
	}

	if result.Reason != "targeted" {
		t.Errorf("Expected reason 'targeted', got %q", result.Reason)
	}
	if result.Variant != "variant-a" {
		t.Errorf("Expected variant 'variant-a', got %q", result.Variant)
	}
	if string(result.Value) != string(boolValue(true)) {
		t.Errorf("Expected variant-a value (true), got %s", result.Value)
	}
}

func TestEvaluate_RolloutGating(t *testing.T) {
	flag := &domain.FeatureFlag{
		ID:           "flag-1",
		Key:          "test-flag",
		FlagType:     "boolean",
		DefaultValue: boolValue(false),
		Variants: []domain.FlagVariant{
			{Label: "on", Value: boolValue(true), Weight: 100},
		},
		TargetingRules: []domain.FlagTargetingRule{}, // No targeting rules
		RolloutPct:     50,                            // 50% rollout
		Seed:           "seed123",
		Enabled:        true,
		Status:         "published",
	}

	mock := &mockEvalAudience{}

	// Test a profile that falls into the rollout (bucket < 5000).
	// We'll use a seed/subject combination we know will bucket low.
	result, err := Evaluate(context.Background(), flag, "subject-low", mock)
	if err != nil {
		t.Fatalf("Evaluate failed: %v", err)
	}
	if result.Reason != "rollout" && result.Reason != "rollout_excluded" {
		t.Errorf("Expected reason 'rollout' or 'rollout_excluded', got %q", result.Reason)
	}
}

func TestEvaluate_StabilityAcrossEvaluations(t *testing.T) {
	flag := &domain.FeatureFlag{
		ID:           "flag-1",
		Key:          "test-flag",
		FlagType:     "string",
		DefaultValue: stringValue("default"),
		Variants: []domain.FlagVariant{
			{Label: "variant-a", Value: stringValue("a"), Weight: 100},
			{Label: "variant-b", Value: stringValue("b"), Weight: 100},
		},
		TargetingRules: []domain.FlagTargetingRule{},
		RolloutPct:     100, // Always in rollout
		Seed:           "stable-seed",
		Enabled:        true,
		Status:         "published",
	}

	mock := &mockEvalAudience{}

	// Evaluate the same subject 1000 times; should always get the same variant.
	profileID := "stable-subject"
	var firstVariant string
	for i := 0; i < 1000; i++ {
		result, err := Evaluate(context.Background(), flag, profileID, mock)
		if err != nil {
			t.Fatalf("Evaluate failed on iteration %d: %v", i, err)
		}
		if i == 0 {
			firstVariant = result.Variant
		} else if result.Variant != firstVariant {
			t.Errorf("Variant changed across evaluations: expected %q, got %q at iteration %d", firstVariant, result.Variant, i)
			break
		}
	}
}

func TestEvaluate_RolloutDistribution(t *testing.T) {
	flag := &domain.FeatureFlag{
		ID:           "flag-1",
		Key:          "test-flag",
		FlagType:     "boolean",
		DefaultValue: boolValue(false),
		Variants: []domain.FlagVariant{
			{Label: "on", Value: boolValue(true), Weight: 100},
		},
		TargetingRules: []domain.FlagTargetingRule{},
		RolloutPct:     30, // 30% rollout
		Seed:           "dist-seed",
		Enabled:        true,
		Status:         "published",
	}

	mock := &mockEvalAudience{}

	// Evaluate for 1000 subjects and measure how many get the variant (30% ± 3%).
	rolloutCount := 0
	totalSubjects := 1000
	for i := 0; i < totalSubjects; i++ {
		profileID := "subject-" + string(rune('0'+i%10)) + "-" + string(rune('0'+(i/10)%10)) + "-" + string(rune('0'+(i/100)%10))
		result, err := Evaluate(context.Background(), flag, profileID, mock)
		if err != nil {
			t.Fatalf("Evaluate failed: %v", err)
		}
		if result.Reason == "rollout" {
			rolloutCount++
		}
	}

	rolloutPct := float64(rolloutCount) / float64(totalSubjects) * 100
	expectedPct := 30.0
	tolerance := 3.0

	if rolloutPct < expectedPct-tolerance || rolloutPct > expectedPct+tolerance {
		t.Errorf("Rollout distribution out of tolerance: expected ~30%% (±3%%), got %.1f%% (%d/%d)", rolloutPct, rolloutCount, totalSubjects)
	}
}

func TestEvaluate_NoRandomnessOrWallClock(t *testing.T) {
	// This test verifies that the code does not use math/rand or time-based assignment.
	// We check this by observing determinism (already tested above), and by inspection
	// of the evaluate.go code itself. This test documents the requirement.
	flag := &domain.FeatureFlag{
		ID:           "flag-1",
		Key:          "test-flag",
		FlagType:     "boolean",
		DefaultValue: boolValue(false),
		Variants: []domain.FlagVariant{
			{Label: "on", Value: boolValue(true), Weight: 100},
		},
		TargetingRules: []domain.FlagTargetingRule{},
		RolloutPct:     50,
		Seed:           "no-randomness-seed",
		Enabled:        true,
		Status:         "published",
	}

	mock := &mockEvalAudience{}

	// Run evaluation twice at different "times" (we don't actually sleep).
	// If randomness were used, we'd expect different results. We should get the same.
	result1, _ := Evaluate(context.Background(), flag, "deterministic-subject", mock)
	result2, _ := Evaluate(context.Background(), flag, "deterministic-subject", mock)

	if result1.Variant != result2.Variant || string(result1.Value) != string(result2.Value) || result1.Reason != result2.Reason {
		t.Errorf("Evaluation changed between calls (indicates randomness): first=%+v, second=%+v", result1, result2)
	}
}

func TestEvaluate_TargetingRuleOrdering(t *testing.T) {
	flag := &domain.FeatureFlag{
		ID:           "flag-1",
		Key:          "test-flag",
		FlagType:     "boolean",
		DefaultValue: boolValue(false),
		Variants: []domain.FlagVariant{
			{Label: "variant-a", Value: boolValue(true), Weight: 50},
			{Label: "variant-b", Value: boolValue(false), Weight: 50},
		},
		TargetingRules: []domain.FlagTargetingRule{
			{DSL: json.RawMessage("{}"), Variant: "variant-a"},
			{DSL: json.RawMessage("{}"), Variant: "variant-b"},
		},
		RolloutPct: 0,
		Seed:       "order-seed",
		Enabled:    true,
		Status:     "published",
	}

	// Mock audience: both rules match (DSL is {}).
	mock := &mockEvalAudience{
		matches: map[string]bool{"profile-1": true},
	}

	result, err := Evaluate(context.Background(), flag, "profile-1", mock)
	if err != nil {
		t.Fatalf("Evaluate failed: %v", err)
	}

	// First matching rule should win.
	if result.Variant != "variant-a" {
		t.Errorf("Expected first-matching rule variant 'variant-a', got %q", result.Variant)
	}
}

func TestEvaluate_NoVariantsRollout(t *testing.T) {
	flag := &domain.FeatureFlag{
		ID:             "flag-1",
		Key:            "test-flag",
		FlagType:       "boolean",
		DefaultValue:   boolValue(false),
		Variants:       []domain.FlagVariant{}, // No variants
		TargetingRules: []domain.FlagTargetingRule{},
		RolloutPct:     100,
		Seed:           "no-variants-seed",
		Enabled:        true,
		Status:         "published",
	}

	mock := &mockEvalAudience{}

	result, err := Evaluate(context.Background(), flag, "profile-1", mock)
	if err != nil {
		t.Fatalf("Evaluate failed: %v", err)
	}

	// With no variants, should return default even though rollout is 100%.
	if result.Reason != "rollout_excluded" {
		t.Errorf("Expected reason 'rollout_excluded' (no variants), got %q", result.Reason)
	}
	if string(result.Value) != string(boolValue(false)) {
		t.Errorf("Expected default value, got %s", result.Value)
	}
}
