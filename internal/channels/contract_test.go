package channels_test

// contract_test.go — Provider contract suite (Milestone 10.7.1)
//
// Table-driven tests asserting the contract for every ProviderProfile:
//   • request shape (URL prefix, method, content-type)
//   • auth header
//   • ParseResponse: 2xx → provider-id (non-empty), no error
//   • ParseResponse: 5xx → retryable DeliveryError
//   • ParseResponse: permanent 4xx → non-retryable DeliveryError
//   • IsInvalidToken: invalid-token response → true
//   • IsInvalidToken: generic error response → false

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/buildwithdmytro/openjourney/internal/channels"
	"github.com/buildwithdmytro/openjourney/internal/domain"
	"github.com/buildwithdmytro/openjourney/internal/ports"
)

// profileContract defines what every provider profile must satisfy.
type profileContract struct {
	name    string
	profile channels.ProviderProfile
	msg     ports.RenderedMessage

	// request assertions
	wantURLPrefix  string
	wantMethod     string
	wantAuthPrefix string // prefix of Authorization/authorization header value

	// response → provider-id assertions
	successResp    *http.Response
	successBody    []byte
	wantProviderID string // non-empty means we check for non-empty id

	// retryable 5xx
	retryableResp *http.Response
	retryableBody []byte

	// permanent 4xx
	permanentResp *http.Response
	permanentBody []byte

	// invalid-token detection
	invalidTokenResp *http.Response
	invalidTokenBody []byte
	genericErrResp   *http.Response
	genericErrBody   []byte
}

func resp(status int, body []byte, headers ...string) *http.Response {
	h := http.Header{}
	for i := 0; i+1 < len(headers); i += 2 {
		h.Set(headers[i], headers[i+1])
	}
	return &http.Response{StatusCode: status, Header: h}
}

