package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/buildwithdmytro/openjourney/internal/domain"
	"github.com/buildwithdmytro/openjourney/internal/flags"
	"github.com/buildwithdmytro/openjourney/internal/postgres"
	"github.com/buildwithdmytro/openjourney/internal/publishing"
)

func (s *Server) createFeatureFlag(w http.ResponseWriter, r *http.Request) {
	principal := principalFrom(r)
	var flag domain.FeatureFlag
	if err := decodeJSON(w, r, &flag); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_json", err.Error())
		return
	}
	res, err := s.store.CreateFeatureFlag(r.Context(), principal, flag)
	if err != nil {
		internalError(w, err, "create flag", principal)
		return
	}
	writeJSON(w, http.StatusCreated, res)
}

func (s *Server) getFeatureFlag(w http.ResponseWriter, r *http.Request) {
	principal := principalFrom(r)
	id := r.PathValue("id")
	res, err := s.store.GetFeatureFlag(r.Context(), principal, id)
	if errors.Is(err, postgres.ErrNotFound) {
		writeError(w, http.StatusNotFound, "not_found", "flag not found")
		return
	}
	if err != nil {
		internalError(w, err, "get flag", principal)
		return
	}
	writeJSON(w, http.StatusOK, res)
}

func (s *Server) updateFeatureFlag(w http.ResponseWriter, r *http.Request) {
	principal := principalFrom(r)
	id := r.PathValue("id")
	var flag domain.FeatureFlag
	if err := decodeJSON(w, r, &flag); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_json", err.Error())
		return
	}
	flag.ID = id
	res, err := s.store.UpdateFeatureFlag(r.Context(), principal, flag)
	if errors.Is(err, postgres.ErrNotFound) {
		writeError(w, http.StatusNotFound, "not_found", "flag not found")
		return
	}
	if err != nil {
		internalError(w, err, "update flag", principal)
		return
	}
	writeJSON(w, http.StatusOK, res)
}

func (s *Server) listFeatureFlags(w http.ResponseWriter, r *http.Request) {
	principal := principalFrom(r)
	res, err := s.store.ListFeatureFlags(r.Context(), principal)
	if err != nil {
		internalError(w, err, "list flags", principal)
		return
	}
	if res == nil {
		res = []domain.FeatureFlag{}
	}
	writeJSON(w, http.StatusOK, map[string]any{"flags": res})
}

func (s *Server) publishFeatureFlag(w http.ResponseWriter, r *http.Request) {
	principal := principalFrom(r)
	id := r.PathValue("id")
	if !isHuman(principal) {
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
	// approver_user_id in request is for backwards compatibility; always use the authenticated user
	version, err := s.store.PublishFeatureFlag(r.Context(), principal, id, principal.UserID, "")
	if errors.Is(err, postgres.ErrNotFound) {
		writeError(w, http.StatusNotFound, "not_found", "flag not found")
		return
	}
	if errors.Is(err, postgres.ErrSelfApproval) || errors.Is(err, publishing.ErrSelfApproval) {
		writeError(w, http.StatusForbidden, "self_approval_forbidden", err.Error())
		return
	}

	if err != nil {
		internalError(w, err, "publish flag", principal)
		return
	}
	writeJSON(w, http.StatusOK, version)
}

func (s *Server) setFlagStatus(w http.ResponseWriter, r *http.Request) {
	principal := principalFrom(r)
	id := r.PathValue("id")
	if !isHuman(principal) {
		writeError(w, http.StatusForbidden, "human_approval_required", "changing flag status requires an authenticated user")
		return
	}
	var input struct {
		Status string `json:"status"`
	}
	if err := decodeJSON(w, r, &input); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_json", err.Error())
		return
	}
	// Validate status
	validStatuses := map[string]bool{"draft": true, "published": true, "disabled": true}
	if !validStatuses[input.Status] {
		writeError(w, http.StatusBadRequest, "invalid_status", "status must be one of: draft, published, disabled")
		return
	}

	// Get the current flag
	flag, err := s.store.GetFeatureFlag(r.Context(), principal, id)
	if errors.Is(err, postgres.ErrNotFound) {
		writeError(w, http.StatusNotFound, "not_found", "flag not found")
		return
	}
	if err != nil {
		internalError(w, err, "get flag", principal)
		return
	}

	// Update the status
	flag.Status = input.Status
	res, err := s.store.UpdateFeatureFlag(r.Context(), principal, flag)
	if err != nil {
		internalError(w, err, "update flag status", principal)
		return
	}
	writeJSON(w, http.StatusOK, res)
}

