package extension

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/buildwithdmytro/openjourney/internal/domain"
)

func TestSecurityE2E_RequiredScopeIsDeniedAndAudited(t *testing.T) {
	store := newMockStore()
	host := NewHost(store)

	extID, versionID := "security-scope", "security-scope-v1"
	store.extensions[extID] = domain.Extension{ID: extID, Status: "enabled", CurrentVersionID: &versionID}
	store.versions[versionID] = domain.ExtensionVersion{ID: versionID, ExtensionID: extID, Version: 1, Kind: "channel_provider", Transport: "remote_http", RequestedScopes: []string{"profiles:write"}}
	store.configs[extID] = domain.ExtensionConfig{ExtensionID: extID, Status: "active", Config: json.RawMessage(`{"base_url":"http://example.com"}`), EndpointAllowlist: []string{"http://example.com"}, TimeoutMs: 1000}
	store.grants[extID] = []domain.ExtensionGrant{{ExtensionID: extID, Scope: "profiles:read"}}
	host.httpClient.Transport = &mockRoundTripper{RoundTripFunc: func(req *http.Request) (*http.Response, error) {
		t.Fatal("scope-denied extension reached remote transport")
		return nil, nil
	}}

	_, _, err := host.InvokeWithScope(context.Background(), domain.Principal{TenantID: "tenant", WorkspaceID: "workspace"}, extID, "send", "profiles:write", json.RawMessage(`{}`))
	if err == nil || !strings.Contains(err.Error(), "not granted") {
		t.Fatalf("expected denied scope, got %v", err)
	}
	if len(store.activities) != 1 || store.activities[0].PolicyDecision != "denied_scope" {
		t.Fatalf("expected one denied_scope activity, got %+v", store.activities)
	}
}

func TestSecurityE2E_WasmCannotReachNetworkOrFilesystem(t *testing.T) {
	src := `
package main
import ("fmt"; "net"; "os")
func main() { _, networkErr := net.Dial("tcp", "example.com:80"); if networkErr == nil { fmt.Print("network-open") } else { fmt.Print("network-denied") }; if _, fileErr := os.Open("/etc/passwd"); fileErr == nil { fmt.Print(" file-open") } else { fmt.Print(" file-denied") } }
`
	wasmBytes := compileWasm(t, src)
	store := newMockStore()
	host := NewHost(store)
	host.SetBlobStore(&mockBlobStore{blobs: map[string][]byte{"security-wasm": wasmBytes}})
	extID, versionID := "security-wasm", "security-wasm-v1"
	store.extensions[extID] = domain.Extension{ID: extID, Status: "enabled", CurrentVersionID: &versionID}
	store.versions[versionID] = domain.ExtensionVersion{ID: versionID, ExtensionID: extID, Version: 1, Kind: "ingestion_transform", Transport: "wasm", WasmBlobKey: stringPtr("security-wasm")}
	store.configs[extID] = domain.ExtensionConfig{ExtensionID: extID, Status: "active", TimeoutMs: 5000, MaxMemoryMb: 64}
	output, _, err := host.Invoke(context.Background(), domain.Principal{TenantID: "tenant", WorkspaceID: "workspace"}, extID, "transform", json.RawMessage(`{}`))
	if err != nil {
		t.Fatalf("sandbox invocation failed: %v", err)
	}
	if got := strings.TrimSpace(string(output)); got != "network-denied file-denied" {
		t.Fatalf("sandbox escaped network/filesystem restrictions: %q", got)
	}
	if len(store.activities) != 1 || store.activities[0].PolicyDecision != "allowed" {
		t.Fatalf("expected one allowed audited invocation, got %+v", store.activities)
	}
}

func TestValidateRemoteHTTPConfigRequiresSecretReference(t *testing.T) {
	for name, raw := range map[string]string{"missing": `{"base_url":"https://connector.example"}`, "raw secret": `{"base_url":"https://connector.example","hmac_secret":"secret"}`} {
		t.Run(name, func(t *testing.T) {
			if err := ValidateRemoteHTTPConfig("remote_http", json.RawMessage(raw)); err == nil {
				t.Fatal("expected remote HMAC validation error")
			}
		})
	}
	if err := ValidateRemoteHTTPConfig("remote_http", json.RawMessage(`{"hmac_secret_ref":"CONNECTOR_HMAC"}`)); err != nil {
		t.Fatalf("valid secret reference rejected: %v", err)
	}
	if err := validateResolvedRemoteHMAC(map[string]any{"hmac_secret": ""}); err == nil || !strings.Contains(err.Error(), "not configured") {
		t.Fatalf("expected unresolved secret error, got %v", err)
	}
}
