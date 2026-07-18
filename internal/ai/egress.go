package ai

import (
	"context"
	"fmt"
	"net/url"
	"strings"
)

type contextKey string

const endpointAllowlistKey contextKey = "endpoint_allowlist"

// WithEndpointAllowlist returns a new context with the given endpoint allowlist.
func WithEndpointAllowlist(ctx context.Context, allowlist []string) context.Context {
	return context.WithValue(ctx, endpointAllowlistKey, allowlist)
}

// GetEndpointAllowlist retrieves the endpoint allowlist from the context.
func GetEndpointAllowlist(ctx context.Context) []string {
	if val, ok := ctx.Value(endpointAllowlistKey).([]string); ok {
		return val
	}
	return nil
}

// HostedDomains is the set of known hosted provider domains.
var HostedDomains = map[string]bool{
	"api.anthropic.com":        true,
	"api.openai.com":           true,
	"fake.ai.provider.invalid": true,
}

// IsDomainAllowed checks if the target URL's domain/host is allowed.
// A domain is allowed if:
// 1. It is a known hosted provider domain (in HostedDomains).
// 2. Or, it matches an entry in the endpointAllowlist.
func IsDomainAllowed(targetURL string, endpointAllowlist []string) error {
	parsed, err := url.Parse(targetURL)
	if err != nil {
		return fmt.Errorf("invalid URL: %w", err)
	}

	host := parsed.Host          // e.g. "localhost:11434"
	hostname := parsed.Hostname() // e.g. "localhost"

	// If in HostedDomains, it is allowed
	if HostedDomains[hostname] {
		return nil
	}

	// Check if present in endpointAllowlist
	for _, allowed := range endpointAllowlist {
		allowed = strings.TrimSpace(allowed)
		if allowed == "" {
			continue
		}
		if hostname == allowed || host == allowed {
			return nil
		}
		// Also support matching without scheme if they put scheme/URL in allowlist
		if u, err := url.Parse(allowed); err == nil && u.Host != "" {
			if hostname == u.Hostname() || host == u.Host {
				return nil
			}
		}
	}

	return fmt.Errorf("domain %q is not in the allowed hosted providers or tenant endpoint allowlist", hostname)
}
