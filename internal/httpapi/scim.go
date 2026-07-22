package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"

	"github.com/buildwithdmytro/openjourney/internal/domain"
	"github.com/buildwithdmytro/openjourney/internal/postgres"
)

type scimStore interface {
	AuthenticateSCIM(context.Context, string) (domain.Principal, error)
	ListSCIMUsers(context.Context, string) ([]domain.User, error)
	GetSCIMUser(context.Context, string, string) (domain.User, error)
	CreateSCIMUser(context.Context, domain.Principal, domain.User, bool) (domain.User, error)
	UpdateSCIMUser(context.Context, domain.Principal, string, domain.User, bool) (domain.User, error)
}

func (s *Server) scimAuthenticate(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		header := strings.TrimSpace(r.Header.Get("Authorization"))
		if len(header) < 7 || !strings.EqualFold(header[:7], "bearer ") {
			writeError(w, http.StatusUnauthorized, "unauthorized", "SCIM bearer token is required")
			return
		}
		store, ok := s.store.(scimStore)
		if !ok {
			writeError(w, http.StatusInternalServerError, "internal_error", "SCIM is unavailable")
			return
		}
		p, err := store.AuthenticateSCIM(r.Context(), strings.TrimSpace(header[7:]))
		if errors.Is(err, postgres.ErrUnauthorized) {
			writeError(w, http.StatusUnauthorized, "unauthorized", "invalid SCIM bearer token")
			return
		}
		if err != nil {
			internalError(w, err, "authenticate SCIM", domain.Principal{})
			return
		}
		next.ServeHTTP(w, scimWithPrincipal(r, p))
	})
}

func scimWithPrincipal(r *http.Request, p domain.Principal) *http.Request {
	return r.WithContext(context.WithValue(r.Context(), principalKey{}, p))
}

type scimEmail struct {
	Value   string `json:"value"`
	Primary bool   `json:"primary,omitempty"`
}
type scimUser struct {
	Schemas     []string    `json:"schemas,omitempty"`
	ID          string      `json:"id,omitempty"`
	UserName    string      `json:"userName"`
	Active      bool        `json:"active"`
	DisplayName string      `json:"displayName,omitempty"`
	Emails      []scimEmail `json:"emails,omitempty"`
}

func scimUserResponse(u domain.User) scimUser {
	out := scimUser{Schemas: []string{"urn:ietf:params:scim:schemas:core:2.0:User"}, ID: u.ID, UserName: u.OIDCSubject, Active: u.DisabledAt == nil, DisplayName: u.DisplayName}
	if u.Email != "" {
		out.Emails = []scimEmail{{Value: u.Email, Primary: true}}
	}
	return out
}

func decodeSCIMUser(r *http.Request) (domain.User, bool, error) {
	var in scimUser
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		return domain.User{}, false, err
	}
	u := domain.User{OIDCSubject: in.UserName, DisplayName: in.DisplayName}
	for _, email := range in.Emails {
		if email.Primary || u.Email == "" {
			u.Email = email.Value
		}
	}
	return u, in.Active, nil
}

func (s *Server) listSCIMUsers(w http.ResponseWriter, r *http.Request) {
	p := principalFrom(r)
	users, err := s.store.(scimStore).ListSCIMUsers(r.Context(), p.TenantID)
	if err != nil {
		internalError(w, err, "list SCIM users", p)
		return
	}
	resources := make([]scimUser, 0, len(users))
	for _, user := range users {
		resources = append(resources, scimUserResponse(user))
	}
	writeJSON(w, http.StatusOK, map[string]any{"schemas": []string{"urn:ietf:params:scim:api:messages:2.0:ListResponse"}, "totalResults": len(resources), "Resources": resources})
}

func (s *Server) getSCIMUser(w http.ResponseWriter, r *http.Request) {
	p := principalFrom(r)
	user, err := s.store.(scimStore).GetSCIMUser(r.Context(), p.TenantID, r.PathValue("id"))
	if errors.Is(err, postgres.ErrNotFound) {
		writeError(w, http.StatusNotFound, "not_found", "SCIM user not found")
		return
	}
	if err != nil {
		internalError(w, err, "get SCIM user", p)
		return
	}
	writeJSON(w, http.StatusOK, scimUserResponse(user))
}

func (s *Server) createSCIMUser(w http.ResponseWriter, r *http.Request) {
	user, active, err := decodeSCIMUser(r)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_json", err.Error())
		return
	}
	p := principalFrom(r)
	created, err := s.store.(scimStore).CreateSCIMUser(r.Context(), p, user, active)
	if err != nil {
		writeError(w, http.StatusUnprocessableEntity, "invalid_user", err.Error())
		return
	}
	writeJSON(w, http.StatusCreated, scimUserResponse(created))
}

func (s *Server) replaceSCIMUser(w http.ResponseWriter, r *http.Request) {
	user, active, err := decodeSCIMUser(r)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_json", err.Error())
		return
	}
	p := principalFrom(r)
	updated, err := s.store.(scimStore).UpdateSCIMUser(r.Context(), p, r.PathValue("id"), user, active)
	if errors.Is(err, postgres.ErrNotFound) {
		writeError(w, http.StatusNotFound, "not_found", "SCIM user not found")
		return
	}
	if err != nil {
		writeError(w, http.StatusUnprocessableEntity, "invalid_user", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, scimUserResponse(updated))
}

func (s *Server) deleteSCIMUser(w http.ResponseWriter, r *http.Request) {
	p := principalFrom(r)
	updated, err := s.store.(scimStore).UpdateSCIMUser(r.Context(), p, r.PathValue("id"), domain.User{}, false)
	if errors.Is(err, postgres.ErrNotFound) {
		writeError(w, http.StatusNotFound, "not_found", "SCIM user not found")
		return
	}
	if err != nil {
		internalError(w, err, "deprovision SCIM user", p)
		return
	}
	writeJSON(w, http.StatusOK, scimUserResponse(updated))
}