func TestProviderContracts(t *testing.T) {
	pemStr := generateECDSAKeyPEM(t)

	fcmCfg, _ := json.Marshal(map[string]string{"project_id": "my-proj", "token": "bearer-tok"})
	apnsCfg, _ := json.Marshal(map[string]string{
		"private_key": pemStr,
		"key_id":      "K1",
		"team_id":     "T1",
		"topic":       "com.example.app",
	})
	twilioCfg := twilioConfig("AC123", "authtok", "")

	contracts := []profileContract{
		// ------------------------------------------------------------------
		// Twilio SMS
		// ------------------------------------------------------------------
		{
			name:    "twilio",
			profile: &channels.TwilioSMSProfile{},
			msg: ports.RenderedMessage{
				Channel: "sms", Endpoint: "+15005550001", Body: "Hello",
				Identity: domain.SendingIdentity{
					Channel: "sms", Provider: "twilio",
					FromAddress: func() *string { s := "+15005550006"; return &s }(),
					Config:      twilioCfg,
				},
			},
			wantURLPrefix:  "https://api.twilio.com/",
			wantMethod:     http.MethodPost,
			wantAuthPrefix: "Basic ",
			successResp:    resp(http.StatusCreated, nil),
			successBody:    []byte(`{"sid":"SM123","status":"queued"}`),
			wantProviderID: "SM123",
			retryableResp:  resp(http.StatusInternalServerError, nil),
			retryableBody:  []byte(`{"code":20500,"message":"Internal Server Error","status":500}`),
			permanentResp:  resp(http.StatusBadRequest, nil),
			permanentBody:  []byte(`{"code":21211,"message":"Invalid To phone number","status":400}`),
			// Twilio has no invalid-token concept for SMS — permanent error is the signal
			invalidTokenResp: resp(http.StatusBadRequest, nil),
			invalidTokenBody: []byte(`{"code":21211,"message":"Invalid number","status":400}`),
			genericErrResp:   resp(http.StatusInternalServerError, nil),
			genericErrBody:   []byte(`{"code":20500,"message":"Server error","status":500}`),
		},

		// ------------------------------------------------------------------
		// FCM push
		// ------------------------------------------------------------------
		{
			name:    "fcm",
			profile: &channels.FCMPushProfile{},
			msg: ports.RenderedMessage{
				Channel: "push", Endpoint: "device-tok-fcm", Title: "Hi", Body: "World",
				Identity: domain.SendingIdentity{
					Channel: "push", Provider: "fcm",
					Config: fcmCfg,
				},
			},
			wantURLPrefix:  "https://fcm.googleapis.com/",
			wantMethod:     http.MethodPost,
			wantAuthPrefix: "Bearer ",
			successResp:    resp(http.StatusOK, nil),
			successBody:    []byte(`{"name":"projects/my-proj/messages/msg-999"}`),
			wantProviderID: "projects/my-proj/messages/msg-999",
			retryableResp:  resp(http.StatusInternalServerError, nil),
			retryableBody:  []byte(`{"error":{"code":500,"message":"Internal","status":"INTERNAL"}}`),
			permanentResp:  resp(http.StatusBadRequest, nil),
			permanentBody:  []byte(`{"error":{"code":400,"message":"Bad payload","status":"INVALID_ARGUMENT"}}`),
			invalidTokenResp: resp(http.StatusNotFound, nil),
			invalidTokenBody: []byte(`{"error":{"code":404,"message":"token not found","status":"UNREGISTERED"}}`),
			genericErrResp:  resp(http.StatusInternalServerError, nil),
			genericErrBody:  []byte(`{"error":{"code":500,"message":"Internal","status":"INTERNAL"}}`),
		},

		// ------------------------------------------------------------------
		// APNs push
		// ------------------------------------------------------------------
		{
			name:    "apns",
			profile: &channels.APNsPushProfile{},
			msg: ports.RenderedMessage{
				Channel: "push", Endpoint: "device-tok-apns", Title: "Hi", Body: "World",
				Identity: domain.SendingIdentity{
					Channel: "push", Provider: "apns",
					Config: apnsCfg,
				},
			},
			wantURLPrefix:  "https://api.push.apple.com/",
			wantMethod:     http.MethodPost,
			wantAuthPrefix: "bearer ",
			successResp:    resp(http.StatusOK, nil, "Apns-Id", "uuid-abc"),
			successBody:    nil,
			wantProviderID: "uuid-abc",
			retryableResp:  resp(http.StatusInternalServerError, nil),
			retryableBody:  []byte(`{"reason":"InternalServerError"}`),
			permanentResp:  resp(http.StatusBadRequest, nil),
			permanentBody:  []byte(`{"reason":"BadPayload"}`),
			invalidTokenResp: resp(http.StatusGone, nil),
			invalidTokenBody: []byte(`{"reason":"Unregistered"}`),
			genericErrResp:  resp(http.StatusInternalServerError, nil),
			genericErrBody:  []byte(`{"reason":"InternalServerError"}`),
		},

		// ------------------------------------------------------------------
		// HTTP generic (webhook)
		// ------------------------------------------------------------------
		{
			name: "http",
			profile: &channels.HTTPGenericProfile{},
			msg: ports.RenderedMessage{
				Channel: "webhook", Endpoint: "tok",
				Body:    `{"user":"1"}`,
				Identity: domain.SendingIdentity{
					Channel: "webhook", Provider: "http",
					Config: func() []byte {
						// HTTPGenericProfile reads endpoint from identity config
						b, _ := json.Marshal(map[string]string{"endpoint": "https://example.com/hook"})
						return b
					}(),
				},
			},
			wantURLPrefix:  "https://example.com/",
			wantMethod:     http.MethodPost,
			wantAuthPrefix: "", // no auth header by default
			successResp:    resp(http.StatusOK, nil),
			successBody:    nil,
			wantProviderID: "", // generic profile returns status-code string — just check no error
			retryableResp:  resp(http.StatusInternalServerError, nil),
			retryableBody:  nil,
			permanentResp:  resp(http.StatusBadRequest, nil),
			permanentBody:  nil,
			// HTTP generic never flags invalid-token
			invalidTokenResp: resp(http.StatusGone, nil),
			invalidTokenBody: nil,
			genericErrResp:   resp(http.StatusInternalServerError, nil),
			genericErrBody:   nil,
		},

		// ------------------------------------------------------------------
		// Fake (test double)
		// ------------------------------------------------------------------
		{
			name:    "fake",
			profile: &channels.FakeProviderProfile{},
			msg: ports.RenderedMessage{
				Channel: "push", Endpoint: "fake-endpoint",
				Identity: domain.SendingIdentity{Channel: "push", Provider: "fake"},
			},
			wantURLPrefix:  "https://fake.provider.invalid/",
			wantMethod:     http.MethodPost,
			wantAuthPrefix: "", // fake profile sets Content-Type but no Authorization
			successResp:    resp(http.StatusOK, nil),
			successBody:    []byte(`{"endpoint":"fake-endpoint"}`),
			wantProviderID: "", // returns status-code string, just check no error
			retryableResp:  resp(http.StatusServiceUnavailable, nil),
			retryableBody:  nil,
			permanentResp:  resp(http.StatusBadRequest, nil),
			permanentBody:  nil,
			// FakeProviderProfile.IsInvalidToken always returns false
			invalidTokenResp: resp(http.StatusGone, nil),
			invalidTokenBody: nil,
			genericErrResp:   resp(http.StatusInternalServerError, nil),
			genericErrBody:   nil,
		},
	}

	for _, c := range contracts {
		c := c
		t.Run(c.name, func(t *testing.T) {
			// ---- 1. Request shape & auth ----
			req, err := c.profile.BuildRequest(context.Background(), c.msg)
			if err != nil {
				t.Fatalf("BuildRequest: %v", err)
			}
			if !strings.HasPrefix(req.URL.String(), c.wantURLPrefix) {
				t.Errorf("URL: got %q, want prefix %q", req.URL.String(), c.wantURLPrefix)
			}
			if req.Method != c.wantMethod {
				t.Errorf("method: got %q, want %q", req.Method, c.wantMethod)
			}
			if c.wantAuthPrefix != "" {
				authHeader := req.Header.Get("Authorization")
				if authHeader == "" {
					authHeader = req.Header.Get("authorization")
				}
				if !strings.HasPrefix(authHeader, c.wantAuthPrefix) {
					t.Errorf("auth header: got %q, want prefix %q", authHeader, c.wantAuthPrefix)
				}
			}

			// ---- 2. Success → provider-id ----
			id, err := c.profile.ParseResponse(c.successResp, c.successBody)
			if err != nil {
				t.Errorf("ParseResponse(success): unexpected error: %v", err)
			}
			if c.wantProviderID != "" && id != c.wantProviderID {
				t.Errorf("ParseResponse(success): id=%q, want %q", id, c.wantProviderID)
			}

			// ---- 3. 5xx → retryable DeliveryError ----
			_, err = c.profile.ParseResponse(c.retryableResp, c.retryableBody)
			if err == nil {
				t.Errorf("ParseResponse(5xx): expected error, got nil")
			} else {
				var de *channels.DeliveryError
				if !errors.As(err, &de) {
					t.Errorf("ParseResponse(5xx): expected *DeliveryError, got %T", err)
				} else if !de.Retryable {
					t.Errorf("ParseResponse(5xx): expected Retryable=true, got false")
				}
			}

			// ---- 4. permanent 4xx → non-retryable DeliveryError ----
			_, err = c.profile.ParseResponse(c.permanentResp, c.permanentBody)
			if err == nil {
				t.Errorf("ParseResponse(permanent 4xx): expected error, got nil")
			} else {
				var de *channels.DeliveryError
				if !errors.As(err, &de) {
					t.Errorf("ParseResponse(permanent 4xx): expected *DeliveryError, got %T", err)
				} else if de.Retryable {
					t.Errorf("ParseResponse(permanent 4xx): expected Retryable=false, got true")
				}
			}

			// ---- 5. IsInvalidToken: invalid-token response ----
			// (Twilio, HTTP, and Fake never set this flag; for those we just check it doesn't panic)
			_ = c.profile.IsInvalidToken(c.invalidTokenResp, c.invalidTokenBody)
			// For FCM and APNs specifically, assert true
			if c.name == "fcm" || c.name == "apns" {
				if !c.profile.IsInvalidToken(c.invalidTokenResp, c.invalidTokenBody) {
					t.Errorf("IsInvalidToken(invalid resp): expected true for %s", c.name)
				}
			}

			// ---- 6. IsInvalidToken: generic error → false ----
			if c.profile.IsInvalidToken(c.genericErrResp, c.genericErrBody) {
				t.Errorf("IsInvalidToken(generic error): expected false for %s", c.name)
			}
		})
	}
}

