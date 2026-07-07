package httpapi

import (
	"errors"
	"net/http"

	"github.com/buildwithdmytro/openjourney/internal/domain"
	"github.com/buildwithdmytro/openjourney/internal/postgres"
)

func (s *Server) createSegment(w http.ResponseWriter, r *http.Request) {
	principal := principalFrom(r)
	var seg domain.Segment
	if err := decodeJSON(w, r, &seg); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_json", err.Error())
		return
	}
	res, err := s.store.CreateSegment(r.Context(), principal, seg)
	if err != nil {
		internalError(w, err, "create segment", principal)
		return
	}
	writeJSON(w, http.StatusCreated, res)
}

func (s *Server) getSegment(w http.ResponseWriter, r *http.Request) {
	principal := principalFrom(r)
	id := r.PathValue("id")
	res, err := s.store.GetSegment(r.Context(), principal, id)
	if errors.Is(err, postgres.ErrNotFound) {
		writeError(w, http.StatusNotFound, "not_found", "segment not found")
		return
	}
	if err != nil {
		internalError(w, err, "get segment", principal)
		return
	}
	writeJSON(w, http.StatusOK, res)
}

func (s *Server) updateSegment(w http.ResponseWriter, r *http.Request) {
	principal := principalFrom(r)
	id := r.PathValue("id")
	var seg domain.Segment
	if err := decodeJSON(w, r, &seg); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_json", err.Error())
		return
	}
	seg.ID = id
	res, err := s.store.UpdateSegment(r.Context(), principal, seg)
	if errors.Is(err, postgres.ErrNotFound) {
		writeError(w, http.StatusNotFound, "not_found", "segment not found")
		return
	}
	if err != nil {
		internalError(w, err, "update segment", principal)
		return
	}
	writeJSON(w, http.StatusOK, res)
}

func (s *Server) listSegments(w http.ResponseWriter, r *http.Request) {
	principal := principalFrom(r)
	res, err := s.store.ListSegments(r.Context(), principal)
	if err != nil {
		internalError(w, err, "list segments", principal)
		return
	}
	writeJSON(w, http.StatusOK, res)
}

func (s *Server) setSegmentMembers(w http.ResponseWriter, r *http.Request) {
	principal := principalFrom(r)
	id := r.PathValue("id")
	var input []domain.SegmentMember
	if err := decodeJSON(w, r, &input); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_json", err.Error())
		return
	}
	err := s.store.SetSegmentMembers(r.Context(), principal, id, input)
	if errors.Is(err, postgres.ErrNotFound) {
		writeError(w, http.StatusNotFound, "not_found", "segment not found")
		return
	}
	if err != nil {
		internalError(w, err, "set segment members", principal)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"status": "ok"})
}

func (s *Server) previewSegment(w http.ResponseWriter, r *http.Request) {
	principal := principalFrom(r)
	id := r.PathValue("id")
	count, perLeg, err := s.store.PreviewSegment(r.Context(), principal, id)
	if errors.Is(err, postgres.ErrNotFound) {
		writeError(w, http.StatusNotFound, "not_found", "segment not found")
		return
	}
	if err != nil {
		internalError(w, err, "preview segment", principal)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"count":          count,
		"per_leg_counts": perLeg,
	})
}
