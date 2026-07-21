package channels

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/buildwithdmytro/openjourney/internal/domain"
	"github.com/buildwithdmytro/openjourney/internal/ports"
)

// mockInAppStore embeds ports.Store so it satisfies the full interface, and
// overrides only the two methods InAppAdapter.Send actually calls. Any other
// Store call would panic on the nil embedded interface — which is intentional:
// it asserts the adapter touches nothing else.
type mockInAppStore struct {
	ports.Store
	createInAppCalls int
	lastMessage      domain.InAppMessage
}

func (m *mockInAppStore) GetProfileAppID(ctx context.Context, tenantID, workspaceID, profileID string) (string, error) {
	return "app-123", nil
}

func (m *mockInAppStore) CreateInAppMessage(ctx context.Context, tenantID, workspaceID, appID, profileID string, msg domain.InAppMessage) (domain.InAppMessage, error) {
	m.createInAppCalls++
	m.lastMessage = msg
	msg.ID = "msg-123"
	msg.TenantID = tenantID
	msg.WorkspaceID = workspaceID
	msg.AppID = appID
	msg.ProfileID = profileID
	return msg, nil
}

func TestInAppAdapter_Send(t *testing.T) {
	store := &mockInAppStore{}
	adapter := NewInAppAdapter(store)

	msg := ports.RenderedMessage{
		Channel:        "in_app",
		Endpoint:       "profile-123",
		Subject:        "Test Subject",
		Title:          "Test Title",
		HTML:           "<p>Test HTML</p>",
		Text:           "Test Text",
		Body:           "Test Body",
		Identity:       domain.SendingIdentity{Channel: "in_app", Provider: "inapp"},
		IdempotencyKey: "key-123",
		Data:           map[string]string{"key": "value"},
	}

	providerID, _, err := adapter.Send(context.Background(), msg)
	if err != nil {
		t.Fatalf("Send: %v", err)
	}
	if providerID != "msg-123" {
		t.Fatalf("providerID = %q, want msg-123", providerID)
	}
	if store.createInAppCalls != 1 {
		t.Fatalf("createInAppCalls = %d, want 1", store.createInAppCalls)
	}

	last := store.lastMessage
	if last.MessageType != "modal" {
		t.Fatalf("MessageType = %q, want modal", last.MessageType)
	}
	if last.Status != "delivered" {
		t.Fatalf("Status = %q, want delivered", last.Status)
	}
	if last.DeliveredAt == nil {
		t.Fatal("DeliveredAt is nil, want a delivery timestamp")
	}

	var content map[string]interface{}
	if err := json.Unmarshal(last.Content, &content); err != nil {
		t.Fatalf("unmarshal content: %v", err)
	}
	for key, want := range map[string]string{
		"subject": "Test Subject",
		"title":   "Test Title",
		"html":    "<p>Test HTML</p>",
		"text":    "Test Text",
		"body":    "Test Body",
	} {
		if content[key] != want {
			t.Fatalf("content[%q] = %v, want %q", key, content[key], want)
		}
	}
	if content["data"] == nil {
		t.Fatal("content[data] is nil, want the rendered data map")
	}
}

func TestInAppAdapter_SendIdempotency(t *testing.T) {
	store := &mockInAppStore{}
	adapter := NewInAppAdapter(store)

	msg := ports.RenderedMessage{
		Channel:        "in_app",
		Endpoint:       "profile-123",
		Title:          "Test Title",
		Body:           "Test Body",
		Identity:       domain.SendingIdentity{Channel: "in_app", Provider: "inapp"},
		IdempotencyKey: "key-123",
	}

	id1, _, err := adapter.Send(context.Background(), msg)
	if err != nil {
		t.Fatalf("first Send: %v", err)
	}
	id2, _, err := adapter.Send(context.Background(), msg)
	if err != nil {
		t.Fatalf("second Send: %v", err)
	}
	if id1 != id2 {
		t.Fatalf("provider IDs differ across sends: %q vs %q", id1, id2)
	}
	// The adapter writes on each call; the DB unique constraint on
	// (tenant, profile, idempotency_key) handles dedup, not the adapter.
	if store.createInAppCalls != 2 {
		t.Fatalf("createInAppCalls = %d, want 2", store.createInAppCalls)
	}
}

func TestInAppAdapter_InvalidChannel(t *testing.T) {
	adapter := NewInAppAdapter(&mockInAppStore{})
	_, _, err := adapter.Send(context.Background(), ports.RenderedMessage{
		Channel:  "email",
		Endpoint: "profile-123",
		Identity: domain.SendingIdentity{Channel: "email"},
	})
	if err == nil || !strings.Contains(err.Error(), "invalid channel for in-app") {
		t.Fatalf("err = %v, want an invalid-channel error", err)
	}
}

func TestInAppAdapter_EmptyEndpoint(t *testing.T) {
	adapter := NewInAppAdapter(&mockInAppStore{})
	_, _, err := adapter.Send(context.Background(), ports.RenderedMessage{
		Channel:  "in_app",
		Endpoint: "",
		Identity: domain.SendingIdentity{Channel: "in_app"},
	})
	if err == nil || !strings.Contains(err.Error(), "empty profile endpoint") {
		t.Fatalf("err = %v, want an empty-endpoint error", err)
	}
}

func TestInAppAdapter_ValidateConfig(t *testing.T) {
	adapter := NewInAppAdapter(&mockInAppStore{})
	tests := []struct {
		name     string
		identity domain.SendingIdentity
		wantErr  bool
	}{
		{"valid config", domain.SendingIdentity{Channel: "in_app", Provider: "inapp"}, false},
		{"invalid channel", domain.SendingIdentity{Channel: "email", Provider: "inapp"}, true},
		{"invalid provider", domain.SendingIdentity{Channel: "in_app", Provider: "webhook"}, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := adapter.ValidateConfig(tt.identity)
			if tt.wantErr && err == nil {
				t.Fatal("expected an error, got nil")
			}
			if !tt.wantErr && err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
		})
	}
}
