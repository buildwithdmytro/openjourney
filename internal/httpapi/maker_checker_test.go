package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/buildwithdmytro/openjourney/internal/domain"
	"github.com/buildwithdmytro/openjourney/internal/postgres"
	"github.com/buildwithdmytro/openjourney/internal/publishing"
)

type mcFakeStore struct {
	fakeStore
	policyRequireChecker map[string]bool
}

func (m *mcFakeStore) Authenticate(_ context.Context, key string) (domain.Principal, error) {
	switch key {
	case "user1-key":
		return domain.Principal{
			TenantID:  "t1",
			UserID:    "creator-1",
			ActorType: "user",
			Scopes:    []string{"*"},
		}, nil
	case "user2-key":
		return domain.Principal{
			TenantID:  "t1",
			UserID:    "approver-2",
			ActorType: "user",
			Scopes:    []string{"*"},
		}, nil
	case "apikey-key":
		return domain.Principal{
			TenantID:  "t1",
			KeyID:     "k1",
			ActorType: "api_key",
			Scopes:    []string{"*"},
		}, nil
	default:
		return domain.Principal{}, errors.New("unauthorized")
	}
}

func (m *mcFakeStore) GetMakerCheckerPolicy(ctx context.Context, p domain.Principal, resourceType string) (bool, error) {
	if m.policyRequireChecker == nil {
		return false, nil
	}
	return m.policyRequireChecker[resourceType], nil
}

func (m *mcFakeStore) SetMakerCheckerPolicy(ctx context.Context, p domain.Principal, resourceType string, requireChecker bool) (domain.MakerCheckerPolicy, error) {
	if m.policyRequireChecker == nil {
		m.policyRequireChecker = make(map[string]bool)
	}
	m.policyRequireChecker[resourceType] = requireChecker
	return domain.MakerCheckerPolicy{
		ID:             "mc-1",
		TenantID:       p.TenantID,
		ResourceType:   resourceType,
		RequireChecker: requireChecker,
	}, nil
}

func (m *mcFakeStore) ListMakerCheckerPolicies(ctx context.Context, p domain.Principal) ([]domain.MakerCheckerPolicy, error) {
	var list []domain.MakerCheckerPolicy
	for k, v := range m.policyRequireChecker {
		list = append(list, domain.MakerCheckerPolicy{
			ID:             "mc-1",
			TenantID:       p.TenantID,
			ResourceType:   k,
			RequireChecker: v,
		})
	}
	return list, nil
}

func (m *mcFakeStore) CheckMakerChecker(ctx context.Context, p domain.Principal, resourceType, resourceID string, creatorOrEditorID string) error {
	if p.ActorType != "user" || p.UserID == "" {
		return publishing.ErrHumanActorRequired
	}
	if m.policyRequireChecker[resourceType] {
		if creatorOrEditorID == "" || creatorOrEditorID == p.UserID {
			return postgres.ErrSelfApproval
		}
	}
	return nil
}

func (m *mcFakeStore) PublishFeatureFlag(ctx context.Context, p domain.Principal, flagID string, approverUserID string, manifestKey string) (domain.FeatureFlagVersion, error) {
	if err := m.CheckMakerChecker(ctx, p, "flags", flagID, "creator-1"); err != nil {
		return domain.FeatureFlagVersion{}, err
	}
	return domain.FeatureFlagVersion{ID: "v1", Version: 1}, nil
}

func TestMakerCheckerHTTPEndpoints(t *testing.T) {
	store := &mcFakeStore{policyRequireChecker: make(map[string]bool)}
	handler := New(store, 100)

	// Set policy PUT
	body, _ := json.Marshal(map[string]any{"require_checker": true})
	reqPut := httptest.NewRequest("PUT", "/v1/maker-checker/policies/flags", bytes.NewReader(body))
	reqPut.Header.Set("Authorization", "Bearer user1-key")
	rrPut := httptest.NewRecorder()
	handler.ServeHTTP(rrPut, reqPut)
	if rrPut.Code != http.StatusOK {
		t.Fatalf("PUT maker-checker policy failed: %d %s", rrPut.Code, rrPut.Body.String())
	}

	// Non-human publish -> 403 human_approval_required
	reqAPIKey := httptest.NewRequest("POST", "/v1/flags/flag-1/publish", nil)
	reqAPIKey.Header.Set("Authorization", "Bearer apikey-key")
	rrAPIKey := httptest.NewRecorder()
	handler.ServeHTTP(rrAPIKey, reqAPIKey)
	if rrAPIKey.Code != http.StatusForbidden {
		t.Fatalf("expected 403 for API key publish, got %d: %s", rrAPIKey.Code, rrAPIKey.Body.String())
	}

	// Creator self-approval -> 403 self_approval_forbidden
	reqSelf := httptest.NewRequest("POST", "/v1/flags/flag-1/publish", nil)
	reqSelf.Header.Set("Authorization", "Bearer user1-key")
	rrSelf := httptest.NewRecorder()
	handler.ServeHTTP(rrSelf, reqSelf)
	if rrSelf.Code != http.StatusForbidden {
		t.Fatalf("expected 403 for self approval, got %d: %s", rrSelf.Code, rrSelf.Body.String())
	}
	var errResp map[string]map[string]string
	_ = json.NewDecoder(rrSelf.Body).Decode(&errResp)
	if errResp["error"]["code"] != "self_approval_forbidden" {
		t.Fatalf("expected error code self_approval_forbidden, got %s", errResp["error"]["code"])
	}

	// Distinct user publish -> 200 OK
	reqOther := httptest.NewRequest("POST", "/v1/flags/flag-1/publish", nil)
	reqOther.Header.Set("Authorization", "Bearer user2-key")
	rrOther := httptest.NewRecorder()
	handler.ServeHTTP(rrOther, reqOther)
	if rrOther.Code != http.StatusOK {
		t.Fatalf("expected 200 OK for distinct user, got %d: %s", rrOther.Code, rrOther.Body.String())
	}
}
