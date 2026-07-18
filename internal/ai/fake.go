package ai

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
)

// DeterministicFakeProvider is the provider used by local governed flows and
// tests. It never makes network calls and returns a stable content-draft
// envelope that exercises the gateway's schema gate.
type DeterministicFakeProvider struct{}

func (DeterministicFakeProvider) Generate(context.Context, GenerateRequest) (*GenerateResponse, error) {
	return &GenerateResponse{Content: `{"subject":"A thoughtful next step","body":"Discover what is next for you.","title":"A thoughtful next step","push_data":{},"localizations":{},"qa":{"passed":true,"issues":[]}}`, Usage: Usage{InputTokens: 32, OutputTokens: 28, CostCents: 1}}, nil
}
func (DeterministicFakeProvider) Embed(context.Context, EmbedRequest) (*EmbedResponse, error) {
	return &EmbedResponse{Embeddings: [][]float32{{0.1, 0.2, 0.3}}, Usage: Usage{InputTokens: 1}}, nil
}
func (DeterministicFakeProvider) Moderate(context.Context, ModerateRequest) (*ModerateResponse, error) {
	return &ModerateResponse{Usage: Usage{InputTokens: 1}}, nil
}

// FakeProfile implements ProviderProfile for tests.
type FakeProfile struct {
	Requests []*http.Request
}

// NewFakeProfile creates an initialized FakeProfile.
func NewFakeProfile() *FakeProfile {
	return &FakeProfile{
		Requests: make([]*http.Request, 0),
	}
}

func (f *FakeProfile) BuildGenerateRequest(ctx context.Context, req GenerateRequest) (*http.Request, error) {
	body, _ := json.Marshal(req)
	urlStr := "https://fake.ai.provider.invalid/v1/generate"
	if req.BaseURL != "" {
		urlStr = req.BaseURL
	}
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, urlStr, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	httpReq.Header.Set("Content-Type", "application/json")
	f.Requests = append(f.Requests, httpReq)
	return httpReq, nil
}

func (f *FakeProfile) ParseGenerateResponse(resp *http.Response, body []byte) (*GenerateResponse, error) {
	if resp.StatusCode >= 500 {
		return nil, fmt.Errorf("fake provider server error status: %d", resp.StatusCode)
	}
	if resp.StatusCode >= 400 {
		return nil, fmt.Errorf("fake provider client error status: %d", resp.StatusCode)
	}

	if len(body) == 0 {
		return &GenerateResponse{
			Content: `{"status": "success"}`,
			Usage: Usage{
				InputTokens:  10,
				OutputTokens: 20,
				CostCents:    5,
			},
		}, nil
	}

	var res GenerateResponse
	if err := json.Unmarshal(body, &res); err != nil {
		// Fallback: return raw body as text content
		return &GenerateResponse{
			Content: string(body),
			Usage: Usage{
				InputTokens:  10,
				OutputTokens: 20,
				CostCents:    5,
			},
		}, nil
	}
	return &res, nil
}

func (f *FakeProfile) BuildEmbedRequest(ctx context.Context, req EmbedRequest) (*http.Request, error) {
	body, _ := json.Marshal(req)
	urlStr := "https://fake.ai.provider.invalid/v1/embeddings"
	if req.BaseURL != "" {
		urlStr = req.BaseURL
	}
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, urlStr, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	httpReq.Header.Set("Content-Type", "application/json")
	return httpReq, nil
}

func (f *FakeProfile) ParseEmbedResponse(resp *http.Response, body []byte) (*EmbedResponse, error) {
	if resp.StatusCode >= 400 {
		return nil, fmt.Errorf("fake provider error status: %d", resp.StatusCode)
	}
	if len(body) == 0 {
		return &EmbedResponse{
			Embeddings: [][]float32{{0.1, 0.2, 0.3}},
			Usage: Usage{
				InputTokens:  5,
				OutputTokens: 0,
				CostCents:    1,
			},
		}, nil
	}
	var res EmbedResponse
	if err := json.Unmarshal(body, &res); err != nil {
		return nil, err
	}
	return &res, nil
}

func (f *FakeProfile) BuildModerateRequest(ctx context.Context, req ModerateRequest) (*http.Request, error) {
	body, _ := json.Marshal(req)
	urlStr := "https://fake.ai.provider.invalid/v1/moderations"
	if req.BaseURL != "" {
		urlStr = req.BaseURL
	}
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, urlStr, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	httpReq.Header.Set("Content-Type", "application/json")
	return httpReq, nil
}

func (f *FakeProfile) ParseModerateResponse(resp *http.Response, body []byte) (*ModerateResponse, error) {
	if resp.StatusCode >= 400 {
		return nil, fmt.Errorf("fake provider error status: %d", resp.StatusCode)
	}
	if len(body) == 0 {
		return &ModerateResponse{
			Flagged: false,
			Usage: Usage{
				InputTokens:  5,
				OutputTokens: 0,
				CostCents:    1,
			},
		}, nil
	}
	var res ModerateResponse
	if err := json.Unmarshal(body, &res); err != nil {
		return nil, err
	}
	return &res, nil
}
