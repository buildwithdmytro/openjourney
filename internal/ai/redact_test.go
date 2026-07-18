package ai

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/buildwithdmytro/openjourney/internal/domain"
)

func TestRedactRestrictedFieldFailsClosed(t *testing.T) {
	_, err := Redact(map[string]any{"ssn": "123-45-6789"}, []domain.FieldClassification{{
		FieldPath: "ssn", Classification: "restricted", SendToModel: "deny",
	}}, "content")
	if err == nil || !strings.Contains(err.Error(), ErrRedactionDenied.Error()) {
		t.Fatalf("expected restricted field denial, got %v", err)
	}
}

func TestGatewayRedactsBeforeFakeProviderEgress(t *testing.T) {
	profile := NewFakeProfile()
	provider := NewHTTPModelProvider(profile)
	provider.Client.Transport = roundTripper(func(req *http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(`{"content":"{}","usage":{}}`)), Header: make(http.Header)}, nil
	})
	g := NewGateway(&mockStore{
		getConfigFunc: func(context.Context, domain.Principal) (domain.AIProviderConfig, error) {
			return domain.AIProviderConfig{Provider: "fake", Status: "active", Config: json.RawMessage(`{}`)}, nil
		},
		getUsageFunc: func(context.Context, string, string, string) (domain.AIBudgetUsage, error) {
			return domain.AIBudgetUsage{}, nil
		},
		incrementUsageFunc: func(context.Context, string, string, string, int64, int64, int64) error { return nil },
	})
	g.newProvider = func(ProviderProfile) ModelProvider { return provider }
	_, err := g.Generate(context.Background(), domain.Principal{}, GenerateRequest{
		Prompt: "Summarize the profile.", RetrievedData: map[string]any{"email": "ada@example.com", "plan": "pro"},
		Classifications: []domain.FieldClassification{{FieldPath: "email", Classification: "confidential", SendToModel: "redact"}, {FieldPath: "plan", Classification: "internal", SendToModel: "allow"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	body, err := io.ReadAll(profile.Requests[0].Body)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(body), "ada@example.com") {
		t.Fatal("redacted email reached provider request")
	}
	if !strings.Contains(string(body), redactedValue) {
		t.Fatal("redaction marker missing from provider request")
	}
}
