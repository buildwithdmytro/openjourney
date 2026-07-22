package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/buildwithdmytro/openjourney/internal/ai"
	"github.com/buildwithdmytro/openjourney/internal/ai/agent"
	"github.com/buildwithdmytro/openjourney/internal/ai/tools"
	"github.com/buildwithdmytro/openjourney/internal/domain"
	"github.com/buildwithdmytro/openjourney/internal/ports"
	"github.com/buildwithdmytro/openjourney/internal/postgres"
)

type aiDepthSecE2EStore struct {
	ports.Store
	prompts            map[string]domain.Prompt
	promptVersions     map[string]domain.PromptVersion
	recordedActivities []domain.AIActivity
	toolCalls          []tools.ToolCall
}

func newAIDepthSecE2EStore() *aiDepthSecE2EStore {
	return &aiDepthSecE2EStore{
		prompts:        make(map[string]domain.Prompt),
		promptVersions: make(map[string]domain.PromptVersion),
	}
}

func (s *aiDepthSecE2EStore) Authenticate(context.Context, string) (domain.Principal, error) {
	return domain.Principal{
		TenantID:    "t-sec",
		WorkspaceID: "w-sec",
		UserID:      "u-human-1",
		ActorType:   "user",
		Scopes:      []string{"ai:invoke", "prompts:read", "prompts:write", "reports:read"},
	}, nil
}

func (s *aiDepthSecE2EStore) GetPromptByName(ctx context.Context, p domain.Principal, name string) (domain.Prompt, error) {
	if name == "analytics-insight" {
		versionID := "pv-analytics-insight-sec"
		return domain.Prompt{ID: "prompt-sec-1", Name: "analytics-insight", CurrentVersionID: &versionID, TaskType: "analytics_insight"}, nil
	}
	return domain.Prompt{}, postgres.ErrNotFound
}

func (s *aiDepthSecE2EStore) GetPromptVersion(ctx context.Context, p domain.Principal, id string) (domain.PromptVersion, error) {
	if id == "pv-analytics-insight-sec" {
		return domain.PromptVersion{
			ID:           "pv-analytics-insight-sec",
			PromptID:     "prompt-sec-1",
			Status:       "active",
			EvalStatus:   "passed",
			Model:        "fake-model",
			InputSchema:  json.RawMessage(`{"type":"object","required":["question"],"properties":{"question":{"type":"string"}}}`),
			OutputSchema: json.RawMessage(`{"type":"object","required":["summary","insights","key_metrics"],"properties":{"summary":{"type":"string"},"insights":{"type":"array"},"key_metrics":{"type":"array"}}}`),
		}, nil
	}
	if pv, ok := s.promptVersions[id]; ok {
		return pv, nil
	}
	return domain.PromptVersion{}, postgres.ErrNotFound
}

func (s *aiDepthSecE2EStore) GetDefaultAIProviderConfig(context.Context, domain.Principal) (domain.AIProviderConfig, error) {
	return domain.AIProviderConfig{Provider: "fake", Status: "active", Config: json.RawMessage(`{}`)}, nil
}

func (s *aiDepthSecE2EStore) GetAIBudgetUsage(context.Context, string, string, string) (domain.AIBudgetUsage, error) {
	return domain.AIBudgetUsage{}, nil
}

func (s *aiDepthSecE2EStore) IncrementAIBudgetUsage(context.Context, string, string, string, int64, int64, int64) error {
	return nil
}

func (s *aiDepthSecE2EStore) RecordToolCall(ctx context.Context, call tools.ToolCall) error {
	s.toolCalls = append(s.toolCalls, call)
	return nil
}

func (s *aiDepthSecE2EStore) RecordAIActivity(_ context.Context, _ domain.Principal, activity domain.AIActivity) (domain.AIActivity, error) {
	activity.ID = "activity-sec-" + activity.Action
	s.recordedActivities = append(s.recordedActivities, activity)
	return activity, nil
}

