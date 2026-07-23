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
	ListSCIMGroups(context.Context, string) ([]domain.SCIMGroup, error)
	GetSCIMGroup(context.Context, string, string) (domain.SCIMGroup, error)
	CreateSCIMGroup(context.Context, domain.Principal, domain.SCIMGroup) (domain.SCIMGroup, error)
	UpdateSCIMGroup(context.Context, domain.Principal, string, domain.SCIMGroup) (domain.SCIMGroup, error)
	PatchSCIMGroup(context.Context, domain.Principal, string, domain.SCIMGroupPatch) (domain.SCIMGroup, error)
	DeleteSCIMGroup(context.Context, domain.Principal, string) error
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

type scimGroupMemberResponse struct {
	Value   string `json:"value"`
	Display string `json:"display,omitempty"`
	Ref     string `json:"$ref,omitempty"`
}

type scimGroupResponse struct {
	Schemas     []string                  `json:"schemas"`
	ID          string                    `json:"id"`
	DisplayName string                    `json:"displayName"`
	Members     []scimGroupMemberResponse `json:"members"`
}

func scimGroupToResponse(g domain.SCIMGroup) scimGroupResponse {
	members := make([]scimGroupMemberResponse, 0, len(g.Members))
	for _, m := range g.Members {
		members = append(members, scimGroupMemberResponse{
			Value:   m.Value,
			Display: m.Display,
			Ref:     m.Ref,
		})
	}
	return scimGroupResponse{
		Schemas:     []string{"urn:ietf:params:scim:schemas:core:2.0:Group"},
		ID:          g.ID,
		DisplayName: g.DisplayName,
		Members:     members,
	}
}

func (s *Server) listSCIMGroups(w http.ResponseWriter, r *http.Request) {
	p := principalFrom(r)
	groups, err := s.store.(scimStore).ListSCIMGroups(r.Context(), p.TenantID)
	if err != nil {
		internalError(w, err, "list SCIM groups", p)
		return
	}
	resources := make([]scimGroupResponse, 0, len(groups))
	for _, group := range groups {
		resources = append(resources, scimGroupToResponse(group))
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"schemas":      []string{"urn:ietf:params:scim:api:messages:2.0:ListResponse"},
		"totalResults": len(resources),
		"Resources":    resources,
	})
}

func (s *Server) getSCIMGroup(w http.ResponseWriter, r *http.Request) {
	p := principalFrom(r)
	group, err := s.store.(scimStore).GetSCIMGroup(r.Context(), p.TenantID, r.PathValue("id"))
	if errors.Is(err, postgres.ErrNotFound) {
		writeError(w, http.StatusNotFound, "not_found", "SCIM group not found")
		return
	}
	if err != nil {
		internalError(w, err, "get SCIM group", p)
		return
	}
	writeJSON(w, http.StatusOK, scimGroupToResponse(group))
}

func (s *Server) createSCIMGroup(w http.ResponseWriter, r *http.Request) {
	var in scimGroupResponse
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_json", err.Error())
		return
	}
	members := make([]domain.SCIMGroupMember, 0, len(in.Members))
	for _, m := range in.Members {
		members = append(members, domain.SCIMGroupMember{Value: m.Value, Display: m.Display, Ref: m.Ref})
	}
	g := domain.SCIMGroup{
		DisplayName: in.DisplayName,
		Members:     members,
	}
	p := principalFrom(r)
	created, err := s.store.(scimStore).CreateSCIMGroup(r.Context(), p, g)
	if err != nil {
		writeError(w, http.StatusUnprocessableEntity, "invalid_group", err.Error())
		return
	}
	writeJSON(w, http.StatusCreated, scimGroupToResponse(created))
}

func (s *Server) replaceSCIMGroup(w http.ResponseWriter, r *http.Request) {
	var in scimGroupResponse
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_json", err.Error())
		return
	}
	members := make([]domain.SCIMGroupMember, 0, len(in.Members))
	for _, m := range in.Members {
		members = append(members, domain.SCIMGroupMember{Value: m.Value, Display: m.Display, Ref: m.Ref})
	}
	g := domain.SCIMGroup{
		DisplayName: in.DisplayName,
		Members:     members,
	}
	p := principalFrom(r)
	updated, err := s.store.(scimStore).UpdateSCIMGroup(r.Context(), p, r.PathValue("id"), g)
	if errors.Is(err, postgres.ErrNotFound) {
		writeError(w, http.StatusNotFound, "not_found", "SCIM group not found")
		return
	}
	if err != nil {
		writeError(w, http.StatusUnprocessableEntity, "invalid_group", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, scimGroupToResponse(updated))
}

func (s *Server) patchSCIMGroup(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Schemas    []string `json:"schemas,omitempty"`
		Operations []struct {
			Op    string          `json:"op"`
			Path  string          `json:"path,omitempty"`
			Value json.RawMessage `json:"value,omitempty"`
		} `json:"Operations,omitempty"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_json", err.Error())
		return
	}

	patch := domain.SCIMGroupPatch{}
	for _, op := range body.Operations {
		var members []domain.SCIMGroupMember
		if len(op.Value) > 0 {
			var memberList []scimGroupMemberResponse
			if err := json.Unmarshal(op.Value, &memberList); err == nil {
				for _, m := range memberList {
					members = append(members, domain.SCIMGroupMember{Value: m.Value, Display: m.Display, Ref: m.Ref})
				}
			} else {
				var singleMember scimGroupMemberResponse
				if err := json.Unmarshal(op.Value, &singleMember); err == nil {
					members = append(members, domain.SCIMGroupMember{Value: singleMember.Value, Display: singleMember.Display, Ref: singleMember.Ref})
				}
			}
		}
		patch.Operations = append(patch.Operations, domain.SCIMGroupOperation{
			Op:    op.Op,
			Path:  op.Path,
			Value: members,
		})
	}

	p := principalFrom(r)
	updated, err := s.store.(scimStore).PatchSCIMGroup(r.Context(), p, r.PathValue("id"), patch)
	if errors.Is(err, postgres.ErrNotFound) {
		writeError(w, http.StatusNotFound, "not_found", "SCIM group not found")
		return
	}
	if err != nil {
		writeError(w, http.StatusUnprocessableEntity, "invalid_patch", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, scimGroupToResponse(updated))
}

func (s *Server) deleteSCIMGroup(w http.ResponseWriter, r *http.Request) {
	p := principalFrom(r)
	err := s.store.(scimStore).DeleteSCIMGroup(r.Context(), p, r.PathValue("id"))
	if errors.Is(err, postgres.ErrNotFound) {
		writeError(w, http.StatusNotFound, "not_found", "SCIM group not found")
		return
	}
	if err != nil {
		internalError(w, err, "delete SCIM group", p)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

