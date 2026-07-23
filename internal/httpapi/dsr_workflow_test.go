package httpapi

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/buildwithdmytro/openjourney/internal/domain"
)

type dsrFakeStore struct {
	fakeStore
	createdReq        domain.PrivacyRequest
	verifiedReq       domain.PrivacyRequest
	rejectedReq       domain.PrivacyRequest
	lastVerifiedToken string
	lastRejectReason  string
	privacyReq        domain.PrivacyRequest
}

func (s *dsrFakeStore) GetPrivacyRequest(_ context.Context, _ domain.Principal, id string) (domain.PrivacyRequest, error) {
	request := s.privacyReq
	if request.ID == "" {
		request = domain.PrivacyRequest{ID: id}
	}
	return request, nil
}

func (s *dsrFakeStore) VerifyPrivacyRequest(_ context.Context, _ domain.Principal, id, token string) (domain.PrivacyRequest, error) {
	s.lastVerifiedToken = token
	return domain.PrivacyRequest{ID: id, VerificationStatus: "verified", Status: "pending"}, nil
}

func (s *dsrFakeStore) RejectPrivacyRequest(_ context.Context, _ domain.Principal, id, reason string) (domain.PrivacyRequest, error) {
	s.lastRejectReason = reason
	return domain.PrivacyRequest{ID: id, VerificationStatus: "rejected", Status: "rejected", Error: reason}, nil
}

func TestDSRHTTPVerifyAndRejectScopes(t *testing.T) {
	mockStore := &dsrFakeStore{}
	handler := New(mockStore, 100)

	// 1. Without privacy:approve scope -> 403 Forbidden on verify
	verifyReq := httptest.NewRequest(http.MethodPost, "/v1/privacy/requests/req-123/verify", bytes.NewBufferString(`{"token":"abc"}`))
	verifyReq.Header.Set("Authorization", "Bearer test-key")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, verifyReq)

	if rec.Code != http.StatusForbidden {
		t.Fatalf("expected 403 Forbidden without privacy:approve scope, got %d", rec.Code)
	}

	// 2. Without privacy:approve scope -> 403 Forbidden on reject
	rejectReq := httptest.NewRequest(http.MethodPost, "/v1/privacy/requests/req-123/reject", bytes.NewBufferString(`{"reason":"bad"}`))
	rejectReq.Header.Set("Authorization", "Bearer test-key")
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, rejectReq)

	if rec.Code != http.StatusForbidden {
		t.Fatalf("expected 403 Forbidden without privacy:approve scope, got %d", rec.Code)
	}

	// Now use key with privacy:approve scope
	mockStore.scopes = []string{"privacy:approve", "privacy:read"}

	// 3. Verify with privacy:approve scope -> 200 OK
	verifyReq = httptest.NewRequest(http.MethodPost, "/v1/privacy/requests/req-123/verify", bytes.NewBufferString(`{"token":"secret-token-123"}`))
	verifyReq.Header.Set("Authorization", "Bearer test-key")
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, verifyReq)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 OK for verify with privacy:approve scope, got %d: %s", rec.Code, rec.Body.String())
	}
	if mockStore.lastVerifiedToken != "secret-token-123" {
		t.Fatalf("expected token secret-token-123, got %q", mockStore.lastVerifiedToken)
	}

	// 4. Reject with privacy:approve scope -> 200 OK
	rejectReq = httptest.NewRequest(http.MethodPost, "/v1/privacy/requests/req-123/reject", bytes.NewBufferString(`{"reason":"invalid identity proof"}`))
	rejectReq.Header.Set("Authorization", "Bearer test-key")
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, rejectReq)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 OK for reject with privacy:approve scope, got %d: %s", rec.Code, rec.Body.String())
	}
	if mockStore.lastRejectReason != "invalid identity proof" {
		t.Fatalf("expected reject reason 'invalid identity proof', got %q", mockStore.lastRejectReason)
	}
}

func TestDSRDownloadRequiresScopeAndCompletedExport(t *testing.T) {
	store := &dsrFakeStore{privacyReq: domain.PrivacyRequest{
		ID: "request-1", RequestType: "export", Status: "complete", ArtifactKey: "privacy/tenant/request-1/export.json",
	}}
	server := &Server{store: store, blobStore: &fakeBlobStore{objects: map[string][]byte{
		"privacy/tenant/request-1/export.json": []byte(`{"request_id":"request-1"}`),
	}}}
	handler := server.buildMux()

	request := httptest.NewRequest(http.MethodGet, "/v1/privacy/requests/request-1/download", nil)
	request.Header.Set("Authorization", "Bearer test-key")
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusForbidden {
		t.Fatalf("expected privacy:read to gate download, got %d", recorder.Code)
	}

	store.scopes = []string{"privacy:read"}
	request = httptest.NewRequest(http.MethodGet, "/v1/privacy/requests/request-1/download", nil)
	request.Header.Set("Authorization", "Bearer test-key")
	recorder = httptest.NewRecorder()
	handler.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusOK || recorder.Body.String() != `{"request_id":"request-1"}` {
		t.Fatalf("expected authorized completed export, got %d %q", recorder.Code, recorder.Body.String())
	}
	if got := recorder.Header().Get("Content-Disposition"); got != "attachment; filename=privacy-export.json" {
		t.Fatalf("unexpected download disposition %q", got)
	}

	store.privacyReq.Status = "pending"
	request = httptest.NewRequest(http.MethodGet, "/v1/privacy/requests/request-1/download", nil)
	request.Header.Set("Authorization", "Bearer test-key")
	recorder = httptest.NewRecorder()
	handler.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusNotFound {
		t.Fatalf("expected incomplete export to be unavailable, got %d", recorder.Code)
	}
}
