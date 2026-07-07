package render

import (
	"strings"
	"testing"
)

func TestRewriteLinks(t *testing.T) {
	htmlBody := `<html><body><p>Click <a href="https://example.com/promo">here</a>.</p></body></html>`
	tenantID := "tenant-001"
	appID := "app-001"
	campaignID := "camp-123"
	profileID := "prof-456"
	templateID := "tmpl-789"
	dispatchID := "disp-012"
	secretKey := []byte("my-secret-key-1234567890-abcdef")
	baseTrackingURL := "https://track.openjourney.io"

	upsert := func(url string) (string, error) {
		if url != "https://example.com/promo" {
			t.Errorf("unexpected upsert url: %s", url)
		}
		return "link-789", nil
	}

	updated, err := RewriteLinks(htmlBody, tenantID, appID, campaignID, profileID, templateID, dispatchID, upsert, secretKey, baseTrackingURL)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !strings.Contains(updated, "https://track.openjourney.io/r/") {
		t.Errorf("expected updated body to contain tracking url, got: %s", updated)
	}

	parts := strings.Split(updated, `href="https://track.openjourney.io/r/`)
	if len(parts) < 2 {
		t.Fatalf("failed to find href in updated body: %s", updated)
	}
	token := strings.Split(parts[1], `"`)[0]

	tnt, app, cID, pID, lID, tID, dID, destURL, err := VerifyLinkToken(token, secretKey)
	if err != nil {
		t.Fatalf("token verification failed: %v", err)
	}

	if tnt != tenantID || app != appID || cID != campaignID || pID != profileID || lID != "link-789" || tID != templateID || dID != dispatchID || destURL != "https://example.com/promo" {
		t.Errorf("invalid token contents: tnt=%s app=%s cID=%s pID=%s lID=%s tID=%s dID=%s url=%s", tnt, app, cID, pID, lID, tID, dID, destURL)
	}
}

func TestOpenToken(t *testing.T) {
	tenantID := "tenant-001"
	appID := "app-001"
	campaignID := "camp-123"
	profileID := "prof-456"
	templateID := "tmpl-789"
	dispatchID := "disp-012"
	secretKey := []byte("my-secret-key-1234567890-abcdef")

	token, err := SignOpenToken(tenantID, appID, campaignID, profileID, templateID, dispatchID, secretKey)
	if err != nil {
		t.Fatalf("failed to sign open token: %v", err)
	}

	tnt, app, cID, pID, tID, dID, err := VerifyOpenToken(token, secretKey)
	if err != nil {
		t.Fatalf("failed to verify open token: %v", err)
	}

	if tnt != tenantID || app != appID || cID != campaignID || pID != profileID || tID != templateID || dID != dispatchID {
		t.Errorf("invalid open token contents: tnt=%s app=%s cID=%s pID=%s tID=%s dID=%s", tnt, app, cID, pID, tID, dID)
	}
}
