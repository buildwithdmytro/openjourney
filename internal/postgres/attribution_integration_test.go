package postgres

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/buildwithdmytro/openjourney/internal/domain"
)

func TestProjectEventAttributesFrozenGoalWithinWindowIdempotentlyAndByWorkspace(t *testing.T) {
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

	key := fmt.Sprintf("attribution-%d", time.Now().UnixNano())
	if err := store.EnsureDevelopmentTenant(ctx, key); err != nil {
		t.Fatal(err)
	}
	p, err := store.Authenticate(ctx, key)
	if err != nil {
		t.Fatal(err)
	}

	var profileID string
	err = store.pool.QueryRow(ctx, `INSERT INTO profiles
		(tenant_id, workspace_id, app_id, external_id)
		VALUES ($1,$2,$3,$4) RETURNING id`,
		p.TenantID, p.WorkspaceID, p.AppID, "buyer-1").Scan(&profileID)
	if err != nil {
		t.Fatal(err)
	}

	segment, err := store.CreateSegment(ctx, p, domain.Segment{
		Name: "Attribution audience", Type: "dynamic", DSL: json.RawMessage(`{}`),
	})
	if err != nil {
		t.Fatal(err)
	}
	from := fmt.Sprintf("attribution-%d@example.com", time.Now().UnixNano())
	identity, err := store.CreateSendingIdentity(ctx, p, domain.SendingIdentity{
		Channel: "email", FromAddress: &from, Provider: "ses", MaxSendRate: 1,
	})
	if err != nil {
		t.Fatal(err)
	}
	html := "Attribution"
	template, err := store.CreateTemplate(ctx, p, domain.Template{
		Name: "Attribution template", Channel: "email", HTMLTemplate: &html,
		SendingIdentityID: &identity.ID,
	})
	if err != nil {
		t.Fatal(err)
	}
	experiment, err := store.CreateExperiment(ctx, p, domain.Experiment{
		Name: "Revenue experiment", SubjectType: "campaign", Seed: "attribution-seed",
	})
	if err != nil {
		t.Fatal(err)
	}
	campaign, err := store.CreateCampaign(ctx, p, domain.Campaign{
		Name: "Frozen revenue campaign", SegmentID: segment.ID, TemplateID: template.ID,
		ExperimentID: &experiment.ID,
	})
	if err != nil {
		t.Fatal(err)
	}
	frozenGoal := json.RawMessage(`{"name":"purchase","event_type":"order.completed","filter":{"currency":"USD"},"value_field":"order.total","window":"2 hours"}`)
	if err := store.SaveCampaignManifestAndJobs(ctx, campaign.ID, "attribution-manifest", 1, 1, 1, frozenGoal, "2 hours", nil); err != nil {
		t.Fatal(err)
	}

	goalTime := time.Now().UTC().Truncate(time.Microsecond)
	sendTime := goalTime.Add(-time.Hour)
	_, err = store.pool.Exec(ctx, `INSERT INTO delivery_attempts
		(campaign_id, tenant_id, profile_id, channel, endpoint, decision, attempted_at, experiment_id, variant)
		VALUES ($1,$2,$3,'email','buyer@example.com','sent',$4,$5,'b')`,
		campaign.ID, p.TenantID, profileID, sendTime, experiment.ID)
	if err != nil {
		t.Fatal(err)
	}

	// A newer matching send in another workspace must never win attribution.
	var otherWorkspaceID, otherCampaignID string
	if err := store.pool.QueryRow(ctx, `INSERT INTO workspaces (tenant_id, name) VALUES ($1,$2) RETURNING id`,
		p.TenantID, "other-attribution-workspace").Scan(&otherWorkspaceID); err != nil {
		t.Fatal(err)
	}
	if err := store.pool.QueryRow(ctx, `INSERT INTO campaigns
		(tenant_id, workspace_id, name, segment_id, template_id, experiment_id, status,
		 conversion_goal, attribution_window)
		VALUES ($1,$2,'Other workspace campaign',$3,$4,$5,'sending',$6,'24 hours') RETURNING id`,
		p.TenantID, otherWorkspaceID, segment.ID, template.ID, experiment.ID, frozenGoal).Scan(&otherCampaignID); err != nil {
		t.Fatal(err)
	}
	_, err = store.pool.Exec(ctx, `INSERT INTO delivery_attempts
		(campaign_id, tenant_id, profile_id, channel, endpoint, decision, attempted_at, experiment_id, variant)
		VALUES ($1,$2,$3,'email','buyer@example.com','sent',$4,$5,'wrong-workspace')`,
		otherCampaignID, p.TenantID, profileID, goalTime.Add(-time.Minute), experiment.ID)
	if err != nil {
		t.Fatal(err)
	}

	inside := attributionTestEvent(t, store, p, "buyer-1", goalTime,
		json.RawMessage(`{"currency":"USD","order":{"total":42.75}}`))
	if err := store.ProjectEvent(ctx, inside); err != nil {
		t.Fatal(err)
	}
	if err := store.ProjectEvent(ctx, inside); err != nil {
		t.Fatal(err)
	}

	var (
		factCount       int
		sourceType      string
		sourceID        string
		factExperiment  string
		variant         string
		value           float64
		attributedSend  time.Time
		factWorkspaceID string
	)
	err = store.pool.QueryRow(ctx, `SELECT count(*), min(source_type), min(source_id::text),
		min(experiment_id::text), min(variant), min(value)::float8, min(attributed_send_at),
		min(workspace_id::text)
		FROM conversion_facts WHERE source_event_id=$1`, inside.ID).Scan(
		&factCount, &sourceType, &sourceID, &factExperiment, &variant, &value,
		&attributedSend, &factWorkspaceID,
	)
	if err != nil {
		t.Fatal(err)
	}
	if factCount != 1 {
		t.Fatalf("projecting the same event twice created %d facts, want 1", factCount)
	}
	if sourceType != "campaign" || sourceID != campaign.ID || factExperiment != experiment.ID || variant != "b" {
		t.Fatalf("attribution source=(%s,%s,%s,%s), want campaign %s experiment %s variant b",
			sourceType, sourceID, factExperiment, variant, campaign.ID, experiment.ID)
	}
	if factWorkspaceID != p.WorkspaceID {
		t.Fatalf("fact workspace=%s, want %s", factWorkspaceID, p.WorkspaceID)
	}
	if value != 42.75 {
		t.Fatalf("conversion value=%v, want 42.75", value)
	}
	if !attributedSend.Equal(sendTime) {
		t.Fatalf("attributed send=%s, want %s", attributedSend, sendTime)
	}

	outside := attributionTestEvent(t, store, p, "buyer-1", goalTime.Add(3*time.Hour),
		json.RawMessage(`{"currency":"USD","order":{"total":99}}`))
	if err := store.ProjectEvent(ctx, outside); err != nil {
		t.Fatal(err)
	}
	var outsideCount int
	if err := store.pool.QueryRow(ctx, `SELECT count(*) FROM conversion_facts WHERE source_event_id=$1`, outside.ID).Scan(&outsideCount); err != nil {
		t.Fatal(err)
	}
	if outsideCount != 0 {
		t.Fatalf("outside-window event created %d facts, want 0", outsideCount)
	}
}

func attributionTestEvent(t *testing.T, store *Store, p domain.Principal, externalID string, occurredAt time.Time, payload json.RawMessage) domain.AcceptedEvent {
	t.Helper()
	var id string
	if err := store.pool.QueryRow(context.Background(), `SELECT gen_random_uuid()::text`).Scan(&id); err != nil {
		t.Fatal(err)
	}
	return domain.AcceptedEvent{
		ID: id,
		Principal: domain.Principal{
			TenantID: p.TenantID, WorkspaceID: p.WorkspaceID, AppID: p.AppID,
		},
		Type: "order.completed", ExternalID: externalID, OccurredAt: occurredAt, Payload: payload,
	}
}
