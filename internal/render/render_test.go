package render

import (
	"testing"
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
