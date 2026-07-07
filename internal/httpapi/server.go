package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/buildwithdmytro/openjourney/internal/domain"
	"github.com/buildwithdmytro/openjourney/internal/ports"
	"github.com/buildwithdmytro/openjourney/internal/postgres"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"
)

type principalKey struct{}

type Server struct {
	store              ports.Store
	maxBatchSize       int
	tokenVerifier      ports.TokenVerifier
	corsAllowedOrigin  string
	sessionTTL         time.Duration
	trackingSecretKey  []byte
	trackingBaseURL    string
}

func New(store ports.Store, maxBatchSize int) http.Handler {
	return NewWithOptions(store, maxBatchSize, nil, "http://localhost:3000")
}

func NewWithOptions(store ports.Store, maxBatchSize int, verifier ports.TokenVerifier, corsAllowedOrigin string) http.Handler {
	return NewWithSessionTTL(store, maxBatchSize, verifier, corsAllowedOrigin, 12*time.Hour)
}

func NewWithSessionTTL(store ports.Store, maxBatchSize int, verifier ports.TokenVerifier, corsAllowedOrigin string, sessionTTL time.Duration, opts ...func(*Server)) http.Handler {
	s := &Server{
		store: store, maxBatchSize: maxBatchSize,
		tokenVerifier: verifier, corsAllowedOrigin: corsAllowedOrigin, sessionTTL: sessionTTL,
		trackingSecretKey: []byte("change-me-in-production"),
		trackingBaseURL:   "http://localhost:8080",
	}
	for _, opt := range opts {
		opt(s)
	}
	return s.buildMux()
}

// SetTracking sets the HMAC secret key and tracking base URL used by link redirect and open pixel handlers.
func (s *Server) SetTracking(secretKey []byte, baseURL string) {
	s.trackingSecretKey = secretKey
	s.trackingBaseURL = baseURL
}

func (s *Server) buildMux() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /health/live", s.live)
	mux.HandleFunc("GET /health/ready", s.ready)
	mux.Handle("GET /metrics", promhttp.Handler())
	mux.HandleFunc("POST /v1/auth/login", s.login)
	mux.HandleFunc("POST /v1/auth/logout", s.logout)
	mux.Handle("POST /v1/events/batch", s.authenticate("events:write", http.HandlerFunc(s.acceptEvents)))
	mux.Handle("GET /v1/profiles/{externalID}", s.authenticate("profiles:read", http.HandlerFunc(s.getProfile)))
	mux.Handle("GET /v1/schemas", s.authenticate("schemas:read", http.HandlerFunc(s.listSchemas)))
	mux.Handle("POST /v1/schemas", s.authenticate("schemas:write", http.HandlerFunc(s.createSchema)))
	mux.Handle("POST /v1/segments", s.authenticate("segments:write", http.HandlerFunc(s.createSegment)))
	mux.Handle("GET /v1/segments", s.authenticate("segments:read", http.HandlerFunc(s.listSegments)))
	mux.Handle("GET /v1/segments/{id}", s.authenticate("segments:read", http.HandlerFunc(s.getSegment)))
	mux.Handle("PUT /v1/segments/{id}", s.authenticate("segments:write", http.HandlerFunc(s.updateSegment)))
	mux.Handle("PUT /v1/segments/{id}/members", s.authenticate("segments:write", http.HandlerFunc(s.setSegmentMembers)))
	mux.Handle("POST /v1/segments/{id}/preview", s.authenticate("segments:read", http.HandlerFunc(s.previewSegment)))
	mux.Handle("POST /v1/sending-identities", s.authenticate("templates:write", http.HandlerFunc(s.createSendingIdentity)))
	mux.Handle("GET /v1/sending-identities", s.authenticate("templates:read", http.HandlerFunc(s.listSendingIdentities)))
	mux.Handle("GET /v1/sending-identities/{id}", s.authenticate("templates:read", http.HandlerFunc(s.getSendingIdentity)))
	mux.Handle("POST /v1/templates", s.authenticate("templates:write", http.HandlerFunc(s.createTemplate)))
	mux.Handle("GET /v1/templates", s.authenticate("templates:read", http.HandlerFunc(s.listTemplates)))
	mux.Handle("GET /v1/templates/{id}", s.authenticate("templates:read", http.HandlerFunc(s.getTemplate)))
	mux.Handle("PUT /v1/templates/{id}", s.authenticate("templates:write", http.HandlerFunc(s.updateTemplate)))
	mux.Handle("POST /v1/templates/{id}/preview", s.authenticate("templates:read", http.HandlerFunc(s.previewTemplate)))
	mux.HandleFunc("GET /r/{token}", s.redirectLink)
	mux.HandleFunc("GET /o/{token}", s.openPixel)
	mux.Handle("GET /v1/suppressions", s.authenticate("suppressions:read", http.HandlerFunc(s.listSuppressions)))
	mux.Handle("POST /v1/suppressions", s.authenticate("suppressions:write", http.HandlerFunc(s.createSuppression)))
	mux.Handle("DELETE /v1/suppressions", s.authenticate("suppressions:write", http.HandlerFunc(s.deleteSuppression)))
	mux.Handle("GET /v1/api-keys", s.authenticate("api_keys:read", http.HandlerFunc(s.listAPIKeys)))
	mux.Handle("POST /v1/api-keys", s.authenticate("api_keys:write", http.HandlerFunc(s.createAPIKey)))
	mux.Handle("DELETE /v1/api-keys/{id}", s.authenticate("api_keys:write", http.HandlerFunc(s.revokeAPIKey)))
	mux.Handle("POST /v1/privacy/requests", s.authenticate("privacy:write", http.HandlerFunc(s.createPrivacyRequest)))
	mux.Handle("GET /v1/privacy/requests/{id}", s.authenticate("privacy:write", http.HandlerFunc(s.getPrivacyRequest)))
	mux.Handle("GET /v1/operations/queues", s.authenticate("operations:read", http.HandlerFunc(s.queueStatus)))
	mux.Handle("GET /v1/operations/dlq", s.authenticate("operations:read", http.HandlerFunc(s.listDeadLetters)))
	mux.Handle("POST /v1/operations/dlq/{queue}/{id}/retry", s.authenticate("operations:write", http.HandlerFunc(s.retryDeadLetter)))
	mux.Handle("POST /v1/operations/dlq/{queue}/{id}/discard", s.authenticate("operations:write", http.HandlerFunc(s.discardDeadLetter)))
	mux.Handle("POST /v1/operations/replay/verify", s.authenticate("operations:read", http.HandlerFunc(s.verifyReplay)))
	mux.Handle("GET /v1/roles", s.authenticate("roles:read", http.HandlerFunc(s.listRoles)))
	mux.Handle("POST /v1/roles", s.authenticate("roles:write", http.HandlerFunc(s.createRole)))
	mux.Handle("GET /v1/users", s.authenticate("users:read", http.HandlerFunc(s.listUsers)))
	mux.Handle("POST /v1/users", s.authenticate("users:write", http.HandlerFunc(s.createUser)))
	mux.Handle("GET /v1/audit", s.authenticate("operations:read", http.HandlerFunc(s.listAudit)))
	return otelhttp.NewHandler(requestLog(s.cors(mux)), "openjourney-api")
}

