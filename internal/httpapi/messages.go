package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/buildwithdmytro/openjourney/internal/domain"
	"github.com/buildwithdmytro/openjourney/internal/postgres"
)

// fetchInbox is registered directly on the mux as a public endpoint.
// It serves the inbox for both anonymous and known subjects.
// Query parameters: tenant, app, anonymous_id (or external_id + token)
func (s *Server) fetchInbox(w http.ResponseWriter, r *http.Request) {
	clientIP := ClientIP(r, s.trustedProxy)
	if s.publicLimiter != nil && !s.publicLimiter.Allow(clientIP) {
		writeError(w, http.StatusTooManyRequests, "rate_limited", "too many requests")
		return
	}

	// Parse query parameters
	tenantID := strings.TrimSpace(r.URL.Query().Get("tenant"))
	appID := strings.TrimSpace(r.URL.Query().Get("app"))
	anonID := strings.TrimSpace(r.URL.Query().Get("anonymous_id"))
	token := strings.TrimSpace(r.URL.Query().Get("token"))
	externalID := strings.TrimSpace(r.URL.Query().Get("external_id"))

	if tenantID == "" || appID == "" {
		writeError(w, http.StatusBadRequest, "missing_context", "tenant and app parameters required")
		return
	}

	// Determine the subject and authentication method
	var subject string

	if token != "" && externalID != "" {
		// Token-authenticated known subject
		subject = externalID
	} else if anonID != "" {
		// Anonymous subject
		subject = anonID
	} else {
		writeError(w, http.StatusBadRequest, "missing_subject", "either anonymous_id or (external_id + token) required")
		return
	}

	principalTenantID := tenantID
	principalAppID := appID

	// If token is provided, verify it
	if token != "" {
		verified, err := VerifyInAppToken(token, principalTenantID, principalAppID, s.trackingSecretKey, time.Now())
		if err == ErrExpiredInAppToken {
			writeError(w, http.StatusForbidden, "expired_token", "token has expired")
			return
		} else if err != nil {
			writeError(w, http.StatusForbidden, "invalid_token", "token is invalid or forged")
			return
		}
		// Verify the token subject matches the requested external_id
		if verified.Subject != externalID {
			writeError(w, http.StatusForbidden, "subject_mismatch", "token subject does not match requested external_id")
			return
		}
	}

	// Fetch the inbox: map subject to profile_id, then list messages
	inboxStore, ok := s.store.(interface {
		GetProfileIDBySubject(context.Context, string, string, string) (string, error)
		ListInboxForProfile(context.Context, string, string, string, int) ([]domain.InAppMessage, error)
	})
	if !ok {
		writeError(w, http.StatusServiceUnavailable, "store_unavailable", "store is unavailable")
		return
	}

	profileID, err := inboxStore.GetProfileIDBySubject(r.Context(), principalTenantID, principalAppID, subject)
	if errors.Is(err, postgres.ErrNotFound) {
		// Return empty inbox for unknown profile (no error)
		w.Header().Set("Content-Type", "application/json")
		writeJSON(w, http.StatusOK, map[string]any{"messages": []domain.InAppMessage{}})
		return
	}
	if err != nil {
		internalError(w, err, "resolve profile", domain.Principal{TenantID: principalTenantID, AppID: principalAppID, ActorType: "public"})
		return
	}

	// Fetch the inbox (limit to 100 for v1)
	messages, err := inboxStore.ListInboxForProfile(r.Context(), principalTenantID, principalAppID, profileID, 100)
	if err != nil {
		internalError(w, err, "fetch inbox", domain.Principal{TenantID: principalTenantID, AppID: principalAppID, ActorType: "public"})
		return
	}
	if messages == nil {
		messages = []domain.InAppMessage{}
	}

	w.Header().Set("Content-Type", "application/json")
	writeJSON(w, http.StatusOK, map[string]any{"messages": messages})
}

