package scoring

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestBuildLeadScoreDefinitionUsesExpressionRegistryShape(t *testing.T) {
	definition, outputMax, err := BuildLeadScoreDefinition(LeadScoreInput{
		Name: "Acquisition score", ScoreName: "lead_score",
		Expression: "events.demo.count_30d * 10", OutputMax: 250,
	})
	if err != nil {
		t.Fatal(err)
	}
	var got struct {
		Expr string `json:"expr"`
	}
	if err := json.Unmarshal(definition, &got); err != nil {
		t.Fatal(err)
	}
	if got.Expr != "events.demo.count_30d * 10" || outputMax != 250 {
		t.Fatalf("unexpected definition: %s max=%v", definition, outputMax)
	}
}

func TestBuildLeadScoreDefinitionDefaultsAndRejectsInvalidInput(t *testing.T) {
	_, outputMax, err := BuildLeadScoreDefinition(LeadScoreInput{Name: "score", ScoreName: "lead", Expression: "1"})
	if err != nil || outputMax != 100 {
		t.Fatalf("expected default cap 100, got %v, %v", outputMax, err)
	}
	_, _, err = BuildLeadScoreDefinition(LeadScoreInput{Name: "score", ScoreName: "lead", Expression: "profile."})
	if err == nil || !strings.Contains(err.Error(), "invalid expression") {
		t.Fatalf("expected invalid expression error, got %v", err)
	}
}
