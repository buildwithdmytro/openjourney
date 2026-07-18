package ai

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/buildwithdmytro/openjourney/internal/domain"
	"github.com/buildwithdmytro/openjourney/internal/ports"
	"github.com/buildwithdmytro/openjourney/internal/telemetry"
	"go.opentelemetry.io/otel/attribute"
	otelmetric "go.opentelemetry.io/otel/metric"
)

var (
	ErrBudgetExceeded = errors.New("monthly AI budget exceeded")
)

type Gateway struct {
	store       ports.Store
	newProvider func(profile ProviderProfile) ModelProvider
}

func NewGateway(store ports.Store) *Gateway {
	return &Gateway{
		store: store,
	}
}

type ProviderJSONConfig struct {
	APIKeyRef    string `json:"api_key_ref"`
	BaseURL      string `json:"base_url"`
	DefaultModel string `json:"default_model"`
	CheapModel   string `json:"cheap_model"`
}

func (g *Gateway) getProvider(ctx context.Context, principal domain.Principal) (domain.AIProviderConfig, ProviderJSONConfig, ModelProvider, error) {
	cfg, err := g.store.GetDefaultAIProviderConfig(ctx, principal)
	if err != nil {
		return domain.AIProviderConfig{}, ProviderJSONConfig{}, nil, fmt.Errorf("failed to get default AI provider config: %w", err)
	}

	if cfg.Status != "active" {
		return domain.AIProviderConfig{}, ProviderJSONConfig{}, nil, fmt.Errorf("default AI provider config is disabled")
	}

	var jsonCfg ProviderJSONConfig
	if len(cfg.Config) > 0 {
		if err := json.Unmarshal(cfg.Config, &jsonCfg); err != nil {
			return domain.AIProviderConfig{}, ProviderJSONConfig{}, nil, fmt.Errorf("failed to unmarshal provider config: %w", err)
		}
	}

	apiKey, err := resolveSecret(jsonCfg.APIKeyRef)
	if err != nil {
		return domain.AIProviderConfig{}, ProviderJSONConfig{}, nil, fmt.Errorf("failed to resolve API key: %w", err)
	}

	var profile ProviderProfile
	switch cfg.Provider {
	case "fake":
		profile = NewFakeProfile()
	case "anthropic":
		profile = &AnthropicProfile{}
	case "openai":
		profile = &OpenAIProfile{}
	default:
		return domain.AIProviderConfig{}, ProviderJSONConfig{}, nil, fmt.Errorf("unknown provider: %s", cfg.Provider)
	}

	var prov ModelProvider
	if g.newProvider != nil {
		prov = g.newProvider(profile)
	} else {
		prov = NewHTTPModelProvider(profile)
	}
	_ = apiKey // APIKey is passed per-request context

	return cfg, jsonCfg, prov, nil
}

func (g *Gateway) checkBudget(ctx context.Context, principal domain.Principal, cfg domain.AIProviderConfig, period string) error {
	if cfg.MonthlyBudgetCents > 0 {
		usage, err := g.store.GetAIBudgetUsage(ctx, principal.TenantID, principal.WorkspaceID, period)
		if err != nil {
			return fmt.Errorf("failed to get AI budget usage: %w", err)
		}
		if usage.CostCents >= cfg.MonthlyBudgetCents {
			telemetry.AIBudgetExceeded.Add(ctx, 1, otelmetric.WithAttributes(
				attribute.String("tenant_id", principal.TenantID),
				attribute.String("workspace_id", principal.WorkspaceID),
			))
			telemetry.AISafetyRejections.Add(ctx, 1, otelmetric.WithAttributes(
				attribute.String("tenant_id", principal.TenantID),
				attribute.String("workspace_id", principal.WorkspaceID),
				attribute.String("reason", "budget_exceeded"),
			))
			return ErrBudgetExceeded
		}
	}
	return nil
}

func (g *Gateway) recordMetricsAndIncrementBudget(ctx context.Context, principal domain.Principal, cfg domain.AIProviderConfig, period string, usage Usage, durationMs int64, model string) error {
	attrs := otelmetric.WithAttributes(
		attribute.String("tenant_id", principal.TenantID),
		attribute.String("workspace_id", principal.WorkspaceID),
		attribute.String("provider", cfg.Provider),
		attribute.String("model", model),
	)
	telemetry.AILatency.Record(ctx, durationMs, attrs)
	telemetry.AICost.Record(ctx, usage.CostCents, attrs)
	telemetry.AIInputTokens.Add(ctx, int64(usage.InputTokens), attrs)
	telemetry.AIOutputTokens.Add(ctx, int64(usage.OutputTokens), attrs)
	telemetry.AICostTotal.Add(ctx, usage.CostCents, attrs)
	telemetry.AILatencyTotal.Add(ctx, durationMs, attrs)

	return g.store.IncrementAIBudgetUsage(ctx, principal.TenantID, principal.WorkspaceID, period, usage.CostCents, int64(usage.InputTokens), int64(usage.OutputTokens))
}

