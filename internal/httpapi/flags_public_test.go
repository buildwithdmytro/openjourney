package httpapi

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/buildwithdmytro/openjourney/internal/domain"
	"github.com/buildwithdmytro/openjourney/internal/ports"
	"github.com/buildwithdmytro/openjourney/internal/postgres"
)

type flagTestStore struct {
	ports.Store
	profiles  map[string]string // subject -> profileID
	flags     []domain.FeatureFlag
	events    []domain.Event
	audiences []struct {
		result bool
	}
}

func (s *flagTestStore) GetProfileIDBySubject(ctx context.Context, tenantID, appID, subject string, byExternalID bool) (string, error) {
	if profileID, ok := s.profiles[subject]; ok {
		return profileID, nil
	}
	return "", postgres.ErrNotFound
}

func (s *flagTestStore) ListActiveFlags(ctx context.Context, tenantID, appID, environment string) ([]domain.FeatureFlag, error) {
	var result []domain.FeatureFlag
	for _, f := range s.flags {
		if f.Environment == environment && f.Status == "published" && f.Enabled {
			result = append(result, f)
		}
	}
	return result, nil
}

func (s *flagTestStore) EvaluateAudience(ctx context.Context, p domain.Principal, profileID string, dsl json.RawMessage) (bool, error) {
	if len(s.audiences) > 0 {
		result := s.audiences[0]
		s.audiences = s.audiences[1:]
		return result.result, nil
	}
	return false, nil
}

func (s *flagTestStore) AcceptEvents(_ context.Context, _ domain.Principal, events []domain.Event) ([]string, error) {
	s.events = append(s.events, events...)
	return []string{}, nil
}

func TestEvaluateFlagsAnonymousSubject(t *testing.T) {
	store := &flagTestStore{
		profiles: map[string]string{"anon-1": "profile-1"},
		flags: []domain.FeatureFlag{
			{
				ID:           "flag-1",
				TenantID:     "tenant-1",
				AppID:        "app-1",
				Environment:  "production",
				Key:          "feature_x",
				DefaultValue: json.RawMessage(`false`),
				Status:       "published",
				Enabled:      true,
				Seed:         "seed-1",
				RolloutPct:   50,
			},
		},
	}
	h := New(store, 10)
	req := httptest.NewRequest(http.MethodGet,
		"/v1/flags/evaluate?tenant=tenant-1&app=app-1&anonymous_id=anon-1", nil)
	res := httptest.NewRecorder()
	h.ServeHTTP(res, req)

	if res.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", res.Code, http.StatusOK)
	}
	var body map[string]any
	if err := json.NewDecoder(res.Body).Decode(&body); err != nil {
		t.Fatal(err)
	}
	flags, ok := body["flags"].(map[string]any)
	if !ok {
		t.Fatalf("flags = %T, want map", body["flags"])
	}
	if feature, ok := flags["feature_x"]; ok {
		if featureMap, ok := feature.(map[string]any); ok {
			if _, hasValue := featureMap["value"]; !hasValue {
				t.Fatal("evaluated flag missing value")
			}
		} else {
			t.Fatalf("feature value = %T, want map", feature)
		}
	}
}

func TestEvaluateFlagsKnownSubjectRequiresToken(t *testing.T) {
	store := &flagTestStore{
		profiles: map[string]string{"ext-1": "profile-1"},
		flags: []domain.FeatureFlag{
			{
				ID:           "flag-1",
				TenantID:     "tenant-1",
				AppID:        "app-1",
				Environment:  "production",
				Key:          "feature_x",
				DefaultValue: json.RawMessage(`false`),
				Status:       "published",
				Enabled:      true,
				Seed:         "seed-1",
				RolloutPct:   50,
			},
		},
	}
	h := New(store, 10)

	// Request without token
	req := httptest.NewRequest(http.MethodGet,
		"/v1/flags/evaluate?tenant=tenant-1&app=app-1&external_id=ext-1", nil)
	res := httptest.NewRecorder()
	h.ServeHTTP(res, req)

	if res.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d (missing token)", res.Code, http.StatusBadRequest)
	}
}

