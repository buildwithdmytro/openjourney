package extension

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/buildwithdmytro/openjourney/internal/domain"
	"github.com/buildwithdmytro/openjourney/internal/ports"
)

type mockStore struct {
	ports.Store
	extensions      map[string]domain.Extension
	versions        map[string]domain.ExtensionVersion
	configs         map[string]domain.ExtensionConfig
	health          map[string]domain.ExtensionHealth
	grants          map[string][]domain.ExtensionGrant
	activities      []domain.ExtensionActivity
	invocationCount int
	budgetUsage     int64
}

func newMockStore() *mockStore {
	return &mockStore{
		extensions: make(map[string]domain.Extension),
		versions:   make(map[string]domain.ExtensionVersion),
		configs:    make(map[string]domain.ExtensionConfig),
		health:     make(map[string]domain.ExtensionHealth),
		grants:     make(map[string][]domain.ExtensionGrant),
	}
}

func (m *mockStore) GetExtension(ctx context.Context, p domain.Principal, id string) (domain.Extension, error) {
	ext, ok := m.extensions[id]
	if !ok {
		return domain.Extension{}, errors.New("not found")
	}
	return ext, nil
}

func (m *mockStore) GetExtensionVersion(ctx context.Context, p domain.Principal, id string) (domain.ExtensionVersion, error) {
	for _, ev := range m.versions {
		if ev.ID == id {
			return ev, nil
		}
	}
	return domain.ExtensionVersion{}, errors.New("not found")
}

func (m *mockStore) GetExtensionVersionByNumber(ctx context.Context, p domain.Principal, extensionID string, version int) (domain.ExtensionVersion, error) {
	for _, ev := range m.versions {
		if ev.ExtensionID == extensionID && ev.Version == version {
			return ev, nil
		}
	}
	return domain.ExtensionVersion{}, errors.New("not found")
}


func (m *mockStore) GetExtensionConfig(ctx context.Context, p domain.Principal, extensionID string) (domain.ExtensionConfig, error) {
	cfg, ok := m.configs[extensionID]
	if !ok {
		return domain.ExtensionConfig{}, errors.New("not found")
	}
	return cfg, nil
}

func (m *mockStore) GetExtensionHealth(ctx context.Context, p domain.Principal, extensionID string) (domain.ExtensionHealth, error) {
	h, ok := m.health[extensionID]
	if !ok {
		return domain.ExtensionHealth{
			ExtensionID:         extensionID,
			TenantID:            p.TenantID,
			State:               "closed",
			ConsecutiveFailures: 0,
		}, nil
	}
	return h, nil
}

func (m *mockStore) UpdateExtensionHealth(ctx context.Context, p domain.Principal, health domain.ExtensionHealth) (domain.ExtensionHealth, error) {
	m.health[health.ExtensionID] = health
	return health, nil
}

func (m *mockStore) ListExtensionGrants(ctx context.Context, p domain.Principal, extensionID string) ([]domain.ExtensionGrant, error) {
	return m.grants[extensionID], nil
}

func (m *mockStore) RecordExtensionActivity(ctx context.Context, p domain.Principal, act domain.ExtensionActivity) (domain.ExtensionActivity, error) {
	act.ID = fmt.Sprintf("activity-%d", len(m.activities)+1)
	act.CreatedAt = time.Now()
	m.activities = append(m.activities, act)
	return act, nil
}

func (m *mockStore) GetExtensionInvocationCountLastMin(ctx context.Context, tenantID, workspaceID, extensionID string) (int, error) {
	return m.invocationCount, nil
}

func (m *mockStore) GetExtensionBudgetUsage(ctx context.Context, tenantID, workspaceID, extensionID, period string) (int64, error) {
	return m.budgetUsage, nil
}

type mockRoundTripper struct {
	RoundTripFunc func(req *http.Request) (*http.Response, error)
}

func (m *mockRoundTripper) RoundTrip(req *http.Request) (*http.Response, error) {
	return m.RoundTripFunc(req)
}

