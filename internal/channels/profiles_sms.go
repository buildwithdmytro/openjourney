package channels

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"

	"github.com/buildwithdmytro/openjourney/internal/ports"
)

// TwilioConfig holds the Twilio credentials stored in SendingIdentity.Config.
type TwilioConfig struct {
	// AccountSID is the Twilio Account SID (starts with "AC…").
	AccountSID string `json:"account_sid"`
	// AuthToken is the Twilio Auth Token.
	AuthToken string `json:"auth_token"`
	// StatusCallback is an optional URL Twilio will POST delivery receipts to.
	// Set this to the OpenJourney /v1/callbacks/sms/twilio endpoint.
	StatusCallback string `json:"status_callback,omitempty"`
}

// twilioErrorResponse is the JSON body Twilio returns on 4xx/5xx errors.
type twilioErrorResponse struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
	Status  int    `json:"status"`
}

// twilioSuccessResponse is the JSON body Twilio returns on 201 Created.
type twilioSuccessResponse struct {
	SID string `json:"sid"`
}

// twilioEndpoint returns the Twilio Messages API URL for the given account SID.
func twilioEndpoint(accountSID string) string {
	return fmt.Sprintf(
		"https://api.twilio.com/2010-04-01/Accounts/%s/Messages.json",
		url.PathEscape(accountSID),
	)
}

// isTwilioPermanentError returns true for Twilio error codes that indicate a
// permanent, non-retryable failure (invalid number, opted-out, blacklisted, etc.).
// Reference: https://www.twilio.com/docs/api/errors
func IsTwilioPermanentError(code int) bool {
	switch code {
	case 21211: // Invalid 'To' Phone Number
		return true
	case 21214: // 'To' phone number cannot receive SMS
		return true
	case 21610: // Attempt to send to unsubscribed recipient
		return true
	case 21611: // Source number has exceeded max number of queued messages
		return false // retryable (queue full)
	case 21612: // The 'To' phone number is not currently reachable
		return false // transient
	case 21614: // 'To' number is not a valid mobile number
		return true
	case 21617: // The concatenated message body exceeds the 1600 character limit
		return true
	case 30003: // Unreachable destination handset
		return true
	case 30004: // Message blocked
		return true
	case 30005: // Unknown destination handset
		return true
	case 30006: // Landline or unreachable carrier
		return true
	case 30007: // Carrier violation
		return true
	case 30008: // Unknown error from carrier
		return false // retryable — carrier-level transient
	}
	return false
}

// TwilioSMSProfile implements ProviderProfile for Twilio Programmable SMS.
//
// Request: form-encoded POST to the Twilio Messages API with HTTP Basic auth.
// Response: JSON with `sid` on 201 Created; JSON error body on 4xx/5xx.
// IsInvalidToken: always false (SMS has no device tokens).
type TwilioSMSProfile struct{}

// BuildRequest constructs a Twilio Messages API request from the RenderedMessage.
// The endpoint (recipient phone) comes from msg.Endpoint (E.164).
// The sender comes from msg.Identity.FromAddress.
// Credentials are read from msg.Identity.Config (TwilioConfig).
func (p *TwilioSMSProfile) BuildRequest(ctx context.Context, msg ports.RenderedMessage) (*http.Request, error) {
	var cfg TwilioConfig
	if len(msg.Identity.Config) > 0 && string(msg.Identity.Config) != "{}" {
		if err := json.Unmarshal(msg.Identity.Config, &cfg); err != nil {
			return nil, fmt.Errorf("twilio: invalid identity config: %w", err)
		}
	}

	if cfg.AccountSID == "" {
		return nil, errors.New("twilio: account_sid is required in identity config")
	}
	if cfg.AuthToken == "" {
		return nil, errors.New("twilio: auth_token is required in identity config")
	}
	if msg.Endpoint == "" {
		return nil, errors.New("twilio: recipient endpoint (To phone number) is required")
	}
	from := ""
	if msg.Identity.FromAddress != nil {
		from = *msg.Identity.FromAddress
	}
	if from == "" {
		return nil, errors.New("twilio: sender (from_address) is required")
	}

	// Resolve the message body: prefer Body, fall back to Text.
	body := msg.Body
	if body == "" {
		body = msg.Text
	}
	if body == "" {
		return nil, errors.New("twilio: message body is required")
	}

	// Build form payload.
	form := url.Values{}
	form.Set("To", msg.Endpoint)
	form.Set("From", from)
	form.Set("Body", body)
	if cfg.StatusCallback != "" {
		form.Set("StatusCallback", cfg.StatusCallback)
	}

	endpoint := twilioEndpoint(cfg.AccountSID)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, strings.NewReader(form.Encode()))
	if err != nil {
		return nil, fmt.Errorf("twilio: create request: %w", err)
	}

	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("User-Agent", "OpenJourney-Twilio-SMS/1.0")
	req.SetBasicAuth(cfg.AccountSID, cfg.AuthToken)

	// Idempotency: Twilio supports a X-Twilio-Idempotency-Token header to prevent
	// duplicate sends on retry. Use the OpenJourney idempotency key if present.
	if msg.IdempotencyKey != "" {
		req.Header.Set("X-Twilio-Idempotency-Token", msg.IdempotencyKey)
	}

	return req, nil
}

