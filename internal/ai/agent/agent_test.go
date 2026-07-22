package agent

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/buildwithdmytro/openjourney/internal/ai"
	"github.com/buildwithdmytro/openjourney/internal/ai/tools"
	"github.com/buildwithdmytro/openjourney/internal/domain"
	"github.com/buildwithdmytro/openjourney/internal/ports"
)

type testMockStore struct {
	ports.Store
	recordedActivities []domain.AIActivity
	toolCalls          []tools.ToolCall
}

func (m *testMockStore) GetDefaultAIProviderConfig(ctx context.Context, p domain.Principal) (domain.AIProviderConfig, error) {
	return domain.AIProviderConfig{Provider: "fake", Status: "active", Config: json.RawMessage(`{}`)}, nil
}

func (m *testMockStore) GetPromptVersion(ctx context.Context, p domain.Principal, id string) (domain.PromptVersion, error) {
	return domain.PromptVersion{ID: id, Status: "active", EvalStatus: "passed", Provider: "fake", Model: "fake-model"}, nil
}

func (m *testMockStore) GetAIBudgetUsage(ctx context.Context, tenantID, workspaceID string, period string) (domain.AIBudgetUsage, error) {
	return domain.AIBudgetUsage{}, nil
}

func (m *testMockStore) IncrementAIBudgetUsage(ctx context.Context, tenantID, workspaceID string, period string, costCents, inputTokens, outputTokens int64) error {
	return nil
}

func (m *testMockStore) RecordAIActivity(ctx context.Context, p domain.Principal, activity domain.AIActivity) (domain.AIActivity, error) {
	m.recordedActivities = append(m.recordedActivities, activity)
	activity.ID = "activity-123"
	return activity, nil
}

func (m *testMockStore) RecordToolCall(ctx context.Context, call tools.ToolCall) error {
	m.toolCalls = append(m.toolCalls, call)
	return nil
}

func (m *testMockStore) ListEventSchemas(ctx context.Context, p domain.Principal) ([]domain.EventSchema, error) {
	return []domain.EventSchema{
		{ID: "schema-1", EventType: "purchase"},
	}, nil
}

func (m *testMockStore) CampaignReport(ctx context.Context, p domain.Principal, id string, q domain.ReportQuery) (domain.CampaignReport, error) {
	return domain.CampaignReport{
		CampaignID: id,
		Funnel:     domain.ReportFunnel{Sent: domain.ReportCount{Total: 1000}},
	}, nil
}

type testModelProvider struct {
	responses []string
	callIdx   int
}

func (m *testModelProvider) Generate(ctx context.Context, req ai.GenerateRequest) (*ai.GenerateResponse, error) {
	if m.callIdx >= len(m.responses) {
		return &ai.GenerateResponse{
			Content: `{"action":"final","answer":"Default answer"}`,
		}, nil
	}
	respStr := m.responses[m.callIdx]
	m.callIdx++
	return &ai.GenerateResponse{
		Content:    respStr,
		ActivityID: "activity-gen-123",
		Usage:      ai.Usage{InputTokens: 10, OutputTokens: 10, CostCents: 1},
	}, nil
}

func (m *testModelProvider) Embed(ctx context.Context, req ai.EmbedRequest) (*ai.EmbedResponse, error) {
	return &ai.EmbedResponse{}, nil
}

func (m *testModelProvider) Moderate(ctx context.Context, req ai.ModerateRequest) (*ai.ModerateResponse, error) {
	return &ai.ModerateResponse{}, nil
}

func setupTestAgent(t *testing.T, responses []string) (*Agent, *testMockStore, domain.Principal) {
	store := &testMockStore{}
	g := ai.NewGateway(store)
	provider := &testModelProvider{responses: responses}
	g.SetProviderFactory(func(ai.ProviderProfile) ai.ModelProvider {
		return provider
	})

	runner := tools.NewRunner(store, store)
	readTools := tools.ReadOnlyTools()
	for _, tool := range readTools {
		if err := runner.Register(tool); err != nil {
			t.Fatalf("failed to register tool %s: %v", tool.Definition().Name, err)
		}
	}

	principal := domain.Principal{
		TenantID:    "tenant-1",
		WorkspaceID: "workspace-1",
		UserID:      "user-1",
		ActorType:   "user",
		Scopes:      []string{"schemas:read", "reports:read", "ai:invoke"},
	}

	agent := NewAgent(g, runner, readTools, Config{MaxSteps: 5})
	return agent, store, principal
}

func TestAgentAnswersQuestionWithToolsAndTerminates(t *testing.T) {
	responses := []string{
		`{"action":"tool","tool":"report.read","args":{"report_type":"campaign","resource_id":"camp-1"}}`,
		`{"action":"final","answer":"The campaign camp-1 delivered 1000 messages."}`,
	}
	agent, store, principal := setupTestAgent(t, responses)

	result, err := agent.Run(context.Background(), principal, "How many messages did campaign camp-1 deliver?")
	if err != nil {
		t.Fatalf("agent.Run failed: %v", err)
	}

	if result.Status != "completed" {
		t.Errorf("expected status 'completed', got %q", result.Status)
	}
	if result.Answer != "The campaign camp-1 delivered 1000 messages." {
		t.Errorf("unexpected answer: %q", result.Answer)
	}
	if len(result.Trace) != 2 {
		t.Fatalf("expected 2 trace steps, got %d", len(result.Trace))
	}
	if result.Trace[0].Action != "tool" || result.Trace[0].Tool != "report.read" {
		t.Errorf("unexpected step 0 trace: %#v", result.Trace[0])
	}
	if result.Trace[1].Action != "final" {
		t.Errorf("unexpected step 1 trace: %#v", result.Trace[1])
	}

	// Verify tool execution was audited
	if len(store.toolCalls) != 1 {
		t.Fatalf("expected 1 tool call audit, got %d", len(store.toolCalls))
	}
	if store.toolCalls[0].PolicyDecision != "allowed" {
		t.Errorf("expected allowed tool decision, got %q", store.toolCalls[0].PolicyDecision)
	}
}

