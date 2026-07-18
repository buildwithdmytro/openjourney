package httpapi

import (
	"errors"
	"mime"
	"net/http"
	"net/url"
	"path"
	"strconv"
	"strings"

	"github.com/buildwithdmytro/openjourney/internal/render"
)

var errPublicAssetStoreUnavailable = errors.New("public asset store is unavailable")

// RenderHTML renders a pinned public resource and writes it as HTML. Callers
// are responsible for loading the immutable version; this helper deliberately
// does not accept a draft resource.
func RenderHTML(w http.ResponseWriter, template string, vars map[string]any) error {
	html, err := render.Render(template, vars)
	if err != nil {
		return err
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_, err = w.Write([]byte(html))
	return err
}

func (s *Server) serveAsset(w http.ResponseWriter, r *http.Request) {
	if s.blobStore == nil {
		writeError(w, http.StatusServiceUnavailable, "asset_store_unavailable", errPublicAssetStoreUnavailable.Error())
		return
	}
	key, err := url.PathUnescape(strings.TrimSpace(r.PathValue("blobKey")))
	if err != nil || key == "" || strings.Contains(key, "..") {
		writeError(w, http.StatusBadRequest, "invalid_asset", "asset key is invalid")
		return
	}
	data, err := s.blobStore.Get(r.Context(), key)
	if err != nil {
		writeError(w, http.StatusNotFound, "asset_not_found", "asset was not found")
		return
	}
	contentType := mime.TypeByExtension(strings.ToLower(path.Ext(key)))
	if contentType == "" {
		contentType = http.DetectContentType(data)
	}
	w.Header().Set("Content-Type", contentType)
	w.Header().Set("Content-Length", formatContentLength(len(data)))
	_, _ = w.Write(data)
}

func formatContentLength(n int) string {
	return strconv.Itoa(n)
}
