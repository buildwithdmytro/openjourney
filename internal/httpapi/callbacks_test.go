package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/buildwithdmytro/openjourney/internal/domain"
)

type callbacksMockStore struct {
	fakeStore
	capturedEvents []domain.Event
}

func (c *callbacksMockStore) AcceptEvents(ctx context.Context, p domain.Principal, events []domain.Event) ([]string, error) {
	c.capturedEvents = append(c.capturedEvents, events...)
	return []string{"event-1"}, nil
}

func TestHandleSESCallback_Bounce(t *testing.T) {
	store := &callbacksMockStore{}
	server := &Server{
		store: store,
	}

	sesMessageJSON := `{
		"eventType": "Bounce",
		"bounce": {
			"bounceType": "Permanent",
			"bounceSubType": "General",
			"bouncedRecipients": [
				{
					"emailAddress": "bounced-recipient@example.com"
				}
			]
		},
		"mail": {
			"messageId": "ses-message-id-111",
			"headers": [
				{"name": "X-Campaign-ID", "value": "test-campaign-123"},
				{"name": "X-Tenant-ID", "value": "test-tenant-456"},
				{"name": "X-Workspace-ID", "value": "test-workspace-789"}
			]
		}
	}`

	snsEnvelope := SNSMessage{
		Type:             "Notification",
		MessageId:        "sns-msg-id-000",
		Message:          sesMessageJSON,
		Timestamp:        "2026-07-07T12:00:00Z",
		SignatureVersion: "1",
		Signature:        "mock-valid-signature",
		SigningCertURL:   "https://sns.us-east-1.amazonaws.com/SimpleNotificationService-abcdef.pem",
	}

	bodyBytes, _ := json.Marshal(snsEnvelope)

	req := httptest.NewRequest("POST", "/v1/callbacks/ses", bytes.NewReader(bodyBytes))
	w := httptest.NewRecorder()

	server.handleSESCallback(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected HTTP 200, got: %d (%s)", w.Code, w.Body.String())
	}

	if len(store.capturedEvents) != 1 {
		t.Fatalf("expected 1 captured event, got: %d", len(store.capturedEvents))
	}

	ev := store.capturedEvents[0]
	if ev.Type != "message.bounced" {
		t.Errorf("expected event type 'message.bounced', got: %s", ev.Type)
	}

	var payload map[string]any
	if err := json.Unmarshal(ev.Payload, &payload); err != nil {
		t.Fatalf("failed to parse event payload: %v", err)
	}

	if payload["campaign_id"] != "test-campaign-123" {
		t.Errorf("expected campaign_id 'test-campaign-123', got: %v", payload["campaign_id"])
	}
	if payload["endpoint"] != "bounced-recipient@example.com" {
		t.Errorf("expected endpoint 'bounced-recipient@example.com', got: %v", payload["endpoint"])
	}
	if payload["bounce_type"] != "Permanent" {
		t.Errorf("expected bounce_type 'Permanent', got: %v", payload["bounce_type"])
	}
}

func TestHandleSESCallback_Complaint(t *testing.T) {
	store := &callbacksMockStore{}
	server := &Server{
		store: store,
	}

	sesMessageJSON := `{
		"eventType": "Complaint",
		"complaint": {
			"complainedRecipients": [
				{
					"emailAddress": "complaining-recipient@example.com"
				}
			]
		},
		"mail": {
			"messageId": "ses-message-id-222",
			"headers": [
				{"name": "X-Campaign-ID", "value": "test-campaign-456"},
				{"name": "X-Tenant-ID", "value": "test-tenant-456"},
				{"name": "X-Workspace-ID", "value": "test-workspace-789"}
			]
		}
	}`

	snsEnvelope := SNSMessage{
		Type:             "Notification",
		MessageId:        "sns-msg-id-111",
		Message:          sesMessageJSON,
		Timestamp:        "2026-07-07T12:00:00Z",
		SignatureVersion: "1",
		Signature:        "mock-valid-signature",
		SigningCertURL:   "https://sns.us-east-1.amazonaws.com/SimpleNotificationService-abcdef.pem",
	}

	bodyBytes, _ := json.Marshal(snsEnvelope)

	req := httptest.NewRequest("POST", "/v1/callbacks/ses", bytes.NewReader(bodyBytes))
	w := httptest.NewRecorder()

	server.handleSESCallback(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected HTTP 200, got: %d (%s)", w.Code, w.Body.String())
	}

	if len(store.capturedEvents) != 1 {
		t.Fatalf("expected 1 captured event, got: %d", len(store.capturedEvents))
	}

	ev := store.capturedEvents[0]
	if ev.Type != "message.complained" {
		t.Errorf("expected event type 'message.complained', got: %s", ev.Type)
	}

	var payload map[string]any
	if err := json.Unmarshal(ev.Payload, &payload); err != nil {
		t.Fatalf("failed to parse event payload: %v", err)
	}

	if payload["campaign_id"] != "test-campaign-456" {
		t.Errorf("expected campaign_id 'test-campaign-456', got: %v", payload["campaign_id"])
	}
	if payload["endpoint"] != "complaining-recipient@example.com" {
		t.Errorf("expected endpoint 'complaining-recipient@example.com', got: %v", payload["endpoint"])
	}
}

func TestHandleSESCallback_SSRFBlockOnSubscriptionConfirmation(t *testing.T) {
	store := &callbacksMockStore{}
	server := &Server{
		store: store,
	}

	// Craft confirmation targeting localhost (non-AWS URL)
	snsEnvelope := SNSMessage{
		Type:             "SubscriptionConfirmation",
		MessageId:        "sns-msg-id-sub",
		Token:            "sub-token",
		TopicArn:         "arn:aws:sns:us-east-1:123456789012:test-topic",
		Timestamp:        "2026-07-07T12:00:00Z",
		SignatureVersion: "1",
		Signature:        "mock-valid-signature",
		SubscribeURL:     "http://127.0.0.1:8080/malicious",
		SigningCertURL:   "https://sns.us-east-1.amazonaws.com/SimpleNotificationService-abcdef.pem",
	}

	bodyBytes, _ := json.Marshal(snsEnvelope)

	req := httptest.NewRequest("POST", "/v1/callbacks/ses", bytes.NewReader(bodyBytes))
	w := httptest.NewRecorder()

	server.handleSESCallback(w, req)

	// Since local host confirmation URL is blocked, it should fail validation and return HTTP 500
	if w.Code != http.StatusInternalServerError {
		t.Errorf("expected HTTP 500 for blocked local subscription target, got: %d", w.Code)
	}

	if !strings.Contains(w.Body.String(), "subscription target host") && !strings.Contains(w.Body.String(), "SSRF") {
		t.Errorf("expected subscription rejection error, got: %s", w.Body.String())
	}
}
