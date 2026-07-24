package httpapi

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/buildwithdmytro/openjourney/internal/campaigns"
	"github.com/buildwithdmytro/openjourney/internal/domain"
	"github.com/buildwithdmytro/openjourney/internal/postgres"
	"github.com/buildwithdmytro/openjourney/internal/ports"
	"github.com/buildwithdmytro/openjourney/internal/render"
)

func setupCatalogsTestTenant(t *testing.T, ctx context.Context, store *postgres.Store) (domain.Principal, string) {
	tenantKey := fmt.Sprintf("%s-%d", t.Name(), time.Now().UnixNano())
	if err := store.EnsureDevelopmentTenant(ctx, tenantKey); err != nil {
		t.Fatalf("ensure tenant: %v", err)
	}
	p, err := store.Authenticate(ctx, tenantKey)
	if err != nil {
		t.Fatalf("authenticate: %v", err)
	}
	appID, err := store.GetFirstAppID(ctx, p.TenantID, p.WorkspaceID)
	if err != nil {
		t.Fatalf("get app: %v", err)
	}
	p.AppID = appID
	p.Scopes = []string{"*"}
	return p, p.TenantID
}

// fakeChannelAdapter simulates a channel adapter for testing
type fakeChannelAdapter struct {
	mu           sync.Mutex
	sentMessages map[string][]ports.RenderedMessage
}

func newFakeChannelAdapter() *fakeChannelAdapter {
	return &fakeChannelAdapter{
		sentMessages: make(map[string][]ports.RenderedMessage),
	}
}

func (f *fakeChannelAdapter) Send(ctx context.Context, msg ports.RenderedMessage) (string, int64, error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	channel := msg.Channel
	if channel == "" {
		channel = "email"
	}

	f.sentMessages[channel] = append(f.sentMessages[channel], msg)
	return "msg-" + msg.Endpoint[:8], 0, nil
}

func (f *fakeChannelAdapter) ValidateConfig(iden domain.SendingIdentity) error {
	return nil
}

