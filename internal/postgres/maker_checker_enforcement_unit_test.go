package postgres

import "testing"

func TestMakerCheckerEnforcementRejectsSelfApproval_NonGated(t *testing.T) {
	for _, approver := range []string{"creator-1", "editor-2"} {
		if err := EvaluateMakerChecker(true, approver, []string{"creator-1", "editor-2"}); err != ErrSelfApproval {
			t.Fatalf("expected real maker-checker enforcement to reject %s, got %v", approver, err)
		}
	}
	if err := EvaluateMakerChecker(true, "approver-3", []string{"creator-1", "editor-2"}); err != nil {
		t.Fatalf("expected distinct approver to pass real maker-checker enforcement, got %v", err)
	}
}
