package httpapi

import (
	"errors"
	"net/http"
	"strings"

	"github.com/buildwithdmytro/openjourney/internal/postgres"
)

func (s *Server) listSuppressions(w http.ResponseWriter, r *http.Request) {
	principal := principalFrom(r)
	res, err := s.store.ListSuppressions(r.Context(), principal)
	if err != nil {
		internalError(w, err, "list suppressions", principal)
		return
	}
	writeJSON(w, http.StatusOK, res)
}

func (s *Server) createSuppression(w http.ResponseWriter, r *http.Request) {
	principal := principalFrom(r)
	var input struct {
		Channel  string `json:"channel"`
		Endpoint string `json:"endpoint"`
		Reason   string `json:"reason"`
	}
	if err := decodeJSON(w, r, &input); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_json", err.Error())
		return
	}
	if strings.TrimSpace(input.Channel) == "" {
		writeError(w, http.StatusBadRequest, "bad_request", "channel is required")
		return
	}
	if strings.TrimSpace(input.Endpoint) == "" {
		writeError(w, http.StatusBadRequest, "bad_request", "endpoint is required")
		return
	}
	reason := strings.ToLower(strings.TrimSpace(input.Reason))
	if reason == "" {
		reason = "admin"
	}
	if reason != "bounce" && reason != "complaint" && reason != "unsubscribe" && reason != "admin" {
		writeError(w, http.StatusBadRequest, "bad_request", "reason must be bounce, complaint, unsubscribe, or admin")
		return
	}

	err := s.store.SuppressEndpoint(r.Context(), principal, input.Channel, input.Endpoint, reason)
	if err != nil {
		internalError(w, err, "create suppression", principal)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "suppressed"})
}

func (s *Server) deleteSuppression(w http.ResponseWriter, r *http.Request) {
	principal := principalFrom(r)
	channel := r.URL.Query().Get("channel")
	endpoint := r.URL.Query().Get("endpoint")
	if channel == "" || endpoint == "" {
		writeError(w, http.StatusBadRequest, "bad_request", "channel and endpoint query parameters are required")
		return
	}

	err := s.store.RemoveSuppression(r.Context(), principal, channel, endpoint)
	if errors.Is(err, postgres.ErrNotFound) {
		writeError(w, http.StatusNotFound, "not_found", "suppression not found")
		return
	}
	if err != nil {
		internalError(w, err, "delete suppression", principal)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
