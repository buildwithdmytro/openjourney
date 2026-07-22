package httpapi

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/buildwithdmytro/openjourney/internal/domain"
	"github.com/buildwithdmytro/openjourney/internal/ports"
	"github.com/buildwithdmytro/openjourney/internal/postgres"
)

type scimTestStore struct {
	ports.Store
	principal     domain.Principal
	called        bool
	updatedActive bool
}

func (s *scimTestStore) AuthenticateSCIM(_ context.Context, token string) (domain.Principal, error) {
	s.called = true
	if token != "valid" {
		return domain.Principal{}, postgres.ErrUnauthorized
	}
	return s.principal, nil
}
func (s *scimTestStore) ListSCIMUsers(context.Context, string) ([]domain.User, error) {
	return nil, nil
}
func (s *scimTestStore) GetSCIMUser(context.Context, string, string) (domain.User, error) {
	return domain.User{ID: "u-1", OIDCSubject: "alice", Email: "alice@example.test"}, nil
}
func (s *scimTestStore) CreateSCIMUser(context.Context, domain.Principal, domain.User, bool) (domain.User, error) {
	return domain.User{ID: "u-1", OIDCSubject: "alice"}, nil
}
func (s *scimTestStore) UpdateSCIMUser(_ context.Context, _ domain.Principal, id string, _ domain.User, active bool) (domain.User, error) {
	s.updatedActive = active
	return domain.User{ID: id, OIDCSubject: "alice"}, nil
}

func TestSCIMBearerIsDedicatedAndInvalidTokensAre401(t *testing.T) {
	store := &scimTestStore{principal: domain.Principal{TenantID: "tenant-1", ActorType: "scim"}}
	s := &Server{store: store}
	h := s.scimAuthenticate(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := principalFrom(r).TenantID; got != "tenant-1" {
			t.Errorf("tenant=%q", got)
		}
		w.WriteHeader(http.StatusNoContent)
	}))
	for _, header := range []string{"", "Bearer wrong"} {
		req := httptest.NewRequest(http.MethodGet, "/v1/scim/v2/Users", nil)
		req.Header.Set("Authorization", header)
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		if rec.Code != http.StatusUnauthorized {
			t.Errorf("header %q: status=%d", header, rec.Code)
		}
	}
	req := httptest.NewRequest(http.MethodGet, "/v1/scim/v2/Users", nil)
	req.Header.Set("Authorization", "Bearer valid")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusNoContent || !store.called {
		t.Fatalf("valid bearer was not accepted: status=%d called=%v", rec.Code, store.called)
	}
}

func TestSCIMDeleteDeprovisionsUser(t *testing.T) {
	store := &scimTestStore{principal: domain.Principal{TenantID: "tenant-1", ActorType: "scim"}}
	s := &Server{store: store}
	req := httptest.NewRequest(http.MethodDelete, "/v1/scim/v2/Users/u-1", nil)
	req = withPrincipal(req, store.principal)
	req.SetPathValue("id", "u-1")
	rec := httptest.NewRecorder()
	s.deleteSCIMUser(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d", rec.Code)
	}
	if store.updatedActive {
		t.Fatal("deprovision did not deactivate user")
	}
}
