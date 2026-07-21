package render

import (
	"context"
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
	items map[string]domain.CatalogItem
}

func (m *mockStore) GetCatalogItem(ctx context.Context, p domain.Principal, catalogID, itemKey string) (domain.CatalogItem, error) {
	if item, ok := m.items[itemKey]; ok {
		return item, nil
	}
	return domain.CatalogItem{}, ports.ErrNotFound
}

func TestRenderWithContextBackwardCompatible(t *testing.T) {
	// Test that RenderWithContext works like Render for profile attributes
	tmpl := "Hello, {{ name | capitalize }}!"
	vars := map[string]any{"name": "world"}

	deps := RenderDeps{
		Store:     &mockStore{items: make(map[string]domain.CatalogItem)},
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
		Store:     &mockStore{items: make(map[string]domain.CatalogItem)},
		Principal: domain.Principal{TenantID: "test", AppID: "app1"},
		Fetcher:   nil,
	}

	out, err := RenderWithContext(context.Background(), tmpl, vars, deps)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// Missing item should render to empty string (fallback), not fail
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
		Store:     &mockStore{items: make(map[string]domain.CatalogItem)},
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