func TestAgentNeverExceedsMaxSteps(t *testing.T) {
	responses := []string{
		`{"action":"tool","tool":"schema.inspect","args":{}}`,
		`{"action":"tool","tool":"schema.inspect","args":{}}`,
		`{"action":"tool","tool":"schema.inspect","args":{}}`,
	}
	store := &testMockStore{}
	g := ai.NewGateway(store)
	provider := &testModelProvider{responses: responses}
	g.SetProviderFactory(func(ai.ProviderProfile) ai.ModelProvider {
		return provider
	})

	runner := tools.NewRunner(store, store)
	readTools := tools.ReadOnlyTools()
	for _, tool := range readTools {
		_ = runner.Register(tool)
	}

	principal := domain.Principal{
		TenantID: "tenant-1", WorkspaceID: "workspace-1", UserID: "user-1", Scopes: []string{"schemas:read"},
	}

	agent := NewAgent(g, runner, readTools, Config{MaxSteps: 3})
	result, err := agent.Run(context.Background(), principal, "Infinite loop question?")

	if !errors.Is(err, ErrMaxStepsExceeded) {
		t.Fatalf("expected ErrMaxStepsExceeded, got %v", err)
	}
	if result.Status != "max_steps_exceeded" {
		t.Errorf("expected status 'max_steps_exceeded', got %q", result.Status)
	}
	if len(result.Trace) != 3 {
		t.Errorf("expected exactly 3 trace steps, got %d", len(result.Trace))
	}
}

func TestAgentDeniedScopeAudited(t *testing.T) {
	responses := []string{
		`{"action":"tool","tool":"report.read","args":{"report_type":"campaign","resource_id":"camp-1"}}`,
		`{"action":"final","answer":"I cannot read the report because permission was denied."}`,
	}
	store := &testMockStore{}
	g := ai.NewGateway(store)
	provider := &testModelProvider{responses: responses}
	g.SetProviderFactory(func(ai.ProviderProfile) ai.ModelProvider {
		return provider
	})

	runner := tools.NewRunner(store, store)
	readTools := tools.ReadOnlyTools()
	for _, tool := range readTools {
		_ = runner.Register(tool)
	}

	// Principal missing "reports:read" scope
	principal := domain.Principal{
		TenantID: "tenant-1", WorkspaceID: "workspace-1", UserID: "user-1", Scopes: []string{"schemas:read"},
	}

	agent := NewAgent(g, runner, readTools, Config{MaxSteps: 5})
	result, err := agent.Run(context.Background(), principal, "Get campaign report?")
	if err != nil {
		t.Fatalf("expected clean run, got %v", err)
	}

	if result.Trace[0].Error == "" {
		t.Errorf("expected tool call error in trace, got empty")
	}

	if len(store.toolCalls) != 1 {
		t.Fatalf("expected 1 audited tool call, got %d", len(store.toolCalls))
	}
	if store.toolCalls[0].PolicyDecision != "denied_scope" {
		t.Errorf("expected policy_decision 'denied_scope', got %q", store.toolCalls[0].PolicyDecision)
	}
}

func TestAgentBudgetCapTerminates(t *testing.T) {
	store := &testMockStore{}

	overriddenStore := &budgetExceededMockStore{testMockStore: store}
	gOverridden := ai.NewGateway(overriddenStore)
	gOverridden.SetProviderFactory(func(ai.ProviderProfile) ai.ModelProvider {
		return &testModelProvider{}
	})

	runner := tools.NewRunner(store, store)
	readTools := tools.ReadOnlyTools()
	agent := NewAgent(gOverridden, runner, readTools, Config{MaxSteps: 5})

	principal := domain.Principal{TenantID: "tenant-1", WorkspaceID: "workspace-1", UserID: "user-1"}
	result, err := agent.Run(context.Background(), principal, "Budget test?")

	if !errors.Is(err, ai.ErrBudgetExceeded) {
		t.Fatalf("expected ErrBudgetExceeded, got %v", err)
	}
	if result.Status != "budget_exceeded" {
		t.Errorf("expected status 'budget_exceeded', got %q", result.Status)
	}
}

type budgetExceededMockStore struct {
	*testMockStore
}

func (m *budgetExceededMockStore) GetDefaultAIProviderConfig(ctx context.Context, p domain.Principal) (domain.AIProviderConfig, error) {
	return domain.AIProviderConfig{Provider: "fake", Status: "active", MonthlyBudgetCents: 100}, nil
}

func (m *budgetExceededMockStore) GetAIBudgetUsage(ctx context.Context, tenantID, workspaceID string, period string) (domain.AIBudgetUsage, error) {
	return domain.AIBudgetUsage{CostCents: 100}, nil
}