func (s *aiDepthSecE2EStore) FunnelOverTimeReport(context.Context, domain.Principal, string, domain.ReportQuery) (domain.FunnelOverTimeReport, error) {
	return domain.FunnelOverTimeReport{
		CampaignID: "camp-sec",
		Buckets: []domain.TimeBucket{
			{
				Funnel: domain.ReportFunnel{
					Sent: domain.ReportCount{Total: 500},
				},
			},
		},
	}, nil
}

func (s *aiDepthSecE2EStore) PublishPromptVersion(ctx context.Context, p domain.Principal, promptID string, version int, approverUserID string, manifestKey string) (domain.PromptVersion, error) {
	if p.ActorType != "user" || p.UserID == "" {
		return domain.PromptVersion{}, postgres.ErrUnauthorized
	}
	return domain.PromptVersion{ID: "pv-pub", Status: "active", EvalStatus: "passed"}, nil
}

type roundTripperFunc func(req *http.Request) (*http.Response, error)

func (f roundTripperFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}

func TestAIDepthSecurityE2E(t *testing.T) {
	t.Run("RunawayLoop_TerminatesAtMaxStepsCap", func(t *testing.T) {
		store := newAIDepthSecE2EStore()
		gw := ai.NewGateway(store)
		prov := &runawayLoopProvider{}
		gw.SetProviderFactory(func(ai.ProviderProfile) ai.ModelProvider {
			return prov
		})

		runner := tools.NewRunner(store, store)
		readTools := tools.ReadOnlyTools()
		for _, tool := range readTools {
			_ = runner.Register(tool)
		}

		ag := agent.NewAgent(gw, runner, readTools, agent.Config{
			MaxSteps: 4,
			Timeout:  5 * time.Second,
		})

		userP := domain.Principal{
			TenantID:    "t-sec",
			WorkspaceID: "w-sec",
			UserID:      "u-human-1",
			ActorType:   "user",
			Scopes:      []string{"reports:read"},
		}

		res, err := ag.Run(context.Background(), userP, "Run runaway loop test")
		if err == nil {
			t.Fatalf("expected error from runaway loop reaching max steps, got nil")
		}
		if res.Status != "max_steps_exceeded" {
			t.Fatalf("expected status max_steps_exceeded, got %s", res.Status)
		}
		if len(res.Trace) != 4 {
			t.Fatalf("expected exactly 4 trace steps, got %d", len(res.Trace))
		}
	})

	t.Run("OverScopedToolCall_DeniedAndAudited", func(t *testing.T) {
		store := newAIDepthSecE2EStore()
		gw := ai.NewGateway(store)
		prov := &singleToolProvider{
			toolName: "report.timeseries",
			toolArgs: `{"report_type":"funnel_over_time","campaign_id":"camp-sec"}`,
		}
		gw.SetProviderFactory(func(ai.ProviderProfile) ai.ModelProvider {
			return prov
		})

		runner := tools.NewRunner(store, store)
		readTools := tools.ReadOnlyTools()
		for _, tool := range readTools {
			_ = runner.Register(tool)
		}

		ag := agent.NewAgent(gw, runner, readTools, agent.Config{
			MaxSteps: 3,
			Timeout:  5 * time.Second,
		})

		// Principal MISSING "reports:read" scope
		overScopedP := domain.Principal{
			TenantID:    "t-sec",
			WorkspaceID: "w-sec",
			UserID:      "u-human-1",
			ActorType:   "user",
			Scopes:      []string{"catalogs:read"}, // Lacks reports:read
		}

		res, err := ag.Run(context.Background(), overScopedP, "Get report without scope")
		if err != nil {
			t.Fatalf("agent.Run unexpected system error: %v", err)
		}

		if len(res.Trace) == 0 {
			t.Fatalf("expected trace steps")
		}

		// Verify tool execution policy decision was denied_scope and audited
		if len(store.toolCalls) == 0 {
			t.Fatalf("expected audited tool call record")
		}
		foundDenied := false
		for _, tc := range store.toolCalls {
			if tc.PolicyDecision == "denied_scope" {
				foundDenied = true
				break
			}
		}
		if !foundDenied {
			t.Fatalf("expected policy decision 'denied_scope' in tool calls audit log, got: %#v", store.toolCalls)
		}
	})

	t.Run("UngroundedInsight_Rejected", func(t *testing.T) {
		store := newAIDepthSecE2EStore()
		gw := ai.NewGateway(store)
		prov := &ungroundedInsightProvider{}
		gw.SetProviderFactory(func(ai.ProviderProfile) ai.ModelProvider {
			return prov
		})

		server := &Server{
			store:     store,
			aiGateway: gw,
		}

		userP := domain.Principal{
			TenantID:    "t-sec",
			WorkspaceID: "w-sec",
			UserID:      "u-human-1",
			ActorType:   "user",
			Scopes:      []string{"ai:invoke", "reports:read"},
		}

		body, _ := json.Marshal(map[string]any{
			"question": "Give me insights for camp-sec",
		})
		req := httptest.NewRequest("POST", "/v1/ai/copilots/insights", bytes.NewReader(body))
		req = req.WithContext(context.WithValue(req.Context(), principalKey{}, userP))
		rr := httptest.NewRecorder()

		server.createInsightsCopilot(rr, req)

		// Must return 422 Unprocessable Entity due to ungrounded citation validation failure
		if rr.Code != http.StatusUnprocessableEntity {
			t.Fatalf("expected HTTP 422 for ungrounded metric response, got %d: %s", rr.Code, rr.Body.String())
		}
		if !strings.Contains(rr.Body.String(), "ungrounded") {
			t.Fatalf("expected error message to mention ungrounded metrics, got: %s", rr.Body.String())
		}
	})

	t.Run("NonHumanPromptPublish_Forbidden", func(t *testing.T) {
		store := newAIDepthSecE2EStore()
		gw := ai.NewGateway(store)
		server := &Server{
			store:     store,
			aiGateway: gw,
		}

		// Agent principal (non-human actor type)
		agentP := domain.Principal{
			TenantID:    "t-sec",
			WorkspaceID: "w-sec",
			ActorType:   "ai_agent",
			Scopes:      []string{"prompts:write"},
		}

		pubBody, _ := json.Marshal(map[string]any{"version": 1})
		req := httptest.NewRequest("POST", "/v1/ai/prompts/prompt-1/versions/1/publish", bytes.NewReader(pubBody))
		req = req.WithContext(context.WithValue(req.Context(), principalKey{}, agentP))
		rr := httptest.NewRecorder()

		server.publishPromptVersion(rr, req)

		if rr.Code != http.StatusForbidden {
			t.Fatalf("expected 403 Forbidden for non-human prompt publish, got %d: %s", rr.Code, rr.Body.String())
		}
	})

	t.Run("PIIRedactionCheck_RestrictedFieldsRedactedBeforeEgress", func(t *testing.T) {
		store := newAIDepthSecE2EStore()
		profile := ai.NewFakeProfile()
		capturedProvider := ai.NewHTTPModelProvider(profile)

		var capturedBody string
		capturedProvider.Client.Transport = roundTripperFunc(func(req *http.Request) (*http.Response, error) {
			b, _ := io.ReadAll(req.Body)
			capturedBody = string(b)
			return &http.Response{
				StatusCode: http.StatusOK,
				Body:       io.NopCloser(strings.NewReader(`{"content":"{\"status\":\"ok\"}","usage":{}}`)),
				Header:     make(http.Header),
			}, nil
		})

		gw := ai.NewGateway(store)
		gw.SetProviderFactory(func(ai.ProviderProfile) ai.ModelProvider {
			return capturedProvider
		})

		userP := domain.Principal{
			TenantID:    "t-sec",
			WorkspaceID: "w-sec",
			UserID:      "u-human-1",
			ActorType:   "user",
			Scopes:      []string{"ai:invoke"},
		}

		_, err := gw.Generate(context.Background(), userP, ai.GenerateRequest{
			Prompt: "Inspect profile details",
			RetrievedData: map[string]any{
				"email": "sensitive_user@example.com",
				"tier":  "enterprise",
			},
			Classifications: []domain.FieldClassification{
				{FieldPath: "email", Classification: "confidential", SendToModel: "redact"},
				{FieldPath: "tier", Classification: "internal", SendToModel: "allow"},
			},
		})

		if err != nil {
			t.Fatalf("Gateway Generate failed: %v", err)
		}

		if strings.Contains(capturedBody, "sensitive_user@example.com") {
			t.Fatalf("sensitive PII email was NOT redacted before egress! Body: %s", capturedBody)
		}
		if !strings.Contains(capturedBody, "[REDACTED]") {
			t.Fatalf("expected [REDACTED] placeholder in provider request body: %s", capturedBody)
		}
	})
}

