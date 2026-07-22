package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
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

type aiDepthE2EStore struct {
	ports.Store
	prompts            map[string]domain.Prompt
	promptVersions     map[string]domain.PromptVersion
	recordedActivities []domain.AIActivity
}

func newAIDepthE2EStore() *aiDepthE2EStore {
	return &aiDepthE2EStore{
		prompts:        make(map[string]domain.Prompt),
		promptVersions: make(map[string]domain.PromptVersion),
	}
}

func (s *aiDepthE2EStore) Authenticate(context.Context, string) (domain.Principal, error) {
	return domain.Principal{
		TenantID:    "t-e2e",
		WorkspaceID: "w-e2e",
		UserID:      "u-human-1",
		ActorType:   "user",
		Scopes:      []string{"ai:invoke", "prompts:read", "prompts:write", "reports:read", "catalogs:read"},
	}, nil
}

func (s *aiDepthE2EStore) CreatePrompt(ctx context.Context, p domain.Principal, prompt domain.Prompt) (domain.Prompt, error) {
	prompt.ID = "prompt-e2e-1"
	prompt.TenantID = p.TenantID
	prompt.WorkspaceID = p.WorkspaceID
	s.prompts[prompt.ID] = prompt
	return prompt, nil
}

func (s *aiDepthE2EStore) GetPrompt(ctx context.Context, p domain.Principal, id string) (domain.Prompt, error) {
	if item, ok := s.prompts[id]; ok {
		return item, nil
	}
	return domain.Prompt{}, postgres.ErrNotFound
}

func (s *aiDepthE2EStore) GetPromptByName(ctx context.Context, p domain.Principal, name string) (domain.Prompt, error) {
	for _, prompt := range s.prompts {
		if prompt.Name == name {
			return prompt, nil
		}
	}
	if name == "analytics-insight" {
		versionID := "pv-analytics-insight-1"
		return domain.Prompt{ID: "prompt-ai-1", Name: "analytics-insight", CurrentVersionID: &versionID, TaskType: "analytics_insight"}, nil
	}
	return domain.Prompt{}, postgres.ErrNotFound
}

func (s *aiDepthE2EStore) ListPrompts(ctx context.Context, p domain.Principal) ([]domain.Prompt, error) {
	var list []domain.Prompt
	for _, item := range s.prompts {
		list = append(list, item)
	}
	return list, nil
}

func (s *aiDepthE2EStore) UpdatePrompt(ctx context.Context, p domain.Principal, prompt domain.Prompt) (domain.Prompt, error) {
	if _, ok := s.prompts[prompt.ID]; !ok {
		return domain.Prompt{}, postgres.ErrNotFound
	}
	s.prompts[prompt.ID] = prompt
	return prompt, nil
}

func (s *aiDepthE2EStore) CreatePromptVersion(ctx context.Context, p domain.Principal, pv domain.PromptVersion) (domain.PromptVersion, error) {
	if _, ok := s.prompts[pv.PromptID]; !ok {
		return domain.PromptVersion{}, postgres.ErrNotFound
	}
	pv.ID = "pv-e2e-1"
	pv.TenantID = p.TenantID
	pv.Version = 1
	pv.Status = "draft"
	pv.EvalStatus = "pending"
	s.promptVersions[pv.ID] = pv

	prompt := s.prompts[pv.PromptID]
	prompt.LatestVersion = 1
	s.prompts[pv.PromptID] = prompt
	return pv, nil
}

