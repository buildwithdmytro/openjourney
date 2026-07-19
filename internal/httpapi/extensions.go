package httpapi

import (
	"encoding/json"
	"net/http"
	"strconv"

	"github.com/buildwithdmytro/openjourney/internal/domain"
	"github.com/buildwithdmytro/openjourney/internal/extension"
)

func (s *Server) listExtensions(w http.ResponseWriter, r *http.Request) {
	items, err := s.store.ListExtensions(r.Context(), principalFrom(r))
	if err != nil {
		internalError(w, err, "list extensions", principalFrom(r))
		return
	}
	if items == nil {
		items = []domain.Extension{}
	}
	writeJSON(w, http.StatusOK, map[string]any{"extensions": items})
}

type installExtensionRequest struct {
	Name            string          `json:"name"`
	Publisher       string          `json:"publisher"`
	Version         int             `json:"version"`
	Kind            string          `json:"kind"`
	Transport       string          `json:"transport"`
	Manifest        json.RawMessage `json:"manifest"`
	RequestedScopes []string        `json:"requested_scopes"`
	Signature       string          `json:"signature"`
	SigningKeyID    string          `json:"signing_key_id"`
}

func (s *Server) installExtension(w http.ResponseWriter, r *http.Request) {
	p := principalFrom(r)
	var in installExtensionRequest
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		http.Error(w, "invalid extension manifest", http.StatusBadRequest)
		return
	}
	if p.ActorType != "user" || p.UserID == "" {
		http.Error(w, "human actor required", http.StatusForbidden)
		return
	}
	ext, err := s.store.CreateExtension(r.Context(), p, domain.Extension{Name: in.Name, Publisher: in.Publisher})
	if err != nil {
		internalError(w, err, "create extension", p)
		return
	}
	if in.Version < 1 {
		in.Version = 1
	}
	ev, err := s.store.CreateExtensionVersion(r.Context(), p, domain.ExtensionVersion{ExtensionID: ext.ID, Version: in.Version, Kind: in.Kind, Transport: in.Transport, Manifest: in.Manifest, RequestedScopes: in.RequestedScopes, Signature: in.Signature, SigningKeyID: in.SigningKeyID})
	if err != nil {
		internalError(w, err, "create extension version", p)
		return
	}
	if s.blobStore == nil {
		http.Error(w, "blob store unavailable", http.StatusServiceUnavailable)
		return
	}
	installed, err := extension.Publish(r.Context(), s.store, s.blobStore, p, ext.ID, ev.Version, p.UserID)
	if err != nil {
		internalError(w, err, "verify and install extension", p)
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{"extension": ext, "version": installed})
}

func (s *Server) updateExtension(w http.ResponseWriter, r *http.Request) {
	p := principalFrom(r)
	var in domain.Extension
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		http.Error(w, "invalid extension", http.StatusBadRequest)
		return
	}
	in.ID = r.PathValue("id")
	if in.Status == "enabled" && (p.ActorType != "user" || p.UserID == "") {
		writeError(w, http.StatusForbidden, "human_approval_required", "enabling an extension requires an authenticated user")
		return
	}
	out, err := s.store.UpdateExtension(r.Context(), p, in)
	if err != nil {
		internalError(w, err, "update extension", p)
		return
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) getExtensionConfig(w http.ResponseWriter, r *http.Request) {
	p := principalFrom(r)
	out, err := s.store.GetExtensionConfig(r.Context(), p, r.PathValue("id"))
	if err != nil {
		internalError(w, err, "get extension config", p)
		return
	}
	writeJSON(w, http.StatusOK, out)
}
func (s *Server) upsertExtensionConfig(w http.ResponseWriter, r *http.Request) {
	p := principalFrom(r)
	var in domain.ExtensionConfig
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		http.Error(w, "invalid extension config", http.StatusBadRequest)
		return
	}
	in.ExtensionID = r.PathValue("id")
	out, err := s.store.UpsertExtensionConfig(r.Context(), p, in)
	if err != nil {
		internalError(w, err, "save extension config", p)
		return
	}
	writeJSON(w, http.StatusOK, out)
}
func (s *Server) listExtensionGrants(w http.ResponseWriter, r *http.Request) {
	p := principalFrom(r)
	out, err := s.store.ListExtensionGrants(r.Context(), p, r.PathValue("id"))
	if err != nil {
		internalError(w, err, "list extension grants", p)
		return
	}
	if out == nil {
		out = []domain.ExtensionGrant{}
	}
	writeJSON(w, http.StatusOK, map[string]any{"grants": out})
}
func (s *Server) createExtensionGrant(w http.ResponseWriter, r *http.Request) {
	p := principalFrom(r)
	var in domain.ExtensionGrant
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		http.Error(w, "invalid extension grant", http.StatusBadRequest)
		return
	}
	in.ExtensionID = r.PathValue("id")
	in.GrantedBy = p.UserID
	out, err := s.store.CreateExtensionGrant(r.Context(), p, in)
	if err != nil {
		internalError(w, err, "create extension grant", p)
		return
	}
	writeJSON(w, http.StatusCreated, out)
}
func (s *Server) deleteExtensionGrant(w http.ResponseWriter, r *http.Request) {
	p := principalFrom(r)
	if err := s.store.DeleteExtensionGrant(r.Context(), p, r.PathValue("id"), r.PathValue("scope")); err != nil {
		internalError(w, err, "delete extension grant", p)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// listExtensionActivity returns the immutable invocation audit together with
// the current operational health state. The store applies both tenant and
// workspace predicates to the activity query.
func (s *Server) listExtensionActivity(w http.ResponseWriter, r *http.Request) {
	principal := principalFrom(r)
	limit := 100
	if raw := r.URL.Query().Get("limit"); raw != "" {
		if parsed, err := strconv.Atoi(raw); err == nil {
			limit = parsed
		}
	}

	extensionID := r.PathValue("id")
	activities, err := s.store.ListExtensionActivities(r.Context(), principal, extensionID, limit)
	if err != nil {
		internalError(w, err, "list extension activity", principal)
		return
	}
	health, err := s.store.GetExtensionHealth(r.Context(), principal, extensionID)
	if err != nil {
		internalError(w, err, "get extension health", principal)
		return
	}
	if activities == nil {
		activities = []domain.ExtensionActivity{}
	}
	writeJSON(w, http.StatusOK, map[string]any{"activities": activities, "health": health})
}
