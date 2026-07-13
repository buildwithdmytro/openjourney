package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"os"
	"testing"
	"time"

	"github.com/buildwithdmytro/openjourney/internal/domain"
)

func TestCampaignAndJourneyReportsExactCountsAndWorkspaceIsolation(t *testing.T) {
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
	for _, index := range []string{"delivery_attempts_report_idx", "journey_message_intents_report_idx"} {
		var exists bool
		if err := store.pool.QueryRow(ctx, `SELECT to_regclass('public.' || $1) IS NOT NULL`, index).Scan(&exists); err != nil {
			t.Fatal(err)
		}
		if !exists {
			t.Fatalf("report index %s does not exist", index)
		}
	}

	key := fmt.Sprintf("analytics-report-%d", time.Now().UnixNano())
	if err := store.EnsureDevelopmentTenant(ctx, key); err != nil {
		t.Fatal(err)
	}
	p, err := store.Authenticate(ctx, key)
	if err != nil {
		t.Fatal(err)
	}
	pOther := domain.Principal{TenantID: p.TenantID}
	if err := store.pool.QueryRow(ctx, `INSERT INTO workspaces (tenant_id,name)
		VALUES ($1,'Report isolation') RETURNING id`, p.TenantID).Scan(&pOther.WorkspaceID); err != nil {
		t.Fatal(err)
	}
	if err := store.pool.QueryRow(ctx, `INSERT INTO applications (tenant_id,workspace_id,name)
		VALUES ($1,$2,'Report isolation') RETURNING id`, p.TenantID, pOther.WorkspaceID).Scan(&pOther.AppID); err != nil {
		t.Fatal(err)
	}

	profileIDs := make([]string, 7)
	for i := range profileIDs {
		if err := store.pool.QueryRow(ctx, `INSERT INTO profiles
			(tenant_id,workspace_id,app_id,external_id) VALUES ($1,$2,$3,$4) RETURNING id`,
			p.TenantID, p.WorkspaceID, p.AppID, fmt.Sprintf("report-profile-%d", i)).Scan(&profileIDs[i]); err != nil {
			t.Fatal(err)
		}
	}
	var otherProfileID string
	if err := store.pool.QueryRow(ctx, `INSERT INTO profiles
		(tenant_id,workspace_id,app_id,external_id) VALUES ($1,$2,$3,'report-other-profile') RETURNING id`,
		p.TenantID, pOther.WorkspaceID, pOther.AppID).Scan(&otherProfileID); err != nil {
		t.Fatal(err)
	}

	var segmentID, identityID, templateID string
	if err := store.pool.QueryRow(ctx, `INSERT INTO segments (tenant_id,workspace_id,name)
		VALUES ($1,$2,'Report segment') RETURNING id`, p.TenantID, p.WorkspaceID).Scan(&segmentID); err != nil {
		t.Fatal(err)
	}
	if err := store.pool.QueryRow(ctx, `INSERT INTO sending_identities
		(tenant_id,workspace_id,channel,from_address,provider) VALUES ($1,$2,'email',$3,'ses') RETURNING id`,
		p.TenantID, p.WorkspaceID, fmt.Sprintf("report-%d@example.com", time.Now().UnixNano())).Scan(&identityID); err != nil {
		t.Fatal(err)
	}
	if err := store.pool.QueryRow(ctx, `INSERT INTO templates
		(tenant_id,workspace_id,name,channel,html_template,sending_identity_id)
		VALUES ($1,$2,'Report template','email','hello',$3) RETURNING id`,
		p.TenantID, p.WorkspaceID, identityID).Scan(&templateID); err != nil {
		t.Fatal(err)
	}
	var campaignID string
	if err := store.pool.QueryRow(ctx, `INSERT INTO campaigns
		(tenant_id,workspace_id,name,segment_id,template_id) VALUES ($1,$2,'Report campaign',$3,$4) RETURNING id`,
		p.TenantID, p.WorkspaceID, segmentID, templateID).Scan(&campaignID); err != nil {
		t.Fatal(err)
	}

	seedCampaignAttempts(t, ctx, store, p, campaignID, profileIDs)
	seedReportFacts(t, ctx, store, p, "campaign", campaignID, profileIDs)
	seedCrossWorkspaceFacts(t, ctx, store, pOther, "campaign", campaignID, otherProfileID)

	campaign, err := store.CampaignReport(ctx, p, campaignID)
	if err != nil {
		t.Fatal(err)
	}
	assertReportCounts(t, campaign.Funnel, campaign.Deliverability)
	if campaign.CampaignID != campaignID {
		t.Errorf("campaign id = %q, want %q", campaign.CampaignID, campaignID)
	}
	if _, err := store.CampaignReport(ctx, pOther, campaignID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("cross-workspace campaign report error = %v, want ErrNotFound", err)
	}

	journeyID, versionID := seedJourneySource(t, ctx, store, p)
	seedJourneyIntents(t, ctx, store, p, journeyID, versionID, templateID, profileIDs)
	seedCrossWorkspaceJourneyIntent(t, ctx, store, pOther, journeyID, versionID, templateID, otherProfileID)
	seedReportFacts(t, ctx, store, p, "journey", journeyID, profileIDs)
	seedCrossWorkspaceFacts(t, ctx, store, pOther, "journey", journeyID, otherProfileID)

	journey, err := store.JourneyReport(ctx, p, journeyID)
	if err != nil {
		t.Fatal(err)
	}
	assertReportCounts(t, journey.Funnel, journey.Deliverability)
	if journey.JourneyID != journeyID {
		t.Errorf("journey id = %q, want %q", journey.JourneyID, journeyID)
	}
	if _, err := store.JourneyReport(ctx, pOther, journeyID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("cross-workspace journey report error = %v, want ErrNotFound", err)
	}
}