// reportMessageEngagement handles impression/click/dismiss reports from the client.
func (s *Server) reportMessageEngagement(w http.ResponseWriter, r *http.Request) {
	clientIP := ClientIP(r, s.trustedProxy)
	if s.publicLimiter != nil && !s.publicLimiter.Allow(clientIP) {
		writeError(w, http.StatusTooManyRequests, "rate_limited", "too many requests")
		return
	}

	// Parse path parameters
	messageID := strings.TrimSpace(r.PathValue("id"))
	action := strings.TrimSpace(r.PathValue("action"))

	// Parse query parameters for authentication
	tenantID := strings.TrimSpace(r.URL.Query().Get("tenant"))
	appID := strings.TrimSpace(r.URL.Query().Get("app"))
	anonID := strings.TrimSpace(r.URL.Query().Get("anonymous_id"))
	token := strings.TrimSpace(r.URL.Query().Get("token"))
	externalID := strings.TrimSpace(r.URL.Query().Get("external_id"))

	// Validate path and query parameters
	if messageID == "" || action == "" {
		writeError(w, http.StatusBadRequest, "missing_params", "message id and action required")
		return
	}
	if tenantID == "" || appID == "" {
		writeError(w, http.StatusBadRequest, "missing_context", "tenant and app parameters required")
		return
	}
	if action != "impression" && action != "click" && action != "dismiss" {
		writeError(w, http.StatusBadRequest, "invalid_action", "action must be impression, click, or dismiss")
		return
	}

	// Determine the subject and authentication method
	var subject string
	if token != "" && externalID != "" {
		subject = externalID
	} else if anonID != "" {
		subject = anonID
	} else {
		writeError(w, http.StatusBadRequest, "missing_subject", "either anonymous_id or (external_id + token) required")
		return
	}

	principalTenantID := tenantID
	principalAppID := appID

	// If token is provided, verify it
	if token != "" {
		verified, err := VerifyInAppToken(token, principalTenantID, principalAppID, s.trackingSecretKey, time.Now())
		if err == ErrExpiredInAppToken {
			writeError(w, http.StatusForbidden, "expired_token", "token has expired")
			return
		} else if err != nil {
			writeError(w, http.StatusForbidden, "invalid_token", "token is invalid or forged")
			return
		}
		if verified.Subject != externalID {
			writeError(w, http.StatusForbidden, "subject_mismatch", "token subject does not match requested external_id")
			return
		}
	}

	// Get the store interface
	msgStore, ok := s.store.(interface {
		GetProfileIDBySubject(context.Context, string, string, string) (string, error)
		GetInAppMessage(context.Context, string, string) (domain.InAppMessage, error)
		AcceptEvents(context.Context, domain.Principal, []domain.Event) ([]string, error)
	})
	if !ok {
		writeError(w, http.StatusServiceUnavailable, "store_unavailable", "store is unavailable")
		return
	}

	// Resolve subject to profile ID
	profileID, err := msgStore.GetProfileIDBySubject(r.Context(), principalTenantID, principalAppID, subject)
	if errors.Is(err, postgres.ErrNotFound) {
		writeError(w, http.StatusForbidden, "subject_not_found", "subject does not exist")
		return
	}
	if err != nil {
		internalError(w, err, "resolve profile", domain.Principal{TenantID: principalTenantID, AppID: principalAppID, ActorType: "public"})
		return
	}

	// Get the message and verify it belongs to this profile
	msg, err := msgStore.GetInAppMessage(r.Context(), principalTenantID, messageID)
	if errors.Is(err, postgres.ErrNotFound) {
		writeError(w, http.StatusNotFound, "message_not_found", "message not found")
		return
	}
	if err != nil {
		internalError(w, err, "fetch message", domain.Principal{TenantID: principalTenantID, AppID: principalAppID, ActorType: "public"})
		return
	}

	// IDOR check: message must belong to the caller's profile
	if msg.ProfileID != profileID {
		writeError(w, http.StatusForbidden, "forbidden", "message does not belong to the caller")
		return
	}

	// Build event type based on action
	var eventType string
	switch action {
	case "impression":
		eventType = "message.impression"
	case "click":
		eventType = "message.clicked"
	case "dismiss":
		eventType = "message.dismissed"
	}

	// Create the event
	eventPayload, _ := json.Marshal(map[string]string{"message_id": msg.ID})
	event := domain.Event{
		Type:           eventType,
		SchemaVersion:  1,
		IdempotencyKey: msg.ID + ":" + action + ":" + subject,
		OccurredAt:     time.Now(),
		Payload:        eventPayload,
	}

	// Set subject (either external_id or anonymous_id)
	if subject == externalID && externalID != "" {
		event.ExternalID = externalID
	} else {
		event.AnonymousID = anonID
	}

	// Accept the event
	principal := domain.Principal{
		TenantID:  principalTenantID,
		AppID:     principalAppID,
		ActorType: "public",
	}
	_, err = msgStore.AcceptEvents(r.Context(), principal, []domain.Event{event})
	if err != nil {
		internalError(w, err, "accept event", principal)
		return
	}

	// Return success
	w.Header().Set("Content-Type", "application/json")
	writeJSON(w, http.StatusOK, map[string]any{"status": "accepted"})
}

