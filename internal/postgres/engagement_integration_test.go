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

func TestProjectEventProjectsEngagementFactsIdempotentlyAndByWorkspace(t *testing.T) {
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

	key := fmt.Sprintf("engagement-%d", time.Now().UnixNano())
	if err := store.EnsureDevelopmentTenant(ctx, key); err != nil {
		t.Fatal(err)
	}
	p, err := store.Authenticate(ctx, key)
	if err != nil {
		t.Fatal(err)
	}

	var profileID string
	if err := store.pool.QueryRow(ctx, `INSERT INTO profiles
		(tenant_id, workspace_id, app_id, external_id)
		VALUES ($1,$2,$3,$4) RETURNING id`,
		p.TenantID, p.WorkspaceID, p.AppID, "engaged-profile").Scan(&profileID); err != nil {
		t.Fatal(err)
	}
	segment, err := store.CreateSegment(ctx, p, domain.Segment{
		Name: "Engagement audience", Type: "dynamic", DSL: json.RawMessage(`{}`),
	})
	if err != nil {
		t.Fatal(err)
	}
	from := fmt.Sprintf("engagement-%d@example.com", time.Now().UnixNano())
	identity, err := store.CreateSendingIdentity(ctx, p, domain.SendingIdentity{
		Channel: "email", FromAddress: &from, Provider: "ses", MaxSendRate: 1,
	})
	if err != nil {
		t.Fatal(err)
	}
	html := "Engagement"
	template, err := store.CreateTemplate(ctx, p, domain.Template{
		Name: "Engagement template", Channel: "email", HTMLTemplate: &html,
		SendingIdentityID: &identity.ID,
	})
	if err != nil {
		t.Fatal(err)
	}
	experiment, err := store.CreateExperiment(ctx, p, domain.Experiment{
		Name: "Engagement experiment", SubjectType: "campaign", Seed: "engagement-seed",
	})
	if err != nil {
		t.Fatal(err)
	}
	campaign, err := store.CreateCampaign(ctx, p, domain.Campaign{
		Name: "Engagement campaign", SegmentID: segment.ID, TemplateID: template.ID,
		ExperimentID: &experiment.ID,
	})
	if err != nil {
		t.Fatal(err)
	}
	_, err = store.pool.Exec(ctx, `INSERT INTO delivery_attempts
		(campaign_id, tenant_id, profile_id, channel, endpoint, decision, attempted_at,
		 experiment_id, variant)
		VALUES ($1,$2,$3,'email','campaign@example.com','sent',now(),$4,'campaign-a')`,
		campaign.ID, p.TenantID, profileID, experiment.ID)
	if err != nil {
		t.Fatal(err)
	}

	var journeyID, versionID, runID string
	if err := store.pool.QueryRow(ctx, `INSERT INTO journeys
		(tenant_id, workspace_id, name, graph, status)
		VALUES ($1,$2,'Engagement journey','{}','published') RETURNING id`,
		p.TenantID, p.WorkspaceID).Scan(&journeyID); err != nil {
		t.Fatal(err)
	}
	if err := store.pool.QueryRow(ctx, `INSERT INTO journey_versions
		(journey_id, tenant_id, workspace_id, version, graph, entry_kind, status)
		VALUES ($1,$2,$3,1,'{}','event','active') RETURNING id`,
		journeyID, p.TenantID, p.WorkspaceID).Scan(&versionID); err != nil {
		t.Fatal(err)
	}
	if err := store.pool.QueryRow(ctx, `INSERT INTO journey_runs
		(tenant_id, workspace_id, journey_id, journey_version_id, profile_id, entry_key, current_node_id)
		VALUES ($1,$2,$3,$4,$5,'engagement-entry','message-1') RETURNING id`,
		p.TenantID, p.WorkspaceID, journeyID, versionID, profileID).Scan(&runID); err != nil {
		t.Fatal(err)
	}
	_, err = store.pool.Exec(ctx, `INSERT INTO journey_message_intents
		(run_id, tenant_id, workspace_id, journey_id, journey_version_id, node_id, profile_id,
		 template_id, channel, endpoint, status, decision, experiment_id, variant)
		VALUES ($1,$2,$3,$4,$5,'message-1',$6,$7,'email','journey@example.com','completed',
		 'sent',$8,'journey-b')`, runID, p.TenantID, p.WorkspaceID, journeyID, versionID,
		profileID, template.ID, experiment.ID)
	if err != nil {
		t.Fatal(err)
	}

	tests := []struct {
		name       string
		eventType  string
		payload    json.RawMessage
		sourceType string
		sourceID   string
		nodeID     string
		variant    string
		factType   string
	}{
		{"delivered campaign", "message.delivered", json.RawMessage(fmt.Sprintf(`{"campaign_id":%q,"endpoint":"campaign@example.com"}`, campaign.ID)), "campaign", campaign.ID, "", "campaign-a", "delivered"},
		{"opened journey", "email.opened", json.RawMessage(fmt.Sprintf(`{"template_id":%q,"dispatch_id":"journey-open","journey_id":%q,"node_id":"message-1"}`, template.ID, journeyID)), "journey", journeyID, "message-1", "journey-b", "opened"},
		{"clicked campaign", "link.clicked", json.RawMessage(fmt.Sprintf(`{"template_id":%q,"dispatch_id":"campaign-click","url":"https://example.com","campaign_id":%q}`, template.ID, campaign.ID)), "campaign", campaign.ID, "", "campaign-a", "clicked"},
		{"bounced journey", "message.bounced", json.RawMessage(fmt.Sprintf(`{"journey_id":%q,"node_id":"message-1","endpoint":"journey@example.com"}`, journeyID)), "journey", journeyID, "message-1", "journey-b", "bounced"},
		{"complained campaign", "message.complained", json.RawMessage(fmt.Sprintf(`{"campaign_id":%q,"endpoint":"campaign@example.com"}`, campaign.ID)), "campaign", campaign.ID, "", "campaign-a", "complained"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			event := engagementTestEvent(t, store, p, tt.eventType, tt.payload)
			if err := store.ProjectEvent(ctx, event); err != nil {
				t.Fatal(err)
			}
			if err := store.ProjectEvent(ctx, event); err != nil {
				t.Fatal(err)
			}

			var count int
			var sourceType, sourceID, variant, factType, factProfileID, workspaceID string
			var nodeID *string
			err := store.pool.QueryRow(ctx, `SELECT count(*), min(source_type), min(source_id::text),
				min(node_id), min(variant), min(event_type), min(profile_id::text), min(workspace_id::text)
				FROM engagement_facts WHERE source_event_id=$1`, event.ID).Scan(
				&count, &sourceType, &sourceID, &nodeID, &variant, &factType, &factProfileID, &workspaceID)
			if err != nil {
				t.Fatal(err)
			}
			if count != 1 {
				t.Fatalf("projecting twice created %d facts, want 1", count)
			}
			if sourceType != tt.sourceType || sourceID != tt.sourceID || variant != tt.variant || factType != tt.factType {
				t.Fatalf("fact=(%s,%s,%s,%s), want (%s,%s,%s,%s)", sourceType, sourceID,
					variant, factType, tt.sourceType, tt.sourceID, tt.variant, tt.factType)
			}
			if tt.nodeID == "" && nodeID != nil || tt.nodeID != "" && (nodeID == nil || *nodeID != tt.nodeID) {
				t.Fatalf("node_id=%v, want %q", nodeID, tt.nodeID)
			}
			if factProfileID != profileID || workspaceID != p.WorkspaceID {
				t.Fatalf("fact profile/workspace=(%s,%s), want (%s,%s)", factProfileID,
					workspaceID, profileID, p.WorkspaceID)
			}
		})
	}

	var otherWorkspaceID string
	if err := store.pool.QueryRow(ctx, `INSERT INTO workspaces (tenant_id, name)
		VALUES ($1,'Other engagement workspace') RETURNING id`, p.TenantID).Scan(&otherWorkspaceID); err != nil {
		t.Fatal(err)
	}
	wrongWorkspace := p
	wrongWorkspace.WorkspaceID = otherWorkspaceID
	isolationEvent := engagementTestEvent(t, store, wrongWorkspace, "message.delivered",
		json.RawMessage(fmt.Sprintf(`{"campaign_id":%q,"endpoint":"campaign@example.com"}`, campaign.ID)))
	if err := store.ProjectEvent(ctx, isolationEvent); err != nil {
		t.Fatal(err)
	}
	var isolatedCount int
	if err := store.pool.QueryRow(ctx, `SELECT count(*) FROM engagement_facts WHERE source_event_id=$1`,
		isolationEvent.ID).Scan(&isolatedCount); err != nil {
		t.Fatal(err)
	}
	if isolatedCount != 0 {
		t.Fatalf("cross-workspace event created %d facts, want 0", isolatedCount)
	}
}

func engagementTestEvent(t *testing.T, store *Store, p domain.Principal, eventType string, payload json.RawMessage) domain.AcceptedEvent {
	t.Helper()
	var id string
	occurredAt := time.Now().UTC()
	if err := store.pool.QueryRow(context.Background(), `INSERT INTO accepted_events
		(tenant_id, workspace_id, app_id, event_type, schema_version, external_id,
		 idempotency_key, occurred_at, payload)
		VALUES ($1,$2,$3,$4,1,'engaged-profile',gen_random_uuid()::text,$5,$6)
		RETURNING id::text`, p.TenantID, p.WorkspaceID, p.AppID, eventType, occurredAt,
		payload).Scan(&id); err != nil {
		t.Fatal(err)
	}
	return domain.AcceptedEvent{
		ID: id, Principal: p, Type: eventType, ExternalID: "engaged-profile",
		OccurredAt: occurredAt, Payload: payload,
	}
}
