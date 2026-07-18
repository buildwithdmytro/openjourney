package ai_test

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/buildwithdmytro/openjourney/internal/ai"
)

type mockRoundTripper func(*http.Request) (*http.Response, error)

func (m mockRoundTripper) RoundTrip(req *http.Request) (*http.Response, error) {
	return m(req)
}

func TestFakeProviderRoundTrip(t *testing.T) {
	fakeProfile := ai.NewFakeProfile()
	provider := ai.NewHTTPModelProvider(fakeProfile)

	expectedResponse := &ai.GenerateResponse{
		Content: `{"status":"ok"}`,
		Usage: ai.Usage{
			InputTokens:  12,
			OutputTokens: 24,
			CostCents:    6,
		},
	}

	provider.Client.Transport = mockRoundTripper(func(req *http.Request) (*http.Response, error) {
		if req.URL.String() != "https://fake.ai.provider.invalid/v1/generate" {
			t.Errorf("unexpected request URL: %s", req.URL.String())
		}
		if req.Header.Get("Content-Type") != "application/json" {
			t.Errorf("unexpected content type: %s", req.Header.Get("Content-Type"))
		}

		respBody, _ := json.Marshal(expectedResponse)
		return &http.Response{
			StatusCode: 200,
			Body:       io.NopCloser(bytes.NewReader(respBody)),
			Header:     make(http.Header),
		}, nil
	})

	req := ai.GenerateRequest{
		Model:        "fake-model",
		Prompt:       "hello fake",
		OutputSchema: json.RawMessage(`{"type":"object"}`),
	}
	resp, err := provider.Generate(context.Background(), req)
	if err != nil {
		t.Fatalf("Generate failed: %v", err)
	}

	if resp.Content != expectedResponse.Content {
		t.Errorf("expected Content %q, got %q", expectedResponse.Content, resp.Content)
	}
	if resp.Usage.InputTokens != expectedResponse.Usage.InputTokens {
		t.Errorf("expected InputTokens %d, got %d", expectedResponse.Usage.InputTokens, resp.Usage.InputTokens)
	}
	if resp.Usage.OutputTokens != expectedResponse.Usage.OutputTokens {
		t.Errorf("expected OutputTokens %d, got %d", expectedResponse.Usage.OutputTokens, resp.Usage.OutputTokens)
	}
	if resp.Usage.CostCents != expectedResponse.Usage.CostCents {
		t.Errorf("expected CostCents %d, got %d", expectedResponse.Usage.CostCents, resp.Usage.CostCents)
	}

	if len(fakeProfile.Requests) != 1 {
		t.Errorf("expected 1 captured request in profile, got %d", len(fakeProfile.Requests))
	}
}

func TestAnthropicProfile(t *testing.T) {
	profile := &ai.AnthropicProfile{}

	t.Run("BuildGenerateRequest", func(t *testing.T) {
		req := ai.GenerateRequest{
			Model:        "opus",
			SystemPrompt: "you are a helpful assistant",
			Prompt:       "hello claude",
			Temperature:  0.5,
			MaxTokens:    2048,
			APIKey:       "sk-ant-testkey",
			OutputSchema: json.RawMessage(`{"type":"object","properties":{"message":{"type":"string"}},"required":["message"]}`),
		}

		httpReq, err := profile.BuildGenerateRequest(context.Background(), req)
		if err != nil {
			t.Fatalf("BuildGenerateRequest failed: %v", err)
		}

		if httpReq.Method != http.MethodPost {
			t.Errorf("expected POST, got %s", httpReq.Method)
		}
		if httpReq.Header.Get("x-api-key") != "sk-ant-testkey" {
			t.Errorf("expected x-api-key header, got %s", httpReq.Header.Get("x-api-key"))
		}
		if httpReq.Header.Get("anthropic-version") != "2023-06-01" {
			t.Errorf("expected anthropic-version, got %s", httpReq.Header.Get("anthropic-version"))
		}

		var payload map[string]interface{}
		bodyBytes, _ := io.ReadAll(httpReq.Body)
		if err := json.Unmarshal(bodyBytes, &payload); err != nil {
			t.Fatalf("failed to unmarshal request body: %v", err)
		}

		if payload["model"] != "claude-3-opus-20240229" {
			t.Errorf("expected model claude-3-opus-20240229, got %v", payload["model"])
		}
		if payload["system"] != "you are a helpful assistant" {
			t.Errorf("expected system prompt, got %v", payload["system"])
		}
		if payload["max_tokens"].(float64) != 2048 {
			t.Errorf("expected max_tokens 2048, got %v", payload["max_tokens"])
		}
		if payload["temperature"].(float64) != 0.5 {
			t.Errorf("expected temperature 0.5, got %v", payload["temperature"])
		}

		tools, ok := payload["tools"].([]interface{})
		if !ok || len(tools) != 1 {
			t.Errorf("expected exactly 1 tool in tools list")
		}
		toolChoice, ok := payload["tool_choice"].(map[string]interface{})
		if !ok || toolChoice["type"] != "tool" || toolChoice["name"] != "generate_structured_output" {
			t.Errorf("expected tool_choice to be tool with name generate_structured_output")
		}
	})

	t.Run("ParseGenerateResponse", func(t *testing.T) {
		respBody := `{
			"id": "msg_01",
			"type": "message",
			"role": "assistant",
			"content": [
				{
					"type": "tool_use",
					"id": "toolu_01",
					"name": "generate_structured_output",
					"input": {
						"message": "hello from claude"
					}
				}
			],
			"model": "claude-3-opus-20240229",
			"usage": {
				"input_tokens": 100,
				"output_tokens": 200
			}
		}`

		httpReq, _ := http.NewRequest(http.MethodPost, "https://api.anthropic.com/v1/messages", strings.NewReader(`{"model":"claude-3-opus-20240229"}`))
		httpResp := &http.Response{
			StatusCode: http.StatusOK,
			Request:    httpReq,
		}

		genResp, err := profile.ParseGenerateResponse(httpResp, []byte(respBody))
		if err != nil {
			t.Fatalf("ParseGenerateResponse failed: %v", err)
		}

		expectedContent := `{"message":"hello from claude"}`
		if !strings.Contains(genResp.Content, "hello from claude") {
			t.Errorf("expected Content to contain %q, got %q", expectedContent, genResp.Content)
		}
		if genResp.Usage.InputTokens != 100 || genResp.Usage.OutputTokens != 200 {
			t.Errorf("unexpected tokens: %+v", genResp.Usage)
		}
		if genResp.Usage.CostCents <= 0 {
			t.Errorf("expected cost to be greater than 0, got %d", genResp.Usage.CostCents)
		}
	})
}

