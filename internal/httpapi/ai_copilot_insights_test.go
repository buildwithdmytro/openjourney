package httpapi

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/buildwithdmytro/openjourney/internal/ai"
	"github.com/buildwithdmytro/openjourney/internal/domain"
	"github.com/buildwithdmytro/openjourney/internal/ports"
)

type insightsCopilotStore struct {
	ports.Store
}

func (s *insightsCopilotStore) Authenticate(context.Context, string) (domain.Principal, error) {
	return domain.Principal{TenantID: "tenant-1", WorkspaceID: "workspace-1", UserID: "user-1", ActorType: "user", Scopes: []string{"ai:invoke", "reports:read"}}, nil
}

func (s *insightsCopilotStore) GetPromptByName(context.Context, domain.Principal, string) (domain.Prompt, error) {
	versionID := "version-1"
	return domain.Prompt{ID: "prompt-1", CurrentVersionID: &versionID, TaskType: "analytics_insight"}, nil
}

func (s *insightsCopilotStore) GetPromptVersion(context.Context, domain.Principal, string) (domain.PromptVersion, error) {
	return domain.PromptVersion{
		ID:           "version-1",
		Status:       "active",
		EvalStatus:   "passed",
		Model:        "fake-analytics-insight-v1",
		Template:     "Analyze data.",
		InputSchema:  json.RawMessage(`{"type":"object","required":["question"],"properties":{"question":{"type":"string"}}}`),
		OutputSchema: json.RawMessage(`{"type":"object","required":["summary","insights","key_metrics"],"properties":{"summary":{"type":"string"},"insights":{"type":"array"},"key_metrics":{"type":"array"}}}`),
	}, nil
}

func (s *insightsCopilotStore) GetDefaultAIProviderConfig(context.Context, domain.Principal) (domain.AIProviderConfig, error) {
	return domain.AIProviderConfig{Provider: "fake", Status: "active", Config: json.RawMessage(`{}`)}, nil
}

func (s *insightsCopilotStore) GetAIBudgetUsage(context.Context, string, string, string) (domain.AIBudgetUsage, error) {
	return domain.AIBudgetUsage{}, nil
}

func (s *insightsCopilotStore) IncrementAIBudgetUsage(context.Context, string, string, string, int64, int64, int64) error {
	return nil
}

func (s *insightsCopilotStore) RecordAIActivity(_ context.Context, _ domain.Principal, activity domain.AIActivity) (domain.AIActivity, error) {
	activity.ID = "activity-insight-1"
	return activity, nil
}

func (s *insightsCopilotStore) FunnelOverTimeReport(context.Context, domain.Principal, string, domain.ReportQuery) (domain.FunnelOverTimeReport, error) {
	return domain.FunnelOverTimeReport{
		CampaignID: "campaign-1",
		Buckets: []domain.TimeBucket{
			{
				Funnel: domain.ReportFunnel{
					Sent: domain.ReportCount{Total: 42},
				},
			},
		},
	}, nil
}

type insightsProvider struct {
	responseContent string
}

func (p insightsProvider) Generate(ctx context.Context, req ai.GenerateRequest) (*ai.GenerateResponse, error) {
	if req.Action == "ai.agent.step" {
		// If step prompt contains Tool Result with 42, produce final grounded answer
		if strings.Contains(req.Prompt, "42") {
			return &ai.GenerateResponse{
				Content: `{"action":"final","answer":"{\"summary\":\"Total sent reached 42.\",\"insights\":[\"Significant volume observed.\"],\"key_metrics\":[{\"name\":\"sent_total\",\"value\":42,\"source\":\"report.timeseries\"}]}"}`,
				Usage:   ai.Usage{InputTokens: 10, OutputTokens: 20, CostCents: 1},
			}, nil
		}
		// First step: call tool report.timeseries
		return &ai.GenerateResponse{
			Content: `{"action":"tool","tool":"report.timeseries","args":{"report_type":"funnel_over_time","campaign_id":"campaign-1"}}`,
			Usage:   ai.Usage{InputTokens: 10, OutputTokens: 20, CostCents: 1},
		}, nil
	}

	return &ai.GenerateResponse{
		Content: p.responseContent,
		Usage:   ai.Usage{InputTokens: 10, OutputTokens: 20, CostCents: 1},
	}, nil
}

