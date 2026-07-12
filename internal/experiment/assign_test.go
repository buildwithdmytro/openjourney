package experiment

import "testing"

func TestAssignStableAcrossRepeatedCallsAndRestartEquivalent(t *testing.T) {
	variants := []Variant{{Label: "control", Weight: 50}, {Label: "treatment", Weight: 50}}
	wantVariant, wantHoldout := Assign("immutable-seed", "profile-42", variants, 10)
	for i := 0; i < 10_000; i++ {
		gotVariant, gotHoldout := Assign("immutable-seed", "profile-42", variants, 10)
		if gotVariant != wantVariant || gotHoldout != wantHoldout {
			t.Fatalf("call %d changed assignment: got (%q, %t), want (%q, %t)", i, gotVariant, gotHoldout, wantVariant, wantHoldout)
		}
	}

	// Assign has no process-local state: invoking it again from only persisted inputs
	// models reconstructing the assignment after a process restart.
	restartedVariant, restartedHoldout := Assign(string([]byte("immutable-seed")), string([]byte("profile-42")), append([]Variant(nil), variants...), 10)
	if restartedVariant != wantVariant || restartedHoldout != wantHoldout {
		t.Fatalf("restart-equivalent assignment changed: got (%q, %t), want (%q, %t)", restartedVariant, restartedHoldout, wantVariant, wantHoldout)
	}
}

func TestAssignHoldoutAndWeightedDistribution(t *testing.T) {
	variants := []Variant{{Label: "control", Weight: 20}, {Label: "a", Weight: 30}, {Label: "b", Weight: 50}}
	const population = 100_000
	counts := map[string]int{}
	for i := 0; i < population; i++ {
		variant, holdout := Assign("distribution-seed", profileID(i), variants, 10)
		if holdout && variant != "holdout" {
			t.Fatalf("holdout assignment returned variant %q", variant)
		}
		counts[variant]++
	}

	assertNearPct(t, counts["holdout"], population, 10, 0.6)
	assertNearPct(t, counts["control"], population, 18, 0.6)
	assertNearPct(t, counts["a"], population, 27, 0.6)
	assertNearPct(t, counts["b"], population, 45, 0.6)
}

func profileID(i int) string {
	const digits = "0123456789"
	if i == 0 {
		return "profile-0"
	}
	buf := make([]byte, 0, 16)
	for ; i > 0; i /= 10 {
		buf = append(buf, digits[i%10])
	}
	for left, right := 0, len(buf)-1; left < right; left, right = left+1, right-1 {
		buf[left], buf[right] = buf[right], buf[left]
	}
	return "profile-" + string(buf)
}

func assertNearPct(t *testing.T, count, total int, want, tolerance float64) {
	t.Helper()
	got := float64(count) * 100 / float64(total)
	if got < want-tolerance || got > want+tolerance {
		t.Fatalf("distribution %.3f%% outside %.1f%% ± %.1f%%", got, want, tolerance)
	}
}
