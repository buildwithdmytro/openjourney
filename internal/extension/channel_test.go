package extension

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"testing"

	"github.com/buildwithdmytro/openjourney/internal/channels"
	"github.com/buildwithdmytro/openjourney/internal/domain"
	"github.com/buildwithdmytro/openjourney/internal/ports"
)

func TestExtensionChannelAdapter_Send_Success(t *testing.T) {
	store := newMockStore()
	host := NewHost(store)

	extID := "ext-channel-1"
	versionID := "ver-channel-1"
	store.extensions[extID] = domain.Extension{
		ID:               extID,
		Name:             "custom_sms",
		Status:           "enabled",
		CurrentVersionID: &versionID,
	}
	store.versions[versionID] = domain.ExtensionVersion{
		ID:              versionID,
		ExtensionID:     extID,
		Version:         1,
		Kind:            "channel_provider",
		Transport:       "remote_http",
		RequestedScopes: []string{"messages:write"},
	}
	store.grants[extID] = []domain.ExtensionGrant{{ExtensionID: extID, Scope: "messages:write"}}
	store.configs[extID] = domain.ExtensionConfig{
		ExtensionID:       extID,
		Status:            "active",
		Config:            json.RawMessage(`{"base_url": "http://example.com"}`),
		EndpointAllowlist: []string{"http://example.com"},
	}

	var capturedBody string
	host.httpClient.Transport = &mockRoundTripper{
		RoundTripFunc: func(req *http.Request) (*http.Response, error) {
			bodyBytes, _ := io.ReadAll(req.Body)
			capturedBody = string(bodyBytes)
			return &http.Response{
				StatusCode: 200,
				Body:       io.NopCloser(bytes.NewBufferString(`{"provider_id": "msg-from-custom-123"}`)),
			}, nil
		},
	}

	adapter := NewExtensionChannelAdapter(host, store, "custom_sms")
	msg := ports.RenderedMessage{
		Channel:  "sms",
		Endpoint: "+1234567890",
		Body:     "Hello from OpenJourney!",
		Identity: domain.SendingIdentity{
			ID:          "iden-1",
			TenantID:    "tenant-1",
			WorkspaceID: "workspace-1",
			Channel:     "sms",
			Provider:    "custom_sms",
		},
	}

	providerID, err := adapter.Send(context.Background(), msg)
	if err != nil {
		t.Fatalf("unexpected Send error: %v", err)
	}

	if providerID != "msg-from-custom-123" {
		t.Errorf("expected providerID = msg-from-custom-123, got %s", providerID)
	}

	// Verify that the sent payload matches the marshaled msg
	var sentMsg ports.RenderedMessage
	if err := json.Unmarshal([]byte(capturedBody), &sentMsg); err != nil {
		t.Fatalf("failed to unmarshal captured body: %v", err)
	}
	if sentMsg.Endpoint != msg.Endpoint || sentMsg.Body != msg.Body {
		t.Errorf("captured body did not match input message: %+v", sentMsg)
	}
}

func TestExtensionChannelAdapter_Send_FailureWrapsInDeliveryError(t *testing.T) {
	store := newMockStore()
	host := NewHost(store)

	extID := "ext-channel-1"
	versionID := "ver-channel-1"
	store.extensions[extID] = domain.Extension{
		ID:               extID,
		Name:             "custom_sms",
		Status:           "enabled",
		CurrentVersionID: &versionID,
	}
	store.versions[versionID] = domain.ExtensionVersion{
		ID:              versionID,
		ExtensionID:     extID,
		Version:         1,
		Kind:            "channel_provider",
		Transport:       "remote_http",
		RequestedScopes: []string{"messages:write"},
	}
	store.grants[extID] = []domain.ExtensionGrant{{ExtensionID: extID, Scope: "messages:write"}}
	store.configs[extID] = domain.ExtensionConfig{
		ExtensionID:       extID,
		Status:            "active",
		Config:            json.RawMessage(`{"base_url": "http://example.com"}`),
		EndpointAllowlist: []string{"http://example.com"},
	}

	host.httpClient.Transport = &mockRoundTripper{
		RoundTripFunc: func(req *http.Request) (*http.Response, error) {
			return &http.Response{
				StatusCode: 500,
				Body:       io.NopCloser(bytes.NewBufferString(`server error`)),
			}, nil
		},
	}

	adapter := NewExtensionChannelAdapter(host, store, "custom_sms")
	msg := ports.RenderedMessage{
		Channel:  "sms",
		Endpoint: "+1234567890",
		Body:     "Hello!",
		Identity: domain.SendingIdentity{
			TenantID:    "tenant-1",
			WorkspaceID: "workspace-1",
		},
	}

	_, err := adapter.Send(context.Background(), msg)
	if err == nil {
		t.Fatal("expected error, got nil")
	}

	var delErr *channels.DeliveryError
	if !errors.As(err, &delErr) {
		t.Errorf("expected a channels.DeliveryError, got %T: %v", err, err)
	}
}

