package httpapi

import (
	"net/http"

	"github.com/buildwithdmytro/openjourney/internal/domain"
)

type registerDeviceTokenRequest struct {
	ProfileID string `json:"profile_id"`
	Platform  string `json:"platform"`
	Provider  string `json:"provider"`
	Token     string `json:"token"`
}

func (s *Server) registerDeviceToken(w http.ResponseWriter, r *http.Request) {
	var req registerDeviceTokenRequest
	if err := decodeJSON(w, r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_json", err.Error())
		return
	}
	if req.ProfileID == "" || req.Platform == "" || req.Provider == "" || req.Token == "" {
		writeError(w, http.StatusBadRequest, "missing_fields", "profile_id, platform, provider, and token are required")
		return
	}

	principal := r.Context().Value(principalKey{}).(domain.Principal)
	tok, err := s.store.RegisterDeviceToken(r.Context(), principal.TenantID, principal.WorkspaceID, principal.AppID, req.ProfileID, req.Platform, req.Provider, req.Token)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "database_error", err.Error())
		return
	}

	writeJSON(w, http.StatusCreated, tok)
}

func (s *Server) deactivateDeviceToken(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if id == "" {
		writeError(w, http.StatusBadRequest, "missing_id", "device token id is required")
		return
	}

	principal := r.Context().Value(principalKey{}).(domain.Principal)
	err := s.store.RetireDeviceTokenByID(r.Context(), principal.TenantID, id)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "database_error", err.Error())
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

type syncDeviceTokensRequest struct {
	ProfileID string `json:"profile_id"`
	Tokens    []struct {
		Token    string `json:"token"`
		Platform string `json:"platform"`
		Provider string `json:"provider"`
	} `json:"tokens"`
}

func (s *Server) syncDeviceTokens(w http.ResponseWriter, r *http.Request) {
	var req syncDeviceTokensRequest
	if err := decodeJSON(w, r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_json", err.Error())
		return
	}
	if req.ProfileID == "" {
		writeError(w, http.StatusBadRequest, "missing_profile_id", "profile_id is required")
		return
	}

	principal := r.Context().Value(principalKey{}).(domain.Principal)

	// 1. Fetch currently active device tokens
	activeTokens, err := s.store.ListActiveDeviceTokens(r.Context(), principal.TenantID, principal.WorkspaceID, req.ProfileID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "database_error", err.Error())
		return
	}

	// 2. Map existing active tokens by token value
	activeMap := make(map[string]domain.DeviceToken)
	for _, tok := range activeTokens {
		activeMap[tok.Token] = tok
	}

	// 3. Reconcile client's tokens
	clientMap := make(map[string]bool)
	for _, clientTok := range req.Tokens {
		if clientTok.Token == "" || clientTok.Platform == "" || clientTok.Provider == "" {
			writeError(w, http.StatusBadRequest, "invalid_token_payload", "token, platform, and provider are required for all tokens in the list")
			return
		}
		clientMap[clientTok.Token] = true

		// Upsert/refresh the token
		_, err := s.store.RegisterDeviceToken(
			r.Context(),
			principal.TenantID,
			principal.WorkspaceID,
			principal.AppID,
			req.ProfileID,
			clientTok.Platform,
			clientTok.Provider,
			clientTok.Token,
		)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "database_error", err.Error())
			return
		}
	}

	// 4. Retire any active token not present in client list
	for tokenVal := range activeMap {
		if !clientMap[tokenVal] {
			err := s.store.RetireDeviceToken(r.Context(), principal.TenantID, principal.AppID, tokenVal)
			if err != nil {
				writeError(w, http.StatusInternalServerError, "database_error", err.Error())
				return
			}
		}
	}

	// 5. Return the new list of active tokens
	newActiveTokens, err := s.store.ListActiveDeviceTokens(r.Context(), principal.TenantID, principal.WorkspaceID, req.ProfileID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "database_error", err.Error())
		return
	}

	if newActiveTokens == nil {
		newActiveTokens = []domain.DeviceToken{}
	}

	writeJSON(w, http.StatusOK, newActiveTokens)
}
