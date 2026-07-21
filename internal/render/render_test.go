package render

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/buildwithdmytro/openjourney/internal/domain"
	"github.com/buildwithdmytro/openjourney/internal/ports"
)

func TestRender(t *testing.T) {
	tmpl := "Hello, {{ name | capitalize }} from {{ country }}!"
	vars := map[string]any{
		"name":    "ada",
		"country": "US",
	}
	out, err := Render(tmpl, vars)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	expected := "Hello, Ada from US!"
	if out != expected {
		t.Errorf("expected %q, got %q", expected, out)
	}
}

func TestLiquidSandbox(t *testing.T) {
	tmpl := `Some text. {% include "secret.txt" %} other text.`
	vars := map[string]any{}
	_, err := Render(tmpl, vars)
	if err == nil {
		t.Fatal("expected error when using include tag, but rendering succeeded")
	}
	if !strings.Contains(err.Error(), "include tag is disabled") {
		t.Errorf("expected error message to mention include tag shutdown, got: %v", err)
	}
}

// Mock store for testing RenderWithContext
type mockStore struct {
	ports.Store
	catalogs map[string]domain.Catalog           // key -> Catalog
	items    map[string]domain.CatalogItem       // catalogID:itemKey -> CatalogItem
}

func (m *mockStore) GetCatalogByKey(ctx context.Context, p domain.Principal, key string) (domain.Catalog, error) {
	if cat, ok := m.catalogs[key]; ok {
		return cat, nil
	}
	return domain.Catalog{}, ports.ErrNotFound
}

func (m *mockStore) GetCatalogItem(ctx context.Context, p domain.Principal, catalogID, itemKey string) (domain.CatalogItem, error) {
	key := catalogID + ":" + itemKey
	if item, ok := m.items[key]; ok {
		return item, nil
	}
	return domain.CatalogItem{}, ports.ErrNotFound
}

func TestRenderWithContextBackwardCompatible(t *testing.T) {
	// Test that RenderWithContext works like Render for profile attributes
	tmpl := "Hello, {{ name | capitalize }}!"
	vars := map[string]any{"name": "world"}

	deps := RenderDeps{
		Store:     &mockStore{catalogs: make(map[string]domain.Catalog), items: make(map[string]domain.CatalogItem)},
		Principal: domain.Principal{TenantID: "test", AppID: "app1"},
		Fetcher:   nil,
	}

	out, err := RenderWithContext(context.Background(), tmpl, vars, deps)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	expected := "Hello, World!"
	if out != expected {
		t.Errorf("expected %q, got %q", expected, out)
	}
}

func TestRenderWithContextCatalogItemFilterMissing(t *testing.T) {
	// Test that catalog_item filter degrades to fallback on missing item
	tmpl := `Item: {{ item_key | catalog_item: 'catalog1' }}`
	vars := map[string]any{"item_key": "missing"}

	deps := RenderDeps{
		Store:     &mockStore{catalogs: make(map[string]domain.Catalog), items: make(map[string]domain.CatalogItem)},
		Principal: domain.Principal{TenantID: "test", AppID: "app1"},
		Fetcher:   nil,
	}

	out, err := RenderWithContext(context.Background(), tmpl, vars, deps)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// Missing catalog should render to empty string (fallback), not fail
	expected := "Item: "
	if out != expected {
		t.Errorf("expected %q, got %q", expected, out)
	}
}

func TestRenderWithContextConnectedContentTag(t *testing.T) {
	// Test that connected_content tag is registered (stub)
	tmpl := `Before{% connected_content "http://api.example.com/data" save: data %}After`
	vars := map[string]any{}

	deps := RenderDeps{
		Store:     &mockStore{catalogs: make(map[string]domain.Catalog), items: make(map[string]domain.CatalogItem)},
		Principal: domain.Principal{TenantID: "test", AppID: "app1"},
		Fetcher:   nil,
	}

	out, err := RenderWithContext(context.Background(), tmpl, vars, deps)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// Tag should render to nothing (tag doesn't output, it just binds vars)
	expected := "BeforeAfter"
	if out != expected {
		t.Errorf("expected %q, got %q", expected, out)
	}
}

func TestCatalogItemFilterWithRealItem(t *testing.T) {
	// Test that catalog_item filter renders a real item's payload
	tmpl := `SKU: {{ sku | catalog_item: 'products' }}`
	vars := map[string]any{"sku": "prod123"}

	// Create test data
	productPayload := map[string]any{
		"name":  "Laptop",
		"price": 999.99,
	}
	payloadJSON, err := json.Marshal(productPayload)
	if err != nil {
		t.Fatalf("failed to marshal payload: %v", err)
	}

	catalog := domain.Catalog{
		ID:  "cat-id-1",
		Key: "products",
	}

	item := domain.CatalogItem{
		CatalogID: "cat-id-1",
		ItemKey:   "prod123",
		Payload:   payloadJSON,
	}

	store := &mockStore{
		catalogs: map[string]domain.Catalog{"products": catalog},
		items:    map[string]domain.CatalogItem{"cat-id-1:prod123": item},
	}

	deps := RenderDeps{
		Store:     store,
		Principal: domain.Principal{TenantID: "test", AppID: "app1"},
		Fetcher:   nil,
		Cache:     NewTTLCache(100, SystemClock{}),
	}

	out, err := RenderWithContext(context.Background(), tmpl, vars, deps)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Check that the payload is rendered as JSON
	if !strings.Contains(out, "Laptop") || !strings.Contains(out, "999.99") {
		t.Errorf("expected payload to be rendered, got %q", out)
	}
}

