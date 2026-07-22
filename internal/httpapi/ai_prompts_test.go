package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/buildwithdmytro/openjourney/internal/domain"
	"github.com/buildwithdmytro/openjourney/internal/ports"
	"github.com/buildwithdmytro/openjourney/internal/postgres"
)

type mockPromptStore struct {
	ports.Store
	prompts        map[string]domain.Prompt
	promptVersions map[string]domain.PromptVersion
}

func newMockPromptStore() *mockPromptStore {
	return &mockPromptStore{
		prompts:        make(map[string]domain.Prompt),
		promptVersions: make(map[string]domain.PromptVersion),
	}
}

func (m *mockPromptStore) CreatePrompt(ctx context.Context, p domain.Principal, prompt domain.Prompt) (domain.Prompt, error) {
	prompt.ID = "prompt-1"
	prompt.TenantID = p.TenantID
	prompt.WorkspaceID = p.WorkspaceID
	m.prompts[prompt.ID] = prompt
	return prompt, nil
}

func (m *mockPromptStore) GetPrompt(ctx context.Context, p domain.Principal, id string) (domain.Prompt, error) {
	if item, ok := m.prompts[id]; ok {
		return item, nil
	}
	return domain.Prompt{}, postgres.ErrNotFound
}

func (m *mockPromptStore) ListPrompts(ctx context.Context, p domain.Principal) ([]domain.Prompt, error) {
	var list []domain.Prompt
	for _, item := range m.prompts {
		list = append(list, item)
	}
	return list, nil
}

func (m *mockPromptStore) UpdatePrompt(ctx context.Context, p domain.Principal, prompt domain.Prompt) (domain.Prompt, error) {
	if _, ok := m.prompts[prompt.ID]; !ok {
		return domain.Prompt{}, postgres.ErrNotFound
	}
	m.prompts[prompt.ID] = prompt
	return prompt, nil
}

func (m *mockPromptStore) DeletePrompt(ctx context.Context, p domain.Principal, id string) error {
	if _, ok := m.prompts[id]; !ok {
		return postgres.ErrNotFound
	}
	delete(m.prompts, id)
	return nil
}

func (m *mockPromptStore) CreatePromptVersion(ctx context.Context, p domain.Principal, pv domain.PromptVersion) (domain.PromptVersion, error) {
	if _, ok := m.prompts[pv.PromptID]; !ok {
		return domain.PromptVersion{}, postgres.ErrNotFound
	}
	pv.ID = "pv-1"
	pv.TenantID = p.TenantID
	pv.Version = 1
	pv.Status = "draft"
	pv.EvalStatus = "pending"
	m.promptVersions[pv.ID] = pv

	prompt := m.prompts[pv.PromptID]
	prompt.LatestVersion = 1
	m.prompts[pv.PromptID] = prompt
	return pv, nil
}

func (m *mockPromptStore) GetPromptVersion(ctx context.Context, p domain.Principal, id string) (domain.PromptVersion, error) {
	if pv, ok := m.promptVersions[id]; ok {
		return pv, nil
	}
	return domain.PromptVersion{}, postgres.ErrNotFound
}

func (m *mockPromptStore) GetPromptVersionByNumber(ctx context.Context, p domain.Principal, promptID string, version int) (domain.PromptVersion, error) {
	for _, pv := range m.promptVersions {
		if pv.PromptID == promptID && pv.Version == version {
			return pv, nil
		}
	}
	return domain.PromptVersion{}, postgres.ErrNotFound
}

func (m *mockPromptStore) ListPromptVersions(ctx context.Context, p domain.Principal, promptID string) ([]domain.PromptVersion, error) {
	var list []domain.PromptVersion
	for _, pv := range m.promptVersions {
		if pv.PromptID == promptID {
			list = append(list, pv)
		}
	}
	return list, nil
}

func (m *mockPromptStore) SetPromptVersionEvalStatus(ctx context.Context, p domain.Principal, id string, evalStatus string) error {
	pv, ok := m.promptVersions[id]
	if !ok {
		return postgres.ErrNotFound
	}
	pv.EvalStatus = evalStatus
	m.promptVersions[id] = pv
	return nil
}

func (m *mockPromptStore) PublishPromptVersion(ctx context.Context, p domain.Principal, promptID string, version int, approverUserID string, manifestKey string) (domain.PromptVersion, error) {
	if p.ActorType != "user" || p.UserID == "" {
		return domain.PromptVersion{}, postgres.ErrUnauthorized
	}
	var target *domain.PromptVersion
	for id, pv := range m.promptVersions {
		if pv.PromptID == promptID && pv.Version == version {
			t := pv
			target = &t
			if target.EvalStatus != "passed" {
				return domain.PromptVersion{}, errors.New("cannot publish version with non-passed eval status")
			}
			target.Status = "active"
			publishedBy := approverUserID
			now := time.Now()
			target.PublishedBy = &publishedBy
			target.PublishedAt = &now
			target.ManifestKey = manifestKey
			m.promptVersions[id] = *target

			prompt := m.prompts[promptID]
			prompt.CurrentVersionID = &target.ID
			m.prompts[promptID] = prompt
			break
		}
	}
	if target == nil {
		return domain.PromptVersion{}, postgres.ErrNotFound
	}
	return *target, nil
}

