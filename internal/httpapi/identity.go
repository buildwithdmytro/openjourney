package httpapi

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"time"

	"github.com/buildwithdmytro/openjourney/internal/domain"
)

type identityCommand struct {
	ExternalID       string         `json:"external_id,omitempty"`
	AnonymousID      string         `json:"anonymous_id,omitempty"`
	IdempotencyKey   string         `json:"idempotency_key,omitempty"`
	Namespace        string         `json:"namespace,omitempty"`
	Value            string         `json:"value,omitempty"`
	Identities       map[string]any `json:"identities,omitempty"`
	Attributes       map[string]any `json:"attributes,omitempty"`
	SourceExternalID string         `json:"source_external_id,omitempty"`
	MergeID          string         `json:"merge_id,omitempty"`
	SourceProfileID  string         `json:"source_profile_id,omitempty"`
}

func (s *Server) identifyIdentity(w http.ResponseWriter, r *http.Request) {
	var input identityCommand
	if err := decodeJSON(w, r, &input); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_json", err.Error())
		return
	}
	if (input.Namespace == "" || input.Value == "") && len(input.Identities) == 0 {
		writeError(w, http.StatusBadRequest, "invalid_identity", "namespace and value or identities are required")
		return
	}
	if input.ExternalID == "" && input.AnonymousID == "" {
		writeError(w, http.StatusBadRequest, "invalid_identity", "external_id or anonymous_id is required")
		return
	}
	payload := map[string]any{}
	if input.Namespace != "" && input.Value != "" {
		payload["namespace"] = input.Namespace
		payload["value"] = input.Value
	}
	if input.Identities != nil {
		payload["identities"] = input.Identities
	}
	s.returnIdentityEvent(w, r, "identity.alias", input, payload, input.ExternalID)
}

func (s *Server) mergeIdentity(w http.ResponseWriter, r *http.Request) {
	var input identityCommand
	if err := decodeJSON(w, r, &input); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_json", err.Error())
		return
	}
	if input.ExternalID == "" || input.SourceExternalID == "" {
		writeError(w, http.StatusBadRequest, "invalid_identity", "external_id and source_external_id are required")
		return
	}
	if !isHuman(principalFrom(r)) {
		writeError(w, http.StatusForbidden, "human_approval_required", "identity merge requires an authenticated user")
		return
	}
	s.returnIdentityEvent(w, r, "identity.merge", input, map[string]any{"source_external_id": input.SourceExternalID}, input.ExternalID)
}

func (s *Server) unmergeIdentity(w http.ResponseWriter, r *http.Request) {
	var input identityCommand
	if err := decodeJSON(w, r, &input); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_json", err.Error())
		return
	}
	if input.MergeID == "" && input.SourceProfileID == "" {
		writeError(w, http.StatusBadRequest, "invalid_identity", "merge_id or source_profile_id is required")
		return
	}
	if !isHuman(principalFrom(r)) {
		writeError(w, http.StatusForbidden, "human_approval_required", "identity unmerge requires an authenticated user")
		return
	}
	s.returnIdentityEvent(w, r, "identity.unmerge", input, map[string]any{"merge_id": input.MergeID, "source_profile_id": input.SourceProfileID}, input.SourceProfileID)
}

func isHuman(p domain.Principal) bool { return p.ActorType == "user" && p.UserID != "" }

func (s *Server) returnIdentityEvent(w http.ResponseWriter, r *http.Request, eventType string, input identityCommand, payload map[string]any, externalID string) {
	data, err := json.Marshal(payload)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_identity", err.Error())
		return
	}
	key := input.IdempotencyKey
	if key == "" {
		sum := sha256.Sum256(data)
		key = "identity:" + eventType + ":" + hex.EncodeToString(sum[:])
	}
	event := domain.Event{Type: eventType, SchemaVersion: 1, ExternalID: externalID, AnonymousID: input.AnonymousID, IdempotencyKey: key, OccurredAt: time.Now().UTC(), Source: "api", Payload: data}
	if err := event.Validate(time.Now().UTC()); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_identity", err.Error())
		return
	}
	p := principalFrom(r)
	if _, err := s.store.AcceptEvents(r.Context(), p, []domain.Event{event}); err != nil {
		internalError(w, err, "accept identity event", p)
		return
	}
	writeJSON(w, http.StatusAccepted, map[string]any{"status": "accepted", "event_type": eventType, "idempotency_key": key})
}
