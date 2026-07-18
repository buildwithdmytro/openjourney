package ai

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
)

// OpenAIProfile implements ProviderProfile for OpenAI-compatible APIs.
type OpenAIProfile struct{}

const (
	defaultOpenAIGenerateURL = "https://api.openai.com/v1/chat/completions"
	defaultOpenAIEmbedURL    = "https://api.openai.com/v1/embeddings"
	defaultOpenAIModerateURL = "https://api.openai.com/v1/moderations"
)

func resolveOpenAIURL(baseURL, defaultURL, path string) string {
	if baseURL == "" {
		return defaultURL
	}
	if strings.Contains(baseURL, "completions") || strings.Contains(baseURL, "embeddings") || strings.Contains(baseURL, "moderations") {
		return baseURL
	}
	return strings.TrimSuffix(baseURL, "/") + path
}

func estimateOpenAICost(model string, inputTokens, outputTokens int) int64 {
	var inputPricePerMillion, outputPricePerMillion float64

	switch model {
	case "gpt-4o", "gpt-4":
		inputPricePerMillion = 250.0 // $2.50 in cents
		outputPricePerMillion = 1000.0 // $10.00 in cents
	case "gpt-4o-mini", "gpt-3.5-turbo":
		inputPricePerMillion = 15.0  // $0.15 in cents
		outputPricePerMillion = 60.0  // $0.60 in cents
	default:
		// Default to gpt-4o pricing
		inputPricePerMillion = 250.0
		outputPricePerMillion = 1000.0
	}

	cost := (float64(inputTokens)*inputPricePerMillion + float64(outputTokens)*outputPricePerMillion) / 1000000.0
	if cost > 0 && cost < 1.0 {
		return 1
	}
	return int64(cost)
}

type openAIMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type openAIResponseFormatSchema struct {
	Name   string          `json:"name"`
	Strict bool            `json:"strict"`
	Schema json.RawMessage `json:"schema"`
}

type openAIResponseFormat struct {
	Type       string                     `json:"type"`
	JSONSchema *openAIResponseFormatSchema `json:"json_schema,omitempty"`
}

type openAIRequest struct {
	Model          string                `json:"model"`
	Messages       []openAIMessage       `json:"messages"`
	Temperature    *float64              `json:"temperature,omitempty"`
	MaxTokens      int                   `json:"max_tokens,omitempty"`
	ResponseFormat *openAIResponseFormat `json:"response_format,omitempty"`
}

type openAIChoice struct {
	Message openAIMessage `json:"message"`
}

type openAIUsage struct {
	PromptTokens     int `json:"prompt_tokens"`
	CompletionTokens int `json:"completion_tokens"`
}

type openAIResponse struct {
	Choices []openAIChoice `json:"choices"`
	Usage   openAIUsage    `json:"usage"`
}