type runawayLoopProvider struct{}

func (p *runawayLoopProvider) Generate(_ context.Context, req ai.GenerateRequest) (*ai.GenerateResponse, error) {
	return &ai.GenerateResponse{
		Content: `{"action":"tool","tool":"report.timeseries","args":{"report_type":"funnel_over_time"}}`,
		Usage:   ai.Usage{InputTokens: 10, OutputTokens: 10, CostCents: 1},
	}, nil
}

func (runawayLoopProvider) Embed(context.Context, ai.EmbedRequest) (*ai.EmbedResponse, error) {
	return nil, ai.ErrNotSupported
}

func (runawayLoopProvider) Moderate(context.Context, ai.ModerateRequest) (*ai.ModerateResponse, error) {
	return nil, ai.ErrNotSupported
}

type singleToolProvider struct {
	toolName string
	toolArgs string
	step     int
}

func (p *singleToolProvider) Generate(_ context.Context, req ai.GenerateRequest) (*ai.GenerateResponse, error) {
	if p.step == 0 {
		p.step++
		return &ai.GenerateResponse{
			Content: `{"action":"tool","tool":"` + p.toolName + `","args":` + p.toolArgs + `}`,
			Usage:   ai.Usage{InputTokens: 10, OutputTokens: 10, CostCents: 1},
		}, nil
	}
	return &ai.GenerateResponse{
		Content: `{"action":"final","answer":"Done tool test"}`,
		Usage:   ai.Usage{InputTokens: 10, OutputTokens: 10, CostCents: 1},
	}, nil
}

