package channels

import "github.com/buildwithdmytro/openjourney/internal/ports"

// Registry maps provider strings to their ChannelAdapter implementations.
// Build it once per process (see NewRegistry / DefaultRegistry) and pass it to
// both the campaigns-delivery and journeys-worker configs so that a new provider
// is registered in exactly ONE place.
type Registry struct {
	adapters map[string]ports.ChannelAdapter
	fallback ports.ChannelAdapter
}

// NewRegistry constructs a Registry with the given adapter map and a fallback.
// Unknown or empty provider strings resolve to the fallback (typically FakeAdapter).
func NewRegistry(adapters map[string]ports.ChannelAdapter, fallback ports.ChannelAdapter) *Registry {
	return &Registry{
		adapters: adapters,
		fallback: fallback,
	}
}

// DefaultRegistry builds the production-default registry with:
//   - "ses"     → a fresh SESAdapter
//   - "webhook" → a fresh WebhookAdapter
//   - "twilio"  → a fresh TwilioSMSAdapter (HTTPProviderAdapter + TwilioSMSProfile)
//   - "http"    → a fresh HTTPProviderAdapter with HTTPGenericProfile (generic gateway)
//   - "fake"    → a fresh FakeAdapter (also the fallback for unknown providers)
func DefaultRegistry() *Registry {
	fake := NewFakeAdapter()
	return NewRegistry(
		map[string]ports.ChannelAdapter{
			"ses":     NewSESAdapter(),
			"webhook": NewWebhookAdapter(),
			"twilio":  NewTwilioSMSAdapter(),
			"http":    NewHTTPProviderAdapter(&HTTPGenericProfile{}, "sms"),
			"fake":    fake,
		},
		fake,
	)
}

// For returns the ChannelAdapter registered under the given provider string.
// If the provider is unknown or empty it returns the fallback adapter (never nil).
func (r *Registry) For(provider string) ports.ChannelAdapter {
	if provider != "" {
		if a, ok := r.adapters[provider]; ok {
			return a
		}
	}
	return r.fallback
}

// Register adds or replaces an adapter in the registry. This is safe to call
// during program initialization (before any concurrent For calls).
func (r *Registry) Register(provider string, adapter ports.ChannelAdapter) {
	r.adapters[provider] = adapter
}
