package httpapi

import (
	"bytes"
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
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type mockCatalogStore struct {
	ports.Store
	catalogs map[string]domain.Catalog
	items    map[string][]domain.CatalogItem
}

func (m *mockCatalogStore) GetCatalog(ctx interface{}, p domain.Principal, id string) (domain.Catalog, error) {
	if cat, ok := m.catalogs[id]; ok {
		return cat, nil
	}
	return domain.Catalog{}, ErrNotFound
}

func (m *mockCatalogStore) BulkUpsertCatalogItems(ctx interface{}, p domain.Principal, items []domain.CatalogItem) (domain.BulkUpsertResult, error) {
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

func (m *mockCatalogStore) Authenticate(ctx interface{}, key string) (domain.Principal, error) {
	return domain.Principal{
		TenantID:    "tenant-123",
		WorkspaceID: "workspace-123",
		AppID:       "app-123",
		Scopes:      []string{"catalogs:write"},
	}, nil
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
	req.Header.Set("id", "cat-123")
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
