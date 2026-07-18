package ai

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/buildwithdmytro/openjourney/internal/channels"
	"github.com/buildwithdmytro/openjourney/internal/domain"
)

var (
	ErrNotSupported = errors.New("method not supported by provider")
)

// Usage holds token usage and estimated cost in cents.
type Usage struct {
	InputTokens  int   `json:"input_tokens"`
	OutputTokens int   `json:"output_tokens"`
	CostCents    int64 `json:"cost_cents"`
}

// GenerateRequest represents the inputs to a structured model completion.
type GenerateRequest struct {
	Model           string                       `json:"model"`
	SystemPrompt    string                       `json:"system_prompt,omitempty"`
	Prompt          string                       `json:"prompt"`
	OutputSchema    json.RawMessage              `json:"output_schema,omitempty"`
	Temperature     float64                      `json:"temperature,omitempty"`
	MaxTokens       int                          `json:"max_tokens,omitempty"`
	APIKey          string                       `json:"-"`
	BaseURL         string                       `json:"-"`
	DomainValidator func([]byte) error           `json:"-"`
	RetrievedData   any                          `json:"-"`
	Classifications []domain.FieldClassification `json:"-"`
	Purpose         string                       `json:"-"`
	Action          string                       `json:"-"`
	PromptVersionID string                       `json:"-"`
	RetrievalRefs   json.RawMessage              `json:"-"`
	ToolCalls       json.RawMessage              `json:"-"`
	Classification  string                       `json:"-"`
	Timeout         time.Duration                `json:"-"`
	MaxCostCents    int64                        `json:"-"`
}

// GenerateResponse represents the result of a model completion.
type GenerateResponse struct {
	Content    string `json:"content"`
	Usage      Usage  `json:"usage"`
	ActivityID string `json:"activity_id,omitempty"`
}

// EmbedRequest represents inputs for generating text embeddings.
type EmbedRequest struct {
	Model   string   `json:"model"`
	Input   []string `json:"input"`
	APIKey  string   `json:"-"`
	BaseURL string   `json:"-"`
}

// EmbedResponse represents the embeddings result.
type EmbedResponse struct {
	Embeddings [][]float32 `json:"embeddings"`
	Usage      Usage       `json:"usage"`
}

// ModerateRequest represents inputs for moderating text.
type ModerateRequest struct {
	Model   string `json:"model"`
	Input   string `json:"input"`
	APIKey  string `json:"-"`
	BaseURL string `json:"-"`
}

// ModerateResponse represents the moderation result.
type ModerateResponse struct {
	Flagged bool  `json:"flagged"`
	Usage   Usage `json:"usage"`
}

// ModelProvider is the high-level interface for executing AI operations.
type ModelProvider interface {
	Generate(ctx context.Context, req GenerateRequest) (*GenerateResponse, error)
	Embed(ctx context.Context, req EmbedRequest) (*EmbedResponse, error)
	Moderate(ctx context.Context, req ModerateRequest) (*ModerateResponse, error)
}

// ProviderProfile is the interface implemented by each specific model provider profile.
type ProviderProfile interface {
	BuildGenerateRequest(ctx context.Context, req GenerateRequest) (*http.Request, error)
	ParseGenerateResponse(resp *http.Response, body []byte) (*GenerateResponse, error)

	BuildEmbedRequest(ctx context.Context, req EmbedRequest) (*http.Request, error)
	ParseEmbedResponse(resp *http.Response, body []byte) (*EmbedResponse, error)

	BuildModerateRequest(ctx context.Context, req ModerateRequest) (*http.Request, error)
	ParseModerateResponse(resp *http.Response, body []byte) (*ModerateResponse, error)
}

// HTTPModelProvider wraps a ProviderProfile to make SSRF-guarded HTTP calls.
type HTTPModelProvider struct {
	Profile ProviderProfile
	Client  *http.Client
}

