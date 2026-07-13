package httpapi

import (
	"errors"
	"net/http"

	"github.com/buildwithdmytro/openjourney/internal/postgres"
)

func (s *Server) getCampaignReport(w http.ResponseWriter, r *http.Request) {
	principal := principalFrom(r)
	report, err := s.store.CampaignReport(r.Context(), principal, r.PathValue("id"))
	if errors.Is(err, postgres.ErrNotFound) {
		writeError(w, http.StatusNotFound, "not_found", "campaign report not found")
		return
	}
	if err != nil {
		internalError(w, err, "get campaign report", principal)
		return
	}
	writeJSON(w, http.StatusOK, report)
}

func (s *Server) getJourneyReport(w http.ResponseWriter, r *http.Request) {
	principal := principalFrom(r)
	report, err := s.store.JourneyReport(r.Context(), principal, r.PathValue("id"))
	if errors.Is(err, postgres.ErrNotFound) {
		writeError(w, http.StatusNotFound, "not_found", "journey report not found")
		return
	}
	if err != nil {
		internalError(w, err, "get journey report", principal)
		return
	}
	writeJSON(w, http.StatusOK, report)
}

func (s *Server) getExperimentReport(w http.ResponseWriter, r *http.Request) {
	principal := principalFrom(r)
	report, err := s.store.ExperimentReport(r.Context(), principal, r.PathValue("id"))
	if errors.Is(err, postgres.ErrNotFound) {
		writeError(w, http.StatusNotFound, "not_found", "experiment report not found")
		return
	}
	if err != nil {
		internalError(w, err, "get experiment report", principal)
		return
	}
	writeJSON(w, http.StatusOK, report)
}
