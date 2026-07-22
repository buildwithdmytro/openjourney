package render

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/buildwithdmytro/openjourney/internal/channels"
	"github.com/buildwithdmytro/openjourney/internal/domain"
	"github.com/buildwithdmytro/openjourney/internal/ports"
)

// circuitBreakerState tracks the state of a source's circuit breaker.
type circuitBreakerState struct {
	failureCount int
	lastFailure  time.Time
	state        string // "closed", "open", "half_open"
	mu           sync.Mutex
}

// DefaultConnectedContentFetcher implements ConnectedContentFetcher with SSRF guards,
// circuit breaking, timeout, caching, and audit.
type DefaultConnectedContentFetcher struct {
	store      ports.Store
	cache      *TTLCache
	client     *http.Client
	breakers   map[string]*circuitBreakerState
	breakerMu  sync.Mutex
	maxRetries int
}

// NewDefaultConnectedContentFetcher creates a new SSRF-guarded fetcher.
func NewDefaultConnectedContentFetcher(store ports.Store, cache *TTLCache) *DefaultConnectedContentFetcher {
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
			// Validate ALL resolved IPs before dialing
			for _, ip := range ips {
				if channels.IsPrivateIP(ip) {
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
		Timeout:   15 * time.Second, // default; overridden per source
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if len(via) >= 5 {
				return fmt.Errorf("redirect limit exceeded")
			}
			// Re-check SSRF guard on redirect
			if err := channels.IsSafeURL(req.URL.String()); err != nil {
				return fmt.Errorf("redirect SSRF safeguard: %w", err)
			}
			return nil
		},
	}

	return &DefaultConnectedContentFetcher{
		store:      store,
		cache:      cache,
		client:     client,
		breakers:   make(map[string]*circuitBreakerState),
		maxRetries: 1,
	}
}

// Fetch retrieves data from a connected-content source.
// It validates the URL against enabled sources, checks SSRF, respects circuit breakers,
// caches responses, and audits all activity.
func (f *DefaultConnectedContentFetcher) Fetch(ctx context.Context, principal domain.Principal, url string, ttl int) (map[string]any, error) {
	// Build cache key
	cacheKey := fmt.Sprintf("cc:%s", url)

	// Check cache first
	if f.cache != nil {
		if cached, ok := f.cache.Get(cacheKey); ok {
			if m, ok := cached.(map[string]any); ok {
				return m, nil
			}
		}
	}

	// Parse URL
	parsed, err := parseConnectedContentURL(url)
	if err != nil {
		// Audit and fall back
		f.auditFetch(ctx, principal, url, "invalid_url", 0, 0)
		return nil, nil
	}

	// Find matching enabled source
	sources, err := f.store.ListConnectedContentSources(ctx, principal)
	if err != nil {
		f.auditFetch(ctx, principal, url, "error", 0, 0)
		return nil, nil
	}

	var source *domain.ConnectedContentSource
	for i := range sources {
		if sources[i].Enabled && f.matchesHost(parsed.Host, sources[i].AllowedHost) {
			source = &sources[i]
			break
		}
	}

	if source == nil {
		// No matching enabled source
		f.auditFetch(ctx, principal, url, "denied", 0, 0)
		return nil, nil
	}

	// Check circuit breaker
	if f.isCircuitOpen(source.ID) {
		f.auditFetch(ctx, principal, url, "circuit_open", 0, 0)
		return nil, nil
	}

	// Validate URL with IsSafeURL (blocks private IPs)
	if err := channels.IsSafeURL(url); err != nil {
		f.auditFetch(ctx, principal, url, "ssrf_blocked", 0, 0)
		return nil, nil
	}

	// Perform fetch with timeout
	start := time.Now()
	timeout := time.Duration(source.TimeoutMs) * time.Millisecond
	fetchCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	req, err := http.NewRequestWithContext(fetchCtx, "GET", url, nil)
	if err != nil {
		f.recordBreaker(source.ID, true)
		f.auditFetch(ctx, principal, url, "error", int64(time.Since(start).Milliseconds()), 0)
		return nil, nil
	}

	// Add auth header if configured
	if source.AuthSecretRef != "" && source.AuthHeaderName != "" {
		authVal, err := f.resolveAuthSecret(source.AuthSecretRef)
		if err == nil && authVal != "" {
			req.Header.Set(source.AuthHeaderName, authVal)
		}
	}

	resp, err := f.client.Do(req)
	latency := int64(time.Since(start).Milliseconds())

	if err != nil {
		f.recordBreaker(source.ID, true)
		f.auditFetch(ctx, principal, url, "error", latency, 0)
		return nil, nil
	}
	defer resp.Body.Close()

	// Check response status
	if resp.StatusCode >= 500 {
		f.recordBreaker(source.ID, true)
		f.auditFetch(ctx, principal, url, "server_error", latency, 0)
		return nil, nil
	}
	if resp.StatusCode >= 400 {
		f.recordBreaker(source.ID, false) // not transient
		f.auditFetch(ctx, principal, url, "client_error", latency, 0)
		return nil, nil
	}
	if resp.StatusCode != 200 {
		f.recordBreaker(source.ID, true)
		f.auditFetch(ctx, principal, url, "error", latency, 0)
		return nil, nil
	}

	// Success: reset breaker
	f.recordBreaker(source.ID, false)

	// Read response body (bounded to prevent memory exhaustion)
	bodyBytes, err := io.ReadAll(io.LimitReader(resp.Body, 10*1024*1024)) // 10MB limit
	if err != nil {
		f.auditFetch(ctx, principal, url, "read_error", latency, 0)
		return nil, nil
	}

	// Parse JSON
	var data map[string]any
	if err := json.Unmarshal(bodyBytes, &data); err != nil {
		f.auditFetch(ctx, principal, url, "json_error", latency, 0)
		return nil, nil
	}

	// Cache the result
	if f.cache != nil {
		cacheTTL := time.Duration(source.DefaultTTLSeconds) * time.Second
		f.cache.Set(cacheKey, data, cacheTTL)
	}

	f.auditFetch(ctx, principal, url, "success", latency, 0)
	return data, nil
}

