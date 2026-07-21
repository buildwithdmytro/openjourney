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
}
