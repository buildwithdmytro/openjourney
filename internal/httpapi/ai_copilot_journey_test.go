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

type journeyCopilotStore struct {
	ports.Store
	draft domain.Journey
}

func (s *journeyCopilotStore) Authenticate(context.Context, string) (domain.Principal, error) {
	return domain.Principal{TenantID: "tenant-1", WorkspaceID: "workspace-1", UserID: "user-1", ActorType: "user", Scopes: []string{"ai:invoke"}}, nil
}
func (s *journeyCopilotStore) GetPromptByName(context.Context, domain.Principal, string) (domain.Prompt, error) {
	versionID := "version-1"
	return domain.Prompt{ID: "prompt-1", CurrentVersionID: &versionID, TaskType: "journey_draft"}, nil
}
func (s *journeyCopilotStore) GetPromptVersion(context.Context, domain.Principal, string) (domain.PromptVersion, error) {
	return domain.PromptVersion{ID: "version-1", Status: "active", EvalStatus: "passed", Model: "fake-journey-draft-v1",
		Template:     "Translate DATA into a journey graph.",
		InputSchema:  json.RawMessage(`{"type":"object","properties":{"brief":{"type":"string"}},"required":["brief"],"additionalProperties":false}`),
		OutputSchema: json.RawMessage(`{"type":"object","required":["entry_node_id","nodes","edges"],"properties":{"entry_node_id":{"type":"string"},"nodes":{"type":"array"},"edges":{"type":"array"}}}`)}, nil
}
func (s *journeyCopilotStore) GetDefaultAIProviderConfig(context.Context, domain.Principal) (domain.AIProviderConfig, error) {
	return domain.AIProviderConfig{Provider: "fake", Status: "active", Config: json.RawMessage(`{}`)}, nil
}
func (s *journeyCopilotStore) GetAIBudgetUsage(context.Context, string, string, string) (domain.AIBudgetUsage, error) {
	return domain.AIBudgetUsage{}, nil
}
func (s *journeyCopilotStore) IncrementAIBudgetUsage(context.Context, string, string, string, int64, int64, int64) error {
	return nil
}
func (s *journeyCopilotStore) RecordAIActivity(_ context.Context, _ domain.Principal, activity domain.AIActivity) (domain.AIActivity, error) {
	activity.ID = "activity-1"
	return activity, nil
}
func (s *journeyCopilotStore) CreateJourney(_ context.Context, _ domain.Principal, journey domain.Journey) (domain.Journey, error) {
	journey.ID = "draft-journey-1"
	s.draft = journey
	return journey, nil
}

type journeyProvider struct{}

func (journeyProvider) Generate(context.Context, ai.GenerateRequest) (*ai.GenerateResponse, error) {
	return &ai.GenerateResponse{Content: `{"entry_node_id":"entry","nodes":[{"id":"entry","type":"entry","config":{"trigger":"event","event_type":"signup.completed"}},{"id":"exit","type":"exit","config":{"reason":"completed"}}],"edges":[{"from":"entry","to":"exit"}]}`, Usage: ai.Usage{InputTokens: 10, OutputTokens: 16, CostCents: 1}}, nil
}
func (journeyProvider) Embed(context.Context, ai.EmbedRequest) (*ai.EmbedResponse, error) {
	return nil, ai.ErrNotSupported
}
func (journeyProvider) Moderate(context.Context, ai.ModerateRequest) (*ai.ModerateResponse, error) {
	return nil, ai.ErrNotSupported
}

func TestJourneyCopilotCreatesValidatedDraftWithoutPublishing(t *testing.T) {
	store := &journeyCopilotStore{}
	gateway := ai.NewGateway(store)
	gateway.SetProviderFactory(func(ai.ProviderProfile) ai.ModelProvider { return journeyProvider{} })
	handler := NewWithSessionTTL(store, 10, nil, "http://localhost:3000", 0, WithAIGateway(gateway))
	req := httptest.NewRequest(http.MethodPost, "/v1/ai/copilots/journey", io.NopCloser(strings.NewReader(`{"brief":"welcome new customers"}`)))
	req.Header.Set("Authorization", "Bearer test-key")
	res := httptest.NewRecorder()
	handler.ServeHTTP(res, req)
	if res.Code != http.StatusCreated {
		t.Fatalf("journey copilot status = %d, body = %s", res.Code, res.Body.String())
	}
	if store.draft.ID != "draft-journey-1" || store.draft.Status != "draft" {
		t.Fatalf("expected a new draft journey, got %#v", store.draft)
	}
	if store.draft.CurrentVersionID != nil || store.draft.LatestVersion != 0 {
		t.Fatalf("copilot unexpectedly published or versioned journey: %#v", store.draft)
	}
	var response map[string]any
	if err := json.Unmarshal(res.Body.Bytes(), &response); err != nil {
		t.Fatal(err)
	}
	if response["activity_id"] != "activity-1" {
		t.Fatalf("expected activity id, got %#v", response["activity_id"])
	}
}
