package render

import (
	"fmt"

	"github.com/osteele/liquid"
	liquidrender "github.com/osteele/liquid/render"
)

func Render(tmpl string, vars map[string]any) (string, error) {
	engine := NewEngine()
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
