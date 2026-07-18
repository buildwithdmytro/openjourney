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
	getConfigFunc     func(ctx context.Context, p domain.Principal) (domain.AIProviderConfig, error)
	getUsageFunc      func(ctx context.Context, tenantID, workspaceID string, period string) (domain.AIBudgetUsage, error)
	incrementUsageFunc func(ctx context.Context, tenantID, workspaceID string, period string, costCents, inputTokens, outputTokens int64) error
}

func (m *mockStore) GetDefaultAIProviderConfig(ctx context.Context, p domain.Principal) (domain.AIProviderConfig, error) {
	return m.getConfigFunc(ctx, p)
}

func (m *mockStore) GetAIBudgetUsage(ctx context.Context, tenantID, workspaceID string, period string) (domain.AIBudgetUsage, error) {
	return m.getUsageFunc(ctx, tenantID, workspaceID, period)
}

func (m *mockStore) IncrementAIBudgetUsage(ctx context.Context, tenantID, workspaceID string, period string, costCents, inputTokens, outputTokens int64) error {
	return m.incrementUsageFunc(ctx, tenantID, workspaceID, period, costCents, inputTokens, outputTokens)
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
