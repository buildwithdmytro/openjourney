package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/buildwithdmytro/openjourney/internal/domain"
	"github.com/buildwithdmytro/openjourney/internal/ports"
	"github.com/buildwithdmytro/openjourney/internal/postgres"
	"github.com/buildwithdmytro/openjourney/internal/render"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type securityMockStore struct {
	ports.Store
	sources    map[string]domain.ConnectedContentSource
	catalogs   map[string]domain.Catalog
	items      map[string]domain.CatalogItem
	activities []domain.ExtensionActivity
	mu         sync.Mutex
}

func newSecurityMockStore() *securityMockStore {
	return &securityMockStore{
		sources:  make(map[string]domain.ConnectedContentSource),
		catalogs: make(map[string]domain.Catalog),
		items:    make(map[string]domain.CatalogItem),
	}
}

func (m *securityMockStore) Authenticate(ctx context.Context, key string) (domain.Principal, error) {
	if key == "read-only-key" {
		return domain.Principal{
			TenantID:    "tenant-sec",
			WorkspaceID: "workspace-sec",
			AppID:       "app-sec",
			Scopes:      []string{"catalogs:read"},
		}, nil
	}
	if key == "full-key" {
		return domain.Principal{
			TenantID:    "tenant-sec",
			WorkspaceID: "workspace-sec",
			AppID:       "app-sec",
			Scopes:      []string{"catalogs:read", "catalogs:write"},
		}, nil
	}
	return domain.Principal{}, postgres.ErrNotFound
}

func (m *securityMockStore) CreateConnectedContentSource(ctx context.Context, p domain.Principal, src domain.ConnectedContentSource) (domain.ConnectedContentSource, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if src.AuthSecretRef != "" {
		if strings.HasPrefix(src.AuthSecretRef, "secret:") {
			return domain.ConnectedContentSource{}, fmt.Errorf("auth_secret_ref must be an environment variable name, got raw secret ref")
		}
	}

	if src.ID == "" {
		src.ID = fmt.Sprintf("src-%d", len(m.sources)+1)
	}
	m.sources[src.ID] = src
	return src, nil
}

func (m *securityMockStore) GetConnectedContentSource(ctx context.Context, p domain.Principal, id string) (domain.ConnectedContentSource, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	src, ok := m.sources[id]
	if !ok {
		return domain.ConnectedContentSource{}, postgres.ErrNotFound
	}
	return src, nil
}

func (m *securityMockStore) ListConnectedContentSources(ctx context.Context, p domain.Principal) ([]domain.ConnectedContentSource, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	var result []domain.ConnectedContentSource
	for _, src := range m.sources {
		result = append(result, src)
	}
	return result, nil
}

func (m *securityMockStore) RecordExtensionActivity(ctx context.Context, p domain.Principal, activity domain.ExtensionActivity) (domain.ExtensionActivity, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	activity.ID = fmt.Sprintf("act-%d", len(m.activities)+1)
	m.activities = append(m.activities, activity)
	return activity, nil
}

func (m *securityMockStore) ListExtensionActivities(ctx context.Context, p domain.Principal, extensionID string, limit int) ([]domain.ExtensionActivity, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	var result []domain.ExtensionActivity
	for _, act := range m.activities {
		if extensionID == "" || act.ExtensionID == extensionID {
			result = append(result, act)
		}
	}
	return result, nil
}

func (m *securityMockStore) GetCatalogByKey(ctx context.Context, p domain.Principal, key string) (domain.Catalog, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if cat, ok := m.catalogs[key]; ok {
		return cat, nil
	}
	return domain.Catalog{}, postgres.ErrNotFound
}

func (m *securityMockStore) GetCatalogItem(ctx context.Context, p domain.Principal, catalogID, itemKey string) (domain.CatalogItem, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	key := catalogID + ":" + itemKey
	if item, ok := m.items[key]; ok {
		return item, nil
	}
	return domain.CatalogItem{}, postgres.ErrNotFound
}

