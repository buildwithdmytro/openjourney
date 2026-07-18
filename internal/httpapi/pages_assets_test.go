package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/buildwithdmytro/openjourney/internal/domain"
	"github.com/buildwithdmytro/openjourney/internal/ports"
)

type pageAssetStore struct {
	ports.Store
	page    domain.LandingPage
	version domain.PageVersion
	asset   domain.Asset
}

func (s *pageAssetStore) CreateLandingPage(context.Context, domain.Principal, domain.LandingPage) (domain.LandingPage, error) {
	return s.page, nil
}
func (s *pageAssetStore) GetLandingPage(context.Context, domain.Principal, string) (domain.LandingPage, error) {
	return s.page, nil
}
func (s *pageAssetStore) PublishLandingPage(_ context.Context, _ domain.Principal, _, publisher, manifest string, definition json.RawMessage) (domain.PageVersion, error) {
	s.version = domain.PageVersion{ID: "version-1", PageID: s.page.ID, Version: 1, PublishedBy: publisher, ManifestKey: manifest, Definition: definition}
	return s.version, nil
}
func (s *pageAssetStore) CreateAsset(_ context.Context, _ domain.Principal, asset domain.Asset) (domain.Asset, error) {
	asset.ID = "asset-1"
	s.asset = asset
	return asset, nil
}

func withPrincipal(r *http.Request, p domain.Principal) *http.Request {
	return r.WithContext(context.WithValue(r.Context(), principalKey{}, p))
}

func TestPublishLandingPageFreezesImmutableDefinition(t *testing.T) {
	store := &pageAssetStore{page: domain.LandingPage{ID: "page-1", Draft: json.RawMessage(`{"template":"<h1>hello</h1>"}`)}}
	blobs := &fakeBlobStore{}
	s := &Server{store: store, blobStore: blobs}
	req := withPrincipal(httptest.NewRequest(http.MethodPost, "/v1/pages/page-1/publish", nil), domain.Principal{TenantID: "tenant-1", ActorType: "user", UserID: "user-1"})
	req.SetPathValue("id", "page-1")
	rec := httptest.NewRecorder()
	s.publishLandingPage(rec, req)
	if rec.Code != http.StatusCreated {
		t.Fatalf("publish status = %d, body=%s", rec.Code, rec.Body.String())
	}
	if len(blobs.objects) != 1 || store.version.ManifestKey == "" {
		t.Fatalf("publish did not freeze a manifest: version=%+v blobs=%v", store.version, blobs.objects)
	}
	var got map[string]any
	if err := json.Unmarshal(blobs.objects[store.version.ManifestKey], &got); err != nil || got["template"] != "<h1>hello</h1>" {
		t.Fatalf("unexpected frozen definition: %s", blobs.objects[store.version.ManifestKey])
	}
}

func TestUploadAssetStoresContentAddressedBlobAndRecordsAsset(t *testing.T) {
	store := &pageAssetStore{}
	blobs := &fakeBlobStore{}
	s := &Server{store: store, blobStore: blobs}
	var body bytes.Buffer
	mw := multipart.NewWriter(&body)
	part, err := mw.CreateFormFile("file", "logo.svg")
	if err != nil {
		t.Fatal(err)
	}
	_, _ = part.Write([]byte("<svg></svg>"))
	_ = mw.Close()
	req := withPrincipal(httptest.NewRequest(http.MethodPost, "/v1/assets", &body), domain.Principal{TenantID: "tenant-1", WorkspaceID: "workspace-1", ActorType: "user", UserID: "user-1"})
	req.Header.Set("Content-Type", mw.FormDataContentType())
	rec := httptest.NewRecorder()
	s.uploadAsset(rec, req)
	if rec.Code != http.StatusCreated {
		t.Fatalf("upload status = %d, body=%s", rec.Code, rec.Body.String())
	}
	if store.asset.BlobKey == "" || string(blobs.objects[store.asset.BlobKey]) != "<svg></svg>" {
		t.Fatalf("asset was not content-addressed and stored: %+v", store.asset)
	}
}
