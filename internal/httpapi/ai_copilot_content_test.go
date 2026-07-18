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

type contentCopilotStore struct {
	ports.Store
	draft domain.Template
}

func (s *contentCopilotStore) Authenticate(context.Context, string) (domain.Principal, error) {
	return domain.Principal{TenantID: "tenant-1", WorkspaceID: "workspace-1", UserID: "user-1", ActorType: "user", Scopes: []string{"ai:invoke"}}, nil
}
func (s *contentCopilotStore) GetPromptByName(context.Context, domain.Principal, string) (domain.Prompt, error) {
	versionID := "version-1"
	return domain.Prompt{ID: "prompt-1", CurrentVersionID: &versionID, TaskType: "content_draft"}, nil
}
func (s *contentCopilotStore) GetPromptVersion(context.Context, domain.Principal, string) (domain.PromptVersion, error) {
	return domain.PromptVersion{
		ID: "version-1", Status: "active", EvalStatus: "passed", Provider: "fake", Model: "fake-content-draft-v1",
		Template:     "Draft content from DATA.",
		InputSchema:  json.RawMessage(`{"type":"object","properties":{"brief":{"type":"string"},"locale":{"type":"string"},"brand_voice":{"type":"string"}},"required":["brief"],"additionalProperties":false}`),
		OutputSchema: json.RawMessage(`{"type":"object","properties":{"subject":{"type":"string"},"body":{"type":"string"},"title":{"type":"string"},"push_data":{"type":"object","additionalProperties":{"type":"string"}},"localizations":{"type":"object"},"qa":{"type":"object","properties":{"passed":{"type":"boolean"},"issues":{"type":"array","items":{"type":"string"}}},"required":["passed","issues"]}},"required":["subject","body","title","push_data","localizations","qa"],"additionalProperties":false}`),
	}, nil
}
func (s *contentCopilotStore) GetDefaultAIProviderConfig(context.Context, domain.Principal) (domain.AIProviderConfig, error) {
	return domain.AIProviderConfig{Provider: "fake", Status: "active", Config: json.RawMessage(`{}`)}, nil
}
func (s *contentCopilotStore) GetAIBudgetUsage(context.Context, string, string, string) (domain.AIBudgetUsage, error) {
	return domain.AIBudgetUsage{}, nil
}
func (s *contentCopilotStore) IncrementAIBudgetUsage(context.Context, string, string, string, int64, int64, int64) error {
	return nil
}
func (s *contentCopilotStore) RecordAIActivity(_ context.Context, _ domain.Principal, activity domain.AIActivity) (domain.AIActivity, error) {
	activity.ID = "activity-1"
	return activity, nil
}
func (s *contentCopilotStore) CreateTemplate(_ context.Context, _ domain.Principal, template domain.Template) (domain.Template, error) {
	template.ID = "draft-template-1"
	s.draft = template
	return template, nil
}

func TestContentCopilotCreatesRenderableDraftWithoutMutation(t *testing.T) {
	store := &contentCopilotStore{}
	gateway := ai.NewGateway(store)
	gateway.SetProviderFactory(func(ai.ProviderProfile) ai.ModelProvider { return ai.DeterministicFakeProvider{} })
	handler := NewWithSessionTTL(store, 10, nil, "http://localhost:3000", 0, WithAIGateway(gateway))
	req := httptest.NewRequest(http.MethodPost, "/v1/ai/copilots/content", io.NopCloser(strings.NewReader(`{"brief":"welcome back","locale":"en-US"}`)))
	req.Header.Set("Authorization", "Bearer test-key")
	res := httptest.NewRecorder()
	handler.ServeHTTP(res, req)
	if res.Code != http.StatusCreated {
		t.Fatalf("content copilot status = %d, body = %s", res.Code, res.Body.String())
	}
	if store.draft.ID != "draft-template-1" || store.draft.Channel != "email" || store.draft.HTMLTemplate == nil {
		t.Fatalf("expected a new email draft, got %#v", store.draft)
	}
	if _, err := json.Marshal(store.draft); err != nil {
		t.Fatal(err)
	}
	var response map[string]any
	if err := json.Unmarshal(res.Body.Bytes(), &response); err != nil {
		t.Fatal(err)
	}
	if response["activity_id"] != "activity-1" {
		t.Fatalf("expected activity id, got %#v", response["activity_id"])
	}
}
