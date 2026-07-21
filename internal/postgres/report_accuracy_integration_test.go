package postgres

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"os"
	"testing"
	"time"

	"github.com/buildwithdmytro/openjourney/internal/domain"
)

func TestReportAccuracyFromProjectedEvents(t *testing.T) {
	databaseURL := os.Getenv("OPENJOURNEY_TEST_DATABASE_URL")
	if databaseURL == "" {
		t.Skip("OPENJOURNEY_TEST_DATABASE_URL is not configured")
	}
	ctx := context.Background()
	store, err := Open(ctx, databaseURL)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	if err := store.Migrate(ctx); err != nil {
		t.Fatal(err)
	}

	key := fmt.Sprintf("report-accuracy-%d", time.Now().UnixNano())
	if err := store.EnsureDevelopmentTenant(ctx, key); err != nil {
		t.Fatal(err)
	}
	p, err := store.Authenticate(ctx, key)
	if err != nil {
		t.Fatal(err)
	}

	segment, err := store.CreateSegment(ctx, p, domain.Segment{
		Name: "Report accuracy audience", Type: "dynamic", DSL: json.RawMessage(`{}`),
	})
	if err != nil {
		t.Fatal(err)
	}
	from := fmt.Sprintf("report-accuracy-%d@example.com", time.Now().UnixNano())
	identity, err := store.CreateSendingIdentity(ctx, p, domain.SendingIdentity{
		Channel: "email", FromAddress: &from, Provider: "ses", MaxSendRate: 1,
	})
	if err != nil {
		t.Fatal(err)
	}
	html := "Report accuracy"
	template, err := store.CreateTemplate(ctx, p, domain.Template{
		Name: "Report accuracy template", Channel: "email", HTMLTemplate: &html,
		SendingIdentityID: &identity.ID,
	})
	if err != nil {
		t.Fatal(err)
	}
	experiment, err := store.CreateExperiment(ctx, p, domain.Experiment{
		Name: "Report accuracy experiment", SubjectType: "campaign", Seed: "report-accuracy-seed",
		PrimaryGoal: json.RawMessage(`{"name":"purchase","event_type":"order.completed"}`),
		Variants: []domain.ExperimentVariant{
			{Label: "control", Weight: 50, IsControl: true},
			{Label: "treatment", Weight: 50},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	campaign, err := store.CreateCampaign(ctx, p, domain.Campaign{
		Name: "Report accuracy campaign", SegmentID: segment.ID, TemplateID: template.ID,
		ExperimentID: &experiment.ID,
	})
	if err != nil {
		t.Fatal(err)
	}
	goal := json.RawMessage(`{"name":"purchase","event_type":"order.completed","value_field":"order.total"}`)
	result, err := store.pool.Exec(ctx, `UPDATE campaigns SET conversion_goal=$1, attribution_window='24 hours'
		WHERE tenant_id=$2 AND workspace_id=$3 AND id=$4`, goal, p.TenantID, p.WorkspaceID, campaign.ID)
	if err != nil {
		t.Fatal(err)
	}
	if result.RowsAffected() != 1 {
		t.Fatalf("updated campaign goals=%d, want 1", result.RowsAffected())
	}

	type recipient struct {
		profileID  string
		externalID string
		endpoint   string
		variant    string
	}
	recipients := make([]recipient, 40)
	sentAt := time.Now().UTC().Add(-time.Hour).Truncate(time.Microsecond)
	for i := range recipients {
		recipients[i].externalID = fmt.Sprintf("report-recipient-%02d", i)
		recipients[i].endpoint = fmt.Sprintf("report-recipient-%02d@example.com", i)
		recipients[i].variant = "control"
		if i >= 20 {
			recipients[i].variant = "treatment"
		}
		if err := store.pool.QueryRow(ctx, `INSERT INTO profiles
			(tenant_id,workspace_id,app_id,external_id) VALUES ($1,$2,$3,$4) RETURNING id`,
			p.TenantID, p.WorkspaceID, p.AppID, recipients[i].externalID).Scan(&recipients[i].profileID); err != nil {
			t.Fatal(err)
		}
		if _, err := store.pool.Exec(ctx, `INSERT INTO delivery_attempts
			(campaign_id,tenant_id,profile_id,channel,endpoint,decision,attempted_at,experiment_id,variant)
			VALUES ($1,$2,$3,'email',$4,'sent',$5,$6,$7)`, campaign.ID, p.TenantID,
			recipients[i].profileID, recipients[i].endpoint, sentAt, experiment.ID, recipients[i].variant); err != nil {
			t.Fatal(err)
		}
	}

	occurredAt := time.Now().UTC().Truncate(time.Microsecond)
	events := make([]domain.Event, 0, 75)
	addEvent := func(eventType string, recipient recipient, suffix string, payload json.RawMessage) {
		events = append(events, domain.Event{
			Type: eventType, SchemaVersion: 1, ExternalID: recipient.externalID,
			IdempotencyKey: fmt.Sprintf("report-%s-%s-%s", eventType, recipient.externalID, suffix),
			OccurredAt:     occurredAt, Payload: payload,
		})
	}
	for i, recipient := range recipients {
		addEvent("message.delivered", recipient, "delivered", json.RawMessage(fmt.Sprintf(
			`{"campaign_id":%q,"endpoint":%q}`, campaign.ID, recipient.endpoint)))
		if i < 15 {
			addEvent("email.opened", recipient, "opened", json.RawMessage(fmt.Sprintf(
				`{"template_id":%q,"dispatch_id":%q,"campaign_id":%q}`, template.ID, fmt.Sprintf("open-%02d", i), campaign.ID)))
		}
		if i < 6 {
			addEvent("link.clicked", recipient, "clicked", json.RawMessage(fmt.Sprintf(
				`{"template_id":%q,"dispatch_id":%q,"url":"https://example.com/offer","campaign_id":%q}`,
				template.ID, fmt.Sprintf("click-%02d", i), campaign.ID)))
		}
		if i < 3 {
			addEvent("message.bounced", recipient, "bounced", json.RawMessage(fmt.Sprintf(
				`{"campaign_id":%q,"channel":"email","endpoint":%q}`, campaign.ID, recipient.endpoint)))
		}
		if i == 3 {
			addEvent("message.complained", recipient, "complained", json.RawMessage(fmt.Sprintf(
				`{"campaign_id":%q,"channel":"email","endpoint":%q}`, campaign.ID, recipient.endpoint)))
		}
		if i < 2 || i >= 20 && i < 28 {
			addEvent("order.completed", recipient, "purchase", json.RawMessage(`{"order":{"total":25}}`))
		}
	}
	if len(events) != 75 {
		t.Fatalf("events=%d, want 75", len(events))
	}
	ids, err := store.AcceptEvents(ctx, p, events)
	if err != nil {
		t.Fatal(err)
	}
	for i, event := range events {
		accepted := domain.AcceptedEvent{
			ID: ids[i], Principal: p, Type: event.Type, SchemaVersion: event.SchemaVersion,
			ExternalID: event.ExternalID, IdempotencyKey: event.IdempotencyKey,
			OccurredAt: event.OccurredAt, Payload: event.Payload,
		}
		if err := store.ProjectEvent(ctx, accepted); err != nil {
			t.Fatalf("project %s for %s: %v", event.Type, event.ExternalID, err)
		}
	}

	campaignReport, err := store.CampaignReport(ctx, p, campaign.ID, domain.ReportQuery{})
	if err != nil {
		t.Fatal(err)
	}
	wantFunnel := domain.ReportFunnel{
		Targeted:  domain.ReportCount{Total: 40, Unique: 40},
		Sent:      domain.ReportCount{Total: 40, Unique: 40},
		Delivered: domain.ReportCount{Total: 40, Unique: 40},
		Opened:    domain.ReportCount{Total: 15, Unique: 15},
		Clicked:   domain.ReportCount{Total: 6, Unique: 6},
		Converted: domain.ReportCount{Total: 10, Unique: 10},
	}
	if campaignReport.Funnel != wantFunnel {
		t.Fatalf("campaign funnel=%+v, want %+v", campaignReport.Funnel, wantFunnel)
	}
	wantDeliverability := domain.ReportDeliverability{
		Bounced: domain.ReportCount{Total: 3, Unique: 3}, Complained: domain.ReportCount{Total: 1, Unique: 1},
		BounceRate: 0.075, ComplaintRate: 0.025,
	}
	if campaignReport.Deliverability != wantDeliverability {
		t.Fatalf("campaign deliverability=%+v, want %+v", campaignReport.Deliverability, wantDeliverability)
	}

	experimentReport, err := store.ExperimentReport(ctx, p, experiment.ID, domain.ReportQuery{})
	if err != nil {
		t.Fatal(err)
	}
	variants := make(map[string]domain.ExperimentVariantReport, len(experimentReport.Variants))
	for _, variant := range experimentReport.Variants {
		variants[variant.Label] = variant
	}
	control, treatment := variants["control"], variants["treatment"]
	if control.Sent != 20 || control.Conversions != 2 || control.Rate != 0.1 {
		t.Fatalf("control report=%+v, want sent=20 conversions=2 rate=0.1", control)
	}
	if treatment.Sent != 20 || treatment.Conversions != 8 || treatment.Rate != 0.4 || math.Abs(treatment.Uplift-3) > 1e-12 {
		t.Fatalf("treatment report=%+v, want sent=20 conversions=8 rate=0.4 uplift=3", treatment)
	}
	if math.Abs(treatment.PValue-0.02846) > 0.00001 {
		t.Fatalf("treatment p-value=%v, want about 0.02846", treatment.PValue)
	}
	if experimentReport.WinnerVariant == nil || *experimentReport.WinnerVariant != "treatment" {
		t.Fatalf("winner=%v, want treatment", experimentReport.WinnerVariant)
	}

	// Test time-range filtering: query with range that includes no events should return zero counts
	// but same structure as point-in-time report
	futureStart := occurredAt.Add(time.Hour)
	futureEnd := futureStart.Add(time.Hour)
	emptyRangeReport, err := store.CampaignReport(ctx, p, campaign.ID, domain.ReportQuery{
		Start: futureStart,
		End:   futureEnd,
	})
	if err != nil {
		t.Fatal(err)
	}
	emptyRangeFunnel := domain.ReportFunnel{
		Targeted:  domain.ReportCount{Total: 0, Unique: 0},
		Sent:      domain.ReportCount{Total: 0, Unique: 0},
		Delivered: domain.ReportCount{Total: 0, Unique: 0},
		Opened:    domain.ReportCount{Total: 0, Unique: 0},
		Clicked:   domain.ReportCount{Total: 0, Unique: 0},
		Converted: domain.ReportCount{Total: 0, Unique: 0},
	}
	if emptyRangeReport.Funnel != emptyRangeFunnel {
		t.Fatalf("empty range funnel=%+v, want %+v", emptyRangeReport.Funnel, emptyRangeFunnel)
	}

	// Test time-range filtering: query with range that includes all events should match point-in-time
	inclusiveStart := sentAt.Add(-time.Minute)
	inclusiveEnd := occurredAt.Add(time.Minute)
	inclusiveReport, err := store.CampaignReport(ctx, p, campaign.ID, domain.ReportQuery{
		Start: inclusiveStart,
		End:   inclusiveEnd,
	})
	if err != nil {
		t.Fatal(err)
	}
	if inclusiveReport.Funnel != wantFunnel {
		t.Fatalf("inclusive range funnel=%+v, want %+v", inclusiveReport.Funnel, wantFunnel)
	}

	// Test that ExperimentReport also supports time-range filtering
	experimentReportFuture, err := store.ExperimentReport(ctx, p, experiment.ID, domain.ReportQuery{
		Start: futureStart,
		End:   futureEnd,
	})
	if err != nil {
		t.Fatal(err)
	}
	futureVariants := make(map[string]domain.ExperimentVariantReport, len(experimentReportFuture.Variants))
	for _, variant := range experimentReportFuture.Variants {
		futureVariants[variant.Label] = variant
	}
	futureControl, futuretreatment := futureVariants["control"], futureVariants["treatment"]
	if futureControl.Sent != 0 || futuretreatment.Sent != 0 {
		t.Fatalf("future experiment sent control=%d treatment=%d, want both 0", futureControl.Sent, futuretreatment.Sent)
	}

	// Test that ExperimentReport with inclusive range returns all data
	experimentReportInclusive, err := store.ExperimentReport(ctx, p, experiment.ID, domain.ReportQuery{
		Start: inclusiveStart,
		End:   inclusiveEnd,
	})
	if err != nil {
		t.Fatal(err)
	}
	inclusiveVariants := make(map[string]domain.ExperimentVariantReport, len(experimentReportInclusive.Variants))
	for _, variant := range experimentReportInclusive.Variants {
		inclusiveVariants[variant.Label] = variant
	}
	inclusiveControl, inclusiveTreatment := inclusiveVariants["control"], inclusiveVariants["treatment"]
	if inclusiveControl.Sent != control.Sent || inclusiveControl.Conversions != control.Conversions ||
		inclusiveTreatment.Sent != treatment.Sent || inclusiveTreatment.Conversions != treatment.Conversions {
		t.Fatalf("inclusive experiment results differ from empty query: control=%+v vs %+v, treatment=%+v vs %+v",
			inclusiveControl, control, inclusiveTreatment, treatment)
	}
}

func TestFunnelOverTimeReport(t *testing.T) {
	databaseURL := os.Getenv("OPENJOURNEY_TEST_DATABASE_URL")
	if databaseURL == "" {
		t.Skip("OPENJOURNEY_TEST_DATABASE_URL is not configured")
	}
	ctx := context.Background()
	store, err := Open(ctx, databaseURL)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	if err := store.Migrate(ctx); err != nil {
		t.Fatal(err)
	}

	key := fmt.Sprintf("funnel-over-time-%d", time.Now().UnixNano())
	if err := store.EnsureDevelopmentTenant(ctx, key); err != nil {
		t.Fatal(err)
	}
	p, err := store.Authenticate(ctx, key)
	if err != nil {
		t.Fatal(err)
	}

	segment, err := store.CreateSegment(ctx, p, domain.Segment{
		Name: "Over-time audience", Type: "dynamic", DSL: json.RawMessage(`{}`),
	})
	if err != nil {
		t.Fatal(err)
	}
	from := fmt.Sprintf("funnel-over-time-%d@example.com", time.Now().UnixNano())
	identity, err := store.CreateSendingIdentity(ctx, p, domain.SendingIdentity{
		Channel: "email", FromAddress: &from, Provider: "ses", MaxSendRate: 1,
	})
	if err != nil {
		t.Fatal(err)
	}
	html := "Over-time report"
	template, err := store.CreateTemplate(ctx, p, domain.Template{
		Name: "Over-time template", Channel: "email", HTMLTemplate: &html,
		SendingIdentityID: &identity.ID,
	})
	if err != nil {
		t.Fatal(err)
	}
	campaign, err := store.CreateCampaign(ctx, p, domain.Campaign{
		Name: "Over-time campaign", SegmentID: segment.ID, TemplateID: template.ID,
	})
	if err != nil {
		t.Fatal(err)
	}

	// Create recipients and seed them with time-bucketed delivery attempts
	// We'll use 3 days: day 1 has 10 sent, day 2 has 15 sent, day 3 has 12 sent
	baseTime := time.Now().UTC().Truncate(time.Hour)
	day1 := baseTime.Add(-2 * 24 * time.Hour)
	day2 := baseTime.Add(-1 * 24 * time.Hour)
	day3 := baseTime

	type recipient struct {
		profileID  string
		externalID string
		endpoint   string
		day        time.Time
	}

	recipients := make([]recipient, 37)
	for i := 0; i < 10; i++ {
		recipients[i] = recipient{
			externalID: fmt.Sprintf("over-time-day1-%02d", i),
			endpoint:   fmt.Sprintf("over-time-day1-%02d@example.com", i),
			day:        day1,
		}
	}
	for i := 10; i < 25; i++ {
		recipients[i] = recipient{
			externalID: fmt.Sprintf("over-time-day2-%02d", i-10),
			endpoint:   fmt.Sprintf("over-time-day2-%02d@example.com", i-10),
			day:        day2,
		}
	}
	for i := 25; i < 37; i++ {
		recipients[i] = recipient{
			externalID: fmt.Sprintf("over-time-day3-%02d", i-25),
			endpoint:   fmt.Sprintf("over-time-day3-%02d@example.com", i-25),
			day:        day3,
		}
	}

	for i := range recipients {
		if err := store.pool.QueryRow(ctx, `INSERT INTO profiles
			(tenant_id,workspace_id,app_id,external_id) VALUES ($1,$2,$3,$4) RETURNING id`,
			p.TenantID, p.WorkspaceID, p.AppID, recipients[i].externalID).Scan(&recipients[i].profileID); err != nil {
			t.Fatal(err)
		}
		if _, err := store.pool.Exec(ctx, `INSERT INTO delivery_attempts
			(campaign_id,tenant_id,profile_id,channel,endpoint,decision,attempted_at)
			VALUES ($1,$2,$3,'email',$4,'sent',$5)`, campaign.ID, p.TenantID,
			recipients[i].profileID, recipients[i].endpoint, recipients[i].day); err != nil {
			t.Fatal(err)
		}
	}

	// Seed engagement events spread across buckets
	// Day 1: 8 delivered, 4 opened, 2 clicked, 1 bounced, 1 complained
	// Day 2: 12 delivered, 6 opened, 3 clicked, 2 bounced, 1 complained
	// Day 3: 10 delivered, 5 opened, 2 clicked, 1 bounced, 1 complained
	events := make([]domain.Event, 0, 80)
	addEvent := func(eventType string, recipient recipient, suffix string, payload json.RawMessage) {
		events = append(events, domain.Event{
			Type: eventType, SchemaVersion: 1, ExternalID: recipient.externalID,
			IdempotencyKey: fmt.Sprintf("over-time-%s-%s-%s", eventType, recipient.externalID, suffix),
			OccurredAt:     recipient.day, Payload: payload,
		})
	}

	// Day 1 events: 8 delivered, 4 opened, 2 clicked, 1 bounced, 1 complained
	for i := 0; i < 10; i++ {
		if i < 8 {
			addEvent("message.delivered", recipients[i], "delivered",
				json.RawMessage(fmt.Sprintf(`{"campaign_id":%q,"endpoint":%q}`, campaign.ID, recipients[i].endpoint)))
		}
		if i < 4 {
			addEvent("email.opened", recipients[i], "opened",
				json.RawMessage(fmt.Sprintf(`{"template_id":%q,"dispatch_id":%q,"campaign_id":%q}`,
					template.ID, fmt.Sprintf("open-d1-%02d", i), campaign.ID)))
		}
		if i < 2 {
			addEvent("link.clicked", recipients[i], "clicked",
				json.RawMessage(fmt.Sprintf(`{"template_id":%q,"dispatch_id":%q,"url":"https://example.com/offer","campaign_id":%q}`,
					template.ID, fmt.Sprintf("click-d1-%02d", i), campaign.ID)))
		}
		if i == 8 {
			addEvent("message.bounced", recipients[i], "bounced",
				json.RawMessage(fmt.Sprintf(`{"campaign_id":%q,"endpoint":%q,"bounce_type":"permanent"}`,
					campaign.ID, recipients[i].endpoint)))
		}
		if i == 9 {
			addEvent("message.complained", recipients[i], "complained",
				json.RawMessage(fmt.Sprintf(`{"campaign_id":%q,"endpoint":%q}`,
					campaign.ID, recipients[i].endpoint)))
		}
	}

	// Day 2 events: 12 delivered, 6 opened, 3 clicked, 2 bounced, 1 complained
	for i := 10; i < 25; i++ {
		if i < 22 {
			addEvent("message.delivered", recipients[i], "delivered",
				json.RawMessage(fmt.Sprintf(`{"campaign_id":%q,"endpoint":%q}`, campaign.ID, recipients[i].endpoint)))
		}
		if i < 16 {
			addEvent("email.opened", recipients[i], "opened",
				json.RawMessage(fmt.Sprintf(`{"template_id":%q,"dispatch_id":%q,"campaign_id":%q}`,
					template.ID, fmt.Sprintf("open-d2-%02d", i-10), campaign.ID)))
		}
		if i < 13 {
			addEvent("link.clicked", recipients[i], "clicked",
				json.RawMessage(fmt.Sprintf(`{"template_id":%q,"dispatch_id":%q,"url":"https://example.com/offer","campaign_id":%q}`,
					template.ID, fmt.Sprintf("click-d2-%02d", i-10), campaign.ID)))
		}
		if i == 22 || i == 23 {
			addEvent("message.bounced", recipients[i], "bounced",
				json.RawMessage(fmt.Sprintf(`{"campaign_id":%q,"endpoint":%q,"bounce_type":"permanent"}`,
					campaign.ID, recipients[i].endpoint)))
		}
		if i == 24 {
			addEvent("message.complained", recipients[i], "complained",
				json.RawMessage(fmt.Sprintf(`{"campaign_id":%q,"endpoint":%q}`,
					campaign.ID, recipients[i].endpoint)))
		}
	}

	// Day 3 events: 10 delivered, 5 opened, 2 clicked, 1 bounced, 1 complained
	for i := 25; i < 37; i++ {
		if i < 35 {
			addEvent("message.delivered", recipients[i], "delivered",
				json.RawMessage(fmt.Sprintf(`{"campaign_id":%q,"endpoint":%q}`, campaign.ID, recipients[i].endpoint)))
		}
		if i < 30 {
			addEvent("email.opened", recipients[i], "opened",
				json.RawMessage(fmt.Sprintf(`{"template_id":%q,"dispatch_id":%q,"campaign_id":%q}`,
					template.ID, fmt.Sprintf("open-d3-%02d", i-25), campaign.ID)))
		}
		if i < 27 {
			addEvent("link.clicked", recipients[i], "clicked",
				json.RawMessage(fmt.Sprintf(`{"template_id":%q,"dispatch_id":%q,"url":"https://example.com/offer","campaign_id":%q}`,
					template.ID, fmt.Sprintf("click-d3-%02d", i-25), campaign.ID)))
		}
		if i == 35 {
			addEvent("message.bounced", recipients[i], "bounced",
				json.RawMessage(fmt.Sprintf(`{"campaign_id":%q,"endpoint":%q,"bounce_type":"permanent"}`,
					campaign.ID, recipients[i].endpoint)))
		}
		if i == 36 {
			addEvent("message.complained", recipients[i], "complained",
				json.RawMessage(fmt.Sprintf(`{"campaign_id":%q,"endpoint":%q}`,
					campaign.ID, recipients[i].endpoint)))
		}
	}

	// Total events: Day 1 (16) + Day 2 (24) + Day 3 (19) = 59
	expectedEventCount := 16 + 24 + 19
	if len(events) != expectedEventCount {
		t.Fatalf("events=%d, want %d", len(events), expectedEventCount)
	}

	ids, err := store.AcceptEvents(ctx, p, events)
	if err != nil {
		t.Fatal(err)
	}
	for i, event := range events {
		accepted := domain.AcceptedEvent{
			ID: ids[i], Principal: p, Type: event.Type, SchemaVersion: event.SchemaVersion,
			ExternalID: event.ExternalID, IdempotencyKey: event.IdempotencyKey,
			OccurredAt: event.OccurredAt, Payload: event.Payload,
		}
		if err := store.ProjectEvent(ctx, accepted); err != nil {
			t.Fatalf("project %s for %s: %v", event.Type, event.ExternalID, err)
		}
	}

	// Test with daily granularity, spanning 3 days
	reportStart := day1.Truncate(24 * time.Hour)
	reportEnd := day3.Truncate(24 * time.Hour).Add(24 * time.Hour)
	overTimeReport, err := store.FunnelOverTimeReport(ctx, p, campaign.ID, domain.ReportQuery{
		Start:       reportStart,
		End:         reportEnd,
		Granularity: "day",
	})
	if err != nil {
		t.Fatal(err)
	}

	if len(overTimeReport.Buckets) != 3 {
		t.Fatalf("buckets=%d, want 3", len(overTimeReport.Buckets))
	}

	// Verify day 1: 10 sent, 8 delivered, 4 opened, 2 clicked, 1 bounced, 1 complained
	day1Bucket := overTimeReport.Buckets[0]
	if day1Bucket.Funnel.Sent.Total != 10 || day1Bucket.Funnel.Sent.Unique != 10 {
		t.Fatalf("day 1 sent=%+v, want {Total:10, Unique:10}", day1Bucket.Funnel.Sent)
	}
	if day1Bucket.Funnel.Delivered.Total != 8 || day1Bucket.Funnel.Delivered.Unique != 8 {
		t.Fatalf("day 1 delivered=%+v, want {Total:8, Unique:8}", day1Bucket.Funnel.Delivered)
	}
	if day1Bucket.Funnel.Opened.Total != 4 || day1Bucket.Funnel.Opened.Unique != 4 {
		t.Fatalf("day 1 opened=%+v, want {Total:4, Unique:4}", day1Bucket.Funnel.Opened)
	}
	if day1Bucket.Funnel.Clicked.Total != 2 || day1Bucket.Funnel.Clicked.Unique != 2 {
		t.Fatalf("day 1 clicked=%+v, want {Total:2, Unique:2}", day1Bucket.Funnel.Clicked)
	}
	if day1Bucket.Deliverability.Bounced.Total != 1 || day1Bucket.Deliverability.Bounced.Unique != 1 {
		t.Fatalf("day 1 bounced=%+v, want {Total:1, Unique:1}", day1Bucket.Deliverability.Bounced)
	}
	if day1Bucket.Deliverability.Complained.Total != 1 || day1Bucket.Deliverability.Complained.Unique != 1 {
		t.Fatalf("day 1 complained=%+v, want {Total:1, Unique:1}", day1Bucket.Deliverability.Complained)
	}
	expectedDay1BounceRate := 1.0 / 10.0 // 1 bounce / 10 sent
	if day1Bucket.Deliverability.BounceRate != expectedDay1BounceRate {
		t.Fatalf("day 1 bounce_rate=%v, want %v", day1Bucket.Deliverability.BounceRate, expectedDay1BounceRate)
	}
	expectedDay1ComplaintRate := 1.0 / 10.0 // 1 complaint / 10 sent
	if day1Bucket.Deliverability.ComplaintRate != expectedDay1ComplaintRate {
		t.Fatalf("day 1 complaint_rate=%v, want %v", day1Bucket.Deliverability.ComplaintRate, expectedDay1ComplaintRate)
	}

	// Verify day 2: 15 sent, 12 delivered, 6 opened, 3 clicked, 2 bounced, 1 complained
	day2Bucket := overTimeReport.Buckets[1]
	if day2Bucket.Funnel.Sent.Total != 15 || day2Bucket.Funnel.Sent.Unique != 15 {
		t.Fatalf("day 2 sent=%+v, want {Total:15, Unique:15}", day2Bucket.Funnel.Sent)
	}
	if day2Bucket.Funnel.Delivered.Total != 12 || day2Bucket.Funnel.Delivered.Unique != 12 {
		t.Fatalf("day 2 delivered=%+v, want {Total:12, Unique:12}", day2Bucket.Funnel.Delivered)
	}
	if day2Bucket.Funnel.Opened.Total != 6 || day2Bucket.Funnel.Opened.Unique != 6 {
		t.Fatalf("day 2 opened=%+v, want {Total:6, Unique:6}", day2Bucket.Funnel.Opened)
	}
	if day2Bucket.Funnel.Clicked.Total != 3 || day2Bucket.Funnel.Clicked.Unique != 3 {
		t.Fatalf("day 2 clicked=%+v, want {Total:3, Unique:3}", day2Bucket.Funnel.Clicked)
	}
	if day2Bucket.Deliverability.Bounced.Total != 2 || day2Bucket.Deliverability.Bounced.Unique != 2 {
		t.Fatalf("day 2 bounced=%+v, want {Total:2, Unique:2}", day2Bucket.Deliverability.Bounced)
	}
	if day2Bucket.Deliverability.Complained.Total != 1 || day2Bucket.Deliverability.Complained.Unique != 1 {
		t.Fatalf("day 2 complained=%+v, want {Total:1, Unique:1}", day2Bucket.Deliverability.Complained)
	}
	expectedDay2BounceRate := 2.0 / 15.0 // 2 bounces / 15 sent
	if day2Bucket.Deliverability.BounceRate != expectedDay2BounceRate {
		t.Fatalf("day 2 bounce_rate=%v, want %v", day2Bucket.Deliverability.BounceRate, expectedDay2BounceRate)
	}
	expectedDay2ComplaintRate := 1.0 / 15.0 // 1 complaint / 15 sent
	if day2Bucket.Deliverability.ComplaintRate != expectedDay2ComplaintRate {
		t.Fatalf("day 2 complaint_rate=%v, want %v", day2Bucket.Deliverability.ComplaintRate, expectedDay2ComplaintRate)
	}

	// Verify day 3: 12 sent, 10 delivered, 5 opened, 2 clicked, 1 bounced, 1 complained
	day3Bucket := overTimeReport.Buckets[2]
	if day3Bucket.Funnel.Sent.Total != 12 || day3Bucket.Funnel.Sent.Unique != 12 {
		t.Fatalf("day 3 sent=%+v, want {Total:12, Unique:12}", day3Bucket.Funnel.Sent)
	}
	if day3Bucket.Funnel.Delivered.Total != 10 || day3Bucket.Funnel.Delivered.Unique != 10 {
		t.Fatalf("day 3 delivered=%+v, want {Total:10, Unique:10}", day3Bucket.Funnel.Delivered)
	}
	if day3Bucket.Funnel.Opened.Total != 5 || day3Bucket.Funnel.Opened.Unique != 5 {
		t.Fatalf("day 3 opened=%+v, want {Total:5, Unique:5}", day3Bucket.Funnel.Opened)
	}
	if day3Bucket.Funnel.Clicked.Total != 2 || day3Bucket.Funnel.Clicked.Unique != 2 {
		t.Fatalf("day 3 clicked=%+v, want {Total:2, Unique:2}", day3Bucket.Funnel.Clicked)
	}
	if day3Bucket.Deliverability.Bounced.Total != 1 || day3Bucket.Deliverability.Bounced.Unique != 1 {
		t.Fatalf("day 3 bounced=%+v, want {Total:1, Unique:1}", day3Bucket.Deliverability.Bounced)
	}
	if day3Bucket.Deliverability.Complained.Total != 1 || day3Bucket.Deliverability.Complained.Unique != 1 {
		t.Fatalf("day 3 complained=%+v, want {Total:1, Unique:1}", day3Bucket.Deliverability.Complained)
	}
	expectedDay3BounceRate := 1.0 / 12.0 // 1 bounce / 12 sent
	if day3Bucket.Deliverability.BounceRate != expectedDay3BounceRate {
		t.Fatalf("day 3 bounce_rate=%v, want %v", day3Bucket.Deliverability.BounceRate, expectedDay3BounceRate)
	}
	expectedDay3ComplaintRate := 1.0 / 12.0 // 1 complaint / 12 sent
	if day3Bucket.Deliverability.ComplaintRate != expectedDay3ComplaintRate {
		t.Fatalf("day 3 complaint_rate=%v, want %v", day3Bucket.Deliverability.ComplaintRate, expectedDay3ComplaintRate)
	}
}

func TestRetentionReport(t *testing.T) {
	databaseURL := os.Getenv("OPENJOURNEY_TEST_DATABASE_URL")
	if databaseURL == "" {
		t.Skip("OPENJOURNEY_TEST_DATABASE_URL is not configured")
	}
	ctx := context.Background()
	store, err := Open(ctx, databaseURL)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	if err := store.Migrate(ctx); err != nil {
		t.Fatal(err)
	}

	key := fmt.Sprintf("retention-report-%d", time.Now().UnixNano())
	if err := store.EnsureDevelopmentTenant(ctx, key); err != nil {
		t.Fatal(err)
	}
	p, err := store.Authenticate(ctx, key)
	if err != nil {
		t.Fatal(err)
	}

	// Create campaign and related resources
	segment, err := store.CreateSegment(ctx, p, domain.Segment{
		Name: "Retention audience", Type: "dynamic", DSL: json.RawMessage(`{}`),
	})
	if err != nil {
		t.Fatal(err)
	}
	from := fmt.Sprintf("retention-report-%d@example.com", time.Now().UnixNano())
	identity, err := store.CreateSendingIdentity(ctx, p, domain.SendingIdentity{
		Channel: "email", FromAddress: &from, Provider: "ses", MaxSendRate: 1,
	})
	if err != nil {
		t.Fatal(err)
	}
	html := "Retention report"
	template, err := store.CreateTemplate(ctx, p, domain.Template{
		Name: "Retention template", Channel: "email", HTMLTemplate: &html,
		SendingIdentityID: &identity.ID,
	})
	if err != nil {
		t.Fatal(err)
	}
	campaign, err := store.CreateCampaign(ctx, p, domain.Campaign{
		Name: "Retention campaign", SegmentID: segment.ID, TemplateID: template.ID,
	})
	if err != nil {
		t.Fatal(err)
	}

	// Create retention cohorts across 3 days
	// Day 1: 5 profiles in cohort 1
	// Day 2: 6 profiles in cohort 2
	// Day 3: 4 profiles in cohort 3
	baseTime := time.Now().UTC().Truncate(time.Hour)
	day1 := baseTime.Add(-2 * 24 * time.Hour)
	day2 := baseTime.Add(-1 * 24 * time.Hour)
	day3 := baseTime

	type profileEvent struct {
		externalID string
		eventDays  []int // which days to create events (0=cohort day, 1=day+1, 2=day+2)
		cohortDay  time.Time
	}

	// Cohort 1 (day 1): 5 profiles
	// Profile 1-1: events on days 1, 2, 3 (retained through day 3)
	// Profile 1-2: events on days 1, 2 (retained through day 2)
	// Profile 1-3: events on days 1, 2 (retained through day 2)
	// Profile 1-4: events on day 1 only
	// Profile 1-5: events on day 1 only
	cohort1Events := []profileEvent{
		{"cohort1-profile1", []int{0, 1, 2}, day1},
		{"cohort1-profile2", []int{0, 1}, day1},
		{"cohort1-profile3", []int{0, 1}, day1},
		{"cohort1-profile4", []int{0}, day1},
		{"cohort1-profile5", []int{0}, day1},
	}

	// Cohort 2 (day 2): 6 profiles
	// Profile 2-1: events on days 2, 3 (retained through day 3)
	// Profile 2-2: events on days 2, 3 (retained through day 3)
	// Profile 2-3: events on day 2 only
	// Profile 2-4: events on day 2 only
	// Profile 2-5: events on day 2 only
	// Profile 2-6: events on day 2 only
	cohort2Events := []profileEvent{
		{"cohort2-profile1", []int{0, 1}, day2},
		{"cohort2-profile2", []int{0, 1}, day2},
		{"cohort2-profile3", []int{0}, day2},
		{"cohort2-profile4", []int{0}, day2},
		{"cohort2-profile5", []int{0}, day2},
		{"cohort2-profile6", []int{0}, day2},
	}

	// Cohort 3 (day 3): 4 profiles - all single-day
	// Profile 3-1: event on day 3
	// Profile 3-2: event on day 3
	// Profile 3-3: event on day 3
	// Profile 3-4: event on day 3
	cohort3Events := []profileEvent{
		{"cohort3-profile1", []int{0}, day3},
		{"cohort3-profile2", []int{0}, day3},
		{"cohort3-profile3", []int{0}, day3},
		{"cohort3-profile4", []int{0}, day3},
	}

	allProfiles := append(cohort1Events, append(cohort2Events, cohort3Events...)...)

	// Create profiles and seed engagement events
	events := make([]domain.Event, 0)
	for _, pe := range allProfiles {
		var profileID string
		if err := store.pool.QueryRow(ctx, `INSERT INTO profiles
			(tenant_id,workspace_id,app_id,external_id) VALUES ($1,$2,$3,$4) RETURNING id`,
			p.TenantID, p.WorkspaceID, p.AppID, pe.externalID).Scan(&profileID); err != nil {
			t.Fatal(err)
		}

		// Create delivery attempt on cohort day
		if _, err := store.pool.Exec(ctx, `INSERT INTO delivery_attempts
			(campaign_id,tenant_id,profile_id,channel,endpoint,decision,attempted_at)
			VALUES ($1,$2,$3,'email',$4,'sent',$5)`, campaign.ID, p.TenantID,
			profileID, pe.externalID+"@example.com", pe.cohortDay); err != nil {
			t.Fatal(err)
		}

		// Create engagement events on specified days
		for _, dayOffset := range pe.eventDays {
			eventTime := pe.cohortDay.Add(time.Duration(dayOffset*24) * time.Hour)
			events = append(events, domain.Event{
				Type:           "message.delivered",
				SchemaVersion:  1,
				ExternalID:     pe.externalID,
				IdempotencyKey: fmt.Sprintf("retention-%s-d%d", pe.externalID, dayOffset),
				OccurredAt:     eventTime,
				Payload:        json.RawMessage(fmt.Sprintf(`{"campaign_id":%q,"endpoint":%q}`, campaign.ID, pe.externalID+"@example.com")),
			})
		}
	}

	// Accept and project events
	ids, err := store.AcceptEvents(ctx, p, events)
	if err != nil {
		t.Fatal(err)
	}
	for i, event := range events {
		accepted := domain.AcceptedEvent{
			ID: ids[i], Principal: p, Type: event.Type, SchemaVersion: event.SchemaVersion,
			ExternalID: event.ExternalID, IdempotencyKey: event.IdempotencyKey,
			OccurredAt: event.OccurredAt, Payload: event.Payload,
		}
		if err := store.ProjectEvent(ctx, accepted); err != nil {
			t.Fatalf("project %s for %s: %v", event.Type, event.ExternalID, err)
		}
	}

	// Test retention report with daily granularity
	reportStart := day1.Truncate(24 * time.Hour)
	reportEnd := day3.Truncate(24 * time.Hour).Add(24 * time.Hour)
	retentionReport, err := store.RetentionReport(ctx, p, campaign.ID, domain.ReportQuery{
		Start:       reportStart,
		End:         reportEnd,
		Granularity: "day",
	})
	if err != nil {
		t.Fatal(err)
	}

	if retentionReport.CampaignID != campaign.ID {
		t.Errorf("campaign_id = %q, want %q", retentionReport.CampaignID, campaign.ID)
	}

	if len(retentionReport.Cohorts) != 3 {
		t.Fatalf("cohorts=%d, want 3", len(retentionReport.Cohorts))
	}

	// Verify Cohort 1 (day 1): [5 retained on day 0, 4 on day 1, 2 on day 2]
	cohort1 := retentionReport.Cohorts[0]
	expectedCohort1Sizes := []int64{5, 4, 2}
	if len(cohort1.Sizes) != len(expectedCohort1Sizes) {
		t.Fatalf("cohort 1 sizes length=%d, want %d; sizes=%v", len(cohort1.Sizes), len(expectedCohort1Sizes), cohort1.Sizes)
	}
	for i, expected := range expectedCohort1Sizes {
		if cohort1.Sizes[i] != expected {
			t.Errorf("cohort 1 day %d: size=%d, want %d", i, cohort1.Sizes[i], expected)
		}
	}

	// Verify Cohort 2 (day 2): [6 retained on day 0, 2 on day 1]
	cohort2 := retentionReport.Cohorts[1]
	expectedCohort2Sizes := []int64{6, 2}
	if len(cohort2.Sizes) != len(expectedCohort2Sizes) {
		t.Fatalf("cohort 2 sizes length=%d, want %d; sizes=%v", len(cohort2.Sizes), len(expectedCohort2Sizes), cohort2.Sizes)
	}
	for i, expected := range expectedCohort2Sizes {
		if cohort2.Sizes[i] != expected {
			t.Errorf("cohort 2 day %d: size=%d, want %d", i, cohort2.Sizes[i], expected)
		}
	}

	// Verify Cohort 3 (day 3): [4 retained on day 0]
	cohort3 := retentionReport.Cohorts[2]
	expectedCohort3Sizes := []int64{4}
	if len(cohort3.Sizes) != len(expectedCohort3Sizes) {
		t.Fatalf("cohort 3 sizes length=%d, want %d; sizes=%v", len(cohort3.Sizes), len(expectedCohort3Sizes), cohort3.Sizes)
	}
	for i, expected := range expectedCohort3Sizes {
		if cohort3.Sizes[i] != expected {
			t.Errorf("cohort 3 day %d: size=%d, want %d", i, cohort3.Sizes[i], expected)
		}
	}
}

func TestGrowthReport(t *testing.T) {
	databaseURL := os.Getenv("OPENJOURNEY_TEST_DATABASE_URL")
	if databaseURL == "" {
		t.Skip("OPENJOURNEY_TEST_DATABASE_URL is not configured")
	}
	ctx := context.Background()
	store, err := Open(ctx, databaseURL)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	if err := store.Migrate(ctx); err != nil {
		t.Fatal(err)
	}

	key := fmt.Sprintf("growth-report-%d", time.Now().UnixNano())
	if err := store.EnsureDevelopmentTenant(ctx, key); err != nil {
		t.Fatal(err)
	}
	p, err := store.Authenticate(ctx, key)
	if err != nil {
		t.Fatal(err)
	}

	segment, err := store.CreateSegment(ctx, p, domain.Segment{
		Name: "Growth audience", Type: "dynamic", DSL: json.RawMessage(`{}`),
	})
	if err != nil {
		t.Fatal(err)
	}
	from := fmt.Sprintf("growth-report-%d@example.com", time.Now().UnixNano())
	identity, err := store.CreateSendingIdentity(ctx, p, domain.SendingIdentity{
		Channel: "email", FromAddress: &from, Provider: "ses", MaxSendRate: 1,
	})
	if err != nil {
		t.Fatal(err)
	}
	html := "Growth report"
	template, err := store.CreateTemplate(ctx, p, domain.Template{
		Name: "Growth template", Channel: "email", HTMLTemplate: &html,
		SendingIdentityID: &identity.ID,
	})
	if err != nil {
		t.Fatal(err)
	}
	campaign, err := store.CreateCampaign(ctx, p, domain.Campaign{
		Name: "Growth campaign", SegmentID: segment.ID, TemplateID: template.ID,
	})
	if err != nil {
		t.Fatal(err)
	}

	// Create base times for 3 days
	baseTime := time.Now().UTC().Truncate(time.Hour)
	day1 := baseTime.Add(-2 * 24 * time.Hour)
	day2 := baseTime.Add(-1 * 24 * time.Hour)
	day3 := baseTime

	// Seed profiles with created_at timestamps across 3 days
	// Day 1: 5 profiles, Day 2: 8 profiles, Day 3: 7 profiles
	profileIDs := make([]string, 0, 20)

	for i := 0; i < 5; i++ {
		var profileID string
		externalID := fmt.Sprintf("growth-day1-%02d", i)
		if err := store.pool.QueryRow(ctx, `INSERT INTO profiles
			(tenant_id,workspace_id,app_id,external_id,created_at) VALUES ($1,$2,$3,$4,$5) RETURNING id`,
			p.TenantID, p.WorkspaceID, p.AppID, externalID, day1).Scan(&profileID); err != nil {
			t.Fatal(err)
		}
		profileIDs = append(profileIDs, profileID)
	}

	for i := 0; i < 8; i++ {
		var profileID string
		externalID := fmt.Sprintf("growth-day2-%02d", i)
		if err := store.pool.QueryRow(ctx, `INSERT INTO profiles
			(tenant_id,workspace_id,app_id,external_id,created_at) VALUES ($1,$2,$3,$4,$5) RETURNING id`,
			p.TenantID, p.WorkspaceID, p.AppID, externalID, day2).Scan(&profileID); err != nil {
			t.Fatal(err)
		}
		profileIDs = append(profileIDs, profileID)
	}

	for i := 0; i < 7; i++ {
		var profileID string
		externalID := fmt.Sprintf("growth-day3-%02d", i)
		if err := store.pool.QueryRow(ctx, `INSERT INTO profiles
			(tenant_id,workspace_id,app_id,external_id,created_at) VALUES ($1,$2,$3,$4,$5) RETURNING id`,
			p.TenantID, p.WorkspaceID, p.AppID, externalID, day3).Scan(&profileID); err != nil {
			t.Fatal(err)
		}
		profileIDs = append(profileIDs, profileID)
	}

	// Seed segment memberships
	// Day 1: 4 memberships, Day 2: 6 memberships, Day 3: 5 memberships
	// All with membership='include'

	for i := 0; i < 4; i++ {
		if _, err := store.pool.Exec(ctx, `INSERT INTO segment_members
			(segment_id,profile_id,tenant_id,membership,created_at)
			VALUES ($1,$2,$3,'include',$4)`, segment.ID, profileIDs[i], p.TenantID, day1); err != nil {
			t.Fatal(err)
		}
	}

	for i := 0; i < 6; i++ {
		if _, err := store.pool.Exec(ctx, `INSERT INTO segment_members
			(segment_id,profile_id,tenant_id,membership,created_at)
			VALUES ($1,$2,$3,'include',$4)`, segment.ID, profileIDs[5+i], p.TenantID, day2); err != nil {
			t.Fatal(err)
		}
	}

	for i := 0; i < 5; i++ {
		if _, err := store.pool.Exec(ctx, `INSERT INTO segment_members
			(segment_id,profile_id,tenant_id,membership,created_at)
			VALUES ($1,$2,$3,'include',$4)`, segment.ID, profileIDs[13+i], p.TenantID, day3); err != nil {
			t.Fatal(err)
		}
	}

	// Test with daily granularity, spanning 3 days
	reportStart := day1.Truncate(24 * time.Hour)
	reportEnd := day3.Truncate(24 * time.Hour).Add(24 * time.Hour)
	growthReport, err := store.GrowthReport(ctx, p, campaign.ID, domain.ReportQuery{
		Start:       reportStart,
		End:         reportEnd,
		Granularity: "day",
	})
	if err != nil {
		t.Fatal(err)
	}

	if growthReport.CampaignID != campaign.ID {
		t.Errorf("campaign_id = %q, want %q", growthReport.CampaignID, campaign.ID)
	}

	if len(growthReport.Buckets) != 3 {
		t.Fatalf("buckets=%d, want 3", len(growthReport.Buckets))
	}

	// Verify day 1: 5 new profiles, 4 segment memberships, 4 net growth
	day1Bucket := growthReport.Buckets[0]
	if day1Bucket.NewProfiles != 5 {
		t.Errorf("day 1 new_profiles=%d, want 5", day1Bucket.NewProfiles)
	}
	if day1Bucket.SegmentMemberships != 4 {
		t.Errorf("day 1 segment_memberships=%d, want 4", day1Bucket.SegmentMemberships)
	}
	if day1Bucket.NetGrowth != 4 {
		t.Errorf("day 1 net_growth=%d, want 4", day1Bucket.NetGrowth)
	}

	// Verify day 2: 8 new profiles, 6 segment memberships, 6 net growth
	day2Bucket := growthReport.Buckets[1]
	if day2Bucket.NewProfiles != 8 {
		t.Errorf("day 2 new_profiles=%d, want 8", day2Bucket.NewProfiles)
	}
	if day2Bucket.SegmentMemberships != 6 {
		t.Errorf("day 2 segment_memberships=%d, want 6", day2Bucket.SegmentMemberships)
	}
	if day2Bucket.NetGrowth != 6 {
		t.Errorf("day 2 net_growth=%d, want 6", day2Bucket.NetGrowth)
	}

	// Verify day 3: 7 new profiles, 5 segment memberships, 5 net growth
	day3Bucket := growthReport.Buckets[2]
	if day3Bucket.NewProfiles != 7 {
		t.Errorf("day 3 new_profiles=%d, want 7", day3Bucket.NewProfiles)
	}
	if day3Bucket.SegmentMemberships != 5 {
		t.Errorf("day 3 segment_memberships=%d, want 5", day3Bucket.SegmentMemberships)
	}
	if day3Bucket.NetGrowth != 5 {
		t.Errorf("day 3 net_growth=%d, want 5", day3Bucket.NetGrowth)
	}
}