func TestCatalogsAndConnectedContentE2E(t *testing.T) {
	databaseURL := os.Getenv("OPENJOURNEY_TEST_DATABASE_URL")
	if databaseURL == "" {
		t.Skip("OPENJOURNEY_TEST_DATABASE_URL is not configured")
	}

	ctx := context.Background()
	store, err := postgres.Open(ctx, databaseURL)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	if err := store.Migrate(ctx); err != nil {
		t.Fatal(err)
	}

	principal, tenantID := setupCatalogsTestTenant(t, ctx, store)

	// Create a catalog for products
	productCatalog, err := store.CreateCatalog(ctx, principal, domain.Catalog{
		Key:            "products",
		Name:           "Product Catalog",
		ItemKeyField:   "sku",
		Status:         "active",
	})
	if err != nil {
		t.Fatalf("failed to create catalog: %v", err)
	}

	// Bulk upsert items to the catalog
	items := []domain.CatalogItem{
		{
			CatalogID: productCatalog.ID,
			ItemKey:   "laptop-001",
			Payload:   json.RawMessage(`{"sku":"laptop-001","name":"Professional Laptop","price":1299.99,"stock":50}`),
		},
		{
			CatalogID: productCatalog.ID,
			ItemKey:   "mouse-001",
			Payload:   json.RawMessage(`{"sku":"mouse-001","name":"Wireless Mouse","price":29.99,"stock":200}`),
		},
	}

	_, err = store.BulkUpsertCatalogItems(ctx, principal, items)
	if err != nil {
		t.Fatalf("failed to bulk upsert catalog items: %v", err)
	}

	// Create a connected content source
	source, err := store.CreateConnectedContentSource(ctx, principal, domain.ConnectedContentSource{
		Name:               "discount-service",
		AllowedHost:        "api.example.com",
		AuthHeaderName:     "X-API-Key",
		AuthSecretRef:      "CC_SECRET_DISCOUNT_API_KEY",
		DefaultTTLSeconds:  300,
		TimeoutMs:          2000,
		Enabled:            true,
		Status:             "active",
	})
	if err != nil {
		t.Fatalf("failed to create connected content source: %v", err)
	}

	// Create profiles directly via SQL
	profileID1 := "550e8400-e29b-41d4-a716-446655440000"
	profileID2 := "550e8400-e29b-41d4-a716-446655440001"

	_ = store.Pool().QueryRow(ctx, `
		INSERT INTO profiles (id, tenant_id, workspace_id, app_id, external_id, attributes)
		VALUES ($1, $2, $3, $4, $5, $6)
		ON CONFLICT (id) DO NOTHING
	`, profileID1, tenantID, principal.WorkspaceID, principal.AppID, "user-123",
		json.RawMessage(`{"name":"Alice","email":"alice@example.com","selected_product":"laptop-001"}`)).Scan()

	_ = store.Pool().QueryRow(ctx, `
		INSERT INTO profiles (id, tenant_id, workspace_id, app_id, external_id, attributes)
		VALUES ($1, $2, $3, $4, $5, $6)
		ON CONFLICT (id) DO NOTHING
	`, profileID2, tenantID, principal.WorkspaceID, principal.AppID, "user-456",
		json.RawMessage(`{"name":"Bob","email":"bob@example.com","selected_product":"mouse-001"}`)).Scan()

	// Create a sending identity
	identity, err := store.CreateSendingIdentity(ctx, principal, domain.SendingIdentity{
		Channel:     "email",
		Provider:    "fake",
		MaxSendRate: 100,
	})
	if err != nil {
		t.Fatalf("failed to create sending identity: %v", err)
	}

	// Create a template that uses the catalog_item filter
	htmlTmpl := "Check out: {{ selected_product | catalog_item: 'products' }}"
	template, err := store.CreateTemplate(ctx, principal, domain.Template{
		Name:              "Catalog Test Template",
		Channel:           "email",
		HTMLTemplate:      &htmlTmpl,
		SendingIdentityID: &identity.ID,
	})
	if err != nil {
		t.Fatalf("failed to create template: %v", err)
	}

	// Create a campaign
	campaign, err := store.CreateCampaign(ctx, principal, domain.Campaign{
		Name:       "Catalog E2E Test",
		TemplateID: template.ID,
	})
	if err != nil {
		t.Fatalf("failed to create campaign: %v", err)
	}

	// Claim the campaign to prepare for delivery
	claimedCampaign, found, err := store.ClaimScheduledCampaign(ctx)
	if err == nil && found {
		// If a campaign was claimed, update it to be the one we just created
		campaign = claimedCampaign
	} else {
		// Update campaign status to building/sending for testing
		campaign.Status = "sending"
		campaign, err = store.UpdateCampaign(ctx, principal, campaign)
		if err != nil {
			t.Fatalf("failed to update campaign status: %v", err)
		}
	}

	// Save campaign manifest and delivery jobs
	jobs := []domain.DeliveryJob{
		{
			TenantID: tenantID,
			Shard:    0,
			Recipients: []domain.Recipient{
				{ProfileID: profileID1, Endpoint: "alice@example.com"},
				{ProfileID: profileID2, Endpoint: "bob@example.com"},
			},
		},
	}

	err = store.SaveCampaignManifestAndJobs(ctx, campaign.ID, "manifests/catalog-test.json", 2, 1, 1, nil, "", jobs)
	if err != nil {
		t.Fatalf("failed to save campaign manifest and jobs: %v", err)
	}

	// Set up delivery configuration with fake adapter
	fakeAdapter := newFakeChannelAdapter()
	deliveryConfig := campaigns.Config{
		TrackingSecretKey: []byte("test-secret"),
		TrackingBaseURL:   "http://localhost:3000",
		Adapter:           fakeAdapter,
	}

	// Deliver the campaign
	deliveryCount := 0
	for {
		delivered, err := campaigns.DeliverNext(ctx, store, "worker-1", deliveryConfig)
		if err != nil {
			t.Fatalf("delivery failed: %v", err)
		}
		if !delivered {
			break
		}
		deliveryCount++
		if deliveryCount > 10 { // Safety limit
			break
		}
	}

	if deliveryCount != 2 {
		t.Errorf("expected 2 deliveries, got %d", deliveryCount)
	}

	// Verify the catalog items were rendered in the output
	if len(fakeAdapter.sentMessages["email"]) < 2 {
		t.Fatalf("expected at least 2 emails to be sent, got %d", len(fakeAdapter.sentMessages["email"]))
	}

	email1 := fakeAdapter.sentMessages["email"][0]
	email2 := fakeAdapter.sentMessages["email"][1]

	// Both should contain catalog item data (either the laptop or mouse)
	if !strings.Contains(email1.HTML, "Professional Laptop") && !strings.Contains(email1.HTML, "Wireless Mouse") {
		t.Errorf("expected catalog item name in first email HTML, got: %s", email1.HTML)
	}

	if !strings.Contains(email2.HTML, "Professional Laptop") && !strings.Contains(email2.HTML, "Wireless Mouse") {
		t.Errorf("expected catalog item name in second email HTML, got: %s", email2.HTML)
	}

	// Verify the connected content source was persisted and retrievable
	retrievedSource, err := store.GetConnectedContentSource(ctx, principal, source.ID)
	if err != nil {
		t.Fatalf("failed to retrieve connected content source: %v", err)
	}

	if retrievedSource.AllowedHost != source.AllowedHost {
		t.Errorf("source host mismatch: expected %s, got %s", source.AllowedHost, retrievedSource.AllowedHost)
	}

	// Verify catalog items can be listed
	listedItems, err := store.ListCatalogItems(ctx, principal, productCatalog.ID, 10)
	if err != nil {
		t.Fatalf("failed to list catalog items: %v", err)
	}

	if len(listedItems) != 2 {
		t.Errorf("expected 2 items, got %d", len(listedItems))
	}

	// Test caching by rendering the same filter multiple times
	templateStr := `Product: {{ sku | catalog_item: 'products' }}`
	cache := render.NewTTLCache(100, render.SystemClock{})
	deps := render.RenderDeps{
		Store:     store,
		Principal: principal,
		Fetcher:   nil,
		Cache:     cache,
	}

	// First render should hit the store and cache
	result1, err := render.RenderWithContext(ctx, templateStr, map[string]any{"sku": "laptop-001"}, deps)
	if err != nil {
		t.Fatalf("failed to render filter: %v", err)
	}
	if !strings.Contains(result1, "Professional Laptop") {
		t.Errorf("expected laptop in result, got: %s", result1)
	}

	// Verify cache was populated
	cacheKey := "catalog:" + principal.TenantID + ":" + principal.AppID + ":products:laptop-001"
	cachedItem, ok := cache.Get(cacheKey)
	if !ok {
		t.Fatal("expected item to be cached after first render")
	}
	cachedPayload, ok := cachedItem.(map[string]any)
	if !ok || cachedPayload["sku"] != "laptop-001" {
		t.Errorf("cached item mismatch: %v", cachedItem)
	}

	// Second render should use cached value (won't fail even if store is down)
	result2, err := render.RenderWithContext(ctx, templateStr, map[string]any{"sku": "laptop-001"}, deps)
	if err != nil {
		t.Fatalf("second render failed: %v", err)
	}

	// Both renders should produce same output
	if result1 != result2 {
		t.Errorf("renders should be identical: %q vs %q", result1, result2)
	}
}