func seedCampaignAttempts(t *testing.T, ctx context.Context, store *Store, p domain.Principal, sourceID string, profiles []string) {
	t.Helper()
	rows := []struct{ profile, channel, decision string }{
		{profiles[0], "email", "sent"}, {profiles[0], "webhook", "sent"}, {profiles[1], "email", "sent"},
		{profiles[2], "email", "suppressed"}, {profiles[2], "webhook", "suppressed"},
		{profiles[3], "email", "no_consent"}, {profiles[4], "email", "fatigued"},
		{profiles[5], "email", "holdout"}, {profiles[6], "email", "failed"},
	}
	for _, row := range rows {
		if _, err := store.pool.Exec(ctx, `INSERT INTO delivery_attempts
			(campaign_id,tenant_id,profile_id,channel,endpoint,decision)
			VALUES ($1,$2,$3,$4,'report@example.com',$5)`, sourceID, p.TenantID, row.profile, row.channel, row.decision); err != nil {
			t.Fatal(err)
		}
	}
}

func seedJourneySource(t *testing.T, ctx context.Context, store *Store, p domain.Principal) (string, string) {
	t.Helper()
	var journeyID, versionID string
	if err := store.pool.QueryRow(ctx, `INSERT INTO journeys
		(tenant_id,workspace_id,name,status,graph) VALUES ($1,$2,'Report journey','published','{}') RETURNING id`,
		p.TenantID, p.WorkspaceID).Scan(&journeyID); err != nil {
		t.Fatal(err)
	}
	if err := store.pool.QueryRow(ctx, `INSERT INTO journey_versions
		(journey_id,tenant_id,workspace_id,version,graph,entry_kind,status)
		VALUES ($1,$2,$3,1,'{}','event','active') RETURNING id`, journeyID, p.TenantID, p.WorkspaceID).Scan(&versionID); err != nil {
		t.Fatal(err)
	}
	return journeyID, versionID
}

func seedJourneyIntents(t *testing.T, ctx context.Context, store *Store, p domain.Principal, journeyID, versionID, templateID string, profiles []string) {
	t.Helper()
	rows := []struct{ profile, node, decision string }{
		{profiles[0], "sent-1", "sent"}, {profiles[0], "sent-2", "sent"}, {profiles[1], "sent-1", "sent"},
		{profiles[2], "suppressed-1", "suppressed"}, {profiles[2], "suppressed-2", "suppressed"},
		{profiles[3], "no-consent", "no_consent"}, {profiles[4], "fatigued", "fatigued"},
		{profiles[5], "holdout", "holdout"}, {profiles[6], "failed", "failed"},
	}
	for i, row := range rows {
		var runID string
		if err := store.pool.QueryRow(ctx, `INSERT INTO journey_runs
			(tenant_id,workspace_id,journey_id,journey_version_id,profile_id,entry_key,current_node_id)
			VALUES ($1,$2,$3,$4,$5,$6,$7) RETURNING id`, p.TenantID, p.WorkspaceID, journeyID, versionID,
			row.profile, fmt.Sprintf("report-entry-%d", i), row.node).Scan(&runID); err != nil {
			t.Fatal(err)
		}
		if _, err := store.pool.Exec(ctx, `INSERT INTO journey_message_intents
			(run_id,tenant_id,workspace_id,journey_id,journey_version_id,node_id,profile_id,template_id,channel,endpoint,status,decision)
			VALUES ($1,$2,$3,$4,$5,$6,$7,$8,'email','report@example.com','completed',$9)`, runID, p.TenantID,
			p.WorkspaceID, journeyID, versionID, row.node, row.profile, templateID, row.decision); err != nil {
			t.Fatal(err)
		}
	}
}

