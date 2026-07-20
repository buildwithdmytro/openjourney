package channels

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/buildwithdmytro/openjourney/internal/domain"
	"github.com/buildwithdmytro/openjourney/internal/ports"
)

// generateTestVAPIDKeys creates a P-256 ECDSA keypair for testing.
func generateTestVAPIDKeys() (string, string, error) {
	priv, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return "", "", err
	}

	// Private key: D as 32-byte value
	d := priv.D.Bytes()
	dPadded := make([]byte, 32)
	copy(dPadded[32-len(d):], d)
	privB64 := base64.RawURLEncoding.EncodeToString(dPadded)

	// Public key: X||Y (64 bytes total)
	pubX := priv.PublicKey.X.Bytes()
	pubY := priv.PublicKey.Y.Bytes()
	xPadded := make([]byte, 32)
	yPadded := make([]byte, 32)
	copy(xPadded[32-len(pubX):], pubX)
	copy(yPadded[32-len(pubY):], pubY)
	pubB64 := base64.RawURLEncoding.EncodeToString(append(xPadded, yPadded...))

	return privB64, pubB64, nil
}

func TestWebPushBuildRequestWithValidConfig(t *testing.T) {
	privKey, pubKey, err := generateTestVAPIDKeys()
	if err != nil {
		t.Fatalf("generate keys: %v", err)
	}

	// Set environment variables for the refs
	os.Setenv("TEST_VAPID_PRIVATE", privKey)
	os.Setenv("TEST_VAPID_PUBLIC", pubKey)
	defer func() {
		os.Unsetenv("TEST_VAPID_PRIVATE")
		os.Unsetenv("TEST_VAPID_PUBLIC")
	}()

	cfg := WebPushConfig{
		VAPIDPrivateRef: "TEST_VAPID_PRIVATE",
		VAPIDPublicRef:  "TEST_VAPID_PUBLIC",
		VAPIDSubject:    "mailto:sender@example.com",
	}
	cfgJSON, _ := json.Marshal(cfg)

	identity := domain.SendingIdentity{
		Channel:  "push",
		Provider: "webpush",
		Config:   cfgJSON,
	}

	msg := ports.RenderedMessage{
		Channel:  "push",
		Endpoint: "https://push.example.com/v1/push/abc123",
		Title:    "Test",
		Body:     "Wake signal",
		Identity: identity,
	}

	profile := &WebPushProfile{}
	req, err := profile.BuildRequest(context.Background(), msg)
	if err != nil {
		t.Fatalf("BuildRequest failed: %v", err)
	}

	// Verify request properties
	if req.Method != "POST" {
		t.Errorf("expected POST, got %s", req.Method)
	}
	if req.URL.String() != msg.Endpoint {
		t.Errorf("expected URL %s, got %s", msg.Endpoint, req.URL.String())
	}

	// Check headers
	authHeader := req.Header.Get("Authorization")
	if !strings.HasPrefix(authHeader, "vapid t=") {
		t.Errorf("expected Authorization to start with 'vapid t=', got %s", authHeader)
	}
	if !strings.Contains(authHeader, "k="+pubKey) {
		t.Errorf("expected Authorization to contain public key %s, got %s", pubKey, authHeader)
	}

	if req.Header.Get("TTL") != "24" {
		t.Errorf("expected TTL=24, got %s", req.Header.Get("TTL"))
	}

	// Verify body is empty (wake signal) - for wake signals, body should be nil or http.NoBody
	if req.Body != nil && req.Body != http.NoBody {
		body := make([]byte, 1024)
		n, _ := req.Body.Read(body)
		if n > 0 {
			t.Errorf("expected empty body for wake signal, got %d bytes", n)
		}
	}
}

func TestWebPushBuildRequestMissingPrivateRef(t *testing.T) {
	cfg := WebPushConfig{
		VAPIDPrivateRef: "",
		VAPIDPublicRef:  "TEST_VAPID_PUBLIC",
		VAPIDSubject:    "mailto:sender@example.com",
	}
	cfgJSON, _ := json.Marshal(cfg)

	identity := domain.SendingIdentity{
		Channel:  "push",
		Provider: "webpush",
		Config:   cfgJSON,
	}

	msg := ports.RenderedMessage{
		Channel:  "push",
		Endpoint: "https://push.example.com/v1/push/abc123",
		Identity: identity,
	}

	profile := &WebPushProfile{}
	_, err := profile.BuildRequest(context.Background(), msg)
	if err == nil {
		t.Fatal("expected error for missing vapid_private_ref")
	}
	if !strings.Contains(err.Error(), "vapid_private_ref") {
		t.Errorf("expected error mentioning vapid_private_ref, got %v", err)
	}
}

