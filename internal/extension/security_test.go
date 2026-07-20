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

func TestValidateNativeConnectorConfigRejectsRawSecrets(t *testing.T) {
	tests := []struct {
		name      string
		transport string
		config    string
		wantError bool
	}{
		{
			name:      "s3 with raw secret_key",
			transport: "s3",
			config:    `{"bucket":"mybucket","secret_key":"mysecret"}`,
			wantError: true,
		},
		{
			name:      "s3 with raw access_key",
			transport: "s3",
			config:    `{"bucket":"mybucket","access_key":"mykey"}`,
			wantError: true,
		},
		{
			name:      "s3 with secret references only",
			transport: "s3",
			config:    `{"bucket":"mybucket","access_key_ref":"AWS_KEY","secret_key_ref":"AWS_SECRET"}`,
			wantError: false,
		},
		{
			name:      "clickhouse with raw password",
			transport: "clickhouse",
			config:    `{"host":"localhost","password":"mypass"}`,
			wantError: true,
		},
		{
			name:      "clickhouse with secret reference",
			transport: "clickhouse",
			config:    `{"host":"localhost","password_ref":"CH_PASS"}`,
			wantError: false,
		},
		{
			name:      "kafka with raw secret_key",
			transport: "kafka",
			config:    `{"brokers":"localhost:9092","secret_key":"mysecret"}`,
			wantError: true,
		},
		{
			name:      "kafka with secret reference",
			transport: "kafka",
			config:    `{"brokers":"localhost:9092","secret_key_ref":"KAFKA_SECRET"}`,
			wantError: false,
		},
		{
			name:      "webhook with raw hmac_secret",
			transport: "webhook",
			config:    `{"url":"https://example.com/webhook","hmac_secret":"secret123"}`,
			wantError: true,
		},
		{
			name:      "webhook with secret reference",
			transport: "webhook",
			config:    `{"url":"https://example.com/webhook","hmac_secret_ref":"WEBHOOK_KEY"}`,
			wantError: false,
		},
		{
			name:      "unsupported transport",
			transport: "custom",
			config:    `{"password":"raw"}`,
			wantError: false,
		},
		{
			name:      "empty config allowed",
			transport: "s3",
			config:    `{}`,
			wantError: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateNativeConnectorConfig(tt.transport, json.RawMessage(tt.config))
			if (err != nil) != tt.wantError {
				t.Fatalf("ValidateNativeConnectorConfig() error = %v, wantError %v", err, tt.wantError)
			}
			if tt.wantError && err != nil {
				if !strings.Contains(err.Error(), "_ref") {
					t.Fatalf("expected error to suggest using _ref suffix, got: %v", err)
				}
			}
		})
	}
}

func TestRedactExtensionConfigRemovesRawSecrets(t *testing.T) {
	config := map[string]any{
		"host":           "localhost",
		"access_key":     "should-be-removed",
		"access_key_ref": "AWS_KEY",
		"secret_key":     "should-be-removed",
		"secret_key_ref": "AWS_SECRET",
		"password":       "should-be-removed",
		"password_ref":   "PASSWORD_REF",
		"hmac_secret":    "should-be-removed",
		"hmac_secret_ref": "HMAC_REF",
		"public_key":     "this-is-ok",
	}

	redacted := RedactExtensionConfig(config)

	// Check that raw secrets are removed
	if _, exists := redacted["access_key"]; exists {
		t.Fatal("access_key should be redacted")
	}
	if _, exists := redacted["secret_key"]; exists {
		t.Fatal("secret_key should be redacted")
	}
	if _, exists := redacted["password"]; exists {
		t.Fatal("password should be redacted")
	}
	if _, exists := redacted["hmac_secret"]; exists {
		t.Fatal("hmac_secret should be redacted")
	}

	// Check that references and other fields are preserved
	if redacted["access_key_ref"] != "AWS_KEY" {
		t.Fatal("access_key_ref should be preserved")
	}
	if redacted["secret_key_ref"] != "AWS_SECRET" {
		t.Fatal("secret_key_ref should be preserved")
	}
	if redacted["password_ref"] != "PASSWORD_REF" {
		t.Fatal("password_ref should be preserved")
	}
	if redacted["hmac_secret_ref"] != "HMAC_REF" {
		t.Fatal("hmac_secret_ref should be preserved")
	}
	if redacted["host"] != "localhost" {
		t.Fatal("host should be preserved")
	}
	if redacted["public_key"] != "this-is-ok" {
		t.Fatal("public_key should be preserved")
	}
}
