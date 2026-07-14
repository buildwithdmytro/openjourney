package channels

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"strings"
	"time"

	"github.com/buildwithdmytro/openjourney/internal/domain"
	"github.com/buildwithdmytro/openjourney/internal/ports"
)

// ProviderProfile is the per-provider strategy that drives the generic HTTPProviderAdapter.
// Each SMS/push provider implements this interface; the HTTP transport is shared.
//
// Contract:
//   - BuildRequest shapes the request body + auth + URL from the rendered message.
//     It must attach the request context so cancellation propagates.
//   - ParseResponse extracts the provider-assigned message ID and classifies errors.
//     Return a *DeliveryError with Retryable=true for transient failures (5xx/network),
//     Retryable=false for permanent failures (4xx, bad credentials, invalid payload).
//   - IsInvalidToken is push-only: return true when the provider signals the device token
//     is permanently invalid (FCM UNREGISTERED, APNs BadDeviceToken). SMS profiles
//     always return false.
type ProviderProfile interface {
	BuildRequest(ctx context.Context, msg ports.RenderedMessage) (*http.Request, error)
	ParseResponse(resp *http.Response, body []byte) (providerID string, err error)
	IsInvalidToken(resp *http.Response, body []byte) bool
}

// HTTPProviderAdapter implements ports.ChannelAdapter for any provider that speaks HTTP.
// The per-provider specifics (URL, auth, body shape, response parsing) are delegated to
// a ProviderProfile; this struct owns only the SSRF-safe transport and retry logic.
type HTTPProviderAdapter struct {
	profile ProviderProfile
	client  *http.Client
	channel string // e.g. "sms" or "push"
}

// NewHTTPProviderAdapter constructs an HTTPProviderAdapter with the given profile and
// an SSRF-guarded HTTP client (same dial-time IP validation as WebhookAdapter).
func NewHTTPProviderAdapter(profile ProviderProfile, channel string) *HTTPProviderAdapter {
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
			for _, ip := range ips {
				if IsPrivateIP(ip) {
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
		Timeout:   15 * time.Second,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if len(via) >= 5 {
				return errors.New("redirect limit exceeded")
			}
			if err := IsSafeURL(req.URL.String()); err != nil {
				return fmt.Errorf("redirect SSRF safeguard: %w", err)
			}
			return nil
		},
	}

	return &HTTPProviderAdapter{
		profile: profile,
		client:  client,
		channel: channel,
	}
}

// Send builds the provider request, executes it (with retries on 5xx/network errors),
// and returns the provider-assigned message ID on success.
func (a *HTTPProviderAdapter) Send(ctx context.Context, msg ports.RenderedMessage) (string, error) {
	req, err := a.profile.BuildRequest(ctx, msg)
	if err != nil {
		return "", &DeliveryError{Err: fmt.Errorf("build request: %w", err), Retryable: false}
	}

	resp, body, netErr := a.doRequest(req)
	if netErr != nil {
		return "", a.mapNetError(netErr)
	}

	return a.profile.ParseResponse(resp, body)
}

// doRequest executes the HTTP request and returns the response and fully-read body bytes.
// 5xx responses and network errors trigger retries (up to 3 attempts); 4xx are returned
// immediately for ParseResponse to classify as permanent failures.
func (a *HTTPProviderAdapter) doRequest(req *http.Request) (*http.Response, []byte, error) {
	// Snapshot the request body for replays on retry.
	var bodyBytes []byte
	if req.Body != nil {
		var err error
		bodyBytes, err = io.ReadAll(req.Body)
		if err != nil {
			return nil, nil, fmt.Errorf("read request body: %w", err)
		}
	}

	var (
		lastResp  *http.Response
		lastBody  []byte
		lastErr   error
		retryWait = 150 * time.Millisecond
	)

	for attempt := 1; attempt <= 3; attempt++ {
		// Restore the body for every attempt.
		if len(bodyBytes) > 0 {
			req.Body = io.NopCloser(bytes.NewReader(bodyBytes))
			req.ContentLength = int64(len(bodyBytes))
		}

		resp, err := a.client.Do(req)
		if err != nil {
			lastErr = err
			if attempt < 3 {
				select {
				case <-req.Context().Done():
					return nil, nil, req.Context().Err()
				case <-time.After(retryWait):
					retryWait *= 2
				}
			}
			continue
		}

		// Read and close the body immediately.
		respBody, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
		resp.Body.Close()
		resp.Body = io.NopCloser(bytes.NewReader(respBody))

		// 5xx → retry.
		if resp.StatusCode >= 500 {
			lastResp = resp
			lastBody = respBody
			lastErr = fmt.Errorf("provider returned %d", resp.StatusCode)
			if attempt < 3 {
				select {
				case <-req.Context().Done():
					return nil, nil, req.Context().Err()
				case <-time.After(retryWait):
					retryWait *= 2
				}
			}
			continue
		}

		// Non-5xx: hand to ParseResponse.
		lastErr = nil
		return resp, respBody, nil
	}

	// Retries exhausted.
	if lastErr != nil && lastResp == nil {
		// Pure network failure.
		return nil, nil, lastErr
	}
	// Last response was a 5xx — return it so ParseResponse can classify.
	return lastResp, lastBody, nil
}

// mapNetError converts a network-level error into a retryable DeliveryError.
func (a *HTTPProviderAdapter) mapNetError(err error) error {
	if err == nil {
		return nil
	}
	retryable := true
	var netErr net.Error
	if errors.As(err, &netErr) {
		retryable = netErr.Timeout() || netErr.Temporary()
	} else {
		s := strings.ToLower(err.Error())
		if strings.Contains(s, "connection refused") ||
			strings.Contains(s, "connection reset") ||
			strings.Contains(s, "timeout") ||
			strings.Contains(s, "temporary") {
			retryable = true
		}
	}
	return &DeliveryError{Err: err, Retryable: retryable}
}