func (s *Server) login(w http.ResponseWriter, r *http.Request) {
	var input struct {
		Email    string `json:"email"`
		Password string `json:"password"`
	}
	if err := decodeJSON(w, r, &input); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_json", err.Error())
		return
	}
	session, err := s.store.CreateLocalSession(r.Context(), input.Email, input.Password, s.sessionTTL)
	if errors.Is(err, postgres.ErrUnauthorized) {
		writeError(w, http.StatusUnauthorized, "unauthorized", "email or password is invalid")
		return
	}
	if err != nil {
		slog.Error("local login", "error", err)
		writeError(w, http.StatusInternalServerError, "internal_error", "login could not be completed")
		return
	}
	writeJSON(w, http.StatusOK, session)
}

func (s *Server) logout(w http.ResponseWriter, r *http.Request) {
	rawToken := strings.TrimSpace(strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer "))
	if err := s.store.RevokeLocalSession(r.Context(), rawToken); err != nil {
		slog.Error("local logout", "error", err)
		writeError(w, http.StatusInternalServerError, "internal_error", "logout could not be completed")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) live(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (s *Server) ready(w http.ResponseWriter, r *http.Request) {
	if err := s.store.Ready(r.Context()); err != nil {
		writeError(w, http.StatusServiceUnavailable, "not_ready", "database is unavailable")
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ready"})
}

func (s *Server) authenticate(requiredScope string, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		rawKey := strings.TrimSpace(strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer "))
		if rawKey == "" {
			writeError(w, http.StatusUnauthorized, "unauthorized", "a bearer API key is required")
			return
		}
		principal, err := s.store.Authenticate(r.Context(), rawKey)
		if err != nil && s.tokenVerifier != nil {
			claims, verifyErr := s.tokenVerifier.Verify(r.Context(), rawKey)
			if verifyErr == nil {
				principal, err = s.store.AuthenticateOIDC(r.Context(), claims)
			}
		}
		if err != nil {
			writeError(w, http.StatusUnauthorized, "unauthorized", "the API key is invalid or expired")
			return
		}
		if !principal.HasScope(requiredScope) {
			writeError(w, http.StatusForbidden, "forbidden", "the API key does not have the required scope")
			return
		}
		trace.SpanFromContext(r.Context()).SetAttributes(
			attribute.String("openjourney.tenant_id", principal.TenantID),
			attribute.String("openjourney.workspace_id", principal.WorkspaceID),
			attribute.String("openjourney.app_id", principal.AppID),
			attribute.String("openjourney.actor_type", principal.ActorType),
		)
		next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), principalKey{}, principal)))
	})
}