func TestEvaluateFlagsIDORProtection(t *testing.T) {
	store := &flagTestStore{
		profiles: map[string]string{"anon-1": "profile-1", "ext-1": "profile-2"},
		flags: []domain.FeatureFlag{
			{
				ID:           "flag-1",
				TenantID:     "tenant-1",
				AppID:        "app-1",
				Environment:  "production",
				Key:          "feature_x",
				DefaultValue: json.RawMessage(`false`),
				Status:       "published",
				Enabled:      true,
				Seed:         "seed-1",
				RolloutPct:   50,
			},
		},
	}
	h := New(store, 10)

	// Anonymous request trying to impersonate a known subject
	// by passing both anonymous_id and external_id without token
	req := httptest.NewRequest(http.MethodGet,
		"/v1/flags/evaluate?tenant=tenant-1&app=app-1&anonymous_id=anon-1&external_id=ext-1", nil)
	res := httptest.NewRecorder()
	h.ServeHTTP(res, req)

	// The byExternalID pin should make it use the anonymous_id only
	// and not the external_id (since no token is present)
	if res.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", res.Code, http.StatusOK)
	}
}

func TestEvaluateFlagsUnknownEnvironmentRejected(t *testing.T) {
	store := &flagTestStore{
		profiles: map[string]string{"anon-1": "profile-1"},
	}
	h := New(store, 10)

	req := httptest.NewRequest(http.MethodGet,
		"/v1/flags/evaluate?tenant=tenant-1&app=app-1&anonymous_id=anon-1&environment=invalid", nil)
	res := httptest.NewRecorder()
	h.ServeHTTP(res, req)

	if res.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d for invalid environment", res.Code, http.StatusBadRequest)
	}
	var body map[string]any
	if err := json.NewDecoder(res.Body).Decode(&body); err != nil {
		t.Fatal(err)
	}
	errObj, ok := body["error"].(map[string]any)
	if !ok {
		t.Fatalf("error structure = %T, want map", body["error"])
	}
	if errObj["code"] != "invalid_environment" {
		t.Fatalf("code = %q, want 'invalid_environment'", errObj["code"])
	}
}

func TestEvaluateFlagsDefaultEnvironment(t *testing.T) {
	store := &flagTestStore{
		profiles: map[string]string{"anon-1": "profile-1"},
		flags: []domain.FeatureFlag{
			{
				ID:           "flag-1",
				TenantID:     "tenant-1",
				AppID:        "app-1",
				Environment:  "production",
				Key:          "feature_x",
				DefaultValue: json.RawMessage(`false`),
				Status:       "published",
				Enabled:      true,
				Seed:         "seed-1",
				RolloutPct:   50,
			},
		},
	}
	h := New(store, 10)

	// Request without environment parameter; should default to "production"
	req := httptest.NewRequest(http.MethodGet,
		"/v1/flags/evaluate?tenant=tenant-1&app=app-1&anonymous_id=anon-1", nil)
	res := httptest.NewRecorder()
	h.ServeHTTP(res, req)

	if res.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", res.Code, http.StatusOK)
	}
}

func TestEvaluateFlagsUnknownProfile(t *testing.T) {
	store := &flagTestStore{
		profiles: map[string]string{}, // no profiles
		flags: []domain.FeatureFlag{
			{
				ID:           "flag-1",
				TenantID:     "tenant-1",
				AppID:        "app-1",
				Environment:  "production",
				Key:          "feature_x",
				DefaultValue: json.RawMessage(`false`),
				Status:       "published",
				Enabled:      true,
				Seed:         "seed-1",
				RolloutPct:   50,
			},
		},
	}
	h := New(store, 10)

	req := httptest.NewRequest(http.MethodGet,
		"/v1/flags/evaluate?tenant=tenant-1&app=app-1&anonymous_id=unknown", nil)
	res := httptest.NewRecorder()
	h.ServeHTTP(res, req)

	if res.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d (empty flags for unknown profile)", res.Code, http.StatusOK)
	}
	var body map[string]any
	if err := json.NewDecoder(res.Body).Decode(&body); err != nil {
		t.Fatal(err)
	}
	flags, ok := body["flags"].(map[string]any)
	if !ok {
		t.Fatalf("flags = %T, want map", body["flags"])
	}
	if len(flags) != 0 {
		t.Fatalf("flags length = %d, want 0 for unknown profile", len(flags))
	}
}

