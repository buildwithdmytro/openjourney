package audience

import (
	"context"
	"crypto/sha256"
	"fmt"
	"regexp"
)

type EvaluatorStore interface {
	QueryProfileMatches(ctx context.Context, sql string, args []any) (bool, error)
	QueryConsentMatches(ctx context.Context, sql string, args []any) (bool, error)
	QueryClickHouseMatches(ctx context.Context, sql string, args []any) (bool, error)
	GetProfileExternalID(ctx context.Context, tenantID, workspaceID, profileID string) (string, error)
}


func Matches(ctx context.Context, store EvaluatorStore, tenantID, workspaceID, appID, profileID string, node Node) (bool, error) {
	switch n := node.(type) {
	case *And:
		for _, cond := range n.Conditions {
			matched, err := Matches(ctx, store, tenantID, workspaceID, appID, profileID, cond)
			if err != nil {
				return false, err
			}
			if !matched {
				return false, nil
			}
		}
		return true, nil

	case *Or:
		for _, cond := range n.Conditions {
			matched, err := Matches(ctx, store, tenantID, workspaceID, appID, profileID, cond)
			if err != nil {
				return false, err
			}
			if matched {
				return true, nil
			}
		}
		return false, nil

	case *Not:
		matched, err := Matches(ctx, store, tenantID, workspaceID, appID, profileID, n.Condition)
		if err != nil {
			return false, err
		}
		return !matched, nil

	case *ProfileAttribute:
		sql, args, err := CompileProfileSingle(n)
		if err != nil {
			return false, err
		}
		fullArgs := append([]any{tenantID, workspaceID, profileID}, args...)
		return store.QueryProfileMatches(ctx, sql, fullArgs)

	case *Consent:
		sql, args := CompileConsentSingle(n, tenantID, appID, profileID)
		return store.QueryConsentMatches(ctx, sql, args)

	case *EventHistory:
		extID, err := store.GetProfileExternalID(ctx, tenantID, workspaceID, profileID)
		if err != nil {
			return false, err
		}
		if extID == "" {
			return n.Operator == "has_not_occurred", nil
		}
		h := sha256.Sum256([]byte(extID))
		subjectHash := fmt.Sprintf("%x", h)

		sql, args := CompileClickHouseSingle(n, tenantID, subjectHash)
		matched, err := store.QueryClickHouseMatches(ctx, sql, args)
		if err != nil {
			return false, err
		}

		if n.Operator == "has_not_occurred" {
			return !matched, nil
		}
		return matched, nil

	default:
		return false, fmt.Errorf("unknown AST node type: %T", n)
	}
}

func CompileProfileSingle(node Node) (string, []any, error) {
	var args []any
	expr, err := compileProfileNode(node, &args)
	if err != nil {
		return "", nil, err
	}
	if expr == "" {
		expr = "TRUE"
	}
	// Shift any placeholder > 2 by 1 to make room for $3 (profileID)
	re := regexp.MustCompile(`\$([0-9]+)`)
	expr = re.ReplaceAllStringFunc(expr, func(m string) string {
		var n int
		fmt.Sscanf(m, "$%d", &n)
		if n > 2 {
			return fmt.Sprintf("$%d", n+1)
		}
		return m
	})
	sql := fmt.Sprintf("SELECT 1 FROM profiles WHERE tenant_id=$1 AND workspace_id=$2 AND id=$3 AND (%s)", expr)
	return sql, args, nil
}

func CompileConsentSingle(n *Consent, tenantID, appID, profileID string) (string, []any) {
	sql := `SELECT 1 FROM (
		SELECT state FROM consent_ledger
		WHERE tenant_id = $1 AND app_id = $2 AND profile_id = $3 AND channel = $4 AND topic = $5
		ORDER BY occurred_at DESC LIMIT 1
	) latest WHERE state = $6`
	args := []any{tenantID, appID, profileID, n.Channel, n.Topic, n.State}
	return sql, args
}

func CompileClickHouseSingle(n *EventHistory, tenantID, subjectHash string) (string, []any) {
	sql := `SELECT 1 FROM behavior_events
		WHERE tenant_id = ? AND event_type = ? AND occurred_at >= now() - INTERVAL ? DAY AND subject_hash = ?
		GROUP BY subject_hash HAVING count() >= ?`
	minCount := n.MinCount
	if minCount <= 0 {
		minCount = 1
	}
	args := []any{tenantID, n.EventType, n.TimeWindowDays, subjectHash, minCount}
	return sql, args
}
