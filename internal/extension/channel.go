package extension

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/buildwithdmytro/openjourney/internal/channels"
	"github.com/buildwithdmytro/openjourney/internal/domain"
	"github.com/buildwithdmytro/openjourney/internal/ports"
)

// ExtensionChannelAdapter implements ports.ChannelAdapter by delegating calls
// to the Extension Host.
type ExtensionChannelAdapter struct {
	host         *Host
	store        ports.Store
	providerName string
}

// NewExtensionChannelAdapter creates a new ExtensionChannelAdapter.
func NewExtensionChannelAdapter(host *Host, store ports.Store, providerName string) *ExtensionChannelAdapter {
	return &ExtensionChannelAdapter{
		host:         host,
		store:        store,
		providerName: providerName,
	}
}

// Send serializes the ports.RenderedMessage, resolves the active extension,
// and invokes it on the Extension Host.
func (e *ExtensionChannelAdapter) Send(ctx context.Context, msg ports.RenderedMessage) (string, error) {
	principal := domain.Principal{
		TenantID:    msg.Identity.TenantID,
		WorkspaceID: msg.Identity.WorkspaceID,
		ActorType:   "system",
	}

	// 1. Get the extension ID for the provider name
	ext, err := e.store.GetExtensionByName(ctx, principal, e.providerName)
	if err != nil {
		return "", &channels.DeliveryError{
			Err:       fmt.Errorf("failed to resolve channel provider extension %q: %w", e.providerName, err),
			Retryable: true,
		}
	}

	// 2. Serialize msg to JSON
	input, err := json.Marshal(msg)
	if err != nil {
		return "", &channels.DeliveryError{
			Err:       fmt.Errorf("failed to marshal message for extension: %w", err),
			Retryable: false,
		}
	}

	// 3. Call host.Invoke
	output, _, err := e.host.Invoke(ctx, principal, ext.ID, "send", input)
	if err != nil {
		retryable := true
		if errors.Is(err, ErrRateLimitExceeded) || errors.Is(err, ErrBudgetExceeded) {
			retryable = false
		}
		// Wrap in DeliveryError
		return "", &channels.DeliveryError{
			Err:       fmt.Errorf("extension channel send invocation failed: %w", err),
			Retryable: retryable,
		}
	}

	// 4. Parse the output for a message or provider ID
	var res struct {
		ProviderID string `json:"provider_id"`
		MessageID  string `json:"message_id"`
		ID         string `json:"id"`
	}
	if err := json.Unmarshal(output, &res); err == nil {
		if res.ProviderID != "" {
			return res.ProviderID, nil
		}
		if res.MessageID != "" {
			return res.MessageID, nil
		}
		if res.ID != "" {
			return res.ID, nil
		}
	}

	// Fallback to raw/clean string if not structured JSON
	strVal := string(output)
	strVal = strings.Trim(strVal, `"`+"\n\r\t ")
	if strVal != "" {
		return strVal, nil
	}

	return "ext-default-msg-id", nil
}

// ValidateConfig implements ports.ChannelAdapter
func (e *ExtensionChannelAdapter) ValidateConfig(iden domain.SendingIdentity) error {
	if iden.Channel == "" {
		return errors.New("channel is required")
	}
	return nil
}

// RegisterChannelProviders queries the store for all active channel provider
// extensions and registers their adapters in the channels.Registry.
func RegisterChannelProviders(ctx context.Context, store ports.Store, host *Host, reg *channels.Registry) error {
	exts, err := store.ListActiveChannelProvidersSystem(ctx)
	if err != nil {
		return fmt.Errorf("failed to list active channel providers: %w", err)
	}

	// Names could overlap across tenants, but the registered provider adapter is process-global.
	// Since names are tenant-scoped, we register a single adapter instance per unique name.
	seen := make(map[string]bool)
	for _, ext := range exts {
		if seen[ext.Name] {
			continue
		}
		seen[ext.Name] = true
		adapter := NewExtensionChannelAdapter(host, store, ext.Name)
		reg.Register(ext.Name, adapter)
	}

	return nil
}
