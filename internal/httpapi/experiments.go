package httpapi

import (
	"errors"
	"net/http"

	"github.com/buildwithdmytro/openjourney/internal/domain"
	"github.com/buildwithdmytro/openjourney/internal/postgres"
)

func (s *Server) createExperiment(w http.ResponseWriter, r *http.Request) {
	p := principalFrom(r)
	var input domain.Experiment
	if err := decodeJSON(w, r, &input); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_json", err.Error())
		return
	}
	out, err := s.store.CreateExperiment(r.Context(), p, input)
	if err != nil {
		internalError(w, err, "create experiment", p)
		return
	}
	writeJSON(w, http.StatusCreated, out)
}

func (s *Server) listExperiments(w http.ResponseWriter, r *http.Request) {
	p := principalFrom(r)
	out, err := s.store.ListExperiments(r.Context(), p)
	if err != nil {
		internalError(w, err, "list experiments", p)
		return
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) getExperiment(w http.ResponseWriter, r *http.Request) {
	p := principalFrom(r)
	out, err := s.store.GetExperiment(r.Context(), p, r.PathValue("id"))
	if errors.Is(err, postgres.ErrNotFound) {
		writeError(w, http.StatusNotFound, "not_found", "experiment not found")
		return
	}
	if err != nil {
		internalError(w, err, "get experiment", p)
		return
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) updateExperiment(w http.ResponseWriter, r *http.Request) {
	p := principalFrom(r)
	var input domain.Experiment
	if err := decodeJSON(w, r, &input); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_json", err.Error())
		return
	}
	input.ID = r.PathValue("id")
	out, err := s.store.UpdateExperiment(r.Context(), p, input)
	if errors.Is(err, postgres.ErrNotFound) {
		writeError(w, http.StatusNotFound, "not_found", "experiment not found")
		return
	}
	if err != nil {
		internalError(w, err, "update experiment", p)
		return
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) rolloutExperiment(w http.ResponseWriter, r *http.Request) {
	p := principalFrom(r)
	if p.ActorType != "user" || p.UserID == "" {
		writeError(w, http.StatusForbidden, "human_approval_required", "experiment rollout requires an authenticated user")
		return
	}
	out, err := s.store.RolloutExperiment(r.Context(), p, r.PathValue("id"))
	if errors.Is(err, postgres.ErrNotFound) {
		writeError(w, http.StatusNotFound, "not_found", "experiment or bound subject not found")
		return
	}
	if errors.Is(err, postgres.ErrExperimentWinnerRequired) {
		writeError(w, http.StatusConflict, "winner_required", "experiment has no recommended winner")
		return
	}
	if err != nil {
		internalError(w, err, "roll out experiment", p)
		return
	}
	writeJSON(w, http.StatusCreated, out)
}

func (s *Server) proposeExperimentOptimization(w http.ResponseWriter, r *http.Request) {
	p := principalFrom(r)
	out, err := s.store.ProposeExperimentOptimization(r.Context(), p, r.PathValue("id"))
	if errors.Is(err, postgres.ErrNotFound) {
		writeError(w, http.StatusNotFound, "not_found", "experiment not found")
		return
	}
	if errors.Is(err, postgres.ErrOptimizationUnavailable) {
		writeError(w, http.StatusConflict, "optimization_unavailable", "experiment has no eligible winner")
		return
	}
	if err != nil {
		internalError(w, err, "propose experiment optimization", p)
		return
	}
	writeJSON(w, http.StatusCreated, out)
}