// TestTwilioAuth_IsBasicAuthWithAccountSIDAndToken verifies the Authorization
// header is Basic base64(accountSID:authToken) — a critical Twilio contract.
func TestTwilioAuth_IsBasicAuthWithAccountSIDAndToken(t *testing.T) {
	const accountSID = "AC123456"
	const authToken = "secret789"
	cfg := twilioConfig(accountSID, authToken, "")
	msg := ports.RenderedMessage{
		Channel: "sms", Endpoint: "+15005550001", Body: "test",
		Identity: domain.SendingIdentity{
			Channel: "sms", Provider: "twilio",
			FromAddress: func() *string { s := "+15005550006"; return &s }(),
			Config:      cfg,
		},
	}
	profile := &channels.TwilioSMSProfile{}
	req, err := profile.BuildRequest(context.Background(), msg)
	if err != nil {
		t.Fatalf("BuildRequest: %v", err)
	}
	auth := req.Header.Get("Authorization")
	if !strings.HasPrefix(auth, "Basic ") {
		t.Fatalf("Authorization: got %q, want Basic ...", auth)
	}
	decoded, err := base64.StdEncoding.DecodeString(strings.TrimPrefix(auth, "Basic "))
	if err != nil {
		t.Fatalf("decode Basic: %v", err)
	}
	wantCreds := accountSID + ":" + authToken
	if string(decoded) != wantCreds {
		t.Errorf("Basic creds: got %q, want %q", string(decoded), wantCreds)
	}
}

