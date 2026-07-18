package scoring

import (
	"encoding/json"
	"testing"
)

func TestEvaluate(t *testing.T) {
	// Golden test cases verifying different kinds of expressions, clamping, and correctness.
	tests := []struct {
		name      string
		expr      string
		env       map[string]any
		min       float64
		max       float64
		want      float64
		expectErr bool
	}{
		{
			name: "Simple boolean condition - true",
			expr: "profile.age > 18",
			env: map[string]any{
				"profile": map[string]any{
					"age": 20.0,
				},
			},
			min:  0.0,
			max:  1.0,
			want: 1.0,
		},
		{
			name: "Simple boolean condition - false",
			expr: "profile.age > 18",
			env: map[string]any{
				"profile": map[string]any{
					"age": 16.0,
				},
			},
			min:  0.0,
			max:  1.0,
			want: 0.0,
		},
		{
			name: "Arithmetic with clamping",
			expr: "profile.income * 0.1",
			env: map[string]any{
				"profile": map[string]any{
					"income": 50000.0,
				},
			},
			min:  0.0,
			max:  100.0, // 5000.0 clamped to 100.0
			want: 100.0,
		},
		{
			name: "Arithmetic within bounds",
			expr: "profile.score_base + 15.5",
			env: map[string]any{
				"profile": map[string]any{
					"score_base": 10.0,
				},
			},
			min:  0.0,
			max:  100.0,
			want: 25.5,
		},
		{
			name: "Nested selector and event history",
			expr: "profile.attributes.is_member && (events.click.count_30d > 5 || events.purchase.count_90d >= 1)",
			env: map[string]any{
				"profile": map[string]any{
					"attributes": map[string]any{
						"is_member": true,
					},
				},
				"events": map[string]any{
					"click": map[string]any{
						"count_30d": 2.0,
					},
					"purchase": map[string]any{
						"count_90d": 1.0,
					},
				},
			},
			min:  0.0,
			max:  1.0,
			want: 1.0,
		},
		{
			name: "Complex arithmetic and parenthesis",
			expr: "((profile.a + profile.b) * 2.0) / profile.c",
			env: map[string]any{
				"profile": map[string]any{
					"a": 10.0,
					"b": 5.0,
					"c": 3.0,
				},
			},
			min:  0.0,
			max:  100.0,
			want: 10.0,
		},
		{
			name: "Unary negation",
			expr: "-profile.offset",
			env: map[string]any{
				"profile": map[string]any{
					"offset": 5.5,
				},
			},
			min:  -10.0,
			max:  10.0,
			want: -5.5,
		},
		{
			name: "Unary not",
			expr: "!profile.active",
			env: map[string]any{
				"profile": map[string]any{
					"active": false,
				},
			},
			min:  0.0,
			max:  1.0,
			want: 1.0,
		},
		{
			name: "String comparison",
			expr: `profile.tier == "gold"`,
			env: map[string]any{
				"profile": map[string]any{
					"tier": "gold",
				},
			},
			min:  0.0,
			max:  1.0,
			want: 1.0,
		},
		{
			name: "String comparison unequal",
			expr: `profile.tier != "platinum"`,
			env: map[string]any{
				"profile": map[string]any{
					"tier": "gold",
				},
			},
			min:  0.0,
			max:  1.0,
			want: 1.0,
		},
		{
			name: "JSON RawMessage unmarshalling in selector",
			expr: "profile.data.valid",
			env: map[string]any{
				"profile": map[string]any{
					"data": json.RawMessage(`{"valid": true}`),
				},
			},
			min:  0.0,
			max:  1.0,
			want: 1.0,
		},
		{
			name:      "Division by zero error",
			expr:      "10.0 / profile.zero",
			env:       map[string]any{"profile": map[string]any{"zero": 0.0}},
			expectErr: true,
		},
		{
			name:      "Missing identifier error",
			expr:      "profile.missing_field",
			env:       map[string]any{"profile": map[string]any{}},
			expectErr: true,
		},
		{
			name:      "Type mismatch operator error",
			expr:      `profile.age > "eighteen"`,
			env:       map[string]any{"profile": map[string]any{"age": 20.0}},
			expectErr: true,
		},
		{
			name:      "Syntax error",
			expr:      "profile.age >",
			env:       map[string]any{"profile": map[string]any{"age": 20.0}},
			expectErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := Evaluate(tt.expr, tt.env, tt.min, tt.max)
			if tt.expectErr {
				if err == nil {
					t.Fatalf("expected error but got nil")
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got != tt.want {
				t.Errorf("Evaluate() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestEvaluateDeterminism(t *testing.T) {
	expr := "((profile.age * 1.5) + events.click.count_30d) * 10"
	env := map[string]any{
		"profile": map[string]any{
			"age": 20.0,
		},
		"events": map[string]any{
			"click": map[string]any{
				"count_30d": 5.0,
			},
		},
	}

	// The same expression and environment must always produce the exact same result.
	first, err := Evaluate(expr, env, 0, 1000)
	if err != nil {
		t.Fatal(err)
	}

	for i := 0; i < 100; i++ {
		got, err := Evaluate(expr, env, 0, 1000)
		if err != nil {
			t.Fatal(err)
		}
		if got != first {
			t.Fatalf("determinism failed: run %d got %v, expected %v", i, got, first)
		}
	}
}
