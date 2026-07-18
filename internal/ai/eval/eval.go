// Package eval contains the deterministic, offline prompt evaluation gate.
package eval

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/buildwithdmytro/openjourney/internal/ai"
	"github.com/buildwithdmytro/openjourney/internal/domain"
	"github.com/buildwithdmytro/openjourney/internal/schemas"
)

// Store is the persistence needed by the evaluator. Keeping this interface
// narrow makes the runner usable with both Postgres and deterministic tests.
type Store interface {
	GetPromptVersion(ctx context.Context, p domain.Principal, id string) (domain.PromptVersion, error)
	ListEvalCases(ctx context.Context, p domain.Principal, datasetID string) ([]domain.EvalCase, error)
	CreateEvalRun(ctx context.Context, p domain.Principal, run domain.EvalRun) (domain.EvalRun, error)
	SetPromptVersionEvalStatus(ctx context.Context, p domain.Principal, id string, evalStatus string) error
}

type Result struct {
	Run    domain.EvalRun
	Passed int
	Failed int
}

type CaseResult struct {
	CaseID string
	Passed bool
	Error  string
}

// Runner executes cases with a provider supplied by the caller. Production
// evaluation must use the deterministic fake provider; no network provider is
// accepted by this package.
type Runner struct {
	store      Store
	provider   ai.ModelProvider
	validators map[string]func([]byte) error
	now        func() time.Time
}

func NewRunner(store Store, provider ai.ModelProvider) *Runner {
	if provider == nil {
		provider = ai.DeterministicFakeProvider{}
	}
	return &Runner{store: store, provider: provider, validators: make(map[string]func([]byte) error), now: time.Now}
}

func (r *Runner) SetValidator(promptVersionID string, validator func([]byte) error) {
	r.validators[promptVersionID] = validator
}

func (r *Runner) Run(ctx context.Context, p domain.Principal, promptVersionID, datasetID string) (Result, []CaseResult, error) {
	pv, err := r.store.GetPromptVersion(ctx, p, promptVersionID)
	if err != nil {
		return Result{}, nil, err
	}
	if pv.Provider != "fake" {
		return Result{}, nil, fmt.Errorf("offline eval requires fake provider, got %q", pv.Provider)
	}
	cases, err := r.store.ListEvalCases(ctx, p, datasetID)
	if err != nil {
		return Result{}, nil, err
	}

	results := make([]CaseResult, 0, len(cases))
	passed, failed := 0, 0
	for _, evalCase := range cases {
		caseResult := r.runCase(ctx, pv, evalCase)
		results = append(results, caseResult)
		if caseResult.Passed {
			passed++
		} else {
			failed++
		}
	}
	verdict := "passed"
	if failed > 0 {
		verdict = "failed"
	}
	run, err := r.store.CreateEvalRun(ctx, p, domain.EvalRun{
		PromptVersionID: promptVersionID, TenantID: p.TenantID, DatasetID: datasetID,
		Passed: passed, Failed: failed, Verdict: verdict,
	})
	if err != nil {
		return Result{}, results, err
	}
	if err := r.store.SetPromptVersionEvalStatus(ctx, p, promptVersionID, verdict); err != nil {
		return Result{}, results, err
	}
	return Result{Run: run, Passed: passed, Failed: failed}, results, nil
}

func (r *Runner) runCase(ctx context.Context, pv domain.PromptVersion, evalCase domain.EvalCase) CaseResult {
	result := CaseResult{CaseID: evalCase.ID}
	var expectations struct {
		MustPassSchema     bool     `json:"must_pass_schema"`
		ForbiddenFields    []string `json:"forbidden_fields"`
		UnauthorizedFields []string `json:"unauthorized_fields"`
		ForbiddenTerms     []string `json:"forbidden_terms"`
		RequiredFields     []string `json:"required_fields"`
		MaxLatencyMS       int64    `json:"max_latency_ms"`
		MaxCostCents       int64    `json:"max_cost_cents"`
	}
	if err := json.Unmarshal(evalCase.Expectations, &expectations); err != nil {
		return fail(result, fmt.Errorf("invalid expectations: %w", err))
	}
	if len(pv.InputSchema) > 0 {
		if err := schemas.Validate(pv.InputSchema, evalCase.Input); err != nil {
			return fail(result, fmt.Errorf("input schema: %w", err))
		}
	}
	start := r.now()
	response, err := r.provider.Generate(ctx, ai.GenerateRequest{
		Model: pv.Model, Prompt: pv.Template + "\n\n<EVAL_INPUT>\n" + string(evalCase.Input) + "\n</EVAL_INPUT>",
		OutputSchema: pv.OutputSchema,
	})
	if err != nil {
		return fail(result, err)
	}
	latency := r.now().Sub(start).Milliseconds()
	if expectations.MaxLatencyMS > 0 && latency > expectations.MaxLatencyMS {
		return fail(result, fmt.Errorf("latency %dms exceeds %dms", latency, expectations.MaxLatencyMS))
	}
	if expectations.MaxCostCents > 0 && response.Usage.CostCents > expectations.MaxCostCents {
		return fail(result, fmt.Errorf("cost %d cents exceeds %d cents", response.Usage.CostCents, expectations.MaxCostCents))
	}
	if expectations.MustPassSchema || len(pv.OutputSchema) > 0 {
		if err := schemas.Validate(pv.OutputSchema, []byte(response.Content)); err != nil {
			return fail(result, fmt.Errorf("output schema: %w", err))
		}
	}
	if validator := r.validators[pv.ID]; validator != nil {
		if err := validator([]byte(response.Content)); err != nil {
			return fail(result, fmt.Errorf("domain validator: %w", err))
		}
	}
	var output any
	if err := json.Unmarshal([]byte(response.Content), &output); err != nil {
		return fail(result, fmt.Errorf("output JSON: %w", err))
	}
	for _, field := range append(expectations.ForbiddenFields, expectations.UnauthorizedFields...) {
		if jsonPathExists(output, field) {
			return fail(result, fmt.Errorf("forbidden field %q present", field))
		}
	}
	for _, field := range expectations.RequiredFields {
		if !jsonPathExists(output, field) {
			return fail(result, fmt.Errorf("required field %q missing", field))
		}
	}
	serialized := strings.ToLower(response.Content)
	for _, term := range expectations.ForbiddenTerms {
		if strings.Contains(serialized, strings.ToLower(term)) {
			return fail(result, fmt.Errorf("forbidden term %q present", term))
		}
	}
	result.Passed = true
	return result
}

func fail(result CaseResult, err error) CaseResult {
	result.Error = err.Error()
	return result
}

func jsonPathExists(value any, path string) bool {
	current := value
	for _, part := range strings.Split(path, ".") {
		object, ok := current.(map[string]any)
		if !ok {
			return false
		}
		current, ok = object[part]
		if !ok {
			return false
		}
	}
	return true
}
