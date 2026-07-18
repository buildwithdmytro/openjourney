package scoring

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/buildwithdmytro/openjourney/internal/ai"
	"github.com/buildwithdmytro/openjourney/internal/domain"
	"github.com/buildwithdmytro/openjourney/internal/ports"
)

type mockLLMStore struct {
	ports.Store
	model          domain.ScoringModel
	scoringVersion domain.ScoringModelVersion
	promptVersion  domain.PromptVersion
	cases          []domain.EvalCase
	evalStatus     string
	providerConfig domain.AIProviderConfig
	budgetUsage    domain.AIBudgetUsage
}

func (m *mockLLMStore) GetScoringModel(ctx context.Context, p domain.Principal, id string) (domain.ScoringModel, error) {
	return m.model, nil
}

func (m *mockLLMStore) GetScoringModelVersion(ctx context.Context, p domain.Principal, id string) (domain.ScoringModelVersion, error) {
	return m.scoringVersion, nil
}

func (m *mockLLMStore) GetPromptVersion(ctx context.Context, p domain.Principal, id string) (domain.PromptVersion, error) {
	return m.promptVersion, nil
}

func (m *mockLLMStore) ListEvalCases(ctx context.Context, p domain.Principal, datasetID string) ([]domain.EvalCase, error) {
	return m.cases, nil
}

func (m *mockLLMStore) SetScoringModelVersionEvalStatus(ctx context.Context, p domain.Principal, id string, evalStatus string) error {
	m.evalStatus = evalStatus
	return nil
}

func (m *mockLLMStore) GetDefaultAIProviderConfig(ctx context.Context, p domain.Principal) (domain.AIProviderConfig, error) {
	return m.providerConfig, nil
}

func (m *mockLLMStore) GetAIBudgetUsage(ctx context.Context, tenantID, workspaceID string, period string) (domain.AIBudgetUsage, error) {
	return m.budgetUsage, nil
}

func (m *mockLLMStore) IncrementAIBudgetUsage(ctx context.Context, tenantID, workspaceID string, period string, costCents, inputTokens, outputTokens int64) error {
	return nil
}

func (m *mockLLMStore) RecordAIActivity(ctx context.Context, p domain.Principal, activity domain.AIActivity) (domain.AIActivity, error) {
	return activity, nil
}

type testModelProvider struct {
	content string
	err     error
}

func (t *testModelProvider) Generate(ctx context.Context, req ai.GenerateRequest) (*ai.GenerateResponse, error) {
	if t.err != nil {
		return nil, t.err
	}
	return &ai.GenerateResponse{
		Content: t.content,
		Usage:   ai.Usage{InputTokens: 5, OutputTokens: 5, CostCents: 1},
	}, nil
}

func (t *testModelProvider) Embed(ctx context.Context, req ai.EmbedRequest) (*ai.EmbedResponse, error) {
	return nil, nil
}

func (t *testModelProvider) Moderate(ctx context.Context, req ai.ModerateRequest) (*ai.ModerateResponse, error) {
	return nil, nil
}

func TestEvaluateLLMModel_AllPassed(t *testing.T) {
	store := &mockLLMStore{
		model: domain.ScoringModel{
			ID:   "model-1",
			Kind: "llm",
		},
		scoringVersion: domain.ScoringModelVersion{
			ID:             "sv-1",
			ScoringModelID: "model-1",
			Definition:     json.RawMessage(`{"prompt_version_id": "pv-1"}`),
			OutputMin:      0.0,
			OutputMax:      10.0,
		},
		promptVersion: domain.PromptVersion{
			ID:         "pv-1",
			Status:     "active",
			EvalStatus: "passed",
			Model:      "fake-model",
		},
		cases: []domain.EvalCase{
			{
				ID:           "case-1",
				Input:        json.RawMessage(`{"profile": {"age": 20}}`),
				Expectations: json.RawMessage(`{"expected_score": 8.5}`),
			},
			{
				ID:           "case-2",
				Input:        json.RawMessage(`{"profile": {"age": 16}}`),
				Expectations: json.RawMessage(`{"expected_value": 4.2}`),
			},
		},
		providerConfig: domain.AIProviderConfig{
			Provider: "fake",
			Status:   "active",
			Config:   json.RawMessage(`{}`),
		},
	}

	gateway := ai.NewGateway(store)
	var contentToReturn string
	gateway.SetProviderFactory(func(ai.ProviderProfile) ai.ModelProvider {
		return &testModelProvider{
			content: contentToReturn,
		}
	})

	p := domain.Principal{TenantID: "tenant-1", WorkspaceID: "workspace-1"}

	// Test 1: case-1 returns 8.5, case-2 returns 4.2 -> Passed
	contentToReturn = `{"score": 8.5}`
	store.cases = store.cases[:1] // run case-1 first
	res, caseResults, err := EvaluateLLMModel(context.Background(), store, gateway, p, "sv-1", "ds-1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if res.Verdict != "passed" || res.Passed != 1 || res.Failed != 0 {
		t.Errorf("expected passed, got %+v", res)
	}
	if store.evalStatus != "passed" {
		t.Errorf("expected evalStatus to be updated to passed, got %s", store.evalStatus)
	}
	if !caseResults[0].Passed {
		t.Errorf("case-1 failed: %s", caseResults[0].Error)
	}
}

