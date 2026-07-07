package render

import (
	"github.com/osteele/liquid"
)

func Render(tmpl string, vars map[string]any) (string, error) {
	engine := liquid.NewEngine()
	out, err := engine.ParseAndRenderString(tmpl, vars)
	if err != nil {
		return "", err
	}
	return out, nil
}