// TestFCMAuth_IsBearerToken verifies FCM sends Authorization: Bearer <token>.
func TestFCMAuth_IsBearerToken(t *testing.T) {
	const bearerToken = "ya29.fake-oauth-token"
	cfg, _ := json.Marshal(map[string]string{"project_id": "proj", "token": bearerToken})
	msg := ports.RenderedMessage{
		Channel: "push", Endpoint: "device-tok", Title: "T", Body: "B",
		Identity: domain.SendingIdentity{
			Channel: "push", Provider: "fcm",
			Config: cfg,
		},
	}
	profile := &channels.FCMPushProfile{}
	req, err := profile.BuildRequest(context.Background(), msg)
	if err != nil {
		t.Fatalf("BuildRequest: %v", err)
	}
	auth := req.Header.Get("Authorization")
	if auth != "Bearer "+bearerToken {
		t.Errorf("Authorization: got %q, want %q", auth, "Bearer "+bearerToken)
	}
}

// TestAPNsAuth_IsES256JWT verifies APNs sends a 3-part JWT in the authorization header.
func TestAPNsAuth_IsES256JWT(t *testing.T) {
	pemStr := generateECDSAKeyPEM(t)
	apnsCfg, _ := json.Marshal(map[string]string{
		"private_key": pemStr, "key_id": "K1", "team_id": "T1", "topic": "com.example.app",
	})
	msg := ports.RenderedMessage{
		Channel: "push", Endpoint: "device-tok", Title: "T", Body: "B",
		Identity: domain.SendingIdentity{
			Channel: "push", Provider: "apns",
			Config: apnsCfg,
		},
	}
	profile := &channels.APNsPushProfile{}
	req, err := profile.BuildRequest(context.Background(), msg)
	if err != nil {
		t.Fatalf("BuildRequest: %v", err)
	}
	auth := req.Header.Get("authorization")
	if !strings.HasPrefix(auth, "bearer ") {
		t.Fatalf("authorization: got %q, want 'bearer <jwt>'", auth)
	}
	token := strings.TrimPrefix(auth, "bearer ")
	parts := strings.Split(token, ".")
	if len(parts) != 3 {
		t.Fatalf("JWT: expected 3 parts, got %d", len(parts))
	}
	headerBytes, err := base64.RawURLEncoding.DecodeString(parts[0])
	if err != nil {
		t.Fatalf("decode JWT header: %v", err)
	}
	var header map[string]string
	if err := json.Unmarshal(headerBytes, &header); err != nil {
		t.Fatalf("unmarshal JWT header: %v", err)
	}
	if header["alg"] != "ES256" {
		t.Errorf("JWT alg: got %q, want ES256", header["alg"])
	}
	if header["kid"] != "K1" {
		t.Errorf("JWT kid: got %q, want K1", header["kid"])
	}
}

