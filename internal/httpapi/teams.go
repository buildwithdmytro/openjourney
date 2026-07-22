package httpapi

import (
	"errors"
	"net/http"

	"github.com/buildwithdmytro/openjourney/internal/domain"
	"github.com/buildwithdmytro/openjourney/internal/ports"
)

func (s *Server) listTeams(w http.ResponseWriter, r *http.Request) {
	principal := principalFrom(r)
	items, err := s.store.ListTeams(r.Context(), principal)
	if err != nil {
		internalError(w, err, "list teams", principal)
		return
	}
	if items == nil {
		items = []domain.Team{}
	}
	writeJSON(w, http.StatusOK, map[string]any{"teams": items})
}

func (s *Server) createTeam(w http.ResponseWriter, r *http.Request) {
	var input domain.Team
	if err := decodeJSON(w, r, &input); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_json", err.Error())
		return
	}
	principal := principalFrom(r)
	item, err := s.store.CreateTeam(r.Context(), principal, input)
	if err != nil {
		writeError(w, http.StatusUnprocessableEntity, "invalid_team", err.Error())
		return
	}
	writeJSON(w, http.StatusCreated, item)
}

func (s *Server) getTeam(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	principal := principalFrom(r)
	item, err := s.store.GetTeam(r.Context(), principal, id)
	if err != nil {
		if errors.Is(err, ports.ErrNotFound) {
			writeError(w, http.StatusNotFound, "not_found", "team not found")
			return
		}
		internalError(w, err, "get team", principal)
		return
	}
	writeJSON(w, http.StatusOK, item)
}

func (s *Server) updateTeam(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	var input domain.Team
	if err := decodeJSON(w, r, &input); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_json", err.Error())
		return
	}
	input.ID = id
	principal := principalFrom(r)
	item, err := s.store.UpdateTeam(r.Context(), principal, input)
	if err != nil {
		if errors.Is(err, ports.ErrNotFound) {
			writeError(w, http.StatusNotFound, "not_found", "team not found")
			return
		}
		writeError(w, http.StatusUnprocessableEntity, "invalid_team", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, item)
}

func (s *Server) deleteTeam(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	principal := principalFrom(r)
	if err := s.store.DeleteTeam(r.Context(), principal, id); err != nil {
		if errors.Is(err, ports.ErrNotFound) {
			writeError(w, http.StatusNotFound, "not_found", "team not found")
			return
		}
		internalError(w, err, "delete team", principal)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
