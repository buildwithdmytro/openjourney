package policy

import (
	"context"
	"testing"
	"time"

	"github.com/buildwithdmytro/openjourney/internal/domain"
	"github.com/buildwithdmytro/openjourney/internal/postgres"
	"github.com/buildwithdmytro/openjourney/internal/ports"
)

type fakeStore struct {
	ports.Store
	suppressed  bool
	suppressErr error
	consent     domain.Consent
	consentErr  error
	sent24h     int
	sent24hErr  error
	sent7d      int
	sent7dErr   error
}

func (f *fakeStore) IsSuppressed(ctx context.Context, p domain.Principal, channel, endpoint string) (bool, error) {
	return f.suppressed, f.suppressErr
}

func (f *fakeStore) LatestConsent(ctx context.Context, p domain.Principal, profileID, channel, topic string) (domain.Consent, error) {
	return f.consent, f.consentErr
}

func (f *fakeStore) SentCountSince(ctx context.Context, p domain.Principal, profileID string, since time.Time) (int, error) {
	if since.After(time.Now().Add(-25 * time.Hour)) {
		return f.sent24h, f.sent24hErr
	}
	return f.sent7d, f.sent7dErr
}

func TestEvaluate(t *testing.T) {
	principal := domain.Principal{TenantID: "tenant-1"}
	recipient := Recipient{
		ProfileID:  "profile-123",
		ExternalID: "ext-123",
		Endpoint:   "user@example.com",
	}
	caps := Caps{
		Channel:     "email",
		Topic:       "marketing",
		MaxSends24h: 2,
		MaxSends7d:  5,
	}

	t.Run("suppressed endpoint", func(t *testing.T) {
		store := &fakeStore{
			suppressed: true,
		}
		verdict := Evaluate(context.Background(), store, principal, recipient, caps)
		if verdict.Decision != "suppressed" {
			t.Errorf("expected decision to be suppressed, got %q", verdict.Decision)
		}
		if verdict.Reason != "endpoint is suppressed" {
			t.Errorf("expected reason 'endpoint is suppressed', got %q", verdict.Reason)
		}
	})

	t.Run("no explicit consent", func(t *testing.T) {
		store := &fakeStore{
			suppressed: false,
			consentErr: postgres.ErrNotFound,
		}
		verdict := Evaluate(context.Background(), store, principal, recipient, caps)
		if verdict.Decision != "no_consent" {
			t.Errorf("expected decision to be no_consent, got %q", verdict.Decision)
		}
		if verdict.Reason != "no explicit consent found" {
			t.Errorf("expected reason 'no explicit consent found', got %q", verdict.Reason)
		}
	})

	t.Run("unsubscribed consent", func(t *testing.T) {
		store := &fakeStore{
			suppressed: false,
			consent: domain.Consent{
				State: "unsubscribed",
			},
		}
		verdict := Evaluate(context.Background(), store, principal, recipient, caps)
		if verdict.Decision != "no_consent" {
			t.Errorf("expected decision to be no_consent, got %q", verdict.Decision)
		}
		if verdict.Reason != "consent state is unsubscribed" {
			t.Errorf("expected reason 'consent state is unsubscribed', got %q", verdict.Reason)
		}
	})

	t.Run("fatigued 24h", func(t *testing.T) {
		store := &fakeStore{
			suppressed: false,
			consent: domain.Consent{
				State: "subscribed",
			},
			sent24h: 2,
		}
		verdict := Evaluate(context.Background(), store, principal, recipient, caps)
		if verdict.Decision != "fatigued" {
			t.Errorf("expected decision to be fatigued, got %q", verdict.Decision)
		}
		if verdict.Reason != "24h fatigue limit reached (2/2 sends)" {
			t.Errorf("expected fatigue reason, got %q", verdict.Reason)
		}
	})

	t.Run("fatigued 7d", func(t *testing.T) {
		store := &fakeStore{
			suppressed: false,
			consent: domain.Consent{
				State: "subscribed",
			},
			sent24h: 1,
			sent7d:  5,
		}
		verdict := Evaluate(context.Background(), store, principal, recipient, caps)
		if verdict.Decision != "fatigued" {
			t.Errorf("expected decision to be fatigued, got %q", verdict.Decision)
		}
		if verdict.Reason != "7d fatigue limit reached (5/5 sends)" {
			t.Errorf("expected fatigue reason, got %q", verdict.Reason)
		}
	})

	t.Run("eligible (happy path)", func(t *testing.T) {
		store := &fakeStore{
			suppressed: false,
			consent: domain.Consent{
				State: "subscribed",
			},
			sent24h: 1,
			sent7d:  3,
		}
		verdict := Evaluate(context.Background(), store, principal, recipient, caps)
		if verdict.Decision != "sent" {
			t.Errorf("expected decision to be sent, got %q", verdict.Decision)
		}
		if verdict.Reason != "eligible" {
			t.Errorf("expected reason 'eligible', got %q", verdict.Reason)
		}
		if val, ok := verdict.Snapshot["consent"].(string); !ok || val != "subscribed" {
			t.Errorf("expected snapshot consent 'subscribed', got %v", verdict.Snapshot["consent"])
		}
	})
}
