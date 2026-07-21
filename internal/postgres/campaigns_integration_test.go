package postgres

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/buildwithdmytro/openjourney/internal/campaigns"
	"github.com/buildwithdmytro/openjourney/internal/channels"
	"github.com/buildwithdmytro/openjourney/internal/domain"
	"github.com/buildwithdmytro/openjourney/internal/projector"
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

	p, tenantID := setupTestTenant(t, ctx, store)

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
	err = store.SaveCampaignManifestAndJobs(ctx, claimedCamp.ID, "manifests/holiday-promo.json", 2, 1, 1, nil, "", jobs)
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
		TenantID:   tenantID,
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
	err = store.UpdateDeliveryAttempt(ctx, claimedCamp.ID, "550e8400-e29b-41d4-a716-446655440000", "email", "user1@example.com", "sent", "", "msg-12345", nil, 0)
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

type mockBlobStore struct {
	data map[string][]byte
}

func (m *mockBlobStore) Put(ctx context.Context, key string, data []byte, contentType string) error {
	if m.data == nil {
		m.data = make(map[string][]byte)
	}
	m.data[key] = data
	return nil
}

func (m *mockBlobStore) Get(ctx context.Context, key string) ([]byte, error) {
	if m.data == nil {
		return nil, fmt.Errorf("not found")
	}
	val, ok := m.data[key]
	if !ok {
		return nil, fmt.Errorf("not found")
	}
	return val, nil
}

