package httpapi

import (
	"bytes"
	"context"
	"encoding/csv"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/buildwithdmytro/openjourney/internal/domain"
	"github.com/buildwithdmytro/openjourney/internal/ports"
	"github.com/buildwithdmytro/openjourney/internal/postgres"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type mockCatalogStore struct {
	ports.Store
	catalogs map[string]domain.Catalog
	items    map[string][]domain.CatalogItem
	sources  map[string]domain.ConnectedContentSource
}

func (m *mockCatalogStore) GetCatalog(ctx context.Context, p domain.Principal, id string) (domain.Catalog, error) {
	if cat, ok := m.catalogs[id]; ok {
		return cat, nil
	}
	return domain.Catalog{}, postgres.ErrNotFound
}

func (m *mockCatalogStore) BulkUpsertCatalogItems(ctx context.Context, p domain.Principal, items []domain.CatalogItem) (domain.BulkUpsertResult, error) {
	if len(items) == 0 {
		return domain.BulkUpsertResult{}, nil
	}

	catalogID := items[0].CatalogID
	if _, ok := m.catalogs[catalogID]; !ok {
		return domain.BulkUpsertResult{}, fmt.Errorf("catalog not found")
	}

	if m.items == nil {
		m.items = make(map[string][]domain.CatalogItem)
	}

	// Track inserted count (simplified - just counting new items)
	existingKeys := make(map[string]bool)
	if existing, ok := m.items[catalogID]; ok {
		for _, item := range existing {
			existingKeys[item.ItemKey] = true
		}
	}

	inserted := int64(0)
	for _, item := range items {
		if !existingKeys[item.ItemKey] {
			inserted++
		}
		m.items[catalogID] = append(m.items[catalogID], item)
	}

	// Update catalog item count
	cat := m.catalogs[catalogID]
	cat.ItemCount = int64(len(m.items[catalogID]))
	m.catalogs[catalogID] = cat

	return domain.BulkUpsertResult{InsertedCount: inserted, UpdatedCount: 0}, nil
}

func (m *mockCatalogStore) ListCatalogItems(ctx context.Context, p domain.Principal, catalogID string, limit int) ([]domain.CatalogItem, error) {
	items, ok := m.items[catalogID]
	if !ok {
		return []domain.CatalogItem{}, nil
	}
	if limit <= 0 {
		limit = 100
	}
	if limit > len(items) {
		limit = len(items)
	}
	return items[:limit], nil
}

func (m *mockCatalogStore) Authenticate(ctx context.Context, key string) (domain.Principal, error) {
	return domain.Principal{
		TenantID:    "tenant-123",
		WorkspaceID: "workspace-123",
		AppID:       "app-123",
		Scopes:      []string{"catalogs:write", "catalogs:read"},
	}, nil
}

func (m *mockCatalogStore) CreateConnectedContentSource(ctx context.Context, p domain.Principal, src domain.ConnectedContentSource) (domain.ConnectedContentSource, error) {
	if m.sources == nil {
		m.sources = make(map[string]domain.ConnectedContentSource)
	}
	src.ID = fmt.Sprintf("src-%d", len(m.sources)+1)
	m.sources[src.ID] = src
	return src, nil
}

func (m *mockCatalogStore) GetConnectedContentSource(ctx context.Context, p domain.Principal, id string) (domain.ConnectedContentSource, error) {
	if src, ok := m.sources[id]; ok {
		return src, nil
	}
	return domain.ConnectedContentSource{}, postgres.ErrNotFound
}

func (m *mockCatalogStore) ListConnectedContentSources(ctx context.Context, p domain.Principal) ([]domain.ConnectedContentSource, error) {
	var result []domain.ConnectedContentSource
	for _, src := range m.sources {
		result = append(result, src)
	}
	return result, nil
}

func (m *mockCatalogStore) UpdateConnectedContentSource(ctx context.Context, p domain.Principal, src domain.ConnectedContentSource) (domain.ConnectedContentSource, error) {
	if _, ok := m.sources[src.ID]; !ok {
		return domain.ConnectedContentSource{}, postgres.ErrNotFound
	}
	m.sources[src.ID] = src
	return src, nil
}

func (m *mockCatalogStore) DeleteConnectedContentSource(ctx context.Context, p domain.Principal, id string) error {
	if _, ok := m.sources[id]; !ok {
		return postgres.ErrNotFound
	}
	delete(m.sources, id)
	return nil
}

func TestBulkUploadCSV(t *testing.T) {
	store := &mockCatalogStore{
		catalogs: map[string]domain.Catalog{
			"cat-123": {
				ID:     "cat-123",
				Key:    "products",
				Name:   "Products",
				Status: "active",
			},
		},
	}

	server := &Server{store: store}

	// Create CSV data
	var buf bytes.Buffer
	writer := csv.NewWriter(&buf)
	writer.Write([]string{"item_key", "name", "price"})
	writer.Write([]string{"SKU001", "Product 1", "10.00"})
	writer.Write([]string{"SKU002", "Product 2", "20.00"})
	writer.Write([]string{"SKU003", "Product 3", "30.00"})
	writer.Flush()

	// Create multipart request
	var formBuf bytes.Buffer
	formWriter := multipart.NewWriter(&formBuf)
	part, err := formWriter.CreateFormFile("file", "items.csv")
	require.NoError(t, err)
	_, err = io.Copy(part, &buf)
	require.NoError(t, err)
	formWriter.Close()

	// Make request
	req := httptest.NewRequest("POST", "/v1/catalogs/cat-123/items:bulk", &formBuf)
	req.Header.Set("Content-Type", formWriter.FormDataContentType())
	req.Header.Set("Authorization", "Bearer test-key")
	req.SetPathValue("id", "cat-123")
	req = req.WithContext(req.Context())

	w := httptest.NewRecorder()
	http.Handler(server.authenticate("catalogs:write", http.HandlerFunc(server.bulkUploadCatalogItems))).ServeHTTP(w, req)

	assert.Equal(t, http.StatusAccepted, w.Code)

	var result map[string]any
	err = json.Unmarshal(w.Body.Bytes(), &result)
	require.NoError(t, err)
	assert.Greater(t, result["inserted"], float64(0))
	assert.Equal(t, float64(3), result["total"])
}

func TestBulkUploadJSON(t *testing.T) {
	store := &mockCatalogStore{
		catalogs: map[string]domain.Catalog{
			"cat-456": {
				ID:     "cat-456",
				Key:    "products",
				Name:   "Products",
				Status: "active",
			},
		},
	}

	server := &Server{store: store}

	// Create JSON data (newline-delimited)
	jsonData := `{"item_key":"SKU001","name":"Product 1","price":10.00}
{"item_key":"SKU002","name":"Product 2","price":20.00}
{"item_key":"SKU003","name":"Product 3","price":30.00}`

	// Create multipart request
	var formBuf bytes.Buffer
	formWriter := multipart.NewWriter(&formBuf)
	part, err := formWriter.CreateFormFile("file", "items.jsonl")
	require.NoError(t, err)
	_, err = io.WriteString(part, jsonData)
	require.NoError(t, err)
	formWriter.Close()

	// Make request
	req := httptest.NewRequest("POST", "/v1/catalogs/cat-456/items:bulk", &formBuf)
	req.Header.Set("Content-Type", formWriter.FormDataContentType())
	req.Header.Set("Authorization", "Bearer test-key")
	req.SetPathValue("id", "cat-456")
	req = req.WithContext(req.Context())

	w := httptest.NewRecorder()
	http.Handler(server.authenticate("catalogs:write", http.HandlerFunc(server.bulkUploadCatalogItems))).ServeHTTP(w, req)

	assert.Equal(t, http.StatusAccepted, w.Code)

	var result map[string]any
	err = json.Unmarshal(w.Body.Bytes(), &result)
	require.NoError(t, err)
	assert.Greater(t, result["inserted"], float64(0))
	assert.Equal(t, float64(3), result["total"])
}

func TestBulkUploadMissingCatalog(t *testing.T) {
	store := &mockCatalogStore{
		catalogs: map[string]domain.Catalog{},
	}

	server := &Server{store: store}

	// Create simple CSV
	var buf bytes.Buffer
	writer := csv.NewWriter(&buf)
	writer.Write([]string{"item_key", "name"})
	writer.Write([]string{"SKU001", "Product 1"})
	writer.Flush()

	// Create multipart request
	var formBuf bytes.Buffer
	formWriter := multipart.NewWriter(&formBuf)
	part, err := formWriter.CreateFormFile("file", "items.csv")
	require.NoError(t, err)
	_, err = io.Copy(part, &buf)
	require.NoError(t, err)
	formWriter.Close()

	// Make request
	req := httptest.NewRequest("POST", "/v1/catalogs/nonexistent/items:bulk", &formBuf)
	req.Header.Set("Content-Type", formWriter.FormDataContentType())
	req.Header.Set("Authorization", "Bearer test-key")
	req = req.WithContext(req.Context())

	w := httptest.NewRecorder()
	http.Handler(server.authenticate("catalogs:write", http.HandlerFunc(server.bulkUploadCatalogItems))).ServeHTTP(w, req)

	assert.Equal(t, http.StatusNotFound, w.Code)
}

func TestBulkUploadEmptyFile(t *testing.T) {
	store := &mockCatalogStore{
		catalogs: map[string]domain.Catalog{
			"cat-789": {
				ID:     "cat-789",
				Key:    "products",
				Name:   "Products",
				Status: "active",
			},
		},
	}

	server := &Server{store: store}

	// Create multipart request with empty file
	var formBuf bytes.Buffer
	formWriter := multipart.NewWriter(&formBuf)
	formWriter.CreateFormFile("file", "empty.csv")
	formWriter.Close()

	// Make request
	req := httptest.NewRequest("POST", "/v1/catalogs/cat-789/items:bulk", &formBuf)
	req.Header.Set("Content-Type", formWriter.FormDataContentType())
	req.Header.Set("Authorization", "Bearer test-key")
	req = req.WithContext(req.Context())

	w := httptest.NewRecorder()
	http.Handler(server.authenticate("catalogs:write", http.HandlerFunc(server.bulkUploadCatalogItems))).ServeHTTP(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestMockStoreListCatalogItems(t *testing.T) {
	store := &mockCatalogStore{
		catalogs: map[string]domain.Catalog{
			"cat-list": {
				ID:     "cat-list",
				Key:    "products",
				Name:   "Products",
				Status: "active",
			},
		},
		items: map[string][]domain.CatalogItem{
			"cat-list": {
				{
					ID:       "item-1",
					CatalogID: "cat-list",
					TenantID: "tenant-123",
					AppID:    "app-123",
					ItemKey:  "SKU001",
					Payload:  []byte(`{"name":"Product 1","price":10}`),
				},
				{
					ID:       "item-2",
					CatalogID: "cat-list",
					TenantID: "tenant-123",
					AppID:    "app-123",
					ItemKey:  "SKU002",
					Payload:  []byte(`{"name":"Product 2","price":20}`),
				},
				{
					ID:       "item-3",
					CatalogID: "cat-list",
					TenantID: "tenant-123",
					AppID:    "app-123",
					ItemKey:  "SKU003",
					Payload:  []byte(`{"name":"Product 3","price":30}`),
				},
			},
		},
	}

	principal := domain.Principal{
		TenantID: "tenant-123",
		AppID:    "app-123",
	}

	// Test listing all items
	items, err := store.ListCatalogItems(context.Background(), principal, "cat-list", 100)
	require.NoError(t, err)
	assert.Equal(t, 3, len(items))
	assert.Equal(t, "SKU001", items[0].ItemKey)
}

func TestMockStoreListCatalogItemsWithLimit(t *testing.T) {
	store := &mockCatalogStore{
		catalogs: map[string]domain.Catalog{
			"cat-limit": {
				ID:     "cat-limit",
				Key:    "products",
				Name:   "Products",
				Status: "active",
			},
		},
		items: map[string][]domain.CatalogItem{
			"cat-limit": {
				{
					ID:       "item-1",
					CatalogID: "cat-limit",
					TenantID: "tenant-123",
					AppID:    "app-123",
					ItemKey:  "SKU001",
					Payload:  []byte(`{"name":"Product 1"}`),
				},
				{
					ID:       "item-2",
					CatalogID: "cat-limit",
					TenantID: "tenant-123",
					AppID:    "app-123",
					ItemKey:  "SKU002",
					Payload:  []byte(`{"name":"Product 2"}`),
				},
				{
					ID:       "item-3",
					CatalogID: "cat-limit",
					TenantID: "tenant-123",
					AppID:    "app-123",
					ItemKey:  "SKU003",
					Payload:  []byte(`{"name":"Product 3"}`),
				},
			},
		},
	}

	principal := domain.Principal{
		TenantID: "tenant-123",
		AppID:    "app-123",
	}

	// Test with limit=2
	items, err := store.ListCatalogItems(context.Background(), principal, "cat-limit", 2)
	require.NoError(t, err)
	assert.Equal(t, 2, len(items))
}

func TestMockStoreListCatalogItemsEmpty(t *testing.T) {
	store := &mockCatalogStore{
		catalogs: map[string]domain.Catalog{
			"cat-empty": {
				ID:     "cat-empty",
				Key:    "products",
				Name:   "Products",
				Status: "active",
			},
		},
		items: map[string][]domain.CatalogItem{},
	}

	principal := domain.Principal{
		TenantID: "tenant-123",
		AppID:    "app-123",
	}

	// Test empty catalog
	items, err := store.ListCatalogItems(context.Background(), principal, "cat-empty", 100)
	require.NoError(t, err)
	assert.Equal(t, 0, len(items))
}

func TestCreateConnectedContentSource(t *testing.T) {
	store := &mockCatalogStore{
		catalogs: map[string]domain.Catalog{},
		sources:  make(map[string]domain.ConnectedContentSource),
	}
	server := &Server{store: store}

	input := map[string]any{
		"name":                   "Test Source",
		"allowed_host":           "api.example.com",
		"auth_header_name":       "X-API-Key",
		"auth_secret_ref":        "CC_SECRET_API_KEY",
		"default_ttl_seconds":    300,
		"timeout_ms":             2000,
		"enabled":                false,
		"status":                 "draft",
	}

	body, _ := json.Marshal(input)
	req := httptest.NewRequest("POST", "/v1/connected-content-sources", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer test-key")
	req = req.WithContext(req.Context())

	w := httptest.NewRecorder()
	http.Handler(server.authenticate("catalogs:write", http.HandlerFunc(server.createConnectedContentSource))).ServeHTTP(w, req)

	assert.Equal(t, http.StatusCreated, w.Code)

	var result domain.ConnectedContentSource
	err := json.Unmarshal(w.Body.Bytes(), &result)
	require.NoError(t, err)
	assert.Equal(t, "Test Source", result.Name)
	assert.Equal(t, "api.example.com", result.AllowedHost)
	assert.Empty(t, result.AuthSecretRef, "secret should be redacted on read")
}

func TestGetConnectedContentSourceRedactsSecret(t *testing.T) {
	store := &mockCatalogStore{
		sources: map[string]domain.ConnectedContentSource{
			"src-123": {
				ID:               "src-123",
				TenantID:         "tenant-123",
				WorkspaceID:      "workspace-123",
				Name:             "Test Source",
				AllowedHost:      "api.example.com",
				AuthHeaderName:   "X-API-Key",
				AuthSecretRef:    "CC_SECRET_API_KEY",
				DefaultTTLSeconds: 300,
				TimeoutMs:        2000,
				Enabled:          true,
				Status:           "active",
			},
		},
	}
	server := &Server{store: store}

	req := httptest.NewRequest("GET", "/v1/connected-content-sources/src-123", nil)
	req.Header.Set("Authorization", "Bearer test-key")
	req.SetPathValue("id", "src-123")
	req = req.WithContext(req.Context())

	w := httptest.NewRecorder()
	http.Handler(server.authenticate("catalogs:read", http.HandlerFunc(server.getConnectedContentSource))).ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)

	var result domain.ConnectedContentSource
	err := json.Unmarshal(w.Body.Bytes(), &result)
	require.NoError(t, err)
	assert.Empty(t, result.AuthSecretRef, "secret must be redacted on read")
	assert.Equal(t, "Test Source", result.Name)
}

func TestListConnectedContentSourcesRedactsSecrets(t *testing.T) {
	store := &mockCatalogStore{
		sources: map[string]domain.ConnectedContentSource{
			"src-1": {
				ID:            "src-1",
				Name:          "Source 1",
				AllowedHost:   "api1.example.com",
				AuthSecretRef: "CC_SECRET_1",
			},
			"src-2": {
				ID:            "src-2",
				Name:          "Source 2",
				AllowedHost:   "api2.example.com",
				AuthSecretRef: "CC_SECRET_2",
			},
		},
	}
	server := &Server{store: store}

	req := httptest.NewRequest("GET", "/v1/connected-content-sources", nil)
	req.Header.Set("Authorization", "Bearer test-key")
	req = req.WithContext(req.Context())

	w := httptest.NewRecorder()
	http.Handler(server.authenticate("catalogs:read", http.HandlerFunc(server.listConnectedContentSources))).ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)

	var result map[string]any
	err := json.Unmarshal(w.Body.Bytes(), &result)
	require.NoError(t, err)

	sources, ok := result["sources"].([]any)
	require.True(t, ok)
	assert.Equal(t, 2, len(sources))

	for _, srcAny := range sources {
		src := srcAny.(map[string]any)
		assert.Empty(t, src["auth_secret_ref"], "all secrets must be redacted")
	}
}

func TestEnableConnectedContentSourceRequiresHuman(t *testing.T) {
	store := &mockCatalogStore{
		sources: map[string]domain.ConnectedContentSource{
			"src-123": {
				ID:           "src-123",
				TenantID:     "tenant-123",
				WorkspaceID:  "workspace-123",
				Name:         "Test Source",
				AllowedHost:  "api.example.com",
				Enabled:      false,
				Status:       "draft",
			},
		},
	}
	server := &Server{store: store}

	req := httptest.NewRequest("POST", "/v1/connected-content-sources/src-123:enable", nil)
	req.Header.Set("Authorization", "Bearer test-key")
	req.SetPathValue("id", "src-123")
	req = req.WithContext(req.Context())

	w := httptest.NewRecorder()
	http.Handler(server.authenticate("catalogs:write", http.HandlerFunc(server.enableConnectedContentSource))).ServeHTTP(w, req)

	assert.Equal(t, http.StatusForbidden, w.Code)

	var result map[string]any
	err := json.Unmarshal(w.Body.Bytes(), &result)
	require.NoError(t, err)
	errObj, ok := result["error"].(map[string]any)
	require.True(t, ok, "error field should be an object")
	assert.Equal(t, "human_approval_required", errObj["code"])
}

func TestUpdateConnectedContentSource(t *testing.T) {
	store := &mockCatalogStore{
		sources: map[string]domain.ConnectedContentSource{
			"src-123": {
				ID:               "src-123",
				TenantID:         "tenant-123",
				WorkspaceID:      "workspace-123",
				Name:             "Old Name",
				AllowedHost:      "api.example.com",
				DefaultTTLSeconds: 300,
				TimeoutMs:        2000,
				Enabled:          false,
				Status:           "draft",
			},
		},
	}
	server := &Server{store: store}

	input := domain.ConnectedContentSource{
		ID:               "src-123",
		Name:             "Updated Name",
		AllowedHost:      "api-updated.example.com",
		DefaultTTLSeconds: 600,
		TimeoutMs:        3000,
		Enabled:          false,
		Status:           "draft",
	}

	body, _ := json.Marshal(input)
	req := httptest.NewRequest("PUT", "/v1/connected-content-sources/src-123", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer test-key")
	req.SetPathValue("id", "src-123")
	req = req.WithContext(req.Context())

	w := httptest.NewRecorder()
	http.Handler(server.authenticate("catalogs:write", http.HandlerFunc(server.updateConnectedContentSource))).ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)

	var result domain.ConnectedContentSource
	err := json.Unmarshal(w.Body.Bytes(), &result)
	require.NoError(t, err)
	assert.Equal(t, "Updated Name", result.Name)
	assert.Equal(t, "api-updated.example.com", result.AllowedHost)
}

func TestDeleteConnectedContentSource(t *testing.T) {
	store := &mockCatalogStore{
		sources: map[string]domain.ConnectedContentSource{
			"src-123": {
				ID:           "src-123",
				Name:         "Test Source",
				AllowedHost:  "api.example.com",
			},
		},
	}
	server := &Server{store: store}

	req := httptest.NewRequest("DELETE", "/v1/connected-content-sources/src-123", nil)
	req.Header.Set("Authorization", "Bearer test-key")
	req.SetPathValue("id", "src-123")
	req = req.WithContext(req.Context())

	w := httptest.NewRecorder()
	http.Handler(server.authenticate("catalogs:write", http.HandlerFunc(server.deleteConnectedContentSource))).ServeHTTP(w, req)

	assert.Equal(t, http.StatusNoContent, w.Code)

	// Verify it's deleted
	_, err := store.GetConnectedContentSource(context.Background(), domain.Principal{}, "src-123")
	assert.Equal(t, postgres.ErrNotFound, err)
}

func TestCreateConnectedContentSourceRequiredFields(t *testing.T) {
	store := &mockCatalogStore{sources: make(map[string]domain.ConnectedContentSource)}
	server := &Server{store: store}

	input := map[string]any{
		"name": "Test Source",
	}

	body, _ := json.Marshal(input)
	req := httptest.NewRequest("POST", "/v1/connected-content-sources", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer test-key")
	req = req.WithContext(req.Context())

	w := httptest.NewRecorder()
	http.Handler(server.authenticate("catalogs:write", http.HandlerFunc(server.createConnectedContentSource))).ServeHTTP(w, req)

	require.Equal(t, http.StatusBadRequest, w.Code)

	var result map[string]any
	err := json.Unmarshal(w.Body.Bytes(), &result)
	require.NoError(t, err)
	errObj, ok := result["error"].(map[string]any)
	require.True(t, ok, "error field should be an object")
	assert.Equal(t, "invalid_source", errObj["code"])
}

func TestCreateConnectedContentSource_RejectsNonAllowlistedAuthSecretRef(t *testing.T) {
	store := &mockCatalogStore{sources: make(map[string]domain.ConnectedContentSource)}
	server := &Server{store: store}

	// Attempt to register a source referencing DATABASE_URL
	input := map[string]any{
		"name":             "Exfiltration Attempt",
		"allowed_host":     "evil.example.com",
		"auth_header_name": "Authorization",
		"auth_secret_ref":  "DATABASE_URL",
	}

	body, _ := json.Marshal(input)
	req := httptest.NewRequest("POST", "/v1/connected-content-sources", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer test-key")
	req = req.WithContext(req.Context())

	w := httptest.NewRecorder()
	http.Handler(server.authenticate("catalogs:write", http.HandlerFunc(server.createConnectedContentSource))).ServeHTTP(w, req)

	require.Equal(t, http.StatusBadRequest, w.Code)

	var result map[string]any
	err := json.Unmarshal(w.Body.Bytes(), &result)
	require.NoError(t, err)
	errObj, ok := result["error"].(map[string]any)
	require.True(t, ok, "error field should be an object")
	assert.Equal(t, "invalid_source", errObj["code"])
	assert.Contains(t, errObj["message"], "auth_secret_ref must match pattern")
}

