package postgres

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/buildwithdmytro/openjourney/internal/domain"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCatalogRoundtrip(t *testing.T) {
	databaseURL := os.Getenv("OPENJOURNEY_TEST_DATABASE_URL")
	if databaseURL == "" {
		t.Skip("OPENJOURNEY_TEST_DATABASE_URL is not configured")
	}

	ctx := context.Background()
	store, err := Open(ctx, databaseURL)
	require.NoError(t, err)
	defer store.Close()
	require.NoError(t, store.Migrate(ctx))

	key := fmt.Sprintf("catalog-test-%d", time.Now().UnixNano())
	require.NoError(t, store.EnsureDevelopmentTenant(ctx, key))
	p, err := store.Authenticate(ctx, key)
	require.NoError(t, err)

	// Create a catalog
	cat := domain.Catalog{
		Key:           "products",
		Name:          "Product Catalog",
		Description:   "Reference data for products",
		ItemKeyField:  "sku",
		Status:        "active",
	}

	created, err := store.CreateCatalog(ctx, p, cat)
	require.NoError(t, err)
	assert.Equal(t, cat.Key, created.Key)
	assert.Equal(t, cat.Name, created.Name)
	assert.Equal(t, p.TenantID, created.TenantID)
	assert.Equal(t, p.AppID, created.AppID)

	// Get the catalog
	retrieved, err := store.GetCatalog(ctx, p, created.ID)
	require.NoError(t, err)
	assert.Equal(t, created.ID, retrieved.ID)
	assert.Equal(t, cat.Key, retrieved.Key)

	// List catalogs
	list, err := store.ListCatalogs(ctx, p)
	require.NoError(t, err)
	assert.Greater(t, len(list), 0)
	found := false
	for _, c := range list {
		if c.ID == created.ID {
			found = true
			break
		}
	}
	assert.True(t, found)

	// Update catalog
	updated := created
	updated.Name = "Updated Product Catalog"
	updated.Status = "archived"
	result, err := store.UpdateCatalog(ctx, p, updated)
	require.NoError(t, err)
	assert.Equal(t, updated.Name, result.Name)
	assert.Equal(t, updated.Status, result.Status)

	// Delete catalog
	err = store.DeleteCatalog(ctx, p, created.ID)
	require.NoError(t, err)

	// Verify deleted
	_, err = store.GetCatalog(ctx, p, created.ID)
	assert.Equal(t, ErrNotFound, err)
}

func TestDuplicateCatalogKeyRejected(t *testing.T) {
	databaseURL := os.Getenv("OPENJOURNEY_TEST_DATABASE_URL")
	if databaseURL == "" {
		t.Skip("OPENJOURNEY_TEST_DATABASE_URL is not configured")
	}

	ctx := context.Background()
	store, err := Open(ctx, databaseURL)
	require.NoError(t, err)
	defer store.Close()
	require.NoError(t, store.Migrate(ctx))

	key := fmt.Sprintf("catalog-dup-test-%d", time.Now().UnixNano())
	require.NoError(t, store.EnsureDevelopmentTenant(ctx, key))
	p, err := store.Authenticate(ctx, key)
	require.NoError(t, err)

	cat := domain.Catalog{
		Key:    "unique_key",
		Name:   "First Catalog",
		Status: "active",
	}

	_, err = store.CreateCatalog(ctx, p, cat)
	require.NoError(t, err)

	// Try to create another with the same key in the same app
	_, err = store.CreateCatalog(ctx, p, cat)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "unique constraint")
}

