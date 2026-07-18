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
	record := func(provider, model, decision string, usage Usage, latencyMs int64, outputRef string) error {
		return g.recordActivity(ctx, principal, req.Action, provider, model, decision, usage, latencyMs,
			req.PromptVersionID, req.RetrievalRefs, req.ToolCalls, req.Classification, outputRef)
	}
	if req.Action == "" {
		req.Action = "ai.generate"
	}
	if req.RetrievedData != nil {
		redacted, err := Redact(req.RetrievedData, req.Classifications, req.Purpose)
		if err != nil {
			if record("unknown", req.Model, "denied_policy", Usage{}, 0, "") != nil {
				return nil, fmt.Errorf("%w; activity recording failed", err)
			}
			return nil, err
		}
		prompt, err := GovernedPrompt(req.Prompt, redacted)
		if err != nil {
			if activityErr := record("unknown", req.Model, "denied_policy", Usage{}, 0, ""); activityErr != nil {
				return nil, fmt.Errorf("%w; activity recording failed: %v", err, activityErr)
			}
			return nil, err
		}
		req.Prompt = prompt
	}
	cfg, jsonCfg, prov, err := g.getProvider(ctx, principal)
	if err != nil {
		if activityErr := record("unknown", req.Model, "denied_policy", Usage{}, 0, ""); activityErr != nil {
			return nil, fmt.Errorf("%w; activity recording failed: %v", err, activityErr)
		}
		return nil, err
	}

	period := time.Now().UTC().Format("2006-01")
	if err := g.checkBudget(ctx, principal, cfg, period); err != nil {
		if record(cfg.Provider, req.Model, "denied_budget", Usage{}, 0, "") != nil {
			return nil, fmt.Errorf("%w; activity recording failed", err)
		}
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
		if record(cfg.Provider, req.Model, "denied_policy", Usage{}, time.Since(start).Milliseconds(), "") != nil {
			return nil, fmt.Errorf("%w; activity recording failed", err)
		}
		return nil, err
	}
	duration := time.Since(start).Milliseconds()

	if err := g.recordMetricsAndIncrementBudget(ctx, principal, cfg, period, resp.Usage, duration, req.Model); err != nil {
		return nil, err
	}

	// Validate output against output_schema and domain validator
	var valErr error
	if len(req.OutputSchema) > 0 || req.DomainValidator != nil {
		valErr = ValidateOutput([]byte(resp.Content), req.OutputSchema, req.DomainValidator)
	}

	if valErr != nil {
		// Schema/domain validation failed: increment schema rejections counter
		telemetry.AISchemaRejections.Add(ctx, 1, otelmetric.WithAttributes(
			attribute.String("tenant_id", principal.TenantID),
			attribute.String("workspace_id", principal.WorkspaceID),
		))

		// Check budget again before attempting the repair call
		if err := g.checkBudget(ctx, principal, cfg, period); err != nil {
			if activityErr := record(cfg.Provider, req.Model, "schema_reject", resp.Usage, duration, ""); activityErr != nil {
				return nil, fmt.Errorf("model output failed validation: %v; activity recording failed: %w", valErr, activityErr)
			}
			return nil, fmt.Errorf("model output failed validation: %v; repair retry blocked: %w", valErr, err)
		}

		// Prepare repair request by suffixing the prompt with repair instructions
		repairReq := req
		repairReq.Prompt = fmt.Sprintf(
			"%s\n\nYour previous response failed validation with the following error:\n%v\n\nPlease fix the response and return it conforming strictly to the requested schema. Do not include any other text.",
			req.Prompt,
			valErr,
		)

		startRepair := time.Now()
		repairResp, repairErr := prov.Generate(ctx, repairReq)
		if repairErr != nil {
			if activityErr := record(cfg.Provider, req.Model, "schema_reject", resp.Usage, duration, ""); activityErr != nil {
				return nil, fmt.Errorf("model output failed validation: %v; activity recording failed: %w", valErr, activityErr)
			}
			return nil, fmt.Errorf("model output failed validation: %v; repair retry failed: %w", valErr, repairErr)
		}
		durationRepair := time.Since(startRepair).Milliseconds()

		if err := g.recordMetricsAndIncrementBudget(ctx, principal, cfg, period, repairResp.Usage, durationRepair, req.Model); err != nil {
			return nil, err
		}

		// Validate repaired output
		if repairValErr := ValidateOutput([]byte(repairResp.Content), req.OutputSchema, req.DomainValidator); repairValErr != nil {
			telemetry.AISchemaRejections.Add(ctx, 1, otelmetric.WithAttributes(
				attribute.String("tenant_id", principal.TenantID),
				attribute.String("workspace_id", principal.WorkspaceID),
			))
			if activityErr := record(cfg.Provider, req.Model, "schema_reject", repairResp.Usage, duration+durationRepair, ""); activityErr != nil {
				return nil, fmt.Errorf("model output failed validation: %v; activity recording failed: %w", valErr, activityErr)
			}
			return nil, fmt.Errorf("model output failed validation: %v; repaired output also failed validation: %w", valErr, repairValErr)
		}

		resp = repairResp
	}
	if err := record(cfg.Provider, req.Model, "allowed", resp.Usage, duration, ""); err != nil {
		return nil, fmt.Errorf("AI activity recording failed: %w", err)
	}

	return resp, nil
}

