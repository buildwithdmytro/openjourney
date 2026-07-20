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

// CompileProfileRows compiles an audience into a projection suitable for a
// reverse-ETL sink. Column names are configuration supplied identifiers, so
// they are validated before being interpolated into the SELECT list. Values
// used by the audience expression (and consent ledger) remain parameters.
func CompileProfileRows(node Node, fields []string) (string, []any, error) {
	if len(fields) == 0 {
		return "", nil, fmt.Errorf("reverse-ETL projection requires at least one field")
	}

	columns := make([]string, 0, len(fields))
	seen := make(map[string]struct{}, len(fields))
	for _, field := range fields {
		if !fieldSafetyRegex.MatchString(field) {
			return "", nil, fmt.Errorf("unsafe or invalid mapped profile field name: %s", field)
		}
		if _, ok := seen[field]; ok {
			return "", nil, fmt.Errorf("duplicate mapped profile field: %s", field)
		}
		seen[field] = struct{}{}

		switch field {
		case "id", "external_id", "anonymous_id", "created_at", "updated_at":
			columns = append(columns, field)
		default:
			columns = append(columns, fmt.Sprintf("attributes->>'%s' AS %s", field, field))
		}
	}

	args := make([]any, 0)
	expr, err := compileProfileNode(node, &args)
	if err != nil {
		return "", nil, err
	}
	if expr == "" {
		expr = "TRUE"
	}
	return fmt.Sprintf("SELECT %s FROM profiles WHERE tenant_id=$1 AND workspace_id=$2 AND (%s)",
		strings.Join(columns, ", "), expr), args, nil
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

	case *Consent:
		// Consent is correlated to the profile's app so this compiler can keep
		// the existing tenant/workspace parameter contract while still using
		// the latest consent row for reverse-ETL suppression decisions.
		*args = append(*args, n.Channel, n.Topic, n.State)
		channelPlaceholder := fmt.Sprintf("$%d", len(*args))
		topicPlaceholder := fmt.Sprintf("$%d", len(*args)+1)
		statePlaceholder := fmt.Sprintf("$%d", len(*args)+2)
		return fmt.Sprintf(`id IN (
			SELECT latest.profile_id FROM (
				SELECT DISTINCT ON (cl.profile_id) cl.profile_id, cl.state
				FROM consent_ledger cl
				WHERE cl.tenant_id = profiles.tenant_id AND cl.app_id = profiles.app_id
					AND cl.channel = %s AND cl.topic = %s
				ORDER BY cl.profile_id, cl.occurred_at DESC
			) latest WHERE latest.state = %s
		)`, channelPlaceholder, topicPlaceholder, statePlaceholder), nil

	case *Score:
		if n.Model == "" {
			return "", fmt.Errorf("score condition requires model")
		}
		if n.ScoreName == "" {
			return "", fmt.Errorf("score condition requires score_name")
		}
		var opSql string
		switch n.Operator {
		case "greater_than":
			opSql = ">"
		case "less_than":
			opSql = "<"
		case "equals":
			opSql = "="
		default:
			return "", fmt.Errorf("unsupported score operator: %s", n.Operator)
		}

		*args = append(*args, n.Model)
		modelPlaceholder := fmt.Sprintf("$%d", len(*args)+2)

		*args = append(*args, n.ScoreName)
		namePlaceholder := fmt.Sprintf("$%d", len(*args)+2)

		*args = append(*args, n.Value)
		valuePlaceholder := fmt.Sprintf("$%d", len(*args)+2)

		return fmt.Sprintf("id IN (SELECT profile_id FROM profile_scores WHERE tenant_id = $1 AND workspace_id = $2 AND scoring_model_id = %s AND score_name = %s AND value %s %s)",
			modelPlaceholder, namePlaceholder, opSql, valuePlaceholder), nil

	case *Company:
		if !fieldSafetyRegex.MatchString(n.Field) {
			return "", fmt.Errorf("unsafe or invalid company field name: %s", n.Field)
		}
		*args = append(*args, n.Value)
		placeholder := fmt.Sprintf("$%d", len(*args)+2)
		condition := fmt.Sprintf("c.attributes->>'%s' = %s", n.Field, placeholder)
		switch n.Operator {
		case "equals":
		case "contains":
			condition = fmt.Sprintf("c.attributes->>'%s' LIKE '%%' || %s || '%%'", n.Field, placeholder)
		case "in":
			condition = fmt.Sprintf("c.attributes->>'%s' = ANY(%s)", n.Field, placeholder)
		default:
			return "", fmt.Errorf("unsupported company operator: %s", n.Operator)
		}
		return "id IN (SELECT m.profile_id FROM company_members m JOIN companies c ON c.id=m.company_id WHERE m.tenant_id=$1 AND c.tenant_id=$1 AND c.workspace_id=$2 AND " + condition + ")", nil

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