func TestCampaignsEndToEnd(t *testing.T) {
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

	p, tenantID := setupTestTenant(t, ctx, store)
	p.Scopes = []string{"*"}
	defer func() {
		_, _ = store.pool.Exec(ctx, "DELETE FROM tenants WHERE id=$1", tenantID)
	}()

	// 1. Create a profile and accept event to project attributes & consent
	events := []domain.Event{
		{
			Type: "profile.updated", SchemaVersion: 1, ExternalID: "cust-e2e-1",
			IdempotencyKey: "identify-e2e-1", OccurredAt: time.Now().UTC(),
			Payload: json.RawMessage(`{"attributes":{"country":"US", "email":"e2e-rec@example.com"}}`),
		},
		{
			Type: "consent.changed", SchemaVersion: 1, ExternalID: "cust-e2e-1",
			IdempotencyKey: "consent-e2e-1", OccurredAt: time.Now().UTC(),
			Payload: json.RawMessage(`{"channel":"email","topic":"marketing","state":"subscribed"}`),
		},
	}
	_, err = store.AcceptEvents(ctx, p, events)
	if err != nil {
		t.Fatalf("accept events: %v", err)
	}

	// Project the events
	_, err = projector.Drain(ctx, store, 2, false)
	if err != nil {
		t.Fatalf("projector drain: %v", err)
	}

	// Get profile to verify creation and find the profile ID
	prof, _, err := store.GetProfile(ctx, p, "cust-e2e-1")
	if err != nil {
		t.Fatalf("get profile: %v", err)
	}

	// 2. Create dynamic segment selector
	seg, err := store.CreateSegment(ctx, p, domain.Segment{
		Name: "E2E Dynamic Segment",
		Type: "dynamic",
		DSL:  []byte(`{"type":"profile_attribute","field":"country","operator":"equals","value":"US"}`),
	})
	if err != nil {
		t.Fatalf("create segment: %v", err)
	}

	// 3. Create dependent sending identity and template
	iden, err := store.CreateSendingIdentity(ctx, p, domain.SendingIdentity{
		Channel:     "email",
		FromAddress: ptr("sender@example.com"),
		FromName:    ptr("Sender"),
		Provider:    "fake",
		MaxSendRate: 10,
	})
	if err != nil {
		t.Fatalf("create sending identity: %v", err)
	}

	htmlTmpl := "Hello E2E!"
	tmpl, err := store.CreateTemplate(ctx, p, domain.Template{
		Name:              "E2E Template",
		Channel:           "email",
		HTMLTemplate:      &htmlTmpl,
		SendingIdentityID: &iden.ID,
	})
	if err != nil {
		t.Fatalf("create template: %v", err)
	}

	// 4. Create Campaign and schedule it
	camp, err := store.CreateCampaign(ctx, p, domain.Campaign{
		Name:            "E2E Campaign",
		SegmentID:       seg.ID,
		TemplateID:      tmpl.ID,
		Status:          "draft",
		SegmentVersion:  1,
		TemplateVersion: 1,
	})
	if err != nil {
		t.Fatalf("create campaign: %v", err)
	}

	pastTime := time.Now().Add(-1 * time.Minute)
	camp.Status = "scheduled"
	camp.ScheduledAt = &pastTime
	camp, err = store.UpdateCampaign(ctx, p, camp)
	if err != nil {
		t.Fatalf("update campaign: %v", err)
	}

	// 5. Run campaigns.DispatchNext (e2e dispatching)
	blob := &mockBlobStore{}
	dispatched, err := campaigns.DispatchNext(ctx, store, blob)
	if err != nil {
		t.Fatalf("DispatchNext: %v", err)
	}
	if !dispatched {
		t.Fatal("expected to dispatch a campaign")
	}

	// Assert manifest was written to the blob store
	campFetched, err := store.GetCampaign(ctx, p, camp.ID)
	if err != nil {
		t.Fatalf("get campaign after dispatch: %v", err)
	}
	if campFetched.Status != "sending" {
		t.Errorf("expected campaign status to be 'sending', got %s", campFetched.Status)
	}
	if campFetched.ManifestKey == nil || *campFetched.ManifestKey == "" {
		t.Fatal("expected manifest key to be set on campaign")
	}
	manifestData, err := blob.Get(ctx, *campFetched.ManifestKey)
	if err != nil {
		t.Fatalf("get manifest data from blob: %v", err)
	}
	if len(manifestData) == 0 {
		t.Error("expected manifest data to be non-empty")
	}

	// 6. Run campaigns.DeliverNext (e2e delivery)
	fakeAdapter := channels.NewFakeAdapter()
	cfg := campaigns.Config{
		TrackingSecretKey: []byte("tracking-secret-key-12345"),
		TrackingBaseURL:   "http://localhost:8080",
		FakeAdapter:       fakeAdapter,
	}

	delivered, err := campaigns.DeliverNext(ctx, store, "worker-1", cfg)
	if err != nil {
		t.Fatalf("DeliverNext: %v", err)
	}
	if !delivered {
		t.Fatal("expected to deliver a job")
	}

	// Assert campaign is completed
	campCompleted, err := store.GetCampaign(ctx, p, camp.ID)
	if err != nil {
		t.Fatalf("get campaign after delivery: %v", err)
	}
	if campCompleted.Status != "completed" {
		t.Errorf("expected campaign status to be 'completed', got %s", campCompleted.Status)
	}

	// Assert delivery attempt row is present with decision 'sent'
	rows, err := store.pool.Query(ctx, `SELECT decision, reason FROM delivery_attempts WHERE campaign_id = $1 AND profile_id = $2`, camp.ID, prof.ID)
	if err != nil {
		t.Fatalf("query delivery attempts: %v", err)
	}
	defer rows.Close()
	if !rows.Next() {
		t.Fatal("expected a delivery attempt row to be inserted")
	}
	var decision, reason string
	if err := rows.Scan(&decision, &reason); err != nil {
		t.Fatalf("scan delivery attempt: %v", err)
	}
	if decision != "sent" {
		t.Errorf("expected decision 'sent', got %s", decision)
	}
	if reason != "eligible" {
		t.Errorf("expected reason 'eligible', got %s", reason)
	}

	// Assert message.sent event is emitted
	var eventType string
	err = store.pool.QueryRow(ctx, `SELECT event_type FROM accepted_events WHERE tenant_id = $1 AND external_id = $2 AND event_type = 'message.sent'`, tenantID, prof.ExternalID).Scan(&eventType)
	if err != nil {
		t.Fatalf("query events: %v", err)
	}
	if eventType != "message.sent" {
		t.Errorf("expected event type 'message.sent', got %q", eventType)
	}
}

