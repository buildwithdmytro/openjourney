package extension

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/buildwithdmytro/openjourney/internal/domain"
	"github.com/buildwithdmytro/openjourney/internal/ports"
)

type mockBlobStore struct {
	ports.BlobStore
	blobs map[string][]byte
}

func (m *mockBlobStore) Get(ctx context.Context, key string) ([]byte, error) {
	b, ok := m.blobs[key]
	if !ok {
		return nil, errors.New("blob not found")
	}
	return b, nil
}

func compileWasm(t *testing.T, src string) []byte {
	tmpDir := t.TempDir()
	srcPath := filepath.Join(tmpDir, "main.go")
	if err := os.WriteFile(srcPath, []byte(src), 0644); err != nil {
		t.Fatal(err)
	}

	wasmPath := filepath.Join(tmpDir, "main.wasm")
	// Compile to WASIP1 Wasm using the Go toolchain
	cmd := exec.Command("go", "build", "-o", wasmPath, srcPath)
	cmd.Env = append(os.Environ(), "GOOS=wasip1", "GOARCH=wasm")

	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("failed to compile wasm: %v, output: %s", err, string(output))
	}

	wasmBytes, err := os.ReadFile(wasmPath)
	if err != nil {
		t.Fatal(err)
	}
	return wasmBytes
}

func TestWasm_Deterministic(t *testing.T) {
	src := `
package main

import (
	"io"
	"os"
)

func main() {
	input, _ := io.ReadAll(os.Stdin)
	// Just reverse/echo back the JSON input
	os.Stdout.Write(input)
}
`
	wasmBytes := compileWasm(t, src)

	store := newMockStore()
	blobs := &mockBlobStore{blobs: map[string][]byte{"wasm-key": wasmBytes}}
	host := NewHost(store)
	host.SetBlobStore(blobs)

	extID := "ext-wasm-1"
	versionID := "ver-wasm-1"
	active := "enabled"
	store.extensions[extID] = domain.Extension{
		ID:               extID,
		Status:           active,
		CurrentVersionID: &versionID,
	}

	store.versions[versionID] = domain.ExtensionVersion{
		ID:              versionID,
		ExtensionID:     extID,
		Version:         1,
		Kind:            "ingestion_transform",
		Transport:       "wasm",
		WasmBlobKey:     stringPtr("wasm-key"),
		RequestedScopes: []string{},
	}

	store.configs[extID] = domain.ExtensionConfig{
		ExtensionID: extID,
		Status:      "active",
		TimeoutMs:   10000,
		MaxMemoryMb: 64,
	}

	principal := domain.Principal{
		TenantID:    "tenant-1",
		WorkspaceID: "workspace-1",
	}

	input := json.RawMessage(`{"value":42}`)

	// Call 1
	res1, _, err := host.Invoke(context.Background(), principal, extID, "transform", input)
	if err != nil {
		t.Fatalf("Invoke failed: %v", err)
	}

	// Call 2
	res2, _, err := host.Invoke(context.Background(), principal, extID, "transform", input)
	if err != nil {
		t.Fatalf("Invoke failed: %v", err)
	}

	if string(res1) != string(res2) {
		t.Errorf("execution was not deterministic: %s vs %s", string(res1), string(res2))
	}

	if string(res1) != `{"value":42}` {
		t.Errorf("unexpected output: %s", string(res1))
	}

	if len(store.activities) != 2 {
		t.Fatalf("expected exactly one audit row per invocation, got %d", len(store.activities))
	}
	for i, activity := range store.activities {
		if activity.PolicyDecision != "allowed" {
			t.Errorf("activity %d: expected allowed decision, got %q", i, activity.PolicyDecision)
		}
	}
}

func TestWasm_DeadlineKill(t *testing.T) {
	src := `
package main

func main() {
	// Infinite busy loop to trigger timeout
	for {
	}
}
`
	wasmBytes := compileWasm(t, src)

	store := newMockStore()
	blobs := &mockBlobStore{blobs: map[string][]byte{"wasm-key": wasmBytes}}
	host := NewHost(store)
	host.SetBlobStore(blobs)

	extID := "ext-wasm-2"
	versionID := "ver-wasm-2"
	active := "enabled"
	store.extensions[extID] = domain.Extension{
		ID:               extID,
		Status:           active,
		CurrentVersionID: &versionID,
	}

	store.versions[versionID] = domain.ExtensionVersion{
		ID:              versionID,
		ExtensionID:     extID,
		Version:         1,
		Kind:            "ingestion_transform",
		Transport:       "wasm",
		WasmBlobKey:     stringPtr("wasm-key"),
		RequestedScopes: []string{},
	}

	store.configs[extID] = domain.ExtensionConfig{
		ExtensionID: extID,
		Status:      "active",
		TimeoutMs:   100, // Short timeout
		MaxMemoryMb: 64,
	}

	principal := domain.Principal{
		TenantID:    "tenant-1",
		WorkspaceID: "workspace-1",
	}

	_, _, err := host.Invoke(context.Background(), principal, extID, "transform", json.RawMessage(`{}`))
	if err == nil {
		t.Fatal("expected timeout/deadline error, got nil")
	}

	if len(store.activities) != 1 {
		t.Fatalf("expected 1 activity row, got %d", len(store.activities))
	}

	decision := store.activities[0].PolicyDecision
	if decision != "timeout" {
		t.Errorf("expected decision to be timeout, got %s", decision)
	}
}

func TestWasm_NoFileAccess(t *testing.T) {
	src := `
package main

import (
	"os"
)

func main() {
	_, err := os.Open("/etc/passwd")
	if err != nil {
		os.Stdout.Write([]byte("denied"))
		return
	}
	os.Stdout.Write([]byte("allowed"))
}
`
	wasmBytes := compileWasm(t, src)

	store := newMockStore()
	blobs := &mockBlobStore{blobs: map[string][]byte{"wasm-key": wasmBytes}}
	host := NewHost(store)
	host.SetBlobStore(blobs)

	extID := "ext-wasm-3"
	versionID := "ver-wasm-3"
	active := "enabled"
	store.extensions[extID] = domain.Extension{
		ID:               extID,
		Status:           active,
		CurrentVersionID: &versionID,
	}

	store.versions[versionID] = domain.ExtensionVersion{
		ID:              versionID,
		ExtensionID:     extID,
		Version:         1,
		Kind:            "ingestion_transform",
		Transport:       "wasm",
		WasmBlobKey:     stringPtr("wasm-key"),
		RequestedScopes: []string{},
	}

	store.configs[extID] = domain.ExtensionConfig{
		ExtensionID: extID,
		Status:      "active",
		TimeoutMs:   10000,
		MaxMemoryMb: 64,
	}

	principal := domain.Principal{
		TenantID:    "tenant-1",
		WorkspaceID: "workspace-1",
	}

	res, _, err := host.Invoke(context.Background(), principal, extID, "transform", json.RawMessage(`{}`))
	if err != nil {
		t.Fatalf("Invoke failed: %v", err)
	}

	if string(res) != "denied" {
		t.Errorf("expected filesystem access to be denied, but stdout got: %s", string(res))
	}
}

func stringPtr(s string) *string {
	return &s
}
