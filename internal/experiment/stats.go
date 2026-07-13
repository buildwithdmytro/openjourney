// Package experiment contains deterministic experiment assignment and statistical analysis primitives.
package experiment

import (
	"math"
)

// VariantStats contains the results of a two-proportion z-test comparing a variant to a control.
type VariantStats struct {
	Rate   float64 `json:"rate"`    // Conversion rate of the variant (conversions / sent)
	Uplift float64 `json:"uplift"`  // Relative uplift vs control: (p_v - p_c) / p_c
	ZScore float64 `json:"z_score"` // Calculated z-statistic
	PValue float64 `json:"p_value"` // Two-tailed p-value
	CILow  float64 `json:"ci_low"`  // Lower bound of 95% confidence interval for the difference (p_v - p_c)
	CIHigh float64 `json:"ci_high"` // Upper bound of 95% confidence interval for the difference (p_v - p_c)
}

// CompareProportions performs a two-proportion z-test comparing a variant group to a control group.
// sentControl: total subjects in the control group
// convControl: total conversions in the control group
// sentVariant: total subjects in the variant group
// convVariant: total conversions in the variant group
func CompareProportions(sentControl, convControl, sentVariant, convVariant int64) VariantStats {
	stats := VariantStats{
		PValue: 1.0,
	}

	if sentVariant > 0 {
		stats.Rate = float64(convVariant) / float64(sentVariant)
	}

	// If control or variant has zero or negative subjects, we cannot perform statistical comparison.
	if sentControl <= 0 || sentVariant <= 0 {
		return stats
	}

	pControl := float64(convControl) / float64(sentControl)
	pVariant := stats.Rate

	// Relative uplift: (pVariant - pControl) / pControl
	if pControl > 0 {
		stats.Uplift = (pVariant - pControl) / pControl
	}

	// Pooled proportion for z-test
	pooledP := float64(convControl+convVariant) / float64(sentControl+sentVariant)

	// Standard error of difference for z-score using pooled proportion
	sePooled := math.Sqrt(pooledP * (1.0 - pooledP) * (1.0/float64(sentControl) + 1.0/float64(sentVariant)))

	if sePooled > 0 {
		stats.ZScore = (pVariant - pControl) / sePooled
		// Two-tailed p-value using complementary error function (erfc)
		stats.PValue = math.Erfc(math.Abs(stats.ZScore) / math.Sqrt(2.0))
	} else {
		stats.ZScore = 0.0
		stats.PValue = 1.0
	}

	// Standard error of difference for Confidence Interval (unpooled)
	seDiff := math.Sqrt((pControl*(1.0-pControl))/float64(sentControl) + (pVariant*(1.0-pVariant))/float64(sentVariant))

	// Critical value for 95% confidence (two-tailed)
	const z95 = 1.959963984540054

	diff := pVariant - pControl
	stats.CILow = diff - z95*seDiff
	stats.CIHigh = diff + z95*seDiff

	return stats
}
