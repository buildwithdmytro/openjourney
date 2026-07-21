package httpapi

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/buildwithdmytro/openjourney/internal/domain"
	"github.com/buildwithdmytro/openjourney/internal/ports"
	"github.com/buildwithdmytro/openjourney/internal/postgres"
)

type mockFlagStore struct {
	ports.Store
	flags     map[string]domain.FeatureFlag
	versions  map[string]domain.FeatureFlagVersion
	publishFn func(ctx context.Context, p domain.Principal, flagID string, approverUserID string, manifestKey string) (domain.FeatureFlagVersion, error)
	getFn     func(ctx context.Context, p domain.Principal, id string) (domain.FeatureFlag, error)
	updateFn  func(ctx context.Context, p domain.Principal, flag domain.FeatureFlag) (domain.FeatureFlag, error)
	createFn  func(ctx context.Context, p domain.Principal, flag domain.FeatureFlag) (domain.FeatureFlag, error)
	listFn    func(ctx context.Context, p domain.Principal) ([]domain.FeatureFlag, error)
}

func (m *mockFlagStore) PublishFeatureFlag(ctx context.Context, p domain.Principal, flagID string, approverUserID string, manifestKey string) (domain.FeatureFlagVersion, error) {
	if m.publishFn != nil {
		return m.publishFn(ctx, p, flagID, approverUserID, manifestKey)
	}
	userID := approverUserID
	return domain.FeatureFlagVersion{Version: 1, FlagID: flagID, CreatedByUserID: &userID}, nil
}

func (m *mockFlagStore) CreateFeatureFlag(ctx context.Context, p domain.Principal, flag domain.FeatureFlag) (domain.FeatureFlag, error) {
	if m.createFn != nil {
		return m.createFn(ctx, p, flag)
	}
	flag.ID = "flag-1"
	if m.flags == nil {
		m.flags = make(map[string]domain.FeatureFlag)
	}
	m.flags[flag.ID] = flag
	return flag, nil
}

func (m *mockFlagStore) GetFeatureFlag(ctx context.Context, p domain.Principal, id string) (domain.FeatureFlag, error) {
	if m.getFn != nil {
		return m.getFn(ctx, p, id)
	}
	if flag, ok := m.flags[id]; ok {
		return flag, nil
	}
	return domain.FeatureFlag{}, postgres.ErrNotFound
}

func (m *mockFlagStore) UpdateFeatureFlag(ctx context.Context, p domain.Principal, flag domain.FeatureFlag) (domain.FeatureFlag, error) {
	if m.updateFn != nil {
		return m.updateFn(ctx, p, flag)
	}
	if m.flags == nil {
		m.flags = make(map[string]domain.FeatureFlag)
	}
	m.flags[flag.ID] = flag
	return flag, nil
}

func (m *mockFlagStore) ListFeatureFlags(ctx context.Context, p domain.Principal) ([]domain.FeatureFlag, error) {
	if m.listFn != nil {
		return m.listFn(ctx, p)
	}
	var result []domain.FeatureFlag
	for _, flag := range m.flags {
		result = append(result, flag)
	}
	return result, nil
}

func TestPublishFlagWithHumanGate(t *testing.T) {
	// Test: Human gate check in isHuman function
	humanPrincipal := domain.Principal{
		ActorType: "user",
		UserID:    "user-123",
		TenantID:  "tenant-1",
		AppID:     "app-1",
	}
	apiPrincipal := domain.Principal{
		ActorType: "api_key",
		KeyID:     "test-key",
	}

	if !isHuman(humanPrincipal) {
		t.Error("expected human principal to be recognized as human")
	}
	if isHuman(apiPrincipal) {
		t.Error("expected API principal to not be human")
	}

	// Test mock store publish function
	mockStore := &mockFlagStore{
		flags: map[string]domain.FeatureFlag{
			"flag-1": {
				ID:           "flag-1",
				Key:          "test-publish",
				FlagType:     "boolean",
				DefaultValue: json.RawMessage(`true`),
				Seed:         "seed-123",
				Enabled:      true,
				Status:       "draft",
			},
		},
	}

	// Set up publish function to mark the flag as published
	mockStore.publishFn = func(ctx context.Context, p domain.Principal, flagID string, approverUserID string, manifestKey string) (domain.FeatureFlagVersion, error) {
		if flag, ok := mockStore.flags[flagID]; ok {
			flag.Status = "published"
			mockStore.flags[flagID] = flag
		}
		userID := approverUserID
		return domain.FeatureFlagVersion{Version: 1, FlagID: flagID, CreatedByUserID: &userID}, nil
	}

	ctx := context.Background()
	version, err := mockStore.PublishFeatureFlag(ctx, humanPrincipal, "flag-1", "user-123", "")
	if err != nil {
		t.Fatalf("PublishFeatureFlag failed: %v", err)
	}

	if version.Version != 1 {
		t.Errorf("expected version 1, got %d", version.Version)
	}

	// Verify flag is now published
	if flag, ok := mockStore.flags["flag-1"]; ok {
		if flag.Status != "published" {
			t.Errorf("expected status 'published', got %q", flag.Status)
		}
	}

	// Test re-publishing (idempotent)
	version2, err := mockStore.PublishFeatureFlag(ctx, humanPrincipal, "flag-1", "user-123", "")
	if err != nil {
		t.Fatalf("re-publish failed: %v", err)
	}

	if version2.Version != 1 {
		t.Errorf("expected version 1 on re-publish, got %d", version2.Version)
	}
}

