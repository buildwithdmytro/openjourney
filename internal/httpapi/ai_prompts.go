package httpapi

import (
	"context"
	"errors"
	"net/http"
	"strconv"
	"strings"

	"github.com/buildwithdmytro/openjourney/internal/domain"
	"github.com/buildwithdmytro/openjourney/internal/ports"
	"github.com/buildwithdmytro/openjourney/internal/postgres"
	"github.com/buildwithdmytro/openjourney/internal/prompts"
)

type noopBlobStore struct{}

func (n noopBlobStore) Put(ctx context.Context, key string, data []byte, contentType string) error {
	return nil
}

func (n noopBlobStore) Get(ctx context.Context, key string) ([]byte, error) {
	return nil, nil
}

func (n noopBlobStore) Delete(ctx context.Context, key string) error {
	return nil
}

func (s *Server) listPrompts(w http.ResponseWriter, r *http.Request) {
	p := principalFrom(r)
	items, err := s.store.ListPrompts(r.Context(), p)
	if err != nil {
		internalError(w, err, "list prompts", p)
		return
	}
	if items == nil {
		items = []domain.Prompt{}
	}
	writeJSON(w, http.StatusOK, map[string]any{"prompts": items})
}

func (s *Server) createPrompt(w http.ResponseWriter, r *http.Request) {
	p := principalFrom(r)
	var input domain.Prompt
	if err := decodeJSON(w, r, &input); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_json", err.Error())
		return
	}
	out, err := s.store.CreatePrompt(r.Context(), p, input)
	if err != nil {
		writeError(w, http.StatusUnprocessableEntity, "invalid_prompt", err.Error())
		return
	}
	writeJSON(w, http.StatusCreated, out)
}