// evaluateFlags is registered directly on the mux as a public endpoint.
// It evaluates all active flags for a subject in a given environment.
// Query parameters: tenant, app, environment, anonymous_id (or external_id + token)
func (s *Server) evaluateFlags(w http.ResponseWriter, r *http.Request) {
	clientIP := ClientIP(r, s.trustedProxy)
	if s.publicLimiter != nil && !s.publicLimiter.Allow(clientIP) {
		writeError(w, http.StatusTooManyRequests, "rate_limited", "too many requests")
		return
	}

	// Parse query parameters
	tenantID := strings.TrimSpace(r.URL.Query().Get("tenant"))
	appID := strings.TrimSpace(r.URL.Query().Get("app"))
	environment := strings.TrimSpace(r.URL.Query().Get("environment"))
	if environment == "" {
		environment = "production"
	}
	anonID := strings.TrimSpace(r.URL.Query().Get("anonymous_id"))
	token := strings.TrimSpace(r.URL.Query().Get("token"))
	externalID := strings.TrimSpace(r.URL.Query().Get("external_id"))

	if tenantID == "" || appID == "" {
		writeError(w, http.StatusBadRequest, "missing_context", "tenant and app parameters required")
		return
	}

	// Validate environment
	validEnvs := map[string]bool{"development": true, "staging": true, "production": true}
	if !validEnvs[environment] {
		writeError(w, http.StatusBadRequest, "invalid_environment", "environment must be one of: development, staging, production")
		return
	}

	// Determine the subject and authentication method. byExternalID pins the
	// profile lookup to the SINGLE column this subject was authenticated against,
	// so an untrusted anonymous_id param cannot smuggle a victim's external_id.
	var subject string
	var byExternalID bool

	if token != "" && externalID != "" {
		// Token-authenticated known subject
		subject = externalID
		byExternalID = true
	} else if anonID != "" {
		// Anonymous subject
		subject = anonID
		byExternalID = false
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

	// Fetch the flags: map subject to profile_id, then list active flags
	flagStore, ok := s.store.(interface {
		GetProfileIDBySubject(context.Context, string, string, string, bool) (string, error)
		ListActiveFlags(context.Context, string, string, string) ([]domain.FeatureFlag, error)
		EvaluateAudience(ctx context.Context, p domain.Principal, profileID string, dsl json.RawMessage) (bool, error)
		AcceptEvents(context.Context, domain.Principal, []domain.Event) ([]string, error)
	})
	if !ok {
		writeError(w, http.StatusServiceUnavailable, "store_unavailable", "store is unavailable")
		return
	}

	profileID, err := flagStore.GetProfileIDBySubject(r.Context(), principalTenantID, principalAppID, subject, byExternalID)
	if errors.Is(err, postgres.ErrNotFound) {
		// Return empty flags for unknown profile (no error)
		w.Header().Set("Content-Type", "application/json")
		writeJSON(w, http.StatusOK, map[string]any{"flags": map[string]any{}})
		return
	}
	if err != nil {
		internalError(w, err, "resolve profile", domain.Principal{TenantID: principalTenantID, AppID: principalAppID, ActorType: "public"})
		return
	}

	// Fetch active published flags for this environment
	activeFlags, err := flagStore.ListActiveFlags(r.Context(), principalTenantID, principalAppID, environment)
	if err != nil {
		internalError(w, err, "fetch active flags", domain.Principal{TenantID: principalTenantID, AppID: principalAppID, ActorType: "public"})
		return
	}
	if activeFlags == nil {
		activeFlags = []domain.FeatureFlag{}
	}

	// Evaluate each flag and build the response
	evaluatedFlags := make(map[string]map[string]any)
	eventsToEmit := []domain.Event{}

	for _, flag := range activeFlags {
		// Create an audience evaluator
		evaluator := &storeEvaluator{
			store:      flagStore,
			ctx:        r.Context(),
			principal:  domain.Principal{TenantID: principalTenantID, AppID: principalAppID, ActorType: "public"},
			profileID:  profileID,
		}

		result, err := flags.Evaluate(r.Context(), &flag, profileID, evaluator)
		if err != nil {
			// Log error but don't fail the entire evaluation; skip this flag
			continue
		}

		// Build the evaluated flag response
		evaluatedFlags[flag.Key] = map[string]any{
			"variant": result.Variant,
			"value":   result.Value,
		}

		// Emit an exposure event for this flag
		exposurePayload, _ := json.Marshal(map[string]string{
			"flag_id":     flag.ID,
			"variant":     result.Variant,
			"environment": environment,
		})

		// Idempotency key: flag:version:subject:bucket-window (using hour window)
		hour := time.Now().UTC().Truncate(time.Hour).Unix()
		idempotencyKey := flag.ID + ":" + result.Variant + ":" + subject + ":" + strconv.FormatInt(hour/3600, 10)

		exposureEvent := domain.Event{
			Type:           "feature_flag.exposure",
			SchemaVersion:  1,
			IdempotencyKey: idempotencyKey,
			OccurredAt:     time.Now(),
			Payload:        exposurePayload,
		}

		// Set subject (either external_id or anonymous_id)
		if subject == externalID && externalID != "" {
			exposureEvent.ExternalID = externalID
		} else {
			exposureEvent.AnonymousID = anonID
		}

		eventsToEmit = append(eventsToEmit, exposureEvent)
	}

	// Accept exposure events (best effort; don't fail if it errors)
	if len(eventsToEmit) > 0 {
		principal := domain.Principal{
			TenantID:  principalTenantID,
			AppID:     principalAppID,
			ActorType: "public",
		}
		_, _ = flagStore.AcceptEvents(r.Context(), principal, eventsToEmit)
	}

	w.Header().Set("Content-Type", "application/json")
	writeJSON(w, http.StatusOK, map[string]any{"flags": evaluatedFlags})
}

// storeEvaluator implements the flags.EvalAudience interface by calling store.EvaluateAudience.
type storeEvaluator struct {
	store     interface {
		EvaluateAudience(ctx context.Context, p domain.Principal, profileID string, dsl json.RawMessage) (bool, error)
	}
	ctx       context.Context
	principal domain.Principal
	profileID string
}

func (e *storeEvaluator) Eval(ctx context.Context, profileID string, dsl json.RawMessage) (bool, error) {
	return e.store.EvaluateAudience(ctx, e.principal, profileID, dsl)
}
