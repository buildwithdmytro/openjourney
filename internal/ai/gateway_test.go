package ai

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/buildwithdmytro/openjourney/internal/domain"
	"github.com/buildwithdmytro/openjourney/internal/ports"
)

type mockStore struct {
	ports.Store
	getConfigFunc        func(ctx context.Context, p domain.Principal) (domain.AIProviderConfig, error)
	getPromptVersionFunc func(ctx context.Context, p domain.Principal, id string) (domain.PromptVersion, error)
	getUsageFunc         func(ctx context.Context, tenantID, workspaceID string, period string) (domain.AIBudgetUsage, error)
	incrementUsageFunc   func(ctx context.Context, tenantID, workspaceID string, period string, costCents, inputTokens, outputTokens int64) error
	recordActivityFunc   func(context.Context, domain.Principal, domain.AIActivity) (domain.AIActivity, error)
}

func (m *mockStore) GetDefaultAIProviderConfig(ctx context.Context, p domain.Principal) (domain.AIProviderConfig, error) {
	return m.getConfigFunc(ctx, p)
}

func (m *mockStore) GetPromptVersion(ctx context.Context, p domain.Principal, id string) (domain.PromptVersion, error) {
	if m.getPromptVersionFunc != nil {
		return m.getPromptVersionFunc(ctx, p, id)
	}
	return domain.PromptVersion{ID: id, Status: "active", EvalStatus: "passed", Provider: "fake", Model: "fake-model"}, nil
}

func (m *mockStore) GetAIBudgetUsage(ctx context.Context, tenantID, workspaceID string, period string) (domain.AIBudgetUsage, error) {
	return m.getUsageFunc(ctx, tenantID, workspaceID, period)
}

func (m *mockStore) IncrementAIBudgetUsage(ctx context.Context, tenantID, workspaceID string, period string, costCents, inputTokens, outputTokens int64) error {
	return m.incrementUsageFunc(ctx, tenantID, workspaceID, period, costCents, inputTokens, outputTokens)
}

func (m *mockStore) RecordAIActivity(ctx context.Context, p domain.Principal, activity domain.AIActivity) (domain.AIActivity, error) {
	if m.recordActivityFunc == nil {
		return activity, nil
	}
	return m.recordActivityFunc(ctx, p, activity)
}

func TestGatewayRecordsExactlyOneActivityPerInvoke(t *testing.T) {
	principal := domain.Principal{TenantID: "tenant-1", WorkspaceID: "workspace-1", UserID: "user-1", ActorType: "user"}
	var activities []domain.AIActivity
	store := &mockStore{
		getConfigFunc: func(context.Context, domain.Principal) (domain.AIProviderConfig, error) {
			return domain.AIProviderConfig{Provider: "fake", Status: "active", Config: json.RawMessage(`{}`)}, nil
		},
		getUsageFunc: func(context.Context, string, string, string) (domain.AIBudgetUsage, error) {
			return domain.AIBudgetUsage{}, nil
		},
		incrementUsageFunc: func(context.Context, string, string, string, int64, int64, int64) error { return nil },
		recordActivityFunc: func(_ context.Context, _ domain.Principal, activity domain.AIActivity) (domain.AIActivity, error) {
			activities = append(activities, activity)
			return activity, nil
		},
	}
	g := NewGateway(store)
	g.newProvider = func(ProviderProfile) ModelProvider {
		return &mockModelProvider{generateFunc: func(context.Context, GenerateRequest) (*GenerateResponse, error) {
			return &GenerateResponse{Content: "ok", Usage: Usage{InputTokens: 2, OutputTokens: 3, CostCents: 4}}, nil
		}}
	}
	if _, err := g.Generate(context.Background(), principal, GenerateRequest{PromptVersionID: "prompt-version-1", Action: "ai.content_draft"}); err != nil {
		t.Fatalf("allowed invoke: %v", err)
	}
	if len(activities) != 1 || activities[0].PolicyDecision != "allowed" {
		t.Fatalf("expected one allowed activity, got %#v", activities)
	}
	if activities[0].InputTokens != 2 || activities[0].OutputTokens != 3 || activities[0].CostCents != 4 {
		t.Fatalf("usage was not recorded: %#v", activities[0])
	}

	store.getConfigFunc = func(context.Context, domain.Principal) (domain.AIProviderConfig, error) {
		return domain.AIProviderConfig{Provider: "fake", Status: "active", MonthlyBudgetCents: 1, Config: json.RawMessage(`{}`)}, nil
	}
	store.getUsageFunc = func(context.Context, string, string, string) (domain.AIBudgetUsage, error) {
		return domain.AIBudgetUsage{CostCents: 1}, nil
	}
	if _, err := g.Generate(context.Background(), principal, GenerateRequest{Action: "ai.content_draft"}); !errors.Is(err, ErrBudgetExceeded) {
		t.Fatalf("expected budget denial, got %v", err)
	}
	if len(activities) != 2 || activities[1].PolicyDecision != "denied_budget" {
		t.Fatalf("expected one denied_budget activity, got %#v", activities)
	}
}

