package scoring

import (
	"encoding/json"
	"errors"
	"fmt"
	"go/parser"
	"math"
)

// LeadScoreInput is the friendly authoring shape for an expression scoring
// model. The expression uses the same profile/events environment as M7.
type LeadScoreInput struct {
	Name       string  `json:"name"`
	ScoreName  string  `json:"score_name"`
	Expression string  `json:"expression"`
	OutputMax  float64 `json:"output_max"`
}

// BuildLeadScoreDefinition validates the authoring input and produces the
// existing scoring_model_versions definition. Lead scores deliberately remain
// expression models so scores.compute writes profile_scores and the Score DSL
// leaf can query them.
func BuildLeadScoreDefinition(input LeadScoreInput) (json.RawMessage, float64, error) {
	if input.Name == "" {
		return nil, 0, errors.New("name is required")
	}
	if input.ScoreName == "" {
		return nil, 0, errors.New("score_name is required")
	}
	if input.Expression == "" {
		return nil, 0, errors.New("expression is required")
	}
	if _, err := parser.ParseExpr(input.Expression); err != nil {
		return nil, 0, fmt.Errorf("invalid expression: %w", err)
	}
	if input.OutputMax == 0 {
		input.OutputMax = 100
	}
	if input.OutputMax <= 0 || math.IsNaN(input.OutputMax) || math.IsInf(input.OutputMax, 0) {
		return nil, 0, errors.New("output_max must be a finite positive number")
	}
	definition, err := json.Marshal(struct {
		Expr string `json:"expr"`
	}{Expr: input.Expression})
	if err != nil {
		return nil, 0, fmt.Errorf("marshal lead score definition: %w", err)
	}
	return definition, input.OutputMax, nil
}
