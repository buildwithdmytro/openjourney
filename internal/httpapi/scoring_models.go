package httpapi

import (
	"errors"
	"net/http"

	"github.com/buildwithdmytro/openjourney/internal/domain"
	"github.com/buildwithdmytro/openjourney/internal/postgres"
)

func (s *Server) listScoringModels(w http.ResponseWriter, r *http.Request) {
	p := principalFrom(r)
	items, err := s.store.ListScoringModels(r.Context(), p)
	if err != nil {
		internalError(w, err, "list scoring models", p)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"models": items})
}

func (s *Server) createScoringModel(w http.ResponseWriter, r *http.Request) {
	p := principalFrom(r)
	var input domain.ScoringModel
	if err := decodeJSON(w, r, &input); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_json", err.Error())
		return
	}
	out, err := s.store.CreateScoringModel(r.Context(), p, input)
	if err != nil {
		writeError(w, http.StatusUnprocessableEntity, "invalid_scoring_model", err.Error())
		return
	}
	writeJSON(w, http.StatusCreated, out)
}

func (s *Server) createScoringModelVersion(w http.ResponseWriter, r *http.Request) {
	p := principalFrom(r)
	var input domain.ScoringModelVersion
	if err := decodeJSON(w, r, &input); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_json", err.Error())
		return
	}
	input.ScoringModelID = r.PathValue("id")
	out, err := s.store.CreateScoringModelVersion(r.Context(), p, input)
	if err != nil {
		writeError(w, http.StatusUnprocessableEntity, "invalid_scoring_model_version", err.Error())
		return
	}
	writeJSON(w, http.StatusCreated, out)
}

func (s *Server) publishScoringModelVersion(w http.ResponseWriter, r *http.Request) {
	p := principalFrom(r)
	if p.ActorType != "user" || p.UserID == "" {
		writeError(w, http.StatusForbidden, "human_approval_required", "scoring model publish requires an authenticated user")
		return
	}
	modelID := r.PathValue("id")
	var input struct {
		Version     int    `json:"version"`
		ManifestKey string `json:"manifest_key"`
	}
	if err := decodeJSON(w, r, &input); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_json", err.Error())
		return
	}
	out, err := s.store.PublishScoringModelVersion(r.Context(), p, modelID, input.Version, p.UserID, input.ManifestKey)
	if errors.Is(err, postgres.ErrNotFound) {
		writeError(w, http.StatusNotFound, "not_found", "scoring model version was not found")
		return
	}
	if err != nil {
		writeError(w, http.StatusUnprocessableEntity, "publish_failed", err.Error())
		return
	}
	writeJSON(w, http.StatusCreated, out)
}

func (s *Server) listProfileScores(w http.ResponseWriter, r *http.Request) {
	p := principalFrom(r)
	items, err := s.store.ListProfileScores(r.Context(), p, r.PathValue("profileID"))
	if err != nil {
		internalError(w, err, "list profile scores", p)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"scores": items})
}