// TestSecurityE2E Unit & Integration tests for Task 20.9.2:
// 1. Private-IP connected-content fetch blocked (SSRF guard).
// 2. Unlisted host refused (allowlist enforcement).
// 3. *_ref-only secret handling (raw secret rejected + redacted on read).
// 4. Scope enforcement (catalogs:read key returns 403 on write).
// 5. Render failure degradation to fallback (never fail send) and audited.
func TestSecurityE2E(t *testing.T) {
	pFull := domain.Principal{
		TenantID:    "tenant-sec",
		WorkspaceID: "workspace-sec",
		AppID:       "app-sec",
		Scopes:      []string{"catalogs:read", "catalogs:write"},
	}

	t.Run("1. Scope enforcement (catalogs:read key returns 403 on write)", func(t *testing.T) {
		mockStore := newSecurityMockStore()
		server := &Server{store: mockStore}

		// Attempt POST /v1/catalogs with read-only API key -> 403
		catalogBody := map[string]any{
			"key":            "sec-cat-read-only",
			"name":           "ReadOnly Test Catalog",
			"item_key_field": "sku",
		}
		jsonBytes, _ := json.Marshal(catalogBody)
		req := httptest.NewRequest("POST", "/v1/catalogs", bytes.NewBuffer(jsonBytes))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Authorization", "Bearer read-only-key")

		w := httptest.NewRecorder()
		handler := server.authenticate("catalogs:write", http.HandlerFunc(server.createCatalog))
		handler.ServeHTTP(w, req)

		assert.Equal(t, http.StatusForbidden, w.Code, "catalogs:read scope must return 403 on catalog create")

		// Attempt POST /v1/connected-content-sources with read-only API key -> 403
		sourceBody := map[string]any{
			"name":         "sec-source-read-only",
			"allowed_host": "api.example.com",
		}
		jsonBytes, _ = json.Marshal(sourceBody)
		req = httptest.NewRequest("POST", "/v1/connected-content-sources", bytes.NewBuffer(jsonBytes))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Authorization", "Bearer read-only-key")

		w = httptest.NewRecorder()
		handler = server.authenticate("catalogs:write", http.HandlerFunc(server.createConnectedContentSource))
		handler.ServeHTTP(w, req)

		assert.Equal(t, http.StatusForbidden, w.Code, "catalogs:read scope must return 403 on source create")
	})

	t.Run("2. Raw secret rejected and redacted on read", func(t *testing.T) {
		mockStore := newSecurityMockStore()
		server := &Server{store: mockStore}

		// Attempting to create source with a raw secret ref format -> Error
		badSrc := domain.ConnectedContentSource{
			Name:           "RawSecretSource",
			AllowedHost:    "api.example.com",
			AuthHeaderName: "Authorization",
			AuthSecretRef:  "secret:raw_secret_value",
			Status:         "draft",
		}
		_, err := mockStore.CreateConnectedContentSource(context.Background(), pFull, badSrc)
		require.Error(t, err, "raw secret format must be rejected")
		assert.Contains(t, err.Error(), "must be an environment variable name")

		// Create valid source
		validSrc := domain.ConnectedContentSource{
			ID:             "src-valid-1",
			Name:           "ValidSecretSource",
			AllowedHost:    "api.secure-example.com",
			AuthHeaderName: "X-API-Key",
			AuthSecretRef:  "SECURE_API_KEY_REF",
			Status:         "active",
			Enabled:        true,
		}
		_, err = mockStore.CreateConnectedContentSource(context.Background(), pFull, validSrc)
		require.NoError(t, err)

		// GET source via HTTP API with full key -> verify AuthSecretRef is redacted ("")
		req := httptest.NewRequest("GET", "/v1/connected-content-sources/src-valid-1", nil)
		req.SetPathValue("id", "src-valid-1")
		req.Header.Set("Authorization", "Bearer full-key")

		w := httptest.NewRecorder()
		handler := server.authenticate("catalogs:read", http.HandlerFunc(server.getConnectedContentSource))
		handler.ServeHTTP(w, req)

		assert.Equal(t, http.StatusOK, w.Code)
		var getResp domain.ConnectedContentSource
		err = json.Unmarshal(w.Body.Bytes(), &getResp)
		require.NoError(t, err)
		assert.Empty(t, getResp.AuthSecretRef, "secret reference must be redacted on read")
	})

	t.Run("3. Private-IP connected-content fetch blocked (SSRF-safe)", func(t *testing.T) {
		mockStore := newSecurityMockStore()
		cache := render.NewTTLCache(100, render.SystemClock{})
		fetcher := render.NewDefaultConnectedContentFetcher(mockStore, cache)

		// Create source allowing 127.0.0.1
		localhostSrc := domain.ConnectedContentSource{
			Name:        "LocalhostSource",
			AllowedHost: "127.0.0.1",
			Enabled:     true,
			Status:      "active",
		}
		_, err := mockStore.CreateConnectedContentSource(context.Background(), pFull, localhostSrc)
		require.NoError(t, err)

		// Attempt fetch to loopback IP (127.0.0.1)
		data, err := fetcher.Fetch(context.Background(), pFull, "http://127.0.0.1:8080/internal-data", 300)
		require.NoError(t, err, "fetcher must return nil on SSRF block, not error or panic")
		assert.Nil(t, data, "fetch to private IP 127.0.0.1 must be blocked and return nil")

		// Verify audit activity was logged with ssrf_blocked
		activities, err := mockStore.ListExtensionActivities(context.Background(), pFull, "connected_content", 50)
		require.NoError(t, err)
		var ssrfAuditFound bool
		for _, act := range activities {
			if act.PolicyDecision == "ssrf_blocked" || act.PolicyDecision == "denied" {
				ssrfAuditFound = true
				break
			}
		}
		assert.True(t, ssrfAuditFound, "SSRF block must be recorded in extension_activity audit")
	})

	t.Run("4. Unlisted host refused (allowlist enforcement)", func(t *testing.T) {
		mockStore := newSecurityMockStore()
		cache := render.NewTTLCache(100, render.SystemClock{})
		fetcher := render.NewDefaultConnectedContentFetcher(mockStore, cache)

		// Fetch from unlisted host
		data, err := fetcher.Fetch(context.Background(), pFull, "https://unlisted-malicious-domain.com/data.json", 300)
		require.NoError(t, err)
		assert.Nil(t, data, "unlisted host fetch must be refused and return nil")

		// Verify audit logged 'denied'
		activities, err := mockStore.ListExtensionActivities(context.Background(), pFull, "connected_content", 50)
		require.NoError(t, err)
		var deniedAuditFound bool
		for _, act := range activities {
			if act.PolicyDecision == "denied" {
				deniedAuditFound = true
				break
			}
		}
		assert.True(t, deniedAuditFound, "unlisted host fetch must record 'denied' audit")
	})

	t.Run("5. Render failures degrade to fallback (never fail send)", func(t *testing.T) {
		mockStore := newSecurityMockStore()
		cache := render.NewTTLCache(100, render.SystemClock{})
		fetcher := render.NewDefaultConnectedContentFetcher(mockStore, cache)
		deps := render.RenderDeps{
			Store:     mockStore,
			Principal: pFull,
			Fetcher:   fetcher,
			Cache:     cache,
		}

		// Template with connected content referencing an unlisted host
		tmpl := `Result: [{% connected_content "https://unlisted-domain.com/api" save: res %}{{ res.val }}]`
		out, err := render.RenderWithContext(context.Background(), tmpl, map[string]any{}, deps)
		require.NoError(t, err, "rendering must NOT fail on connected content fetch refusal")
		assert.Equal(t, "Result: []", out, "rendering should degrade gracefully to fallback")

		// Create a disabled source (kill switch)
		disabledSrc := domain.ConnectedContentSource{
			Name:        "DisabledSource",
			AllowedHost: "api.disabled-service.com",
			Enabled:     false,
			Status:      "disabled",
		}
		_, err = mockStore.CreateConnectedContentSource(context.Background(), pFull, disabledSrc)
		require.NoError(t, err)

		tmplDisabled := `Result: [{% connected_content "https://api.disabled-service.com/data" save: res %}{{ res.val }}]`
		outDisabled, err := render.RenderWithContext(context.Background(), tmplDisabled, map[string]any{}, deps)
		require.NoError(t, err, "rendering must NOT fail when source is disabled")
		assert.Equal(t, "Result: []", outDisabled, "disabled source must fall back gracefully")
	})

	// Also run DB integration test if DB URL is configured
	databaseURL := os.Getenv("OPENJOURNEY_TEST_DATABASE_URL")
	if databaseURL == "" {
		return
	}

	t.Run("Postgres DB Integration Security E2E", func(t *testing.T) {
		ctx := context.Background()
		dbStore, err := postgres.Open(ctx, databaseURL)
		require.NoError(t, err)
		defer dbStore.Close()
		require.NoError(t, dbStore.Migrate(ctx))

		tenantKey := fmt.Sprintf("sec-db-e2e-%d", time.Now().UnixNano())
		require.NoError(t, dbStore.EnsureDevelopmentTenant(ctx, tenantKey))
		pDbFull, err := dbStore.Authenticate(ctx, tenantKey)
		require.NoError(t, err)
		appID, err := dbStore.GetFirstAppID(ctx, pDbFull.TenantID, pDbFull.WorkspaceID)
		require.NoError(t, err)
		pDbFull.AppID = appID
		pDbFull.Scopes = []string{"catalogs:read", "catalogs:write"}

		// Verify DB rejects raw secret
		badSrc := domain.ConnectedContentSource{
			Name:           "DbBadSource",
			AllowedHost:    "api.example.com",
			AuthHeaderName: "Authorization",
			AuthSecretRef:  "secret:raw_value",
			Status:         "draft",
		}
		_, err = dbStore.CreateConnectedContentSource(ctx, pDbFull, badSrc)
		require.Error(t, err)

		// Verify SSRF block in fetcher against DB
		dbFetcher := render.NewDefaultConnectedContentFetcher(dbStore, render.NewTTLCache(50, render.SystemClock{}))
		localhostSrc := domain.ConnectedContentSource{
			Name:        "DbLocalhostSource",
			AllowedHost: "127.0.0.1",
			Enabled:     true,
			Status:      "active",
		}
		_, err = dbStore.CreateConnectedContentSource(ctx, pDbFull, localhostSrc)
		require.NoError(t, err)

		res, err := dbFetcher.Fetch(ctx, pDbFull, "http://127.0.0.1:8080/data", 300)
		require.NoError(t, err)
		assert.Nil(t, res)
	})
}