func seedReportFacts(t *testing.T, ctx context.Context, store *Store, p domain.Principal, sourceType, sourceID string, profiles []string) {
	t.Helper()
	engagement := []struct{ profile, eventType string }{
		{profiles[0], "delivered"}, {profiles[0], "delivered"}, {profiles[1], "delivered"},
		{profiles[0], "opened"}, {profiles[0], "opened"},
		{profiles[0], "clicked"}, {profiles[1], "clicked"},
		{profiles[0], "bounced"}, {profiles[0], "bounced"}, {profiles[1], "complained"},
	}
	for _, fact := range engagement {
		if _, err := store.pool.Exec(ctx, `INSERT INTO engagement_facts
			(tenant_id,workspace_id,source_type,source_id,profile_id,event_type,occurred_at,source_event_id)
			VALUES ($1,$2,$3,$4,$5,$6,now(),gen_random_uuid())`, p.TenantID, p.WorkspaceID, sourceType, sourceID,
			fact.profile, fact.eventType); err != nil {
			t.Fatal(err)
		}
	}
	for _, profileID := range []string{profiles[0], profiles[0], profiles[1]} {
		if _, err := store.pool.Exec(ctx, `INSERT INTO conversion_facts
			(tenant_id,workspace_id,source_type,source_id,profile_id,goal_name,occurred_at,attributed_send_at,source_event_id)
			VALUES ($1,$2,$3,$4,$5,'purchase',now(),now(),gen_random_uuid())`, p.TenantID, p.WorkspaceID, sourceType,
			sourceID, profileID); err != nil {
			t.Fatal(err)
		}
	}
}

func seedCrossWorkspaceFacts(t *testing.T, ctx context.Context, store *Store, p domain.Principal, sourceType, sourceID, profileID string) {
	t.Helper()
	if _, err := store.pool.Exec(ctx, `INSERT INTO engagement_facts
		(tenant_id,workspace_id,source_type,source_id,profile_id,event_type,occurred_at,source_event_id)
		VALUES ($1,$2,$3,$4,$5,'delivered',now(),gen_random_uuid())`, p.TenantID, p.WorkspaceID, sourceType, sourceID, profileID); err != nil {
		t.Fatal(err)
	}
	if _, err := store.pool.Exec(ctx, `INSERT INTO conversion_facts
		(tenant_id,workspace_id,source_type,source_id,profile_id,goal_name,occurred_at,attributed_send_at,source_event_id)
		VALUES ($1,$2,$3,$4,$5,'purchase',now(),now(),gen_random_uuid())`, p.TenantID, p.WorkspaceID, sourceType, sourceID, profileID); err != nil {
		t.Fatal(err)
	}
}

func seedCrossWorkspaceJourneyIntent(t *testing.T, ctx context.Context, store *Store, p domain.Principal, journeyID, versionID, templateID, profileID string) {
	t.Helper()
	var runID string
	if err := store.pool.QueryRow(ctx, `INSERT INTO journey_runs
		(tenant_id,workspace_id,journey_id,journey_version_id,profile_id,entry_key,current_node_id)
		VALUES ($1,$2,$3,$4,$5,'cross-workspace-report','message') RETURNING id`, p.TenantID, p.WorkspaceID, journeyID,
		versionID, profileID).Scan(&runID); err != nil {
		t.Fatal(err)
	}
	if _, err := store.pool.Exec(ctx, `INSERT INTO journey_message_intents
		(run_id,tenant_id,workspace_id,journey_id,journey_version_id,node_id,profile_id,template_id,channel,endpoint,status,decision)
		VALUES ($1,$2,$3,$4,$5,'message',$6,$7,'email','other@example.com','completed','sent')`, runID, p.TenantID,
		p.WorkspaceID, journeyID, versionID, profileID, templateID); err != nil {
		t.Fatal(err)
	}
}