func TestCampaignsReproducibility(t *testing.T) {
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

	p, tenantID := setupTestTenant(t, ctx, store)
	p.Scopes = []string{"*"}
	defer func() {
		_, _ = store.pool.Exec(ctx, "DELETE FROM tenants WHERE id=$1", tenantID)
	}()

	// Create 3 profiles (p1, p2, p3) in segment
	for _, extID := range []string{"p1", "p2", "p3"} {
		events := []domain.Event{
			{
				Type: "profile.updated", SchemaVersion: 1, ExternalID: extID,
				IdempotencyKey: "identify-rep-" + extID, OccurredAt: time.Now().UTC(),
				Payload: json.RawMessage(`{"attributes":{"country":"US", "email":"` + extID + `@example.com"}}`),
			},
			{
				Type: "consent.changed", SchemaVersion: 1, ExternalID: extID,
				IdempotencyKey: "consent-rep-" + extID, OccurredAt: time.Now().UTC(),
				Payload: json.RawMessage(`{"channel":"email","topic":"marketing","state":"subscribed"}`),
			},
		}
		_, err = store.AcceptEvents(ctx, p, events)
		if err != nil {
			t.Fatalf("accept events for %s: %v", extID, err)
		}
	}

	_, err = projector.Drain(ctx, store, 6, false)
	if err != nil {
		t.Fatalf("projector drain: %v", err)
	}

	seg, err := store.CreateSegment(ctx, p, domain.Segment{
		Name: "Rep Dynamic Segment",
		Type: "dynamic",
		DSL:  []byte(`{"type":"profile_attribute","field":"country","operator":"equals","value":"US"}`),
	})
	if err != nil {
		t.Fatalf("create segment: %v", err)
	}

	iden, err := store.CreateSendingIdentity(ctx, p, domain.SendingIdentity{
		Channel:     "email",
		FromAddress: ptr("sender@example.com"),
		FromName:    ptr("Sender"),
		Provider:    "fake",
		MaxSendRate: 10,
	})
	if err != nil {
		t.Fatalf("create sending identity: %v", err)
	}

	htmlTmpl := "Hello Rep!"
	tmpl, err := store.CreateTemplate(ctx, p, domain.Template{
		Name:              "Rep Template",
		Channel:           "email",
		HTMLTemplate:      &htmlTmpl,
		SendingIdentityID: &iden.ID,
	})
	if err != nil {
		t.Fatalf("create template: %v", err)
	}

	camp, err := store.CreateCampaign(ctx, p, domain.Campaign{
		Name:            "Rep Campaign",
		SegmentID:       seg.ID,
		TemplateID:      tmpl.ID,
		Status:          "draft",
		SegmentVersion:  1,
		TemplateVersion: 1,
	})
	if err != nil {
		t.Fatalf("create campaign: %v", err)
	}

	pastTime := time.Now().Add(-1 * time.Minute)
	camp.Status = "scheduled"
	camp.ScheduledAt = &pastTime
	camp, err = store.UpdateCampaign(ctx, p, camp)
	if err != nil {
		t.Fatalf("update campaign: %v", err)
	}

	blob := &mockBlobStore{}
	dispatched, err := campaigns.DispatchNext(ctx, store, blob)
	if err != nil {
		t.Fatalf("DispatchNext: %v", err)
	}
	if !dispatched {
		t.Fatal("expected to dispatch a campaign")
	}

	// 1. Verify original dispatch recipient list is stored in the jobs
	var countBefore int
	err = store.pool.QueryRow(ctx, "SELECT count(*) FROM delivery_jobs WHERE campaign_id = $1", camp.ID).Scan(&countBefore)
	if err != nil {
		t.Fatalf("query jobs before: %v", err)
	}
	if countBefore != 1 {
		t.Errorf("expected 1 delivery job, got %d", countBefore)
	}

	// 2. Change the database state: delete the profiles and update dynamic segment so they no longer match
	_, err = store.pool.Exec(ctx, "DELETE FROM consent_ledger WHERE tenant_id = $1", tenantID)
	if err != nil {
		t.Fatalf("delete consent: %v", err)
	}
	_, err = store.pool.Exec(ctx, "DELETE FROM profiles WHERE tenant_id = $1", tenantID)
	if err != nil {
		t.Fatalf("delete profiles: %v", err)
	}

	// 3. Clear existing delivery jobs to simulate redispatching
	_, err = store.pool.Exec(ctx, "DELETE FROM delivery_jobs WHERE campaign_id = $1", camp.ID)
	if err != nil {
		t.Fatalf("clear jobs: %v", err)
	}

	// 4. Redispatch from the stored manifest
	recipients, err := campaigns.RedispatchFromManifest(ctx, store, blob, tenantID, camp.ID)
	if err != nil {
		t.Fatalf("RedispatchFromManifest: %v", err)
	}

	if len(recipients) != 3 {
		t.Errorf("expected 3 recipients from manifest, got %d", len(recipients))
	}

	// 5. Verify the sharded delivery jobs were recreated and are identical
	var countAfter int
	err = store.pool.QueryRow(ctx, "SELECT count(*) FROM delivery_jobs WHERE campaign_id = $1", camp.ID).Scan(&countAfter)
	if err != nil {
		t.Fatalf("query jobs after: %v", err)
	}
	if countAfter != 1 {
		t.Errorf("expected 1 delivery job recreated, got %d", countAfter)
	}
}

