package extension

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/buildwithdmytro/openjourney/internal/channels"
	"github.com/buildwithdmytro/openjourney/internal/domain"
	"github.com/buildwithdmytro/openjourney/internal/ports"
)

var ErrCircuitOpen = errors.New("circuit breaker is open")
var ErrRateLimitExceeded = errors.New("rate limit exceeded")
var ErrBudgetExceeded = errors.New("monthly budget exceeded")

type terminalExtensionError struct{ message string }

func (e *terminalExtensionError) Error() string           { return e.message }
func (e *terminalExtensionError) TerminalOperation() bool { return true }

var ErrExtensionDisabled = &terminalExtensionError{message: "extension is disabled"}
var ErrExtensionConfigDisabled = &terminalExtensionError{message: "extension config is disabled"}

type Host struct {
	store      ports.Store
	httpClient *http.Client
	blobs      ports.BlobStore
}

func NewHost(store ports.Store) *Host {
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
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if len(via) >= 5 {
				return errors.New("redirect limit exceeded")
			}
			if err := channels.IsSafeURL(req.URL.String()); err != nil {
				return fmt.Errorf("redirect SSRF safeguard: %w", err)
			}
			return nil
		},
	}

	return &Host{
		store:      store,
		httpClient: client,
	}
}

func (h *Host) SetBlobStore(blobs ports.BlobStore) {
	h.blobs = blobs
}

func (h *Host) Invoke(ctx context.Context, principal domain.Principal, extensionID string, invocation string, input json.RawMessage) (json.RawMessage, string, error) {
	return h.invoke(ctx, principal, extensionID, invocation, "", input)
}

// InvokeWithScope invokes an extension operation that requires one manifest
// scope. The required scope is checked against the derived grant before any
// transport is reached, and the denial is audited like every other call.
func (h *Host) InvokeWithScope(ctx context.Context, principal domain.Principal, extensionID string, invocation string, requiredScope string, input json.RawMessage) (json.RawMessage, string, error) {
	return h.invoke(ctx, principal, extensionID, invocation, requiredScope, input)
}

