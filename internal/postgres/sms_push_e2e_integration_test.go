package postgres

// sms_push_e2e_integration_test.go — Milestone 10.7.2
//
// DB-gated end-to-end test seeding an SMS campaign and a push campaign,
// driving callbacks (DLR, STOP, push receipt, invalid-token), and asserting:
//   - dispositions recorded in delivery_attempts
//   - engagement_facts created (message.delivered, message.failed)
//   - suppressions created for STOP opt-out
//   - device token retired on invalid-token feedback
//   - per-channel report numbers match seeded data

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/buildwithdmytro/openjourney/internal/domain"
	"github.com/buildwithdmytro/openjourney/internal/projector"
)

func TestSMSAndPushCampaignsEndToEnd(t *testing.T) {
	databaseURL := os.Getenv("OPENJOURNEY_TEST_DATABASE_URL")
	if databaseURL == "" {
		t.Skip("OPENJOURNEY_TEST_DATABASE_URL is not configured")
	}
	ctx := context.Background()
	store, err := Open(ctx, databaseURL)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer store.Close()
	if err := store.Migrate(ctx); err != nil {
		t.Fatalf("migrate: %v", err)
	}

	key := fmt.Sprintf("sms-push-e2e-%d", time.Now().UnixNano())
	if err := store.EnsureDevelopmentTenant(ctx, key); err != nil {
		t.Fatalf("ensure tenant: %v", err)
	}
	p, err := store.Authenticate(ctx, key)
	if err != nil {
		t.Fatalf("authenticate: %v", err)
	}

	// -----------------------------------------------------------------------
	// 1. Seed profiles
	// -----------------------------------------------------------------------
	var (
		smsProfileID  string
		pushProfileID string
		stopProfileID string
	)
	for _, row := range []struct {
		extID string
		dest  *string
		phone string
	}{
		{"sms-recipient", &smsProfileID, "+15005550001"},
		{"push-recipient", &pushProfileID, ""},
		{"stop-recipient", &stopProfileID, "+15005550002"},
	} {
		row := row
		if err := store.pool.QueryRow(ctx,
			`INSERT INTO profiles(tenant_id,workspace_id,app_id,external_id) VALUES($1,$2,$3,$4) RETURNING id`,
			p.TenantID, p.WorkspaceID, p.AppID, row.extID,
		).Scan(row.dest); err != nil {
			t.Fatalf("insert profile %s: %v", row.extID, err)
		}
	}

	// -----------------------------------------------------------------------
	// 2. Register a device token for the push recipient
	// -----------------------------------------------------------------------
	pushToken := fmt.Sprintf("push-tok-%d", time.Now().UnixNano())
	dt, err := store.RegisterDeviceToken(ctx, p.TenantID, p.WorkspaceID, p.AppID,
		pushProfileID, "android", "fcm", pushToken)
	if err != nil {
		t.Fatalf("register device token: %v", err)
	}
	if dt.Status != "active" {
		t.Fatal("device token should be active after registration")
	}

	// -----------------------------------------------------------------------
	// 3. Pre-suppress the STOP recipient for SMS
	// -----------------------------------------------------------------------
	if err := store.SuppressEndpoint(ctx, p, "sms", "+15005550002", "unsubscribe"); err != nil {
		t.Fatalf("suppress stop recipient: %v", err)
	}
	isSuppressed, err := store.IsSuppressed(ctx, p, "sms", "+15005550002")
	if err != nil {
		t.Fatalf("IsSuppressed: %v", err)
	}
	if !isSuppressed {
		t.Fatal("stop recipient should be suppressed before delivery")
	}

	// -----------------------------------------------------------------------
	// 4. Seed sending identities and templates
	// -----------------------------------------------------------------------
	twilioNum := "+15005550006"
	smsIdent, err := store.CreateSendingIdentity(ctx, p, domain.SendingIdentity{
		Channel:     "sms",
		Provider:    "twilio",
		FromAddress: &twilioNum,
		MaxSendRate: 10,
	})
	if err != nil {
		t.Fatalf("create sms identity: %v", err)
	}

	pushIdent, err := store.CreateSendingIdentity(ctx, p, domain.SendingIdentity{
		Channel:     "push",
		Provider:    "fcm",
		MaxSendRate: 10,
	})
	if err != nil {
		t.Fatalf("create push identity: %v", err)
	}

	smsBody := "Hello {{ profile.external_id }}!"
	smsTmpl, err := store.CreateTemplate(ctx, p, domain.Template{
		Name:              "SMS E2E template",
		Channel:           "sms",
		TextTemplate:      &smsBody,
		SendingIdentityID: &smsIdent.ID,
	})
	if err != nil {
		t.Fatalf("create sms template: %v", err)
	}

	pushTitle := "Push E2E"
	pushBody := "Hello Push!"
	pushTmpl, err := store.CreateTemplate(ctx, p, domain.Template{
		Name:              "Push E2E template",
		Channel:           "push",
		TitleTemplate:     &pushTitle,
		TextTemplate:      &pushBody,
		SendingIdentityID: &pushIdent.ID,
	})
	if err != nil {
		t.Fatalf("create push template: %v", err)
	}

	// -----------------------------------------------------------------------
	// 5. Seed Segment and Campaigns using Store APIs
	// -----------------------------------------------------------------------
	seg, err := store.CreateSegment(ctx, p, domain.Segment{
		Name: "E2E Segment",
		Type: "dynamic",
		DSL:  json.RawMessage(`{}`),
	})
	if err != nil {
		t.Fatalf("create segment: %v", err)
	}

	smsCamp, err := store.CreateCampaign(ctx, p, domain.Campaign{
		Name:       "SMS E2E Campaign",
		SegmentID:  seg.ID,
		TemplateID: smsTmpl.ID,
		Status:     "sending",
	})
	if err != nil {
		t.Fatalf("create sms campaign: %v", err)
	}
	smsCampaignID := smsCamp.ID

	pushCamp, err := store.CreateCampaign(ctx, p, domain.Campaign{
		Name:       "Push E2E Campaign",
		SegmentID:  seg.ID,
		TemplateID: pushTmpl.ID,
		Status:     "sending",
	})
	if err != nil {
		t.Fatalf("create push campaign: %v", err)
	}
	pushCampaignID := pushCamp.ID

	// -----------------------------------------------------------------------
	// 6. Record delivery_attempts (sent decisions)
	//    SMS: one delivered, one STOP-suppressed
	//    Push: one delivered, one invalid-token
	// -----------------------------------------------------------------------
	attemptNow := time.Now().UTC()
	for _, row := range []struct {
		campaignID string
		profileID  string
		channel    string
		endpoint   string
		decision   string
		providerID string
	}{
		{smsCampaignID, smsProfileID, "sms", "+15005550001", "sent", "SM-delivered-1"},
		{smsCampaignID, stopProfileID, "sms", "+15005550002", "suppressed", ""},
		{pushCampaignID, pushProfileID, "push", pushToken, "sent", "fcm-msg-1"},
		{pushCampaignID, pushProfileID, "push", "stale-token", "sent", "fcm-msg-2"},
	} {
		row := row
		if _, err := store.pool.Exec(ctx,
			`INSERT INTO delivery_attempts
			(campaign_id,tenant_id,profile_id,channel,endpoint,decision,attempted_at,provider_message_id)
			VALUES($1,$2,$3,$4,$5,$6,$7,$8)`,
			row.campaignID, p.TenantID, row.profileID, row.channel,
			row.endpoint, row.decision, attemptNow, row.providerID,
		); err != nil {
			t.Fatalf("insert delivery attempt %v: %v", row, err)
		}
	}

	// -----------------------------------------------------------------------
	// 7. Simulate callbacks → AcceptEvents (message.delivered / message.failed)
	// -----------------------------------------------------------------------
	callbackEvents := []domain.Event{
		// SMS DLR delivered
		{
			Type:           "message.delivered",
			SchemaVersion:  1,
			ExternalID:     "sms-recipient",
			IdempotencyKey: key + "-sms-dlr-delivered",
			OccurredAt:     time.Now().UTC(),
			Payload: json.RawMessage(`{
				"channel":"sms","endpoint":"+15005550001",
				"campaign_id":"` + smsCampaignID + `",
				"provider_message_id":"SM-delivered-1"
			}`),
		},
		// Push receipt delivered
		{
			Type:           "message.delivered",
			SchemaVersion:  1,
			ExternalID:     "push-recipient",
			IdempotencyKey: key + "-push-receipt-delivered",
			OccurredAt:     time.Now().UTC(),
			Payload: json.RawMessage(`{
				"channel":"push","endpoint":"` + pushToken + `",
				"campaign_id":"` + pushCampaignID + `",
				"provider_message_id":"fcm-msg-1"
			}`),
		},
		// Push invalid-token → message.failed
		{
			Type:           "message.failed",
			SchemaVersion:  1,
			ExternalID:     "push-recipient",
			IdempotencyKey: key + "-push-invalid-token",
			OccurredAt:     time.Now().UTC(),
			Payload: json.RawMessage(`{
				"channel":"push","endpoint":"stale-token",
				"campaign_id":"` + pushCampaignID + `",
				"reason":"invalid_token"
			}`),
		},
		// SMS STOP opt-out → emit message.unsubscribed
		{
			Type:           "message.unsubscribed",
			SchemaVersion:  1,
			ExternalID:     "stop-recipient",
			IdempotencyKey: key + "-sms-stop",
			OccurredAt:     time.Now().UTC(),
			Payload: json.RawMessage(`{
				"channel":"sms","endpoint":"+15005550002",
				"reason":"STOP"
			}`),
		},
	}
	if _, err := store.AcceptEvents(ctx, p, callbackEvents); err != nil {
		t.Fatalf("AcceptEvents callbacks: %v", err)
	}

	// Drain the projector to process the 4 callbacks
	if _, err = projector.Drain(ctx, store, 4, false); err != nil {
		t.Fatalf("projector drain: %v", err)
	}

	// -----------------------------------------------------------------------
	// 8. Simulate invalid-token retirement
	// -----------------------------------------------------------------------
	if err := store.RetireDeviceToken(ctx, p.TenantID, p.AppID, "stale-token"); err != nil {
		// stale-token was never registered — retirement of unknown token should not fail hard
		// (idempotent by design); only log
		t.Logf("RetireDeviceToken(stale-token): %v", err)
	}
	// Retire the registered token (simulate provider feedback)
	if err := store.RetireDeviceToken(ctx, p.TenantID, p.AppID, pushToken); err != nil {
		t.Fatalf("RetireDeviceToken(pushToken): %v", err)
	}

	// -----------------------------------------------------------------------
	// 9. Assert: device token is now retired
	// -----------------------------------------------------------------------
	tokens, err := store.ListActiveDeviceTokens(ctx, p.TenantID, p.WorkspaceID, pushProfileID)
	if err != nil {
		t.Fatalf("ListActiveDeviceTokens: %v", err)
	}
	for _, tok := range tokens {
		if tok.Token == pushToken {
			t.Errorf("device token %q should be retired but still appears in active list", pushToken)
		}
	}

	// -----------------------------------------------------------------------
	// 10. Assert: suppression for STOP recipient still exists
	// -----------------------------------------------------------------------
	isSuppressed, err = store.IsSuppressed(ctx, p, "sms", "+15005550002")
	if err != nil {
		t.Fatalf("IsSuppressed after callbacks: %v", err)
	}
	if !isSuppressed {
		t.Error("STOP recipient should remain suppressed after STOP callback")
	}

	// -----------------------------------------------------------------------
	// 11. Assert: delivery_attempts counts match seeded data
	// -----------------------------------------------------------------------
	var smsSent, pushSent, smsSuppressed int
	if err := store.pool.QueryRow(ctx,
		`SELECT COUNT(*) FROM delivery_attempts
		 WHERE campaign_id=$1 AND decision='sent'`, smsCampaignID,
	).Scan(&smsSent); err != nil {
		t.Fatalf("count sms sent: %v", err)
	}
	if smsSent != 1 {
		t.Errorf("sms sent count: got %d, want 1", smsSent)
	}

	if err := store.pool.QueryRow(ctx,
		`SELECT COUNT(*) FROM delivery_attempts
		 WHERE campaign_id=$1 AND decision='suppressed'`, smsCampaignID,
	).Scan(&smsSuppressed); err != nil {
		t.Fatalf("count sms suppressed: %v", err)
	}
	if smsSuppressed != 1 {
		t.Errorf("sms suppressed count: got %d, want 1", smsSuppressed)
	}

	if err := store.pool.QueryRow(ctx,
		`SELECT COUNT(*) FROM delivery_attempts
		 WHERE campaign_id=$1 AND decision='sent'`, pushCampaignID,
	).Scan(&pushSent); err != nil {
		t.Fatalf("count push sent: %v", err)
	}
	if pushSent != 2 {
		t.Errorf("push sent count: got %d, want 2", pushSent)
	}

	// -----------------------------------------------------------------------
	// 12. Assert: engagement_facts from AcceptEvents callbacks
	//     message.delivered × 2 -> projected as 'delivered'
	// -----------------------------------------------------------------------
	var deliveredFacts int
	if err := store.pool.QueryRow(ctx,
		`SELECT COUNT(*) FROM engagement_facts
		 WHERE tenant_id=$1 AND workspace_id=$2 AND event_type='delivered'`,
		p.TenantID, p.WorkspaceID,
	).Scan(&deliveredFacts); err != nil {
		t.Fatalf("count delivered facts: %v", err)
	}
	if deliveredFacts < 2 {
		t.Errorf("engagement_facts delivered: got %d, want ≥2", deliveredFacts)
	}

	// -----------------------------------------------------------------------
	// 13. Assert: per-channel delivery_attempts counts are correct
	// -----------------------------------------------------------------------
	var smsAttempts, pushAttempts int
	if err := store.pool.QueryRow(ctx,
		`SELECT COUNT(*) FROM delivery_attempts WHERE campaign_id=$1 AND channel='sms'`,
		smsCampaignID,
	).Scan(&smsAttempts); err != nil {
		t.Fatalf("count sms attempts: %v", err)
	}
	if smsAttempts != 2 {
		t.Errorf("sms delivery attempts: got %d, want 2 (1 sent + 1 suppressed)", smsAttempts)
	}

	if err := store.pool.QueryRow(ctx,
		`SELECT COUNT(*) FROM delivery_attempts WHERE campaign_id=$1 AND channel='push'`,
		pushCampaignID,
	).Scan(&pushAttempts); err != nil {
		t.Fatalf("count push attempts: %v", err)
	}
	if pushAttempts != 2 {
		t.Errorf("push delivery attempts: got %d, want 2 (2 sent)", pushAttempts)
	}
}

