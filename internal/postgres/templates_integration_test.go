package postgres

import (
	"context"
	"os"
	"testing"

	"github.com/buildwithdmytro/openjourney/internal/domain"
)

func TestTemplatesIntegration(t *testing.T) {
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

	// 1. Test Sending Identities
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

	fetchedIden, err := store.GetSendingIdentity(ctx, p, iden.ID)
	if err != nil {
		t.Fatalf("get sending identity: %v", err)
	}
	if *fetchedIden.FromAddress != "sender@example.com" {
		t.Errorf("expected sender@example.com, got %s", *fetchedIden.FromAddress)
	}

	listIdens, err := store.ListSendingIdentities(ctx, p)
	if err != nil {
		t.Fatalf("list sending identities: %v", err)
	}
	if len(listIdens) != 1 {
		t.Errorf("expected 1 sending identity, got %d", len(listIdens))
	}

	// 2. Test Templates
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

	fetchedTmpl, err := store.GetTemplate(ctx, p, tmpl.ID)
	if err != nil {
		t.Fatalf("get template: %v", err)
	}
	if *fetchedTmpl.HTMLTemplate != htmlTmpl {
		t.Errorf("expected %s, got %s", htmlTmpl, *fetchedTmpl.HTMLTemplate)
	}
	if fetchedTmpl.Version != 1 {
		t.Errorf("expected version 1, got %d", fetchedTmpl.Version)
	}

	// Update template text and check version bump
	newHtml := "Hello {{ name }}! Welcome!"
	fetchedTmpl.HTMLTemplate = &newHtml
	updatedTmpl, err := store.UpdateTemplate(ctx, p, fetchedTmpl)
	if err != nil {
		t.Fatalf("update template: %v", err)
	}
	if updatedTmpl.Version != 2 {
		t.Errorf("expected version bump to 2, got %d", updatedTmpl.Version)
	}

	listTmpls, err := store.ListTemplates(ctx, p)
	if err != nil {
		t.Fatalf("list templates: %v", err)
	}
	if len(listTmpls) != 1 {
		t.Errorf("expected 1 template, got %d", len(listTmpls))
	}

	// 3. Test Tracked Links
	linkID1, err := store.UpsertTrackedLink(ctx, tenantID, tmpl.ID, "https://example.com/promo")
	if err != nil {
		t.Fatalf("upsert tracked link: %v", err)
	}
	linkID2, err := store.UpsertTrackedLink(ctx, tenantID, tmpl.ID, "https://example.com/promo")
	if err != nil {
		t.Fatalf("upsert duplicate tracked link: %v", err)
	}

	if linkID1 != linkID2 {
		t.Errorf("expected same link ID on conflict, got %s and %s", linkID1, linkID2)
	}

	// 4. Test Push Templates
	titleTmpl := "Title for {{ profile.attributes.name }}"
	bodyTmpl := "Body with link: {{ deep_link }}"
	pushTmpl, err := store.CreateTemplate(ctx, p, domain.Template{
		Name:          "Push Template",
		Channel:       "push",
		TitleTemplate: &titleTmpl,
		BodyTemplate:  &bodyTmpl,
		PushData: map[string]string{
			"deep_link": "https://example.com/promo?id={{ profile.id }}",
			"image":     "https://example.com/img.png",
		},
	})
	if err != nil {
		t.Fatalf("create push template: %v", err)
	}

	fetchedPushTmpl, err := store.GetTemplate(ctx, p, pushTmpl.ID)
	if err != nil {
		t.Fatalf("get push template: %v", err)
	}
	if fetchedPushTmpl.TitleTemplate == nil || *fetchedPushTmpl.TitleTemplate != titleTmpl {
		t.Errorf("expected TitleTemplate %q, got %v", titleTmpl, fetchedPushTmpl.TitleTemplate)
	}
	if fetchedPushTmpl.PushData["deep_link"] != "https://example.com/promo?id={{ profile.id }}" {
		t.Errorf("expected deep_link data to match")
	}

	// Update push template
	fetchedPushTmpl.PushData["image"] = "https://example.com/new-img.png"
	updatedPushTmpl, err := store.UpdateTemplate(ctx, p, fetchedPushTmpl)
	if err != nil {
		t.Fatalf("update push template: %v", err)
	}
	if updatedPushTmpl.Version != 2 {
		t.Errorf("expected push template version bump to 2, got %d", updatedPushTmpl.Version)
	}

	// Clean up
	_, _ = store.pool.Exec(ctx, "DELETE FROM tenants WHERE id=$1", tenantID)
}

func ptr[T any](v T) *T {
	return &v
}