// TestHTTPGeneric_ContentTypeIsJSON verifies the HTTP generic profile sets Content-Type: application/json.
func TestHTTPGeneric_ContentTypeIsJSON(t *testing.T) {
	cfg, _ := json.Marshal(map[string]string{"endpoint": "https://example.com/hook"})
	msg := ports.RenderedMessage{
		Channel: "webhook", Endpoint: "tok",
		Body:    `{"user":"1"}`,
		Identity: domain.SendingIdentity{
			Channel: "webhook", Provider: "http",
			Config:  cfg,
		},
	}
	profile := &channels.HTTPGenericProfile{}
	req, err := profile.BuildRequest(context.Background(), msg)
	if err != nil {
		t.Fatalf("BuildRequest: %v", err)
	}
	ct := req.Header.Get("Content-Type")
	if ct != "application/json" {
		t.Errorf("Content-Type: got %q, want application/json", ct)
	}
	// Verify body is readable and contains channel field
	body, _ := io.ReadAll(req.Body)
	if !bytes.Contains(body, []byte("channel")) {
		t.Errorf("body missing 'channel' field: %q", string(body))
	}
}

// TestFakeProfile_ContractRoundTrip verifies the fake profile behaves correctly.
func TestFakeProfile_ContractRoundTrip(t *testing.T) {
	profile := &channels.FakeProviderProfile{}
	msg := ports.RenderedMessage{
		Channel: "push", Endpoint: "tok",
		Identity: domain.SendingIdentity{Channel: "push", Provider: "fake"},
	}
	req, err := profile.BuildRequest(context.Background(), msg)
	if err != nil {
		t.Fatalf("BuildRequest: %v", err)
	}
	if req == nil {
		t.Fatal("BuildRequest returned nil request")
	}
	if len(profile.Requests) != 1 {
		t.Errorf("expected 1 recorded request, got %d", len(profile.Requests))
	}
	// ParseResponse 200 → no error
	_, err = profile.ParseResponse(resp(http.StatusOK, nil), []byte(`{"endpoint":"tok"}`))
	if err != nil {
		t.Errorf("ParseResponse(200): unexpected error: %v", err)
	}
	// ParseResponse 5xx → retryable
	_, err = profile.ParseResponse(resp(http.StatusServiceUnavailable, nil), nil)
	var de *channels.DeliveryError
	if !errors.As(err, &de) || !de.Retryable {
		t.Errorf("ParseResponse(5xx): expected retryable DeliveryError, got %v", err)
	}
	// IsInvalidToken always false for fake
	if profile.IsInvalidToken(resp(http.StatusGone, nil), nil) {
		t.Error("IsInvalidToken: expected false for fake profile")
	}
}