func (o *OpenAIProfile) BuildGenerateRequest(ctx context.Context, req GenerateRequest) (*http.Request, error) {
	if req.APIKey == "" {
		return nil, fmt.Errorf("openai: api_key is required")
	}

	urlStr := resolveOpenAIURL(req.BaseURL, defaultOpenAIGenerateURL, "/v1/chat/completions")

	reqBody := openAIRequest{
		Model:     req.Model,
		MaxTokens: req.MaxTokens,
	}
	if reqBody.Model == "" {
		reqBody.Model = "gpt-4o"
	}
	if req.Temperature > 0 {
		tVal := req.Temperature
		reqBody.Temperature = &tVal
	}

	messages := []openAIMessage{}
	if req.SystemPrompt != "" {
		messages = append(messages, openAIMessage{Role: "system", Content: req.SystemPrompt})
	}
	messages = append(messages, openAIMessage{Role: "user", Content: req.Prompt})
	reqBody.Messages = messages

	if len(req.OutputSchema) > 0 && string(req.OutputSchema) != "{}" {
		reqBody.ResponseFormat = &openAIResponseFormat{
			Type: "json_schema",
			JSONSchema: &openAIResponseFormatSchema{
				Name:   "structured_output",
				Strict: true,
				Schema: req.OutputSchema,
			},
		}
	}

	bodyBytes, err := json.Marshal(reqBody)
	if err != nil {
		return nil, fmt.Errorf("openai: marshal body: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, urlStr, bytes.NewReader(bodyBytes))
	if err != nil {
		return nil, fmt.Errorf("openai: create request: %w", err)
	}

	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Authorization", "Bearer "+req.APIKey)

	return httpReq, nil
}

func (o *OpenAIProfile) ParseGenerateResponse(resp *http.Response, body []byte) (*GenerateResponse, error) {
	if resp.StatusCode >= 400 {
		return nil, fmt.Errorf("openai: API error status %d: %s", resp.StatusCode, string(body))
	}

	var openAIResp openAIResponse
	if err := json.Unmarshal(body, &openAIResp); err != nil {
		return nil, fmt.Errorf("openai: unmarshal response: %w", err)
	}

	if len(openAIResp.Choices) == 0 {
		return nil, fmt.Errorf("openai: zero choices returned")
	}

	modelUsed := resp.Header.Get("x-model-override")
	if resp.Request != nil {
		var reqBody openAIRequest
		if reqData, err := resp.Request.GetBody(); err == nil {
			if data, err := io.ReadAll(reqData); err == nil {
				_ = json.Unmarshal(data, &reqBody)
			}
		}
		if reqBody.Model != "" {
			modelUsed = reqBody.Model
		}
	}

	cost := estimateOpenAICost(modelUsed, openAIResp.Usage.PromptTokens, openAIResp.Usage.CompletionTokens)

	return &GenerateResponse{
		Content: openAIResp.Choices[0].Message.Content,
		Usage: Usage{
			InputTokens:  openAIResp.Usage.PromptTokens,
			OutputTokens: openAIResp.Usage.CompletionTokens,
			CostCents:    cost,
		},
	}, nil
}

type openAIEmbedRequest struct {
	Model string   `json:"model"`
	Input []string `json:"input"`
}

type openAIEmbedData struct {
	Embedding []float32 `json:"embedding"`
	Index     int       `json:"index"`
}

type openAIEmbedResponse struct {
	Data  []openAIEmbedData `json:"data"`
	Usage openAIUsage       `json:"usage"`
}

func (o *OpenAIProfile) BuildEmbedRequest(ctx context.Context, req EmbedRequest) (*http.Request, error) {
	if req.APIKey == "" {
		return nil, fmt.Errorf("openai: api_key is required")
	}

	urlStr := resolveOpenAIURL(req.BaseURL, defaultOpenAIEmbedURL, "/v1/embeddings")

	reqBody := openAIEmbedRequest{
		Model: req.Model,
		Input: req.Input,
	}
	if reqBody.Model == "" {
		reqBody.Model = "text-embedding-3-small"
	}

	bodyBytes, err := json.Marshal(reqBody)
	if err != nil {
		return nil, fmt.Errorf("openai: marshal embed body: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, urlStr, bytes.NewReader(bodyBytes))
	if err != nil {
		return nil, fmt.Errorf("openai: create embed request: %w", err)
	}

	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Authorization", "Bearer "+req.APIKey)

	return httpReq, nil
}

func (o *OpenAIProfile) ParseEmbedResponse(resp *http.Response, body []byte) (*EmbedResponse, error) {
	if resp.StatusCode >= 400 {
		return nil, fmt.Errorf("openai: API embed error status %d: %s", resp.StatusCode, string(body))
	}

	var openAIResp openAIEmbedResponse
	if err := json.Unmarshal(body, &openAIResp); err != nil {
		return nil, fmt.Errorf("openai: unmarshal embed response: %w", err)
	}

	embeddings := make([][]float32, len(openAIResp.Data))
	for _, item := range openAIResp.Data {
		if item.Index >= 0 && item.Index < len(embeddings) {
			embeddings[item.Index] = item.Embedding
		}
	}

	// OpenAI Embeddings cost ~ $0.02 / million tokens for text-embedding-3-small
	cost := int64((float64(openAIResp.Usage.PromptTokens) * 2.0) / 1000000.0)
	if cost == 0 && openAIResp.Usage.PromptTokens > 0 {
		cost = 1
	}

	return &EmbedResponse{
		Embeddings: embeddings,
		Usage: Usage{
			InputTokens:  openAIResp.Usage.PromptTokens,
			OutputTokens: 0,
			CostCents:    cost,
		},
	}, nil
}

type openAIModerateRequest struct {
	Model string `json:"model"`
	Input string `json:"input"`
}

type openAIModerateResult struct {
	Flagged bool `json:"flagged"`
}

type openAIModerateResponse struct {
	Results []openAIModerateResult `json:"results"`
}

func (o *OpenAIProfile) BuildModerateRequest(ctx context.Context, req ModerateRequest) (*http.Request, error) {
	if req.APIKey == "" {
		return nil, fmt.Errorf("openai: api_key is required")
	}

	urlStr := resolveOpenAIURL(req.BaseURL, defaultOpenAIModerateURL, "/v1/moderations")

	reqBody := openAIModerateRequest{
		Model: req.Model,
		Input: req.Input,
	}
	if reqBody.Model == "" {
		reqBody.Model = "text-moderation-latest"
	}

	bodyBytes, err := json.Marshal(reqBody)
	if err != nil {
		return nil, fmt.Errorf("openai: marshal moderate body: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, urlStr, bytes.NewReader(bodyBytes))
	if err != nil {
		return nil, fmt.Errorf("openai: create moderate request: %w", err)
	}

	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Authorization", "Bearer "+req.APIKey)

	return httpReq, nil
}

func (o *OpenAIProfile) ParseModerateResponse(resp *http.Response, body []byte) (*ModerateResponse, error) {
	if resp.StatusCode >= 400 {
		return nil, fmt.Errorf("openai: API moderate error status %d: %s", resp.StatusCode, string(body))
	}

	var openAIResp openAIModerateResponse
	if err := json.Unmarshal(body, &openAIResp); err != nil {
		return nil, fmt.Errorf("openai: unmarshal moderate response: %w", err)
	}

	if len(openAIResp.Results) == 0 {
		return nil, fmt.Errorf("openai: zero moderation results returned")
	}

	return &ModerateResponse{
		Flagged: openAIResp.Results[0].Flagged,
		Usage: Usage{
			InputTokens:  0, // Moderation is free, so no tokens/cost returned
			OutputTokens: 0,
			CostCents:    0,
		},
	}, nil
}