func TestGatewayEvalGateRejectsUnevaluatedPromptVersions(t *testing.T) {
	principal := domain.Principal{TenantID: "tenant-1", WorkspaceID: "workspace-1", UserID: "user-1", ActorType: "user"}
	var activities []domain.AIActivity
	status := "pending"
	store := &mockStore{
		getPromptVersionFunc: func(context.Context, domain.Principal, string) (domain.PromptVersion, error) {
			return domain.PromptVersion{ID: "version-1", Status: "active", EvalStatus: status, Provider: "fake", Model: "fake-model"}, nil
		},
		recordActivityFunc: func(_ context.Context, _ domain.Principal, activity domain.AIActivity) (domain.AIActivity, error) {
			activities = append(activities, activity)
			return activity, nil
		},
	}
	g := NewGateway(store)
	providerCalled := false
	g.newProvider = func(ProviderProfile) ModelProvider {
		return &mockModelProvider{generateFunc: func(context.Context, GenerateRequest) (*GenerateResponse, error) {
			providerCalled = true
			return &GenerateResponse{Content: `{"ok":true}`}, nil
		}}
	}

	for _, evalStatus := range []string{"pending", "failed"} {
		status = evalStatus
		_, err := g.Generate(context.Background(), principal, GenerateRequest{PromptVersionID: "version-1", Action: "ai.eval_gate"})
		if !errors.Is(err, ErrPromptVersionNotUsable) {
			t.Fatalf("eval_status=%q: expected unusable prompt rejection, got %v", evalStatus, err)
		}
		if len(activities) != 1 || activities[len(activities)-1].PolicyDecision != "denied_policy" {
			t.Fatalf("eval_status=%q: expected one denied activity, got %#v", evalStatus, activities)
		}
		activities = activities[:0]
	}
	if providerCalled {
		t.Fatal("provider was called for an unevaluated prompt version")
	}
}

type mockModelProvider struct {
	generateFunc func(ctx context.Context, req GenerateRequest) (*GenerateResponse, error)
	embedFunc    func(ctx context.Context, req EmbedRequest) (*EmbedResponse, error)
	moderateFunc func(ctx context.Context, req ModerateRequest) (*ModerateResponse, error)
}

func (m *mockModelProvider) Generate(ctx context.Context, req GenerateRequest) (*GenerateResponse, error) {
	return m.generateFunc(ctx, req)
}

func (m *mockModelProvider) Embed(ctx context.Context, req EmbedRequest) (*EmbedResponse, error) {
	return m.embedFunc(ctx, req)
}

func (m *mockModelProvider) Moderate(ctx context.Context, req ModerateRequest) (*ModerateResponse, error) {
	return m.moderateFunc(ctx, req)
}

