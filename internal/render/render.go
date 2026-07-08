package render

import (
	"fmt"

	"github.com/osteele/liquid"
	liquidrender "github.com/osteele/liquid/render"
)

func Render(tmpl string, vars map[string]any) (string, error) {
	engine := liquid.NewEngine()
	// Secure Liquid rendering environment by disabling the include tag explicitly
	engine.RegisterTag("include", func(ctx liquidrender.Context) (string, error) {
		return "", fmt.Errorf("include tag is disabled")
	})
	out, err := engine.ParseAndRenderString(tmpl, vars)
	if err != nil {
		return "", err
	}
	return out, nil
}

