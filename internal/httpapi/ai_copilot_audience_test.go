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

type audienceCopilotStore struct {
	ports.Store
	draft domain.Segment
}

func (s *audienceCopilotStore) Authenticate(context.Context, string) (domain.Principal, error) {
	return domain.Principal{TenantID: "tenant-1", WorkspaceID: "workspace-1", UserID: "user-1", ActorType: "user", Scopes: []string{"ai:invoke"}}, nil
}
func (s *audienceCopilotStore) GetPromptByName(context.Context, domain.Principal, string) (domain.Prompt, error) {
	versionID := "version-1"
	return domain.Prompt{ID: "prompt-1", CurrentVersionID: &versionID, TaskType: "audience_dsl"}, nil
}
func (s *audienceCopilotStore) GetPromptVersion(context.Context, domain.Principal, string) (domain.PromptVersion, error) {
	return domain.PromptVersion{ID: "version-1", Status: "active", EvalStatus: "passed", Model: "fake-audience-dsl-v1",
		Template:     "Translate DATA into an audience AST.",
		InputSchema:  json.RawMessage(`{"type":"object","properties":{"brief":{"type":"string"}},"required":["brief"],"additionalProperties":false}`),
		OutputSchema: json.RawMessage(`{"type":"object","required":["type","field","operator","value"],"properties":{"type":{"const":"profile_attribute"},"field":{"type":"string"},"operator":{"enum":["equals"]},"value":{}}}`)}, nil
}
func (s *audienceCopilotStore) GetDefaultAIProviderConfig(context.Context, domain.Principal) (domain.AIProviderConfig, error) {
	return domain.AIProviderConfig{Provider: "fake", Status: "active", Config: json.RawMessage(`{}`)}, nil
}
func (s *audienceCopilotStore) GetAIBudgetUsage(context.Context, string, string, string) (domain.AIBudgetUsage, error) {
	return domain.AIBudgetUsage{}, nil
}
func (s *audienceCopilotStore) IncrementAIBudgetUsage(context.Context, string, string, string, int64, int64, int64) error {
	return nil
}
func (s *audienceCopilotStore) RecordAIActivity(_ context.Context, _ domain.Principal, activity domain.AIActivity) (domain.AIActivity, error) {
	activity.ID = "activity-1"
	return activity, nil
}
func (s *audienceCopilotStore) CreateSegment(_ context.Context, _ domain.Principal, segment domain.Segment) (domain.Segment, error) {
	segment.ID, segment.Status = "draft-segment-1", "draft"
	s.draft = segment
	return segment, nil
}
func (s *audienceCopilotStore) PreviewSegment(_ context.Context, _ domain.Principal, id string) (int, map[string]int, error) {
	if id != s.draft.ID {
		return 0, nil, context.Canceled
	}
	return 7, map[string]int{"profile_attributes": 7}, nil
}

type audienceProvider struct{}

func (audienceProvider) Generate(context.Context, ai.GenerateRequest) (*ai.GenerateResponse, error) {
	return &ai.GenerateResponse{Content: `{"type":"profile_attribute","field":"country","operator":"equals","value":"US"}`, Usage: ai.Usage{InputTokens: 10, OutputTokens: 8, CostCents: 1}}, nil
}
func (audienceProvider) Embed(context.Context, ai.EmbedRequest) (*ai.EmbedResponse, error) {
	return nil, ai.ErrNotSupported
}
func (audienceProvider) Moderate(context.Context, ai.ModerateRequest) (*ai.ModerateResponse, error) {
	return nil, ai.ErrNotSupported
}

func TestAudienceCopilotCreatesValidatedDraftAndPreview(t *testing.T) {
	store := &audienceCopilotStore{}
	gateway := ai.NewGateway(store)
	gateway.SetProviderFactory(func(ai.ProviderProfile) ai.ModelProvider { return audienceProvider{} })
	handler := NewWithSessionTTL(store, 10, nil, "http://localhost:3000", 0, WithAIGateway(gateway))
	req := httptest.NewRequest(http.MethodPost, "/v1/ai/copilots/audience", io.NopCloser(strings.NewReader(`{"brief":"US customers"}`)))
	req.Header.Set("Authorization", "Bearer test-key")
	res := httptest.NewRecorder()
	handler.ServeHTTP(res, req)
	if res.Code != http.StatusCreated {
		t.Fatalf("audience copilot status = %d, body = %s", res.Code, res.Body.String())
	}
	if store.draft.Status != "draft" || string(store.draft.DSL) != `{"type":"profile_attribute","field":"country","operator":"equals","value":"US"}` {
		t.Fatalf("unexpected draft: %+v", store.draft)
	}
	var body map[string]any
	if err := json.Unmarshal(res.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if body["activity_id"] != "activity-1" || body["explanation"] == "" {
		t.Fatalf("missing governed response fields: %#v", body)
	}
	preview := body["preview"].(map[string]any)
	if preview["count"] != float64(7) {
		t.Fatalf("unexpected preview: %#v", preview)
	}
}