func TestGatewayBudgetEnforcement(t *testing.T) {
	principal := domain.Principal{
		TenantID:    "tenant-1",
		WorkspaceID: "workspace-1",
	}

	t.Run("UnderBudgetSucceedsAndIncrements", func(t *testing.T) {
		var incremented bool
		var incCost, incInput, incOutput int64

		store := &mockStore{
			getConfigFunc: func(ctx context.Context, p domain.Principal) (domain.AIProviderConfig, error) {
				return domain.AIProviderConfig{
					Provider:           "fake",
					Status:             "active",
					MonthlyBudgetCents: 100,
					Config:             json.RawMessage(`{}`),
				}, nil
			},
			getUsageFunc: func(ctx context.Context, tenantID, workspaceID string, period string) (domain.AIBudgetUsage, error) {
				return domain.AIBudgetUsage{
					CostCents: 50,
				}, nil
			},
			incrementUsageFunc: func(ctx context.Context, tenantID, workspaceID string, period string, costCents, inputTokens, outputTokens int64) error {
				incremented = true
				incCost = costCents
				incInput = inputTokens
				incOutput = outputTokens
				return nil
			},
		}

		g := NewGateway(store)
		g.newProvider = func(profile ProviderProfile) ModelProvider {
			return &mockModelProvider{
				generateFunc: func(ctx context.Context, req GenerateRequest) (*GenerateResponse, error) {
					return &GenerateResponse{
						Content: "ok",
						Usage: Usage{
							InputTokens:  10,
							OutputTokens: 20,
							CostCents:    10,
						},
					}, nil
				},
			}
		}

		resp, err := g.Generate(context.Background(), principal, GenerateRequest{})
		if err != nil {
			t.Fatalf("expected success, got %v", err)
		}
		if resp.Content != "ok" {
			t.Errorf("expected response content 'ok', got %q", resp.Content)
		}

		if !incremented {
			t.Errorf("expected budget usage to be incremented")
		}
		if incCost != 10 || incInput != 10 || incOutput != 20 {
			t.Errorf("unexpected incremented values: cost=%d, input=%d, output=%d", incCost, incInput, incOutput)
		}
	})

	t.Run("OverBudgetIsDenied", func(t *testing.T) {
		store := &mockStore{
			getConfigFunc: func(ctx context.Context, p domain.Principal) (domain.AIProviderConfig, error) {
				return domain.AIProviderConfig{
					Provider:           "fake",
					Status:             "active",
					MonthlyBudgetCents: 100,
					Config:             json.RawMessage(`{}`),
				}, nil
			},
			getUsageFunc: func(ctx context.Context, tenantID, workspaceID string, period string) (domain.AIBudgetUsage, error) {
				return domain.AIBudgetUsage{
					CostCents: 100, // already reached the budget limit
				}, nil
			},
		}

		g := NewGateway(store)
		// We set newProvider to return a provider that should NOT be called.
		g.newProvider = func(profile ProviderProfile) ModelProvider {
			return &mockModelProvider{
				generateFunc: func(ctx context.Context, req GenerateRequest) (*GenerateResponse, error) {
					t.Fatal("provider should not have been called when over budget")
					return nil, nil
				},
			}
		}

		_, err := g.Generate(context.Background(), principal, GenerateRequest{})
		if !errors.Is(err, ErrBudgetExceeded) {
			t.Errorf("expected ErrBudgetExceeded, got %v", err)
		}
	})

	t.Run("NoLimitBudgetSucceeds", func(t *testing.T) {
		var incremented bool
		store := &mockStore{
			getConfigFunc: func(ctx context.Context, p domain.Principal) (domain.AIProviderConfig, error) {
				return domain.AIProviderConfig{
					Provider:           "fake",
					Status:             "active",
					MonthlyBudgetCents: 0, // 0 = unlimited
					Config:             json.RawMessage(`{}`),
				}, nil
			},
			getUsageFunc: func(ctx context.Context, tenantID, workspaceID string, period string) (domain.AIBudgetUsage, error) {
				// Even if usage cost is high, limit is 0 (unlimited), so it shouldn't query or deny
				t.Fatal("should not call getUsage when monthly_budget_cents is 0")
				return domain.AIBudgetUsage{}, nil
			},
			incrementUsageFunc: func(ctx context.Context, tenantID, workspaceID string, period string, costCents, inputTokens, outputTokens int64) error {
				incremented = true
				return nil
			},
		}

		g := NewGateway(store)
		g.newProvider = func(profile ProviderProfile) ModelProvider {
			return &mockModelProvider{
				generateFunc: func(ctx context.Context, req GenerateRequest) (*GenerateResponse, error) {
					return &GenerateResponse{
						Content: "unlimited-ok",
						Usage: Usage{
							InputTokens:  5,
							OutputTokens: 5,
							CostCents:    2,
						},
					}, nil
				},
			}
		}

		resp, err := g.Generate(context.Background(), principal, GenerateRequest{})
		if err != nil {
			t.Fatalf("expected success, got %v", err)
		}
		if resp.Content != "unlimited-ok" {
			t.Errorf("expected 'unlimited-ok', got %q", resp.Content)
		}
		if !incremented {
			t.Errorf("expected budget usage to still increment")
		}
	})
}

