package extension

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"github.com/buildwithdmytro/openjourney/internal/domain"
	"github.com/buildwithdmytro/openjourney/internal/ports"
)

// ResolveScopes yields the intersection of the extension version's requested scopes
// and the workspace/tenant granted scopes.
func ResolveScopes(ctx context.Context, store ports.Store, p domain.Principal, extensionID string, version int) ([]string, error) {
	// Get version to retrieve requested scopes
	ev, err := store.GetExtensionVersionByNumber(ctx, p, extensionID, version)
	if err != nil {
		return nil, fmt.Errorf("get extension version: %w", err)
	}

	// Get all grants for this extension
	grants, err := store.ListExtensionGrants(ctx, p, extensionID)
	if err != nil {
		return nil, fmt.Errorf("list extension grants: %w", err)
	}

	grantedMap := make(map[string]bool)
	for _, g := range grants {
		grantedMap[g.Scope] = true
	}

	var intersection []string
	for _, req := range ev.RequestedScopes {
		if grantedMap[req] {
			intersection = append(intersection, req)
		}
	}
	return intersection, nil
}

// resolveSecret resolves a secret key from environment variable or env_FILE path.
func resolveSecret(ref string) (string, error) {
	if ref == "" {
		return "", nil
	}
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

// ResolveConfigMap parses the raw JSON config, resolves all values with keys ending in "_ref",
// and returns a map with both the original config and the resolved keys (e.g., "api_key" containing the value of "api_key_ref").
func ResolveConfigMap(rawConfig json.RawMessage) (map[string]any, error) {
	if len(rawConfig) == 0 {
		return make(map[string]any), nil
	}
	var m map[string]any
	if err := json.Unmarshal(rawConfig, &m); err != nil {
		return nil, fmt.Errorf("unmarshal config: %w", err)
	}

	resolved := make(map[string]any)
	for k, v := range m {
		resolved[k] = v
		if strings.HasSuffix(k, "_ref") {
			if refStr, ok := v.(string); ok && refStr != "" {
				secretVal, err := resolveSecret(refStr)
				if err != nil {
					return nil, err
				}
				resolved[strings.TrimSuffix(k, "_ref")] = secretVal
			}
		}
	}
	return resolved, nil
}
