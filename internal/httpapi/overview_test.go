package httpapi

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/buildwithdmytro/openjourney/internal/domain"
)

type overviewHTTPStore struct {
	fakeStore
	principals []domain.Principal
}

func (s *overviewHTTPStore) GetOverview(_ context.Context, p domain.Principal) (domain.Overview, error) {
	s.principals = append(s.principals, p)
	return domain.Overview{
		Profiles:         42,
		Journeys:         5,
		Campaigns:        10,
		DeliveryAttempts: 1000,
		InAppMessages:    350,
		ConnectorRuns:    15,
	}, nil
}

func TestGetOverviewReturnsJSONAndPassesScopedPrincipal(t *testing.T) {
	store := &overviewHTTPStore{}
	store.scopes = []string{"reports:read"}
	server := New(store, 75)

	req := httptest.NewRequest(http.MethodGet, "/v1/overview", nil)
	req.Header.Set("Authorization", "Bearer test-key")
	res := httptest.NewRecorder()
	server.ServeHTTP(res, req)

	if res.Code != http.StatusOK {
		t.Fatalf("GET /v1/overview status=%d body=%s", res.Code, res.Body.String())
	}
	if contentType := res.Header().Get("Content-Type"); contentType != "application/json" {
		t.Fatalf("GET /v1/overview content-type=%q", contentType)
	}

	var got domain.Overview
	if err := json.Unmarshal(res.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode overview: %v", err)
	}
	if got.Profiles != 42 || got.Journeys != 5 || got.Campaigns != 10 || got.DeliveryAttempts != 1000 || got.InAppMessages != 350 || got.ConnectorRuns != 15 {
		t.Fatalf("unexpected overview JSON: %+v", got)
	}

	if len(store.principals) != 1 {
		t.Fatalf("overview calls principals=%d", len(store.principals))
	}
	principal := store.principals[0]
	if principal.TenantID != "tenant" || principal.WorkspaceID != "workspace" {
		t.Fatalf("overview received unscoped principal: %+v", principal)
	}
}

func TestGetOverviewRequiresReportsReadScope(t *testing.T) {
	store := &overviewHTTPStore{}
	store.scopes = []string{"campaigns:read", "journeys:read"}
	server := New(store, 75)

	req := httptest.NewRequest(http.MethodGet, "/v1/overview", nil)
	req.Header.Set("Authorization", "Bearer test-key")
	res := httptest.NewRecorder()
	server.ServeHTTP(res, req)

	if res.Code != http.StatusForbidden {
		t.Fatalf("GET /v1/overview status=%d body=%s", res.Code, res.Body.String())
	}
	if len(store.principals) != 0 {
		t.Fatalf("store called without reports:read: %+v", store.principals)
	}
}
