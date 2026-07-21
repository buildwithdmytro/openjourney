package render

import (
	"context"
	"fmt"

	"github.com/buildwithdmytro/openjourney/internal/domain"
	"github.com/buildwithdmytro/openjourney/internal/ports"
	"github.com/osteele/liquid"
	liquidrender "github.com/osteele/liquid/render"
)

// ConnectedContentFetcher handles fetching external data at render time.
type ConnectedContentFetcher interface {
	// Fetch retrieves data from a connected-content source.
	// Returns the fetched data or nil/fallback on error.
	Fetch(ctx context.Context, principal domain.Principal, url string, ttl int) (map[string]any, error)
}

// RenderDeps holds the dependencies needed for render-time catalog and connected-content access.
type RenderDeps struct {
	Store    ports.Store
	Principal domain.Principal
	Fetcher  ConnectedContentFetcher
}

func Render(tmpl string, vars map[string]any) (string, error) {
	engine := NewEngine()
	out, err := engine.ParseAndRenderString(tmpl, vars)
	if err != nil {
		return "", err
	}
	return out, nil
}

// RenderWithContext renders a template with access to store, principal, and fetcher
// for catalog lookups and connected-content tags.
func RenderWithContext(ctx context.Context, tmpl string, vars map[string]any, deps RenderDeps) (string, error) {
	engine := NewEngine()

	// Register the catalog_item filter
	RegisterFilter(engine, "catalog_item", func(value interface{}, args ...interface{}) (interface{}, error) {
		if len(args) == 0 {
			return "", nil
		}
		// In the Liquid template: {{ item_key | catalog_item: 'catalog_key' }}
		// args[0] is 'catalog_key', value is the item_key
		_, ok := args[0].(string)
		if !ok {
			return "", nil
		}
		itemKey, ok := value.(string)
		if !ok {
			return "", nil
		}

		// Look up the catalog item from the store (cache-first, via store implementation)
		// For now (20.4 stub), just return fallback on any error
		item, err := deps.Store.GetCatalogItem(ctx, deps.Principal, "", itemKey)
		if err != nil {
			// Missing item: fallback to empty, never fail the render
			return "", nil
		}
		return item.Payload, nil
	})

	// Register the connected_content tag (stub)
	RegisterTag(engine, "connected_content", func(ctx liquidrender.Context) (string, error) {
		// For now, return empty string (tag renders nothing, it binds vars)
		// Full implementation in 20.7
		return "", nil
	})

	out, err := engine.ParseAndRenderString(tmpl, vars)
	if err != nil {
		return "", err
	}
	return out, nil
}

// RegisterFilter adds a filter to a Liquid engine. Keeping registration here
// gives extension-backed filters the same seam as built-in filters.
func RegisterFilter(engine *liquid.Engine, name string, fn interface{}) {
	engine.RegisterFilter(name, fn)
}

// RegisterTag adds a tag to a Liquid engine. Callers should use this helper
// instead of reaching into the engine when installing governed extensions.
func RegisterTag(engine *liquid.Engine, name string, fn liquid.Renderer) {
	engine.RegisterTag(name, fn)
}

// NewEngine returns the secured Liquid engine used by Render. It is useful to
// callers that need to install extension filters before rendering.
func NewEngine() *liquid.Engine {
	engine := liquid.NewEngine()
	// Secure Liquid rendering environment by disabling the include tag explicitly
	RegisterTag(engine, "include", func(ctx liquidrender.Context) (string, error) {
		return "", fmt.Errorf("include tag is disabled")
	})
	return engine
}