func TestExtensionChannelAdapter_OverScopedInvocationIsDeniedAndAudited(t *testing.T) {
	store := newMockStore()
	host := NewHost(store)
	versionID := "ver-channel-scope"
	store.extensions["ext-channel-scope"] = domain.Extension{ID: "ext-channel-scope", Name: "scoped_sms", Status: "enabled", CurrentVersionID: &versionID}
	store.versions[versionID] = domain.ExtensionVersion{ID: versionID, ExtensionID: "ext-channel-scope", Version: 1, Kind: "channel_provider", Transport: "remote_http", RequestedScopes: []string{"messages:write"}}
	store.grants["ext-channel-scope"] = []domain.ExtensionGrant{{ExtensionID: "ext-channel-scope", Scope: "messages:read"}}
	store.configs["ext-channel-scope"] = domain.ExtensionConfig{ExtensionID: "ext-channel-scope", Status: "active", Config: json.RawMessage(`{"base_url":"http://example.com"}`), EndpointAllowlist: []string{"http://example.com"}}
	host.httpClient.Transport = &mockRoundTripper{RoundTripFunc: func(*http.Request) (*http.Response, error) {
		t.Fatal("over-scoped channel invocation reached transport")
		return nil, nil
	}}

	_, err := NewExtensionChannelAdapter(host, store, "scoped_sms").Send(context.Background(), ports.RenderedMessage{
		Channel: "sms", Endpoint: "+1", Body: "blocked", Identity: domain.SendingIdentity{TenantID: "tenant-1", WorkspaceID: "workspace-1"},
	})
	if err == nil || len(store.activities) != 1 || store.activities[0].PolicyDecision != "denied_scope" {
		t.Fatalf("expected audited scope denial, err=%v activities=%+v", err, store.activities)
	}
}

func TestRegisterChannelProviders(t *testing.T) {
	store := newMockStore()
	host := NewHost(store)
	reg := channels.NewRegistry(map[string]ports.ChannelAdapter{}, channels.NewFakeAdapter())

	// 1. Setup active extension of type channel_provider
	extID := "ext-channel-1"
	versionID := "ver-channel-1"
	store.extensions[extID] = domain.Extension{
		ID:               extID,
		Name:             "custom_sms",
		Status:           "enabled",
		CurrentVersionID: &versionID,
	}
	store.versions[versionID] = domain.ExtensionVersion{
		ID:          versionID,
		ExtensionID: extID,
		Version:     1,
		Kind:        "channel_provider",
		Status:      "active",
	}

	// 2. Setup another extension of a different kind
	extID2 := "ext-action-2"
	versionID2 := "ver-action-2"
	store.extensions[extID2] = domain.Extension{
		ID:               extID2,
		Name:             "custom_action",
		Status:           "enabled",
		CurrentVersionID: &versionID2,
	}
	store.versions[versionID2] = domain.ExtensionVersion{
		ID:          versionID2,
		ExtensionID: extID2,
		Version:     1,
		Kind:        "journey_action",
		Status:      "active",
	}

	err := RegisterChannelProviders(context.Background(), store, host, reg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Validate that custom_sms is registered
	adapter := reg.For("custom_sms")
	if _, ok := adapter.(*ExtensionChannelAdapter); !ok {
		t.Errorf("expected custom_sms to be registered as *ExtensionChannelAdapter, got %T", adapter)
	}

	// Validate that custom_action is NOT registered as a channel provider
	adapter2 := reg.For("custom_action")
	if _, ok := adapter2.(*ExtensionChannelAdapter); ok {
		t.Error("expected custom_action to not be registered as a channel provider")
	}
}
