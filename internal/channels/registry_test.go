package channels_test

import (
	"context"
	"testing"

	"github.com/buildwithdmytro/openjourney/internal/channels"
	"github.com/buildwithdmytro/openjourney/internal/domain"
	"github.com/buildwithdmytro/openjourney/internal/ports"
)

// stubAdapter is a minimal ports.ChannelAdapter for registry tests.
type stubAdapter struct{ name string }

func (s *stubAdapter) Send(_ context.Context, _ ports.RenderedMessage) (string, error) {
	return s.name, nil
}
func (s *stubAdapter) ValidateConfig(_ domain.SendingIdentity) error { return nil }

func TestRegistry_For(t *testing.T) {
	ses := &stubAdapter{name: "ses"}
	webhook := &stubAdapter{name: "webhook"}
	fake := &stubAdapter{name: "fake"}
	fallback := &stubAdapter{name: "fallback"}

	reg := channels.NewRegistry(
		map[string]ports.ChannelAdapter{
			"ses":     ses,
			"webhook": webhook,
			"fake":    fake,
		},
		fallback,
	)

	tests := []struct {
		provider string
		want     ports.ChannelAdapter
	}{
		{"ses", ses},
		{"webhook", webhook},
		{"fake", fake},
		{"", fallback},        // empty string → fallback
		{"unknown", fallback}, // unregistered → fallback
		{"twilio", fallback},  // not yet registered → fallback
	}

	for _, tc := range tests {
		got := reg.For(tc.provider)
		if got != tc.want {
			t.Errorf("For(%q): got %v, want %v", tc.provider, got, tc.want)
		}
	}
}

func TestRegistry_DefaultRegistry_KnownProviders(t *testing.T) {
	reg := channels.DefaultRegistry()

	// DefaultRegistry registers ses, webhook, fake.
	// We can't compare pointer identity to freshly created adapters,
	// but we can verify that known providers don't return the fallback
	// and unknown providers do return the fallback.
	unknown := reg.For("unknown")
	fake := reg.For("fake")
	ses := reg.For("ses")
	webhook := reg.For("webhook")
	empty := reg.For("")

	if ses == nil {
		t.Error("ses adapter must not be nil")
	}
	if webhook == nil {
		t.Error("webhook adapter must not be nil")
	}
	if fake == nil {
		t.Error("fake adapter must not be nil")
	}
	// unknown and "" should return the fallback, which is the same as the fake adapter
	if unknown != fake {
		t.Errorf("unknown provider: got %T, want fake fallback %T", unknown, fake)
	}
	if empty != fake {
		t.Errorf("empty provider: got %T, want fake fallback %T", empty, fake)
	}
}

func TestRegistry_Register(t *testing.T) {
	fallback := &stubAdapter{name: "fallback"}
	reg := channels.NewRegistry(map[string]ports.ChannelAdapter{}, fallback)

	// Before registration: unknown → fallback
	if reg.For("twilio") != fallback {
		t.Error("expected fallback before registration")
	}

	twilio := &stubAdapter{name: "twilio"}
	reg.Register("twilio", twilio)

	// After registration: twilio → twilio adapter
	if reg.For("twilio") != twilio {
		t.Error("expected twilio adapter after registration")
	}
}