func (s *Server) acceptEvents(w http.ResponseWriter, r *http.Request) {
	var request struct {
		Events []domain.Event `json:"events"`
	}
	if err := decodeJSON(w, r, &request); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_json", err.Error())
		return
	}
	if len(request.Events) == 0 || len(request.Events) > s.maxBatchSize {
		writeError(w, http.StatusBadRequest, "invalid_batch",
			fmt.Sprintf("events must contain between 1 and %d items", s.maxBatchSize))
		return
	}
	now := time.Now()
	for index, event := range request.Events {
		if err := event.Validate(now); err != nil {
			writeError(w, http.StatusUnprocessableEntity, "invalid_event",
				fmt.Sprintf("events[%d]: %s", index, err))
			return
		}
		principal := r.Context().Value(principalKey{}).(domain.Principal)
		if err := s.store.ValidateEventSchema(r.Context(), principal, event); err != nil {
			writeError(w, http.StatusUnprocessableEntity, "schema_validation_failed",
				fmt.Sprintf("events[%d]: %s", index, err))
			return
		}
	}
	principal := r.Context().Value(principalKey{}).(domain.Principal)
	ids, err := s.store.AcceptEvents(r.Context(), principal, request.Events)
	if errors.Is(err, postgres.ErrQuotaExceeded) {
		writeError(w, http.StatusTooManyRequests, "quota_exceeded", err.Error())
		return
	}
	if errors.Is(err, postgres.ErrIdempotencyConflict) {
		writeError(w, http.StatusConflict, "idempotency_conflict", err.Error())
		return
	}
	if err != nil {
		slog.Error("accept events", "error", err, "tenant_id", principal.TenantID)
		writeError(w, http.StatusInternalServerError, "internal_error", "events could not be accepted")
		return
	}
	writeJSON(w, http.StatusAccepted, map[string]any{
		"status":    "accepted",
		"event_ids": ids,
	})
}

func (s *Server) getProfile(w http.ResponseWriter, r *http.Request) {
	principal := r.Context().Value(principalKey{}).(domain.Principal)
	profile, consents, err := s.store.GetProfile(r.Context(), principal, r.PathValue("externalID"))
	if errors.Is(err, postgres.ErrNotFound) {
		writeError(w, http.StatusNotFound, "not_found", "profile was not found")
		return
	}
	if err != nil {
		slog.Error("get profile", "error", err, "tenant_id", principal.TenantID)
		writeError(w, http.StatusInternalServerError, "internal_error", "profile could not be loaded")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"profile": profile, "consents": consents})
}

func (s *Server) listSchemas(w http.ResponseWriter, r *http.Request) {
	principal := principalFrom(r)
	items, err := s.store.ListEventSchemas(r.Context(), principal)
	if err != nil {
		internalError(w, err, "list schemas", principal)
		return
	}
	if items == nil {
		items = []domain.EventSchema{}
	}
	writeJSON(w, http.StatusOK, map[string]any{"schemas": items})
}

func (s *Server) createSchema(w http.ResponseWriter, r *http.Request) {
	var input domain.EventSchema
	if err := decodeJSON(w, r, &input); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_json", err.Error())
		return
	}
	principal := principalFrom(r)
	item, err := s.store.CreateEventSchema(r.Context(), principal, input)
	if err != nil {
		writeError(w, http.StatusUnprocessableEntity, "invalid_schema", err.Error())
		return
	}
	writeJSON(w, http.StatusCreated, item)
}

func (s *Server) listAPIKeys(w http.ResponseWriter, r *http.Request) {
	principal := principalFrom(r)
	items, err := s.store.ListAPIKeys(r.Context(), principal)
	if err != nil {
		internalError(w, err, "list API keys", principal)
		return
	}
	if items == nil {
		items = []domain.APIKey{}
	}
	writeJSON(w, http.StatusOK, map[string]any{"api_keys": items})
}

