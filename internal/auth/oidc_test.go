package auth

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/go-jose/go-jose/v4"
	"github.com/go-jose/go-jose/v4/jwt"
)

func TestOIDCVerifierValidatesDiscoveryJWKSAndClaims(t *testing.T) {
	provider, key := newTestOIDCProvider(t)
	defer provider.Close()

	verifier, err := NewOIDCVerifier(context.Background(), provider.URL, "openjourney-web")
	if err != nil {
		t.Fatalf("new verifier: %v", err)
	}

	raw := signTestIDToken(t, key, provider.URL, "openjourney-web", map[string]any{
		"tenant_id":    "tenant-1",
		"workspace_id": "workspace-1",
		"app_id":       "app-1",
		"email":        "operator@example.com",
		"name":         "Operator",
	})
	claims, err := verifier.Verify(context.Background(), raw)
	if err != nil {
		t.Fatalf("verify valid token: %v", err)
	}
	if claims.Issuer != provider.URL || claims.Subject != "subject-1" ||
		claims.TenantID != "tenant-1" || claims.WorkspaceID != "workspace-1" || claims.AppID != "app-1" ||
		claims.Email != "operator@example.com" || claims.Name != "Operator" {
		t.Fatalf("unexpected claims: %+v", claims)
	}
}

func TestOIDCVerifierRejectsWrongAudience(t *testing.T) {
	provider, key := newTestOIDCProvider(t)
	defer provider.Close()

	verifier, err := NewOIDCVerifier(context.Background(), provider.URL, "openjourney-web")
	if err != nil {
		t.Fatalf("new verifier: %v", err)
	}
	raw := signTestIDToken(t, key, provider.URL, "different-client", map[string]any{
		"tenant_id": "tenant-1", "workspace_id": "workspace-1", "app_id": "app-1",
	})
	if _, err := verifier.Verify(context.Background(), raw); err == nil {
		t.Fatal("wrong audience token was accepted")
	}
}

func TestOIDCVerifierRejectsMissingTenantContextClaims(t *testing.T) {
	provider, key := newTestOIDCProvider(t)
	defer provider.Close()

	verifier, err := NewOIDCVerifier(context.Background(), provider.URL, "openjourney-web")
	if err != nil {
		t.Fatalf("new verifier: %v", err)
	}
	raw := signTestIDToken(t, key, provider.URL, "openjourney-web", map[string]any{
		"tenant_id": "tenant-1", "workspace_id": "workspace-1",
	})
	if _, err := verifier.Verify(context.Background(), raw); err == nil {
		t.Fatal("token without app_id was accepted")
	}
}

func newTestOIDCProvider(t *testing.T) (*httptest.Server, *rsa.PrivateKey) {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}
	jwks := jose.JSONWebKeySet{Keys: []jose.JSONWebKey{{
		Key:       &key.PublicKey,
		KeyID:     "test-key",
		Algorithm: string(jose.RS256),
		Use:       "sig",
	}}}
	var server *httptest.Server
	server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/.well-known/openid-configuration":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"issuer":                                server.URL,
				"jwks_uri":                              server.URL + "/keys",
				"id_token_signing_alg_values_supported": []string{"RS256"},
			})
		case "/keys":
			_ = json.NewEncoder(w).Encode(jwks)
		default:
			http.NotFound(w, r)
		}
	}))
	return server, key
}

func signTestIDToken(t *testing.T, key *rsa.PrivateKey, issuer, audience string, custom map[string]any) string {
	t.Helper()
	signer, err := jose.NewSigner(
		jose.SigningKey{Algorithm: jose.RS256, Key: key},
		(&jose.SignerOptions{}).WithType("JWT").WithHeader("kid", "test-key"),
	)
	if err != nil {
		t.Fatalf("new signer: %v", err)
	}
	now := time.Now()
	builder := jwt.Signed(signer).Claims(jwt.Claims{
		Issuer:   issuer,
		Subject:  "subject-1",
		Audience: jwt.Audience{audience},
		IssuedAt: jwt.NewNumericDate(now.Add(-time.Minute)),
		Expiry:   jwt.NewNumericDate(now.Add(time.Hour)),
	}).Claims(custom)
	raw, err := builder.Serialize()
	if err != nil {
		t.Fatalf("serialize token: %v", err)
	}
	return raw
}
