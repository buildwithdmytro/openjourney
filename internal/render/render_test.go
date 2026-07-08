package render

import (
	"strings"
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