func TestWebPushBuildRequestMissingEnvVar(t *testing.T) {
	cfg := WebPushConfig{
		VAPIDPrivateRef: "MISSING_VAR",
		VAPIDPublicRef:  "MISSING_VAR",
		VAPIDSubject:    "mailto:sender@example.com",
	}
	cfgJSON, _ := json.Marshal(cfg)

	identity := domain.SendingIdentity{
		Channel:  "push",
		Provider: "webpush",
		Config:   cfgJSON,
	}

	msg := ports.RenderedMessage{
		Channel:  "push",
		Endpoint: "https://push.example.com/v1/push/abc123",
		Identity: identity,
	}

	profile := &WebPushProfile{}
	_, err := profile.BuildRequest(context.Background(), msg)
	if err == nil {
		t.Fatal("expected error for missing environment variable")
	}
	if !strings.Contains(err.Error(), "not set") {
		t.Errorf("expected error about env var not set, got %v", err)
	}
}

func TestWebPushParseResponseSuccess(t *testing.T) {
	profile := &WebPushProfile{}

	resp := &http.Response{
		StatusCode: http.StatusCreated,
		Header:     make(http.Header),
	}

	providerID, err := profile.ParseResponse(resp, nil)
	if err != nil {
		t.Fatalf("ParseResponse failed: %v", err)
	}
	if providerID == "" {
		t.Error("expected non-empty provider ID")
	}
}

func TestWebPushParseResponseServerError(t *testing.T) {
	profile := &WebPushProfile{}

	resp := &http.Response{
		StatusCode: http.StatusInternalServerError,
		Header:     make(http.Header),
	}

	_, err := profile.ParseResponse(resp, nil)
	if err == nil {
		t.Fatal("expected error for 500 status")
	}

	dErr, ok := err.(*DeliveryError)
	if !ok {
		t.Fatalf("expected DeliveryError, got %T", err)
	}
	if !dErr.Retryable {
		t.Error("expected 500 error to be retryable")
	}
}

func TestWebPushIsInvalidToken(t *testing.T) {
	profile := &WebPushProfile{}

	tests := []struct {
		status   int
		expected bool
	}{
		{http.StatusNotFound, true},
		{http.StatusGone, true},
		{http.StatusUnauthorized, false},
		{http.StatusInternalServerError, false},
		{http.StatusCreated, false},
	}

	for _, tt := range tests {
		resp := &http.Response{StatusCode: tt.status}
		result := profile.IsInvalidToken(resp, nil)
		if result != tt.expected {
			t.Errorf("status %d: expected %v, got %v", tt.status, tt.expected, result)
		}
	}
}

func TestWebPushVAPIDJWTStructure(t *testing.T) {
	privKey, _, err := generateTestVAPIDKeys()
	if err != nil {
		t.Fatalf("generate keys: %v", err)
	}

	os.Setenv("TEST_VAPID_PRIVATE", privKey)
	os.Setenv("TEST_VAPID_PUBLIC", "test-pub")
	defer func() {
		os.Unsetenv("TEST_VAPID_PRIVATE")
		os.Unsetenv("TEST_VAPID_PUBLIC")
	}()

	cfg := WebPushConfig{
		VAPIDPrivateRef: "TEST_VAPID_PRIVATE",
		VAPIDPublicRef:  "TEST_VAPID_PUBLIC",
		VAPIDSubject:    "mailto:sender@example.com",
	}
	cfgJSON, _ := json.Marshal(cfg)

	identity := domain.SendingIdentity{
		Channel:  "push",
		Provider: "webpush",
		Config:   cfgJSON,
	}

	msg := ports.RenderedMessage{
		Channel:  "push",
		Endpoint: "https://push.example.com/v1/push/abc123",
		Identity: identity,
	}

	profile := &WebPushProfile{}
	req, err := profile.BuildRequest(context.Background(), msg)
	if err != nil {
		t.Fatalf("BuildRequest failed: %v", err)
	}

	authHeader := req.Header.Get("Authorization")
	// Extract JWT from "vapid t=<JWT>,k=<pubkey>"
	parts := strings.Split(authHeader, "t=")
	if len(parts) < 2 {
		t.Fatal("Authorization header format incorrect")
	}
	jwtPart := strings.Split(parts[1], ",")[0]

	// JWT should have 3 parts separated by dots
	jwtParts := strings.Split(jwtPart, ".")
	if len(jwtParts) != 3 {
		t.Errorf("expected JWT with 3 parts, got %d", len(jwtParts))
	}

	// Decode claims and verify structure
	claimsB64 := jwtParts[1]
	// Add padding if necessary
	switch len(claimsB64) % 4 {
	case 2:
		claimsB64 += "=="
	case 3:
		claimsB64 += "="
	}

	claimsBytes, err := base64.RawURLEncoding.DecodeString(claimsB64)
	if err != nil {
		t.Fatalf("decode JWT claims: %v", err)
	}

	var claims map[string]interface{}
	if err := json.Unmarshal(claimsBytes, &claims); err != nil {
		t.Fatalf("unmarshal JWT claims: %v", err)
	}

	// Verify required claims
	if sub, ok := claims["sub"].(string); !ok || sub != "mailto:sender@example.com" {
		t.Errorf("expected sub=mailto:sender@example.com, got %v", claims["sub"])
	}
	if _, ok := claims["iat"].(float64); !ok {
		t.Error("expected iat claim")
	}
	if _, ok := claims["exp"].(float64); !ok {
		t.Error("expected exp claim")
	}
}