func assertReportCounts(t *testing.T, funnel domain.ReportFunnel, deliverability domain.ReportDeliverability) {
	t.Helper()
	want := map[string]struct{ got, want domain.ReportCount }{
		"targeted":   {funnel.Targeted, domain.ReportCount{Total: 9, Unique: 7}},
		"sent":       {funnel.Sent, domain.ReportCount{Total: 3, Unique: 2}},
		"suppressed": {funnel.Suppressed, domain.ReportCount{Total: 2, Unique: 1}},
		"no_consent": {funnel.NoConsent, domain.ReportCount{Total: 1, Unique: 1}},
		"fatigued":   {funnel.Fatigued, domain.ReportCount{Total: 1, Unique: 1}},
		"failed":     {funnel.Failed, domain.ReportCount{Total: 1, Unique: 1}},
		"holdout":    {funnel.Holdout, domain.ReportCount{Total: 1, Unique: 1}},
		"delivered":  {funnel.Delivered, domain.ReportCount{Total: 3, Unique: 2}},
		"opened":     {funnel.Opened, domain.ReportCount{Total: 2, Unique: 1}},
		"clicked":    {funnel.Clicked, domain.ReportCount{Total: 2, Unique: 2}},
		"converted":  {funnel.Converted, domain.ReportCount{Total: 3, Unique: 2}},
		"bounced":    {deliverability.Bounced, domain.ReportCount{Total: 2, Unique: 1}},
		"complained": {deliverability.Complained, domain.ReportCount{Total: 1, Unique: 1}},
	}
	for name, test := range want {
		if test.got != test.want {
			t.Errorf("%s = %+v, want %+v", name, test.got, test.want)
		}
	}
	if deliverability.BounceRate != 2.0/3.0 {
		t.Errorf("bounce rate = %v, want %v", deliverability.BounceRate, 2.0/3.0)
	}
	if deliverability.ComplaintRate != 1.0/3.0 {
		t.Errorf("complaint rate = %v, want %v", deliverability.ComplaintRate, 1.0/3.0)
	}
}

