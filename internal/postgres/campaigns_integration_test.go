package postgres

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/buildwithdmytro/openjourney/internal/domain"
)

func TestCampaignsIntegration(t *testing.T) {
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

	tenantID := "tenant-camp-test-" + time.Now().Format("20060102-150405")
	p := domain.Principal{TenantID: tenantID, WorkspaceID: "workspace-1", AppID: "app-1"}

	_, err = store.pool.Exec(ctx, `INSERT INTO tenants(id, name) VALUES($1, 'Test Tenant')`, tenantID)
	if err != nil {
		t.Fatalf("insert tenant: %v", err)
	}
	_, err = store.pool.Exec(ctx, `INSERT INTO workspaces(id, tenant_id, name) VALUES($1, $2, 'Test Workspace')`, p.WorkspaceID, tenantID)
	if err != nil {
		t.Fatalf("insert workspace: %v", err)
	}

	// Create dependent segment
	seg, err := store.CreateSegment(ctx, p, domain.Segment{
		Name: "Test Segment",
		Type: "dynamic",
		DSL:  []byte(`{}`),
	})
	if err != nil {
		t.Fatalf("create segment: %v", err)
	}

	// Create dependent sending identity
	iden, err := store.CreateSendingIdentity(ctx, p, domain.SendingIdentity{
		Channel:     "email",
		FromAddress: ptr("sender@example.com"),
		FromName:    ptr("Sender"),
		Provider:    "ses",
		MaxSendRate: 10,
	})
	if err != nil {
		t.Fatalf("create sending identity: %v", err)
	}

	// Create dependent template
	htmlTmpl := "Hello {{ name }}!"
	tmpl, err := store.CreateTemplate(ctx, p, domain.Template{
		Name:              "Welcome Email",
		Channel:           "email",
		HTMLTemplate:      &htmlTmpl,
		SendingIdentityID: &iden.ID,
	})
	if err != nil {
		t.Fatalf("create template: %v", err)
	}

	// 1. Create Campaign
	camp, err := store.CreateCampaign(ctx, p, domain.Campaign{
		Name:            "Holiday Promo",
		SegmentID:       seg.ID,
		TemplateID:      tmpl.ID,
		Status:          "draft",
		SegmentVersion:  1,
		TemplateVersion: 1,
	})
	if err != nil {
		t.Fatalf("create campaign: %v", err)
	}

	if camp.Name != "Holiday Promo" {
		t.Errorf("expected Holiday Promo, got %s", camp.Name)
	}

	// 2. Get Campaign
	fetched, err := store.GetCampaign(ctx, p, camp.ID)
	if err != nil {
		t.Fatalf("get campaign: %v", err)
	}
	if fetched.ID != camp.ID {
		t.Errorf("expected ID %s, got %s", camp.ID, fetched.ID)
	}

	// 3. Update Campaign (schedule it)
	schedTime := time.Now().Add(10 * time.Minute)
	fetched.Status = "scheduled"
	fetched.ScheduledAt = &schedTime
	updated, err := store.UpdateCampaign(ctx, p, fetched)
	if err != nil {
		t.Fatalf("update campaign: %v", err)
	}
	if updated.Status != "scheduled" {
		t.Errorf("expected scheduled status, got %s", updated.Status)
	}

	// 4. List Campaigns
	list, err := store.ListCampaigns(ctx, p)
	if err != nil {
		t.Fatalf("list campaigns: %v", err)
	}
	if len(list) != 1 {
		t.Errorf("expected 1 campaign, got %d", len(list))
	}

	// 5. Claim Scheduled Campaign
	// It's scheduled for 10 minutes in the future, so claiming should NOT find it.
	_, found, err := store.ClaimScheduledCampaign(ctx)
	if err != nil {
		t.Fatalf("claim scheduled campaign 1: %v", err)
	}
	if found {
		t.Error("expected scheduled campaign NOT to be claimed because scheduled_at is in the future")
	}

	// Update scheduled_at to be in the past
	pastTime := time.Now().Add(-1 * time.Minute)
	updated.ScheduledAt = &pastTime
	_, err = store.UpdateCampaign(ctx, p, updated)
	if err != nil {
		t.Fatalf("update campaign to past: %v", err)
	}

	// Now claiming should succeed!
	claimedCamp, found, err := store.ClaimScheduledCampaign(ctx)
	if err != nil {
		t.Fatalf("claim scheduled campaign 2: %v", err)
	}
	if !found {
		t.Fatal("expected to claim the scheduled campaign")
	}
	if claimedCamp.Status != "building" {
		t.Errorf("expected status 'building', got %s", claimedCamp.Status)
	}

	// 6. Save Campaign Manifest & Jobs
	jobs := []domain.DeliveryJob{
		{
			TenantID: tenantID,
			Shard:    0,
			Recipients: []domain.Recipient{
				{ProfileID: "550e8400-e29b-41d4-a716-446655440000", Endpoint: "user1@example.com"},
				{ProfileID: "550e8400-e29b-41d4-a716-446655440001", Endpoint: "user2@example.com"},
			},
		},
	}
	err = store.SaveCampaignManifestAndJobs(ctx, claimedCamp.ID, "manifests/holiday-promo.json", 2, jobs)
	if err != nil {
		t.Fatalf("save manifest and jobs: %v", err)
	}

	// Verify campaign status is now 'sending'
	sendingCamp, err := store.GetCampaign(ctx, p, claimedCamp.ID)
	if err != nil {
		t.Fatalf("get sending campaign: %v", err)
	}
	if sendingCamp.Status != "sending" {
		t.Errorf("expected status 'sending', got %s", sendingCamp.Status)
	}

	// 7. Claim Delivery Job
	claimedJob, foundJob, err := store.ClaimDeliveryJob(ctx, "worker-node-1")
	if err != nil {
		t.Fatalf("claim delivery job: %v", err)
	}
	if !foundJob {
		t.Fatal("expected to find a pending delivery job")
	}
	if claimedJob.Status != "processing" {
		t.Errorf("expected status 'processing', got %s", claimedJob.Status)
	}
	if len(claimedJob.Recipients) != 2 {
		t.Errorf("expected 2 recipients, got %d", len(claimedJob.Recipients))
	}

	// 8. Create Delivery Attempt
	attempt := domain.DeliveryAttempt{
		CampaignID: claimedCamp.ID,
		ProfileID:  "550e8400-e29b-41d4-a716-446655440000",
		Channel:    "email",
		Endpoint:   "user1@example.com",
		Decision:   "failed", // temporary initial status
	}
	inserted, err := store.CreateDeliveryAttempt(ctx, attempt)
	if err != nil {
		t.Fatalf("create delivery attempt: %v", err)
	}
	if !inserted {
		t.Error("expected first insert of delivery attempt to succeed")
	}

	// Create same attempt again, ON CONFLICT DO NOTHING should kick in and return false (skipped)
	insertedAgain, err := store.CreateDeliveryAttempt(ctx, attempt)
	if err != nil {
		t.Fatalf("create duplicate delivery attempt: %v", err)
	}
	if insertedAgain {
		t.Error("expected duplicate delivery attempt to be skipped")
	}

	// Update delivery attempt
	err = store.UpdateDeliveryAttempt(ctx, claimedCamp.ID, "550e8400-e29b-41d4-a716-446655440000", "email", "sent", "", "msg-12345")
	if err != nil {
		t.Fatalf("update delivery attempt: %v", err)
	}

	// 9. Complete Delivery Job
	// There is only 1 job, so completing it should also move campaign to 'completed'!
	err = store.CompleteDeliveryJob(ctx, claimedJob.ID)
	if err != nil {
		t.Fatalf("complete delivery job: %v", err)
	}

	completedCamp, err := store.GetCampaign(ctx, p, claimedCamp.ID)
	if err != nil {
		t.Fatalf("get completed campaign: %v", err)
	}
	if completedCamp.Status != "completed" {
		t.Errorf("expected campaign status 'completed', got %s", completedCamp.Status)
	}

	// Clean up
	_, _ = store.pool.Exec(ctx, "DELETE FROM tenants WHERE id=$1", tenantID)
}
