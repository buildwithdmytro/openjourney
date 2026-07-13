package postgres

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/buildwithdmytro/openjourney/internal/domain"
)

func TestRecommendWinnerRequiresSignificanceAndHealthyGuardrails(t *testing.T) {
	tests := []struct {
		name    string
		variant domain.ExperimentVariantReport
		want    string
	}{
		{
			name: "significant uplift without regression",
			variant: domain.ExperimentVariantReport{Label: "treatment", Sent: 1000, Conversions: 200, Rate: .2, Uplift: 1, PValue: .001,
				Guardrails: []domain.ExperimentGuardrail{{GoalName: "churn", Conversions: 10, Rate: .01}}},
			want: "treatment",
		},
		{
			name: "insignificant uplift",
			variant: domain.ExperimentVariantReport{Label: "treatment", Sent: 1000, Conversions: 110, Rate: .11, Uplift: .1, PValue: .2,
				Guardrails: []domain.ExperimentGuardrail{{GoalName: "churn", Conversions: 10, Rate: .01}}},
		},
		{
			name: "significant guardrail regression",
			variant: domain.ExperimentVariantReport{Label: "treatment", Sent: 1000, Conversions: 200, Rate: .2, Uplift: 1, PValue: .001,
				Guardrails: []domain.ExperimentGuardrail{{GoalName: "churn", Conversions: 40, Rate: .04}}},
		},
	}
	control := domain.ExperimentVariantReport{Label: "control", IsControl: true, Sent: 1000, Conversions: 100, Rate: .1,
		Guardrails: []domain.ExperimentGuardrail{{GoalName: "churn", Conversions: 10, Rate: .01}}}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := recommendWinner([]domain.ExperimentVariantReport{control, tc.variant})
			if tc.want == "" && got != nil {
				t.Fatalf("winner = %q, want none", *got)
			}
			if tc.want != "" && (got == nil || *got != tc.want) {
				t.Fatalf("winner = %v, want %q", got, tc.want)
			}
		})
	}
}

func TestPinJourneyExperiment(t *testing.T) {
	variantTemplate := "00000000-0000-0000-0000-000000000099"
	graph := json.RawMessage(`{
		"entry_node_id":"entry",
		"nodes":[
			{"id":"entry","type":"entry","config":{"trigger":"event","event_type":"signed_up"}},
			{"id":"split","type":"split","config":{"experiment_id":"exp-1","branches":[{"label":"control"},{"label":"treatment"}]}},
			{"id":"control","type":"message","config":{"template_id":"base","experiment_id":"exp-1"}},
			{"id":"treatment","type":"message","config":{"template_id":"base","experiment_id":"exp-1"}},
			{"id":"exit","type":"exit","config":{}}
		],
		"edges":[
			{"from":"entry","to":"split"},
			{"from":"split","to":"control","branch":"control"},
			{"from":"split","to":"treatment","branch":"treatment"},
			{"from":"control","to":"exit"},
			{"from":"treatment","to":"exit"}
		]
	}`)
	pinned, err := pinJourneyExperiment(graph, "exp-1", "treatment", &variantTemplate)
	if err != nil {
		t.Fatal(err)
	}
	var decoded map[string]any
	if err := json.Unmarshal(pinned, &decoded); err != nil {
		t.Fatal(err)
	}
	encoded := string(pinned)
	if contains := `"experiment_id"`; len(encoded) == 0 || !json.Valid(pinned) || strings.Contains(encoded, contains) {
		t.Fatalf("pinned graph retains experiment binding: %s", encoded)
	}
	if !strings.Contains(encoded, `"template_id":"`+variantTemplate+`"`) || !strings.Contains(encoded, `"weight":100`) {
		t.Fatalf("pinned graph does not select treatment: %s", encoded)
	}
}
