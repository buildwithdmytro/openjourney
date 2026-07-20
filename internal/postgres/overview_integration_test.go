package postgres

import (
	"context"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/buildwithdmytro/openjourney/internal/domain"
)

func TestGetOverviewReturnsTenantScopedCounts(t *testing.T) {
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

	key := fmt.Sprintf("overview-test-%d", time.Now().UnixNano())
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

	// Test that overview returns zero counts for new tenant
	overview, err := store.GetOverview(ctx, p)
	if err != nil {
		t.Fatalf("GetOverview: %v", err)
	}
	if overview.Profiles != 0 || overview.Journeys != 0 || overview.Campaigns != 0 {
		t.Errorf("expected zero counts for new tenant, got: %+v", overview)
	}

	// Create a profile
	_, err = store.pool.Exec(ctx,
		`INSERT INTO profiles (tenant_id, workspace_id, app_id, external_id, attributes)
		 VALUES ($1, $2, $3, 'profile-1', '{}')`,
		p.TenantID, p.WorkspaceID, p.AppID,
	)
	if err != nil {
		t.Fatalf("create profile: %v", err)
	}

	// Verify profile count increased
	overview, err = store.GetOverview(ctx, p)
	if err != nil {
		t.Fatalf("GetOverview after insert: %v", err)
	}
	if overview.Profiles != 1 {
		t.Errorf("expected 1 profile, got: %d", overview.Profiles)
	}

	// Create a journey
	description := "Test"
	journey := domain.Journey{
		ID:          "j1",
		Name:        "Test Journey",
		Description: &description,
		Graph:       []byte(`{"nodes":[],"edges":[]}`),
		Status:      "draft",
	}
	_, err = store.CreateJourney(ctx, p, journey)
	if err != nil {
		t.Fatalf("create journey: %v", err)
	}

	// Verify journey count increased
	overview, err = store.GetOverview(ctx, p)
	if err != nil {
		t.Fatalf("GetOverview after journey: %v", err)
	}
	if overview.Journeys != 1 {
		t.Errorf("expected 1 journey, got: %d", overview.Journeys)
	}

	// Create a segment first (needed for campaign)
	segment := domain.Segment{
		ID:   "seg1",
		Name: "Test Segment",
		DSL:  []byte(`{"query":"true"}`),
	}
	_, err = store.CreateSegment(ctx, p, segment)
	if err != nil {
		t.Fatalf("create segment: %v", err)
	}

	// Create a template (needed for campaign)
	htmlBody := "<p>Test body</p>"
	template := domain.Template{
		ID:           "tpl1",
		Name:         "Test Template",
		Channel:      "email",
		HTMLTemplate: &htmlBody,
	}
	_, err = store.CreateTemplate(ctx, p, template)
	if err != nil {
		t.Fatalf("create template: %v", err)
	}

	// Create a campaign
	campaign := domain.Campaign{
		ID:         "c1",
		Name:       "Test Campaign",
		SegmentID:  "seg1",
		TemplateID: "tpl1",
		Status:     "draft",
	}
	_, err = store.CreateCampaign(ctx, p, campaign)
	if err != nil {
		t.Fatalf("create campaign: %v", err)
	}

	// Verify campaign count increased
	overview, err = store.GetOverview(ctx, p)
	if err != nil {
		t.Fatalf("GetOverview after campaign: %v", err)
	}
	if overview.Campaigns != 1 {
		t.Errorf("expected 1 campaign, got: %d", overview.Campaigns)
	}

	// Verify counts are tenant-scoped by creating another tenant
	key2 := fmt.Sprintf("overview-test2-%d", time.Now().UnixNano())
	if err := store.EnsureDevelopmentTenant(ctx, key2); err != nil {
		t.Fatalf("ensure second tenant: %v", err)
	}
	p2, err := store.Authenticate(ctx, key2)
	if err != nil {
		t.Fatalf("authenticate second tenant: %v", err)
	}
	appID2, err := store.GetFirstAppID(ctx, p2.TenantID, p2.WorkspaceID)
	if err != nil {
		t.Fatalf("get first app ID for second tenant: %v", err)
	}
	p2.AppID = appID2

	overview2, err := store.GetOverview(ctx, p2)
	if err != nil {
		t.Fatalf("GetOverview for second tenant: %v", err)
	}
	if overview2.Profiles != 0 || overview2.Journeys != 0 || overview2.Campaigns != 0 {
		t.Errorf("expected zero counts for second tenant, got: %+v", overview2)
	}

	// Verify first tenant still has its counts
	overview1, err := store.GetOverview(ctx, p)
	if err != nil {
		t.Fatalf("GetOverview for first tenant after second: %v", err)
	}
	if overview1.Profiles != 1 || overview1.Journeys != 1 || overview1.Campaigns != 1 {
		t.Errorf("expected 1 of each for first tenant, got: %+v", overview1)
	}
}
