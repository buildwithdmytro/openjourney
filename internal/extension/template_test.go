package extension

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/buildwithdmytro/openjourney/internal/domain"
	"github.com/buildwithdmytro/openjourney/internal/render"
	"github.com/osteele/liquid"
)

func (m *mockStore) ListExtensions(context.Context, domain.Principal) ([]domain.Extension, error) {
	result := make([]domain.Extension, 0, len(m.extensions))
	for _, ext := range m.extensions {
		result = append(result, ext)
	}
	return result, nil
}

func templateFixture(t *testing.T, extID, versionID, manifest string) (*Host, *mockStore, domain.Principal) {
	t.Helper()
	wasm := compileWasm(t, `
package main
import "os"
func main() { os.Stdout.Write([]byte("\"rendered-by-wasm\"")) }
`)
	store := newMockStore()
	store.extensions[extID] = domain.Extension{ID: extID, Status: "enabled", CurrentVersionID: &versionID}
	store.versions[versionID] = domain.ExtensionVersion{
		ID: versionID, ExtensionID: extID, Version: 1, Kind: "template_function",
		Transport: "wasm", Status: "active", Manifest: json.RawMessage(manifest), WasmBlobKey: stringPtr("template-wasm"),
	}
	store.configs[extID] = domain.ExtensionConfig{ExtensionID: extID, Status: "active", TimeoutMs: 2000, MaxMemoryMb: 64}
	host := NewHost(store)
	host.SetBlobStore(&mockBlobStore{blobs: map[string][]byte{"template-wasm": wasm}})
	return host, store, domain.Principal{TenantID: "tenant-1", WorkspaceID: "workspace-1"}
}

func TestTemplateFunctionFilterUsesWasmAndAudits(t *testing.T) {
	host, store, principal := templateFixture(t, "template-filter", "template-filter-v1", `{"filter_name":"ext_render","export":"run"}`)
	engine := render.NewEngine()
	if err := RegisterTemplateFunction(engine, host, principal, TemplateRegistration{
		Name: "ext_render", ExtensionID: "template-filter", Invocation: "run",
	}); err != nil {
		t.Fatal(err)
	}
	out, err := engine.ParseAndRenderString(`{{ name | ext_render }}`, map[string]any{"name": "input"})
	if err != nil {
		t.Fatal(err)
	}
	if out != "rendered-by-wasm" {
		t.Fatalf("expected Wasm filter result, got %q", out)
	}
	if len(store.activities) != 1 || store.activities[0].PolicyDecision != "allowed" {
		t.Fatalf("expected one allowed audited invocation, got %#v", store.activities)
	}
}

func TestTemplateFunctionDiscoveryRegistersTag(t *testing.T) {
	host, _, principal := templateFixture(t, "template-tag", "template-tag-v1", `{"tag_name":"ext_tag","wasm_export":"run"}`)
	engine := liquid.NewEngine()
	if err := RegisterTemplateFunctions(context.Background(), engine, host.store, host, principal); err != nil {
		t.Fatal(err)
	}
	out, err := engine.ParseAndRenderString(`{% ext_tag ignored %}`, nil)
	if err != nil {
		t.Fatal(err)
	}
	if out != "rendered-by-wasm" {
		t.Fatalf("expected Wasm tag result, got %q", out)
	}
}