func stringPtrOrNil(value string) *string {
	if value == "" {
		return nil
	}
	return &value
}

func (g *Gateway) recordActivity(ctx context.Context, principal domain.Principal, action, provider, model, decision string, usage Usage, latencyMs int64, promptVersionID string, retrievalRefs, toolCalls json.RawMessage, classification, outputRef string) error {
	recorder, ok := g.store.(ports.AIActivityRecorder)
	if !ok {
		return nil
	}
	activity := domain.AIActivity{
		TenantID: principal.TenantID, WorkspaceID: principal.WorkspaceID,
		Action: action, Provider: provider, Model: model,
		PromptVersionID: stringPtrOrNil(promptVersionID), RetrievalRefs: retrievalRefs,
		ToolCalls: toolCalls, Classification: stringPtrOrNil(classification),
		InputTokens: usage.InputTokens, OutputTokens: usage.OutputTokens,
		CostCents: usage.CostCents, LatencyMs: int(latencyMs), PolicyDecision: decision,
		OutputRef: stringPtrOrNil(outputRef), ActorUserID: stringPtrOrNil(principal.UserID),
	}
	_, err := recorder.RecordAIActivity(ctx, principal, activity)
	return err
}

func (g *Gateway) Embed(ctx context.Context, principal domain.Principal, req EmbedRequest) (*EmbedResponse, error) {
	activity := func(provider, decision string, usage Usage, latencyMs int64) error {
		return g.recordActivity(ctx, principal, "ai.embed", provider, req.Model, decision, usage, latencyMs, "", nil, nil, "", "")
	}
	cfg, jsonCfg, prov, err := g.getProvider(ctx, principal)
	if err != nil {
		_ = activity("unknown", "denied_policy", Usage{}, 0)
		return nil, err
	}

	period := time.Now().UTC().Format("2006-01")
	if err := g.checkBudget(ctx, principal, cfg, period); err != nil {
		_ = activity(cfg.Provider, "denied_budget", Usage{}, 0)
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
		_ = activity(cfg.Provider, "denied_policy", Usage{}, time.Since(start).Milliseconds())
		return nil, err
	}
	duration := time.Since(start).Milliseconds()

	if err := g.recordMetricsAndIncrementBudget(ctx, principal, cfg, period, resp.Usage, duration, req.Model); err != nil {
		return nil, err
	}
	if err := activity(cfg.Provider, "allowed", resp.Usage, duration); err != nil {
		return nil, fmt.Errorf("AI activity recording failed: %w", err)
	}

	return resp, nil
}

func (g *Gateway) Moderate(ctx context.Context, principal domain.Principal, req ModerateRequest) (*ModerateResponse, error) {
	activity := func(provider, decision string, usage Usage, latencyMs int64) error {
		return g.recordActivity(ctx, principal, "ai.moderate", provider, req.Model, decision, usage, latencyMs, "", nil, nil, "", "")
	}
	cfg, jsonCfg, prov, err := g.getProvider(ctx, principal)
	if err != nil {
		_ = activity("unknown", "denied_policy", Usage{}, 0)
		return nil, err
	}

	period := time.Now().UTC().Format("2006-01")
	if err := g.checkBudget(ctx, principal, cfg, period); err != nil {
		_ = activity(cfg.Provider, "denied_budget", Usage{}, 0)
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
		_ = activity(cfg.Provider, "denied_policy", Usage{}, time.Since(start).Milliseconds())
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
	if err := activity(cfg.Provider, "allowed", resp.Usage, duration); err != nil {
		return nil, fmt.Errorf("AI activity recording failed: %w", err)
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
