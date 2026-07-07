package audience

import (
	"os"
	"path/filepath"
	"testing"
)

func TestCompileProfile(t *testing.T) {
	tests := []struct {
		name     string
		node     Node
		expected string
		args     []any
	}{
		{
			name:     "equals operator",
			node:     &ProfileAttribute{Field: "country", Operator: "equals", Value: "US"},
			expected: "SELECT external_id FROM profiles WHERE tenant_id=$1 AND workspace_id=$2 AND (attributes->>'country' = $3)",
			args:     []any{"US"},
		},
		{
			name:     "contains operator",
			node:     &ProfileAttribute{Field: "tags", Operator: "contains", Value: "premium"},
			expected: "SELECT external_id FROM profiles WHERE tenant_id=$1 AND workspace_id=$2 AND (attributes->>'tags' LIKE '%' || $3 || '%')",
			args:     []any{"premium"},
		},
		{
			name:     "in operator",
			node:     &ProfileAttribute{Field: "status", Operator: "in", Value: []string{"active", "pending"}},
			expected: "SELECT external_id FROM profiles WHERE tenant_id=$1 AND workspace_id=$2 AND (attributes->>'status' = ANY($3))",
			args:     []any{[]string{"active", "pending"}},
		},
		{
			name:     "greater_than operator",
			node:     &ProfileAttribute{Field: "age", Operator: "greater_than", Value: 21},
			expected: "SELECT external_id FROM profiles WHERE tenant_id=$1 AND workspace_id=$2 AND ((attributes->>'age')::numeric > ($3)::numeric)",
			args:     []any{21},
		},
		{
			name:     "less_than operator",
			node:     &ProfileAttribute{Field: "score", Operator: "less_than", Value: 100},
			expected: "SELECT external_id FROM profiles WHERE tenant_id=$1 AND workspace_id=$2 AND ((attributes->>'score')::numeric < ($3)::numeric)",
			args:     []any{100},
		},
		{
			name: "nested AND/OR/NOT",
			node: &And{
				Conditions: []Node{
					&ProfileAttribute{Field: "country", Operator: "equals", Value: "US"},
					&Or{
						Conditions: []Node{
							&ProfileAttribute{Field: "plan", Operator: "equals", Value: "gold"},
							&Not{
								Condition: &ProfileAttribute{Field: "churned", Operator: "equals", Value: true},
							},
						},
					},
				},
			},
			expected: "SELECT external_id FROM profiles WHERE tenant_id=$1 AND workspace_id=$2 AND ((attributes->>'country' = $3 AND (attributes->>'plan' = $4 OR NOT (attributes->>'churned' = $5))))",
			args:     []any{"US", "gold", true},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			sql, args, err := CompileProfile(tc.node)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if sql != tc.expected {
				t.Errorf("expected SQL:\n%s\ngot:\n%s", tc.expected, sql)
			}
			if len(args) != len(tc.args) {
				t.Fatalf("expected %d args, got %d", len(tc.args), len(args))
			}
			for i, arg := range args {
				switch v := arg.(type) {
				case []string:
					exp := tc.args[i].([]string)
					if len(v) != len(exp) || v[0] != exp[0] || v[1] != exp[1] {
						t.Errorf("arg %d: expected %v, got %v", i, tc.args[i], arg)
					}
				default:
					if arg != tc.args[i] {
						t.Errorf("arg %d: expected %v, got %v", i, tc.args[i], arg)
					}
				}
			}
		})
	}
}

func TestCompileConsent(t *testing.T) {
	node := &Consent{Channel: "email", Topic: "marketing", State: "subscribed"}
	sql, args := CompileConsent(node)
	expected := `SELECT profile_id FROM (
		SELECT DISTINCT ON (profile_id) profile_id, state
		FROM consent_ledger
		WHERE tenant_id = $1 AND app_id = $2 AND channel = $3 AND topic = $4
		ORDER BY profile_id, occurred_at DESC
	) latest WHERE state = $5`

	if sql != expected {
		t.Errorf("expected SQL:\n%s\ngot:\n%s", expected, sql)
	}
	if len(args) != 3 || args[0] != "email" || args[1] != "marketing" || args[2] != "subscribed" {
		t.Errorf("unexpected args: %v", args)
	}
}

func TestCompileClickHouse(t *testing.T) {
	node := &EventHistory{EventType: "purchase", Operator: "has_occurred", TimeWindowDays: 30, MinCount: 2}
	sql, args := CompileClickHouse(node)
	expected := `SELECT subject_hash FROM behavior_events
		WHERE tenant_id = ? AND event_type = ? AND occurred_at >= now() - INTERVAL ? DAY
		GROUP BY subject_hash HAVING count() >= ?`

	if sql != expected {
		t.Errorf("expected SQL:\n%s\ngot:\n%s", expected, sql)
	}
	if len(args) != 3 || args[0] != "purchase" || args[1] != 30 || args[2] != 2 {
		t.Errorf("unexpected args: %v", args)
	}
}

func TestGoldenQueries(t *testing.T) {
	node := &And{
		Conditions: []Node{
			&ProfileAttribute{Field: "country", Operator: "equals", Value: "US"},
			&ProfileAttribute{Field: "age", Operator: "greater_than", Value: 21},
		},
	}
	sql, _, err := CompileProfile(node)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	goldenPath := filepath.Join("testdata", "nested_profile.sql")

	_ = os.MkdirAll("testdata", 0755)

	if _, err := os.Stat(goldenPath); os.IsNotExist(err) {
		_ = os.WriteFile(goldenPath, []byte(sql), 0644)
	}

	expected, err := os.ReadFile(goldenPath)
	if err != nil {
		t.Fatalf("failed to read golden file: %v", err)
	}

	if sql != string(expected) {
		t.Errorf("golden mismatch!\nexpected: %s\ngot: %s", string(expected), sql)
	}
}
