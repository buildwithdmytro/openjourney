package httpapi

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestGetProfileWithoutAuthMiddlewareReturnsUnauthorized(t *testing.T) {
	s := &Server{}
	req := httptest.NewRequest(http.MethodGet, "/v1/profiles/alice", nil)
	rec := httptest.NewRecorder()

	s.getProfile(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status=%d, want %d", rec.Code, http.StatusUnauthorized)
	}
}
