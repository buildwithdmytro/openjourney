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
	"github.com/buildwithdmytro/openjourney/internal/render"
)

type callbacksMockStore struct {
	fakeStore
	capturedEvents     []domain.Event
	capturedPrincipals []domain.Principal
}

func (c *callbacksMockStore) AcceptEvents(ctx context.Context, p domain.Principal, events []domain.Event) ([]string, error) {
	c.capturedEvents = append(c.capturedEvents, events...)
	c.capturedPrincipals = append(c.capturedPrincipals, p)
	return []string{"event-1"}, nil
}

type mockSNSSignatureVerifier struct {
	err error
}

func (m mockSNSSignatureVerifier) Verify(msg SNSMessage) error {
	return m.err
}

func TestHandleSESCallback_Bounce(t *testing.T) {
	store := &callbacksMockStore{}
	server := &Server{
		store:       store,
		snsVerifier: mockSNSSignatureVerifier{},
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
	if ev.SchemaVersion != 1 {
		t.Errorf("expected schema version 1, got: %d", ev.SchemaVersion)
	}
	if ev.ExternalID != "bounced-recipient@example.com" {
		t.Errorf("expected external id from recipient, got: %s", ev.ExternalID)
	}
	if store.capturedPrincipals[0].TenantID != "test-tenant-456" || store.capturedPrincipals[0].WorkspaceID != "test-workspace-789" {
		t.Errorf("unexpected principal: %+v", store.capturedPrincipals[0])
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
		store:       store,
		snsVerifier: mockSNSSignatureVerifier{},
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
	if ev.SchemaVersion != 1 {
		t.Errorf("expected schema version 1, got: %d", ev.SchemaVersion)
	}
	if ev.ExternalID != "complaining-recipient@example.com" {
		t.Errorf("expected external id from recipient, got: %s", ev.ExternalID)
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

func TestTrackerEventsUseCampaignWorkspace(t *testing.T) {
	store := &callbacksMockStore{}
	server := &Server{
		store:             store,
		trackingSecretKey: []byte("test-tracking-secret"),
	}

	token, err := render.SignOpenToken("tenant", "app", "campaign-1", "profile-1", "template-1", "dispatch-1", server.trackingSecretKey)
	if err != nil {
		t.Fatalf("sign token: %v", err)
	}

	req := httptest.NewRequest(http.MethodGet, "/o/"+token+".gif", nil)
	req.SetPathValue("token", token+".gif")
	w := httptest.NewRecorder()

	server.openPixel(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
	}
	if len(store.capturedEvents) != 1 {
		t.Fatalf("expected 1 tracker event, got %d", len(store.capturedEvents))
	}
	if store.capturedEvents[0].Type != "email.opened" {
		t.Fatalf("unexpected event type: %s", store.capturedEvents[0].Type)
	}
	if store.capturedPrincipals[0].WorkspaceID != "workspace" {
		t.Fatalf("expected campaign workspace, got principal %+v", store.capturedPrincipals[0])
	}
}

func TestHandleSESCallback_SSRFBlockOnSubscriptionConfirmation(t *testing.T) {
	store := &callbacksMockStore{}
	server := &Server{
		store:       store,
		snsVerifier: mockSNSSignatureVerifier{},
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

func TestVerifySNSSignature_HostChecking(t *testing.T) {
	tests := []struct {
		name    string
		certURL string
		wantErr bool
	}{
		{"valid standard region", "https://sns.us-east-1.amazonaws.com/cert.pem", false},
		{"valid other region", "https://sns.ap-southeast-2.amazonaws.com/cert.pem", false},
		{"invalid host s3", "https://bucket.s3.amazonaws.com/cert.pem", true},
		{"invalid host other domain", "https://sns.us-east-1.random.com/cert.pem", true},
		{"invalid host suffix bypass attempt", "https://sns.us-east-1.amazonaws.com.attacker.com/cert.pem", true},
		{"invalid non-https scheme", "http://sns.us-east-1.amazonaws.com/cert.pem", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			msg := SNSMessage{
				Signature:      "some-signature",
				SigningCertURL: tt.certURL,
			}
			err := verifySNSSignature(msg)
			if tt.wantErr {
				if err == nil || !strings.Contains(err.Error(), "invalid cert host") {
					t.Fatalf("expected host validation error, got: %v", err)
				}
			} else {
				if err == nil || strings.Contains(err.Error(), "invalid cert host") {
					t.Fatalf("expected host check to pass, got: %v", err)
				}
			}
		})
	}
}

func TestConfirmSNSSubscription_HostChecking(t *testing.T) {
	tests := []struct {
		name    string
		subURL  string
		wantErr bool
	}{
		{"valid standard region", "https://sns.us-east-1.amazonaws.com/confirm", false},
		{"invalid host s3", "https://bucket.s3.amazonaws.com/confirm", true},
		{"invalid host other domain", "https://sns.us-east-1.random.com/confirm", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := confirmSNSSubscription(tt.subURL)
			if tt.wantErr {
				if err == nil || !strings.Contains(err.Error(), "invalid subscription target host") {
					t.Fatalf("expected host validation error, got: %v", err)
				}
			} else {
				if err == nil || strings.Contains(err.Error(), "invalid subscription target host") {
					t.Fatalf("expected host check to pass, got: %v", err)
				}
			}
		})
	}
}

func TestHandleSESCallback_TopicARNAllowlist(t *testing.T) {
	store := &callbacksMockStore{}
	server := &Server{
		store:            store,
		snsVerifier:      mockSNSSignatureVerifier{},
		allowedTopicARNs: []string{"arn:aws:sns:us-east-1:123456789012:allowed-topic"},
	}

	sesMessageJSON := `{"eventType": "Bounce", "mail": {"messageId": "msg-123"}}`

	tests := []struct {
		name       string
		topicARN   string
		expectCode int
	}{
		{"allowed topic", "arn:aws:sns:us-east-1:123456789012:allowed-topic", http.StatusOK},
		{"forbidden topic", "arn:aws:sns:us-east-1:123456789012:other-topic", http.StatusForbidden},
		{"empty topic", "", http.StatusForbidden},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			snsEnvelope := SNSMessage{
				Type:             "Notification",
				MessageId:        "sns-msg-id",
				TopicArn:         tt.topicARN,
				Message:          sesMessageJSON,
				Timestamp:        "2026-07-07T12:00:00Z",
				SignatureVersion: "1",
				Signature:        "mock-valid-signature",
				SigningCertURL:   "https://sns.us-east-1.amazonaws.com/SimpleNotificationService-abcdef.pem",
			}

			bodyBytes, _ := json.Marshal(snsEnvelope)
			req := httptest.NewRequest("POST", "/v1/callbacks/ses?tenant_id=t1&workspace_id=w1", bytes.NewReader(bodyBytes))
			w := httptest.NewRecorder()

			server.handleSESCallback(w, req)

			if w.Code != tt.expectCode {
				t.Errorf("expected HTTP %d, got: %d (%s)", tt.expectCode, w.Code, w.Body.String())
			}
		})
	}
}

