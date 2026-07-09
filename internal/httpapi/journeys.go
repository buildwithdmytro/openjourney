package httpapi

import (
	"errors"
	"net/http"

	"github.com/buildwithdmytro/openjourney/internal/domain"
	"github.com/buildwithdmytro/openjourney/internal/postgres"
)

func (s *Server) createJourney(w http.ResponseWriter, r *http.Request) {
	principal := principalFrom(r)
	var journey domain.Journey
	if err := decodeJSON(w, r, &journey); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_json", err.Error())
		return
	}
	res, err := s.store.CreateJourney(r.Context(), principal, journey)
	if err != nil {
		internalError(w, err, "create journey", principal)
		return
	}
	writeJSON(w, http.StatusCreated, res)
}

func (s *Server) getJourney(w http.ResponseWriter, r *http.Request) {
	principal := principalFrom(r)
	id := r.PathValue("id")
	res, err := s.store.GetJourney(r.Context(), principal, id)
	if errors.Is(err, postgres.ErrNotFound) {
		writeError(w, http.StatusNotFound, "not_found", "journey not found")
		return
	}
	if err != nil {
		internalError(w, err, "get journey", principal)
		return
	}
	writeJSON(w, http.StatusOK, res)
}

func (s *Server) updateJourney(w http.ResponseWriter, r *http.Request) {
	principal := principalFrom(r)
	id := r.PathValue("id")
	var journey domain.Journey
	if err := decodeJSON(w, r, &journey); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_json", err.Error())
		return
	}
	journey.ID = id
	res, err := s.store.UpdateJourney(r.Context(), principal, journey)
	if errors.Is(err, postgres.ErrNotFound) {
		writeError(w, http.StatusNotFound, "not_found", "journey not found")
		return
	}
	if err != nil {
		internalError(w, err, "update journey", principal)
		return
	}
	writeJSON(w, http.StatusOK, res)
}

func (s *Server) listJourneys(w http.ResponseWriter, r *http.Request) {
	principal := principalFrom(r)
	res, err := s.store.ListJourneys(r.Context(), principal)
	if err != nil {
		internalError(w, err, "list journeys", principal)
		return
	}
	if res == nil {
		res = []domain.Journey{}
	}
	writeJSON(w, http.StatusOK, map[string]any{"journeys": res})
}
