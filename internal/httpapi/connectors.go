package httpapi

import (
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"

	"github.com/buildwithdmytro/openjourney/internal/domain"
	"github.com/buildwithdmytro/openjourney/internal/postgres"
)

func (s *Server) listConnectorPipelines(w http.ResponseWriter, r *http.Request) {
	items, err := s.store.ListConnectorPipelines(r.Context(), principalFrom(r))
	if err != nil {
		internalError(w, err, "list connector pipelines", principalFrom(r))
		return
	}
	if items == nil {
		items = []domain.ConnectorPipeline{}
	}
	writeJSON(w, http.StatusOK, map[string]any{"pipelines": items})
}

func (s *Server) getConnectorPipeline(w http.ResponseWriter, r *http.Request) {
	p := principalFrom(r)
	out, err := s.store.GetConnectorPipeline(r.Context(), p, r.PathValue("id"))
	if errors.Is(err, postgres.ErrNotFound) {
		writeError(w, http.StatusNotFound, "not_found", "connector pipeline not found")
		return
	}
	if err != nil {
		internalError(w, err, "get connector pipeline", p)
		return
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) createConnectorPipeline(w http.ResponseWriter, r *http.Request) {
	p := principalFrom(r)
	var input domain.ConnectorPipeline
	if err := decodeJSON(w, r, &input); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_json", err.Error())
		return
	}
	if input.AppID == "" {
		input.AppID = p.AppID
	}
	out, err := s.store.CreateConnectorPipeline(r.Context(), p, input)
	if err != nil {
		writeError(w, http.StatusUnprocessableEntity, "invalid_pipeline", err.Error())
		return
	}
	writeJSON(w, http.StatusCreated, out)
}

func (s *Server) updateConnectorPipeline(w http.ResponseWriter, r *http.Request) {
	p := principalFrom(r)
	var input domain.ConnectorPipeline
	if err := decodeJSON(w, r, &input); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_json", err.Error())
		return
	}
	if input.Status == "enabled" && (p.ActorType != "user" || p.UserID == "") {
		writeError(w, http.StatusForbidden, "human_approval_required", "enabling requires an authenticated user")
		return
	}
	input.ID = r.PathValue("id")
	if input.AppID == "" {
		input.AppID = p.AppID
	}
	out, err := s.store.UpdateConnectorPipeline(r.Context(), p, input)
	if errors.Is(err, postgres.ErrNotFound) {
		writeError(w, http.StatusNotFound, "not_found", "connector pipeline not found")
		return
	}
	if err != nil {
		writeError(w, http.StatusUnprocessableEntity, "invalid_pipeline", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) publishConnectorPipeline(w http.ResponseWriter, r *http.Request) {
	p := principalFrom(r)
	if p.ActorType != "user" || p.UserID == "" {
		writeError(w, http.StatusForbidden, "human_approval_required", "publishing requires an authenticated user")
		return
	}
	var input struct {
		Mapping json.RawMessage `json:"mapping"`
	}
	if err := decodeJSON(w, r, &input); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_json", err.Error())
		return
	}
	if len(input.Mapping) == 0 {
		writeError(w, http.StatusBadRequest, "invalid_mapping", "mapping is required")
		return
	}
	var value any
	if err := json.Unmarshal(input.Mapping, &value); err != nil {
		writeError(w, http.StatusUnprocessableEntity, "invalid_mapping", err.Error())
		return
	}
	canonical, err := json.Marshal(value)
	if err != nil {
		writeError(w, http.StatusUnprocessableEntity, "invalid_mapping", err.Error())
		return
	}
	sum := sha256.Sum256(canonical)
	manifestKey := fmt.Sprintf("connectors/%s/%s/defs/%x.json", p.TenantID, r.PathValue("id"), sum)
	if s.blobStore == nil {
		writeError(w, http.StatusInternalServerError, "internal_error", "connector pipeline blob store is not configured")
		return
	}
	if err := s.blobStore.Put(r.Context(), manifestKey, canonical, "application/json"); err != nil {
		internalError(w, err, "freeze connector pipeline", p)
		return
	}
	out, err := s.store.PublishConnectorPipeline(r.Context(), p, r.PathValue("id"), p.UserID, manifestKey, canonical, fmt.Sprintf("%x", sum))
	if errors.Is(err, postgres.ErrNotFound) {
		writeError(w, http.StatusNotFound, "not_found", "connector pipeline not found")
		return
	}
	if err != nil {
		internalError(w, err, "publish connector pipeline", p)
		return
	}
	writeJSON(w, http.StatusCreated, out)
}

func (s *Server) replayConnectorRun(w http.ResponseWriter, r *http.Request) {
	jobID, err := s.store.ReplayConnectorRun(r.Context(), principalFrom(r), r.PathValue("id"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "connector_replay_failed", err.Error())
		return
	}
	writeJSON(w, http.StatusAccepted, map[string]string{"job_id": jobID})
}
