package render

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
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

	// Capture the context for use in tag/filter closures
	renderCtx := ctx

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
		catalog, err := deps.Store.GetCatalogByKey(renderCtx, deps.Principal, catalogKey)
		if err != nil {
			// Missing catalog: fallback to empty, never fail the render
			return "", nil
		}

		// Look up catalog item
		item, err := deps.Store.GetCatalogItem(renderCtx, deps.Principal, catalog.ID, itemKey)
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

	// Register the connected_content tag
	RegisterTag(engine, "connected_content", func(ctx liquidrender.Context) (string, error) {
		// TagArgs returns a string like: "url" save: var ttl: 300
		// We need to parse this carefully to handle quoted strings
		argStr := ctx.TagArgs()
		if argStr == "" {
			// No URL provided, fallback silently
			return "", nil
		}

		// Parse the first argument as the URL (should be quoted)
		// Look for the first quoted string
		var urlExpr string
		var restArgs string
		if strings.HasPrefix(argStr, `"`) {
			// Find the closing quote, handling escapes
			idx := 1
			for idx < len(argStr) {
				if argStr[idx] == '"' && (idx == 0 || argStr[idx-1] != '\\') {
					urlExpr = argStr[1:idx]
					if idx+1 < len(argStr) {
						restArgs = strings.TrimSpace(argStr[idx+1:])
					}
					break
				}
				idx++
			}
		} else if strings.HasPrefix(argStr, "'") {
			// Handle single quotes as well
			idx := 1
			for idx < len(argStr) {
				if argStr[idx] == '\'' && (idx == 0 || argStr[idx-1] != '\\') {
					urlExpr = argStr[1:idx]
					if idx+1 < len(argStr) {
						restArgs = strings.TrimSpace(argStr[idx+1:])
					}
					break
				}
				idx++
			}
		}

		if urlExpr == "" {
			return "", nil
		}

		// Evaluate the URL expression
		urlValue, err := ctx.EvaluateString(urlExpr)
		if err != nil {
			return "", nil
		}

		fetchURL, ok := urlValue.(string)
		if !ok || fetchURL == "" {
			return "", nil
		}

		// Parse options: save: var ttl: 300
		saveVar := ""
		ttl := 300

		parts := strings.Fields(restArgs)
		i := 0
		for i < len(parts) {
			if parts[i] == "save:" && i+1 < len(parts) {
				saveVar = parts[i+1]
				i += 2
			} else if parts[i] == "ttl:" && i+1 < len(parts) {
				ttlVal, err := ctx.EvaluateString(parts[i+1])
				if err == nil {
					if f, ok := ttlVal.(float64); ok {
						ttl = int(f)
					} else if s, ok := ttlVal.(string); ok {
						if parsed, err := strconv.Atoi(s); err == nil {
							ttl = parsed
						}
					}
				}
				i += 2
			} else {
				i++
			}
		}

		if deps.Fetcher == nil {
			return "", nil
		}

		// Fetch the data
		data, err := deps.Fetcher.Fetch(renderCtx, deps.Principal, fetchURL, ttl)
		if err != nil {
			return "", nil
		}

		if data == nil {
			return "", nil
		}

		// Bind the result to the save: variable if specified
		if saveVar != "" {
			bindings := ctx.Bindings()
			bindings[saveVar] = data
		}

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
