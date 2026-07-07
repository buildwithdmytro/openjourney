package channels

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/buildwithdmytro/openjourney/internal/domain"
	"github.com/buildwithdmytro/openjourney/internal/ports"
)

type mockRoundTripper struct {
	RoundTripFunc func(req *http.Request) (*http.Response, error)
}

func (m *mockRoundTripper) RoundTrip(req *http.Request) (*http.Response, error) {
	return m.RoundTripFunc(req)
}

func TestWebhookAdapter_SSRFBlocks(t *testing.T) {
	w := NewWebhookAdapter()

	// Try sending to loopback
	msgPrivate := ports.RenderedMessage{
		Channel:  "webhook",
		Endpoint: "http://127.0.0.1:8080/webhook",
		Body:     `{"hello":"world"}`,
		Identity: domain.SendingIdentity{
			Channel:  "webhook",
			Verified: true,
		},
	}

	_, err := w.Send(context.Background(), msgPrivate)
	if err == nil {
		t.Fatal("expected Send to fail for loopback address 127.0.0.1")
	}
	if !strings.Contains(err.Error(), "SSRF safety validation") && !strings.Contains(err.Error(), "forbidden") && !strings.Contains(err.Error(), "private") {
		t.Errorf("expected SSRF block error message, got: %v", err)
	}

	// Try sending to AWS metadata link-local IP
	msgLinkLocal := msgPrivate
	msgLinkLocal.Endpoint = "http://169.254.169.254/latest/meta-data"
	_, err = w.Send(context.Background(), msgLinkLocal)
	if err == nil {
		t.Fatal("expected Send to fail for AWS metadata link-local address 169.254.169.254")
	}
}

func TestWebhookAdapter_HMACSignature(t *testing.T) {
	w := NewWebhookAdapter()

	secret := "supersecret"
	body := `{"event":"user_created"}`

	var capturedHeader string
	var capturedBody string

	w.client.Transport = &mockRoundTripper{
		RoundTripFunc: func(req *http.Request) (*http.Response, error) {
			capturedHeader = req.Header.Get("X-Signature")
			bodyBytes, _ := io.ReadAll(req.Body)
			capturedBody = string(bodyBytes)

			return &http.Response{
				StatusCode: 200,
				Body:       io.NopCloser(bytes.NewBufferString(`{"ok":true}`)),
			}, nil
		},
	}

	cfgBytes, _ := json.Marshal(WebhookConfig{
		Endpoint:   "http://example.com/callback", // standard public/safe URL host
		HMACSecret: secret,
	})

	msg := ports.RenderedMessage{
		Channel:  "webhook",
		Endpoint: "http://example.com/callback",
		Body:     body,
		Identity: domain.SendingIdentity{
			Channel:  "webhook",
			Verified: true,
			Config:   cfgBytes,
		},
	}

	_, err := w.Send(context.Background(), msg)
	if err != nil {
		t.Fatalf("unexpected Send error: %v", err)
	}

	if capturedBody != body {
		t.Errorf("expected body %q, got %q", body, capturedBody)
	}

	// Calculate expected signature
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(body))
	expectedSig := hex.EncodeToString(mac.Sum(nil))

	if capturedHeader != expectedSig {
		t.Errorf("expected X-Signature %s, got %s", expectedSig, capturedHeader)
	}
}

func TestWebhookAdapter_ValidateConfig(t *testing.T) {
	w := NewWebhookAdapter()

	safeCfg, _ := json.Marshal(WebhookConfig{
		Endpoint: "https://example.com/safe",
	})
	safeIden := domain.SendingIdentity{
		Channel:  "webhook",
		Verified: true,
		Config:   safeCfg,
	}

	if err := w.ValidateConfig(safeIden); err != nil {
		t.Fatalf("expected valid webhook config to pass validation, got: %v", err)
	}

	unsafeCfg, _ := json.Marshal(WebhookConfig{
		Endpoint: "http://127.0.0.1/private",
	})
	unsafeIden := safeIden
	unsafeIden.Config = unsafeCfg

	if err := w.ValidateConfig(unsafeIden); err == nil {
		t.Fatal("expected private ip config to fail validation")
	}
}
