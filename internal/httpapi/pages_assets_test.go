package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/buildwithdmytro/openjourney/internal/domain"
	"github.com/buildwithdmytro/openjourney/internal/ports"
)

type pageAssetStore struct {
	ports.Store
	page        domain.LandingPage
	version     domain.PageVersion
	asset       domain.Asset
	form        domain.Form
	formVersion domain.FormVersion
	events      []domain.Event
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
func (s *pageAssetStore) GetPublishedLandingPage(context.Context, string) (domain.LandingPage, domain.PageVersion, error) {
	return s.page, s.version, nil
}
func (s *pageAssetStore) GetPublishedForm(context.Context, string) (domain.Form, domain.FormVersion, error) {
	return s.form, s.formVersion, nil
}
func (s *pageAssetStore) GetFirstAppID(context.Context, string, string) (string, error) {
	return "app-1", nil
}
func (s *pageAssetStore) AcceptEvents(_ context.Context, _ domain.Principal, events []domain.Event) ([]string, error) {
	s.events = append(s.events, events...)
	return []string{"event-1"}, nil
}
func (s *pageAssetStore) RecordFormSubmission(context.Context, domain.Principal, string, int, json.RawMessage, json.RawMessage, string) error {
	return nil
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

func TestVersionedCapturePublishRejectsAPIKey(t *testing.T) {
	store := &pageAssetStore{page: domain.LandingPage{ID: "page-1", Draft: json.RawMessage(`{"template":"<h1>hello</h1>"}`)}}
	s := &Server{store: store, blobStore: &fakeBlobStore{}}
	apiKey := domain.Principal{TenantID: "tenant-1", ActorType: "api_key", KeyID: "key-1"}
	for _, tc := range []struct {
		name string
		h    http.HandlerFunc
		path string
	}{
		{name: "form", h: s.publishForm, path: "form-1"},
		{name: "page", h: s.publishLandingPage, path: "page-1"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			req := withPrincipal(httptest.NewRequest(http.MethodPost, "/publish", nil), apiKey)
			req.SetPathValue("id", tc.path)
			rec := httptest.NewRecorder()
			tc.h(rec, req)
			if rec.Code != http.StatusForbidden {
				t.Fatalf("publish status = %d, body=%s", rec.Code, rec.Body.String())
			}
		})
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

func TestServeLandingPageRendersPinnedVersionAndSignsEmbeddedForm(t *testing.T) {
	store := &pageAssetStore{
		page:        domain.LandingPage{ID: "page-1", TenantID: "tenant-1", WorkspaceID: "workspace-1", Draft: json.RawMessage(`{"template":"DRAFT"}`)},
		version:     domain.PageVersion{ID: "version-1", PageID: "page-1", Version: 1, Definition: json.RawMessage(`{"template":"<h1>{{ page_version }}</h1><input value=\"{{ form_token }}\"><span>{{ form_id }}</span>","form_id":"form-1"}`)},
		form:        domain.Form{ID: "form-1", TenantID: "tenant-1", WorkspaceID: "workspace-1", Status: "published"},
		formVersion: domain.FormVersion{FormID: "form-1", Version: 1, Definition: json.RawMessage(`{"fields":[{"key":"email","type":"email","maps_to":"email"}],"schema":{"type":"object","properties":{"email":{"type":"string","format":"email"}},"required":["email"],"additionalProperties":false}}`)},
	}
	s := &Server{store: store, trackingSecretKey: []byte("test-secret"), publicLimiter: NewIPRateLimiter(100, 10), captchaVerifier: NoopCaptchaVerifier{}}
	req := httptest.NewRequest(http.MethodGet, "/p/home", nil)
	req.SetPathValue("slug", "home")
	rec := httptest.NewRecorder()
	s.serveLandingPage(rec, req)
	if rec.Code != http.StatusOK || rec.Header().Get("Content-Type") != "text/html; charset=utf-8" {
		t.Fatalf("serve status/content type = %d/%q, body=%s", rec.Code, rec.Header().Get("Content-Type"), rec.Body.String())
	}
	if strings.Contains(rec.Body.String(), "DRAFT") || !strings.Contains(rec.Body.String(), "<h1>1</h1>") {
		t.Fatalf("served draft or wrong pinned version: %s", rec.Body.String())
	}
	start := strings.Index(rec.Body.String(), `value="`) + len(`value="`)
	end := strings.Index(rec.Body.String()[start:], `"`)
	if start < len(`value="`) || end < 1 {
		t.Fatalf("signed form token missing: %s", rec.Body.String())
	}
	token := rec.Body.String()[start : start+end]
	if _, err := VerifyFormToken(token, "form-1", 1, []byte("test-secret"), time.Now()); err != nil {
		t.Fatalf("page token did not verify: %v", err)
	}

	body := `{"form_token":"` + token + `","values":{"email":"person@example.com"}}`
	submitReq := httptest.NewRequest(http.MethodPost, "/f/form-1", strings.NewReader(body))
	submitReq.SetPathValue("formId", "form-1")
	submitReq.RemoteAddr = "198.51.100.40:1234"
	submitRec := httptest.NewRecorder()
	s.submitPublicForm(submitRec, submitReq)
	if submitRec.Code != http.StatusAccepted || len(store.events) != 2 {
		t.Fatalf("embedded form submission status/events = %d/%d, body=%s", submitRec.Code, len(store.events), submitRec.Body.String())
	}
}
