package policy

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/buildwithdmytro/openjourney/internal/domain"
	"github.com/buildwithdmytro/openjourney/internal/ports"
)

type Recipient struct {
	ProfileID  string `json:"profile_id"`
	ExternalID string `json:"external_id"`
	Endpoint   string `json:"endpoint"`
}

type Caps struct {
	Channel     string `json:"channel"`
	Topic       string `json:"topic"`
	MaxSends24h int    `json:"max_sends_24h"`
	MaxSends7d  int    `json:"max_sends_7d"`
}

type Verdict struct {
	Decision string         `json:"decision"`
	Reason   string         `json:"reason"`
	Snapshot map[string]any `json:"snapshot"`
}

func Evaluate(ctx context.Context, store ports.Store, p domain.Principal, recipient Recipient, caps Caps) Verdict {
	snapshot := make(map[string]any)
	now := time.Now()

	// 1. Suppression check
	suppressed, err := store.IsSuppressed(ctx, p, caps.Channel, recipient.Endpoint)
	if err != nil {
		return Verdict{
			Decision: "send_failed",
			Reason:   fmt.Sprintf("failed to check suppression: %v", err),
			Snapshot: snapshot,
		}
	}
	snapshot["suppressed"] = suppressed
	if suppressed {
		return Verdict{
			Decision: "suppressed",
			Reason:   "endpoint is suppressed",
			Snapshot: snapshot,
		}
	}

	// 2. Consent check
	topic := caps.Topic
	if topic == "" {
		topic = "marketing"
	}
	consent, err := store.LatestConsent(ctx, p, recipient.ProfileID, caps.Channel, topic)
	if err != nil {
		if errors.Is(err, errors.New("not found")) || stringsContainsNotFound(err) {
			snapshot["consent"] = "missing"
			return Verdict{
				Decision: "no_consent",
				Reason:   "no explicit consent found",
				Snapshot: snapshot,
			}
		}
		return Verdict{
			Decision: "send_failed",
			Reason:   fmt.Sprintf("failed to check consent: %v", err),
			Snapshot: snapshot,
		}
	}
	snapshot["consent"] = consent.State
	if consent.State != "subscribed" {
		return Verdict{
			Decision: "no_consent",
			Reason:   fmt.Sprintf("consent state is %s", consent.State),
			Snapshot: snapshot,
		}
	}

	// 3. Fatigue check
	var sends24h, sends7d int
	if caps.MaxSends24h > 0 {
		sends24h, err = store.SentCountSince(ctx, p, recipient.ProfileID, now.Add(-24*time.Hour))
		if err != nil {
			return Verdict{
				Decision: "send_failed",
				Reason:   fmt.Sprintf("failed to check 24h fatigue count: %v", err),
				Snapshot: snapshot,
			}
		}
		snapshot["sends_24h"] = sends24h
		snapshot["max_sends_24h"] = caps.MaxSends24h
		if sends24h >= caps.MaxSends24h {
			return Verdict{
				Decision: "fatigued",
				Reason:   fmt.Sprintf("24h fatigue limit reached (%d/%d sends)", sends24h, caps.MaxSends24h),
				Snapshot: snapshot,
			}
		}
	}

	if caps.MaxSends7d > 0 {
		sends7d, err = store.SentCountSince(ctx, p, recipient.ProfileID, now.Add(-7*24*time.Hour))
		if err != nil {
			return Verdict{
				Decision: "send_failed",
				Reason:   fmt.Sprintf("failed to check 7d fatigue count: %v", err),
				Snapshot: snapshot,
			}
		}
		snapshot["sends_7d"] = sends7d
		snapshot["max_sends_7d"] = caps.MaxSends7d
		if sends7d >= caps.MaxSends7d {
			return Verdict{
				Decision: "fatigued",
				Reason:   fmt.Sprintf("7d fatigue limit reached (%d/%d sends)", sends7d, caps.MaxSends7d),
				Snapshot: snapshot,
			}
		}
	}

	return Verdict{
		Decision: "sent", // eligible to proceed with rendering and sending
		Reason:   "eligible",
		Snapshot: snapshot,
	}
}

func stringsContainsNotFound(err error) bool {
	if err == nil {
		return false
	}
	return err.Error() == "not found"
}