func TestAIPromptsRoutes(t *testing.T) {
	store := newMockPromptStore()
	handler := NewWithSessionTTL(store, 100, nil, "*", 12*time.Hour)

	// API key with only prompts:read
	readOnlyKey := "read-only-key"
	// API key with prompts:write
	writeKey := "write-key"

	// Mock token verifier / principal via middleware in Server:
	// We can test endpoint logic directly using server with realistic principals.
	s := &Server{
		store: store,
	}
	router := http.NewServeMux()
	router.HandleFunc("GET /v1/ai/prompts", s.listPrompts)
	router.HandleFunc("POST /v1/ai/prompts", s.createPrompt)
	router.HandleFunc("GET /v1/ai/prompts/{id}", s.getPrompt)
	router.HandleFunc("PUT /v1/ai/prompts/{id}", s.updatePrompt)
	router.HandleFunc("DELETE /v1/ai/prompts/{id}", s.deletePrompt)
	router.HandleFunc("GET /v1/ai/prompts/{id}/versions", s.listPromptVersions)
	router.HandleFunc("POST /v1/ai/prompts/{id}/versions", s.createPromptVersion)
	router.HandleFunc("GET /v1/ai/prompts/{id}/versions/{vid}", s.getPromptVersion)
	router.HandleFunc("POST /v1/ai/prompts/{id}/versions/{vid}/eval", s.setPromptVersionEvalStatus)
	router.HandleFunc("POST /v1/ai/prompts/{id}/versions/{vid}/publish", s.publishPromptVersion)

	// Test 1: Create prompt with non-human principal
	userP := domain.Principal{TenantID: "t-1", WorkspaceID: "w-1", ActorType: "user", UserID: "u-1"}
	agentP := domain.Principal{TenantID: "t-1", WorkspaceID: "w-1", ActorType: "ai_agent"}

	// Create prompt
	promptBody, _ := json.Marshal(map[string]any{
		"name":      "test-prompt",
		"task_type": "content_draft",
	})
	req := httptest.NewRequest("POST", "/v1/ai/prompts", bytes.NewReader(promptBody))
	req = req.WithContext(context.WithValue(req.Context(), principalKey{}, userP))
	rr := httptest.NewRecorder()
	router.ServeHTTP(rr, req)

	if rr.Code != http.StatusCreated {
		t.Fatalf("expected status 201 created, got %d: %s", rr.Code, rr.Body.String())
	}

	var createdPrompt domain.Prompt
	_ = json.NewDecoder(rr.Body).Decode(&createdPrompt)
	if createdPrompt.ID != "prompt-1" {
		t.Fatalf("expected prompt ID prompt-1, got %s", createdPrompt.ID)
	}

	// Create version
	versionBody, _ := json.Marshal(map[string]any{
		"template": "Hello {{name}}",
		"provider": "openai",
		"model":    "gpt-4o",
	})
	req = httptest.NewRequest("POST", "/v1/ai/prompts/prompt-1/versions", bytes.NewReader(versionBody))
	req = req.WithContext(context.WithValue(req.Context(), principalKey{}, userP))
	rr = httptest.NewRecorder()
	router.ServeHTTP(rr, req)

	if rr.Code != http.StatusCreated {
		t.Fatalf("expected status 201 created for version, got %d: %s", rr.Code, rr.Body.String())
	}

	// Publish before eval pass -> should fail (422)
	req = httptest.NewRequest("POST", "/v1/ai/prompts/prompt-1/versions/1/publish", nil)
	req = req.WithContext(context.WithValue(req.Context(), principalKey{}, userP))
	rr = httptest.NewRecorder()
	router.ServeHTTP(rr, req)

	if rr.Code != http.StatusUnprocessableEntity {
		t.Fatalf("expected status 422 for un-evaluated publish, got %d: %s", rr.Code, rr.Body.String())
	}

	// Publish by non-human actor -> should fail (403 human_approval_required)
	req = httptest.NewRequest("POST", "/v1/ai/prompts/prompt-1/versions/1/publish", nil)
	req = req.WithContext(context.WithValue(req.Context(), principalKey{}, agentP))
	rr = httptest.NewRecorder()
	router.ServeHTTP(rr, req)

	if rr.Code != http.StatusForbidden {
		t.Fatalf("expected status 403 for non-human publish, got %d: %s", rr.Code, rr.Body.String())
	}

	// Run eval status -> set to passed
	evalBody, _ := json.Marshal(map[string]any{
		"eval_status": "passed",
	})
	req = httptest.NewRequest("POST", "/v1/ai/prompts/prompt-1/versions/1/eval", bytes.NewReader(evalBody))
	req = req.WithContext(context.WithValue(req.Context(), principalKey{}, userP))
	rr = httptest.NewRecorder()
	router.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected status 200 OK for eval set, got %d: %s", rr.Code, rr.Body.String())
	}

	// Publish by human user after eval pass -> should succeed
	req = httptest.NewRequest("POST", "/v1/ai/prompts/prompt-1/versions/1/publish", nil)
	req = req.WithContext(context.WithValue(req.Context(), principalKey{}, userP))
	rr = httptest.NewRecorder()
	router.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected status 200 OK for publish, got %d: %s", rr.Code, rr.Body.String())
	}

	var pubVersion domain.PromptVersion
	_ = json.NewDecoder(rr.Body).Decode(&pubVersion)
	if pubVersion.Status != "active" || pubVersion.EvalStatus != "passed" {
		t.Fatalf("expected version status active + passed, got status=%s eval_status=%s", pubVersion.Status, pubVersion.EvalStatus)
	}

	_ = handler
	_ = readOnlyKey
	_ = writeKey
}
