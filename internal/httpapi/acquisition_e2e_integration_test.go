package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"regexp"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/buildwithdmytro/openjourney/internal/domain"
	formdefinition "github.com/buildwithdmytro/openjourney/internal/forms"
	"github.com/buildwithdmytro/openjourney/internal/postgres"
	"github.com/buildwithdmytro/openjourney/internal/projector"
)

type acquisitionE2EBlobs struct {
	mu      sync.Mutex
	objects map[string][]byte
}

func (b *acquisitionE2EBlobs) Put(_ context.Context, key string, data []byte, _ string) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.objects == nil {
		b.objects = make(map[string][]byte)
	}
	b.objects[key] = append([]byte(nil), data...)
	return nil
}

func (b *acquisitionE2EBlobs) Get(_ context.Context, key string) ([]byte, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	data, ok := b.objects[key]
	if !ok {
		return nil, errors.New("blob not found")
	}
	return append([]byte(nil), data...), nil
}

func (b *acquisitionE2EBlobs) Delete(_ context.Context, key string) error { return nil }

func TestCapturePageSubmitCreatesProfileWithConsentAndUTM(t *testing.T) {
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

	tenantKey := "acquisition-e2e-" + strings.ReplaceAll(t.Name(), "/", "-")
	if err := store.EnsureDevelopmentTenant(ctx, tenantKey); err != nil {
		t.Fatal(err)
	}
	principal, err := store.Authenticate(ctx, tenantKey)
	if err != nil {
		t.Fatal(err)
	}
	principal.AppID, err = store.GetFirstAppID(ctx, principal.TenantID, principal.WorkspaceID)
	if err != nil {
		t.Fatal(err)
	}

	blobs := &acquisitionE2EBlobs{}
	formDraft := json.RawMessage(`{"fields":[{"key":"email","type":"email","required":true,"maps_to":"email"},{"key":"consent","type":"boolean","required":true,"consent":true,"maps_to":"email"}]}`)
	form, err := store.CreateForm(ctx, principal, domain.Form{Name: "Signup", Draft: formDraft})
	if err != nil {
		t.Fatal(err)
	}
	formDefinition, err := formdefinition.CanonicalizeDraft(formDraft)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.PublishForm(ctx, principal, form.ID, "human-e2e", "forms/e2e-v1", formDefinition); err != nil {
		t.Fatal(err)
	}

	pageDraft := json.RawMessage(`{"template":"<h1>Signup</h1><form><input name=\"form_token\" value=\"{{ form_token }}\"></form>","form_id":"` + form.ID + `","form_version":1}`)
	page, err := store.CreateLandingPage(ctx, principal, domain.LandingPage{Slug: "signup-e2e", Name: "Signup", Draft: pageDraft})
	if err != nil {
		t.Fatal(err)
	}
	pageDefinition := json.RawMessage(`{"template":"<h1>Signup</h1><form><input name=\"form_token\" value=\"{{ form_token }}\"></form>","form_id":"` + form.ID + `","form_version":1}`)
	if _, err := store.PublishLandingPage(ctx, principal, page.ID, "human-e2e", "pages/e2e-v1", pageDefinition); err != nil {
		t.Fatal(err)
	}

	handler := NewWithSessionTTL(store, 20, nil, "*", time.Hour, func(s *Server) {
		s.SetBlobStore(blobs)
		s.SetTracking([]byte("change-me-in-production"), "http://localhost")
		s.publicLimiter = NewIPRateLimiter(100, 10)
	})
	server := httptest.NewServer(handler)
	defer server.Close()

	pageResponse, err := server.Client().Get(server.URL + "/p/signup-e2e")
	if err != nil {
		t.Fatal(err)
	}
	defer pageResponse.Body.Close()
	pageHTML, err := io.ReadAll(pageResponse.Body)
	if err != nil {
		t.Fatal(err)
	}
	if pageResponse.StatusCode != http.StatusOK || pageResponse.Header.Get("Content-Type") != "text/html; charset=utf-8" {
		t.Fatalf("page status/content type = %d/%q", pageResponse.StatusCode, pageResponse.Header.Get("Content-Type"))
	}
	tokenMatch := regexp.MustCompile(`name="form_token" value="([^"]+)"`).FindSubmatch(pageHTML)
	if len(tokenMatch) != 2 {
		t.Fatalf("signed form token missing from page: %s", pageHTML)
	}

	submission := `{"form_token":"` + string(tokenMatch[1]) + `","values":{"email":"e2e@example.com","consent":true},"utm":{"source":"newsletter","campaign":"launch"}}`
	request, err := http.NewRequest(http.MethodPost, server.URL+"/f/"+form.ID, strings.NewReader(submission))
	if err != nil {
		t.Fatal(err)
	}
	request.Header.Set("Content-Type", "application/json")
	request.RemoteAddr = "198.51.100.25:1234"
	submitResponse, err := server.Client().Do(request)
	if err != nil {
		t.Fatal(err)
	}
	submitResponse.Body.Close()
	if submitResponse.StatusCode != http.StatusAccepted {
		t.Fatalf("submit status = %d", submitResponse.StatusCode)
	}
	duplicateRequest, err := http.NewRequest(http.MethodPost, server.URL+"/f/"+form.ID, strings.NewReader(submission))
	if err != nil {
		t.Fatal(err)
	}
	duplicateRequest.Header.Set("Content-Type", "application/json")
	duplicateRequest.RemoteAddr = "198.51.100.25:1234"
	duplicateResponse, err := server.Client().Do(duplicateRequest)
	if err != nil {
		t.Fatal(err)
	}
	duplicateResponse.Body.Close()
	if duplicateResponse.StatusCode != http.StatusAccepted {
		t.Fatalf("duplicate submit status = %d", duplicateResponse.StatusCode)
	}

	if _, err := projector.Drain(ctx, store, 3, false); err != nil {
		t.Fatal(err)
	}
	profile, consents, err := store.GetProfile(ctx, principal, "e2e@example.com")
	if err != nil {
		t.Fatalf("get projected profile: %v", err)
	}
	if len(consents) != 1 || consents[0].State != "subscribed" || consents[0].Channel != "email" {
		t.Fatalf("consent ledger = %+v", consents)
	}
	var utmSource string
	if err := store.Pool().QueryRow(ctx, `SELECT attributes->>'utm_source' FROM profiles WHERE id=$1`, profile.ID).Scan(&utmSource); err != nil {
		t.Fatal(err)
	}
	if utmSource != "newsletter" {
		t.Fatalf("profile UTM source = %q", utmSource)
	}
	var submitted, submissions int
	if err := store.Pool().QueryRow(ctx, `SELECT count(*) FROM accepted_events WHERE tenant_id=$1 AND event_type='form.submitted'`, principal.TenantID).Scan(&submitted); err != nil {
		t.Fatal(err)
	}
	if err := store.Pool().QueryRow(ctx, `SELECT count(*) FROM form_submissions WHERE tenant_id=$1 AND form_id=$2`, principal.TenantID, form.ID).Scan(&submissions); err != nil {
		t.Fatal(err)
	}
	var profiles int
	if err := store.Pool().QueryRow(ctx, `SELECT count(*) FROM profiles WHERE tenant_id=$1 AND external_id=$2`, principal.TenantID, "e2e@example.com").Scan(&profiles); err != nil {
		t.Fatal(err)
	}
	if submitted != 1 || submissions != 1 || profiles != 1 {
		t.Fatalf("capture records: form.submitted=%d form_submissions=%d profiles=%d", submitted, submissions, profiles)
	}
}