func TestCampaignsExplainability(t *testing.T) {
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

	p, tenantID := setupTestTenant(t, ctx, store)
	p.Scopes = []string{"*"}
	defer func() {
		_, _ = store.pool.Exec(ctx, "DELETE FROM tenants WHERE id=$1", tenantID)
	}()

	// 1. Create two profiles (p1, p2)
	for _, extID := range []string{"p1", "p2"} {
		events := []domain.Event{
			{
				Type: "profile.updated", SchemaVersion: 1, ExternalID: extID,
				IdempotencyKey: "identify-exp-" + extID, OccurredAt: time.Now().UTC(),
				Payload: json.RawMessage(`{"attributes":{"country":"US", "email":"` + extID + `@example.com"}}`),
			},
			{
				Type: "consent.changed", SchemaVersion: 1, ExternalID: extID,
				IdempotencyKey: "consent-exp-" + extID, OccurredAt: time.Now().UTC(),
				Payload: json.RawMessage(`{"channel":"email","topic":"marketing","state":"subscribed"}`),
			},
		}
		_, err = store.AcceptEvents(ctx, p, events)
		if err != nil {
			t.Fatalf("accept events for %s: %v", extID, err)
		}
	}

	_, err = projector.Drain(ctx, store, 4, false)
	if err != nil {
		t.Fatalf("projector drain: %v", err)
	}

	// Profile 2 is suppressed
	err = store.SuppressEndpoint(ctx, p, "email", "p2@example.com", "unsubscribe")
	if err != nil {
		t.Fatalf("suppress endpoint: %v", err)
	}

	seg, err := store.CreateSegment(ctx, p, domain.Segment{
		Name: "Exp Dynamic Segment",
		Type: "dynamic",
		DSL:  []byte(`{"type":"profile_attribute","field":"country","operator":"equals","value":"US"}`),
	})
	if err != nil {
		t.Fatalf("create segment: %v", err)
	}

	iden, err := store.CreateSendingIdentity(ctx, p, domain.SendingIdentity{
		Channel:     "email",
		FromAddress: ptr("sender@example.com"),
		FromName:    ptr("Sender"),
		Provider:    "fake",
		MaxSendRate: 10,
	})
	if err != nil {
		t.Fatalf("create sending identity: %v", err)
	}

	// Campaign 1: Valid Template (p1 should be sent/eligible, p2 suppressed)
	htmlTmpl1 := "Hello Exp!"
	tmpl1, err := store.CreateTemplate(ctx, p, domain.Template{
		Name:              "Exp Template 1",
		Channel:           "email",
		HTMLTemplate:      &htmlTmpl1,
		SendingIdentityID: &iden.ID,
	})
	if err != nil {
		t.Fatalf("create template 1: %v", err)
	}

	camp1, err := store.CreateCampaign(ctx, p, domain.Campaign{
		Name:            "Exp Campaign 1",
		SegmentID:       seg.ID,
		TemplateID:      tmpl1.ID,
		Status:          "draft",
		SegmentVersion:  1,
		TemplateVersion: 1,
	})
	if err != nil {
		t.Fatalf("create campaign 1: %v", err)
	}

	pastTime := time.Now().Add(-1 * time.Minute)
	camp1.Status = "scheduled"
	camp1.ScheduledAt = &pastTime
	camp1, err = store.UpdateCampaign(ctx, p, camp1)
	if err != nil {
		t.Fatalf("update campaign 1: %v", err)
	}

	blob := &mockBlobStore{}
	dispatched, err := campaigns.DispatchNext(ctx, store, blob)
	if err != nil {
		t.Fatalf("DispatchNext 1: %v", err)
	}
	if !dispatched {
		t.Fatal("expected to dispatch campaign 1")
	}

	fakeAdapter := channels.NewFakeAdapter()
	cfg := campaigns.Config{
		TrackingSecretKey: []byte("tracking-secret-key-12345"),
		TrackingBaseURL:   "http://localhost:8080",
		FakeAdapter:       fakeAdapter,
	}

	delivered, err := campaigns.DeliverNext(ctx, store, "worker-1", cfg)
	if err != nil {
		t.Fatalf("DeliverNext 1: %v", err)
	}
	if !delivered {
		t.Fatal("expected to deliver campaign 1 jobs")
	}

	// 2. Query delivery attempts for Campaign 1 and assert explainability
	rows, err := store.pool.Query(ctx, "SELECT profile_id, decision, reason FROM delivery_attempts WHERE campaign_id = $1", camp1.ID)
	if err != nil {
		t.Fatalf("query attempts 1: %v", err)
	}
	defer rows.Close()

	attemptsCount := 0
	for rows.Next() {
		var profID, decision, reason string
		if err := rows.Scan(&profID, &decision, &reason); err != nil {
			t.Fatalf("scan attempt 1: %v", err)
		}
		attemptsCount++
		if reason == "" {
			t.Errorf("expected non-empty reason for profile %s, decision %s", profID, decision)
		}
		prof, err := store.GetProfileByID(ctx, tenantID, p.AppID, profID)
		if err != nil {
			t.Fatalf("get profile: %v", err)
		}
		if prof.ExternalID == "p1" {
			if decision != "sent" || reason != "eligible" {
				t.Errorf("p1 expected sent/eligible, got %s/%s", decision, reason)
			}
		} else if prof.ExternalID == "p2" {
			if decision != "suppressed" || reason != "endpoint is suppressed" {
				t.Errorf("p2 expected suppressed/endpoint is suppressed, got %s/%s", decision, reason)
			}
		}
	}
	if attemptsCount != 2 {
		t.Errorf("expected 2 delivery attempts for campaign 1, got %d", attemptsCount)
	}

	// Campaign 2: Invalid Template (p1 should fail rendering)
	htmlTmpl2 := "Hello {% invalid_tag %}"
	tmpl2, err := store.CreateTemplate(ctx, p, domain.Template{
		Name:              "Exp Template 2",
		Channel:           "email",
		HTMLTemplate:      &htmlTmpl2,
		SendingIdentityID: &iden.ID,
	})
	if err != nil {
		t.Fatalf("create template 2: %v", err)
	}

	camp2, err := store.CreateCampaign(ctx, p, domain.Campaign{
		Name:            "Exp Campaign 2",
		SegmentID:       seg.ID,
		TemplateID:      tmpl2.ID,
		Status:          "draft",
		SegmentVersion:  1,
		TemplateVersion: 1,
	})
	if err != nil {
		t.Fatalf("create campaign 2: %v", err)
	}

	camp2.Status = "scheduled"
	camp2.ScheduledAt = &pastTime
	camp2, err = store.UpdateCampaign(ctx, p, camp2)
	if err != nil {
		t.Fatalf("update campaign 2: %v", err)
	}

	dispatched, err = campaigns.DispatchNext(ctx, store, blob)
	if err != nil {
		t.Fatalf("DispatchNext 2: %v", err)
	}
	if !dispatched {
		t.Fatal("expected to dispatch campaign 2")
	}

	delivered, err = campaigns.DeliverNext(ctx, store, "worker-1", cfg)
	if err != nil {
		t.Fatalf("DeliverNext 2: %v", err)
	}
	if !delivered {
		t.Fatal("expected to deliver campaign 2 jobs")
	}

	// Query delivery attempts for Campaign 2 and assert explainability
	rows2, err := store.pool.Query(ctx, "SELECT profile_id, decision, reason FROM delivery_attempts WHERE campaign_id = $1", camp2.ID)
	if err != nil {
		t.Fatalf("query attempts 2: %v", err)
	}
	defer rows2.Close()

	attemptsCount2 := 0
	for rows2.Next() {
		var profID, decision, reason string
		if err := rows2.Scan(&profID, &decision, &reason); err != nil {
			t.Fatalf("scan attempt 2: %v", err)
		}
		attemptsCount2++
		if reason == "" {
			t.Errorf("expected non-empty reason for profile %s, decision %s", profID, decision)
		}
		prof, err := store.GetProfileByID(ctx, tenantID, p.AppID, profID)
		if err != nil {
			t.Fatalf("get profile: %v", err)
		}
		if prof.ExternalID == "p1" {
			if decision != "render_failed" || !strings.Contains(reason, "render error") {
				t.Errorf("p1 expected render_failed/render error, got %s/%s", decision, reason)
			}
		}
	}
	if attemptsCount2 != 2 {
		t.Errorf("expected 2 delivery attempts for campaign 2, got %d", attemptsCount2)
	}
}