func TestGatewayEgressPropagation(t *testing.T) {
	principal := domain.Principal{
		TenantID:    "tenant-1",
		WorkspaceID: "workspace-1",
	}

	store := &mockStore{
		getConfigFunc: func(ctx context.Context, p domain.Principal) (domain.AIProviderConfig, error) {
			return domain.AIProviderConfig{
				Provider:           "fake",
				Status:             "active",
				EndpointAllowlist:  []string{"127.0.0.1:11434", "custom-model.local"},
				MonthlyBudgetCents: 0,
				Config:             json.RawMessage(`{}`),
			}, nil
		},
		incrementUsageFunc: func(ctx context.Context, tenantID, workspaceID string, period string, costCents, inputTokens, outputTokens int64) error {
			return nil
		},
	}

	g := NewGateway(store)
	var capturedAllowlist []string
	g.newProvider = func(profile ProviderProfile) ModelProvider {
		return &mockModelProvider{
			generateFunc: func(ctx context.Context, req GenerateRequest) (*GenerateResponse, error) {
				capturedAllowlist = GetEndpointAllowlist(ctx)
				return &GenerateResponse{
					Content: "ok",
					Usage: Usage{
						InputTokens:  1,
						OutputTokens: 1,
						CostCents:    1,
					},
				}, nil
			},
		}
	}

	_, err := g.Generate(context.Background(), principal, GenerateRequest{})
	if err != nil {
		t.Fatalf("expected success, got %v", err)
	}

	if len(capturedAllowlist) != 2 || capturedAllowlist[0] != "127.0.0.1:11434" || capturedAllowlist[1] != "custom-model.local" {
		t.Errorf("expected allowlist [127.0.0.1:11434, custom-model.local], got %v", capturedAllowlist)
	}
}

func TestGatewayValidationAndRepair(t *testing.T) {
	principal := domain.Principal{
		TenantID:    "tenant-1",
		WorkspaceID: "workspace-1",
	}

	outputSchema := json.RawMessage(`{
		"type": "object",
		"properties": {
			"name": {"type": "string"},
			"age": {"type": "integer"}
		},
		"required": ["name", "age"]
	}`)

	domainValidator := func(content []byte) error {
		var val struct {
			Age int `json:"age"`
		}
		if err := json.Unmarshal(content, &val); err != nil {
			return err
		}
		if val.Age < 18 {
			return errors.New("must be at least 18")
		}
		return nil
	}

	t.Run("PassesImmediately", func(t *testing.T) {
		var callCount int
		store := &mockStore{
			getConfigFunc: func(ctx context.Context, p domain.Principal) (domain.AIProviderConfig, error) {
				return domain.AIProviderConfig{
					Provider: "fake",
					Status:   "active",
					Config:   json.RawMessage(`{}`),
				}, nil
			},
			incrementUsageFunc: func(ctx context.Context, tenantID, workspaceID string, period string, costCents, inputTokens, outputTokens int64) error {
				return nil
			},
		}

		g := NewGateway(store)
		g.newProvider = func(profile ProviderProfile) ModelProvider {
			return &mockModelProvider{
				generateFunc: func(ctx context.Context, req GenerateRequest) (*GenerateResponse, error) {
					callCount++
					return &GenerateResponse{
						Content: `{"name": "Alice", "age": 30}`,
						Usage:   Usage{CostCents: 5},
					}, nil
				},
			}
		}

		resp, err := g.Generate(context.Background(), principal, GenerateRequest{
			OutputSchema:    outputSchema,
			DomainValidator: domainValidator,
		})
		if err != nil {
			t.Fatalf("expected success, got %v", err)
		}
		if resp.Content != `{"name": "Alice", "age": 30}` {
			t.Errorf("unexpected content: %s", resp.Content)
		}
		if callCount != 1 {
			t.Errorf("expected 1 call, got %d", callCount)
		}
	})

	t.Run("FailsFirstRepairsSecond", func(t *testing.T) {
		var callCount int
		var promptUsedInRepair string
		var incrementCount int

		store := &mockStore{
			getConfigFunc: func(ctx context.Context, p domain.Principal) (domain.AIProviderConfig, error) {
				return domain.AIProviderConfig{
					Provider: "fake",
					Status:   "active",
					Config:   json.RawMessage(`{}`),
				}, nil
			},
			incrementUsageFunc: func(ctx context.Context, tenantID, workspaceID string, period string, costCents, inputTokens, outputTokens int64) error {
				incrementCount++
				return nil
			},
		}

		g := NewGateway(store)
		g.newProvider = func(profile ProviderProfile) ModelProvider {
			return &mockModelProvider{
				generateFunc: func(ctx context.Context, req GenerateRequest) (*GenerateResponse, error) {
					callCount++
					if callCount == 1 {
						// Malformed output: missing "age"
						return &GenerateResponse{
							Content: `{"name": "Alice"}`,
							Usage:   Usage{CostCents: 5},
						}, nil
					}
					promptUsedInRepair = req.Prompt
					// Corrected output
					return &GenerateResponse{
						Content: `{"name": "Alice", "age": 30}`,
						Usage:   Usage{CostCents: 5},
					}, nil
				},
			}
		}

		resp, err := g.Generate(context.Background(), principal, GenerateRequest{
			Prompt:          "create user",
			OutputSchema:    outputSchema,
			DomainValidator: domainValidator,
		})
		if err != nil {
			t.Fatalf("expected success on repair, got %v", err)
		}
		if resp.Content != `{"name": "Alice", "age": 30}` {
			t.Errorf("unexpected content: %s", resp.Content)
		}
		if callCount != 2 {
			t.Errorf("expected 2 calls (first failed, second repaired), got %d", callCount)
		}
		if incrementCount != 2 {
			t.Errorf("expected 2 budget increments, got %d", incrementCount)
		}
		if !strings.Contains(promptUsedInRepair, "validation failed") {
			t.Errorf("expected repair prompt to include error details, got: %s", promptUsedInRepair)
		}
	})

	t.Run("FailsBothCalls", func(t *testing.T) {
		var callCount int
		store := &mockStore{
			getConfigFunc: func(ctx context.Context, p domain.Principal) (domain.AIProviderConfig, error) {
				return domain.AIProviderConfig{
					Provider: "fake",
					Status:   "active",
					Config:   json.RawMessage(`{}`),
				}, nil
			},
			incrementUsageFunc: func(ctx context.Context, tenantID, workspaceID string, period string, costCents, inputTokens, outputTokens int64) error {
				return nil
			},
		}

		g := NewGateway(store)
		g.newProvider = func(profile ProviderProfile) ModelProvider {
			return &mockModelProvider{
				generateFunc: func(ctx context.Context, req GenerateRequest) (*GenerateResponse, error) {
					callCount++
					// Both outputs fail domain validation (age < 18)
					return &GenerateResponse{
						Content: `{"name": "Alice", "age": 15}`,
						Usage:   Usage{CostCents: 5},
					}, nil
				},
			}
		}

		_, err := g.Generate(context.Background(), principal, GenerateRequest{
			OutputSchema:    outputSchema,
			DomainValidator: domainValidator,
		})
		if err == nil {
			t.Fatal("expected validation error, got nil")
		}
		if !strings.Contains(err.Error(), "repaired output also failed validation") {
			t.Errorf("unexpected error: %v", err)
		}
		if callCount != 2 {
			t.Errorf("expected 2 calls, got %d", callCount)
		}
	})
}