func TestExperimentReport(t *testing.T) {
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

	key := fmt.Sprintf("experiment-report-%d", time.Now().UnixNano())
	if err := store.EnsureDevelopmentTenant(ctx, key); err != nil {
		t.Fatal(err)
	}
	p, err := store.Authenticate(ctx, key)
	if err != nil {
		t.Fatal(err)
	}
	if p.AppID == "" {
		p.AppID, err = store.GetFirstAppID(ctx, p.TenantID, p.WorkspaceID)
		if err != nil {
			t.Fatal(err)
		}
	}
	pOther := domain.Principal{TenantID: p.TenantID}
	if err := store.pool.QueryRow(ctx, `INSERT INTO workspaces (tenant_id,name)
		VALUES ($1,'Experiment isolation') RETURNING id`, p.TenantID).Scan(&pOther.WorkspaceID); err != nil {
		t.Fatal(err)
	}

	// Create dependencies for the campaign
	var segmentID, identityID, templateID string
	if err := store.pool.QueryRow(ctx, `INSERT INTO segments (tenant_id,workspace_id,name)
		VALUES ($1,$2,'Experiment report segment') RETURNING id`, p.TenantID, p.WorkspaceID).Scan(&segmentID); err != nil {
		t.Fatal(err)
	}
	if err := store.pool.QueryRow(ctx, `INSERT INTO sending_identities
		(tenant_id,workspace_id,channel,from_address,provider) VALUES ($1,$2,'email',$3,'ses') RETURNING id`,
		p.TenantID, p.WorkspaceID, fmt.Sprintf("experiment-report-%d@example.com", time.Now().UnixNano())).Scan(&identityID); err != nil {
		t.Fatal(err)
	}
	if err := store.pool.QueryRow(ctx, `INSERT INTO templates
		(tenant_id,workspace_id,name,channel,html_template,sending_identity_id)
		VALUES ($1,$2,'Experiment report template','email','hello',$3) RETURNING id`,
		p.TenantID, p.WorkspaceID, identityID).Scan(&templateID); err != nil {
		t.Fatal(err)
	}
	var campaignID string
	if err := store.pool.QueryRow(ctx, `INSERT INTO campaigns
		(tenant_id,workspace_id,name,segment_id,template_id) VALUES ($1,$2,'Experiment campaign',$3,$4) RETURNING id`,
		p.TenantID, p.WorkspaceID, segmentID, templateID).Scan(&campaignID); err != nil {
		t.Fatal(err)
	}

	// Create experiment
	exp, err := store.CreateExperiment(ctx, p, domain.Experiment{
		Name:        "Test Stats Experiment",
		SubjectType: "campaign",
		Seed:        "some-seed",
		PrimaryGoal: json.RawMessage(`{"event_type": "signup", "name": "signup"}`),
		GuardrailGoals: json.RawMessage(`[{"event_type": "churn", "name": "churn"}]`),
		Variants: []domain.ExperimentVariant{
			{Label: "control", Weight: 50, IsControl: true},
			{Label: "treatment", Weight: 50, IsControl: false},
		},
	})
	if err != nil {
		t.Fatal(err)
	}

	// Seed sends (delivery_attempts)
	// Control: 100 sends
	for i := 0; i < 100; i++ {
		profileID := fmt.Sprintf("00000000-0000-0000-0000-%012d", i)
		// Insert profile first (due to foreign key constraint)
		_, err = store.pool.Exec(ctx, `INSERT INTO profiles (id, tenant_id, workspace_id, app_id, external_id)
			VALUES ($1, $2, $3, $4, $5) ON CONFLICT DO NOTHING`, profileID, p.TenantID, p.WorkspaceID, p.AppID, fmt.Sprintf("p-c-%d", i))
		if err != nil {
			t.Fatal(err)
		}

		_, err = store.pool.Exec(ctx, `INSERT INTO delivery_attempts
			(campaign_id, tenant_id, profile_id, channel, endpoint, decision, experiment_id, variant)
			VALUES ($1, $2, $3, 'email', 'c@example.com', 'sent', $4, 'control')`,
			campaignID, p.TenantID, profileID, exp.ID)
		if err != nil {
			t.Fatal(err)
		}
	}

	// Treatment: 100 sends
	for i := 0; i < 100; i++ {
		profileID := fmt.Sprintf("00000000-0000-0000-0000-%012d", i+100)
		_, err = store.pool.Exec(ctx, `INSERT INTO profiles (id, tenant_id, workspace_id, app_id, external_id)
			VALUES ($1, $2, $3, $4, $5) ON CONFLICT DO NOTHING`, profileID, p.TenantID, p.WorkspaceID, p.AppID, fmt.Sprintf("p-t-%d", i))
		if err != nil {
			t.Fatal(err)
		}

		_, err = store.pool.Exec(ctx, `INSERT INTO delivery_attempts
			(campaign_id, tenant_id, profile_id, channel, endpoint, decision, experiment_id, variant)
			VALUES ($1, $2, $3, 'email', 't@example.com', 'sent', $4, 'treatment')`,
			campaignID, p.TenantID, profileID, exp.ID)
		if err != nil {
			t.Fatal(err)
		}
	}

	// Seed primary conversions: 10 in control, 20 in treatment
	for i := 0; i < 10; i++ {
		profileID := fmt.Sprintf("00000000-0000-0000-0000-%012d", i)
		_, err = store.pool.Exec(ctx, `INSERT INTO conversion_facts
			(tenant_id, workspace_id, source_type, source_id, experiment_id, variant, profile_id, goal_name, occurred_at, attributed_send_at, source_event_id)
			VALUES ($1, $2, 'campaign', $3, $4, 'control', $5, 'signup', now(), now(), gen_random_uuid())`,
			p.TenantID, p.WorkspaceID, campaignID, exp.ID, profileID)
		if err != nil {
			t.Fatal(err)
		}
	}

	for i := 0; i < 20; i++ {
		profileID := fmt.Sprintf("00000000-0000-0000-0000-%012d", i+100)
		_, err = store.pool.Exec(ctx, `INSERT INTO conversion_facts
			(tenant_id, workspace_id, source_type, source_id, experiment_id, variant, profile_id, goal_name, occurred_at, attributed_send_at, source_event_id)
			VALUES ($1, $2, 'campaign', $3, $4, 'treatment', $5, 'signup', now(), now(), gen_random_uuid())`,
			p.TenantID, p.WorkspaceID, campaignID, exp.ID, profileID)
		if err != nil {
			t.Fatal(err)
		}
	}

	// Seed guardrail conversions: 5 in control, 15 in treatment
	for i := 0; i < 5; i++ {
		profileID := fmt.Sprintf("00000000-0000-0000-0000-%012d", i)
		_, err = store.pool.Exec(ctx, `INSERT INTO conversion_facts
			(tenant_id, workspace_id, source_type, source_id, experiment_id, variant, profile_id, goal_name, occurred_at, attributed_send_at, source_event_id)
			VALUES ($1, $2, 'campaign', $3, $4, 'control', $5, 'churn', now(), now(), gen_random_uuid())`,
			p.TenantID, p.WorkspaceID, campaignID, exp.ID, profileID)
		if err != nil {
			t.Fatal(err)
		}
	}

	for i := 0; i < 15; i++ {
		profileID := fmt.Sprintf("00000000-0000-0000-0000-%012d", i+100)
		_, err = store.pool.Exec(ctx, `INSERT INTO conversion_facts
			(tenant_id, workspace_id, source_type, source_id, experiment_id, variant, profile_id, goal_name, occurred_at, attributed_send_at, source_event_id)
			VALUES ($1, $2, 'campaign', $3, $4, 'treatment', $5, 'churn', now(), now(), gen_random_uuid())`,
			p.TenantID, p.WorkspaceID, campaignID, exp.ID, profileID)
		if err != nil {
			t.Fatal(err)
		}
	}

	// Run report
	rpt, err := store.ExperimentReport(ctx, p, exp.ID)
	if err != nil {
		t.Fatal(err)
	}

	if rpt.ExperimentID != exp.ID {
		t.Errorf("expected experiment id %s, got %s", exp.ID, rpt.ExperimentID)
	}

	var controlReport, treatmentReport *domain.ExperimentVariantReport
	for i := range rpt.Variants {
		if rpt.Variants[i].Label == "control" {
			controlReport = &rpt.Variants[i]
		} else if rpt.Variants[i].Label == "treatment" {
			treatmentReport = &rpt.Variants[i]
		}
	}

	if controlReport == nil || treatmentReport == nil {
		t.Fatal("missing control or treatment reports")
	}

	// Assert control stats
	if controlReport.Sent != 100 {
		t.Errorf("control sent: got %d, want 100", controlReport.Sent)
	}
	if controlReport.Conversions != 10 {
		t.Errorf("control conversions: got %d, want 10", controlReport.Conversions)
	}
	if controlReport.Rate != 0.1 {
		t.Errorf("control rate: got %f, want 0.1", controlReport.Rate)
	}

	// Assert treatment stats (matching Example 1 z-test calculations)
	if treatmentReport.Sent != 100 {
		t.Errorf("treatment sent: got %d, want 100", treatmentReport.Sent)
	}
	if treatmentReport.Conversions != 20 {
		t.Errorf("treatment conversions: got %d, want 20", treatmentReport.Conversions)
	}
	if treatmentReport.Rate != 0.2 {
		t.Errorf("treatment rate: got %f, want 0.2", treatmentReport.Rate)
	}
	if treatmentReport.Uplift != 1.0 {
		t.Errorf("treatment uplift: got %f, want 1.0", treatmentReport.Uplift)
	}
	if math.Abs(treatmentReport.ZScore-1.980295) > 1e-5 {
		t.Errorf("treatment z-score: got %f, want ~1.980295", treatmentReport.ZScore)
	}
	if math.Abs(treatmentReport.PValue-0.047670) > 1e-5 {
		t.Errorf("treatment p-value: got %f, want ~0.047670", treatmentReport.PValue)
	}

	// Assert guardrail stats
	if len(controlReport.Guardrails) != 1 || controlReport.Guardrails[0].GoalName != "churn" || controlReport.Guardrails[0].Conversions != 5 || controlReport.Guardrails[0].Rate != 0.05 {
		t.Errorf("control guardrails: %+v", controlReport.Guardrails)
	}
	if len(treatmentReport.Guardrails) != 1 || treatmentReport.Guardrails[0].GoalName != "churn" || treatmentReport.Guardrails[0].Conversions != 15 || treatmentReport.Guardrails[0].Rate != 0.15 {
		t.Errorf("treatment guardrails: %+v", treatmentReport.Guardrails)
	}

	// Isolation check
	_, err = store.ExperimentReport(ctx, pOther, exp.ID)
	if !errors.Is(err, ErrNotFound) {
		t.Errorf("expected ErrNotFound for other workspace, got %v", err)
	}
}