func (singleToolProvider) Embed(context.Context, ai.EmbedRequest) (*ai.EmbedResponse, error) {
	return nil, ai.ErrNotSupported
}

func (singleToolProvider) Moderate(context.Context, ai.ModerateRequest) (*ai.ModerateResponse, error) {
	return nil, ai.ErrNotSupported
}

type ungroundedInsightProvider struct {
	step int
}

func (p *ungroundedInsightProvider) Generate(_ context.Context, req ai.GenerateRequest) (*ai.GenerateResponse, error) {
	if p.step == 0 {
		p.step++
		return &ai.GenerateResponse{
			Content: `{"action":"tool","tool":"report.timeseries","args":{"report_type":"funnel_over_time","campaign_id":"camp-sec"}}`,
			Usage:   ai.Usage{InputTokens: 10, OutputTokens: 10, CostCents: 1},
		}, nil
	}
	// Return ungrounded metric value (999999) which doesn't match retrieved report data (500)
	return &ai.GenerateResponse{
		Content: `{"action":"final","answer":"{\"summary\":\"Fabricated report\",\"insights\":[\"Fake metric\"],\"key_metrics\":[{\"name\":\"sent\",\"value\":999999,\"source\":\"report.timeseries\"}]}"}`,
		Usage:   ai.Usage{InputTokens: 10, OutputTokens: 10, CostCents: 1},
	}, nil
}

func (ungroundedInsightProvider) Embed(context.Context, ai.EmbedRequest) (*ai.EmbedResponse, error) {
	return nil, ai.ErrNotSupported
}

func (ungroundedInsightProvider) Moderate(context.Context, ai.ModerateRequest) (*ai.ModerateResponse, error) {
	return nil, ai.ErrNotSupported
}