func (s *Server) createAPIKey(w http.ResponseWriter, r *http.Request) {
	var input struct {
		Name      string     `json:"name"`
		Scopes    []string   `json:"scopes"`
		ExpiresAt *time.Time `json:"expires_at"`
	}
	if err := decodeJSON(w, r, &input); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_json", err.Error())
		return
	}
	principal := principalFrom(r)
	item, secret, err := s.store.CreateAPIKey(r.Context(), principal, input.Name, input.Scopes, input.ExpiresAt)
	if err != nil {
		writeError(w, http.StatusUnprocessableEntity, "invalid_api_key", err.Error())
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{"api_key": item, "secret": secret})
}

func (s *Server) revokeAPIKey(w http.ResponseWriter, r *http.Request) {
	principal := principalFrom(r)
	err := s.store.RevokeAPIKey(r.Context(), principal, r.PathValue("id"))
	if errors.Is(err, postgres.ErrNotFound) {
		writeError(w, http.StatusNotFound, "not_found", "API key was not found")
		return
	}
	if err != nil {
		internalError(w, err, "revoke API key", principal)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) createPrivacyRequest(w http.ResponseWriter, r *http.Request) {
	var input struct {
		ExternalID  string `json:"external_id"`
		RequestType string `json:"request_type"`
	}
	if err := decodeJSON(w, r, &input); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_json", err.Error())
		return
	}
	principal := principalFrom(r)
	item, err := s.store.CreatePrivacyRequest(r.Context(), principal, input.ExternalID, input.RequestType)
	if err != nil {
		writeError(w, http.StatusUnprocessableEntity, "invalid_privacy_request", err.Error())
		return
	}
	writeJSON(w, http.StatusAccepted, item)
}

func (s *Server) getPrivacyRequest(w http.ResponseWriter, r *http.Request) {
	principal := principalFrom(r)
	item, err := s.store.GetPrivacyRequest(r.Context(), principal, r.PathValue("id"))
	if errors.Is(err, postgres.ErrNotFound) {
		writeError(w, http.StatusNotFound, "not_found", "privacy request was not found")
		return
	}
	if err != nil {
		internalError(w, err, "get privacy request", principal)
		return
	}
	writeJSON(w, http.StatusOK, item)
}

func (s *Server) queueStatus(w http.ResponseWriter, r *http.Request) {
	principal := principalFrom(r)
	items, err := s.store.QueueStatus(r.Context(), principal)
	if err != nil {
		internalError(w, err, "queue status", principal)
		return
	}
	if items == nil {
		items = []domain.QueueStatus{}
	}
	writeJSON(w, http.StatusOK, map[string]any{"queues": items})
}

func (s *Server) listDeadLetters(w http.ResponseWriter, r *http.Request) {
	principal := principalFrom(r)
	limit := 100
	if raw := r.URL.Query().Get("limit"); raw != "" {
		if parsed, err := strconv.Atoi(raw); err == nil {
			limit = parsed
		}
	}
	items, err := s.store.ListDeadLetters(r.Context(), principal, r.URL.Query().Get("queue"), limit)
	if err != nil {
		writeError(w, http.StatusUnprocessableEntity, "invalid_dlq_query", err.Error())
		return
	}
	if items == nil {
		items = []domain.DeadLetterItem{}
	}
	writeJSON(w, http.StatusOK, map[string]any{"dead_letters": items})
}

func (s *Server) retryDeadLetter(w http.ResponseWriter, r *http.Request) {
	principal := principalFrom(r)
	err := s.store.RetryDeadLetter(r.Context(), principal, r.PathValue("queue"), r.PathValue("id"))
	if errors.Is(err, postgres.ErrNotFound) {
		writeError(w, http.StatusNotFound, "not_found", "dead-letter item was not found")
		return
	}
	if err != nil {
		writeError(w, http.StatusUnprocessableEntity, "invalid_dlq_action", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "retry_scheduled"})
}

func (s *Server) discardDeadLetter(w http.ResponseWriter, r *http.Request) {
	principal := principalFrom(r)
	err := s.store.DiscardDeadLetter(r.Context(), principal, r.PathValue("queue"), r.PathValue("id"))
	if errors.Is(err, postgres.ErrNotFound) {
		writeError(w, http.StatusNotFound, "not_found", "dead-letter item was not found")
		return
	}
	if err != nil {
		writeError(w, http.StatusUnprocessableEntity, "invalid_dlq_action", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "discarded"})
}