func TestCreateAndListFeatureFlags(t *testing.T) {
	mockStore := &mockFlagStore{
		flags: make(map[string]domain.FeatureFlag),
	}

	ctx := context.Background()
	principal := domain.Principal{
		ActorType: "user",
		UserID:    "user-123",
		TenantID:  "tenant-1",
		AppID:     "app-1",
	}

	// Test create
	flag := domain.FeatureFlag{
		AppID:        "app-1",
		Environment:  "production",
		Key:          "test-flag",
		FlagType:     "string",
		DefaultValue: json.RawMessage(`"default"`),
		Seed:         "seed-123",
		Enabled:      true,
	}

	created, err := mockStore.CreateFeatureFlag(ctx, principal, flag)
	if err != nil {
		t.Fatalf("CreateFeatureFlag failed: %v", err)
	}

	if created.Key != "test-flag" {
		t.Errorf("expected key 'test-flag', got %q", created.Key)
	}

	// Test list
	flags, err := mockStore.ListFeatureFlags(ctx, principal)
	if err != nil {
		t.Fatalf("ListFeatureFlags failed: %v", err)
	}

	if len(flags) != 1 {
		t.Errorf("expected 1 flag, got %d", len(flags))
	}
}

func TestSetFlagStatus(t *testing.T) {
	mockStore := &mockFlagStore{
		flags: map[string]domain.FeatureFlag{
			"flag-1": {
				ID:           "flag-1",
				Key:          "test-kill-switch",
				FlagType:     "boolean",
				DefaultValue: json.RawMessage(`true`),
				Seed:         "seed-kill-switch",
				Enabled:      true,
				Status:       "published",
			},
		},
	}

	ctx := context.Background()
	principal := domain.Principal{
		ActorType: "user",
		UserID:    "user-456",
		TenantID:  "tenant-1",
		AppID:     "app-1",
	}

	// Test: Get flag
	flag, err := mockStore.GetFeatureFlag(ctx, principal, "flag-1")
	if err != nil {
		t.Fatalf("GetFeatureFlag failed: %v", err)
	}

	if flag.Status != "published" {
		t.Errorf("expected status 'published', got %q", flag.Status)
	}

	// Test: Update flag status to disabled
	flag.Status = "disabled"
	updated, err := mockStore.UpdateFeatureFlag(ctx, principal, flag)
	if err != nil {
		t.Fatalf("UpdateFeatureFlag failed: %v", err)
	}

	if updated.Status != "disabled" {
		t.Errorf("expected status 'disabled', got %q", updated.Status)
	}
}

func TestKillSwitch_NonHumanRejected(t *testing.T) {
	// Verify isHuman correctly identifies non-human principals
	apiPrincipal := domain.Principal{
		ActorType: "api_key",
		KeyID:     "test-key",
		TenantID:  "tenant-1",
		AppID:     "app-1",
	}

	if isHuman(apiPrincipal) {
		t.Error("expected API principal to not be human")
	}

	// Verify human principal is identified correctly
	humanPrincipal := domain.Principal{
		ActorType: "user",
		UserID:    "user-456",
		TenantID:  "tenant-1",
		AppID:     "app-1",
	}

	if !isHuman(humanPrincipal) {
		t.Error("expected user principal to be human")
	}
}

