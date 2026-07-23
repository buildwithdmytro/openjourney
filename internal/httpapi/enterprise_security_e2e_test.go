package httpapi

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/require"
)

// TestEnterpriseSecurityE2E verifies the route-level scope boundary for every
// enterprise permission. The individual security primitives are covered by
// the SAML, SCIM, maker-checker, audit, DSR, and team integration tests.
func TestEnterpriseSecurityE2E(t *testing.T) {
	protected := []string{
		"audit:read",
		"privacy:read",
		"privacy:approve",
		"teams:read",
		"teams:write",
		"scim:manage",
	}

	for _, requiredScope := range protected {
		t.Run(requiredScope, func(t *testing.T) {
			store := &fakeStore{scopes: []string{"profiles:read"}}
			server := &Server{store: store}
			handler := server.authenticate(requiredScope, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(http.StatusNoContent)
			}))

			request := httptest.NewRequest(http.MethodGet, "/v1/enterprise-security", nil)
			request.Header.Set("Authorization", "Bearer test-key")
			response := httptest.NewRecorder()
			handler.ServeHTTP(response, request)
			require.Equal(t, http.StatusForbidden, response.Code)

			store.scopes = []string{requiredScope}
			request = httptest.NewRequest(http.MethodGet, "/v1/enterprise-security", nil)
			request.Header.Set("Authorization", "Bearer test-key")
			response = httptest.NewRecorder()
			handler.ServeHTTP(response, request)
			require.Equal(t, http.StatusNoContent, response.Code)
		})
	}
}
