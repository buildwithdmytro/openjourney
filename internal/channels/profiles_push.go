package channels

import (
	"bytes"
	"context"
	"crypto/ecdsa"
	"crypto/rand"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/buildwithdmytro/openjourney/internal/ports"
)

// FCMConfig holds the FCM credentials stored in SendingIdentity.Config.
type FCMConfig struct {
	ProjectID string `json:"project_id"`
	Token     string `json:"token"` // OAuth2 Access Token / Bearer Token
}

// APNsConfig holds the APNs credentials stored in SendingIdentity.Config.
type APNsConfig struct {
	PrivateKey string `json:"private_key"`
	KeyID      string `json:"key_id"`
	TeamID     string `json:"team_id"`
	Topic      string `json:"topic"`
	Sandbox    bool   `json:"sandbox"`
}

// FCMPushProfile implements ProviderProfile for Google FCM v1.
type FCMPushProfile struct{}

type fcmMessagePayload struct {
	Message fcmMessage `json:"message"`
}

type fcmMessage struct {
	Token        string           `json:"token"`
	Notification *fcmNotification `json:"notification,omitempty"`
	Data         map[string]string `json:"data,omitempty"`
}

type fcmNotification struct {
	Title string `json:"title,omitempty"`
	Body  string `json:"body,omitempty"`
}

type fcmSuccessResponse struct {
	Name string `json:"name"`
}

type fcmErrorResponse struct {
	Error struct {
		Code    int    `json:"code"`
		Message string `json:"message"`
		Status  string `json:"status"`
	} `json:"error"`
}