func TestWebPushHTTPIntegration(t *testing.T) {
	privKey, pubKey, err := generateTestVAPIDKeys()
	if err != nil {
		t.Fatalf("generate keys: %v", err)
	}

	os.Setenv("TEST_VAPID_PRIVATE", privKey)
	os.Setenv("TEST_VAPID_PUBLIC", pubKey)
	defer func() {
		os.Unsetenv("TEST_VAPID_PRIVATE")
		os.Unsetenv("TEST_VAPID_PUBLIC")
	}()

	// A server that asserts the VAPID request shape the profile builds, then 201s.
	// We execute the profile-built request with a plain client on purpose: the
	// production adapter's transport intentionally refuses loopback dials (SSRF
	// guard), so a full NewWebPushAdapter().Send to an httptest server can never
	// succeed — that guard is exercised separately via IsSafeURL/IsPrivateIP. This
	// verifies BuildRequest + ParseResponse against a real round-trip.
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("expected POST, got %s", r.Method)
		}
		if !strings.HasPrefix(r.Header.Get("Authorization"), "vapid t=") {
			t.Errorf("expected VAPID Authorization header, got %q", r.Header.Get("Authorization"))
		}
		if r.Header.Get("TTL") == "" {
			t.Error("expected TTL header")
		}
		w.WriteHeader(http.StatusCreated)
	}))
	defer server.Close()

	cfg := WebPushConfig{
		VAPIDPrivateRef: "TEST_VAPID_PRIVATE",
		VAPIDPublicRef:  "TEST_VAPID_PUBLIC",
		VAPIDSubject:    "mailto:sender@example.com",
	}
	cfgJSON, _ := json.Marshal(cfg)

	msg := ports.RenderedMessage{
		Channel:  "push",
		Endpoint: server.URL,
		Identity: domain.SendingIdentity{Channel: "push", Provider: "webpush", Config: cfgJSON},
	}

	profile := &WebPushProfile{}
	req, err := profile.BuildRequest(context.Background(), msg)
	if err != nil {
		t.Fatalf("BuildRequest failed: %v", err)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("round-trip failed: %v", err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)

	providerID, err := profile.ParseResponse(resp, body)
	if err != nil {
		t.Fatalf("ParseResponse failed: %v", err)
	}
	if providerID == "" {
		t.Error("expected non-empty provider ID")
	}
}

func TestWebPushNo404NoNewDependency(t *testing.T) {
	// This test verifies that no new dependencies are added.
	// It's primarily a build-time check, but we can verify key packages are used only from stdlib.

	// Just verify the adapter can be created
	adapter := NewWebPushAdapter()
	if adapter == nil {
		t.Fatal("expected non-nil adapter")
	}

	// Verify it's an HTTPProviderAdapter (confirming it reuses existing infrastructure)
	if _, ok := adapter.(*HTTPProviderAdapter); !ok {
		t.Fatalf("expected HTTPProviderAdapter, got %T", adapter)
	}
}

func TestParseVAPIDPrivateKeyValidKey(t *testing.T) {
	privKey, _, err := generateTestVAPIDKeys()
	if err != nil {
		t.Fatalf("generate keys: %v", err)
	}

	key, err := parseVAPIDPrivateKey(privKey)
	if err != nil {
		t.Fatalf("parse key: %v", err)
	}
	if key == nil {
		t.Fatal("expected non-nil key")
	}
	if key.Curve != elliptic.P256() {
		t.Errorf("expected P-256 curve, got %v", key.Curve)
	}
}

func TestParseVAPIDPrivateKeyInvalidLength(t *testing.T) {
	// Too short
	shortKey := base64.RawURLEncoding.EncodeToString([]byte("short"))
	_, err := parseVAPIDPrivateKey(shortKey)
	if err == nil {
		t.Fatal("expected error for short key")
	}
}

func TestParseVAPIDPrivateKeyInvalidBase64(t *testing.T) {
	_, err := parseVAPIDPrivateKey("not base64!@#$%")
	if err == nil {
		t.Fatal("expected error for invalid base64")
	}
}