func (s *aiDepthE2EStore) GetPromptVersion(ctx context.Context, p domain.Principal, id string) (domain.PromptVersion, error) {
	if id == "pv-analytics-insight-1" {
		return domain.PromptVersion{
			ID:           "pv-analytics-insight-1",
			PromptID:     "prompt-ai-1",
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

func (s *aiDepthE2EStore) GetPromptVersionByNumber(ctx context.Context, p domain.Principal, promptID string, version int) (domain.PromptVersion, error) {
	for _, pv := range s.promptVersions {
		if pv.PromptID == promptID && pv.Version == version {
			return pv, nil
		}
	}
	return domain.PromptVersion{}, postgres.ErrNotFound
}

func (s *aiDepthE2EStore) SetPromptVersionEvalStatus(ctx context.Context, p domain.Principal, id string, evalStatus string) error {
	pv, ok := s.promptVersions[id]
	if !ok {
		return postgres.ErrNotFound
	}
	pv.EvalStatus = evalStatus
	s.promptVersions[id] = pv
	return nil
}

func (s *aiDepthE2EStore) PublishPromptVersion(ctx context.Context, p domain.Principal, promptID string, version int, approverUserID string, manifestKey string) (domain.PromptVersion, error) {
	if p.ActorType != "user" || p.UserID == "" {
		return domain.PromptVersion{}, postgres.ErrUnauthorized
	}
	var target *domain.PromptVersion
	for id, pv := range s.promptVersions {
		if pv.PromptID == promptID && pv.Version == version {
			t := pv
			target = &t
			if target.EvalStatus != "passed" {
				return domain.PromptVersion{}, postgres.ErrUnauthorized
			}
			target.Status = "active"
			publishedBy := approverUserID
			now := time.Now()
			target.PublishedBy = &publishedBy
			target.PublishedAt = &now
			target.ManifestKey = manifestKey
			s.promptVersions[id] = *target

			prompt := s.prompts[promptID]
			prompt.CurrentVersionID = &target.ID
			s.prompts[promptID] = prompt
			break
		}
	}
	if target == nil {
		return domain.PromptVersion{}, postgres.ErrNotFound
	}
	return *target, nil
}

func (s *aiDepthE2EStore) GetDefaultAIProviderConfig(context.Context, domain.Principal) (domain.AIProviderConfig, error) {
	return domain.AIProviderConfig{Provider: "fake", Status: "active", Config: json.RawMessage(`{}`)}, nil
}

func (s *aiDepthE2EStore) GetAIBudgetUsage(context.Context, string, string, string) (domain.AIBudgetUsage, error) {
	return domain.AIBudgetUsage{}, nil
}

func (s *aiDepthE2EStore) IncrementAIBudgetUsage(context.Context, string, string, string, int64, int64, int64) error {
	return nil
}

func (s *aiDepthE2EStore) RecordToolCall(ctx context.Context, call tools.ToolCall) error {
	return nil
}

func (s *aiDepthE2EStore) RecordAIActivity(_ context.Context, _ domain.Principal, activity domain.AIActivity) (domain.AIActivity, error) {
	activity.ID = "activity-e2e-" + activity.Action
	s.recordedActivities = append(s.recordedActivities, activity)
	return activity, nil
}

func (s *aiDepthE2EStore) FunnelOverTimeReport(context.Context, domain.Principal, string, domain.ReportQuery) (domain.FunnelOverTimeReport, error) {
	return domain.FunnelOverTimeReport{
		CampaignID: "camp-99",
		Buckets: []domain.TimeBucket{
			{
				Funnel: domain.ReportFunnel{
					Sent: domain.ReportCount{Total: 1500},
				},
			},
		},
	}, nil
}

type aiDepthE2EProvider struct{}

func (p aiDepthE2EProvider) Generate(_ context.Context, req ai.GenerateRequest) (*ai.GenerateResponse, error) {
	if req.Action == "ai.agent.step" {
		if strings.Contains(req.Prompt, "1500") {
			return &ai.GenerateResponse{
				Content: `{"action":"final","answer":"{\"summary\":\"Total conversion reached 1500 messages.\",\"insights\":[\"Campaign volume is strong.\"],\"key_metrics\":[{\"name\":\"sent_total\",\"value\":1500,\"source\":\"report.timeseries\"}]}"}`,
				Usage:   ai.Usage{InputTokens: 20, OutputTokens: 30, CostCents: 1},
			}, nil
		}
		return &ai.GenerateResponse{
			Content: `{"action":"tool","tool":"report.timeseries","args":{"report_type":"funnel_over_time","campaign_id":"camp-99"}}`,
			Usage:   ai.Usage{InputTokens: 20, OutputTokens: 30, CostCents: 1},
		}, nil
	}

	return &ai.GenerateResponse{
		Content: `{"status":"ok"}`,
		Usage:   ai.Usage{InputTokens: 5, OutputTokens: 5, CostCents: 1},
	}, nil
}

func (aiDepthE2EProvider) Embed(context.Context, ai.EmbedRequest) (*ai.EmbedResponse, error) {
	return nil, ai.ErrNotSupported
}

func (aiDepthE2EProvider) Moderate(context.Context, ai.ModerateRequest) (*ai.ModerateResponse, error) {
	return nil, ai.ErrNotSupported
}

func TestAIDepthE2E(t *testing.T) {
	store := newAIDepthE2EStore()
	gw := ai.NewGateway(store)
	gw.SetProviderFactory(func(ai.ProviderProfile) ai.ModelProvider {
		return aiDepthE2EProvider{}
	})

	server := &Server{
		store:     store,
		aiGateway: gw,
	}

	router := http.NewServeMux()
	router.HandleFunc("POST /v1/ai/copilots/insights", server.createInsightsCopilot)
	router.HandleFunc("POST /v1/ai/prompts", server.createPrompt)
	router.HandleFunc("POST /v1/ai/prompts/{id}/versions", server.createPromptVersion)
	router.HandleFunc("POST /v1/ai/prompts/{id}/versions/{vid}/eval", server.setPromptVersionEvalStatus)
	router.HandleFunc("POST /v1/ai/prompts/{id}/versions/{vid}/publish", server.publishPromptVersion)

	t.Run("AgentAnswer_BoundedMultiStepLoop", func(t *testing.T) {
		runner := tools.NewRunner(store, store)
		readTools := tools.ReadOnlyTools()
		for _, tool := range readTools {
			_ = runner.Register(tool)
		}

		ag := agent.NewAgent(gw, runner, readTools, agent.Config{
			MaxSteps: 6,
			Timeout:  10 * time.Second,
		})

		userP := domain.Principal{
			TenantID:    "t-e2e",
			WorkspaceID: "w-e2e",
			UserID:      "u-human-1",
			ActorType:   "user",
			Scopes:      []string{"reports:read"},
		}

		res, err := ag.Run(context.Background(), userP, "What is the funnel performance for camp-99?")
		if err != nil {
			t.Fatalf("agent run failed: %v", err)
		}

		if res.Status != "completed" {
			t.Fatalf("expected status completed, got %s", res.Status)
		}
		if len(res.Trace) < 1 {
			t.Fatalf("expected at least 1 step trace, got %d", len(res.Trace))
		}
		if res.Trace[0].Tool != "report.timeseries" {
			t.Fatalf("expected tool report.timeseries, got %s", res.Trace[0].Tool)
		}
		if !strings.Contains(res.Answer, "1500") {
			t.Fatalf("expected answer to contain 1500, got: %s", res.Answer)
		}
	})

	t.Run("InsightsGrounded_EndToEnd", func(t *testing.T) {
		userP := domain.Principal{
			TenantID:    "t-e2e",
			WorkspaceID: "w-e2e",
			UserID:      "u-human-1",
			ActorType:   "user",
			Scopes:      []string{"ai:invoke", "reports:read"},
		}

		body, _ := json.Marshal(map[string]any{
			"question": "Give me analytics insights for camp-99",
		})
		req := httptest.NewRequest("POST", "/v1/ai/copilots/insights", bytes.NewReader(body))
		req = req.WithContext(context.WithValue(req.Context(), principalKey{}, userP))
		rr := httptest.NewRecorder()

		router.ServeHTTP(rr, req)

		if rr.Code != http.StatusOK {
			t.Fatalf("expected 200 OK, got %d: %s", rr.Code, rr.Body.String())
		}

		var resp map[string]any
		if err := json.Unmarshal(rr.Body.Bytes(), &resp); err != nil {
			t.Fatalf("failed to decode response: %v", err)
		}

		if resp["summary"] == nil || resp["summary"] == "" {
			t.Fatalf("expected non-empty summary")
		}
		keyMetrics, ok := resp["key_metrics"].([]any)
		if !ok || len(keyMetrics) == 0 {
			t.Fatalf("expected non-empty key_metrics")
		}

		trace, ok := resp["trace"].([]any)
		if !ok || len(trace) == 0 {
			t.Fatalf("expected non-empty trace in response")
		}
	})

	t.Run("PromptPublishUse_HumanAndEvalGatedFlow", func(t *testing.T) {
		userP := domain.Principal{
			TenantID:    "t-e2e",
			WorkspaceID: "w-e2e",
			UserID:      "u-human-1",
			ActorType:   "user",
			Scopes:      []string{"prompts:read", "prompts:write"},
		}

		// Step 1: Author prompt
		pBody, _ := json.Marshal(map[string]any{
			"name":      "e2e-content-prompt",
			"task_type": "content_draft",
		})
		req1 := httptest.NewRequest("POST", "/v1/ai/prompts", bytes.NewReader(pBody))
		req1 = req1.WithContext(context.WithValue(req1.Context(), principalKey{}, userP))
		rr1 := httptest.NewRecorder()
		router.ServeHTTP(rr1, req1)

		if rr1.Code != http.StatusCreated {
			t.Fatalf("create prompt failed (%d): %s", rr1.Code, rr1.Body.String())
		}

		var createdPrompt domain.Prompt
		_ = json.NewDecoder(rr1.Body).Decode(&createdPrompt)

		// Step 2: Create prompt version
		vBody, _ := json.Marshal(map[string]any{
			"model":    "gpt-4o",
			"template": "Draft content for {{topic}}",
		})
		req2 := httptest.NewRequest("POST", "/v1/ai/prompts/"+createdPrompt.ID+"/versions", bytes.NewReader(vBody))
		req2 = req2.WithContext(context.WithValue(req2.Context(), principalKey{}, userP))
		rr2 := httptest.NewRecorder()
		router.ServeHTTP(rr2, req2)

		if rr2.Code != http.StatusCreated {
			t.Fatalf("create prompt version failed (%d): %s", rr2.Code, rr2.Body.String())
		}

		var createdVersion domain.PromptVersion
		_ = json.NewDecoder(rr2.Body).Decode(&createdVersion)

		// Step 3: Attempt publish before eval passes -> must fail
		pubBody, _ := json.Marshal(map[string]any{"version": 1})
		req3 := httptest.NewRequest("POST", "/v1/ai/prompts/"+createdPrompt.ID+"/versions/1/publish", bytes.NewReader(pubBody))
		req3 = req3.WithContext(context.WithValue(req3.Context(), principalKey{}, userP))
		rr3 := httptest.NewRecorder()
		router.ServeHTTP(rr3, req3)

		if rr3.Code != http.StatusUnprocessableEntity {
			t.Fatalf("expected 422 for publish without eval pass, got %d: %s", rr3.Code, rr3.Body.String())
		}

		// Step 4: Run eval (pass)
		evalBody, _ := json.Marshal(map[string]any{"eval_status": "passed"})
		req4 := httptest.NewRequest("POST", "/v1/ai/prompts/"+createdPrompt.ID+"/versions/1/eval", bytes.NewReader(evalBody))
		req4 = req4.WithContext(context.WithValue(req4.Context(), principalKey{}, userP))
		rr4 := httptest.NewRecorder()
		router.ServeHTTP(rr4, req4)

		if rr4.Code != http.StatusOK {
			t.Fatalf("eval status update failed (%d): %s", rr4.Code, rr4.Body.String())
		}

		// Step 5: Non-human principal publish attempt -> must fail 403
		agentP := domain.Principal{
			TenantID:    "t-e2e",
			WorkspaceID: "w-e2e",
			ActorType:   "ai_agent",
			Scopes:      []string{"prompts:write"},
		}
		req5 := httptest.NewRequest("POST", "/v1/ai/prompts/"+createdPrompt.ID+"/versions/1/publish", bytes.NewReader(pubBody))
		req5 = req5.WithContext(context.WithValue(req5.Context(), principalKey{}, agentP))
		rr5 := httptest.NewRecorder()
		router.ServeHTTP(rr5, req5)

		if rr5.Code != http.StatusForbidden {
			t.Fatalf("expected 403 for non-human publish, got %d: %s", rr5.Code, rr5.Body.String())
		}

		// Step 6: Human publish after eval pass -> succeeds
		req6 := httptest.NewRequest("POST", "/v1/ai/prompts/"+createdPrompt.ID+"/versions/1/publish", bytes.NewReader(pubBody))
		req6 = req6.WithContext(context.WithValue(req6.Context(), principalKey{}, userP))
		rr6 := httptest.NewRecorder()
		router.ServeHTTP(rr6, req6)

		if rr6.Code != http.StatusOK {
			t.Fatalf("human publish failed (%d): %s", rr6.Code, rr6.Body.String())
		}

		var publishedVersion domain.PromptVersion
		_ = json.NewDecoder(rr6.Body).Decode(&publishedVersion)
		if publishedVersion.Status != "active" {
			t.Fatalf("expected published status active, got %s", publishedVersion.Status)
		}

		// Step 7: Verify version is active and usable
		pv, err := store.GetPromptVersion(context.Background(), userP, publishedVersion.ID)
		if err != nil {
			t.Fatalf("failed to retrieve prompt version: %v", err)
		}
		if pv.Status != "active" || pv.EvalStatus != "passed" {
			t.Fatalf("published version is not usable: status=%s eval=%s", pv.Status, pv.EvalStatus)
		}
	})
}