func TestCatalogItemFilterCaching(t *testing.T) {
	// Test that the second lookup is served from cache
	tmpl := `{{ sku | catalog_item: 'products' }}`
	vars := map[string]any{"sku": "prod456"}

	productPayload := map[string]any{"name": "Mouse", "price": 29.99}
	payloadJSON, err := json.Marshal(productPayload)
	if err != nil {
		t.Fatalf("failed to marshal payload: %v", err)
	}

	catalog := domain.Catalog{
		ID:  "cat-id-2",
		Key: "products",
	}

	item := domain.CatalogItem{
		CatalogID: "cat-id-2",
		ItemKey:   "prod456",
		Payload:   payloadJSON,
	}

	store := &mockStore{
		catalogs: map[string]domain.Catalog{"products": catalog},
		items:    map[string]domain.CatalogItem{"cat-id-2:prod456": item},
	}

	cache := NewTTLCache(100, SystemClock{})
	deps := RenderDeps{
		Store:     store,
		Principal: domain.Principal{TenantID: "test", AppID: "app1"},
		Fetcher:   nil,
		Cache:     cache,
	}

	// First render - should hit store and cache
	ctx := context.Background()
	out1, err := RenderWithContext(ctx, tmpl, vars, deps)
	if err != nil {
		t.Fatalf("first render failed: %v", err)
	}

	// Check cache was populated
	cacheKey := "catalog:test:app1:products:prod456"
	cached, ok := cache.Get(cacheKey)
	if !ok {
		t.Fatal("expected item to be cached after first render")
	}

	cachedPayload, ok := cached.(map[string]any)
	if !ok {
		t.Fatal("cached value should be map[string]any")
	}

	if cachedPayload["name"] != "Mouse" {
		t.Errorf("cached payload incorrect: %v", cachedPayload)
	}

	// Second render - should use cache
	out2, err := RenderWithContext(ctx, tmpl, vars, deps)
	if err != nil {
		t.Fatalf("second render failed: %v", err)
	}

	// Both renders should produce same output
	if out1 != out2 {
		t.Errorf("renders should be identical: %q vs %q", out1, out2)
	}
}

func TestCatalogItemFilterMissingCatalog(t *testing.T) {
	// Test fallback when catalog doesn't exist
	tmpl := `Item: {{ sku | catalog_item: 'missing_catalog' }}`
	vars := map[string]any{"sku": "sku123"}

	store := &mockStore{
		catalogs: make(map[string]domain.Catalog),
		items:    make(map[string]domain.CatalogItem),
	}

	deps := RenderDeps{
		Store:     store,
		Principal: domain.Principal{TenantID: "test", AppID: "app1"},
		Fetcher:   nil,
		Cache:     NewTTLCache(100, SystemClock{}),
	}

	out, err := RenderWithContext(context.Background(), tmpl, vars, deps)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Should fall back to empty string
	expected := "Item: "
	if out != expected {
		t.Errorf("expected %q, got %q", expected, out)
	}
}

func TestCatalogItemFilterMissingItem(t *testing.T) {
	// Test fallback when item doesn't exist
	tmpl := `Item: {{ sku | catalog_item: 'products' }}`
	vars := map[string]any{"sku": "missing_sku"}

	catalog := domain.Catalog{
		ID:  "cat-id-3",
		Key: "products",
	}

	store := &mockStore{
		catalogs: map[string]domain.Catalog{"products": catalog},
		items:    make(map[string]domain.CatalogItem),
	}

	deps := RenderDeps{
		Store:     store,
		Principal: domain.Principal{TenantID: "test", AppID: "app1"},
		Fetcher:   nil,
		Cache:     NewTTLCache(100, SystemClock{}),
	}

	out, err := RenderWithContext(context.Background(), tmpl, vars, deps)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Should fall back to empty string
	expected := "Item: "
	if out != expected {
		t.Errorf("expected %q, got %q", expected, out)
	}
}

func TestCatalogItemFilterNoCacheParameter(t *testing.T) {
	// Test that filter works without a cache (backward compatibility)
	tmpl := `{{ sku | catalog_item: 'products' }}`
	vars := map[string]any{"sku": "prod789"}

	productPayload := map[string]any{"name": "Keyboard"}
	payloadJSON, err := json.Marshal(productPayload)
	if err != nil {
		t.Fatalf("failed to marshal payload: %v", err)
	}

	catalog := domain.Catalog{
		ID:  "cat-id-4",
		Key: "products",
	}

	item := domain.CatalogItem{
		CatalogID: "cat-id-4",
		ItemKey:   "prod789",
		Payload:   payloadJSON,
	}

	store := &mockStore{
		catalogs: map[string]domain.Catalog{"products": catalog},
		items:    map[string]domain.CatalogItem{"cat-id-4:prod789": item},
	}

	deps := RenderDeps{
		Store:     store,
		Principal: domain.Principal{TenantID: "test", AppID: "app1"},
		Fetcher:   nil,
		Cache:     nil, // No cache
	}

	out, err := RenderWithContext(context.Background(), tmpl, vars, deps)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !strings.Contains(out, "Keyboard") {
		t.Errorf("expected Keyboard in output, got %q", out)
	}
}