func (h *Host) invoke(ctx context.Context, principal domain.Principal, extensionID string, invocation string, requiredScope string, input json.RawMessage) (json.RawMessage, string, error) {
	// 1. Resolve parent extension
	ext, err := h.store.GetExtension(ctx, principal, extensionID)
	if err != nil {
		// Best-effort audit of the first resolution failure. If the extension truly
		// does not exist the extension_activity FK prevents a row (there is no
		// extension to attribute the call to); a transient store failure on a real
		// extension is still recorded rather than silently dropped.
		actID, _ := h.recordActivity(ctx, principal, extensionID, 0, "unknown", invocation, &input, nil, 0, 0, "error")
		return nil, actID, fmt.Errorf("failed to get extension: %w", err)
	}

	if ext.Status == "disabled" {
		actID, _ := h.recordActivity(ctx, principal, extensionID, 0, "unknown", invocation, &input, nil, 0, 0, "circuit_open")
		return nil, actID, fmt.Errorf("%w: %s", ErrExtensionDisabled, extensionID)
	}

	// 2. Resolve version (enabled/active version)
	if ext.CurrentVersionID == nil || *ext.CurrentVersionID == "" {
		actID, _ := h.recordActivity(ctx, principal, extensionID, 0, "unknown", invocation, &input, nil, 0, 0, "error")
		return nil, actID, fmt.Errorf("extension %s has no active version published", extensionID)
	}

	ev, err := h.store.GetExtensionVersion(ctx, principal, *ext.CurrentVersionID)
	if err != nil {
		actID, _ := h.recordActivity(ctx, principal, extensionID, 0, "unknown", invocation, &input, nil, 0, 0, "error")
		return nil, actID, fmt.Errorf("failed to get extension version: %w", err)
	}

	// 3. Resolve config
	config, err := h.store.GetExtensionConfig(ctx, principal, extensionID)
	if err != nil {
		actID, _ := h.recordActivity(ctx, principal, extensionID, ev.Version, ev.Kind, invocation, &input, nil, 0, 0, "error")
		return nil, actID, fmt.Errorf("failed to get extension config: %w", err)
	}

	if config.Status == "disabled" {
		actID, _ := h.recordActivity(ctx, principal, extensionID, ev.Version, ev.Kind, invocation, &input, nil, 0, 0, "circuit_open")
		return nil, actID, fmt.Errorf("%w: %s", ErrExtensionConfigDisabled, extensionID)
	}

	// 4. Check circuit breaker state
	health, err := h.store.GetExtensionHealth(ctx, principal, extensionID)
	if err != nil {
		actID, _ := h.recordActivity(ctx, principal, extensionID, ev.Version, ev.Kind, invocation, &input, nil, 0, 0, "error")
		return nil, actID, fmt.Errorf("failed to get extension health: %w", err)
	}

	if health.State == "open" {
		if health.OpenedAt != nil && time.Since(*health.OpenedAt) > 30*time.Second {
			health.State = "half_open"
			if _, err := h.store.UpdateExtensionHealth(ctx, principal, health); err != nil {
				slog.Error("failed to update health state to half_open", "error", err)
			}
		} else {
			actID, _ := h.recordActivity(ctx, principal, extensionID, ev.Version, ev.Kind, invocation, &input, nil, 0, 0, "circuit_open")
			return nil, actID, ErrCircuitOpen
		}
	}

	// 5. Intersect scopes (grant ∩ requested)
	intersection, err := ResolveScopes(ctx, h.store, principal, extensionID, ev.Version)
	if err != nil {
		actID, _ := h.recordActivity(ctx, principal, extensionID, ev.Version, ev.Kind, invocation, &input, nil, 0, 0, "error")
		return nil, actID, fmt.Errorf("failed to resolve scopes: %w", err)
	}
	// A caller carrying scopes can only delegate the scopes it possesses. Internal
	// trusted workers omit Scopes and retain the existing system-principal path.
	if principal.Scopes != nil {
		callerScopes := make(map[string]bool, len(principal.Scopes))
		for _, scope := range principal.Scopes {
			callerScopes[scope] = true
		}
		filtered := intersection[:0]
		for _, scope := range intersection {
			if callerScopes[scope] || callerScopes["*"] {
				filtered = append(filtered, scope)
			}
		}
		intersection = filtered
	}

	derivedP := principal
	derivedP.ActorType = "extension"
	derivedP.Scopes = intersection
	if requiredScope != "" && !containsScope(intersection, requiredScope) {
		actID, _ := h.recordActivity(ctx, derivedP, extensionID, ev.Version, ev.Kind, invocation, &input, nil, 0, 0, "denied_scope")
		return nil, actID, fmt.Errorf("extension scope %q is not granted", requiredScope)
	}

	// 6. Rate limit check
	if config.RatePerMin > 0 {
		count, err := h.store.GetExtensionInvocationCountLastMin(ctx, principal.TenantID, principal.WorkspaceID, extensionID)
		if err != nil {
			actID, _ := h.recordActivity(ctx, derivedP, extensionID, ev.Version, ev.Kind, invocation, &input, nil, 0, 0, "error")
			return nil, actID, fmt.Errorf("failed to check invocation count: %w", err)
		}
		if count >= config.RatePerMin {
			actID, _ := h.recordActivity(ctx, derivedP, extensionID, ev.Version, ev.Kind, invocation, &input, nil, 0, 0, "denied_rate")
			return nil, actID, ErrRateLimitExceeded
		}
	}

	// 7. Budget check
	if config.MonthlyBudgetCents > 0 {
		period := time.Now().UTC().Format("2006-01")
		usage, err := h.store.GetExtensionBudgetUsage(ctx, principal.TenantID, principal.WorkspaceID, extensionID, period)
		if err != nil {
			actID, _ := h.recordActivity(ctx, derivedP, extensionID, ev.Version, ev.Kind, invocation, &input, nil, 0, 0, "error")
			return nil, actID, fmt.Errorf("failed to check budget: %w", err)
		}
		if usage >= config.MonthlyBudgetCents {
			actID, _ := h.recordActivity(ctx, derivedP, extensionID, ev.Version, ev.Kind, invocation, &input, nil, 0, 0, "denied_budget")
			return nil, actID, ErrBudgetExceeded
		}
	}

	// 8. Invoke based on transport
	start := time.Now()
	var output json.RawMessage
	var invokeErr error
	var decision = "allowed"

	timeoutMs := config.TimeoutMs
	if timeoutMs <= 0 {
		timeoutMs = 2000
	}
	invokeCtx, cancel := context.WithTimeout(ctx, time.Duration(timeoutMs)*time.Millisecond)
	defer cancel()

	if ev.Transport == "remote_http" {
		output, invokeErr = h.invokeRemoteHTTP(invokeCtx, derivedP, ev, config, invocation, input)
	} else if ev.Transport == "wasm" {
		if h.blobs == nil {
			invokeErr = fmt.Errorf("blob store is not configured on the extension host")
		} else if ev.WasmBlobKey == nil || *ev.WasmBlobKey == "" {
			invokeErr = fmt.Errorf("wasm blob key is missing in extension version")
		} else {
			var wasmBytes []byte
			wasmBytes, invokeErr = h.blobs.Get(invokeCtx, *ev.WasmBlobKey)
			if invokeErr == nil {
				output, invokeErr = h.invokeWasm(invokeCtx, derivedP, ev, config, invocation, input, wasmBytes)
			}
		}
	} else {
		invokeErr = fmt.Errorf("unsupported transport: %s", ev.Transport)
	}

	duration := time.Since(start).Milliseconds()

	// 9. Handle result, update health and record activity
	if invokeErr != nil {
		if errors.Is(invokeErr, context.DeadlineExceeded) || (invokeCtx != nil && errors.Is(invokeCtx.Err(), context.DeadlineExceeded)) {
			decision = "timeout"
		} else {
			decision = "error"
		}

		health.ConsecutiveFailures++
		if health.ConsecutiveFailures >= 5 {
			health.State = "open"
			now := time.Now()
			health.OpenedAt = &now
		}
		if _, err := h.store.UpdateExtensionHealth(ctx, principal, health); err != nil {
			slog.Error("failed to update health state on failure", "error", err)
		}

		actID, _ := h.recordActivity(ctx, derivedP, extensionID, ev.Version, ev.Kind, invocation, &input, nil, duration, 0, decision)
		return nil, actID, invokeErr
	}

	health.ConsecutiveFailures = 0
	health.State = "closed"
	health.OpenedAt = nil
	if _, err := h.store.UpdateExtensionHealth(ctx, principal, health); err != nil {
		slog.Error("failed to reset health state on success", "error", err)
	}

	actID, recErr := h.recordActivity(ctx, derivedP, extensionID, ev.Version, ev.Kind, invocation, &input, &output, duration, 0, decision)
	if recErr != nil {
		// A successful invocation must never be reported as success without a durable
		// audit row. Surface the audit failure so the caller cannot act on an
		// unaudited result.
		return nil, "", fmt.Errorf("extension invocation succeeded but audit write failed: %w", recErr)
	}
	return output, actID, nil
}