func TestHostInvoke_Success(t *testing.T) {
	store := newMockStore()
	host := NewHost(store)

	var capturedHeader string
	var capturedBody string

	host.httpClient.Transport = &mockRoundTripper{
		RoundTripFunc: func(req *http.Request) (*http.Response, error) {
			capturedHeader = req.Header.Get("X-Signature")
			bodyBytes, _ := io.ReadAll(req.Body)
			capturedBody = string(bodyBytes)

			return &http.Response{
				StatusCode: 200,
				Body:       io.NopCloser(bytes.NewBufferString(`{"success":true}`)),
			}, nil
		},
	}

	extID := "ext-1"
	versionID := "ver-1"
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
		Kind:            "channel_provider",
		Transport:       "remote_http",
		RequestedScopes: []string{"profiles:read"},
	}

	store.configs[extID] = domain.ExtensionConfig{
		ExtensionID:       extID,
		Status:            "active",
		Config:            json.RawMessage(`{"base_url": "http://example.com/api", "hmac_secret_ref": "TEST_HMAC_SECRET"}`),
		EndpointAllowlist: []string{"http://example.com/api"},
		TimeoutMs:         1000,
	}

	store.grants[extID] = []domain.ExtensionGrant{
		{ExtensionID: extID, Scope: "profiles:read"},
	}

	t.Setenv("TEST_HMAC_SECRET", "supersecret")

	principal := domain.Principal{
		TenantID:    "tenant-1",
		WorkspaceID: "workspace-1",
		Scopes:      []string{"profiles:read"},
	}

	res, err := host.Invoke(context.Background(), principal, extID, "send", json.RawMessage(`{"hello":"world"}`))
	if err != nil {
		t.Fatalf("Invoke failed: %v", err)
	}

	var parsed map[string]bool
	if err := json.Unmarshal(res, &parsed); err != nil {
		t.Fatal(err)
	}
	if !parsed["success"] {
		t.Error("expected success = true")
	}

	if capturedBody != `{"hello":"world"}` {
		t.Errorf("expected body %q, got %q", `{"hello":"world"}`, capturedBody)
	}

	// Calculate expected signature
	mac := hmac.New(sha256.New, []byte("supersecret"))
	mac.Write([]byte(`{"hello":"world"}`))
	expectedSig := hex.EncodeToString(mac.Sum(nil))
	if capturedHeader != expectedSig {
		t.Errorf("expected X-Signature %s, got %s", expectedSig, capturedHeader)
	}

	if len(store.activities) != 1 {
		t.Errorf("expected 1 activity row, got %d", len(store.activities))
	}
	if store.activities[0].PolicyDecision != "allowed" {
		t.Errorf("expected decision = allowed, got %s", store.activities[0].PolicyDecision)
	}
}

func TestHostInvoke_OffAllowlist(t *testing.T) {
	store := newMockStore()
	host := NewHost(store)

	extID := "ext-1"
	versionID := "ver-1"
	active := "enabled"
	store.extensions[extID] = domain.Extension{
		ID:               extID,
		Status:           active,
		CurrentVersionID: &versionID,
	}

	store.versions[versionID] = domain.ExtensionVersion{
		ID:          versionID,
		ExtensionID: extID,
		Version:     1,
		Kind:        "channel_provider",
		Transport:   "remote_http",
	}

	store.configs[extID] = domain.ExtensionConfig{
		ExtensionID:       extID,
		Status:            "active",
		Config:            json.RawMessage(`{"base_url": "http://evil.com/api"}`),
		EndpointAllowlist: []string{"http://good.com"},
		TimeoutMs:         1000,
	}

	principal := domain.Principal{TenantID: "tenant-1", WorkspaceID: "workspace-1"}
	_, err := host.Invoke(context.Background(), principal, extID, "send", json.RawMessage(`{}`))
	if err == nil {
		t.Fatal("expected error due to off-allowlist")
	}

	if !strings.Contains(err.Error(), "allowlist") {
		t.Errorf("expected allowlist error, got %v", err)
	}
}

func TestHostInvoke_Timeout(t *testing.T) {
	store := newMockStore()
	host := NewHost(store)

	host.httpClient.Transport = &mockRoundTripper{
		RoundTripFunc: func(req *http.Request) (*http.Response, error) {
			select {
			case <-req.Context().Done():
				return nil, req.Context().Err()
			case <-time.After(100 * time.Millisecond):
				return &http.Response{StatusCode: 200, Body: io.NopCloser(bytes.NewBufferString(`{}`))}, nil
			}
		},
	}

	extID := "ext-1"
	versionID := "ver-1"
	active := "enabled"
	store.extensions[extID] = domain.Extension{
		ID:               extID,
		Status:           active,
		CurrentVersionID: &versionID,
	}

	store.versions[versionID] = domain.ExtensionVersion{
		ID:          versionID,
		ExtensionID: extID,
		Version:     1,
		Kind:        "channel_provider",
		Transport:   "remote_http",
	}

	store.configs[extID] = domain.ExtensionConfig{
		ExtensionID:       extID,
		Status:            "active",
		Config:            json.RawMessage(`{"base_url": "http://example.com/api"}`),
		EndpointAllowlist: []string{"http://example.com/api"},
		TimeoutMs:         10, // low timeout
	}

	principal := domain.Principal{TenantID: "tenant-1", WorkspaceID: "workspace-1"}
	_, err := host.Invoke(context.Background(), principal, extID, "send", json.RawMessage(`{}`))
	if err == nil {
		t.Fatal("expected timeout error")
	}

	if len(store.activities) != 1 {
		t.Fatalf("expected 1 activity row, got %d", len(store.activities))
	}
	if store.activities[0].PolicyDecision != "timeout" {
		t.Errorf("expected decision = timeout, got %s", store.activities[0].PolicyDecision)
	}
}

