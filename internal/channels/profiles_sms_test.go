package channels_test

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/url"
	"strings"
	"testing"

	"github.com/buildwithdmytro/openjourney/internal/channels"
	"github.com/buildwithdmytro/openjourney/internal/domain"
	"github.com/buildwithdmytro/openjourney/internal/ports"
)

// ptr is a helper to take a pointer to a string literal.
func ptr(s string) *string { return &s }

// twilioConfig builds a TwilioConfig JSON blob for embedding in SendingIdentity.Config.
func twilioConfig(accountSID, authToken, statusCallback string) []byte {
	m := map[string]string{
		"account_sid": accountSID,
		"auth_token":  authToken,
	}
	if statusCallback != "" {
		m["status_callback"] = statusCallback
	}
	b, _ := json.Marshal(m)
	return b
}

// makeTwilioMsg builds a RenderedMessage suitable for the Twilio profile.
func makeTwilioMsg(to, from, body string, cfg []byte) ports.RenderedMessage {
	return ports.RenderedMessage{
		Channel:        "sms",
		Endpoint:       to,
		Body:           body,
		IdempotencyKey: "idem-test-123",
		Identity: domain.SendingIdentity{
			Channel:     "sms",
			Provider:    "twilio",
			FromAddress: ptr(from),
			Config:      cfg,
		},
	}
}

// parseTwilioRequest is a test helper: builds the Twilio request and parses its form body.
func parseTwilioRequest(t *testing.T, msg ports.RenderedMessage) (*http.Request, url.Values) {
	t.Helper()
	profile := &channels.TwilioSMSProfile{}
	req, err := profile.BuildRequest(context.Background(), msg)
	if err != nil {
		t.Fatalf("BuildRequest: %v", err)
	}
	bodyBytes, _ := io.ReadAll(req.Body)
	form, _ := url.ParseQuery(string(bodyBytes))
	return req, form
}

// -----------------------------------------------------------------------
// TwilioSMSProfile — BuildRequest table tests
// -----------------------------------------------------------------------

func TestTwilioSMSProfile_BuildRequest_Shape(t *testing.T) {
	cfg := twilioConfig("AC123", "tok456", "https://hooks.example.com/sms/twilio")
	msg := makeTwilioMsg("+15005550001", "+15005550006", "Hello there!", cfg)

	req, form := parseTwilioRequest(t, msg)

	// Method and URL
	if req.Method != http.MethodPost {
		t.Errorf("method: got %s, want POST", req.Method)
	}
	wantURL := "https://api.twilio.com/2010-04-01/Accounts/AC123/Messages.json"
	if req.URL.String() != wantURL {
		t.Errorf("URL: got %s, want %s", req.URL.String(), wantURL)
	}

	// Content-Type must be form-encoded
	if ct := req.Header.Get("Content-Type"); ct != "application/x-www-form-urlencoded" {
		t.Errorf("Content-Type: got %q, want application/x-www-form-urlencoded", ct)
	}

	// Basic auth must be set
	user, pass, ok := req.BasicAuth()
	if !ok {
		t.Error("expected Basic auth to be set")
	}
	if user != "AC123" || pass != "tok456" {
		t.Errorf("BasicAuth: got user=%s pass=%s, want AC123/tok456", user, pass)
	}

	// Idempotency token
	if got := req.Header.Get("X-Twilio-Idempotency-Token"); got != "idem-test-123" {
		t.Errorf("X-Twilio-Idempotency-Token: got %q, want idem-test-123", got)
	}

	// Form fields
	if got := form.Get("To"); got != "+15005550001" {
		t.Errorf("To: got %q, want +15005550001", got)
	}
	if got := form.Get("From"); got != "+15005550006" {
		t.Errorf("From: got %q, want +15005550006", got)
	}
	if got := form.Get("Body"); got != "Hello there!" {
		t.Errorf("Body: got %q, want 'Hello there!'", got)
	}
	if got := form.Get("StatusCallback"); got != "https://hooks.example.com/sms/twilio" {
		t.Errorf("StatusCallback: got %q, want the callback URL", got)
	}
}

func TestTwilioSMSProfile_BuildRequest_NoStatusCallback(t *testing.T) {
	cfg := twilioConfig("ACID", "TOKID", "")
	msg := makeTwilioMsg("+15005550001", "+15005550006", "Test", cfg)
	_, form := parseTwilioRequest(t, msg)
	if form.Get("StatusCallback") != "" {
		t.Error("StatusCallback should be absent when not configured")
	}
}

func TestTwilioSMSProfile_BuildRequest_MissingAccountSID(t *testing.T) {
	cfg := twilioConfig("", "tok456", "")
	profile := &channels.TwilioSMSProfile{}
	_, err := profile.BuildRequest(context.Background(), makeTwilioMsg("+1234", "+5678", "hi", cfg))
	if err == nil {
		t.Error("expected error when account_sid is missing")
	}
}

func TestTwilioSMSProfile_BuildRequest_MissingAuthToken(t *testing.T) {
	cfg := twilioConfig("AC123", "", "")
	profile := &channels.TwilioSMSProfile{}
	_, err := profile.BuildRequest(context.Background(), makeTwilioMsg("+1234", "+5678", "hi", cfg))
	if err == nil {
		t.Error("expected error when auth_token is missing")
	}
}

func TestTwilioSMSProfile_BuildRequest_MissingTo(t *testing.T) {
	cfg := twilioConfig("AC123", "tok", "")
	profile := &channels.TwilioSMSProfile{}
	msg := makeTwilioMsg("", "+15005550006", "hi", cfg) // empty endpoint
	_, err := profile.BuildRequest(context.Background(), msg)
	if err == nil {
		t.Error("expected error when To phone number is missing")
	}
}