// NewHTTPModelProvider constructs an HTTPModelProvider with the given profile.
func NewHTTPModelProvider(profile ProviderProfile) *HTTPModelProvider {
	transport := &http.Transport{
		Proxy: http.ProxyFromEnvironment,
		DialContext: func(ctx context.Context, network, addr string) (net.Conn, error) {
			host, port, err := net.SplitHostPort(addr)
			if err != nil {
				return nil, err
			}
			ips, err := net.DefaultResolver.LookupIP(ctx, "ip", host)
			if err != nil {
				return nil, err
			}
			if len(ips) == 0 {
				return nil, fmt.Errorf("no IP addresses resolved for host %s", host)
			}
			allowlist := GetEndpointAllowlist(ctx)
			isAllowlisted := false
			for _, allowed := range allowlist {
				allowed = strings.TrimSpace(allowed)
				if allowed == "" {
					continue
				}
				if host == allowed || addr == allowed {
					isAllowlisted = true
					break
				}
				if u, err := url.Parse(allowed); err == nil && u.Host != "" {
					if host == u.Hostname() || host == u.Host || addr == u.Host {
						isAllowlisted = true
						break
					}
				}
			}
			for _, ip := range ips {
				if channels.IsPrivateIP(ip) && !isAllowlisted {
					return nil, fmt.Errorf("forbidden socket dial to private IP range: %s", ip.String())
				}
			}
			dialer := &net.Dialer{
				Timeout:   5 * time.Second,
				KeepAlive: 30 * time.Second,
			}
			return dialer.DialContext(ctx, network, net.JoinHostPort(ips[0].String(), port))
		},
		MaxIdleConns:          100,
		IdleConnTimeout:       90 * time.Second,
		TLSHandshakeTimeout:   5 * time.Second,
		ExpectContinueTimeout: 1 * time.Second,
	}

	client := &http.Client{
		Transport: transport,
		Timeout:   90 * time.Second, // AI calls might take up to 90s for large generations
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if len(via) >= 5 {
				return fmt.Errorf("redirect limit exceeded")
			}
			allowlist := GetEndpointAllowlist(req.Context())
			if err := IsDomainAllowed(req.URL.String(), allowlist); err != nil {
				return fmt.Errorf("redirect SSRF safeguard: %w", err)
			}
			return nil
		},
	}

	return &HTTPModelProvider{
		Profile: profile,
		Client:  client,
	}
}

// Generate executes a structured completion.
func (h *HTTPModelProvider) Generate(ctx context.Context, req GenerateRequest) (*GenerateResponse, error) {
	httpReq, err := h.Profile.BuildGenerateRequest(ctx, req)
	if err != nil {
		return nil, err
	}
	allowlist := GetEndpointAllowlist(ctx)
	if err := IsDomainAllowed(httpReq.URL.String(), allowlist); err != nil {
		return nil, err
	}
	resp, err := h.Client.Do(httpReq.WithContext(ctx))
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	return h.Profile.ParseGenerateResponse(resp, body)
}

// Embed generates embeddings.
func (h *HTTPModelProvider) Embed(ctx context.Context, req EmbedRequest) (*EmbedResponse, error) {
	httpReq, err := h.Profile.BuildEmbedRequest(ctx, req)
	if err != nil {
		return nil, err
	}
	allowlist := GetEndpointAllowlist(ctx)
	if err := IsDomainAllowed(httpReq.URL.String(), allowlist); err != nil {
		return nil, err
	}
	resp, err := h.Client.Do(httpReq.WithContext(ctx))
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	return h.Profile.ParseEmbedResponse(resp, body)
}

// Moderate checks text safety.
func (h *HTTPModelProvider) Moderate(ctx context.Context, req ModerateRequest) (*ModerateResponse, error) {
	httpReq, err := h.Profile.BuildModerateRequest(ctx, req)
	if err != nil {
		return nil, err
	}
	allowlist := GetEndpointAllowlist(ctx)
	if err := IsDomainAllowed(httpReq.URL.String(), allowlist); err != nil {
		return nil, err
	}
	resp, err := h.Client.Do(httpReq.WithContext(ctx))
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	return h.Profile.ParseModerateResponse(resp, body)
}
