package httpapi

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"path/filepath"
	"strings"

	"github.com/buildwithdmytro/openjourney/internal/domain"
	"github.com/buildwithdmytro/openjourney/internal/postgres"
	"github.com/buildwithdmytro/openjourney/internal/publishing"
)

func (s *Server) createLandingPage(w http.ResponseWriter, r *http.Request) {
	var in domain.LandingPage
	if err := decodeJSON(w, r, &in); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_json", err.Error())
		return
	}
	out, err := s.store.CreateLandingPage(r.Context(), principalFrom(r), in)
	if err != nil {
		writeError(w, http.StatusUnprocessableEntity, "invalid_page", err.Error())
		return
	}
	writeJSON(w, http.StatusCreated, out)
}
func (s *Server) listLandingPages(w http.ResponseWriter, r *http.Request) {
	out, err := s.store.ListLandingPages(r.Context(), principalFrom(r))
	if err != nil {
		internalError(w, err, "list pages", principalFrom(r))
		return
	}
	if out == nil {
		out = []domain.LandingPage{}
	}
	writeJSON(w, http.StatusOK, map[string]any{"pages": out})
}
func (s *Server) getLandingPage(w http.ResponseWriter, r *http.Request) {
	p := principalFrom(r)
	out, err := s.store.GetLandingPage(r.Context(), p, r.PathValue("id"))
	if errors.Is(err, postgres.ErrNotFound) {
		writeError(w, http.StatusNotFound, "not_found", "page not found")
		return
	}
	if err != nil {
		internalError(w, err, "get page", p)
		return
	}
	writeJSON(w, http.StatusOK, out)
}
func (s *Server) updateLandingPage(w http.ResponseWriter, r *http.Request) {
	p := principalFrom(r)
	var in domain.LandingPage
	if err := decodeJSON(w, r, &in); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_json", err.Error())
		return
	}
	in.ID = r.PathValue("id")
	out, err := s.store.UpdateLandingPage(r.Context(), p, in)
	if errors.Is(err, postgres.ErrNotFound) {
		writeError(w, http.StatusNotFound, "not_found", "page not found")
		return
	}
	if err != nil {
		writeError(w, http.StatusUnprocessableEntity, "invalid_page", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, out)
}
func (s *Server) publishLandingPage(w http.ResponseWriter, r *http.Request) {
	p := principalFrom(r)
	if p.ActorType != "user" || p.UserID == "" {
		writeError(w, http.StatusForbidden, "human_approval_required", "publishing requires an authenticated user")
		return
	}
	page, err := s.store.GetLandingPage(r.Context(), p, r.PathValue("id"))
	if errors.Is(err, postgres.ErrNotFound) {
		writeError(w, http.StatusNotFound, "not_found", "page not found")
		return
	}
	if err != nil {
		internalError(w, err, "get page for publish", p)
		return
	}
	var definition any
	if err = json.Unmarshal(page.Draft, &definition); err != nil {
		writeError(w, http.StatusUnprocessableEntity, "invalid_page_definition", err.Error())
		return
	}
	canonical, err := json.Marshal(definition)
	if err != nil {
		writeError(w, http.StatusUnprocessableEntity, "invalid_page_definition", err.Error())
		return
	}
	out, err := publishing.Publish(r.Context(), p, page.ID, "pages", page.Draft, s.blobStore, func(json.RawMessage) ([]byte, error) { return canonical, nil }, func(ctx context.Context, principal domain.Principal, id, publisher, manifest string) (domain.PageVersion, error) {
		return s.store.PublishLandingPage(ctx, principal, id, publisher, manifest, canonical)
	})
	if errors.Is(err, publishing.ErrHumanActorRequired) {
		writeError(w, http.StatusForbidden, "human_approval_required", err.Error())
		return
	}
	if errors.Is(err, postgres.ErrNotFound) {
		writeError(w, http.StatusNotFound, "not_found", "page not found")
		return
	}
	if err != nil {
		internalError(w, err, "publish page", p)
		return
	}
	writeJSON(w, http.StatusCreated, out)
}

func (s *Server) uploadAsset(w http.ResponseWriter, r *http.Request) {
	p := principalFrom(r)
	if s.blobStore == nil {
		writeError(w, http.StatusServiceUnavailable, "asset_store_unavailable", "asset store is unavailable")
		return
	}
	if err := r.ParseMultipartForm(16 << 20); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_multipart", err.Error())
		return
	}
	file, header, err := r.FormFile("file")
	if err != nil {
		writeError(w, http.StatusBadRequest, "file_required", err.Error())
		return
	}
	defer file.Close()
	data, err := io.ReadAll(io.LimitReader(file, 16<<20+1))
	if err != nil {
		writeError(w, http.StatusBadRequest, "read_file", err.Error())
		return
	}
	if len(data) > 16<<20 {
		writeError(w, http.StatusRequestEntityTooLarge, "file_too_large", "asset exceeds 16 MiB")
		return
	}
	name := filepath.Base(strings.TrimSpace(header.Filename))
	if name == "." || name == "" {
		writeError(w, http.StatusBadRequest, "filename_required", "filename is required")
		return
	}
	sum := sha256.Sum256(data)
	key := "assets/" + p.TenantID + "/" + hex.EncodeToString(sum[:])
	contentType := header.Header.Get("Content-Type")
	if contentType == "" {
		contentType = http.DetectContentType(data)
	}
	if err := s.blobStore.Put(r.Context(), key, data, contentType); err != nil {
		internalError(w, err, "store asset", p)
		return
	}
	out, err := s.store.CreateAsset(r.Context(), p, domain.Asset{Filename: name, ContentType: contentType, BlobKey: key, SizeBytes: int64(len(data))})
	if err != nil {
		internalError(w, err, "record asset", p)
		return
	}
	writeJSON(w, http.StatusCreated, out)
}
func (s *Server) listAssets(w http.ResponseWriter, r *http.Request) {
	p := principalFrom(r)
	out, err := s.store.ListAssets(r.Context(), p)
	if err != nil {
		internalError(w, err, "list assets", p)
		return
	}
	if out == nil {
		out = []domain.Asset{}
	}
	writeJSON(w, http.StatusOK, map[string]any{"assets": out})
}
