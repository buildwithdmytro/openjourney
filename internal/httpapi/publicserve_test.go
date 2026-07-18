package httpapi

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
)

type publicServeBlobStore struct {
	objects map[string][]byte
}

func (b publicServeBlobStore) Put(context.Context, string, []byte, string) error { return nil }
func (b publicServeBlobStore) Get(_ context.Context, key string) ([]byte, error) {
	data, ok := b.objects[key]
	if !ok {
		return nil, errors.New("missing")
	}
	return data, nil
}
func (b publicServeBlobStore) Delete(context.Context, string) error { return nil }

func TestRenderHTMLRendersPinnedTemplate(t *testing.T) {
	recorder := httptest.NewRecorder()
	if err := RenderHTML(recorder, "<h1>{{ name }}</h1>", map[string]any{"name": "Acquisition"}); err != nil {
		t.Fatal(err)
	}
	if got := recorder.Header().Get("Content-Type"); got != "text/html; charset=utf-8" {
		t.Fatalf("content type = %q", got)
	}
	if got := recorder.Body.String(); got != "<h1>Acquisition</h1>" {
		t.Fatalf("body = %q", got)
	}
}

func TestServeAssetStreamsBlob(t *testing.T) {
	s := &Server{blobStore: publicServeBlobStore{objects: map[string][]byte{"assets/logo.svg": []byte("<svg></svg>")}}}
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/a/assets/logo.svg", nil)
	request.SetPathValue("blobKey", "assets/logo.svg")
	s.serveAsset(recorder, request)
	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d", recorder.Code)
	}
	if got := recorder.Body.String(); got != "<svg></svg>" {
		t.Fatalf("body = %q", got)
	}
	if got := recorder.Header().Get("Content-Type"); got != "image/svg+xml" {
		t.Fatalf("content type = %q", got)
	}
}

func TestServeAssetRejectsTraversal(t *testing.T) {
	s := &Server{blobStore: publicServeBlobStore{objects: map[string][]byte{}}}
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/a/../secret", nil)
	request.SetPathValue("blobKey", "../secret")
	s.serveAsset(recorder, request)
	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("status = %d", recorder.Code)
	}
}
