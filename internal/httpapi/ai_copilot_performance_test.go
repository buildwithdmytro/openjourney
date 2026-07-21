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

type performanceCopilotStore struct {
	ports.Store
	draft    domain.Campaign
	original domain.Campaign
}

func (s *performanceCopilotStore) Authenticate(context.Context, string) (domain.Principal, error) {
	return domain.Principal{TenantID: "tenant-1", WorkspaceID: "workspace-1", UserID: "user-1", ActorType: "user", Scopes: []string{"ai:invoke"}}, nil
}
func (s *performanceCopilotStore) GetCampaign(context.Context, domain.Principal, string) (domain.Campaign, error) {
	s.original = domain.Campaign{ID: "campaign-1", Name: "Live campaign", SegmentID: "segment-1", TemplateID: "template-1", Status: "scheduled", SegmentVersion: 2, TemplateVersion: 3}
	return s.original, nil
}
func (s *performanceCopilotStore) CampaignReport(context.Context, domain.Principal, string, domain.ReportQuery) (domain.CampaignReport, error) {
	return domain.CampaignReport{CampaignID: "campaign-1", Funnel: domain.ReportFunnel{Sent: domain.ReportCount{Total: 42, Unique: 40}}}, nil
}
func (s *performanceCopilotStore) GetPromptByName(context.Context, domain.Principal, string) (domain.Prompt, error) {
	versionID := "version-1"
	return domain.Prompt{ID: "prompt-1", CurrentVersionID: &versionID, TaskType: "performance_summary"}, nil
}
func (s *performanceCopilotStore) GetPromptVersion(context.Context, domain.Principal, string) (domain.PromptVersion, error) {
	return domain.PromptVersion{ID: "version-1", Status: "active", EvalStatus: "passed", Model: "fake-performance-summary-v1",
		Template:     "Summarize DATA.",
		InputSchema:  json.RawMessage(`{"type":"object","required":["campaign_id","campaign_report"]}`),
		OutputSchema: json.RawMessage(`{"type":"object","required":["summary","key_metrics","recommendations","proposed_version"],"properties":{"summary":{"type":"string"},"key_metrics":{"type":"array","items":{"type":"object","required":["name","value","source"]}},"recommendations":{"type":"array"},"proposed_version":{"type":"object","required":["name","changes"]}}}`)}, nil
}
func (s *performanceCopilotStore) GetDefaultAIProviderConfig(context.Context, domain.Principal) (domain.AIProviderConfig, error) {
	return domain.AIProviderConfig{Provider: "fake", Status: "active", Config: json.RawMessage(`{}`)}, nil
}
func (s *performanceCopilotStore) GetAIBudgetUsage(context.Context, string, string, string) (domain.AIBudgetUsage, error) {
	return domain.AIBudgetUsage{}, nil
}
func (s *performanceCopilotStore) IncrementAIBudgetUsage(context.Context, string, string, string, int64, int64, int64) error {
	return nil
}
func (s *performanceCopilotStore) RecordAIActivity(_ context.Context, _ domain.Principal, activity domain.AIActivity) (domain.AIActivity, error) {
	activity.ID = "activity-1"
	return activity, nil
}
func (s *performanceCopilotStore) CreateCampaign(_ context.Context, _ domain.Principal, campaign domain.Campaign) (domain.Campaign, error) {
	campaign.ID = "draft-campaign-1"
	s.draft = campaign
	return campaign, nil
}

type performanceProvider struct{}

func (performanceProvider) Generate(context.Context, ai.GenerateRequest) (*ai.GenerateResponse, error) {
	return &ai.GenerateResponse{Content: `{"summary":"Sent 42 messages; 40 unique recipients.","key_metrics":[{"name":"sent","value":42,"source":"campaign_report.funnel.sent.total"}],"recommendations":["Review the next draft before approval."],"proposed_version":{"name":"AI next version","changes":{"strategy":"test a clearer subject"}}}`, Usage: ai.Usage{InputTokens: 20, OutputTokens: 30, CostCents: 1}}, nil
}
func (performanceProvider) Embed(context.Context, ai.EmbedRequest) (*ai.EmbedResponse, error) {
	return nil, ai.ErrNotSupported
}
func (performanceProvider) Moderate(context.Context, ai.ModerateRequest) (*ai.ModerateResponse, error) {
	return nil, ai.ErrNotSupported
}

func TestPerformanceCopilotCitesReportAndCreatesDraft(t *testing.T) {
	store := &performanceCopilotStore{}
	gateway := ai.NewGateway(store)
	gateway.SetProviderFactory(func(ai.ProviderProfile) ai.ModelProvider { return performanceProvider{} })
	handler := NewWithSessionTTL(store, 10, nil, "http://localhost:3000", 0, WithAIGateway(gateway))
	req := httptest.NewRequest(http.MethodPost, "/v1/ai/copilots/performance/campaign-1", io.NopCloser(strings.NewReader(`{}`)))
	req.Header.Set("Authorization", "Bearer test-key")
	res := httptest.NewRecorder()
	handler.ServeHTTP(res, req)
	if res.Code != http.StatusCreated {
		t.Fatalf("performance copilot status = %d, body = %s", res.Code, res.Body.String())
	}
	if store.original.Status != "scheduled" || store.draft.Status != "draft" || store.draft.ID == store.original.ID {
		t.Fatalf("live campaign was mutated or proposal was not a draft: live=%+v draft=%+v", store.original, store.draft)
	}
	if store.draft.SegmentID != store.original.SegmentID || store.draft.TemplateID != store.original.TemplateID {
		t.Fatalf("proposal did not preserve immutable campaign inputs: %+v", store.draft)
	}
	if store.original.Status != "scheduled" || store.original.ID != "campaign-1" {
		t.Fatalf("live campaign was mutated by the proposal: %+v", store.original)
	}
	if !strings.Contains(res.Body.String(), `"activity_id":"activity-1"`) || !strings.Contains(res.Body.String(), `"value":42`) {
		t.Fatalf("response did not include audited real report metric: %s", res.Body.String())
	}
}