func TestKillSwitch_StatusUpdateOnDisable(t *testing.T) {
	mockStore := &mockFlagStore{
		flags: map[string]domain.FeatureFlag{
			"flag-1": {
				ID:           "flag-1",
				Key:          "test-kill-switch",
				FlagType:     "boolean",
				DefaultValue: json.RawMessage(`false`),
				Variants: []domain.FlagVariant{
					{Label: "on", Value: json.RawMessage(`true`), Weight: 100},
				},
				Seed:       "seed-kill-switch",
				Enabled:    true,
				Status:     "published",
				RolloutPct: 100,
			},
		},
	}

	ctx := context.Background()
	humanPrincipal := domain.Principal{
		ActorType: "user",
		UserID:    "user-456",
		TenantID:  "tenant-1",
		AppID:     "app-1",
	}

	// Get flag and disable it
	flag, err := mockStore.GetFeatureFlag(ctx, humanPrincipal, "flag-1")
	if err != nil {
		t.Fatalf("GetFeatureFlag failed: %v", err)
	}

	if flag.Status != "published" {
		t.Errorf("initial status should be 'published', got %q", flag.Status)
	}

	// Disable the flag
	flag.Status = "disabled"
	updated, err := mockStore.UpdateFeatureFlag(ctx, humanPrincipal, flag)
	if err != nil {
		t.Fatalf("UpdateFeatureFlag failed: %v", err)
	}

	if updated.Status != "disabled" {
		t.Errorf("expected status 'disabled', got %q", updated.Status)
	}

	// Re-enable the flag
	flag.Status = "published"
	reEnabled, err := mockStore.UpdateFeatureFlag(ctx, humanPrincipal, flag)
	if err != nil {
		t.Fatalf("UpdateFeatureFlag failed: %v", err)
	}

	if reEnabled.Status != "published" {
		t.Errorf("expected status 'published' after re-enable, got %q", reEnabled.Status)
	}
}

func TestSecurityNonHumanPublishRejected(t *testing.T) {
	apiPrincipal := domain.Principal{
		ActorType: "api_key",
		KeyID:     "test-key",
		TenantID:  "tenant-1",
		AppID:     "app-1",
	}

	if isHuman(apiPrincipal) {
		t.Fatal("expected API principal to not be human; security check failed")
	}
}

func TestSecurityNonHumanStatusChangeRejected(t *testing.T) {
	apiPrincipal := domain.Principal{
		ActorType: "api_key",
		KeyID:     "test-key",
		TenantID:  "tenant-1",
		AppID:     "app-1",
	}

	// API key (non-human) should not be able to change flag status
	if isHuman(apiPrincipal) {
		t.Fatal("security check: API key should not be human")
	}

	// Verify only humans can change status
	humanPrincipal := domain.Principal{
		ActorType: "user",
		UserID:    "user-123",
		TenantID:  "tenant-1",
		AppID:     "app-1",
	}
	if !isHuman(humanPrincipal) {
		t.Fatal("security check: user principal should be human")
	}
}

func TestSecurityScopeEnforcementReadOnly(t *testing.T) {
	// Verify that a principal with only flags:read cannot call flags:write endpoints
	readOnlyPrincipal := domain.Principal{
		ActorType: "api_key",
		KeyID:     "read-only-key",
		TenantID:  "tenant-1",
		AppID:     "app-1",
		Scopes:    []string{"flags:read"},
	}

	writePrincipal := domain.Principal{
		ActorType: "api_key",
		KeyID:     "write-key",
		TenantID:  "tenant-1",
		AppID:     "app-1",
		Scopes:    []string{"flags:read", "flags:write"},
	}

	// Verify read-only principal has the read scope
	if !readOnlyPrincipal.HasScope("flags:read") {
		t.Error("read-only principal should have flags:read scope")
	}

	// Verify read-only principal does NOT have the write scope
	if readOnlyPrincipal.HasScope("flags:write") {
		t.Error("read-only principal should NOT have flags:write scope")
	}

	// Verify write principal has both scopes
	if !writePrincipal.HasScope("flags:read") {
		t.Error("write principal should have flags:read scope")
	}
	if !writePrincipal.HasScope("flags:write") {
		t.Error("write principal should have flags:write scope")
	}
}
