package httpapi

import (
	"errors"
	"net/http"

	"github.com/buildwithdmytro/openjourney/internal/domain"
	"github.com/buildwithdmytro/openjourney/internal/postgres"
)

func (s *Server) createSavedReport(w http.ResponseWriter, r *http.Request) {
	principal := principalFrom(r)
	var report domain.SavedReport
	if err := decodeJSON(w, r, &report); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_json", err.Error())
		return
	}
	res, err := s.store.CreateSavedReport(r.Context(), principal, report)
	if err != nil {
		internalError(w, err, "create saved report", principal)
		return
	}
	writeJSON(w, http.StatusCreated, res)
}

func (s *Server) getSavedReport(w http.ResponseWriter, r *http.Request) {
	principal := principalFrom(r)
	id := r.PathValue("id")
	res, err := s.store.GetSavedReport(r.Context(), principal, id)
	if errors.Is(err, postgres.ErrNotFound) {
		writeError(w, http.StatusNotFound, "not_found", "saved report not found")
		return
	}
	if err != nil {
		internalError(w, err, "get saved report", principal)
		return
	}
	writeJSON(w, http.StatusOK, res)
}

func (s *Server) listSavedReports(w http.ResponseWriter, r *http.Request) {
	principal := principalFrom(r)
	res, err := s.store.ListSavedReports(r.Context(), principal)
	if err != nil {
		internalError(w, err, "list saved reports", principal)
		return
	}
	if res == nil {
		res = []domain.SavedReport{}
	}
	writeJSON(w, http.StatusOK, map[string]any{"saved_reports": res})
}

func (s *Server) deleteSavedReport(w http.ResponseWriter, r *http.Request) {
	principal := principalFrom(r)
	id := r.PathValue("id")
	err := s.store.DeleteSavedReport(r.Context(), principal, id)
	if errors.Is(err, postgres.ErrNotFound) {
		writeError(w, http.StatusNotFound, "not_found", "saved report not found")
		return
	}
	if err != nil {
		internalError(w, err, "delete saved report", principal)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
