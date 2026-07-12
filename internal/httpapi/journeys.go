package httpapi

import (
	"errors"
	"net/http"
	"strconv"

	"github.com/buildwithdmytro/openjourney/internal/domain"
	journeyflow "github.com/buildwithdmytro/openjourney/internal/journey"
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

func (s *Server) publishJourney(w http.ResponseWriter, r *http.Request) {
	principal := principalFrom(r)
	id := r.PathValue("id")
	if principal.ActorType != "user" || principal.UserID == "" {
		writeError(w, http.StatusForbidden, "human_approval_required", "publishing requires an authenticated user")
		return
	}
	var input struct {
		ApproverUserID string `json:"approver_user_id"`
	}
	if r.Body != nil && r.ContentLength != 0 {
		if err := decodeJSON(w, r, &input); err != nil {
			writeError(w, http.StatusBadRequest, "invalid_json", err.Error())
			return
		}
	}
	_ = input.ApproverUserID // accepted for backwards compatibility; never trusted
	version, err := journeyflow.Publish(r.Context(), s.store, s.blobStore, principal, id, principal.UserID)
	if errors.Is(err, postgres.ErrNotFound) {
		writeError(w, http.StatusNotFound, "not_found", "journey not found")
		return
	}
	if errors.Is(err, journeyflow.ErrInvalidGraph) {
		writeError(w, http.StatusUnprocessableEntity, "invalid_graph", err.Error())
		return
	}
	if errors.Is(err, journeyflow.ErrApproverRequired) {
		writeError(w, http.StatusForbidden, "human_approval_required", "publishing requires an authenticated user")
		return
	}
	if err != nil {
		internalError(w, err, "publish journey", principal)
		return
	}
	writeJSON(w, http.StatusCreated, version)
}

func (s *Server) setJourneyVersionStatus(w http.ResponseWriter, r *http.Request) {
	principal := principalFrom(r)
	journeyID := r.PathValue("id")
	versionStr := r.PathValue("v")

	version, err := strconv.Atoi(versionStr)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_version", "version must be an integer")
		return
	}

	var input struct {
		Status string `json:"status"`
	}
	if err := decodeJSON(w, r, &input); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_json", err.Error())
		return
	}

	status := input.Status
	if status != "active" && status != "paused" && status != "archived" {
		writeError(w, http.StatusBadRequest, "invalid_status", "status must be one of: active, paused, archived")
		return
	}

	err = s.store.SetJourneyVersionStatus(r.Context(), principal, journeyID, version, status)
	if errors.Is(err, postgres.ErrNotFound) {
		writeError(w, http.StatusNotFound, "not_found", "journey version not found")
		return
	}
	if err != nil {
		internalError(w, err, "set journey version status", principal)
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{"status": status})
}

func (s *Server) getJourneyVersion(w http.ResponseWriter, r *http.Request) {
	principal := principalFrom(r)
	versionID := r.PathValue("v")
	res, err := s.store.GetJourneyVersion(r.Context(), principal.TenantID, versionID)
	if errors.Is(err, postgres.ErrNotFound) {
		writeError(w, http.StatusNotFound, "not_found", "journey version not found")
		return
	}
	if err != nil {
		internalError(w, err, "get journey version", principal)
		return
	}
	writeJSON(w, http.StatusOK, res)
}

func (s *Server) cancelJourneyRun(w http.ResponseWriter, r *http.Request) {
	principal := principalFrom(r)
	journeyID := r.PathValue("id")
	runID := r.PathValue("runID")

	err := s.store.CancelJourneyRun(r.Context(), principal, journeyID, runID)
	if errors.Is(err, postgres.ErrNotFound) {
		writeError(w, http.StatusNotFound, "not_found", "journey run not found")
		return
	}
	if err != nil {
		internalError(w, err, "cancel journey run", principal)
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{"status": "canceled"})
}

func (s *Server) getJourneyDLQ(w http.ResponseWriter, r *http.Request) {
	principal := principalFrom(r)
	steps, intents, err := s.store.GetJourneyDLQ(r.Context(), principal)
	if err != nil {
		internalError(w, err, "get journey dlq", principal)
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"steps":   steps,
		"intents": intents,
	})
}

func (s *Server) retryJourneyDLQ(w http.ResponseWriter, r *http.Request) {
	principal := principalFrom(r)
	kind := r.PathValue("kind")
	id := r.PathValue("id")

	var err error
	switch kind {
	case "step", "steps":
		err = s.store.RetryJourneyStep(r.Context(), principal, id)
	case "intent", "intents":
		err = s.store.RetryJourneyMessageIntent(r.Context(), principal, id)
	default:
		writeError(w, http.StatusBadRequest, "invalid_kind", "kind must be step or intent")
		return
	}

	if errors.Is(err, postgres.ErrNotFound) {
		writeError(w, http.StatusNotFound, "not_found", "DLQ item not found")
		return
	}
	if err != nil {
		internalError(w, err, "retry DLQ item", principal)
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{"status": "pending"})
}

func (s *Server) backfillJourney(w http.ResponseWriter, r *http.Request) {
	principal := principalFrom(r)
	id := r.PathValue("id")
	var input struct {
		SegmentID      string `json:"segment_id"`
		ApproverUserID string `json:"approver_user_id"`
	}
	if err := decodeJSON(w, r, &input); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_json", err.Error())
		return
	}

	if input.SegmentID == "" {
		writeError(w, http.StatusBadRequest, "invalid_input", "segment_id is required")
		return
	}
	if principal.ActorType != "user" || principal.UserID == "" {
		writeError(w, http.StatusForbidden, "human_approval_required", "backfill requires an authenticated user")
		return
	}
	_ = input.ApproverUserID // accepted for backwards compatibility; never trusted
	count, err := journeyflow.Backfill(r.Context(), s.store, principal, id, input.SegmentID, principal.UserID)
	if errors.Is(err, postgres.ErrNotFound) {
		writeError(w, http.StatusNotFound, "not_found", "journey not found")
		return
	}
	if err != nil {
		internalError(w, err, "backfill journey", principal)
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"enrolled_count": count,
	})
}

func (s *Server) listJourneyRuns(w http.ResponseWriter, r *http.Request) {
	principal := principalFrom(r)
	id := r.PathValue("id")
	res, err := s.store.GetJourneyRuns(r.Context(), principal, id)
	if errors.Is(err, postgres.ErrNotFound) {
		writeError(w, http.StatusNotFound, "not_found", "journey not found")
		return
	}
	if err != nil {
		internalError(w, err, "list journey runs", principal)
		return
	}
	if res == nil {
		res = []domain.JourneyRun{}
	}
	writeJSON(w, http.StatusOK, map[string]any{"runs": res})
}

func (s *Server) listJourneyRunTransitions(w http.ResponseWriter, r *http.Request) {
	principal := principalFrom(r)
	runID := r.PathValue("runID")
	res, err := s.store.GetJourneyTransitions(r.Context(), principal, runID)
	if errors.Is(err, postgres.ErrNotFound) {
		writeError(w, http.StatusNotFound, "not_found", "run transitions not found")
		return
	}
	if err != nil {
		internalError(w, err, "list journey run transitions", principal)
		return
	}
	if res == nil {
		res = []domain.JourneyTransition{}
	}
	writeJSON(w, http.StatusOK, map[string]any{"transitions": res})
}
