package postgres

import (
	"context"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"
)

func TestAPIKeyDefaultScopes_NoDrift(t *testing.T) {
	// 1. Collect non-wildcard allowed permissions from rbac.go (allowedPermissions map)
	allowedMap := make(map[string]bool)
	for key := range allowedPermissions {
		if key != "*" {
			allowedMap[key] = true
		}
	}

	// 2. Read latest migration defining api_keys.scopes DEFAULT array
	migDir := filepath.Join("internal", "postgres", "migrations")
	if _, err := os.Stat(migDir); os.IsNotExist(err) {
		migDir = "migrations"
	}
	entries, err := os.ReadDir(migDir)
	if err != nil {
		t.Fatalf("failed to read migrations dir: %v", err)
	}

	var latestContent string
	var latestFile string
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".sql") {
			continue
		}
		data, err := os.ReadFile(filepath.Join(migDir, entry.Name()))
		if err != nil {
			t.Fatalf("failed to read migration %s: %v", entry.Name(), err)
		}
		if strings.Contains(string(data), "ALTER TABLE api_keys ALTER COLUMN scopes SET DEFAULT ARRAY") {
			latestContent = string(data)
			latestFile = entry.Name()
		}
	}
	if latestContent == "" {
		t.Fatalf("could not find migration setting api_keys.scopes DEFAULT ARRAY")
	}

	// Extract content inside ARRAY[...]
	arrayRegex := regexp.MustCompile(`ARRAY\[([\s\S]*?)\]`)
	match := arrayRegex.FindStringSubmatch(latestContent)
	if len(match) < 2 {
		t.Fatalf("could not find ARRAY[...] block in %s", latestFile)
	}

	// Parse individual scope strings from ARRAY['scope1','scope2',...]
	scopeRegex := regexp.MustCompile(`'([^']+)'`)
	scopeMatches := scopeRegex.FindAllStringSubmatch(match[1], -1)

	migratedScopesMap := make(map[string]bool)
	var migratedScopes []string
	for _, m := range scopeMatches {
		if len(m) >= 2 {
			scope := m[1]
			migratedScopesMap[scope] = true
			migratedScopes = append(migratedScopes, scope)
		}
	}

	// 3. Collect permissions inserted into permissions catalog across all migration files
	insertBlockRegex := regexp.MustCompile(`(?i)INSERT INTO permissions\s*\([^)]+\)\s*VALUES\s*([\s\S]*?)(?:ON CONFLICT|;)`)
	tupleKeyRegex := regexp.MustCompile(`\(\s*'([^']+)'`)

	catalogPermissionsMap := make(map[string]bool)
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".sql") {
			continue
		}
		data, err := os.ReadFile(filepath.Join(migDir, entry.Name()))
		if err != nil {
			t.Fatalf("failed to read migration file %s: %v", entry.Name(), err)
		}

		blocks := insertBlockRegex.FindAllStringSubmatch(string(data), -1)
		for _, block := range blocks {
			if len(block) >= 2 {
				tuples := tupleKeyRegex.FindAllStringSubmatch(block[1], -1)
				for _, tMatch := range tuples {
					if len(tMatch) >= 2 && tMatch[1] != "*" {
						catalogPermissionsMap[tMatch[1]] = true
					}
				}
			}
		}
	}

	// 4. Assert zero drift: allowedPermissions == 062 default scopes array == permissions catalog
	for key := range allowedMap {
		if !migratedScopesMap[key] {
			t.Errorf("permission %q in rbac.go allowedPermissions is missing from 062 api_keys.scopes DEFAULT array", key)
		}
		if !catalogPermissionsMap[key] {
			t.Errorf("permission %q in rbac.go allowedPermissions is missing from permissions catalog migration seeds", key)
		}
	}

	for scope := range migratedScopesMap {
		if !allowedMap[scope] {
			t.Errorf("scope %q in 062 api_keys.scopes DEFAULT array is not in rbac.go allowedPermissions", scope)
		}
	}

	for key := range catalogPermissionsMap {
		if !allowedMap[key] {
			t.Errorf("permission %q in permissions catalog migrations is not in rbac.go allowedPermissions", key)
		}
	}

	if len(allowedMap) != len(migratedScopes) {
		t.Errorf("count mismatch: allowedPermissions has %d scopes, 062 DEFAULT array has %d scopes", len(allowedMap), len(migratedScopes))
	}
}

func TestAPIKeyDefaultScopes_DBIntegration(t *testing.T) {
	databaseURL := os.Getenv("OPENJOURNEY_TEST_DATABASE_URL")
	if databaseURL == "" {
		t.Skip("OPENJOURNEY_TEST_DATABASE_URL is not configured")
	}

	ctx := context.Background()
	store, err := Open(ctx, databaseURL)
	if err != nil {
		t.Fatalf("Open failed: %v", err)
	}
	defer store.Close()

	if err := store.Migrate(ctx); err != nil {
		t.Fatalf("Migrate failed: %v", err)
	}

	// Create development tenant which inserts an API key using the default scopes array
	rawKey := "oj_test_default_scopes_key_12345"
	if err := store.EnsureDevelopmentTenant(ctx, rawKey); err != nil {
		t.Fatalf("EnsureDevelopmentTenant failed: %v", err)
	}

	// Query created API key scopes
	var scopes []string
	err = store.pool.QueryRow(ctx, `SELECT scopes FROM api_keys WHERE name='Development key' ORDER BY created_at DESC LIMIT 1`).Scan(&scopes)
	if err != nil {
		t.Fatalf("querying api_key scopes failed: %v", err)
	}

	scopeMap := make(map[string]bool)
	for _, s := range scopes {
		scopeMap[s] = true
	}

	requiredScopes := []string{
		"forms:read", "forms:write", "forms:publish",
		"pages:read", "pages:write", "pages:publish",
		"assets:read", "assets:write",
		"links:read", "links:write",
		"companies:read", "companies:write",
		"stages:read", "stages:write",
		"imports:read", "imports:write",
		"extensions:read", "extensions:write", "extensions:install",
		"connectors:read", "connectors:write", "connectors:run",
		"messages:read", "messages:write",
		"flags:read", "flags:write",
		"catalogs:read", "catalogs:write",
		"reports:write",
	}

	for _, req := range requiredScopes {
		if !scopeMap[req] {
			t.Errorf("newly created default API key is missing scope: %s", req)
		}
	}

	sort.Strings(scopes)
	t.Logf("Newly created API key has %d default scopes", len(scopes))
}