func TestTenantFatigueQuotas(t *testing.T) {
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

	tenantKey := fmt.Sprintf("%s-%d", t.Name(), time.Now().UnixNano())
	tenantID := testUUID(tenantKey)
	p := domain.Principal{TenantID: tenantID, WorkspaceID: testUUID(tenantKey + "-workspace-1"), AppID: testUUID(tenantKey + "-app-1")}

	_, err = store.pool.Exec(ctx, `INSERT INTO tenants(id, name) VALUES($1, 'Test Tenant Quotas')`, tenantID)
	if err != nil {
		t.Fatalf("insert tenant: %v", err)
	}

	// 1. Get fatigue quotas before creating entry (should return defaults: 5, 20)
	maxSends24h, maxSends7d, err := store.GetTenantFatigueQuotas(ctx, p)
	if err != nil {
		t.Fatalf("GetTenantFatigueQuotas defaults: %v", err)
	}
	if maxSends24h != 5 || maxSends7d != 20 {
		t.Errorf("expected default quotas 5, 20; got %d, %d", maxSends24h, maxSends7d)
	}

	// 2. Insert custom quota values into tenant_quotas
	_, err = store.pool.Exec(ctx, `INSERT INTO tenant_quotas(tenant_id, max_sends_24h, max_sends_7d) VALUES($1, $2, $3)`, tenantID, 12, 42)
	if err != nil {
		t.Fatalf("insert tenant_quotas: %v", err)
	}

	// 3. Get quotas again (should return custom values 12, 42)
	maxSends24h, maxSends7d, err = store.GetTenantFatigueQuotas(ctx, p)
	if err != nil {
		t.Fatalf("GetTenantFatigueQuotas custom: %v", err)
	}
	if maxSends24h != 12 || maxSends7d != 42 {
		t.Errorf("expected custom quotas 12, 42; got %d, %d", maxSends24h, maxSends7d)
	}
}