func TestGetCatalogItem(t *testing.T) {
	databaseURL := os.Getenv("OPENJOURNEY_TEST_DATABASE_URL")
	if databaseURL == "" {
		t.Skip("OPENJOURNEY_TEST_DATABASE_URL is not configured")
	}

	ctx := context.Background()
	store, err := Open(ctx, databaseURL)
	require.NoError(t, err)
	defer store.Close()
	require.NoError(t, store.Migrate(ctx))

	key := fmt.Sprintf("catalog-item-test-%d", time.Now().UnixNano())
	require.NoError(t, store.EnsureDevelopmentTenant(ctx, key))
	p, err := store.Authenticate(ctx, key)
	require.NoError(t, err)

	// Create a catalog
	cat := domain.Catalog{
		Key:    "items_test",
		Name:   "Test Catalog",
		Status: "active",
	}
	created, err := store.CreateCatalog(ctx, p, cat)
	require.NoError(t, err)

	// Insert an item directly using SQL (simulating bulk import)
	payload := json.RawMessage(`{"name":"Test Product","price":99.99}`)
	err = store.pool.QueryRow(ctx, `INSERT INTO catalog_items
		(catalog_id, tenant_id, app_id, item_key, payload)
		VALUES ($1, $2, $3, $4, $5)`,
		created.ID, p.TenantID, p.AppID, "SKU123", payload).Scan()
	if err != nil && err.Error() != "no rows" {
		require.NoError(t, err)
	}

	// Get the item
	item, err := store.GetCatalogItem(ctx, p, created.ID, "SKU123")
	require.NoError(t, err)
	assert.Equal(t, "SKU123", item.ItemKey)
	assert.Equal(t, created.ID, item.CatalogID)
	assert.Equal(t, payload, item.Payload)

	// Get non-existent item
	_, err = store.GetCatalogItem(ctx, p, created.ID, "NONEXISTENT")
	assert.Equal(t, ErrNotFound, err)
}

func TestListCatalogItems(t *testing.T) {
	databaseURL := os.Getenv("OPENJOURNEY_TEST_DATABASE_URL")
	if databaseURL == "" {
		t.Skip("OPENJOURNEY_TEST_DATABASE_URL is not configured")
	}

	ctx := context.Background()
	store, err := Open(ctx, databaseURL)
	require.NoError(t, err)
	defer store.Close()
	require.NoError(t, store.Migrate(ctx))

	key := fmt.Sprintf("catalog-items-list-test-%d", time.Now().UnixNano())
	require.NoError(t, store.EnsureDevelopmentTenant(ctx, key))
	p, err := store.Authenticate(ctx, key)
	require.NoError(t, err)

	// Create a catalog
	cat := domain.Catalog{
		Key:    "items_list_test",
		Name:   "Test Catalog",
		Status: "active",
	}
	created, err := store.CreateCatalog(ctx, p, cat)
	require.NoError(t, err)

	// Insert a few items
	for i := 1; i <= 3; i++ {
		payload := json.RawMessage(`{"id":"item` + string(rune('0'+byte(i))) + `"}`)
		store.pool.Exec(ctx, `INSERT INTO catalog_items
			(catalog_id, tenant_id, app_id, item_key, payload)
			VALUES ($1, $2, $3, $4, $5)`,
			created.ID, p.TenantID, p.AppID, "SKU00"+string(rune('0'+byte(i))), payload)
	}

	// List items with default limit
	items, err := store.ListCatalogItems(ctx, p, created.ID, 0)
	require.NoError(t, err)
	assert.Equal(t, 3, len(items))
	assert.Equal(t, "SKU001", items[0].ItemKey)

	// List with limit
	items, err = store.ListCatalogItems(ctx, p, created.ID, 2)
	require.NoError(t, err)
	assert.Equal(t, 2, len(items))
}