func TestEvaluateLLMModel_InvalidKindAndUnevaluated(t *testing.T) {
	p := domain.Principal{TenantID: "tenant-1", WorkspaceID: "workspace-1"}

	// 1. Invalid kind
	store := &mockLLMStore{
		model: domain.ScoringModel{
			ID:   "model-1",
			Kind: "expression",
		},
		scoringVersion: domain.ScoringModelVersion{
			ID:             "sv-1",
			ScoringModelID: "model-1",
		},
	}
	gateway := ai.NewGateway(store)
	_, _, err := EvaluateLLMModel(context.Background(), store, gateway, p, "sv-1", "ds-1")
	if err == nil || !strings.Contains(err.Error(), "only supports llm scoring models") {
		t.Errorf("expected error for invalid model kind, got %v", err)
	}

	// 2. Unevaluated prompt version
	store.model.Kind = "llm"
	store.scoringVersion.Definition = json.RawMessage(`{"prompt_version_id": "pv-1"}`)
	store.promptVersion = domain.PromptVersion{
		ID:         "pv-1",
		Status:     "draft",
		EvalStatus: "pending",
	}
	_, _, err = EvaluateLLMModel(context.Background(), store, gateway, p, "sv-1", "ds-1")
	if err == nil || !strings.Contains(err.Error(), "prompt version is not usable") {
		t.Errorf("expected error for unevaluated prompt version, got %v", err)
	}
}

func TestEvaluateLLM(t *testing.T) {
	store := &mockLLMStore{
		model: domain.ScoringModel{
			ID:   "model-1",
			Kind: "llm",
		},
		scoringVersion: domain.ScoringModelVersion{
			ID:             "sv-1",
			ScoringModelID: "model-1",
			Definition:     json.RawMessage(`{"prompt_version_id": "pv-1"}`),
			OutputMin:      0.0,
			OutputMax:      100.0,
			EvalStatus:     "passed",
		},
		promptVersion: domain.PromptVersion{
			ID:         "pv-1",
			Status:     "active",
			EvalStatus: "passed",
			Model:      "fake-model",
		},
		providerConfig: domain.AIProviderConfig{
			Provider: "fake",
			Status:   "active",
			Config:   json.RawMessage(`{}`),
		},
	}

	gateway := ai.NewGateway(store)
	var returnedContent string
	gateway.SetProviderFactory(func(ai.ProviderProfile) ai.ModelProvider {
		return &testModelProvider{
			content: returnedContent,
		}
	})

	p := domain.Principal{TenantID: "tenant-1", WorkspaceID: "workspace-1"}
	env := map[string]any{"user": "Alice"}

	// Test direct float parse
	returnedContent = "85.5"
	val, err := EvaluateLLM(context.Background(), store, gateway, p, store.scoringVersion, env)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if val != 85.5 {
		t.Errorf("expected 85.5, got %v", val)
	}

	// Test score key in object
	returnedContent = `{"score": 90.0}`
	val, err = EvaluateLLM(context.Background(), store, gateway, p, store.scoringVersion, env)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if val != 90.0 {
		t.Errorf("expected 90.0, got %v", val)
	}

	// Test value key in object
	returnedContent = `{"value": 0.5}`
	val, err = EvaluateLLM(context.Background(), store, gateway, p, store.scoringVersion, env)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if val != 0.5 {
		t.Errorf("expected 0.5, got %v", val)
	}

	// Test clamping
	returnedContent = `150.0` // Clamped to OutputMax (100)
	val, err = EvaluateLLM(context.Background(), store, gateway, p, store.scoringVersion, env)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if val != 100.0 {
		t.Errorf("expected clamped score to be 100.0, got %v", val)
	}

	// Test unevaluated scoring version refused
	store.scoringVersion.EvalStatus = "pending"
	_, err = EvaluateLLM(context.Background(), store, gateway, p, store.scoringVersion, env)
	if err == nil || !strings.Contains(err.Error(), "is not evaluated/passed") {
		t.Errorf("expected error for unevaluated scoring model version, got %v", err)
	}
}