func TestGatewayBudgetIncrementFailureDoesNotBlockAllowedActivity(t *testing.T) {
	principal := domain.Principal{TenantID: "tenant-1", WorkspaceID: "workspace-1", UserID: "user-1", ActorType: "user"}
	var activities []domain.AIActivity
	store := &mockStore{
		getConfigFunc: func(context.Context, domain.Principal) (domain.AIProviderConfig, error) {
			return domain.AIProviderConfig{Provider: "fake", Status: "active", Config: json.RawMessage(`{}`)}, nil
		},
		getUsageFunc: func(context.Context, string, string, string) (domain.AIBudgetUsage, error) {
			return domain.AIBudgetUsage{}, nil
		},
		incrementUsageFunc: func(context.Context, string, string, string, int64, int64, int64) error {
			return errors.New("database increment error")
		},
		recordActivityFunc: func(_ context.Context, _ domain.Principal, activity domain.AIActivity) (domain.AIActivity, error) {
			activities = append(activities, activity)
			return activity, nil
		},
	}
	g := NewGateway(store)
	g.newProvider = func(ProviderProfile) ModelProvider {
		return &mockModelProvider{generateFunc: func(context.Context, GenerateRequest) (*GenerateResponse, error) {
			return &GenerateResponse{Content: "ok", Usage: Usage{InputTokens: 2, OutputTokens: 3, CostCents: 4}}, nil
		}}
	}
	resp, err := g.Generate(context.Background(), principal, GenerateRequest{PromptVersionID: "prompt-version-1", Action: "ai.content_draft"})
	if err != nil {
		t.Fatalf("expected success even if budget increment fails, got: %v", err)
	}
	if len(activities) != 1 || activities[0].PolicyDecision != "allowed" {
		t.Fatalf("expected exactly one allowed activity, got %#v", activities)
	}
	if resp.ActivityID != activities[0].ID {
		t.Errorf("expected activity ID to be populated in response")
	}
}