// ParseResponse extracts the Twilio SID from a 201 response, or classifies errors.
//
// 201 → success, returns `sid` as provider ID.
// 4xx with Twilio error code → permanent DeliveryError for known-permanent codes;
//
//	retryable for transient codes (queue full, unreachable).
//
// 5xx / network → retryable DeliveryError.
func (p *TwilioSMSProfile) ParseResponse(resp *http.Response, body []byte) (string, error) {
	// 2xx success (Twilio uses 201 Created for new messages).
	if resp.StatusCode >= 200 && resp.StatusCode < 300 {
		var ok twilioSuccessResponse
		if err := json.Unmarshal(body, &ok); err != nil || ok.SID == "" {
			// Unexpected body shape — still a success; return a fallback ID.
			return fmt.Sprintf("twilio-ok-%d", resp.StatusCode), nil
		}
		return ok.SID, nil
	}

	// Try to parse Twilio's structured error body.
	var twilioErr twilioErrorResponse
	_ = json.Unmarshal(body, &twilioErr)

	if resp.StatusCode >= 500 {
		return "", &DeliveryError{
			Err: fmt.Errorf("twilio: server error %d (code=%d): %s",
				resp.StatusCode, twilioErr.Code, twilioErr.Message),
			Retryable: true,
		}
	}

	// 4xx: classify by Twilio error code.
	retryable := !IsTwilioPermanentError(twilioErr.Code)
	// If we couldn't parse an error code, assume permanent for 4xx.
	if twilioErr.Code == 0 {
		retryable = false
	}

	return "", &DeliveryError{
		Err: fmt.Errorf("twilio: request failed %d (code=%d): %s",
			resp.StatusCode, twilioErr.Code, twilioErr.Message),
		Retryable: retryable,
	}
}

// IsInvalidToken always returns false — SMS has no device tokens to retire.
func (p *TwilioSMSProfile) IsInvalidToken(_ *http.Response, _ []byte) bool { return false }

// NewTwilioSMSAdapter constructs a fully wired ports.ChannelAdapter for Twilio SMS.
// It wraps HTTPProviderAdapter with the TwilioSMSProfile.
func NewTwilioSMSAdapter() ports.ChannelAdapter {
	return NewHTTPProviderAdapter(&TwilioSMSProfile{}, "sms")
}

// buildTwilioRequest is the exported pure-function version of BuildRequest used in
// table-driven tests so they can inspect the raw *http.Request without needing an
// HTTP server.
func buildTwilioRequest(ctx context.Context, msg ports.RenderedMessage) (*http.Request, []byte, error) {
	p := &TwilioSMSProfile{}
	req, err := p.BuildRequest(ctx, msg)
	if err != nil {
		return nil, nil, err
	}
	// Read and restore the body so the caller can inspect it.
	var buf bytes.Buffer
	if req.Body != nil {
		_, _ = buf.ReadFrom(req.Body)
		req.Body = nil // body consumed; test can read buf
	}
	return req, buf.Bytes(), nil
}