func (insightsProvider) Embed(context.Context, ai.EmbedRequest) (*ai.EmbedResponse, error) {
	return nil, ai.ErrNotSupported
}
func (insightsProvider) Moderate(context.Context, ai.ModerateRequest) (*ai.ModerateResponse, error) {
	return nil, ai.ErrNotSupported
}

func TestInsightsCopilotGrounded(t *testing.T) {
	store := &insightsCopilotStore{}
	gateway := ai.NewGateway(store)
	gateway.SetProviderFactory(func(ai.ProviderProfile) ai.ModelProvider { return insightsProvider{} })
	handler := NewWithSessionTTL(store, 10, nil, "http://localhost:3000", 0, WithAIGateway(gateway))

	reqBody := `{"question":"How is the conversion rate performing over time?"}`
	req := httptest.NewRequest(http.MethodPost, "/v1/ai/copilots/insights", io.NopCloser(strings.NewReader(reqBody)))
	req.Header.Set("Authorization", "Bearer test-key")
	res := httptest.NewRecorder()
	handler.ServeHTTP(res, req)

	if res.Code != http.StatusOK {
		t.Fatalf("insights copilot status = %d, body = %s", res.Code, res.Body.String())
	}
	body := res.Body.String()
	if !strings.Contains(body, `"sent_total"`) || !strings.Contains(body, `"value":42`) {
		t.Fatalf("response missing grounded metric: %s", body)
	}
	if !strings.Contains(body, `"activity_id":"activity-insight-1"`) {
		t.Fatalf("response missing activity_id: %s", body)
	}
	if !strings.Contains(body, `"trace"`) || !strings.Contains(body, `"report.timeseries"`) {
		t.Fatalf("response missing tool trace: %s", body)
	}
}

type ungroundedInsightsProvider struct{}

func (ungroundedInsightsProvider) Generate(ctx context.Context, req ai.GenerateRequest) (*ai.GenerateResponse, error) {
	if req.Action == "ai.agent.step" {
		if strings.Contains(req.Prompt, "42") {
			// Claims ungrounded metric 999
			return &ai.GenerateResponse{
				Content: `{"action":"final","answer":"{\"summary\":\"Fake summary.\",\"insights\":[\"Fake insight.\"],\"key_metrics\":[{\"name\":\"fake_rate\",\"value\":999,\"source\":\"invented\"}]}"}`,
				Usage:   ai.Usage{InputTokens: 10, OutputTokens: 20, CostCents: 1},
			}, nil
		}
		return &ai.GenerateResponse{
			Content: `{"action":"tool","tool":"report.timeseries","args":{"report_type":"funnel_over_time","campaign_id":"campaign-1"}}`,
			Usage:   ai.Usage{InputTokens: 10, OutputTokens: 20, CostCents: 1},
		}, nil
	}
	return nil, nil
}
func (ungroundedInsightsProvider) Embed(context.Context, ai.EmbedRequest) (*ai.EmbedResponse, error) {
	return nil, ai.ErrNotSupported
}
func (ungroundedInsightsProvider) Moderate(context.Context, ai.ModerateRequest) (*ai.ModerateResponse, error) {
	return nil, ai.ErrNotSupported
}

func TestInsightsCopilotUngroundedMetricRejected(t *testing.T) {
	store := &insightsCopilotStore{}
	gateway := ai.NewGateway(store)
	gateway.SetProviderFactory(func(ai.ProviderProfile) ai.ModelProvider { return ungroundedInsightsProvider{} })
	handler := NewWithSessionTTL(store, 10, nil, "http://localhost:3000", 0, WithAIGateway(gateway))

	reqBody := `{"question":"What is the rate?"}`
	req := httptest.NewRequest(http.MethodPost, "/v1/ai/copilots/insights", io.NopCloser(strings.NewReader(reqBody)))
	req.Header.Set("Authorization", "Bearer test-key")
	res := httptest.NewRecorder()
	handler.ServeHTTP(res, req)

	if res.Code != http.StatusUnprocessableEntity {
		t.Fatalf("expected unprocessable entity for ungrounded metric, got %d: %s", res.Code, res.Body.String())
	}
	if !strings.Contains(res.Body.String(), "ungrounded_metric") {
		t.Fatalf("expected ungrounded_metric error code, got %s", res.Body.String())
	}
}