// matchesHost returns true if the URL's host matches the allowed host.
// Supports exact match or suffix match (e.g., "api.example.com" matches "*.example.com").
func (f *DefaultConnectedContentFetcher) matchesHost(urlHost, allowedHost string) bool {
	// Strip port for comparison
	urlHostname := urlHost
	if idx := strings.LastIndex(urlHostname, ":"); idx != -1 {
		urlHostname = urlHostname[:idx]
	}

	allowedHostname := allowedHost
	if idx := strings.LastIndex(allowedHostname, ":"); idx != -1 {
		allowedHostname = allowedHostname[:idx]
	}

	// Exact match
	if urlHostname == allowedHostname {
		return true
	}

	// Suffix match (e.g., ".example.com" suffix)
	if strings.HasPrefix(allowedHostname, ".") {
		if strings.HasSuffix(urlHostname, allowedHostname) {
			return true
		}
	}

	// Wildcard suffix
	if strings.HasPrefix(allowedHostname, "*.") {
		suffix := allowedHostname[1:] // "*.example.com" -> ".example.com"
		if strings.HasSuffix(urlHostname, suffix) {
			return true
		}
	}

	return false
}

// isCircuitOpen returns true if the circuit for the source is open.
func (f *DefaultConnectedContentFetcher) isCircuitOpen(sourceID string) bool {
	f.breakerMu.Lock()
	defer f.breakerMu.Unlock()

	breaker, ok := f.breakers[sourceID]
	if !ok {
		return false
	}

	breaker.mu.Lock()
	defer breaker.mu.Unlock()

	if breaker.state == "open" {
		// Try to half-open after 30 seconds
		if time.Since(breaker.lastFailure) > 30*time.Second {
			breaker.state = "half_open"
			breaker.failureCount = 0
			return false
		}
		return true
	}

	return false
}

// recordBreaker records a fetch result (success or failure) for circuit breaking.
func (f *DefaultConnectedContentFetcher) recordBreaker(sourceID string, isFailure bool) {
	f.breakerMu.Lock()
	if _, ok := f.breakers[sourceID]; !ok {
		f.breakers[sourceID] = &circuitBreakerState{
			state:       "closed",
			failureCount: 0,
		}
	}
	f.breakerMu.Unlock()

	breaker := f.breakers[sourceID]
	breaker.mu.Lock()
	defer breaker.mu.Unlock()

	if isFailure {
		breaker.failureCount++
		breaker.lastFailure = time.Now()
		// Open circuit after 5 failures
		if breaker.failureCount >= 5 {
			breaker.state = "open"
		}
	} else {
		// Success: reset
		breaker.failureCount = 0
		breaker.state = "closed"
	}
}

// resolveAuthSecret resolves a secret reference (env var name) to its value.
func (f *DefaultConnectedContentFetcher) resolveAuthSecret(ref string) (string, error) {
	if ref == "" {
		return "", nil
	}
	// Check both env var and env var + "_FILE"
	val := os.Getenv(ref)
	file := os.Getenv(ref + "_FILE")
	if val != "" && file != "" {
		return "", fmt.Errorf("%s and %s_FILE cannot both be set", ref, ref)
	}
	if file != "" {
		content, err := os.ReadFile(file)
		if err != nil {
			return "", fmt.Errorf("read %s_FILE: %w", ref, err)
		}
		return strings.TrimSpace(string(content)), nil
	}
	return val, nil
}

// auditFetch records a connected-content fetch activity.
func (f *DefaultConnectedContentFetcher) auditFetch(ctx context.Context, principal domain.Principal, fetchURL string, decision string, latencyMs int64, costCents int64) {
	activity := domain.ExtensionActivity{
		TenantID:       principal.TenantID,
		WorkspaceID:    principal.WorkspaceID,
		ExtensionID:    "connected_content", // pseudo-extension for auditing
		ExtensionVersion: 0,
		Kind:           "connected_content",
		Invocation:     "fetch",
		DerivedScopes:  principal.Scopes,
		InputRef:       f.redactPayload([]byte(`{"url":"` + fetchURL + `"}`)), // simplified input ref
		OutputRef:      nil, // response redacted for privacy
		LatencyMs:      int(latencyMs),
		CostCents:      costCents,
		PolicyDecision: decision,
	}

	_, _ = f.store.RecordExtensionActivity(ctx, principal, activity)
}

// redactPayload creates a SHA256 content digest for audit.
func (f *DefaultConnectedContentFetcher) redactPayload(payload []byte) *string {
	if len(payload) == 0 {
		return nil
	}
	sum := sha256.Sum256(payload)
	ref := "redacted:sha256:" + hex.EncodeToString(sum[:])
	return &ref
}

// parseConnectedContentURL parses and validates a connected-content URL.
func parseConnectedContentURL(urlStr string) (*url.URL, error) {
	parsed, err := url.Parse(urlStr)
	if err != nil {
		return nil, err
	}

	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return nil, fmt.Errorf("unsupported protocol scheme: %s", parsed.Scheme)
	}

	if parsed.Host == "" {
		return nil, fmt.Errorf("URL has no host")
	}

	return parsed, nil
}