func (g *Gateway) Generate(ctx context.Context, principal domain.Principal, req GenerateRequest) (*GenerateResponse, error) {
	cfg, jsonCfg, prov, err := g.getProvider(ctx, principal)
	if err != nil {
		return nil, err
	}

	period := time.Now().UTC().Format("2006-01")
	if err := g.checkBudget(ctx, principal, cfg, period); err != nil {
		return nil, err
	}

	if req.Model == "" {
		req.Model = jsonCfg.DefaultModel
	}
	if req.APIKey == "" {
		apiKey, _ := resolveSecret(jsonCfg.APIKeyRef)
		req.APIKey = apiKey
	}
	if req.BaseURL == "" {
		req.BaseURL = jsonCfg.BaseURL
	}
	ctx = WithEndpointAllowlist(ctx, cfg.EndpointAllowlist)

	start := time.Now()
	resp, err := prov.Generate(ctx, req)
	if err != nil {
		return nil, err
	}
	duration := time.Since(start).Milliseconds()

	if err := g.recordMetricsAndIncrementBudget(ctx, principal, cfg, period, resp.Usage, duration, req.Model); err != nil {
		return nil, err
	}

	return resp, nil
}

func (g *Gateway) Embed(ctx context.Context, principal domain.Principal, req EmbedRequest) (*EmbedResponse, error) {
	cfg, jsonCfg, prov, err := g.getProvider(ctx, principal)
	if err != nil {
		return nil, err
	}

	period := time.Now().UTC().Format("2006-01")
	if err := g.checkBudget(ctx, principal, cfg, period); err != nil {
		return nil, err
	}

	if req.Model == "" {
		req.Model = jsonCfg.DefaultModel
	}
	if req.APIKey == "" {
		apiKey, _ := resolveSecret(jsonCfg.APIKeyRef)
		req.APIKey = apiKey
	}
	if req.BaseURL == "" {
		req.BaseURL = jsonCfg.BaseURL
	}
	ctx = WithEndpointAllowlist(ctx, cfg.EndpointAllowlist)

	start := time.Now()
	resp, err := prov.Embed(ctx, req)
	if err != nil {
		return nil, err
	}
	duration := time.Since(start).Milliseconds()

	if err := g.recordMetricsAndIncrementBudget(ctx, principal, cfg, period, resp.Usage, duration, req.Model); err != nil {
		return nil, err
	}

	return resp, nil
}

func (g *Gateway) Moderate(ctx context.Context, principal domain.Principal, req ModerateRequest) (*ModerateResponse, error) {
	cfg, jsonCfg, prov, err := g.getProvider(ctx, principal)
	if err != nil {
		return nil, err
	}

	period := time.Now().UTC().Format("2006-01")
	if err := g.checkBudget(ctx, principal, cfg, period); err != nil {
		return nil, err
	}

	if req.Model == "" {
		req.Model = jsonCfg.DefaultModel
	}
	if req.APIKey == "" {
		apiKey, _ := resolveSecret(jsonCfg.APIKeyRef)
		req.APIKey = apiKey
	}
	if req.BaseURL == "" {
		req.BaseURL = jsonCfg.BaseURL
	}
	ctx = WithEndpointAllowlist(ctx, cfg.EndpointAllowlist)

	start := time.Now()
	resp, err := prov.Moderate(ctx, req)
	if err != nil {
		return nil, err
	}
	duration := time.Since(start).Milliseconds()

	if resp.Flagged {
		telemetry.AISafetyRejections.Add(ctx, 1, otelmetric.WithAttributes(
			attribute.String("tenant_id", principal.TenantID),
			attribute.String("workspace_id", principal.WorkspaceID),
			attribute.String("reason", "content_moderated"),
		))
	}

	if err := g.recordMetricsAndIncrementBudget(ctx, principal, cfg, period, resp.Usage, duration, req.Model); err != nil {
		return nil, err
	}

	return resp, nil
}

func resolveSecret(ref string) (string, error) {
	if ref == "" {
		return "", nil
	}
	val := os.Getenv(ref)
	file := os.Getenv(ref + "_FILE")
	if val != "" && file != "" {
		return "", fmt.Errorf("%s and %s_FILE cannot both be set", ref, ref)
	}
	if file != "" {
		content, err := os.ReadFile(file)
		if err != nil {
			return "", fmt.Errorf("read %s_FILE: %w", ref, err)
		}
		return strings.TrimSpace(string(content)), nil
	}
	return val, nil
}
