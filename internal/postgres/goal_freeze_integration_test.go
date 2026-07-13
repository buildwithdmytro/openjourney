package postgres

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/buildwithdmytro/openjourney/internal/campaigns"
	"github.com/buildwithdmytro/openjourney/internal/domain"
	journeyflow "github.com/buildwithdmytro/openjourney/internal/journey"
)

func TestConversionGoalsFreezeAtCampaignDispatchAndJourneyPublish(t *testing.T) {
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

	key := fmt.Sprintf("goal-freeze-%d", time.Now().UnixNano())
	if err := store.EnsureDevelopmentTenant(ctx, key); err != nil {
		t.Fatal(err)
	}
	p, err := store.Authenticate(ctx, key)
	if err != nil {
		t.Fatal(err)
	}

	segment, err := store.CreateSegment(ctx, p, domain.Segment{Name: "Goal audience", Type: "dynamic", DSL: json.RawMessage(`{}`)})
	if err != nil {
		t.Fatal(err)
	}
	from := "goals@example.com"
	identity, err := store.CreateSendingIdentity(ctx, p, domain.SendingIdentity{Channel: "email", FromAddress: &from, Provider: "ses", MaxSendRate: 1})
	if err != nil {
		t.Fatal(err)
	}
	html := "Goal"
	template, err := store.CreateTemplate(ctx, p, domain.Template{Name: "Goal template", Channel: "email", HTMLTemplate: &html, SendingIdentityID: &identity.ID})
	if err != nil {
		t.Fatal(err)
	}

	originalCampaignGoal := json.RawMessage(`{"name":"purchase","event_type":"order.completed","value_field":"total","window":"3 days"}`)
	exp, err := store.CreateExperiment(ctx, p, domain.Experiment{
		Name: "Campaign goal", SubjectType: "campaign", Seed: "freeze", PrimaryGoal: originalCampaignGoal,
	})
	if err != nil {
		t.Fatal(err)
	}
	scheduledAt := time.Now().Add(-time.Minute)
	campaign, err := store.CreateCampaign(ctx, p, domain.Campaign{
		Name: "Frozen campaign", SegmentID: segment.ID, TemplateID: template.ID, ExperimentID: &exp.ID,
		Status: "scheduled", ScheduledAt: &scheduledAt,
	})
	if err != nil {
		t.Fatal(err)
	}
	dispatched, err := campaigns.DispatchNext(ctx, store, &memoryBlobs{objects: map[string][]byte{}})
	if err != nil {
		t.Fatal(err)
	}
	if !dispatched {
		t.Fatal("scheduled campaign was not dispatched")
	}
	if campaign.ManifestKey != nil {
		t.Fatal("test precondition failed: draft campaign unexpectedly had a manifest")
	}
	exp.PrimaryGoal = json.RawMessage(`{"name":"signup","event_type":"account.created","window":"1 hour"}`)
	if _, err := store.UpdateExperiment(ctx, p, exp); err != nil {
		t.Fatal(err)
	}
	frozenCampaign, err := store.GetCampaign(ctx, p, campaign.ID)
	if err != nil {
		t.Fatal(err)
	}
	assertFrozenGoal(t, frozenCampaign.ConversionGoal, frozenCampaign.AttributionWindow, "order.completed", "3 days")

	originalJourneyGraph := json.RawMessage(`{
		"entry_node_id":"entry",
		"nodes":[
			{"id":"entry","type":"entry","config":{"trigger":"event","event_type":"lead.created"}},
			{"id":"goal","type":"goal","config":{"name":"purchase","event_type":"order.completed","value_field":"total","window":"7 days"}},
			{"id":"exit","type":"exit","config":{"reason":"completed"}}
		],
		"edges":[{"from":"entry","to":"goal"},{"from":"goal","to":"exit"}]
	}`)
	journey, err := store.CreateJourney(ctx, p, domain.Journey{Name: "Frozen journey", Graph: originalJourneyGraph})
	if err != nil {
		t.Fatal(err)
	}
	version, err := journeyflow.Publish(ctx, store, &memoryBlobs{objects: map[string][]byte{}}, p, journey.ID, "00000000-0000-0000-0000-000000000001")
	if err != nil {
		t.Fatal(err)
	}
	journey.Status = "draft"
	journey.Graph = json.RawMessage(`{
		"entry_node_id":"entry",
		"nodes":[
			{"id":"entry","type":"entry","config":{"trigger":"event","event_type":"lead.created"}},
			{"id":"goal","type":"goal","config":{"name":"signup","event_type":"account.created","window":"1 hour"}},
			{"id":"exit","type":"exit","config":{"reason":"completed"}}
		],
		"edges":[{"from":"entry","to":"goal"},{"from":"goal","to":"exit"}]
	}`)
	if _, err := store.UpdateJourney(ctx, p, journey); err != nil {
		t.Fatal(err)
	}
	frozenVersion, err := store.GetJourneyVersionNumber(ctx, p, journey.ID, version.Version)
	if err != nil {
		t.Fatal(err)
	}
	assertFrozenGoal(t, frozenVersion.ConversionGoal, frozenVersion.AttributionWindow, "order.completed", "7 days")
}

func assertFrozenGoal(t *testing.T, raw json.RawMessage, window *string, eventType, wantWindow string) {
	t.Helper()
	var goal struct {
		EventType string `json:"event_type"`
	}
	if err := json.Unmarshal(raw, &goal); err != nil {
		t.Fatalf("decode frozen goal: %v (raw=%s)", err, raw)
	}
	if goal.EventType != eventType {
		t.Fatalf("frozen event type=%q want %q", goal.EventType, eventType)
	}
	if window == nil || *window != wantWindow {
		t.Fatalf("frozen attribution window=%v want %q", window, wantWindow)
	}
}
