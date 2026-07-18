package httpapi

import (
	"errors"
	"net/http"

	"github.com/buildwithdmytro/openjourney/internal/postgres"
)

func (s *Server) createScoringRequest(w http.ResponseWriter, r *http.Request) {
	var input struct {
		ScoringModelID string `json:"scoring_model_id"`
		SegmentID      string `json:"segment_id"`
	}
	if err := decodeJSON(w, r, &input); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_json", err.Error())
		return
	}
	principal := principalFrom(r)
	request, err := s.store.CreateScoringRequest(r.Context(), principal, input.ScoringModelID, input.SegmentID)
	if err != nil {
		writeError(w, http.StatusUnprocessableEntity, "invalid_scoring_request", err.Error())
		return
	}
	writeJSON(w, http.StatusAccepted, request)
}

func (s *Server) getScoringRequest(w http.ResponseWriter, r *http.Request) {
	principal := principalFrom(r)
	request, err := s.store.GetScoringRequest(r.Context(), principal, r.PathValue("id"))
	if errors.Is(err, postgres.ErrNotFound) {
		writeError(w, http.StatusNotFound, "not_found", "Scoring request was not found")
		return
	}
	if err != nil {
		internalError(w, err, "get scoring request", principal)
		return
	}
	writeJSON(w, http.StatusOK, request)
}
