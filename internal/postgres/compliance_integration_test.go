package postgres

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/buildwithdmytro/openjourney/internal/domain"
	"github.com/buildwithdmytro/openjourney/internal/journey"
	"github.com/buildwithdmytro/openjourney/internal/policy"
	"github.com/buildwithdmytro/openjourney/internal/projector"
)

// compliance_integration_test.go — Milestone 10.7.3
//
// DB-gated integration tests asserting compliance requirements:
//  1. Blocking send to a STOP-suppressed phone
//  2. Quiet-hours retrieval and time-window evaluation
//  3. Cross-channel fatigue capping (SMS + push + email counting towards fatigue limits)

func TestComplianceControls(t *testing.T) {
	databaseURL := os.Getenv("OPENJOURNEY_TEST_DATABASE_URL")
	if databaseURL == "" {
		t.Skip("OPENJOURNEY_TEST_DATABASE_URL is not configured")
	}
	ctx := context.Background()
	store, err := Open(ctx, databaseURL)
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer store.Close()

	key := fmt.Sprintf("compliance-test-%d", time.Now().UnixNano())
	if err := store.EnsureDevelopmentTenant(ctx, key); err != nil {
		t.Fatalf("ensure tenant: %v", err)
	}
	p, err := store.Authenticate(ctx, key)
	if err != nil {
		t.Fatalf("authenticate: %v", err)
	}
	appID, err := store.GetFirstAppID(ctx, p.TenantID, p.WorkspaceID)
	if err != nil {
		t.Fatalf("get first app ID: %v", err)
	}
	p.AppID = appID

	// Create profile for testing consent and fatigue
	profileID := ""
	err = store.pool.QueryRow(ctx,
		`INSERT INTO profiles (tenant_id, workspace_id, app_id, external_id, attributes)
		 VALUES ($1, $2, $3, 'compliance-profile', '{"phone":"+15005550009","email":"compliance@example.com"}')
		 RETURNING id`,
		p.TenantID, p.WorkspaceID, p.AppID,
	).Scan(&profileID)
	if err != nil {
		t.Fatalf("create test profile: %v", err)
	}

	// -----------------------------------------------------------------------
	// 1. Blocking send to a STOP-suppressed phone
	// -----------------------------------------------------------------------
	t.Run("STOP-suppression blocking", func(t *testing.T) {
		stopPhone := "+15005550009"

		// Ensure suppressed initially is false
		suppressed, err := store.IsSuppressed(ctx, p, "sms", stopPhone)
		if err != nil {
			t.Fatalf("IsSuppressed initial: %v", err)
		}
		if suppressed {
			t.Error("expected phone to not be suppressed initially")
		}

		// Accept a consent.changed event (unsubscribed) to trigger suppression insertion natively
		events := []domain.Event{
			{
				Type:           "consent.changed",
				SchemaVersion:  1,
				ExternalID:     "compliance-profile",
				IdempotencyKey: key + "-stop-suppress-event",
				OccurredAt:     time.Now().UTC(),
				Payload:        json.RawMessage(`{"channel": "sms", "topic": "marketing", "state": "unsubscribed", "evidence": {}}`),
			},
		}
		_, err = store.AcceptEvents(ctx, p, events)
		if err != nil {
			t.Fatalf("AcceptEvents unsubscribe: %v", err)
		}
		_, err = projector.Drain(ctx, store, len(events), false)
		if err != nil {
			t.Fatalf("projector drain unsubscribe: %v", err)
		}

		// Verify IsSuppressed returns true now
		suppressed, err = store.IsSuppressed(ctx, p, "sms", stopPhone)
		if err != nil {
			t.Fatalf("IsSuppressed after STOP: %v", err)
		}
		if !suppressed {
			t.Error("expected phone to be suppressed after STOP")
		}

		// Evaluate policy engine and assert verdict is "suppressed"
		verdict := policy.Evaluate(ctx, store, p, policy.Recipient{
			ProfileID:  profileID,
			ExternalID: "compliance-profile",
			Endpoint:   stopPhone,
		}, policy.Caps{
			Channel: "sms",
			Topic:   "marketing",
		})
		if verdict.Decision != "suppressed" {
			t.Errorf("expected policy decision to be 'suppressed', got %q (reason: %s)", verdict.Decision, verdict.Reason)
		}
	})

	// -----------------------------------------------------------------------
	// 2. Quiet-hours retrieval and time-window evaluation
	// -----------------------------------------------------------------------
	t.Run("Quiet-hours verification", func(t *testing.T) {
		// Set quiet hours on tenant: 22:00 to 08:00 EST/New_York
		_, err = store.pool.Exec(ctx,
			`UPDATE tenant_quotas SET quiet_hours_start = 22, quiet_hours_end = 8, default_timezone = 'America/New_York' WHERE tenant_id = $1`,
			p.TenantID,
		)
		if err != nil {
			t.Fatalf("update tenant quiet hours: %v", err)
		}

		// Retrieve from store and assert
		start, end, tz, err := store.GetTenantQuietHours(ctx, p)
		if err != nil {
			t.Fatalf("GetTenantQuietHours: %v", err)
		}
		if start == nil || *start != 22 || end == nil || *end != 8 || tz != "America/New_York" {
			t.Errorf("GetTenantQuietHours returned unexpected values: start=%v, end=%v, tz=%q", start, end, tz)
		}

		// Test IsInQuietHours function with various times
		loc, err := time.LoadLocation("America/New_York")
		if err != nil {
			t.Fatalf("load New York timezone: %v", err)
		}

		// Time inside quiet hours: 23:00 (11 PM) NY time
		tInside := time.Date(2026, 7, 17, 23, 0, 0, 0, loc).UTC()
		inQuiet, _, err := journey.IsInQuietHours(tInside, domain.Profile{Attributes: json.RawMessage(`{}`)}, start, end, tz)
		if err != nil {
			t.Fatalf("IsInQuietHours inside: %v", err)
		}
		if !inQuiet {
			t.Error("expected 23:00 America/New_York to be inside quiet hours")
		}

		// Time outside quiet hours: 14:00 (2 PM) NY time
		tOutside := time.Date(2026, 7, 17, 14, 0, 0, 0, loc).UTC()
		inQuiet, _, err = journey.IsInQuietHours(tOutside, domain.Profile{Attributes: json.RawMessage(`{}`)}, start, end, tz)
		if err != nil {
			t.Fatalf("IsInQuietHours outside: %v", err)
		}
		if inQuiet {
			t.Error("expected 14:00 America/New_York to be outside quiet hours")
		}
	})

	// -----------------------------------------------------------------------
	// 3. Cross-channel fatigue capping (SMS + push + email together)
	// -----------------------------------------------------------------------
	t.Run("Cross-channel fatigue capping", func(t *testing.T) {
		// Set fatigue quota: max 1 send per 24 hours
		_, err = store.pool.Exec(ctx,
			`UPDATE tenant_quotas SET max_sends_24h = 1 WHERE tenant_id = $1`,
			p.TenantID,
		)
		if err != nil {
			t.Fatalf("update tenant fatigue quota: %v", err)
		}

		// Seed explicit consent for sms, push, and email topic 'marketing' via AcceptEvents and Drain
		var events []domain.Event
		for _, channel := range []string{"sms", "push", "email"} {
			events = append(events, domain.Event{
				Type:           "consent.changed",
				SchemaVersion:  1,
				ExternalID:     "compliance-profile",
				IdempotencyKey: fmt.Sprintf("%s-consent-%s", key, channel),
				OccurredAt:     time.Now().UTC(),
				Payload:        json.RawMessage(fmt.Sprintf(`{"channel": "%s", "topic": "marketing", "state": "subscribed", "evidence": {}}`, channel)),
			})
		}
		_, err = store.AcceptEvents(ctx, p, events)
		if err != nil {
			t.Fatalf("AcceptEvents consent: %v", err)
		}
		_, err = projector.Drain(ctx, store, len(events), false)
		if err != nil {
			t.Fatalf("projector drain consent: %v", err)
		}

		// Verify initial evaluation is "sent" (eligible)
		verdict := policy.Evaluate(ctx, store, p, policy.Recipient{
			ProfileID:  profileID,
			ExternalID: "compliance-profile",
			Endpoint:   "email@example.com",
		}, policy.Caps{
			Channel:     "email",
			Topic:       "marketing",
			MaxSends24h: 1,
		})
		if verdict.Decision != "sent" {
			t.Fatalf("expected initial decision to be 'sent', got %q (reason: %s)", verdict.Decision, verdict.Reason)
		}

		// Create a mock campaign to satisfy delivery_attempts foreign key
		tmpl, err := store.CreateTemplate(ctx, p, domain.Template{
			Name:            "compliance-temp",
			Channel:         "email",
			SubjectTemplate: ptrStr("Hello"),
			HTMLTemplate:    ptrStr("Body"),
		})
		if err != nil {
			t.Fatalf("create test template: %v", err)
		}
		seg, err := store.CreateSegment(ctx, p, domain.Segment{
			Name: "Compliance Segment",
		})
		if err != nil {
			t.Fatalf("create test segment: %v", err)
		}
		camp, err := store.CreateCampaign(ctx, p, domain.Campaign{
			Name:       "Compliance Campaign",
			SegmentID:  seg.ID,
			TemplateID: tmpl.ID,
			Status:     "sending",
		})
		if err != nil {
			t.Fatalf("create test campaign: %v", err)
		}

		// 1st Send: Email channel
		_, err = store.pool.Exec(ctx,
			`INSERT INTO delivery_attempts (id, tenant_id, campaign_id, profile_id, channel, endpoint, decision, attempted_at)
			 VALUES (gen_random_uuid(), $1, $2, $3, 'email', 'compliance@example.com', 'sent', now())`,
			p.TenantID, camp.ID, profileID,
		)
		if err != nil {
			t.Fatalf("record email send attempt: %v", err)
		}

		// Verify we are fatigued on SMS channel
		smsVerdict := policy.Evaluate(ctx, store, p, policy.Recipient{
			ProfileID:  profileID,
			ExternalID: "compliance-profile",
			Endpoint:   "+15005550009",
		}, policy.Caps{
			Channel:     "sms",
			Topic:       "marketing",
			MaxSends24h: 1,
		})
		if smsVerdict.Decision != "fatigued" {
			t.Errorf("expected SMS to be fatigued, got %q (reason: %s)", smsVerdict.Decision, smsVerdict.Reason)
		}

		// Verify we are fatigued on Push channel
		pushVerdict := policy.Evaluate(ctx, store, p, policy.Recipient{
			ProfileID:  profileID,
			ExternalID: "compliance-profile",
			Endpoint:   "push-token-val",
		}, policy.Caps{
			Channel:     "push",
			Topic:       "marketing",
			MaxSends24h: 1,
		})
		if pushVerdict.Decision != "fatigued" {
			t.Errorf("expected Push to be fatigued, got %q (reason: %s)", pushVerdict.Decision, pushVerdict.Reason)
		}

		// Verify we are fatigued on Email channel
		emailVerdict := policy.Evaluate(ctx, store, p, policy.Recipient{
			ProfileID:  profileID,
			ExternalID: "compliance-profile",
			Endpoint:   "compliance@example.com",
		}, policy.Caps{
			Channel:     "email",
			Topic:       "marketing",
			MaxSends24h: 1,
		})
		if emailVerdict.Decision != "fatigued" {
			t.Errorf("expected Email to be fatigued, got %q (reason: %s)", emailVerdict.Decision, emailVerdict.Reason)
		}
	})
}
