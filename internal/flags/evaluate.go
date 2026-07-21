package flags

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/buildwithdmytro/openjourney/internal/domain"
	"github.com/buildwithdmytro/openjourney/internal/experiment"
)

// EvaluationResult is the outcome of evaluating a flag for a subject.
type EvaluationResult struct {
	Variant string          `json:"variant"`
	Value   json.RawMessage `json:"value"`
	Reason  string          `json:"reason"` // "disabled", "targeted", "rollout", "rollout_excluded"
}

// EvalAudience is an interface for testing evaluation without a database.
type EvalAudience interface {
	Eval(ctx context.Context, profileID string, dsl json.RawMessage) (bool, error)
}

// Evaluate determines the value of a flag for a subject, deterministically.
// If status is 'disabled' or not enabled, returns the default.
// Walks targeting_rules in order; first match returns its variant's value.
// If no rule matches, applies rollout bucketing: if bucket < rollout_pct*100,
// assigns a weighted variant via Assign; else returns default.
// Pure function: no randomness, no wall-clock.
func Evaluate(ctx context.Context, flag *domain.FeatureFlag, profileID string, evalAudience EvalAudience) (*EvaluationResult, error) {
	// Kill switch or disabled flag returns default.
	if flag.Status == "disabled" || !flag.Enabled {
		return &EvaluationResult{
			Variant: "",
			Value:   flag.DefaultValue,
			Reason:  "disabled",
		}, nil
	}

	// Walk targeting rules in order; first match wins.
	for _, rule := range flag.TargetingRules {
		matches, err := evalAudience.Eval(ctx, profileID, rule.DSL)
		if err != nil {
			return nil, fmt.Errorf("evaluate audience rule: %w", err)
		}
		if matches {
			// Find the variant by label and return its value.
			for _, variant := range flag.Variants {
				if variant.Label == rule.Variant {
					return &EvaluationResult{
						Variant: rule.Variant,
						Value:   variant.Value,
						Reason:  "targeted",
					}, nil
				}
			}
			// Rule matched but variant label not found; fall through.
			return nil, fmt.Errorf("targeting rule variant %q not found in flag variants", rule.Variant)
		}
	}

	// No targeting rule matched; apply rollout.
	bucket := experiment.BucketOf(flag.Seed+":"+profileID, 10000)
	rolloutThreshold := uint64(flag.RolloutPct * 100)

	if bucket < rolloutThreshold {
		// Subject is in the rollout; assign a weighted variant.
		expVariants := make([]experiment.Variant, len(flag.Variants))
		for i, v := range flag.Variants {
			expVariants[i] = experiment.Variant{
				Label:  v.Label,
				Weight: v.Weight,
			}
		}
		variantLabel, _ := experiment.Assign(flag.Seed, profileID, expVariants, 0)
		if variantLabel == "" || variantLabel == "holdout" {
			// No variants or all have zero weight; return default.
			return &EvaluationResult{
				Variant: "",
				Value:   flag.DefaultValue,
				Reason:  "rollout_excluded",
			}, nil
		}
		// Find the variant by label.
		for _, variant := range flag.Variants {
			if variant.Label == variantLabel {
				return &EvaluationResult{
					Variant: variantLabel,
					Value:   variant.Value,
					Reason:  "rollout",
				}, nil
			}
		}
		// Variant not found; return default.
		return &EvaluationResult{
			Variant: "",
			Value:   flag.DefaultValue,
			Reason:  "rollout_excluded",
		}, nil
	}

	// Subject is outside the rollout.
	return &EvaluationResult{
		Variant: "",
		Value:   flag.DefaultValue,
		Reason:  "rollout_excluded",
	}, nil
}