// ValidateConfig is provider-specific; the profile should validate in BuildRequest.
func (a *HTTPProviderAdapter) ValidateConfig(_ domain.SendingIdentity) error { return nil }

// -----------------------------------------------------------------------
// FakeProviderProfile — in-process profile for unit and contract tests.
// No HTTP requests are made; responses are synthesised from ForceStatus.
// -----------------------------------------------------------------------

// FakeProviderProfile records BuildRequest calls and classifies synthetic responses.
// Set ForceStatus to control what ParseResponse returns (default 200 = success).
type FakeProviderProfile struct {
	// ForceStatus is the synthetic HTTP status ParseResponse uses (default 200).
	ForceStatus int
	// Requests captures every BuildRequest call.
	Requests []*http.Request
}

// NewFakeProviderProfile creates a FakeProviderProfile that returns HTTP 200 (success).
func NewFakeProviderProfile() *FakeProviderProfile {
	return &FakeProviderProfile{ForceStatus: 200}
}

func (f *FakeProviderProfile) BuildRequest(_ context.Context, msg ports.RenderedMessage) (*http.Request, error) {
	body, _ := json.Marshal(map[string]string{
		"endpoint": msg.Endpoint,
		"body":     msg.Body,
	})
	req, err := http.NewRequest(http.MethodPost, "https://fake.provider.invalid/send", bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	f.Requests = append(f.Requests, req)
	return req, nil
}

func (f *FakeProviderProfile) ParseResponse(resp *http.Response, body []byte) (string, error) {
	status := f.ForceStatus
	if resp != nil {
		status = resp.StatusCode
	}
	if status >= 500 {
		return "", &DeliveryError{Err: fmt.Errorf("fake provider 5xx: %d", status), Retryable: true}
	}
	if status >= 400 {
		return "", &DeliveryError{Err: fmt.Errorf("fake provider 4xx: %d", status), Retryable: false}
	}
	ep := extractEndpoint(body)
	return fmt.Sprintf("fake-provider-id-%s", ep), nil
}

func (f *FakeProviderProfile) IsInvalidToken(_ *http.Response, _ []byte) bool { return false }

// extractEndpoint pulls the "endpoint" key out of a JSON body blob.
func extractEndpoint(body []byte) string {
	var m map[string]string
	if err := json.Unmarshal(body, &m); err != nil {
		return "unknown"
	}
	if v, ok := m["endpoint"]; ok {
		return v
	}
	return "unknown"
}

// -----------------------------------------------------------------------
// HTTPGenericProfile — provider="http": plain JSON POST to any gateway URL.
// -----------------------------------------------------------------------

// HTTPGenericConfig is stored as SendingIdentity.Config for the "http" provider.
type HTTPGenericConfig struct {
	// Endpoint is the full URL of the SMS/push HTTP gateway.
	Endpoint string `json:"endpoint"`
	// BearerToken is sent as Authorization: Bearer <token>.
	BearerToken string `json:"bearer_token,omitempty"`
}

// HTTPGenericProfile implements ProviderProfile for a plain JSON HTTP gateway.
// It POSTs the RenderedMessage fields as JSON and classifies 2xx as success,
// 4xx as permanent failure, and 5xx as retryable.
type HTTPGenericProfile struct{}

func (p *HTTPGenericProfile) BuildRequest(ctx context.Context, msg ports.RenderedMessage) (*http.Request, error) {
	var cfg HTTPGenericConfig
	if len(msg.Identity.Config) > 0 && string(msg.Identity.Config) != "{}" {
		if err := json.Unmarshal(msg.Identity.Config, &cfg); err != nil {
			return nil, fmt.Errorf("invalid http provider config: %w", err)
		}
	}
	if cfg.Endpoint == "" {
		return nil, errors.New("http provider: endpoint is required in identity config")
	}
	if err := IsSafeURL(cfg.Endpoint); err != nil {
		return nil, fmt.Errorf("http provider SSRF check: %w", err)
	}

	payload, err := json.Marshal(map[string]string{
		"endpoint": msg.Endpoint,
		"body":     msg.Body,
		"channel":  msg.Channel,
	})
	if err != nil {
		return nil, fmt.Errorf("marshal payload: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, cfg.Endpoint, bytes.NewReader(payload))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", "OpenJourney-HTTP-Provider/1.0")
	if msg.IdempotencyKey != "" {
		req.Header.Set("X-Idempotency-Key", msg.IdempotencyKey)
	}
	if cfg.BearerToken != "" {
		req.Header.Set("Authorization", "Bearer "+cfg.BearerToken)
	}
	return req, nil
}

func (p *HTTPGenericProfile) ParseResponse(resp *http.Response, body []byte) (string, error) {
	if resp.StatusCode >= 500 {
		return "", &DeliveryError{
			Err:       fmt.Errorf("http provider returned %d: %s", resp.StatusCode, truncate(body, 200)),
			Retryable: true,
		}
	}
	if resp.StatusCode >= 400 {
		return "", &DeliveryError{
			Err:       fmt.Errorf("http provider returned %d: %s", resp.StatusCode, truncate(body, 200)),
			Retryable: false,
		}
	}
	return fmt.Sprintf("http-gateway-%d", resp.StatusCode), nil
}

func (p *HTTPGenericProfile) IsInvalidToken(_ *http.Response, _ []byte) bool { return false }

// truncate shortens a byte slice to at most n bytes for embedding in error messages.
func truncate(b []byte, n int) []byte {
	if len(b) > n {
		return b[:n]
	}
	return b
}