func TestOpenAIProfile(t *testing.T) {
	profile := &ai.OpenAIProfile{}

	t.Run("BuildGenerateRequest", func(t *testing.T) {
		req := ai.GenerateRequest{
			Model:        "gpt-4o",
			SystemPrompt: "system prompt",
			Prompt:       "user prompt",
			APIKey:       "sk-open-testkey",
			OutputSchema: json.RawMessage(`{"type":"object"}`),
		}

		httpReq, err := profile.BuildGenerateRequest(context.Background(), req)
		if err != nil {
			t.Fatalf("BuildGenerateRequest failed: %v", err)
		}

		if httpReq.Method != http.MethodPost {
			t.Errorf("expected POST, got %s", httpReq.Method)
		}
		if httpReq.Header.Get("Authorization") != "Bearer sk-open-testkey" {
			t.Errorf("expected Authorization Bearer header, got %s", httpReq.Header.Get("Authorization"))
		}

		var payload map[string]interface{}
		bodyBytes, _ := io.ReadAll(httpReq.Body)
		if err := json.Unmarshal(bodyBytes, &payload); err != nil {
			t.Fatalf("failed to unmarshal request body: %v", err)
		}

		if payload["model"] != "gpt-4o" {
			t.Errorf("expected model gpt-4o, got %v", payload["model"])
		}

		rf, ok := payload["response_format"].(map[string]interface{})
		if !ok || rf["type"] != "json_schema" {
			t.Errorf("expected response_format type json_schema")
		}
	})

	t.Run("ParseGenerateResponse", func(t *testing.T) {
		respBody := `{
			"choices": [
				{
					"message": {
						"role": "assistant",
						"content": "{\"result\": \"hello\"}"
					}
				}
			],
			"usage": {
				"prompt_tokens": 50,
				"completion_tokens": 100
			}
		}`

		httpReq, _ := http.NewRequest(http.MethodPost, "https://api.openai.com/v1/chat/completions", strings.NewReader(`{"model":"gpt-4o"}`))
		httpResp := &http.Response{
			StatusCode: http.StatusOK,
			Request:    httpReq,
		}

		genResp, err := profile.ParseGenerateResponse(httpResp, []byte(respBody))
		if err != nil {
			t.Fatalf("ParseGenerateResponse failed: %v", err)
		}

		if genResp.Content != `{"result": "hello"}` {
			t.Errorf("unexpected Content: %s", genResp.Content)
		}
		if genResp.Usage.InputTokens != 50 || genResp.Usage.OutputTokens != 100 {
			t.Errorf("unexpected tokens: %+v", genResp.Usage)
		}
		if genResp.Usage.CostCents <= 0 {
			t.Errorf("expected cost to be greater than 0, got %d", genResp.Usage.CostCents)
		}
	})

	t.Run("EmbedAndModerate", func(t *testing.T) {
		embedReq := ai.EmbedRequest{
			Model:  "text-embedding-3-small",
			Input:  []string{"test"},
			APIKey: "sk-test",
		}
		httpReq, err := profile.BuildEmbedRequest(context.Background(), embedReq)
		if err != nil {
			t.Fatalf("BuildEmbedRequest failed: %v", err)
		}
		if httpReq.URL.Path != "/v1/embeddings" {
			t.Errorf("unexpected embed URL path: %s", httpReq.URL.Path)
		}

		modReq := ai.ModerateRequest{
			Model:  "text-moderation-latest",
			Input:  "bad word",
			APIKey: "sk-test",
		}
		httpReq2, err := profile.BuildModerateRequest(context.Background(), modReq)
		if err != nil {
			t.Fatalf("BuildModerateRequest failed: %v", err)
		}
		if httpReq2.URL.Path != "/v1/moderations" {
			t.Errorf("unexpected moderate URL path: %s", httpReq2.URL.Path)
		}
	})
}

func TestHTTPModelProvider_SSRFGuard(t *testing.T) {
	fakeProfile := ai.NewFakeProfile()
	provider := ai.NewHTTPModelProvider(fakeProfile)

	req := ai.GenerateRequest{
		Model:   "fake-model",
		Prompt:  "hello",
		APIKey:  "key",
		BaseURL: "http://127.0.0.1:8080/generate",
	}

	_, err := provider.Generate(context.Background(), req)
	if err == nil {
		t.Fatal("expected error when attempting to dial private IP range")
	}

	if !strings.Contains(err.Error(), "forbidden socket dial to private IP range") && !strings.Contains(err.Error(), "dial tcp 127.0.0.1") && !strings.Contains(err.Error(), "connection refused") {
		if !strings.Contains(err.Error(), "forbidden socket dial to private IP range") {
			t.Errorf("expected SSRF block error, got: %v", err)
		}
	}
}