func (p *FCMPushProfile) BuildRequest(ctx context.Context, msg ports.RenderedMessage) (*http.Request, error) {
	var cfg FCMConfig
	if len(msg.Identity.Config) > 0 && string(msg.Identity.Config) != "{}" {
		if err := json.Unmarshal(msg.Identity.Config, &cfg); err != nil {
			return nil, fmt.Errorf("fcm: invalid identity config: %w", err)
		}
	}

	if cfg.ProjectID == "" {
		return nil, errors.New("fcm: project_id is required in identity config")
	}
	if cfg.Token == "" {
		return nil, errors.New("fcm: token (bearer token) is required in identity config")
	}
	if msg.Endpoint == "" {
		return nil, errors.New("fcm: token (endpoint) is required")
	}

	payload := fcmMessagePayload{
		Message: fcmMessage{
			Token: msg.Endpoint,
			Data:  msg.Data,
		},
	}

	bodyText := msg.Body
	if bodyText == "" {
		bodyText = msg.Text
	}
	if msg.Title != "" || bodyText != "" {
		payload.Message.Notification = &fcmNotification{
			Title: msg.Title,
			Body:  bodyText,
		}
	}

	rawBody, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("fcm: marshal payload: %w", err)
	}

	endpoint := fmt.Sprintf("https://fcm.googleapis.com/v1/projects/%s/messages:send", cfg.ProjectID)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(rawBody))
	if err != nil {
		return nil, fmt.Errorf("fcm: create request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+cfg.Token)

	return req, nil
}

func (p *FCMPushProfile) ParseResponse(resp *http.Response, body []byte) (string, error) {
	if resp.StatusCode >= 200 && resp.StatusCode < 300 {
		var ok fcmSuccessResponse
		if err := json.Unmarshal(body, &ok); err != nil || ok.Name == "" {
			return fmt.Sprintf("fcm-ok-%d", resp.StatusCode), nil
		}
		return ok.Name, nil
	}

	var fcmErr fcmErrorResponse
	_ = json.Unmarshal(body, &fcmErr)

	retryable := true
	status := strings.ToUpper(fcmErr.Error.Status)
	msg := strings.ToUpper(fcmErr.Error.Message)

	if status == "UNREGISTERED" || status == "NOT_FOUND" ||
		strings.Contains(msg, "UNREGISTERED") || strings.Contains(msg, "NOT_FOUND") ||
		strings.Contains(msg, "REGISTRATION_TOKEN_NOT_REGISTERED") {
		retryable = false
	} else if resp.StatusCode >= 400 && resp.StatusCode < 500 {
		retryable = false
	}

	return "", &DeliveryError{
		Err: fmt.Errorf("fcm: send failed %d (%s): %s",
			resp.StatusCode, fcmErr.Error.Status, fcmErr.Error.Message),
		Retryable: retryable,
	}
}

func (p *FCMPushProfile) IsInvalidToken(resp *http.Response, body []byte) bool {
	if resp.StatusCode == http.StatusNotFound || resp.StatusCode == http.StatusGone {
		return true
	}
	var fcmErr fcmErrorResponse
	if err := json.Unmarshal(body, &fcmErr); err == nil {
		status := strings.ToUpper(fcmErr.Error.Status)
		msg := strings.ToUpper(fcmErr.Error.Message)
		if status == "UNREGISTERED" || status == "NOT_FOUND" ||
			strings.Contains(msg, "UNREGISTERED") || strings.Contains(msg, "NOT_FOUND") ||
			strings.Contains(msg, "REGISTRATION_TOKEN_NOT_REGISTERED") {
			return true
		}
	}
	return false
}

// NewFCMPushAdapter constructs a ports.ChannelAdapter for Google FCM.
func NewFCMPushAdapter() ports.ChannelAdapter {
	return NewHTTPProviderAdapter(&FCMPushProfile{}, "push")
}

// APNsPushProfile implements ProviderProfile for Apple APNs.
type APNsPushProfile struct{}

type apnsErrorResponse struct {
	Reason string `json:"reason"`
}

func (p *APNsPushProfile) BuildRequest(ctx context.Context, msg ports.RenderedMessage) (*http.Request, error) {
	var cfg APNsConfig
	if len(msg.Identity.Config) > 0 && string(msg.Identity.Config) != "{}" {
		if err := json.Unmarshal(msg.Identity.Config, &cfg); err != nil {
			return nil, fmt.Errorf("apns: invalid identity config: %w", err)
		}
	}

	if cfg.PrivateKey == "" {
		return nil, errors.New("apns: private_key is required in identity config")
	}
	if cfg.KeyID == "" {
		return nil, errors.New("apns: key_id is required in identity config")
	}
	if cfg.TeamID == "" {
		return nil, errors.New("apns: team_id is required in identity config")
	}
	if cfg.Topic == "" {
		return nil, errors.New("apns: topic is required in identity config")
	}
	if msg.Endpoint == "" {
		return nil, errors.New("apns: device token (endpoint) is required")
	}

	jwt, err := GenerateAPNsJWT(cfg.PrivateKey, cfg.KeyID, cfg.TeamID, time.Now())
	if err != nil {
		return nil, fmt.Errorf("apns: generate JWT: %w", err)
	}

	bodyText := msg.Body
	if bodyText == "" {
		bodyText = msg.Text
	}

	payload := map[string]any{
		"aps": map[string]any{
			"alert": map[string]any{
				"title": msg.Title,
				"body":  bodyText,
			},
		},
	}
	for k, v := range msg.Data {
		payload[k] = v
	}

	rawBody, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("apns: marshal payload: %w", err)
	}

	baseURL := "https://api.push.apple.com"
	if cfg.Sandbox {
		baseURL = "https://api.sandbox.push.apple.com"
	}

	endpoint := fmt.Sprintf("%s/3/device/%s", baseURL, msg.Endpoint)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(rawBody))
	if err != nil {
		return nil, fmt.Errorf("apns: create request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("authorization", "bearer "+jwt)
	req.Header.Set("apns-topic", cfg.Topic)
	if msg.IdempotencyKey != "" {
		req.Header.Set("apns-id", msg.IdempotencyKey)
	}

	return req, nil
}

func (p *APNsPushProfile) ParseResponse(resp *http.Response, body []byte) (string, error) {
	if resp.StatusCode >= 200 && resp.StatusCode < 300 {
		apnsID := resp.Header.Get("apns-id")
		if apnsID == "" {
			apnsID = fmt.Sprintf("apns-ok-%d", resp.StatusCode)
		}
		return apnsID, nil
	}

	var apnsErr apnsErrorResponse
	_ = json.Unmarshal(body, &apnsErr)

	retryable := true
	reason := strings.ToLower(apnsErr.Reason)
	if reason == "baddevicetoken" || reason == "unregistered" {
		retryable = false
	} else if resp.StatusCode >= 400 && resp.StatusCode < 500 {
		retryable = false
	}

	return "", &DeliveryError{
		Err: fmt.Errorf("apns: send failed %d: %s", resp.StatusCode, apnsErr.Reason),
		Retryable: retryable,
	}
}

func (p *APNsPushProfile) IsInvalidToken(resp *http.Response, body []byte) bool {
	if resp.StatusCode == http.StatusGone {
		return true
	}
	var apnsErr apnsErrorResponse
	if err := json.Unmarshal(body, &apnsErr); err == nil {
		reason := strings.ToLower(apnsErr.Reason)
		if reason == "baddevicetoken" || reason == "unregistered" {
			return true
		}
	}
	return false
}

// NewAPNsPushAdapter constructs a ports.ChannelAdapter for Apple APNs.
func NewAPNsPushAdapter() ports.ChannelAdapter {
	return NewHTTPProviderAdapter(&APNsPushProfile{}, "push")
}

// GenerateAPNsJWT mints a standard ES256 APNs authentication token.
func GenerateAPNsJWT(keyPEM string, keyID string, teamID string, now time.Time) (string, error) {
	block, _ := pem.Decode([]byte(keyPEM))
	if block == nil {
		return "", errors.New("apns: invalid PEM block")
	}
	privKey, err := x509.ParsePKCS8PrivateKey(block.Bytes)
	if err != nil {
		return "", fmt.Errorf("apns: parse private key: %w", err)
	}
	ecdsaKey, ok := privKey.(*ecdsa.PrivateKey)
	if !ok {
		return "", errors.New("apns: key is not an ECDSA private key")
	}

	header := map[string]string{
		"alg": "ES256",
		"kid": keyID,
	}
	headerBytes, _ := json.Marshal(header)
	headerB64 := base64.RawURLEncoding.EncodeToString(headerBytes)

	claims := map[string]any{
		"iss": teamID,
		"iat": now.Unix(),
	}
	claimsBytes, _ := json.Marshal(claims)
	claimsB64 := base64.RawURLEncoding.EncodeToString(claimsBytes)

	signingInput := headerB64 + "." + claimsB64
	hasher := sha256.New()
	hasher.Write([]byte(signingInput))
	digest := hasher.Sum(nil)

	sig, err := ecdsa.SignASN1(rand.Reader, ecdsaKey, digest)
	if err != nil {
		return "", fmt.Errorf("apns: sign digest: %w", err)
	}
	sigB64 := base64.RawURLEncoding.EncodeToString(sig)

	return signingInput + "." + sigB64, nil
}
