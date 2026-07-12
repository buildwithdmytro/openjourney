// Package experiment contains deterministic experiment assignment primitives.
package experiment

import (
	"crypto/sha256"
	"encoding/binary"
)

// Variant is a weighted experiment branch.
type Variant struct {
	Label  string
	Weight int
}

// BucketOf maps a key into [0, mod) using a stable SHA-256 digest.
func BucketOf(key string, mod uint64) uint64 {
	if mod == 0 {
		return 0
	}
	hash := sha256.Sum256([]byte(key))
	return binary.BigEndian.Uint64(hash[:8]) % mod
}

// Assign deterministically assigns profileID to holdout or a weighted variant.
// The caller must keep seed immutable for the lifetime of a running experiment.
func Assign(seed, profileID string, variants []Variant, holdoutPct int) (variant string, holdout bool) {
	if holdoutPct < 0 {
		holdoutPct = 0
	} else if holdoutPct > 100 {
		holdoutPct = 100
	}
	bucket := BucketOf(seed+":"+profileID, 10_000)
	holdoutBuckets := uint64(holdoutPct * 100)
	if bucket < holdoutBuckets {
		return "holdout", true
	}

	var totalWeight uint64
	for _, candidate := range variants {
		if candidate.Weight > 0 {
			totalWeight += uint64(candidate.Weight)
		}
	}
	remainingBuckets := uint64(10_000) - holdoutBuckets
	if totalWeight == 0 || remainingBuckets == 0 {
		return "", false
	}
	weightedBucket := (bucket - holdoutBuckets) * totalWeight / remainingBuckets
	var cumulative uint64
	for _, candidate := range variants {
		if candidate.Weight > 0 {
			cumulative += uint64(candidate.Weight)
		}
		if weightedBucket < cumulative {
			return candidate.Label, false
		}
	}
	return variants[len(variants)-1].Label, false
}
