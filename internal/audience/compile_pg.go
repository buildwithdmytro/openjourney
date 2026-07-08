package audience

import (
	"fmt"
	"regexp"
	"strings"
)

func CompileProfile(node Node) (string, []any, error) {
	var args []any
	expr, err := compileProfileNode(node, &args)
	if err != nil {
		return "", nil, err
	}
	if expr == "" {
		expr = "TRUE"
	}
	sql := fmt.Sprintf("SELECT external_id FROM profiles WHERE tenant_id=$1 AND workspace_id=$2 AND (%s)", expr)
	return sql, args, nil
}

func compileProfileNode(node Node, args *[]any) (string, error) {
	switch n := node.(type) {
	case *And:
		var parts []string
		for _, cond := range n.Conditions {
			part, err := compileProfileNode(cond, args)
			if err != nil {
				return "", err
			}
			if part != "" {
				parts = append(parts, part)
			}
		}
		if len(parts) == 0 {
			return "", nil
		}
		return "(" + strings.Join(parts, " AND ") + ")", nil

	case *Or:
		var parts []string
		for _, cond := range n.Conditions {
			part, err := compileProfileNode(cond, args)
			if err != nil {
				return "", err
			}
			if part != "" {
				parts = append(parts, part)
			}
		}
		if len(parts) == 0 {
			return "", nil
		}
		return "(" + strings.Join(parts, " OR ") + ")", nil

	case *Not:
		part, err := compileProfileNode(n.Condition, args)
		if err != nil {
			return "", err
		}
		if part == "" {
			return "", nil
		}
		return "NOT (" + part + ")", nil

	case *ProfileAttribute:
		if !fieldSafetyRegex.MatchString(n.Field) {
			return "", fmt.Errorf("unsafe or invalid profile field name: %s", n.Field)
		}
		*args = append(*args, n.Value)
		paramNum := len(*args) + 2 // $1 is tenant_id, $2 is workspace_id
		placeholder := fmt.Sprintf("$%d", paramNum)

		switch n.Operator {
		case "equals":
			return fmt.Sprintf("attributes->>'%s' = %s", n.Field, placeholder), nil
		case "contains":
			return fmt.Sprintf("attributes->>'%s' LIKE '%%' || %s || '%%'", n.Field, placeholder), nil
		case "in":
			return fmt.Sprintf("attributes->>'%s' = ANY(%s)", n.Field, placeholder), nil
		case "greater_than":
			return fmt.Sprintf("(attributes->>'%s')::numeric > (%s)::numeric", n.Field, placeholder), nil
		case "less_than":
			return fmt.Sprintf("(attributes->>'%s')::numeric < (%s)::numeric", n.Field, placeholder), nil
		default:
			return "", fmt.Errorf("unsupported profile operator: %s", n.Operator)
		}

	default:
		return "", nil
	}
}

var fieldSafetyRegex = regexp.MustCompile(`^[a-zA-Z0-9_]+$`)

func CompileConsent(n *Consent, tenantID, appID string) (string, []any) {
	sql := `SELECT profile_id FROM (
		SELECT DISTINCT ON (profile_id) profile_id, state
		FROM consent_ledger
		WHERE tenant_id = $1 AND app_id = $2 AND channel = $3 AND topic = $4
		ORDER BY profile_id, occurred_at DESC
	) latest WHERE state = $5`
	args := []any{tenantID, appID, n.Channel, n.Topic, n.State}
	return sql, args
}