func (s *Server) verifyReplay(w http.ResponseWriter, r *http.Request) {
	principal := principalFrom(r)
	report, err := s.store.VerifyReplay(r.Context(), principal)
	if err != nil {
		internalError(w, err, "verify replay", principal)
		return
	}
	status := http.StatusOK
	if !report.Match {
		status = http.StatusConflict
	}
	writeJSON(w, status, report)
}

func (s *Server) listRoles(w http.ResponseWriter, r *http.Request) {
	principal := principalFrom(r)
	items, err := s.store.ListRoles(r.Context(), principal)
	if err != nil {
		internalError(w, err, "list roles", principal)
		return
	}
	if items == nil {
		items = []domain.Role{}
	}
	writeJSON(w, http.StatusOK, map[string]any{"roles": items})
}

func (s *Server) createRole(w http.ResponseWriter, r *http.Request) {
	var input struct {
		Name        string   `json:"name"`
		Permissions []string `json:"permissions"`
	}
	if err := decodeJSON(w, r, &input); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_json", err.Error())
		return
	}
	principal := principalFrom(r)
	item, err := s.store.CreateRole(r.Context(), principal, input.Name, input.Permissions)
	if err != nil {
		writeError(w, http.StatusUnprocessableEntity, "invalid_role", err.Error())
		return
	}
	writeJSON(w, http.StatusCreated, item)
}

func (s *Server) listUsers(w http.ResponseWriter, r *http.Request) {
	principal := principalFrom(r)
	items, err := s.store.ListUsers(r.Context(), principal)
	if err != nil {
		internalError(w, err, "list users", principal)
		return
	}
	if items == nil {
		items = []domain.User{}
	}
	writeJSON(w, http.StatusOK, map[string]any{"users": items})
}

func (s *Server) createUser(w http.ResponseWriter, r *http.Request) {
	var input domain.User
	if err := decodeJSON(w, r, &input); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_json", err.Error())
		return
	}
	principal := principalFrom(r)
	item, err := s.store.CreateUser(r.Context(), principal, input)
	if err != nil {
		writeError(w, http.StatusUnprocessableEntity, "invalid_user", err.Error())
		return
	}
	writeJSON(w, http.StatusCreated, item)
}

func (s *Server) listAudit(w http.ResponseWriter, r *http.Request) {
	principal := principalFrom(r)
	limit := 100
	if raw := r.URL.Query().Get("limit"); raw != "" {
		if parsed, err := strconv.Atoi(raw); err == nil {
			limit = parsed
		}
	}
	items, err := s.store.ListAuditEvents(r.Context(), principal, limit)
	if err != nil {
		internalError(w, err, "list audit events", principal)
		return
	}
	if items == nil {
		items = []domain.AuditEvent{}
	}
	writeJSON(w, http.StatusOK, map[string]any{"audit_events": items})
}

func principalFrom(r *http.Request) domain.Principal {
	return r.Context().Value(principalKey{}).(domain.Principal)
}

func internalError(w http.ResponseWriter, err error, operation string, principal domain.Principal) {
	slog.Error(operation, "error", err, "tenant_id", principal.TenantID)
	writeError(w, http.StatusInternalServerError, "internal_error", "the operation could not be completed")
}

func decodeJSON(w http.ResponseWriter, r *http.Request, target any) error {
	r.Body = http.MaxBytesReader(w, r.Body, 1<<20)
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		return err
	}
	if decoder.Decode(&struct{}{}) == nil {
		return errors.New("request body must contain one JSON value")
	}
	return nil
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}

func writeError(w http.ResponseWriter, status int, code, message string) {
	writeJSON(w, status, map[string]any{
		"error": map[string]string{"code": code, "message": message},
	})
}

func (s *Server) cors(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		origin := r.Header.Get("Origin")
		if origin != "" && (s.corsAllowedOrigin == "*" || origin == s.corsAllowedOrigin) {
			w.Header().Set("Access-Control-Allow-Origin", origin)
			w.Header().Set("Vary", "Origin")
		}
		w.Header().Set("Access-Control-Allow-Headers", "Authorization, Content-Type")
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, DELETE, OPTIONS")
		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		next.ServeHTTP(w, r)
	})
}

type responseRecorder struct {
	http.ResponseWriter
	status int
}

func (r *responseRecorder) WriteHeader(status int) {
	r.status = status
	r.ResponseWriter.WriteHeader(status)
}

func requestLog(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		started := time.Now()
		recorder := &responseRecorder{ResponseWriter: w, status: http.StatusOK}
		next.ServeHTTP(recorder, r)
		slog.Info("http request", "method", r.Method, "path", r.URL.Path,
			"status", recorder.status, "duration_ms", time.Since(started).Milliseconds())
	})
}
