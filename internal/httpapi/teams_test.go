package httpapi

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestTeamsRoutesRequireTeamScopes(t *testing.T) {
	store := &fakeStore{scopes: []string{"profiles:read"}}
	handler := New(store, 100)

	req := httptest.NewRequest(http.MethodGet, "/v1/teams", nil)
	req.Header.Set("Authorization", "Bearer test-key")
	res := httptest.NewRecorder()
	handler.ServeHTTP(res, req)
	require.Equal(t, http.StatusForbidden, res.Code)

	store.scopes = []string{"teams:read"}
	req = httptest.NewRequest(http.MethodGet, "/v1/teams", nil)
	req.Header.Set("Authorization", "Bearer test-key")
	res = httptest.NewRecorder()
	handler.ServeHTTP(res, req)
	require.Equal(t, http.StatusOK, res.Code)
}