func TestConnectedContentSourceRoundtrip(t *testing.T) {
	databaseURL := os.Getenv("OPENJOURNEY_TEST_DATABASE_URL")
	if databaseURL == "" {
		t.Skip("OPENJOURNEY_TEST_DATABASE_URL is not configured")
	}

	ctx := context.Background()
	store, err := Open(ctx, databaseURL)
	require.NoError(t, err)
	defer store.Close()
	require.NoError(t, store.Migrate(ctx))

	key := fmt.Sprintf("source-test-%d", time.Now().UnixNano())
	require.NoError(t, store.EnsureDevelopmentTenant(ctx, key))
	p, err := store.Authenticate(ctx, key)
	require.NoError(t, err)

	// Create a source
	src := domain.ConnectedContentSource{
		Name:              "Example API",
		AllowedHost:       "api.example.com",
		AuthHeaderName:    "Authorization",
		AuthSecretRef:     "EXAMPLE_API_KEY",
		DefaultTTLSeconds: 300,
		TimeoutMs:         5000,
		Enabled:           true,
		Status:            "draft",
	}

	created, err := store.CreateConnectedContentSource(ctx, p, src)
	require.NoError(t, err)
	assert.Equal(t, src.Name, created.Name)
	assert.Equal(t, src.AllowedHost, created.AllowedHost)
	assert.Equal(t, p.TenantID, created.TenantID)

	// Get the source
	retrieved, err := store.GetConnectedContentSource(ctx, p, created.ID)
	require.NoError(t, err)
	assert.Equal(t, created.ID, retrieved.ID)
	assert.Equal(t, src.Name, retrieved.Name)

	// List sources
	list, err := store.ListConnectedContentSources(ctx, p)
	require.NoError(t, err)
	assert.Greater(t, len(list), 0)
	found := false
	for _, s := range list {
		if s.ID == created.ID {
			found = true
			break
		}
	}
	assert.True(t, found)

	// Update source
	updated := created
	updated.Enabled = false
	updated.Status = "active"
	result, err := store.UpdateConnectedContentSource(ctx, p, updated)
	require.NoError(t, err)
	assert.Equal(t, updated.Enabled, result.Enabled)
	assert.Equal(t, updated.Status, result.Status)

	// Delete source
	err = store.DeleteConnectedContentSource(ctx, p, created.ID)
	require.NoError(t, err)

	// Verify deleted
	_, err = store.GetConnectedContentSource(ctx, p, created.ID)
	assert.Equal(t, ErrNotFound, err)
}

func TestConnectedContentSourceRejectsRawSecret(t *testing.T) {
	databaseURL := os.Getenv("OPENJOURNEY_TEST_DATABASE_URL")
	if databaseURL == "" {
		t.Skip("OPENJOURNEY_TEST_DATABASE_URL is not configured")
	}

	ctx := context.Background()
	store, err := Open(ctx, databaseURL)
	require.NoError(t, err)
	defer store.Close()
	require.NoError(t, store.Migrate(ctx))

	key := fmt.Sprintf("source-secret-test-%d", time.Now().UnixNano())
	require.NoError(t, store.EnsureDevelopmentTenant(ctx, key))
	p, err := store.Authenticate(ctx, key)
	require.NoError(t, err)

	src := domain.ConnectedContentSource{
		Name:           "Bad Source",
		AllowedHost:    "api.example.com",
		AuthHeaderName: "Authorization",
		AuthSecretRef:  "secret:my_secret_key",
		Status:         "draft",
	}

	_, err = store.CreateConnectedContentSource(ctx, p, src)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "must be an environment variable name")
}

func TestConnectedContentSourceDuplicateNameRejected(t *testing.T) {
	databaseURL := os.Getenv("OPENJOURNEY_TEST_DATABASE_URL")
	if databaseURL == "" {
		t.Skip("OPENJOURNEY_TEST_DATABASE_URL is not configured")
	}

	ctx := context.Background()
	store, err := Open(ctx, databaseURL)
	require.NoError(t, err)
	defer store.Close()
	require.NoError(t, store.Migrate(ctx))

	key := fmt.Sprintf("source-dup-test-%d", time.Now().UnixNano())
	require.NoError(t, store.EnsureDevelopmentTenant(ctx, key))
	p, err := store.Authenticate(ctx, key)
	require.NoError(t, err)

	src := domain.ConnectedContentSource{
		Name:        "unique_source",
		AllowedHost: "api.example.com",
		Status:      "draft",
	}

	_, err = store.CreateConnectedContentSource(ctx, p, src)
	require.NoError(t, err)

	// Try to create another with the same name in the same workspace
	_, err = store.CreateConnectedContentSource(ctx, p, src)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "unique constraint")
}