func TestHostInvoke_CircuitBreaker(t *testing.T) {
	store := newMockStore()
	host := NewHost(store)

	host.httpClient.Transport = &mockRoundTripper{
		RoundTripFunc: func(req *http.Request) (*http.Response, error) {
			return nil, errors.New("network error")
		},
	}

	extID := "ext-1"
	versionID := "ver-1"
	active := "enabled"
	store.extensions[extID] = domain.Extension{
		ID:               extID,
		Status:           active,
		CurrentVersionID: &versionID,
	}

	store.versions[versionID] = domain.ExtensionVersion{
		ID:          versionID,
		ExtensionID: extID,
		Version:     1,
		Kind:        "channel_provider",
		Transport:   "remote_http",
	}

	store.configs[extID] = domain.ExtensionConfig{
		ExtensionID:       extID,
		Status:            "active",
		Config:            json.RawMessage(`{"base_url": "http://example.com/api"}`),
		EndpointAllowlist: []string{"http://example.com/api"},
		TimeoutMs:         1000,
	}

	principal := domain.Principal{TenantID: "tenant-1", WorkspaceID: "workspace-1"}

	// Trigger 5 consecutive failures
	for i := 0; i < 5; i++ {
		_, err := host.Invoke(context.Background(), principal, extID, "send", json.RawMessage(`{}`))
		if err == nil {
			t.Fatal("expected failure")
		}
	}

	health, _ := store.GetExtensionHealth(context.Background(), principal, extID)
	if health.State != "open" {
		t.Errorf("expected state = open, got %s", health.State)
	}

	// 6th call should immediately short circuit with ErrCircuitOpen
	store.activities = nil
	_, err := host.Invoke(context.Background(), principal, extID, "send", json.RawMessage(`{}`))
	if !errors.Is(err, ErrCircuitOpen) {
		t.Errorf("expected ErrCircuitOpen, got %v", err)
	}

	if len(store.activities) != 1 {
		t.Fatalf("expected 1 activity row, got %d", len(store.activities))
	}
	if store.activities[0].PolicyDecision != "circuit_open" {
		t.Errorf("expected decision = circuit_open, got %s", store.activities[0].PolicyDecision)
	}
}

func TestHostInvoke_RateLimit(t *testing.T) {
	store := newMockStore()
	host := NewHost(store)

	extID := "ext-1"
	versionID := "ver-1"
	active := "enabled"
	store.extensions[extID] = domain.Extension{
		ID:               extID,
		Status:           active,
		CurrentVersionID: &versionID,
	}

	store.versions[versionID] = domain.ExtensionVersion{
		ID:          versionID,
		ExtensionID: extID,
		Version:     1,
		Kind:        "channel_provider",
		Transport:   "remote_http",
	}

	store.configs[extID] = domain.ExtensionConfig{
		ExtensionID:       extID,
		Status:            "active",
		Config:            json.RawMessage(`{"base_url": "http://example.com"}`),
		EndpointAllowlist: []string{"http://example.com"},
		RatePerMin:        10,
	}

	store.invocationCount = 10 // rate limit reached

	principal := domain.Principal{TenantID: "tenant-1", WorkspaceID: "workspace-1"}
	_, err := host.Invoke(context.Background(), principal, extID, "send", json.RawMessage(`{}`))
	if !errors.Is(err, ErrRateLimitExceeded) {
		t.Errorf("expected ErrRateLimitExceeded, got %v", err)
	}

	if len(store.activities) != 1 {
		t.Fatalf("expected 1 activity row, got %d", len(store.activities))
	}
	if store.activities[0].PolicyDecision != "denied_rate" {
		t.Errorf("expected decision = denied_rate, got %s", store.activities[0].PolicyDecision)
	}
}

func TestHostInvoke_BudgetLimit(t *testing.T) {
	store := newMockStore()
	host := NewHost(store)

	extID := "ext-1"
	versionID := "ver-1"
	active := "enabled"
	store.extensions[extID] = domain.Extension{
		ID:               extID,
		Status:           active,
		CurrentVersionID: &versionID,
	}

	store.versions[versionID] = domain.ExtensionVersion{
		ID:          versionID,
		ExtensionID: extID,
		Version:     1,
		Kind:        "channel_provider",
		Transport:   "remote_http",
	}

	store.configs[extID] = domain.ExtensionConfig{
		ExtensionID:        extID,
		Status:             "active",
		Config:             json.RawMessage(`{"base_url": "http://example.com"}`),
		EndpointAllowlist:  []string{"http://example.com"},
		MonthlyBudgetCents: 100,
	}

	store.budgetUsage = 100 // budget reached

	principal := domain.Principal{TenantID: "tenant-1", WorkspaceID: "workspace-1"}
	_, err := host.Invoke(context.Background(), principal, extID, "send", json.RawMessage(`{}`))
	if !errors.Is(err, ErrBudgetExceeded) {
		t.Errorf("expected ErrBudgetExceeded, got %v", err)
	}

	if len(store.activities) != 1 {
		t.Fatalf("expected 1 activity row, got %d", len(store.activities))
	}
	if store.activities[0].PolicyDecision != "denied_budget" {
		t.Errorf("expected decision = denied_budget, got %s", store.activities[0].PolicyDecision)
	}
}
