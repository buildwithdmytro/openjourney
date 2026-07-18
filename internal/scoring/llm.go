package scoring

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"

	"github.com/buildwithdmytro/openjourney/internal/ai"
	"github.com/buildwithdmytro/openjourney/internal/domain"
	"github.com/buildwithdmytro/openjourney/internal/ports"
	"github.com/buildwithdmytro/openjourney/internal/render"
)

// EvaluateLLMModel evaluates a scoring model version of kind 'llm' using the AI Gateway.
// It retrieves the cases from a dataset, runs them, checks the expectations,
// and updates the model version's eval_status.
func EvaluateLLMModel(ctx context.Context, store ports.Store, gateway *ai.Gateway, p domain.Principal, versionID string, datasetID string) (EvalResult, []CaseEvalResult, error) {
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

	if model.Kind != "llm" {
		return EvalResult{}, nil, fmt.Errorf("evaluator only supports llm scoring models, got %s", model.Kind)
	}

	// 3. Extract the prompt version ID
	var def struct {
		PromptVersionID string `json:"prompt_version_id"`
	}
	if err := json.Unmarshal(sv.Definition, &def); err != nil {
		return EvalResult{}, nil, fmt.Errorf("invalid scoring model definition: %w", err)
	}
	if def.PromptVersionID == "" {
		return EvalResult{}, nil, errors.New("missing prompt_version_id in definition")
	}

	// Verify the prompt version is usable (status='active' and eval_status='passed')
	pv, err := store.GetPromptVersion(ctx, p, def.PromptVersionID)
	if err != nil {
		return EvalResult{}, nil, fmt.Errorf("load prompt version: %w", err)
	}
	if pv.Status != "active" || pv.EvalStatus != "passed" {
		return EvalResult{}, nil, fmt.Errorf("prompt version is not usable: status=%s, eval_status=%s", pv.Status, pv.EvalStatus)
	}

	// 4. Fetch the eval cases
	cases, err := store.ListEvalCases(ctx, p, datasetID)
	if err != nil {
		return EvalResult{}, nil, err
	}

	var caseResults []CaseEvalResult
	passed, failed := 0, 0

	for _, c := range cases {
		caseResult := evaluateLLMCase(ctx, gateway, pv, sv, c, p)
		caseResults = append(caseResults, caseResult)
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
		return EvalResult{}, caseResults, err
	}

	return EvalResult{
		Passed:  passed,
		Failed:  failed,
		Verdict: verdict,
	}, caseResults, nil
}

func evaluateLLMCase(ctx context.Context, gateway *ai.Gateway, pv domain.PromptVersion, sv domain.ScoringModelVersion, c domain.EvalCase, p domain.Principal) CaseEvalResult {
	res := CaseEvalResult{CaseID: c.ID}

	// Parse input as environment
	var env map[string]any
	if err := json.Unmarshal(c.Input, &env); err != nil {
		res.Error = fmt.Sprintf("invalid input JSON: %v", err)
		return res
	}

	// Render prompt using Liquid
	prompt, err := render.Render(pv.Template, env)
	if err != nil {
		prompt = pv.Template + "\n\n" + string(c.Input)
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

	// Call Gateway.Generate
	response, err := gateway.Generate(ctx, p, ai.GenerateRequest{
		PromptVersionID: pv.ID,
		Prompt:          prompt,
		OutputSchema:    pv.OutputSchema,
		Model:           pv.Model,
		Action:          "scores.compute",
	})
	if err != nil {
		res.Error = fmt.Sprintf("gateway generate error: %v", err)
		return res
	}

	// Parse numeric score from structured JSON output
	got, err := parseJSONScore(response.Content)
	if err != nil {
		res.Error = fmt.Sprintf("parse score error: %v", err)
		return res
	}

	// Clamp to [output_min, output_max]
	if got < sv.OutputMin {
		got = sv.OutputMin
	}
	if got > sv.OutputMax {
		got = sv.OutputMax
	}

	// Compare with tolerance
	if math.Abs(got-expected) > 1e-9 {
		res.Error = fmt.Sprintf("expected score %v, got %v", expected, got)
		return res
	}

	res.Passed = true
	return res
}

// EvaluateLLM computes the score for a single profile using LLM gateway.
// The scoring model version must be usable (eval_status='passed').
func EvaluateLLM(ctx context.Context, store ports.Store, gateway *ai.Gateway, p domain.Principal, sv domain.ScoringModelVersion, env map[string]any) (float64, error) {
	if sv.EvalStatus != "passed" {
		return 0, fmt.Errorf("scoring model version %s is not evaluated/passed", sv.ID)
	}

	// Extract the prompt version ID
	var def struct {
		PromptVersionID string `json:"prompt_version_id"`
	}
	if err := json.Unmarshal(sv.Definition, &def); err != nil {
		return 0, fmt.Errorf("invalid scoring model definition: %w", err)
	}
	if def.PromptVersionID == "" {
		return 0, errors.New("missing prompt_version_id in definition")
	}

	// Verify prompt version exists and is active/passed
	pv, err := store.GetPromptVersion(ctx, p, def.PromptVersionID)
	if err != nil {
		return 0, fmt.Errorf("load prompt version: %w", err)
	}
	if pv.Status != "active" || pv.EvalStatus != "passed" {
		return 0, fmt.Errorf("prompt version %s is not usable: status=%s, eval_status=%s", pv.ID, pv.Status, pv.EvalStatus)
	}

	// Render prompt using Liquid
	prompt, err := render.Render(pv.Template, env)
	if err != nil {
		prompt = pv.Template
	}

	// Call Gateway.Generate
	response, err := gateway.Generate(ctx, p, ai.GenerateRequest{
		PromptVersionID: pv.ID,
		Prompt:          prompt,
		OutputSchema:    pv.OutputSchema,
		Model:           pv.Model,
		Action:          "scores.compute",
	})
	if err != nil {
		return 0, fmt.Errorf("gateway generate: %w", err)
	}

	// Parse numeric score from structured JSON output
	got, err := parseJSONScore(response.Content)
	if err != nil {
		return 0, fmt.Errorf("parse score: %w", err)
	}

	// Clamp to [output_min, output_max]
	if got < sv.OutputMin {
		got = sv.OutputMin
	}
	if got > sv.OutputMax {
		got = sv.OutputMax
	}

	return got, nil
}

func parseJSONScore(content string) (float64, error) {
	// Try parsing as float64 directly
	var val float64
	if err := json.Unmarshal([]byte(content), &val); err == nil {
		return val, nil
	}

	// Try parsing as object
	var obj map[string]any
	if err := json.Unmarshal([]byte(content), &obj); err == nil {
		for _, key := range []string{"score", "value", "rating"} {
			if v, ok := obj[key]; ok {
				if f, ok := toFloat(v); ok {
					return f, nil
				}
			}
		}
		// If object but key not matches, take the first numeric value
		for _, v := range obj {
			if f, ok := toFloat(v); ok {
				return f, nil
			}
		}
	}
	return 0, fmt.Errorf("could not extract numeric score from content: %s", content)
}
