package experiment

import (
	"math"
	"testing"
)

func TestCompareProportions_TextbookExamples(t *testing.T) {
	// Example 1:
	// Control: n=100, x=10 (10% rate)
	// Variant: n=100, x=20 (20% rate)
	stats1 := CompareProportions(100, 10, 100, 20)

	if math.Abs(stats1.Rate-0.20) > 1e-9 {
		t.Errorf("Example 1 Rate: got %f, want 0.20", stats1.Rate)
	}
	if math.Abs(stats1.Uplift-1.0) > 1e-9 {
		t.Errorf("Example 1 Uplift: got %f, want 1.0", stats1.Uplift)
	}
	if math.Abs(stats1.ZScore-1.980295) > 1e-5 {
		t.Errorf("Example 1 ZScore: got %f, want ~1.980295", stats1.ZScore)
	}
	if math.Abs(stats1.PValue-0.047670) > 1e-5 {
		t.Errorf("Example 1 PValue: got %f, want ~0.047670", stats1.PValue)
	}
	if math.Abs(stats1.CILow-0.002002) > 1e-5 {
		t.Errorf("Example 1 CILow: got %f, want ~0.002002", stats1.CILow)
	}
	if math.Abs(stats1.CIHigh-0.197998) > 1e-5 {
		t.Errorf("Example 1 CIHigh: got %f, want ~0.197998", stats1.CIHigh)
	}

	// Example 2:
	// Control: n=500, x=100 (20% rate)
	// Variant: n=400, x=90 (22.5% rate)
	stats2 := CompareProportions(500, 100, 400, 90)

	if math.Abs(stats2.Rate-0.225) > 1e-9 {
		t.Errorf("Example 2 Rate: got %f, want 0.225", stats2.Rate)
	}
	if math.Abs(stats2.Uplift-0.125) > 1e-9 {
		t.Errorf("Example 2 Uplift: got %f, want 0.125", stats2.Uplift)
	}
	if math.Abs(stats2.ZScore-0.913214) > 1e-5 {
		t.Errorf("Example 2 ZScore: got %f, want ~0.913214", stats2.ZScore)
	}
	if math.Abs(stats2.PValue-0.361129) > 1e-5 {
		t.Errorf("Example 2 PValue: got %f, want ~0.361129", stats2.PValue)
	}
	if math.Abs(stats2.CILow-(-0.028888)) > 1e-5 {
		t.Errorf("Example 2 CILow: got %f, want ~-0.028888", stats2.CILow)
	}
	if math.Abs(stats2.CIHigh-0.078888) > 1e-5 {
		t.Errorf("Example 2 CIHigh: got %f, want ~0.078888", stats2.CIHigh)
	}
}

func TestCompareProportions_EdgeCases(t *testing.T) {
	// Case A: Zero sent in variant
	stats := CompareProportions(100, 10, 0, 0)
	assertFinite(t, stats)
	if stats.Rate != 0 || stats.Uplift != 0 || stats.ZScore != 0 || stats.PValue != 1.0 || stats.CILow != 0 || stats.CIHigh != 0 {
		t.Errorf("Zero variant sends should return safe defaults: %+v", stats)
	}

	// Case B: Zero sent in control
	stats = CompareProportions(0, 0, 100, 10)
	assertFinite(t, stats)
	if stats.Rate != 0.1 || stats.Uplift != 0 || stats.ZScore != 0 || stats.PValue != 1.0 || stats.CILow != 0 || stats.CIHigh != 0 {
		t.Errorf("Zero control sends should return safe defaults: %+v", stats)
	}

	// Case C: Identical proportions, 0 conversions
	stats = CompareProportions(100, 0, 100, 0)
	assertFinite(t, stats)
	if stats.Rate != 0 || stats.Uplift != 0 || stats.ZScore != 0 || stats.PValue != 1.0 || stats.CILow != 0 || stats.CIHigh != 0 {
		t.Errorf("Identical zero conversion rates should return safe defaults: %+v", stats)
	}

	// Case D: Identical proportions, 100% conversions
	stats = CompareProportions(100, 100, 100, 100)
	assertFinite(t, stats)
	if stats.Rate != 1.0 || stats.Uplift != 0 || stats.ZScore != 0 || stats.PValue != 1.0 || stats.CILow != 0 || stats.CIHigh != 0 {
		t.Errorf("Identical 100%% conversion rates should return safe defaults: %+v", stats)
	}

	// Case E: Negative inputs
	stats = CompareProportions(-10, -5, -20, -2)
	assertFinite(t, stats)
	if stats.Rate != 0 || stats.Uplift != 0 || stats.ZScore != 0 || stats.PValue != 1.0 || stats.CILow != 0 || stats.CIHigh != 0 {
		t.Errorf("Negative inputs should return safe defaults: %+v", stats)
	}
}

func assertFinite(t *testing.T, stats VariantStats) {
	t.Helper()
	if math.IsNaN(stats.Rate) || math.IsInf(stats.Rate, 0) {
		t.Errorf("Rate is not finite: %f", stats.Rate)
	}
	if math.IsNaN(stats.Uplift) || math.IsInf(stats.Uplift, 0) {
		t.Errorf("Uplift is not finite: %f", stats.Uplift)
	}
	if math.IsNaN(stats.ZScore) || math.IsInf(stats.ZScore, 0) {
		t.Errorf("ZScore is not finite: %f", stats.ZScore)
	}
	if math.IsNaN(stats.PValue) || math.IsInf(stats.PValue, 0) {
		t.Errorf("PValue is not finite: %f", stats.PValue)
	}
	if math.IsNaN(stats.CILow) || math.IsInf(stats.CILow, 0) {
		t.Errorf("CILow is not finite: %f", stats.CILow)
	}
	if math.IsNaN(stats.CIHigh) || math.IsInf(stats.CIHigh, 0) {
		t.Errorf("CIHigh is not finite: %f", stats.CIHigh)
	}
}
