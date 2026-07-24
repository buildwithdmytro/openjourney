package httpapi

import (
	"bytes"
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
	tenants       []string
	createdUser   domain.User
	updatedUser   domain.User
	createdGroup  domain.SCIMGroup
	updatedGroup  domain.SCIMGroup
	patchedGroup  domain.SCIMGroupPatch
}

func (s *scimTestStore) AuthenticateSCIM(_ context.Context, token string) (domain.Principal, error) {
	s.called = true
	if token != "valid" {
		return domain.Principal{}, postgres.ErrUnauthorized
	}
	return s.principal, nil
}
func (s *scimTestStore) ListSCIMUsers(_ context.Context, tenant string) ([]domain.User, error) {
	s.tenants = append(s.tenants, tenant)
	return []domain.User{{ID: "u-1", OIDCSubject: "alice", Email: "alice@example.test"}}, nil
}
func (s *scimTestStore) GetSCIMUser(_ context.Context, tenant, _ string) (domain.User, error) {
	s.tenants = append(s.tenants, tenant)
	return domain.User{ID: "u-1", OIDCSubject: "alice", Email: "alice@example.test"}, nil
}
func (s *scimTestStore) CreateSCIMUser(_ context.Context, p domain.Principal, user domain.User, _ bool) (domain.User, error) {
	s.tenants = append(s.tenants, p.TenantID)
	s.createdUser = user
	return domain.User{ID: "u-1", OIDCSubject: user.OIDCSubject, Email: user.Email}, nil
}
func (s *scimTestStore) UpdateSCIMUser(_ context.Context, p domain.Principal, id string, user domain.User, active bool) (domain.User, error) {
	s.tenants = append(s.tenants, p.TenantID)
	s.updatedActive = active
	s.updatedUser = user
	return domain.User{ID: id, OIDCSubject: user.OIDCSubject, Email: user.Email}, nil
}
func (s *scimTestStore) ListSCIMGroups(_ context.Context, tenant string) ([]domain.SCIMGroup, error) {
	s.tenants = append(s.tenants, tenant)
	return []domain.SCIMGroup{{ID: "g-1", DisplayName: "DevOps", Members: []domain.SCIMGroupMember{{Value: "u-1"}}}}, nil
}
func (s *scimTestStore) GetSCIMGroup(_ context.Context, tenant, _ string) (domain.SCIMGroup, error) {
	s.tenants = append(s.tenants, tenant)
	return domain.SCIMGroup{ID: "g-1", DisplayName: "DevOps"}, nil
}
func (s *scimTestStore) CreateSCIMGroup(_ context.Context, p domain.Principal, group domain.SCIMGroup) (domain.SCIMGroup, error) {
	s.tenants = append(s.tenants, p.TenantID)
	s.createdGroup = group
	return domain.SCIMGroup{ID: "g-1", DisplayName: group.DisplayName, Members: group.Members}, nil
}
func (s *scimTestStore) UpdateSCIMGroup(_ context.Context, p domain.Principal, _ string, group domain.SCIMGroup) (domain.SCIMGroup, error) {
	s.tenants = append(s.tenants, p.TenantID)
	s.updatedGroup = group
	return domain.SCIMGroup{ID: "g-1", DisplayName: group.DisplayName, Members: group.Members}, nil
}
func (s *scimTestStore) PatchSCIMGroup(_ context.Context, p domain.Principal, _ string, patch domain.SCIMGroupPatch) (domain.SCIMGroup, error) {
	s.tenants = append(s.tenants, p.TenantID)
	s.patchedGroup = patch
	return domain.SCIMGroup{ID: "g-1", DisplayName: "DevOps"}, nil
}
func (s *scimTestStore) DeleteSCIMGroup(_ context.Context, p domain.Principal, _ string) error {
	s.tenants = append(s.tenants, p.TenantID)
	return nil
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

func TestSCIMHandlersPropagateTenantAndMapGroupPatch(t *testing.T) {
	store := &scimTestStore{principal: domain.Principal{TenantID: "tenant-1", ActorType: "scim"}}
	s := &Server{store: store}
	userBody := `{"userName":"bob","displayName":"Bob","active":true,"emails":[{"value":"bob@example.test","primary":true}]}`
	groupBody := `{"displayName":"Engineering","members":[{"value":"u-2","display":"Bob","$ref":"/Users/u-2"}]}`
	patchBody := `{"Operations":[{"op":"add","path":"members","value":[{"value":"u-2","display":"Bob","$ref":"/Users/u-2"}]}]}`

	cases := []struct {
		name   string
		method string
		path   string
		body   string
		handle func(http.ResponseWriter, *http.Request)
		status int
	}{
		{"list users", http.MethodGet, "/Users", "", s.listSCIMUsers, http.StatusOK},
		{"create user", http.MethodPost, "/Users", userBody, s.createSCIMUser, http.StatusCreated},
		{"get user", http.MethodGet, "/Users/u-1", "", s.getSCIMUser, http.StatusOK},
		{"replace user", http.MethodPut, "/Users/u-1", userBody, s.replaceSCIMUser, http.StatusOK},
		{"patch user", http.MethodPatch, "/Users/u-1", userBody, s.replaceSCIMUser, http.StatusOK},
		{"delete user", http.MethodDelete, "/Users/u-1", "", s.deleteSCIMUser, http.StatusOK},
		{"list groups", http.MethodGet, "/Groups", "", s.listSCIMGroups, http.StatusOK},
		{"create group", http.MethodPost, "/Groups", groupBody, s.createSCIMGroup, http.StatusCreated},
		{"get group", http.MethodGet, "/Groups/g-1", "", s.getSCIMGroup, http.StatusOK},
		{"replace group", http.MethodPut, "/Groups/g-1", groupBody, s.replaceSCIMGroup, http.StatusOK},
		{"patch group", http.MethodPatch, "/Groups/g-1", patchBody, s.patchSCIMGroup, http.StatusOK},
		{"delete group", http.MethodDelete, "/Groups/g-1", "", s.deleteSCIMGroup, http.StatusNoContent},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			unauthenticated := httptest.NewRequest(tc.method, tc.path, bytes.NewBufferString(tc.body))
			unauthenticatedRec := httptest.NewRecorder()
			s.scimAuthenticate(http.HandlerFunc(tc.handle)).ServeHTTP(unauthenticatedRec, unauthenticated)
			if unauthenticatedRec.Code != http.StatusUnauthorized {
				t.Fatalf("without bearer: status=%d", unauthenticatedRec.Code)
			}

			req := httptest.NewRequest(tc.method, tc.path, bytes.NewBufferString(tc.body))
			req.Header.Set("Authorization", "Bearer valid")
			if id := "u-1"; tc.path == "/Groups/g-1" {
				req.SetPathValue("id", "g-1")
			} else if len(tc.path) > len("/Users") {
				req.SetPathValue("id", id)
			}
			rec := httptest.NewRecorder()
			s.scimAuthenticate(http.HandlerFunc(tc.handle)).ServeHTTP(rec, req)
			if rec.Code != tc.status {
				t.Fatalf("with bearer: status=%d, body=%s", rec.Code, rec.Body.String())
			}
		})
	}

	for _, tenant := range store.tenants {
		if tenant != "tenant-1" {
			t.Fatalf("handler used tenant %q instead of authenticated tenant", tenant)
		}
	}
	if got := store.createdUser.Email; got != "bob@example.test" {
		t.Fatalf("created user email=%q", got)
	}
	if got := store.createdGroup.Members[0].Ref; got != "/Users/u-2" {
		t.Fatalf("created group member ref=%q", got)
	}
	if len(store.patchedGroup.Operations) != 1 || store.patchedGroup.Operations[0].Value[0].Value != "u-2" {
		t.Fatalf("group patch was not mapped: %#v", store.patchedGroup)
	}
}
