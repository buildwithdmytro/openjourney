package httpapi

import (
	"encoding/json"
	"errors"
	"net/http"

	"github.com/buildwithdmytro/openjourney/internal/postgres"
)

func (s *Server) createAIGeneration(w http.ResponseWriter, r *http.Request) {
	var input struct {
		TaskType string          `json:"task_type"`
		Input    json.RawMessage `json:"input"`
	}
	if err := decodeJSON(w, r, &input); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_json", err.Error())
		return
	}
	principal := principalFrom(r)
	request, err := s.store.CreateAIGenerationRequest(r.Context(), principal, input.TaskType, input.Input)
	if err != nil {
		writeError(w, http.StatusUnprocessableEntity, "invalid_ai_generation", err.Error())
		return
	}
	writeJSON(w, http.StatusAccepted, request)
}

func (s *Server) getAIGeneration(w http.ResponseWriter, r *http.Request) {
	principal := principalFrom(r)
	request, err := s.store.GetAIGenerationRequest(r.Context(), principal, r.PathValue("id"))
	if errors.Is(err, postgres.ErrNotFound) {
		writeError(w, http.StatusNotFound, "not_found", "AI generation was not found")
		return
	}
	if err != nil {
		internalError(w, err, "get AI generation", principal)
		return
	}
	writeJSON(w, http.StatusOK, request)
}