// createAdminMessage handles admin creation of in-app messages for broadcast/testing.
func (s *Server) createAdminMessage(w http.ResponseWriter, r *http.Request) {
	principal := principalFrom(r)
	var input domain.InAppMessage
	if err := decodeJSON(w, r, &input); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_json", err.Error())
		return
	}

	// Validate required fields
	if input.AppID == "" || input.ProfileID == "" {
		writeError(w, http.StatusUnprocessableEntity, "validation_error", "app_id and profile_id required")
		return
	}
	if input.MessageType == "" {
		input.MessageType = "modal"
	}
	if input.Content == nil {
		writeError(w, http.StatusUnprocessableEntity, "validation_error", "content required")
		return
	}

	// Set principal's tenant/workspace
	input.TenantID = principal.TenantID
	input.WorkspaceID = principal.WorkspaceID

	// Create the message
	res, err := s.store.(interface {
		CreateInAppMessage(context.Context, string, string, string, string, domain.InAppMessage) (domain.InAppMessage, error)
	}).CreateInAppMessage(r.Context(), principal.TenantID, principal.WorkspaceID, input.AppID, input.ProfileID, input)
	if err != nil {
		internalError(w, err, "create in-app message", principal)
		return
	}
	writeJSON(w, http.StatusCreated, res)
}

// listMessages lists all in-app messages in an app (admin view).
func (s *Server) listMessages(w http.ResponseWriter, r *http.Request) {
	principal := principalFrom(r)
	appID := strings.TrimSpace(r.URL.Query().Get("app_id"))
	if appID == "" {
		writeError(w, http.StatusBadRequest, "missing_params", "app_id query parameter required")
		return
	}

	msgStore, ok := s.store.(interface {
		ListInAppMessages(context.Context, domain.Principal, string) ([]domain.InAppMessage, error)
	})
	if !ok {
		writeError(w, http.StatusServiceUnavailable, "store_unavailable", "store is unavailable")
		return
	}

	res, err := msgStore.ListInAppMessages(r.Context(), principal, appID)
	if err != nil {
		internalError(w, err, "list in-app messages", principal)
		return
	}
	if res == nil {
		res = []domain.InAppMessage{}
	}
	writeJSON(w, http.StatusOK, map[string]any{"messages": res})
}

// getMessage gets a specific in-app message.
func (s *Server) getMessage(w http.ResponseWriter, r *http.Request) {
	principal := principalFrom(r)
	id := r.PathValue("id")
	if id == "" {
		writeError(w, http.StatusBadRequest, "missing_params", "id path parameter required")
		return
	}

	msgStore, ok := s.store.(interface {
		GetInAppMessage(context.Context, string, string) (domain.InAppMessage, error)
	})
	if !ok {
		writeError(w, http.StatusServiceUnavailable, "store_unavailable", "store is unavailable")
		return
	}

	res, err := msgStore.GetInAppMessage(r.Context(), principal.TenantID, id)
	if errors.Is(err, postgres.ErrNotFound) {
		writeError(w, http.StatusNotFound, "not_found", "message not found")
		return
	}
	if err != nil {
		internalError(w, err, "get in-app message", principal)
		return
	}

	// Verify the message belongs to this tenant
	if res.TenantID != principal.TenantID {
		writeError(w, http.StatusForbidden, "forbidden", "message does not belong to this tenant")
		return
	}

	writeJSON(w, http.StatusOK, res)
}

// getProfileInbox lists a profile's inbox (admin view, no filtering).
func (s *Server) getProfileInbox(w http.ResponseWriter, r *http.Request) {
	principal := principalFrom(r)
	profileID := r.PathValue("profileId")
	appID := strings.TrimSpace(r.URL.Query().Get("app_id"))

	if profileID == "" || appID == "" {
		writeError(w, http.StatusBadRequest, "missing_params", "profileId path parameter and app_id query parameter required")
		return
	}

	msgStore, ok := s.store.(interface {
		ListInboxForProfile(context.Context, string, string, string, int) ([]domain.InAppMessage, error)
	})
	if !ok {
		writeError(w, http.StatusServiceUnavailable, "store_unavailable", "store is unavailable")
		return
	}

	res, err := msgStore.ListInboxForProfile(r.Context(), principal.TenantID, appID, profileID, 1000)
	if err != nil {
		internalError(w, err, "get profile inbox", principal)
		return
	}
	if res == nil {
		res = []domain.InAppMessage{}
	}
	writeJSON(w, http.StatusOK, map[string]any{"messages": res})
}