func (s *Server) getPrompt(w http.ResponseWriter, r *http.Request) {
	p := principalFrom(r)
	id := r.PathValue("id")
	out, err := s.store.GetPrompt(r.Context(), p, id)
	if errors.Is(err, postgres.ErrNotFound) {
		writeError(w, http.StatusNotFound, "not_found", "prompt not found")
		return
	}
	if err != nil {
		internalError(w, err, "get prompt", p)
		return
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) updatePrompt(w http.ResponseWriter, r *http.Request) {
	p := principalFrom(r)
	id := r.PathValue("id")
	var input domain.Prompt
	if err := decodeJSON(w, r, &input); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_json", err.Error())
		return
	}
	input.ID = id
	out, err := s.store.UpdatePrompt(r.Context(), p, input)
	if errors.Is(err, postgres.ErrNotFound) {
		writeError(w, http.StatusNotFound, "not_found", "prompt not found")
		return
	}
	if err != nil {
		writeError(w, http.StatusUnprocessableEntity, "invalid_prompt", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) deletePrompt(w http.ResponseWriter, r *http.Request) {
	p := principalFrom(r)
	id := r.PathValue("id")
	err := s.store.DeletePrompt(r.Context(), p, id)
	if errors.Is(err, postgres.ErrNotFound) {
		writeError(w, http.StatusNotFound, "not_found", "prompt not found")
		return
	}
	if err != nil {
		internalError(w, err, "delete prompt", p)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) listPromptVersions(w http.ResponseWriter, r *http.Request) {
	p := principalFrom(r)
	promptID := r.PathValue("id")
	items, err := s.store.ListPromptVersions(r.Context(), p, promptID)
	if err != nil {
		internalError(w, err, "list prompt versions", p)
		return
	}
	if items == nil {
		items = []domain.PromptVersion{}
	}
	writeJSON(w, http.StatusOK, map[string]any{"versions": items})
}

func (s *Server) createPromptVersion(w http.ResponseWriter, r *http.Request) {
	p := principalFrom(r)
	var input domain.PromptVersion
	if err := decodeJSON(w, r, &input); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_json", err.Error())
		return
	}
	input.PromptID = r.PathValue("id")
	out, err := s.store.CreatePromptVersion(r.Context(), p, input)
	if errors.Is(err, postgres.ErrNotFound) {
		writeError(w, http.StatusNotFound, "not_found", "prompt not found")
		return
	}
	if err != nil {
		writeError(w, http.StatusUnprocessableEntity, "invalid_prompt_version", err.Error())
		return
	}
	writeJSON(w, http.StatusCreated, out)
}

func (s *Server) resolvePromptVersion(ctx context.Context, p domain.Principal, promptID, vid string) (domain.PromptVersion, error) {
	if num, err := strconv.Atoi(vid); err == nil {
		return s.store.GetPromptVersionByNumber(ctx, p, promptID, num)
	}
	return s.store.GetPromptVersion(ctx, p, vid)
}

func (s *Server) getPromptVersion(w http.ResponseWriter, r *http.Request) {
	p := principalFrom(r)
	promptID := r.PathValue("id")
	vid := r.PathValue("vid")
	out, err := s.resolvePromptVersion(r.Context(), p, promptID, vid)
	if errors.Is(err, postgres.ErrNotFound) {
		writeError(w, http.StatusNotFound, "not_found", "prompt version not found")
		return
	}
	if err != nil {
		internalError(w, err, "get prompt version", p)
		return
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) setPromptVersionEvalStatus(w http.ResponseWriter, r *http.Request) {
	p := principalFrom(r)
	promptID := r.PathValue("id")
	vid := r.PathValue("vid")

	pv, err := s.resolvePromptVersion(r.Context(), p, promptID, vid)
	if errors.Is(err, postgres.ErrNotFound) {
		writeError(w, http.StatusNotFound, "not_found", "prompt version not found")
		return
	}
	if err != nil {
		internalError(w, err, "resolve prompt version for eval", p)
		return
	}

	var input struct {
		EvalStatus string `json:"eval_status"`
	}
	if err := decodeJSON(w, r, &input); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_json", err.Error())
		return
	}
	if strings.TrimSpace(input.EvalStatus) == "" {
		writeError(w, http.StatusBadRequest, "invalid_eval_status", "eval_status is required")
		return
	}

	if err := s.store.SetPromptVersionEvalStatus(r.Context(), p, pv.ID, input.EvalStatus); err != nil {
		writeError(w, http.StatusUnprocessableEntity, "invalid_eval_status", err.Error())
		return
	}

	updated, err := s.store.GetPromptVersion(r.Context(), p, pv.ID)
	if err != nil {
		internalError(w, err, "fetch updated prompt version", p)
		return
	}
	writeJSON(w, http.StatusOK, updated)
}

func (s *Server) publishPromptVersion(w http.ResponseWriter, r *http.Request) {
	p := principalFrom(r)
	if p.ActorType != "user" || p.UserID == "" {
		writeError(w, http.StatusForbidden, "human_approval_required", "prompt publish requires an authenticated user")
		return
	}

	promptID := r.PathValue("id")
	vid := r.PathValue("vid")

	pv, err := s.resolvePromptVersion(r.Context(), p, promptID, vid)
	if errors.Is(err, postgres.ErrNotFound) {
		writeError(w, http.StatusNotFound, "not_found", "prompt version not found")
		return
	}
	if err != nil {
		internalError(w, err, "resolve prompt version for publish", p)
		return
	}

	var blobStore ports.BlobStore = s.blobStore
	if blobStore == nil {
		blobStore = noopBlobStore{}
	}

	out, err := prompts.Publish(r.Context(), s.store, blobStore, p, promptID, pv.Version, p.UserID)
	if errors.Is(err, postgres.ErrNotFound) {
		writeError(w, http.StatusNotFound, "not_found", "prompt version not found")
		return
	}
	if errors.Is(err, prompts.ErrApproverRequired) {
		writeError(w, http.StatusForbidden, "human_approval_required", err.Error())
		return
	}
	if err != nil {
		writeError(w, http.StatusUnprocessableEntity, "publish_failed", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, out)
}
