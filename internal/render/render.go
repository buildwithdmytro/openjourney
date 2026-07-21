package render

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

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
	Cache    *TTLCache
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

		catalogKey, ok := args[0].(string)
		if !ok {
			return "", nil
		}

		itemKey, ok := value.(string)
		if !ok {
			return "", nil
		}

		// Build cache key: catalog:tenant:app:catalogkey:itemkey
		cacheKey := fmt.Sprintf("catalog:%s:%s:%s:%s", deps.Principal.TenantID, deps.Principal.AppID, catalogKey, itemKey)

		// Check cache first
		if deps.Cache != nil {
			if cached, ok := deps.Cache.Get(cacheKey); ok {
				return cached, nil
			}
		}

		// Look up catalog by key
		catalog, err := deps.Store.GetCatalogByKey(ctx, deps.Principal, catalogKey)
		if err != nil {
			// Missing catalog: fallback to empty, never fail the render
			return "", nil
		}

		// Look up catalog item
		item, err := deps.Store.GetCatalogItem(ctx, deps.Principal, catalog.ID, itemKey)
		if err != nil {
			// Missing item: fallback to empty, never fail the render
			return "", nil
		}

		// Unmarshal payload to a map[string]any for proper rendering
		var payload map[string]any
		if err := json.Unmarshal(item.Payload, &payload); err != nil {
			// Malformed JSON: fallback to empty, never fail the render
			return "", nil
		}

		// Cache the result with a default TTL of 5 minutes
		if deps.Cache != nil {
			deps.Cache.Set(cacheKey, payload, 5*time.Minute)
		}

		return payload, nil
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
