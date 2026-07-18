package ai

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
)

// AnthropicProfile implements ProviderProfile for the Anthropic Claude API.
type AnthropicProfile struct{}

const (
	defaultAnthropicBaseURL = "https://api.anthropic.com/v1/messages"
)

func mapAnthropicModel(model string) string {
	switch model {
	case "opus", "claude-3-opus", "claude-4-opus":
		return "claude-3-opus-20240229"
	case "haiku", "claude-3-haiku", "claude-3-5-haiku", "claude-4-haiku":
		return "claude-3-5-haiku-20241022"
	case "":
		return "claude-3-opus-20240229"
	default:
		return model
	}
}

func estimateAnthropicCost(model string, inputTokens, outputTokens int) int64 {
	mapped := mapAnthropicModel(model)
	var inputPricePerMillion, outputPricePerMillion float64

	switch mapped {
	case "claude-3-opus-20240229":
		inputPricePerMillion = 1500.0 // $15.00 in cents
		outputPricePerMillion = 7500.0 // $75.00 in cents
	case "claude-3-5-haiku-20241022":
		inputPricePerMillion = 80.0   // $0.80 in cents
		outputPricePerMillion = 400.0  // $4.00 in cents
	default:
		// Default to Claude 3.5 Sonnet pricing
		inputPricePerMillion = 300.0
		outputPricePerMillion = 1500.0
	}

	cost := (float64(inputTokens)*inputPricePerMillion + float64(outputTokens)*outputPricePerMillion) / 1000000.0
	if cost > 0 && cost < 1.0 {
		return 1
	}
	return int64(cost)
}

type anthropicMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type anthropicTool struct {
	Name        string          `json:"name"`
	Description string          `json:"description,omitempty"`
	InputSchema json.RawMessage `json:"input_schema"`
}

type anthropicToolChoice struct {
	Type string `json:"type"`
	Name string `json:"name,omitempty"`
}

type anthropicRequest struct {
	Model       string                `json:"model"`
	Messages    []anthropicMessage    `json:"messages"`
	System      string                `json:"system,omitempty"`
	MaxTokens   int                   `json:"max_tokens"`
	Temperature *float64              `json:"temperature,omitempty"`
	Tools       []anthropicTool       `json:"tools,omitempty"`
	ToolChoice  *anthropicToolChoice  `json:"tool_choice,omitempty"`
}

type anthropicResponseContent struct {
	Type  string          `json:"type"`
	Text  string          `json:"text,omitempty"`
	Name  string          `json:"name,omitempty"`
	Input json.RawMessage `json:"input,omitempty"`
}

type anthropicResponseUsage struct {
	InputTokens  int `json:"input_tokens"`
	OutputTokens int `json:"output_tokens"`
}

type anthropicResponse struct {
	Content []anthropicResponseContent `json:"content"`
	Usage   anthropicResponseUsage     `json:"usage"`
}

func (a *AnthropicProfile) BuildGenerateRequest(ctx context.Context, req GenerateRequest) (*http.Request, error) {
	if req.APIKey == "" {
		return nil, fmt.Errorf("anthropic: api_key is required")
	}

	urlStr := req.BaseURL
	if urlStr == "" {
		urlStr = defaultAnthropicBaseURL
	}

	reqBody := anthropicRequest{
		Model:     mapAnthropicModel(req.Model),
		System:    req.SystemPrompt,
		MaxTokens: req.MaxTokens,
	}
	if reqBody.MaxTokens == 0 {
		reqBody.MaxTokens = 4096
	}
	if req.Temperature > 0 {
		tVal := req.Temperature
		reqBody.Temperature = &tVal
	}
	reqBody.Messages = []anthropicMessage{
		{Role: "user", Content: req.Prompt},
	}

	if len(req.OutputSchema) > 0 && string(req.OutputSchema) != "{}" {
		reqBody.Tools = []anthropicTool{
			{
				Name:        "generate_structured_output",
				Description: "Output the structured data matching the requested schema.",
				InputSchema: req.OutputSchema,
			},
		}
		reqBody.ToolChoice = &anthropicToolChoice{
			Type: "tool",
			Name: "generate_structured_output",
		}
	}

	bodyBytes, err := json.Marshal(reqBody)
	if err != nil {
		return nil, fmt.Errorf("anthropic: marshal body: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, urlStr, bytes.NewReader(bodyBytes))
	if err != nil {
		return nil, fmt.Errorf("anthropic: create request: %w", err)
	}

	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("x-api-key", req.APIKey)
	httpReq.Header.Set("anthropic-version", "2023-06-01")

	return httpReq, nil
}

func (a *AnthropicProfile) ParseGenerateResponse(resp *http.Response, body []byte) (*GenerateResponse, error) {
	if resp.StatusCode >= 400 {
		return nil, fmt.Errorf("anthropic: API error status %d: %s", resp.StatusCode, string(body))
	}

	var anthropicResp anthropicResponse
	if err := json.Unmarshal(body, &anthropicResp); err != nil {
		return nil, fmt.Errorf("anthropic: unmarshal response: %w", err)
	}

	var content string
	for _, block := range anthropicResp.Content {
		if block.Type == "tool_use" && block.Name == "generate_structured_output" {
			content = string(block.Input)
			break
		}
	}

	if content == "" {
		for _, block := range anthropicResp.Content {
			if block.Type == "text" {
				content = block.Text
				break
			}
		}
	}

	cost := estimateAnthropicCost(resp.Header.Get("x-model-override"), anthropicResp.Usage.InputTokens, anthropicResp.Usage.OutputTokens)
	if resp.Request != nil {
		// Try to read the model from request body if header is not present
		var reqBody anthropicRequest
		if reqData, err := resp.Request.GetBody(); err == nil {
			if data, err := io.ReadAll(reqData); err == nil {
				_ = json.Unmarshal(data, &reqBody)
			}
		}
		if reqBody.Model != "" {
			cost = estimateAnthropicCost(reqBody.Model, anthropicResp.Usage.InputTokens, anthropicResp.Usage.OutputTokens)
		}
	}

	return &GenerateResponse{
		Content: content,
		Usage: Usage{
			InputTokens:  anthropicResp.Usage.InputTokens,
			OutputTokens: anthropicResp.Usage.OutputTokens,
			CostCents:    cost,
		},
	}, nil
}

func (a *AnthropicProfile) BuildEmbedRequest(ctx context.Context, req EmbedRequest) (*http.Request, error) {
	return nil, ErrNotSupported
}

func (a *AnthropicProfile) ParseEmbedResponse(resp *http.Response, body []byte) (*EmbedResponse, error) {
	return nil, ErrNotSupported
}

func (a *AnthropicProfile) BuildModerateRequest(ctx context.Context, req ModerateRequest) (*http.Request, error) {
	return nil, ErrNotSupported
}

func (a *AnthropicProfile) ParseModerateResponse(resp *http.Response, body []byte) (*ModerateResponse, error) {
	return nil, ErrNotSupported
}