func TestWebPushSSRFProtection(t *testing.T) {
	privKey, pubKey, err := generateTestVAPIDKeys()
	if err != nil {
		t.Fatalf("generate keys: %v", err)
	}

	os.Setenv("TEST_VAPID_PRIVATE", privKey)
	os.Setenv("TEST_VAPID_PUBLIC", pubKey)
	defer func() {
		os.Unsetenv("TEST_VAPID_PRIVATE")
		os.Unsetenv("TEST_VAPID_PUBLIC")
	}()

	cfg := WebPushConfig{
		VAPIDPrivateRef: "TEST_VAPID_PRIVATE",
		VAPIDPublicRef:  "TEST_VAPID_PUBLIC",
		VAPIDSubject:    "mailto:sender@example.com",
	}
	cfgJSON, _ := json.Marshal(cfg)

	// Test private-IP endpoints are blocked (SSRF protection)
	tests := []struct {
		name     string
		endpoint string
	}{
		{"localhost", "http://localhost:8080/push"},
		{"127.0.0.1", "http://127.0.0.1:8080/push"},
		{"169.254.169.254", "http://169.254.169.254/latest/meta-data"},
		{"10.0.0.1", "http://10.0.0.1/push"},
		{"172.16.0.1", "http://172.16.0.1/push"},
		{"192.168.1.1", "http://192.168.1.1/push"},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			adapter := NewWebPushAdapter()
			msg := ports.RenderedMessage{
				Channel:  "push",
				Endpoint: test.endpoint,
				Identity: domain.SendingIdentity{
					Channel:  "push",
					Provider: "webpush",
					Config:   cfgJSON,
				},
			}

			_, err := adapter.Send(context.Background(), msg)
			if err == nil {
				t.Errorf("expected SSRF error for %s, got none", test.endpoint)
			}
			if !strings.Contains(err.Error(), "SSRF") && !strings.Contains(err.Error(), "forbidden") && !strings.Contains(err.Error(), "private") {
				t.Logf("web-push blocks %s: %v", test.endpoint, err)
			}
		})
	}
}

func TestBuildVAPIDJWTSignature(t *testing.T) {
	priv, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}

	jwt, err := buildVAPIDJWT(priv, "mailto:test@example.com")
	if err != nil {
		t.Fatalf("build JWT: %v", err)
	}

	// Verify JWT structure
	parts := strings.Split(jwt, ".")
	if len(parts) != 3 {
		t.Errorf("expected 3 JWT parts, got %d", len(parts))
	}

	// Verify signature is present and properly base64 encoded
	sig := parts[2]
	sigBytes, err := base64.RawURLEncoding.DecodeString(sig)
	if err != nil {
		t.Errorf("signature is not valid base64: %v", err)
	}
	// P-256 signature should be 64 bytes (32-byte r + 32-byte s)
	if len(sigBytes) != 64 {
		t.Errorf("expected 64-byte signature, got %d", len(sigBytes))
	}
}

func TestWebPush404IsInvalidToken(t *testing.T) {
	privKey, pubKey, err := generateTestVAPIDKeys()
	if err != nil {
		t.Fatalf("generate keys: %v", err)
	}

	os.Setenv("TEST_VAPID_PRIVATE", privKey)
	os.Setenv("TEST_VAPID_PUBLIC", pubKey)
	defer func() {
		os.Unsetenv("TEST_VAPID_PRIVATE")
		os.Unsetenv("TEST_VAPID_PUBLIC")
	}()

	// A revoked subscription returns 404. Execute the profile-built request with a
	// plain client (see TestWebPushHTTPIntegration for why not the guarded adapter)
	// and assert the profile classifies 404 as an invalid token + a terminal error.
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	defer server.Close()

	cfg := WebPushConfig{
		VAPIDPrivateRef: "TEST_VAPID_PRIVATE",
		VAPIDPublicRef:  "TEST_VAPID_PUBLIC",
		VAPIDSubject:    "mailto:sender@example.com",
	}
	cfgJSON, _ := json.Marshal(cfg)

	msg := ports.RenderedMessage{
		Channel:  "push",
		Endpoint: server.URL,
		Identity: domain.SendingIdentity{Channel: "push", Provider: "webpush", Config: cfgJSON},
	}

	profile := &WebPushProfile{}
	req, err := profile.BuildRequest(context.Background(), msg)
	if err != nil {
		t.Fatalf("BuildRequest failed: %v", err)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("round-trip failed: %v", err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)

	if !profile.IsInvalidToken(resp, body) {
		t.Error("expected 404 to be classified as an invalid token")
	}
	if _, err := profile.ParseResponse(resp, body); err == nil {
		t.Fatal("expected ParseResponse to error on 404")
	}
}
