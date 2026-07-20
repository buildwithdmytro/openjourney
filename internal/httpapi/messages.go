package httpapi

import (
	"context"
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

	// Placeholder: implement in task 16.3.2
	writeError(w, http.StatusNotImplemented, "not_implemented", "engagement reporting not yet implemented")
}
