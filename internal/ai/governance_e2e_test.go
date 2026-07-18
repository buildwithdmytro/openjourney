package ai_test

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/buildwithdmytro/openjourney/internal/ai"
	"github.com/buildwithdmytro/openjourney/internal/domain"
	"github.com/buildwithdmytro/openjourney/internal/httpapi"
	"github.com/buildwithdmytro/openjourney/internal/ports"
	"github.com/buildwithdmytro/openjourney/internal/postgres"
)

type governanceProvider struct {
	responses []string
	prompts   []string
	calls     int
}

func (p *governanceProvider) Generate(_ context.Context, req ai.GenerateRequest) (*ai.GenerateResponse, error) {
	p.calls++
	p.prompts = append(p.prompts, req.Prompt)
	response := p.responses[len(p.responses)-1]
	if len(p.responses) > 1 {
		response = p.responses[0]
		p.responses = p.responses[1:]
	}
	return &ai.GenerateResponse{Content: response, Usage: ai.Usage{InputTokens: 2, OutputTokens: 3, CostCents: 1}}, nil
}

func (p *governanceProvider) Embed(context.Context, ai.EmbedRequest) (*ai.EmbedResponse, error) {
	return nil, ai.ErrNotSupported
}

func (p *governanceProvider) Moderate(context.Context, ai.ModerateRequest) (*ai.ModerateResponse, error) {
	return nil, ai.ErrNotSupported
}

type retrievalFixture struct{}

func (retrievalFixture) GetProfile(context.Context, domain.Principal, string) (domain.Profile, []domain.Consent, error) {
	return domain.Profile{ID: "profile-1", Attributes: json.RawMessage(`{"email":"ada@example.test","country":"US","ssn":"secret"}`)}, nil, nil
}

func (retrievalFixture) ListFieldClassifications(context.Context, domain.Principal, string) ([]domain.FieldClassification, error) {
	return []domain.FieldClassification{
		{ID: "country-classification", FieldPath: "country", Classification: "public", SendToModel: "allow"},
		{ID: "ssn-classification", FieldPath: "ssn", Classification: "restricted", SendToModel: "deny"},
	}, nil
}

type aiAgentStore struct {
	ports.Store
	principal domain.Principal
}

func (s aiAgentStore) Authenticate(context.Context, string) (domain.Principal, error) {
	return s.principal, nil
}

func activityCount(t *testing.T, ctx context.Context, store *postgres.Store, p domain.Principal) int {
	t.Helper()
	items, err := store.ListAIActivity(ctx, p, 100)
	if err != nil {
		t.Fatalf("list AI activity: %v", err)
	}
	return len(items)
}

