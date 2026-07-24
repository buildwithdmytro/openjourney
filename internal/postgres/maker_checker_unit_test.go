package postgres

import (
	"testing"
)

// TestMakerCheckerFailClosedAndMultiActor_NonGated is a non-gated unit test that asserts
// fail-closed evaluation for unknown creators and multi-actor self-approval blocking for co-authors (F10).
func TestMakerCheckerFailClosedAndMultiActor_NonGated(t *testing.T) {
	t.Run("Unknown creator is DENIED when policy requires checker (fail-closed)", func(t *testing.T) {
		err := EvaluateMakerChecker(true, "user-approver", nil)
		if err != ErrSelfApproval {
			t.Fatalf("expected ErrSelfApproval for unknown creator when checker required, got %v", err)
		}

		errEmpty := EvaluateMakerChecker(true, "user-approver", []string{})
		if errEmpty != ErrSelfApproval {
			t.Fatalf("expected ErrSelfApproval for empty actors list when checker required, got %v", errEmpty)
		}
	})

	t.Run("Creator self-approval is DENIED", func(t *testing.T) {
		err := EvaluateMakerChecker(true, "user-creator", []string{"user-creator"})
		if err != ErrSelfApproval {
			t.Fatalf("expected ErrSelfApproval for creator self-approval, got %v", err)
		}
	})

	t.Run("Co-author self-approval is DENIED (multi-actor check)", func(t *testing.T) {
		// User1 created, User2 edited. User2 attempts to approve.
		errCoauthor := EvaluateMakerChecker(true, "user2-coauthor", []string{"user1-creator", "user2-coauthor"})
		if errCoauthor != ErrSelfApproval {
			t.Fatalf("expected ErrSelfApproval for co-author self-approval, got %v", errCoauthor)
		}

		// User1 created, User2 edited. User1 (original creator) attempts to approve.
		errCreator := EvaluateMakerChecker(true, "user1-creator", []string{"user1-creator", "user2-coauthor"})
		if errCreator != ErrSelfApproval {
			t.Fatalf("expected ErrSelfApproval for creator approval after co-author edit, got %v", errCreator)
		}
	})

	t.Run("Distinct approver is ALLOWED", func(t *testing.T) {
		err := EvaluateMakerChecker(true, "user3-distinct", []string{"user1-creator", "user2-coauthor"})
		if err != nil {
			t.Fatalf("expected nil error for distinct approver, got %v", err)
		}
	})

	t.Run("Policy disabled ALLOWS self-approval and unknown creator", func(t *testing.T) {
		if err := EvaluateMakerChecker(false, "user-creator", []string{"user-creator"}); err != nil {
			t.Fatalf("expected nil error when policy disabled, got %v", err)
		}
		if err := EvaluateMakerChecker(false, "user-creator", nil); err != nil {
			t.Fatalf("expected nil error when policy disabled with unknown creator, got %v", err)
		}
	})
}
