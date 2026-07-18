package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/buildwithdmytro/openjourney/internal/domain"
	formdefinition "github.com/buildwithdmytro/openjourney/internal/forms"
)

// submitPublicForm is deliberately registered directly on the mux. The form
// token supplies the resource binding; the form's tenant/app supply the event
// principal, so no visitor credential is accepted or needed.
func (s *Server) submitPublicForm(w http.ResponseWriter, r *http.Request) {
	clientIP := ClientIP(r, s.trustedProxy)
	if s.publicLimiter != nil && !s.publicLimiter.Allow(clientIP) {
		writeError(w, http.StatusTooManyRequests, "rate_limited", "too many submissions")
		return
	}

	var request struct {
		FormToken string         `json:"form_token"`
		Token     string         `json:"token"`
		Honeypot  string         `json:"honeypot"`
		Website   string         `json:"website"`
		Values    map[string]any `json:"values"`
		UTM       map[string]any `json:"utm,omitempty"`
		Captcha   string         `json:"captcha_token,omitempty"`
	}
	if err := decodeJSON(w, r, &request); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_json", err.Error())
		return
	}
	if !HoneypotEmpty(request.Honeypot) || !HoneypotEmpty(request.Website) {
		// Bots get a successful response but never reach the event pipeline.
		w.WriteHeader(http.StatusOK)
		return
	}
	token := strings.TrimSpace(request.FormToken)
	if token == "" {
		token = strings.TrimSpace(request.Token)
	}
	form, version, err := s.publicForm(r, r.PathValue("formId"))
	if err != nil {
		writeError(w, http.StatusNotFound, "not_found", "published form not found")
		return
	}
	appStore, ok := s.store.(interface {
		GetFirstAppID(context.Context, string, string) (string, error)
	})
	if !ok {
		writeError(w, http.StatusServiceUnavailable, "app_unavailable", "form event app is unavailable")
		return
	}
	appID, err := appStore.GetFirstAppID(r.Context(), form.TenantID, form.WorkspaceID)
	if err != nil {
		internalError(w, err, "resolve form app", domain.Principal{TenantID: form.TenantID, WorkspaceID: form.WorkspaceID})
		return
	}
	verified, err := VerifyFormToken(token, form.ID, version.Version, s.trackingSecretKey, time.Now())
	if err != nil {
		writeError(w, http.StatusForbidden, "invalid_form_token", err.Error())
		return
	}
	if s.captchaVerifier != nil {
		if err := s.captchaVerifier.Verify(r.Context(), CaptchaRequest{Token: request.Captcha, RemoteIP: clientIP}); err != nil {
			writeError(w, http.StatusForbidden, "captcha_failed", "captcha verification failed")
			return
		}
	}
	values := request.Values
	if values == nil {
		values = map[string]any{}
	}
	payload, err := json.Marshal(values)
	if err != nil {
		writeError(w, http.StatusUnprocessableEntity, "invalid_values", err.Error())
		return
	}
	if err := formdefinition.ValidateSubmission(version.Definition, payload); err != nil {
		writeError(w, http.StatusUnprocessableEntity, "invalid_values", err.Error())
		return
	}

	attrs := make(map[string]any, len(values)+1)
	for _, field := range formdefinition.FieldsFromDefinition(version.Definition) {
		if value, ok := values[field.Key]; ok && field.MapsTo != "" {
			attrs[field.MapsTo] = value
		}
	}
	for key, value := range request.UTM {
		attrs["utm_"+key] = value
	}
	externalID := ""
	for _, key := range []string{"external_id", "email", "phone"} {
		if value, ok := attrs[key].(string); ok && strings.TrimSpace(value) != "" {
			externalID = value
			break
		}
	}
	if externalID == "" {
		externalID = "form:" + token
	}

	// The token timestamp makes retries deterministic while remaining safely
	// in the past for AcceptEvents' clock validation.
	occurredAt := verified.ExpiresAt.Add(-time.Second).UTC()
	events := []domain.Event{
		{Type: "profile.updated", SchemaVersion: 1, ExternalID: externalID,
			IdempotencyKey: token + ":profile", OccurredAt: occurredAt, Source: "form",
			Payload: mustJSON(map[string]any{"attributes": attrs})},
		{Type: "form.submitted", SchemaVersion: 1, ExternalID: externalID,
			IdempotencyKey: token + ":submitted", OccurredAt: occurredAt, Source: "form",
			Payload: mustJSON(map[string]any{"form_id": form.ID, "form_version": version.Version, "values": values, "utm": request.UTM})},
	}
	for _, field := range formdefinition.FieldsFromDefinition(version.Definition) {
		if !field.Consent || values[field.Key] != true {
			continue
		}
		channel := field.MapsTo
		if channel == "" {
			channel = "email"
		}
		events = append(events, domain.Event{Type: "consent.changed", SchemaVersion: 1, ExternalID: externalID,
			IdempotencyKey: token + ":consent:" + field.Key, OccurredAt: occurredAt, Source: "form",
			Payload: mustJSON(map[string]any{"channel": channel, "state": "subscribed", "topic": "marketing",
				"evidence": map[string]any{"ip": clientIP, "timestamp": time.Now().UTC(), "form_id": form.ID}})})
	}
	principal := domain.Principal{TenantID: form.TenantID, WorkspaceID: form.WorkspaceID, AppID: appID, ActorType: "public"}
	ids, err := s.store.AcceptEvents(r.Context(), principal, events)
	if err != nil {
		internalError(w, err, "submit form", principal)
		return
	}
	if submissionStore, ok := s.store.(interface {
		RecordFormSubmission(context.Context, domain.Principal, string, int, json.RawMessage, json.RawMessage, string) error
	}); ok && len(ids) > 0 {
		utm := mustJSON(request.UTM)
		if err := submissionStore.RecordFormSubmission(r.Context(), principal, form.ID, version.Version, payload, utm, ids[0]); err != nil {
			internalError(w, err, "record form submission", principal)
			return
		}
	}
	w.Header().Set("Content-Type", "application/json")
	writeJSON(w, http.StatusAccepted, map[string]any{"status": "accepted"})
}

func (s *Server) publicForm(r *http.Request, id string) (domain.Form, domain.FormVersion, error) {
	store, ok := s.store.(interface {
		GetPublishedForm(context.Context, string) (domain.Form, domain.FormVersion, error)
	})
	if !ok {
		return domain.Form{}, domain.FormVersion{}, errors.New("public form store unavailable")
	}
	return store.GetPublishedForm(r.Context(), id)
}