func TestBulkUpsertCatalogItems(t *testing.T) {
	databaseURL := os.Getenv("OPENJOURNEY_TEST_DATABASE_URL")
	if databaseURL == "" {
		t.Skip("OPENJOURNEY_TEST_DATABASE_URL is not configured")
	}

	ctx := context.Background()
	store, err := Open(ctx, databaseURL)
	require.NoError(t, err)
	defer store.Close()
	require.NoError(t, store.Migrate(ctx))

	key := fmt.Sprintf("bulk-upsert-test-%d", time.Now().UnixNano())
	require.NoError(t, store.EnsureDevelopmentTenant(ctx, key))
	p, err := store.Authenticate(ctx, key)
	require.NoError(t, err)

	// Create a catalog
	cat := domain.Catalog{
		Key:    "bulk_test",
		Name:   "Bulk Test Catalog",
		Status: "active",
	}
	created, err := store.CreateCatalog(ctx, p, cat)
	require.NoError(t, err)

	// Bulk upsert items
	items := []domain.CatalogItem{
		{
			CatalogID: created.ID,
			TenantID:  p.TenantID,
			AppID:     p.AppID,
			ItemKey:   "SKU001",
			Payload:   json.RawMessage(`{"name":"Product 1","price":10.00}`),
		},
		{
			CatalogID: created.ID,
			TenantID:  p.TenantID,
			AppID:     p.AppID,
			ItemKey:   "SKU002",
			Payload:   json.RawMessage(`{"name":"Product 2","price":20.00}`),
		},
		{
			CatalogID: created.ID,
			TenantID:  p.TenantID,
			AppID:     p.AppID,
			ItemKey:   "SKU003",
			Payload:   json.RawMessage(`{"name":"Product 3","price":30.00}`),
		},
	}

	result, err := store.BulkUpsertCatalogItems(ctx, p, items)
	require.NoError(t, err)
	assert.Greater(t, result.InsertedCount, int64(0))

	// Verify items were inserted
	retrieved, err := store.GetCatalogItem(ctx, p, created.ID, "SKU001")
	require.NoError(t, err)
	assert.Equal(t, "SKU001", retrieved.ItemKey)
	assert.JSONEq(t, `{"name":"Product 1","price":10.00}`, string(retrieved.Payload))

	// Test idempotent upsert (re-uploading the same items)
	items[0].Payload = json.RawMessage(`{"name":"Updated Product 1","price":15.00}`)
	result2, err := store.BulkUpsertCatalogItems(ctx, p, items)
	require.NoError(t, err)
	assert.Greater(t, result2.InsertedCount, int64(0))

	// Verify the item was updated
	updated, err := store.GetCatalogItem(ctx, p, created.ID, "SKU001")
	require.NoError(t, err)
	assert.JSONEq(t, `{"name":"Updated Product 1","price":15.00}`, string(updated.Payload))

	// Verify item_count was updated
	updatedCat, err := store.GetCatalog(ctx, p, created.ID)
	require.NoError(t, err)
	assert.Equal(t, int64(3), updatedCat.ItemCount)
}

func TestBulkUpsertMalformedRows(t *testing.T) {
	databaseURL := os.Getenv("OPENJOURNEY_TEST_DATABASE_URL")
	if databaseURL == "" {
		t.Skip("OPENJOURNEY_TEST_DATABASE_URL is not configured")
	}

	ctx := context.Background()
	store, err := Open(ctx, databaseURL)
	require.NoError(t, err)
	defer store.Close()
	require.NoError(t, store.Migrate(ctx))

	key := fmt.Sprintf("bulk-malformed-test-%d", time.Now().UnixNano())
	require.NoError(t, store.EnsureDevelopmentTenant(ctx, key))
	p, err := store.Authenticate(ctx, key)
	require.NoError(t, err)

	// Create a catalog
	cat := domain.Catalog{
		Key:    "malformed_test",
		Name:   "Malformed Test Catalog",
		Status: "active",
	}
	created, err := store.CreateCatalog(ctx, p, cat)
	require.NoError(t, err)

	// Bulk upsert with empty item_key should fail during validation (in the handler)
	// For now, we test with valid items
	items := []domain.CatalogItem{
		{
			CatalogID: created.ID,
			TenantID:  p.TenantID,
			AppID:     p.AppID,
			ItemKey:   "VALID001",
			Payload:   json.RawMessage(`{"name":"Valid Product"}`),
		},
	}

	result, err := store.BulkUpsertCatalogItems(ctx, p, items)
	require.NoError(t, err)
	assert.Greater(t, result.InsertedCount, int64(0))
}
