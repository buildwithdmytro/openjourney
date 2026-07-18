package scoring

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"

	"github.com/buildwithdmytro/openjourney/internal/domain"
	"github.com/buildwithdmytro/openjourney/internal/ports"
)

type EvalResult struct {
	Passed  int
	Failed  int
	Verdict string // "passed" or "failed"
}

type CaseEvalResult struct {
	CaseID string
	Passed bool
	Error  string
}

// EvaluateExpressionModel runs the evaluation cases from a dataset against a scoring model version
// of kind 'expression'. It updates the version's eval_status in the store.
func EvaluateExpressionModel(ctx context.Context, store ports.Store, p domain.Principal, versionID string, datasetID string) (EvalResult, []CaseEvalResult, error) {
	// 1. Fetch the scoring model version
	sv, err := store.GetScoringModelVersion(ctx, p, versionID)
	if err != nil {
		return EvalResult{}, nil, err
	}

	// 2. Fetch the parent scoring model to verify kind
	model, err := store.GetScoringModel(ctx, p, sv.ScoringModelID)
	if err != nil {
		return EvalResult{}, nil, err
	}

	if model.Kind != "expression" {
		return EvalResult{}, nil, fmt.Errorf("evaluator only supports expression scoring models, got %s", model.Kind)
	}

	// 3. Extract the expression
	var def struct {
		Expr string `json:"expr"`
	}
	if err := json.Unmarshal(sv.Definition, &def); err != nil {
		return EvalResult{}, nil, fmt.Errorf("invalid scoring model definition: %w", err)
	}
	if def.Expr == "" {
		return EvalResult{}, nil, errors.New("expression 'expr' is empty or missing in definition")
	}

	// 4. Fetch the eval cases
	cases, err := store.ListEvalCases(ctx, p, datasetID)
	if err != nil {
		return EvalResult{}, nil, err
	}

	var results []CaseEvalResult
	passed, failed := 0, 0

	for _, c := range cases {
		caseResult := evaluateCase(def.Expr, sv.OutputMin, sv.OutputMax, c)
		results = append(results, caseResult)
		if caseResult.Passed {
			passed++
		} else {
			failed++
		}
	}

	verdict := "passed"
	if failed > 0 || len(cases) == 0 {
		verdict = "failed"
	}

	// 5. Update status in database
	if err := store.SetScoringModelVersionEvalStatus(ctx, p, versionID, verdict); err != nil {
		return EvalResult{}, results, err
	}

	return EvalResult{
		Passed:  passed,
		Failed:  failed,
		Verdict: verdict,
	}, results, nil
}

func evaluateCase(expr string, min, max float64, c domain.EvalCase) CaseEvalResult {
	res := CaseEvalResult{CaseID: c.ID}

	// Parse input as environment
	var env map[string]any
	if err := json.Unmarshal(c.Input, &env); err != nil {
		res.Error = fmt.Sprintf("invalid input JSON: %v", err)
		return res
	}

	// Parse expectations
	var expectations struct {
		ExpectedScore *float64 `json:"expected_score"`
		ExpectedValue *float64 `json:"expected_value"`
	}
	if err := json.Unmarshal(c.Expectations, &expectations); err != nil {
		res.Error = fmt.Sprintf("invalid expectations JSON: %v", err)
		return res
	}

	var expected float64
	if expectations.ExpectedScore != nil {
		expected = *expectations.ExpectedScore
	} else if expectations.ExpectedValue != nil {
		expected = *expectations.ExpectedValue
	} else {
		res.Error = "missing expected_score or expected_value in expectations"
		return res
	}

	// Evaluate in-process
	got, err := Evaluate(expr, env, min, max)
	if err != nil {
		res.Error = fmt.Sprintf("evaluation error: %v", err)
		return res
	}

	// Compare with tolerance
	if math.Abs(got-expected) > 1e-9 {
		res.Error = fmt.Sprintf("expected score %v, got %v", expected, got)
		return res
	}

	res.Passed = true
	return res
}