// -----------------------------------------------------------------------
// TwilioSMSProfile — ParseResponse table tests
// -----------------------------------------------------------------------

func TestTwilioSMSProfile_ParseResponse_201_Success(t *testing.T) {
	profile := &channels.TwilioSMSProfile{}
	body := []byte(`{"sid":"SMabc123","status":"queued","to":"+15005550001"}`)
	resp := &http.Response{StatusCode: 201}

	sid, err := profile.ParseResponse(resp, body)
	if err != nil {
		t.Fatalf("expected success on 201, got: %v", err)
	}
	if sid != "SMabc123" {
		t.Errorf("sid: got %q, want SMabc123", sid)
	}
}

func TestTwilioSMSProfile_ParseResponse_5xx_Retryable(t *testing.T) {
	profile := &channels.TwilioSMSProfile{}
	body := []byte(`{"code":0,"message":"Internal server error","status":500}`)

	for _, status := range []int{500, 503} {
		resp := &http.Response{StatusCode: status}
		_, err := profile.ParseResponse(resp, body)
		if err == nil {
			t.Fatalf("status %d: expected error", status)
		}
		if !channels.IsRetryableError(err) {
			t.Errorf("status %d: 5xx should be retryable, got: %v", status, err)
		}
	}
}

func TestTwilioSMSProfile_ParseResponse_4xx_PermanentCodes(t *testing.T) {
	profile := &channels.TwilioSMSProfile{}

	permanentCodes := []struct {
		code        int
		description string
	}{
		{21211, "invalid To number"},
		{21610, "unsubscribed recipient"},
		{21614, "not a mobile number"},
		{30003, "unreachable destination handset"},
		{30004, "message blocked"},
		{30006, "landline"},
	}

	for _, tc := range permanentCodes {
		body, _ := json.Marshal(map[string]interface{}{
			"code":    tc.code,
			"message": tc.description,
			"status":  400,
		})
		resp := &http.Response{StatusCode: 400}
		_, err := profile.ParseResponse(resp, body)
		if err == nil {
			t.Fatalf("code %d (%s): expected error", tc.code, tc.description)
		}
		if channels.IsRetryableError(err) {
			t.Errorf("code %d (%s): should be PERMANENT (non-retryable), got: %v",
				tc.code, tc.description, err)
		}
	}
}

func TestTwilioSMSProfile_ParseResponse_4xx_RetryableCodes(t *testing.T) {
	profile := &channels.TwilioSMSProfile{}

	retryCodes := []struct {
		code        int
		description string
	}{
		{21611, "queue full"},
		{21612, "handset temporarily unreachable"},
		{30008, "unknown carrier error"},
	}

	for _, tc := range retryCodes {
		body, _ := json.Marshal(map[string]interface{}{
			"code":    tc.code,
			"message": tc.description,
			"status":  400,
		})
		resp := &http.Response{StatusCode: 400}
		_, err := profile.ParseResponse(resp, body)
		if err == nil {
			t.Fatalf("code %d (%s): expected error", tc.code, tc.description)
		}
		if !channels.IsRetryableError(err) {
			t.Errorf("code %d (%s): should be RETRYABLE, got: %v",
				tc.code, tc.description, err)
		}
	}
}

func TestTwilioSMSProfile_IsInvalidToken_AlwaysFalse(t *testing.T) {
	profile := &channels.TwilioSMSProfile{}
	if profile.IsInvalidToken(nil, nil) {
		t.Error("TwilioSMSProfile.IsInvalidToken must always return false for SMS")
	}
}

// -----------------------------------------------------------------------
// DefaultRegistry includes "twilio"
// -----------------------------------------------------------------------

func TestDefaultRegistry_IncludesTwilio(t *testing.T) {
	reg := channels.DefaultRegistry()
	twilio := reg.For("twilio")
	fake := reg.For("fake")

	if twilio == nil {
		t.Error("expected twilio adapter to be registered")
	}
	// Twilio should be distinct from the fake fallback
	if twilio == fake {
		t.Error("twilio adapter should not be the same instance as the fake fallback")
	}
}

func TestDefaultRegistry_IncludesHTTP(t *testing.T) {
	reg := channels.DefaultRegistry()
	httpAdapter := reg.For("http")
	fake := reg.For("fake")

	if httpAdapter == nil {
		t.Error("expected http adapter to be registered")
	}
	if httpAdapter == fake {
		t.Error("http adapter should not be the same instance as the fake fallback")
	}
}

// -----------------------------------------------------------------------
// Verify the Twilio form body is NOT JSON (content-type sanity check)
// -----------------------------------------------------------------------

func TestTwilioSMSProfile_Body_IsFormEncoded(t *testing.T) {
	cfg := twilioConfig("AC123", "tok456", "")
	msg := makeTwilioMsg("+15005550001", "+15005550006", "Test body", cfg)

	profile := &channels.TwilioSMSProfile{}
	req, err := profile.BuildRequest(context.Background(), msg)
	if err != nil {
		t.Fatalf("BuildRequest: %v", err)
	}
	bodyBytes, _ := io.ReadAll(req.Body)
	bodyStr := string(bodyBytes)

	// Must not be JSON
	if strings.HasPrefix(bodyStr, "{") {
		t.Error("body must be form-encoded (not JSON)")
	}
	// Must contain To=
	if !strings.Contains(bodyStr, "To=") {
		t.Errorf("form body missing To field: %s", bodyStr)
	}
}
