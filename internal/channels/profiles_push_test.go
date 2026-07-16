package channels_test

import (
	"bytes"
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/buildwithdmytro/openjourney/internal/channels"
	"github.com/buildwithdmytro/openjourney/internal/domain"
	"github.com/buildwithdmytro/openjourney/internal/ports"
)

func generateECDSAKeyPEM(t *testing.T) string {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	privBytes, err := x509.MarshalPKCS8PrivateKey(key)
	if err != nil {
		t.Fatal(err)
	}
	block := &pem.Block{
		Type:  "PRIVATE KEY",
		Bytes: privBytes,
	}
	var buf bytes.Buffer
	if err := pem.Encode(&buf, block); err != nil {
		t.Fatal(err)
	}
	return buf.String()
}

func TestFCMPushProfile_BuildRequest(t *testing.T) {
	cfgMap := map[string]string{
		"project_id": "my-fcm-project",
		"token":      "fake-bearer-token",
	}
	cfgBytes, _ := json.Marshal(cfgMap)

	msg := ports.RenderedMessage{
		Channel:  "push",
		Endpoint: "token-fcm-123",
		Title:    "Hello FCM",
		Body:     "Body text",
		Data: map[string]string{
			"key1": "val1",
		},
		Identity: domain.SendingIdentity{
			Channel:  "push",
			Provider: "fcm",
			Config:   cfgBytes,
		},
	}

	profile := &channels.FCMPushProfile{}
	req, err := profile.BuildRequest(context.Background(), msg)
	if err != nil {
		t.Fatal(err)
	}

	if req.Method != http.MethodPost {
		t.Errorf("method: got %s, want POST", req.Method)
	}

	wantURL := "https://fcm.googleapis.com/v1/projects/my-fcm-project/messages:send"
	if req.URL.String() != wantURL {
		t.Errorf("URL: got %s, want %s", req.URL.String(), wantURL)
	}

	authHeader := req.Header.Get("Authorization")
	if authHeader != "Bearer fake-bearer-token" {
		t.Errorf("Authorization: got %q, want Bearer fake-bearer-token", authHeader)
	}

	bodyBytes, _ := io.ReadAll(req.Body)
	var bodyMap map[string]any
	if err := json.Unmarshal(bodyBytes, &bodyMap); err != nil {
		t.Fatal(err)
	}

	msgMap := bodyMap["message"].(map[string]any)
	if msgMap["token"] != "token-fcm-123" {
		t.Errorf("token: got %v, want token-fcm-123", msgMap["token"])
	}

	notifMap := msgMap["notification"].(map[string]any)
	if notifMap["title"] != "Hello FCM" || notifMap["body"] != "Body text" {
		t.Errorf("notification: got title=%v body=%v", notifMap["title"], notifMap["body"])
	}

	dataMap := msgMap["data"].(map[string]any)
	if dataMap["key1"] != "val1" {
		t.Errorf("data: got %v, want val1", dataMap["key1"])
	}
}

func TestFCMPushProfile_ParseResponse(t *testing.T) {
	profile := &channels.FCMPushProfile{}

	t.Run("success", func(t *testing.T) {
		resp := &http.Response{StatusCode: http.StatusOK}
		body := []byte(`{"name": "projects/my-fcm-project/messages/msg-id-777"}`)
		id, err := profile.ParseResponse(resp, body)
		if err != nil {
			t.Fatal(err)
		}
		if id != "projects/my-fcm-project/messages/msg-id-777" {
			t.Errorf("got id %q, want projects/my-fcm-project/messages/msg-id-777", id)
		}
	})

	t.Run("invalid token error", func(t *testing.T) {
		resp := &http.Response{StatusCode: http.StatusNotFound}
		body := []byte(`{"error": {"status": "NOT_FOUND", "message": "Requested entity was not found."}}`)
		id, err := profile.ParseResponse(resp, body)
		if err == nil {
			t.Fatal("expected error, got nil")
		}
		if id != "" {
			t.Errorf("expected empty id, got %q", id)
		}
		var delErr *channels.DeliveryError
		if !errors.As(err, &delErr) {
			t.Fatalf("expected DeliveryError, got %T", err)
		}
		if delErr.Retryable {
			t.Error("expected error to be permanent (Retryable=false) for invalid token")
		}
		if !profile.IsInvalidToken(resp, body) {
			t.Error("expected IsInvalidToken to be true")
		}
	})
}

