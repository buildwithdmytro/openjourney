package httpapi

import (
	"context"
	"encoding/json"
	"net/http"

	"github.com/buildwithdmytro/openjourney/internal/domain"
)

type stageStore interface {
	CreateStageRule(context.Context, domain.Principal, domain.StageRule) (domain.StageRule, error)
	ListStageRules(context.Context, domain.Principal) ([]domain.StageRule, error)
}

func (s *Server) createStageRule(w http.ResponseWriter, r *http.Request) {
	var input domain.StageRule
	if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_json", err.Error())
		return
	}
	store, ok := s.store.(stageStore)
	if !ok {
		writeError(w, http.StatusServiceUnavailable, "stages_unavailable", "stage storage is unavailable")
		return
	}
	out, err := store.CreateStageRule(r.Context(), principalFrom(r), input)
	if err != nil {
		writeError(w, http.StatusUnprocessableEntity, "invalid_stage", err.Error())
		return
	}
	writeJSON(w, http.StatusCreated, out)
}

func (s *Server) listStageRules(w http.ResponseWriter, r *http.Request) {
	store, ok := s.store.(stageStore)
	if !ok {
		writeError(w, http.StatusServiceUnavailable, "stages_unavailable", "stage storage is unavailable")
		return
	}
	out, err := store.ListStageRules(r.Context(), principalFrom(r))
	if err != nil {
		internalError(w, err, "list stage rules", principalFrom(r))
		return
	}
	if out == nil {
		out = []domain.StageRule{}
	}
	writeJSON(w, http.StatusOK, map[string]any{"stages": out})
}
