package channels_test

import (
	"bytes"
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"encoding/json"
	"encoding/pem"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"

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