func TestGovernanceE2E_11_14_1(t *testing.T) {
	databaseURL := os.Getenv("OPENJOURNEY_TEST_DATABASE_URL")
	if databaseURL == "" {
		t.Skip("OPENJOURNEY_TEST_DATABASE_URL is not configured")
	}

	ctx := context.Background()
	store, err := postgres.Open(ctx, databaseURL)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	if err := store.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	key := "ai-governance-e2e-11-14-1-" + time.Now().UTC().Format("20060102150405.000000000")
	if err := store.EnsureDevelopmentTenant(ctx, key); err != nil {
		t.Fatal(err)
	}
	principal, err := store.Authenticate(ctx, key)
	if err != nil {
		t.Fatal(err)
	}

	if _, err := store.CreateAIProviderConfig(ctx, principal, domain.AIProviderConfig{
		Provider: "fake", IsDefault: true, MonthlyBudgetCents: 100, Config: json.RawMessage(`{}`), Status: "active",
	}); err != nil {
		t.Fatal(err)
	}

	// (a) Retrieval is permission-aware and omits unclassified/unauthorized fields.
	retrieved, err := ai.RetrieveProfile(ctx, retrievalFixture{}, domain.Principal{Scopes: []string{"profiles:read"}}, "ada")
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := retrieved.Attributes["country"]; !ok {
		t.Fatalf("authorized field missing from retrieval: %+v", retrieved.Attributes)
	}
	if _, ok := retrieved.Attributes["email"]; ok {
		t.Fatalf("unauthorized email was retrieved: %+v", retrieved.Attributes)
	}
	if _, ok := retrieved.Attributes["ssn"]; ok {
		t.Fatalf("restricted SSN was retrieved: %+v", retrieved.Attributes)
	}

	gateway := ai.NewGateway(store)
	var provider *governanceProvider
	gateway.SetProviderFactory(func(ai.ProviderProfile) ai.ModelProvider { return provider })
	validSchema := json.RawMessage(`{"type":"object","properties":{"name":{"type":"string"}},"required":["name"]}`)

	// (b) Redaction happens before the fake provider sees the request.
	provider = &governanceProvider{responses: []string{`{"name":"draft"}`}}
	before := activityCount(t, ctx, store, principal)
	if _, err := gateway.Generate(ctx, principal, ai.GenerateRequest{
		Action: "ai.governance.redaction", Prompt: "Draft a message", OutputSchema: validSchema,
		RetrievedData: map[string]any{"email": "ada@example.test"}, Classifications: []domain.FieldClassification{{FieldPath: "email", Classification: "confidential", SendToModel: "allow"}},
	}); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(strings.Join(provider.prompts, "\n"), "ada@example.test") || !strings.Contains(provider.prompts[0], "[REDACTED]") {
		t.Fatalf("provider received unredacted data: %q", provider.prompts[0])
	}
	if got := activityCount(t, ctx, store, principal); got != before+1 {
		t.Fatalf("allowed invoke activity delta=%d, want 1", got-before)
	}

	// (b) Restricted data fails closed and still creates exactly one denied activity.
	provider = &governanceProvider{responses: []string{`{"name":"must not run"}`}}
	before = activityCount(t, ctx, store, principal)
	if _, err := gateway.Generate(ctx, principal, ai.GenerateRequest{
		Action: "ai.governance.restricted", Prompt: "Draft", RetrievedData: map[string]any{"ssn": "123-45-6789"},
		Classifications: []domain.FieldClassification{{FieldPath: "ssn", Classification: "restricted", SendToModel: "deny"}},
	}); !errors.Is(err, ai.ErrRedactionDenied) {
		t.Fatalf("restricted payload error=%v, want ErrRedactionDenied", err)
	}
	if provider.calls != 0 {
		t.Fatalf("restricted payload reached provider: %d calls", provider.calls)
	}
	if got := activityCount(t, ctx, store, principal); got != before+1 {
		t.Fatalf("denied redaction activity delta=%d, want 1", got-before)
	}

	// (d) Schema rejection gets one repair attempt; no draft mutation occurs before validation.
	provider = &governanceProvider{responses: []string{`{"wrong":true}`, `{"name":"repaired"}`}}
	before = activityCount(t, ctx, store, principal)
	if _, err := gateway.Generate(ctx, principal, ai.GenerateRequest{Action: "ai.governance.repair", Prompt: "Draft", OutputSchema: validSchema}); err != nil {
		t.Fatal(err)
	}
	if provider.calls != 2 {
		t.Fatalf("schema rejection calls=%d, want one initial plus one repair", provider.calls)
	}
	if got := activityCount(t, ctx, store, principal); got != before+1 {
		t.Fatalf("repair activity delta=%d, want 1", got-before)
	}

	// (e) Budget exhaustion is denied at the gateway and logged once.
	cfgs, err := store.ListAIProviderConfigs(ctx, principal)
	if err != nil || len(cfgs) == 0 {
		t.Fatalf("list provider configs: %v", err)
	}
	cfg := cfgs[0]
	cfg.MonthlyBudgetCents = 1
	if _, err := store.UpdateAIProviderConfig(ctx, principal, cfg); err != nil {
		t.Fatal(err)
	}
	period := time.Now().UTC().Format("2006-01")
	if err := store.IncrementAIBudgetUsage(ctx, principal.TenantID, principal.WorkspaceID, period, 1, 0, 0); err != nil {
		t.Fatal(err)
	}
	provider = &governanceProvider{responses: []string{`{"name":"must not run"}`}}
	before = activityCount(t, ctx, store, principal)
	if _, err := gateway.Generate(ctx, principal, ai.GenerateRequest{Action: "ai.governance.budget", Prompt: "Draft", OutputSchema: validSchema}); !errors.Is(err, ai.ErrBudgetExceeded) {
		t.Fatalf("budget error=%v, want ErrBudgetExceeded", err)
	}
	if provider.calls != 0 || activityCount(t, ctx, store, principal) != before+1 {
		t.Fatalf("budget denial provider_calls=%d activity_delta=%d", provider.calls, activityCount(t, ctx, store, principal)-before)
	}

	// (c) The human approval gate rejects an ai_agent before any publish store call.
	aiPrincipal := principal
	aiPrincipal.ActorType = "ai_agent"
	aiPrincipal.UserID = ""
	aiPrincipal.Scopes = []string{"journeys:write", "journeys:publish"}
	server := httpapi.NewWithSessionTTL(aiAgentStore{Store: store, principal: aiPrincipal}, 75, nil, "http://localhost:3000", time.Hour)
	req := httptest.NewRequest(http.MethodPost, "/v1/journeys/does-not-matter/publish", strings.NewReader(`{}`))
	req.Header.Set("Authorization", "Bearer ai-agent-token")
	res := httptest.NewRecorder()
	server.ServeHTTP(res, req)
	if res.Code != http.StatusForbidden || !strings.Contains(res.Body.String(), "human_approval_required") {
		t.Fatalf("ai_agent publish status=%d body=%s", res.Code, res.Body.String())
	}
}
