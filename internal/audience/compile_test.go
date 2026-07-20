package audience

import (
	"os"
	"path/filepath"
	"strings"
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
		{
			name:     "score greater_than operator",
			node:     &Score{Model: "model-1", ScoreName: "purchase_propensity", Operator: "greater_than", Value: 0.85},
			expected: "SELECT external_id FROM profiles WHERE tenant_id=$1 AND workspace_id=$2 AND (id IN (SELECT profile_id FROM profile_scores WHERE tenant_id = $1 AND workspace_id = $2 AND scoring_model_id = $3 AND score_name = $4 AND value > $5))",
			args:     []any{"model-1", "purchase_propensity", 0.85},
		},
		{
			name:     "score less_than operator",
			node:     &Score{Model: "model-1", ScoreName: "churn_risk", Operator: "less_than", Value: 0.3},
			expected: "SELECT external_id FROM profiles WHERE tenant_id=$1 AND workspace_id=$2 AND (id IN (SELECT profile_id FROM profile_scores WHERE tenant_id = $1 AND workspace_id = $2 AND scoring_model_id = $3 AND score_name = $4 AND value < $5))",
			args:     []any{"model-1", "churn_risk", 0.3},
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

func TestCompileProfileRows(t *testing.T) {
	node := &And{Conditions: []Node{
		&ProfileAttribute{Field: "country", Operator: "equals", Value: "US"},
		&Consent{Channel: "email", Topic: "marketing", State: "subscribed"},
	}}
	sql, args, err := CompileProfileRows(node, []string{"external_id", "email", "plan"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(sql, "SELECT external_id, attributes->>'email' AS email, attributes->>'plan' AS plan FROM profiles") {
		t.Fatalf("projection does not contain mapped fields: %s", sql)
	}
	if !strings.Contains(sql, "FROM consent_ledger") || !strings.Contains(sql, "latest.state = $6") {
		t.Fatalf("projection is not consent-aware: %s", sql)
	}
	if len(args) != 4 || args[0] != "US" || args[1] != "email" || args[2] != "marketing" || args[3] != "subscribed" {
		t.Fatalf("unexpected parameterization: %v", args)
	}

	if _, _, err := CompileProfileRows(node, []string{"external_id", "email' OR TRUE --"}); err == nil {
		t.Fatal("unsafe mapped field was accepted")
	}
}

func TestCompileConsent(t *testing.T) {
	node := &Consent{Channel: "email", Topic: "marketing", State: "subscribed"}
	sql, args := CompileConsent(node, "tenant-1", "app-1")
	expected := `SELECT profile_id FROM (
		SELECT DISTINCT ON (profile_id) profile_id, state
		FROM consent_ledger
		WHERE tenant_id = $1 AND app_id = $2 AND channel = $3 AND topic = $4
		ORDER BY profile_id, occurred_at DESC
	) latest WHERE state = $5`

	if sql != expected {
		t.Errorf("expected SQL:\n%s\ngot:\n%s", expected, sql)
	}
	if len(args) != 5 || args[0] != "tenant-1" || args[1] != "app-1" || args[2] != "email" || args[3] != "marketing" || args[4] != "subscribed" {
		t.Errorf("unexpected args: %v", args)
	}
}

func TestCompileClickHouse(t *testing.T) {
	node := &EventHistory{EventType: "purchase", Operator: "has_occurred", TimeWindowDays: 30, MinCount: 2}
	sql, args := CompileClickHouse(node, "tenant-1")
	expected := `SELECT subject_hash FROM behavior_events
		WHERE tenant_id = ? AND event_type = ? AND occurred_at >= now() - INTERVAL ? DAY
		GROUP BY subject_hash HAVING count() >= ?`

	if sql != expected {
		t.Errorf("expected SQL:\n%s\ngot:\n%s", expected, sql)
	}
	if len(args) != 4 || args[0] != "tenant-1" || args[1] != "purchase" || args[2] != 30 || args[3] != 2 {
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

	if os.Getenv("UPDATE_GOLDEN") == "true" {
		_ = os.MkdirAll("testdata", 0755)
		_ = os.WriteFile(goldenPath, []byte(sql), 0644)
	}

	expected, err := os.ReadFile(goldenPath)
	if err != nil {
		if os.IsNotExist(err) {
			t.Fatalf("golden file is missing: %s. Run with UPDATE_GOLDEN=true env var to generate.", goldenPath)
		}
		t.Fatalf("failed to read golden file: %v", err)
	}

	if sql != string(expected) {
		t.Errorf("golden mismatch!\nexpected: %s\ngot: %s", string(expected), sql)
	}
}
