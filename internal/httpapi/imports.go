package httpapi

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"

	"github.com/buildwithdmytro/openjourney/internal/domain"
	"github.com/buildwithdmytro/openjourney/internal/postgres"
)

type importHTTPStore interface {
	CreateImportRequest(context.Context, domain.Principal, string, string, json.RawMessage, string) (domain.ImportRequest, error)
	GetImportRequest(context.Context, domain.Principal, string) (domain.ImportRequest, error)
}

func (s *Server) createImport(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseMultipartForm(20 << 20); err != nil {
		writeError(w, 400, "invalid_multipart", err.Error())
		return
	}
	kind := strings.TrimSpace(r.FormValue("kind"))
	mapping := json.RawMessage(r.FormValue("mapping"))
	if len(mapping) == 0 {
		mapping = json.RawMessage(`{}`)
	}
	file, _, err := r.FormFile("file")
	if err != nil {
		writeError(w, 400, "file_required", err.Error())
		return
	}
	defer file.Close()
	data, err := io.ReadAll(io.LimitReader(file, 20<<20))
	if err != nil {
		writeError(w, 400, "read_file", err.Error())
		return
	}
	if len(data) == 0 {
		writeError(w, 400, "file_required", "CSV file is empty")
		return
	}
	if s.blobStore == nil {
		writeError(w, 503, "blob_unavailable", "blob storage is not configured")
		return
	}
	principal := principalFrom(r)
	digest := sha256.Sum256(data)
	key := "imports/" + principal.TenantID + "/" + strings.ReplaceAll(kind, "/", "") + "-" + hex.EncodeToString(digest[:]) + ".csv"
	if err := s.blobStore.Put(r.Context(), key, data, "text/csv"); err != nil {
		internalError(w, err, "store import", principal)
		return
	}
	store, ok := s.store.(importHTTPStore)
	if !ok {
		writeError(w, 503, "imports_unavailable", "import storage is not configured")
		return
	}
	item, err := store.CreateImportRequest(r.Context(), principal, kind, key, mapping, principal.AppID)
	if err != nil {
		writeError(w, 422, "invalid_import", err.Error())
		return
	}
	writeJSON(w, http.StatusAccepted, item)
}

func (s *Server) getImport(w http.ResponseWriter, r *http.Request) {
	store, ok := s.store.(importHTTPStore)
	if !ok {
		writeError(w, 503, "imports_unavailable", "import storage is not configured")
		return
	}
	item, err := store.GetImportRequest(r.Context(), principalFrom(r), r.PathValue("id"))
	if errors.Is(err, postgres.ErrNotFound) {
		writeError(w, 404, "not_found", "import request was not found")
		return
	}
	if err != nil {
		internalError(w, err, "get import", principalFrom(r))
		return
	}
	writeJSON(w, 200, item)
}