func TestAPNsPushProfile_BuildRequest(t *testing.T) {
	pemStr := generateECDSAKeyPEM(t)
	cfgMap := map[string]any{
		"private_key": pemStr,
		"key_id":      "key123",
		"team_id":     "teamabc",
		"topic":       "com.example.app",
		"sandbox":     true,
	}
	cfgBytes, _ := json.Marshal(cfgMap)

	msg := ports.RenderedMessage{
		Channel:  "push",
		Endpoint: "token-apns-456",
		Title:    "Hello APNs",
		Body:     "Body text",
		Data: map[string]string{
			"custom_payload_key": "val2",
		},
		Identity: domain.SendingIdentity{
			Channel:  "push",
			Provider: "apns",
			Config:   cfgBytes,
		},
	}

	profile := &channels.APNsPushProfile{}
	req, err := profile.BuildRequest(context.Background(), msg)
	if err != nil {
		t.Fatal(err)
	}

	if req.Method != http.MethodPost {
		t.Errorf("method: got %s, want POST", req.Method)
	}

	wantURL := "https://api.sandbox.push.apple.com/3/device/token-apns-456"
	if req.URL.String() != wantURL {
		t.Errorf("URL: got %s, want %s", req.URL.String(), wantURL)
	}

	authHeader := req.Header.Get("authorization")
	if !strings.HasPrefix(authHeader, "bearer ") {
		t.Errorf("authorization header: got %q, expected 'bearer <jwt>'", authHeader)
	}

	if topic := req.Header.Get("apns-topic"); topic != "com.example.app" {
		t.Errorf("apns-topic: got %q, want com.example.app", topic)
	}

	bodyBytes, _ := io.ReadAll(req.Body)
	var bodyMap map[string]any
	if err := json.Unmarshal(bodyBytes, &bodyMap); err != nil {
		t.Fatal(err)
	}

	aps := bodyMap["aps"].(map[string]any)
	alert := aps["alert"].(map[string]any)
	if alert["title"] != "Hello APNs" || alert["body"] != "Body text" {
		t.Errorf("alert: got title=%v body=%v", alert["title"], alert["body"])
	}

	if bodyMap["custom_payload_key"] != "val2" {
		t.Errorf("custom key: got %v, want val2", bodyMap["custom_payload_key"])
	}
}

func TestAPNsPushProfile_ParseResponse(t *testing.T) {
	profile := &channels.APNsPushProfile{}

	t.Run("success", func(t *testing.T) {
		resp := &http.Response{
			StatusCode: http.StatusOK,
			Header:     http.Header{"Apns-Id": []string{"apns-msg-uuid"}},
		}
		id, err := profile.ParseResponse(resp, nil)
		if err != nil {
			t.Fatal(err)
		}
		if id != "apns-msg-uuid" {
			t.Errorf("got id %q, want apns-msg-uuid", id)
		}
	})

	t.Run("bad device token", func(t *testing.T) {
		resp := &http.Response{StatusCode: http.StatusBadRequest}
		body := []byte(`{"reason": "BadDeviceToken"}`)
		id, err := profile.ParseResponse(resp, body)
		if err == nil {
			t.Fatal("expected error, got nil")
		}
		if id != "" {
			t.Errorf("expected empty id, got %q", id)
		}
		var delErr *channels.DeliveryError
		if !errors.As(err, &delErr) {
			t.Fatalf("expected DeliveryError, got %T", err)
		}
		if delErr.Retryable {
			t.Error("expected error to be permanent (Retryable=false)")
		}
		if !profile.IsInvalidToken(resp, body) {
			t.Error("expected IsInvalidToken to be true")
		}
	})
}

func TestGenerateAPNsJWT(t *testing.T) {
	pemStr := generateECDSAKeyPEM(t)

	// Parse the public key for signature verification.
	block, _ := pem.Decode([]byte(pemStr))
	if block == nil {
		t.Fatal("failed to decode PEM")
	}
	privRaw, err := x509.ParsePKCS8PrivateKey(block.Bytes)
	if err != nil {
		t.Fatal(err)
	}
	ecKey := privRaw.(*ecdsa.PrivateKey)

	now := time.Date(2025, 1, 15, 12, 0, 0, 0, time.UTC)

	cases := []struct {
		name   string
		keyID  string
		teamID string
	}{
		{"standard", "KEY123", "TEAM456"},
		{"different ids", "ABCDE", "FGHIJ"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			token, err := channels.GenerateAPNsJWT(pemStr, tc.keyID, tc.teamID, now)
			if err != nil {
				t.Fatalf("GenerateAPNsJWT error: %v", err)
			}

			parts := strings.Split(token, ".")
			if len(parts) != 3 {
				t.Fatalf("expected 3 JWT parts, got %d: %s", len(parts), token)
			}

			// Decode and check header
			headerBytes, err := base64.RawURLEncoding.DecodeString(parts[0])
			if err != nil {
				t.Fatalf("decode header: %v", err)
			}
			var header map[string]string
			if err := json.Unmarshal(headerBytes, &header); err != nil {
				t.Fatalf("unmarshal header: %v", err)
			}
			if header["alg"] != "ES256" {
				t.Errorf("alg: got %q, want ES256", header["alg"])
			}
			if header["kid"] != tc.keyID {
				t.Errorf("kid: got %q, want %q", header["kid"], tc.keyID)
			}

			// Decode and check claims
			claimsBytes, err := base64.RawURLEncoding.DecodeString(parts[1])
			if err != nil {
				t.Fatalf("decode claims: %v", err)
			}
			var claims map[string]any
			if err := json.Unmarshal(claimsBytes, &claims); err != nil {
				t.Fatalf("unmarshal claims: %v", err)
			}
			if claims["iss"] != tc.teamID {
				t.Errorf("iss: got %v, want %q", claims["iss"], tc.teamID)
			}
			if iat, ok := claims["iat"].(float64); !ok || int64(iat) != now.Unix() {
				t.Errorf("iat: got %v, want %d", claims["iat"], now.Unix())
			}

			// Verify ES256 signature over header.claims
			sigBytes, err := base64.RawURLEncoding.DecodeString(parts[2])
			if err != nil {
				t.Fatalf("decode sig: %v", err)
			}
			signingInput := parts[0] + "." + parts[1]
			h := sha256.New()
			h.Write([]byte(signingInput))
			digest := h.Sum(nil)
			if !ecdsa.VerifyASN1(&ecKey.PublicKey, digest, sigBytes) {
				t.Error("ES256 signature verification failed")
			}
		})
	}
}

