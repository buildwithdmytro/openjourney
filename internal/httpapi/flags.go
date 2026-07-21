package httpapi

import (
	"errors"
	"net/http"

	"github.com/buildwithdmytro/openjourney/internal/domain"
	"github.com/buildwithdmytro/openjourney/internal/postgres"
)

func (s *Server) createFeatureFlag(w http.ResponseWriter, r *http.Request) {
	principal := principalFrom(r)
	var flag domain.FeatureFlag
	if err := decodeJSON(w, r, &flag); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_json", err.Error())
		return
	}
	res, err := s.store.CreateFeatureFlag(r.Context(), principal, flag)
	if err != nil {
		internalError(w, err, "create flag", principal)
		return
	}
	writeJSON(w, http.StatusCreated, res)
}

func (s *Server) getFeatureFlag(w http.ResponseWriter, r *http.Request) {
	principal := principalFrom(r)
	id := r.PathValue("id")
	res, err := s.store.GetFeatureFlag(r.Context(), principal, id)
	if errors.Is(err, postgres.ErrNotFound) {
		writeError(w, http.StatusNotFound, "not_found", "flag not found")
		return
	}
	if err != nil {
		internalError(w, err, "get flag", principal)
		return
	}
	writeJSON(w, http.StatusOK, res)
}

func (s *Server) updateFeatureFlag(w http.ResponseWriter, r *http.Request) {
	principal := principalFrom(r)
	id := r.PathValue("id")
	var flag domain.FeatureFlag
	if err := decodeJSON(w, r, &flag); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_json", err.Error())
		return
	}
	flag.ID = id
	res, err := s.store.UpdateFeatureFlag(r.Context(), principal, flag)
	if errors.Is(err, postgres.ErrNotFound) {
		writeError(w, http.StatusNotFound, "not_found", "flag not found")
		return
	}
	if err != nil {
		internalError(w, err, "update flag", principal)
		return
	}
	writeJSON(w, http.StatusOK, res)
}

func (s *Server) listFeatureFlags(w http.ResponseWriter, r *http.Request) {
	principal := principalFrom(r)
	res, err := s.store.ListFeatureFlags(r.Context(), principal)
	if err != nil {
		internalError(w, err, "list flags", principal)
		return
	}
	if res == nil {
		res = []domain.FeatureFlag{}
	}
	writeJSON(w, http.StatusOK, map[string]any{"flags": res})
}

func (s *Server) publishFeatureFlag(w http.ResponseWriter, r *http.Request) {
	principal := principalFrom(r)
	id := r.PathValue("id")
	if !isHuman(principal) {
		writeError(w, http.StatusForbidden, "human_approval_required", "publishing requires an authenticated user")
		return
	}
	var input struct {
		ApproverUserID string `json:"approver_user_id"`
	}
	if r.Body != nil && r.ContentLength != 0 {
		if err := decodeJSON(w, r, &input); err != nil {
			writeError(w, http.StatusBadRequest, "invalid_json", err.Error())
			return
		}
	}
	// approver_user_id in request is for backwards compatibility; always use the authenticated user
	version, err := s.store.PublishFeatureFlag(r.Context(), principal, id, principal.UserID, "")
	if errors.Is(err, postgres.ErrNotFound) {
		writeError(w, http.StatusNotFound, "not_found", "flag not found")
		return
	}
	if err != nil {
		internalError(w, err, "publish flag", principal)
		return
	}
	writeJSON(w, http.StatusOK, version)
}

func (s *Server) setFlagStatus(w http.ResponseWriter, r *http.Request) {
	principal := principalFrom(r)
	id := r.PathValue("id")
	if !isHuman(principal) {
		writeError(w, http.StatusForbidden, "human_approval_required", "changing flag status requires an authenticated user")
		return
	}
	var input struct {
		Status string `json:"status"`
	}
	if err := decodeJSON(w, r, &input); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_json", err.Error())
		return
	}
	// Validate status
	validStatuses := map[string]bool{"draft": true, "published": true, "disabled": true}
	if !validStatuses[input.Status] {
		writeError(w, http.StatusBadRequest, "invalid_status", "status must be one of: draft, published, disabled")
		return
	}

	// Get the current flag
	flag, err := s.store.GetFeatureFlag(r.Context(), principal, id)
	if errors.Is(err, postgres.ErrNotFound) {
		writeError(w, http.StatusNotFound, "not_found", "flag not found")
		return
	}
	if err != nil {
		internalError(w, err, "get flag", principal)
		return
	}

	// Update the status
	flag.Status = input.Status
	res, err := s.store.UpdateFeatureFlag(r.Context(), principal, flag)
	if err != nil {
		internalError(w, err, "update flag status", principal)
		return
	}
	writeJSON(w, http.StatusOK, res)
}