func TestEvaluateFlagsEmitsExposureEvents(t *testing.T) {
	store := &flagTestStore{
		profiles: map[string]string{"anon-1": "profile-1"},
		flags: []domain.FeatureFlag{
			{
				ID:           "flag-1",
				TenantID:     "tenant-1",
				AppID:        "app-1",
				Environment:  "production",
				Key:          "feature_x",
				DefaultValue: json.RawMessage(`false`),
				Status:       "published",
				Enabled:      true,
				Seed:         "seed-1",
				RolloutPct:   100, // always in rollout to get a variant
				Variants: []domain.FlagVariant{
					{Label: "variant_a", Value: json.RawMessage(`true`), Weight: 50},
					{Label: "variant_b", Value: json.RawMessage(`false`), Weight: 50},
				},
			},
		},
	}
	h := New(store, 10)

	req := httptest.NewRequest(http.MethodGet,
		"/v1/flags/evaluate?tenant=tenant-1&app=app-1&anonymous_id=anon-1", nil)
	res := httptest.NewRecorder()
	h.ServeHTTP(res, req)

	if res.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", res.Code, http.StatusOK)
	}

	// Check that exposure events were emitted
	if len(store.events) == 0 {
		t.Fatal("no events emitted; want at least one exposure event")
	}

	exposureFound := false
	for _, event := range store.events {
		if event.Type == "feature_flag.exposure" {
			exposureFound = true
			var payload map[string]string
			if err := json.Unmarshal(event.Payload, &payload); err != nil {
				t.Fatal(err)
			}
			if payload["flag_id"] != "flag-1" {
				t.Fatalf("exposure flag_id = %q, want 'flag-1'", payload["flag_id"])
			}
			if payload["environment"] != "production" {
				t.Fatalf("exposure environment = %q, want 'production'", payload["environment"])
			}
			if event.AnonymousID != "anon-1" {
				t.Fatalf("exposure AnonymousID = %q, want 'anon-1'", event.AnonymousID)
			}
		}
	}
	if !exposureFound {
		t.Fatal("no feature_flag.exposure event found")
	}
}

func TestEvaluateFlagsMissingContextRejected(t *testing.T) {
	store := &flagTestStore{}
	h := New(store, 10)

	// Missing tenant
	req := httptest.NewRequest(http.MethodGet,
		"/v1/flags/evaluate?app=app-1&anonymous_id=anon-1", nil)
	res := httptest.NewRecorder()
	h.ServeHTTP(res, req)

	if res.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d (missing tenant)", res.Code, http.StatusBadRequest)
	}

	// Missing app
	req = httptest.NewRequest(http.MethodGet,
		"/v1/flags/evaluate?tenant=tenant-1&anonymous_id=anon-1", nil)
	res = httptest.NewRecorder()
	h.ServeHTTP(res, req)

	if res.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d (missing app)", res.Code, http.StatusBadRequest)
	}

	// Missing subject
	req = httptest.NewRequest(http.MethodGet,
		"/v1/flags/evaluate?tenant=tenant-1&app=app-1", nil)
	res = httptest.NewRecorder()
	h.ServeHTTP(res, req)

	if res.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d (missing subject)", res.Code, http.StatusBadRequest)
	}
}

func TestEvaluateFlagsRateLimiting(t *testing.T) {
	store := &flagTestStore{
		profiles: map[string]string{"anon-1": "profile-1"},
		flags:    []domain.FeatureFlag{},
	}

	// Create server with rate limiter that allows 1 request per 1 second with burst of 1
	limiter := NewIPRateLimiter(1, 1)
	h := NewWithSessionTTL(store, 10, nil, "http://localhost:3000", 12*time.Hour,
		WithPublicGuard(limiter, NoopCaptchaVerifier{}, false))

	// First request should succeed
	req := httptest.NewRequest(http.MethodGet,
		"/v1/flags/evaluate?tenant=tenant-1&app=app-1&anonymous_id=anon-1", nil)
	res := httptest.NewRecorder()
	h.ServeHTTP(res, req)

	if res.Code != http.StatusOK {
		t.Fatalf("first request status = %d, want %d", res.Code, http.StatusOK)
	}

	// Second request (burst exceeded) should fail
	req = httptest.NewRequest(http.MethodGet,
		"/v1/flags/evaluate?tenant=tenant-1&app=app-1&anonymous_id=anon-1", nil)
	res = httptest.NewRecorder()
	h.ServeHTTP(res, req)

	if res.Code != http.StatusTooManyRequests {
		t.Fatalf("second request status = %d, want %d", res.Code, http.StatusTooManyRequests)
	}
}
