package httpapi

import (
	"net/http"
)

func (s *Server) listMakerCheckerPolicies(w http.ResponseWriter, r *http.Request) {
	p := principalFrom(r)
	if !p.HasScope("roles:read") && !p.HasScope("teams:read") && !p.HasScope("operations:read") && !p.HasScope("ai:configure") {
		writeError(w, http.StatusForbidden, "forbidden", "insufficient permissions")
		return
	}
	policies, err := s.store.ListMakerCheckerPolicies(r.Context(), p)
	if err != nil {
		internalError(w, err, "list maker checker policies", p)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"policies": policies})
}

func (s *Server) getMakerCheckerPolicy(w http.ResponseWriter, r *http.Request) {
	p := principalFrom(r)
	if !p.HasScope("roles:read") && !p.HasScope("teams:read") && !p.HasScope("operations:read") && !p.HasScope("ai:configure") {
		writeError(w, http.StatusForbidden, "forbidden", "insufficient permissions")
		return
	}
	resourceType := r.PathValue("resource_type")
	requireChecker, err := s.store.GetMakerCheckerPolicy(r.Context(), p, resourceType)
	if err != nil {
		internalError(w, err, "get maker checker policy", p)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"resource_type":   resourceType,
		"require_checker": requireChecker,
	})
}

func (s *Server) setMakerCheckerPolicy(w http.ResponseWriter, r *http.Request) {
	p := principalFrom(r)
	if !p.HasScope("roles:write") && !p.HasScope("teams:write") && !p.HasScope("operations:write") && !p.HasScope("ai:configure") {
		writeError(w, http.StatusForbidden, "forbidden", "insufficient permissions")
		return
	}
	resourceType := r.PathValue("resource_type")
	var req struct {
		RequireChecker bool `json:"require_checker"`
	}
	if err := decodeJSON(w, r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_json", err.Error())
		return
	}
	policy, err := s.store.SetMakerCheckerPolicy(r.Context(), p, resourceType, req.RequireChecker)
	if err != nil {
		internalError(w, err, "set maker checker policy", p)
		return
	}
	writeJSON(w, http.StatusOK, policy)
}
