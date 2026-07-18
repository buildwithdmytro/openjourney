package httpapi

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"time"

	"github.com/buildwithdmytro/openjourney/internal/domain"
	"github.com/buildwithdmytro/openjourney/internal/postgres"
)

func (s *Server) createShortLink(w http.ResponseWriter, r *http.Request) {
	var in domain.ShortLink
	if err := decodeJSON(w, r, &in); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_json", err.Error())
		return
	}
	out, err := s.store.CreateShortLink(r.Context(), principalFrom(r), in)
	if err != nil {
		writeError(w, http.StatusUnprocessableEntity, "invalid_link", err.Error())
		return
	}
	writeJSON(w, http.StatusCreated, out)
}

func (s *Server) listShortLinks(w http.ResponseWriter, r *http.Request) {
	out, err := s.store.ListShortLinks(r.Context(), principalFrom(r))
	if err != nil {
		internalError(w, err, "list short links", principalFrom(r))
		return
	}
	if out == nil {
		out = []domain.ShortLink{}
	}
	writeJSON(w, http.StatusOK, map[string]any{"links": out})
}

func (s *Server) redirectShortLink(w http.ResponseWriter, r *http.Request) {
	store, ok := s.store.(interface {
		GetShortLinkBySlug(context.Context, string) (domain.ShortLink, error)
	})
	if !ok {
		writeError(w, http.StatusServiceUnavailable, "link_store_unavailable", "short-link store is unavailable")
		return
	}
	link, err := store.GetShortLinkBySlug(r.Context(), r.PathValue("slug"))
	if errors.Is(err, postgres.ErrNotFound) {
		http.NotFound(w, r)
		return
	}
	if err != nil {
		writeError(w, http.StatusNotFound, "invalid_link", err.Error())
		return
	}
	destination, err := addUTM(link.DestinationURL, link.UTM)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "invalid_destination", err.Error())
		return
	}
	appStore, ok := s.store.(interface {
		GetFirstAppID(context.Context, string, string) (string, error)
	})
	if ok {
		if appID, appErr := appStore.GetFirstAppID(r.Context(), link.TenantID, link.WorkspaceID); appErr == nil {
			clickID := anonymousClickID(r, link.Slug)
			_, _ = s.store.AcceptEvents(r.Context(), domain.Principal{TenantID: link.TenantID, WorkspaceID: link.WorkspaceID, AppID: appID, ActorType: "public"}, []domain.Event{{
				Type: "link.clicked", SchemaVersion: 1, AnonymousID: clickID,
				IdempotencyKey: "short-link:" + link.ID + ":" + clickID, OccurredAt: time.Now().UTC(), Source: "short-link",
				Payload: mustJSON(map[string]any{"template_id": "short-link:" + link.ID, "dispatch_id": "short-link:" + link.ID, "url": destination, "short_link_id": link.ID, "utm": link.UTM}),
			}})
		}
	}
	http.Redirect(w, r, destination, http.StatusFound)
}

func addUTM(destination string, raw json.RawMessage) (string, error) {
	u, err := url.Parse(destination)
	if err != nil || u.Scheme == "" || u.Host == "" {
		return "", fmt.Errorf("destination_url must be an absolute URL")
	}
	var utm map[string]any
	if len(raw) > 0 && string(raw) != "null" {
		if err := json.Unmarshal(raw, &utm); err != nil {
			return "", err
		}
	}
	q := u.Query()
	for _, key := range []string{"source", "medium", "campaign", "term", "content"} {
		if value, ok := utm[key].(string); ok && value != "" {
			q.Set("utm_"+key, value)
		}
	}
	u.RawQuery = q.Encode()
	return u.String(), nil
}

func anonymousClickID(r *http.Request, slug string) string {
	sum := sha256.Sum256([]byte(ClientIP(r, false) + "\x00" + r.UserAgent() + "\x00" + slug))
	return "anon-" + hex.EncodeToString(sum[:12])
}