func containsScope(scopes []string, required string) bool {
	for _, scope := range scopes {
		if scope == required {
			return true
		}
	}
	return false
}

func (h *Host) invokeRemoteHTTP(ctx context.Context, p domain.Principal, ev domain.ExtensionVersion, cfg domain.ExtensionConfig, invocation string, input json.RawMessage) (json.RawMessage, error) {
	resolvedConfig, err := ResolveConfigMap(cfg.Config)
	if err != nil {
		return nil, fmt.Errorf("failed to resolve config: %w", err)
	}
	if err := ValidateRemoteHTTPConfig(ev.Transport, cfg.Config); err != nil {
		return nil, err
	}
	if err := validateResolvedRemoteHMAC(resolvedConfig); err != nil {
		return nil, err
	}

	baseURLVal, ok := resolvedConfig["base_url"].(string)
	if !ok || baseURLVal == "" {
		return nil, fmt.Errorf("config is missing 'base_url'")
	}

	if err := isEndpointAllowed(baseURLVal, cfg.EndpointAllowlist); err != nil {
		return nil, fmt.Errorf("SSRF / Egress allowlist validation failed: %w", err)
	}

	endpointURL := baseURLVal
	if !strings.HasSuffix(endpointURL, "/") {
		endpointURL += "/"
	}
	endpointURL += invocation

	if err := channels.IsSafeURL(endpointURL); err != nil {
		return nil, fmt.Errorf("SSRF safety validation failed: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, "POST", endpointURL, bytes.NewReader(input))
	if err != nil {
		return nil, fmt.Errorf("failed to create remote request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", "OpenJourney-Extension-Host/1.0")
	req.Header.Set("X-Extension-ID", ev.ExtensionID)
	req.Header.Set("X-Extension-Version", fmt.Sprintf("%d", ev.Version))
	req.Header.Set("X-Extension-Kind", ev.Kind)
	req.Header.Set("X-Extension-Invocation", invocation)

	hmacSecret := resolvedConfig["hmac_secret"].(string)
	mac := hmac.New(sha256.New, []byte(hmacSecret))
	mac.Write(input)
	signature := hex.EncodeToString(mac.Sum(nil))
	req.Header.Set("X-Signature", signature)

	resp, err := h.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("remote call failed: %w", err)
	}
	defer resp.Body.Close()

	bodyBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response body: %w", err)
	}

	if resp.StatusCode >= 400 {
		return nil, fmt.Errorf("remote extension returned status %d: %s", resp.StatusCode, string(bodyBytes))
	}

	return json.RawMessage(bodyBytes), nil
}

func (h *Host) recordActivity(ctx context.Context, p domain.Principal, extensionID string, version int, kind string, invocation string, input *json.RawMessage, output *json.RawMessage, latencyMs int64, costCents int64, decision string) (string, error) {
	activity := domain.ExtensionActivity{
		TenantID:         p.TenantID,
		WorkspaceID:      p.WorkspaceID,
		ExtensionID:      extensionID,
		ExtensionVersion: version,
		Kind:             kind,
		Invocation:       invocation,
		DerivedScopes:    p.Scopes,
		InputRef:         redactActivityPayload(input),
		OutputRef:        redactActivityPayload(output),
		LatencyMs:        int(latencyMs),
		CostCents:        costCents,
		PolicyDecision:   decision,
	}

	stored, err := h.store.RecordExtensionActivity(ctx, p, activity)
	if err != nil {
		return "", err
	}
	return stored.ID, nil
}

// redactActivityPayload keeps raw request/response payloads — which may carry
// secrets — out of the append-only, un-deletable extension_activity audit log.
// It stores a content-digest reference instead of the verbatim JSON so a call
// can still be correlated by payload identity without persisting the payload.
func redactActivityPayload(raw *json.RawMessage) *string {
	if raw == nil {
		return nil
	}
	sum := sha256.Sum256(*raw)
	ref := "redacted:sha256:" + hex.EncodeToString(sum[:])
	return &ref
}

func isEndpointAllowed(targetURL string, allowlist []string) error {
	parsed, err := url.Parse(targetURL)
	if err != nil {
		return fmt.Errorf("invalid URL: %w", err)
	}

	host := parsed.Host           // e.g. "localhost:11434"
	hostname := parsed.Hostname() // e.g. "localhost"

	for _, allowed := range allowlist {
		allowed = strings.TrimSpace(allowed)
		if allowed == "" {
			continue
		}
		if hostname == allowed || host == allowed {
			return nil
		}
		if u, err := url.Parse(allowed); err == nil && u.Host != "" {
			if hostname == u.Hostname() || host == u.Host {
				return nil
			}
		}
	}
	return fmt.Errorf("endpoint %q is not in the allowlist", targetURL)
}
