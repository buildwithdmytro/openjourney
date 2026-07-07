package httpapi

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/buildwithdmytro/openjourney/internal/domain"
	"github.com/buildwithdmytro/openjourney/internal/postgres"
)

type staticTokenVerifier struct {
	claimsByToken map[string]domain.OIDCClaims
}

func (v staticTokenVerifier) Verify(_ context.Context, raw string) (domain.OIDCClaims, error) {
	claims, ok := v.claimsByToken[raw]
	if !ok {
		return domain.OIDCClaims{}, postgres.ErrUnauthorized
	}
	return claims, nil
}

func TestHTTPAPIEnforcesTenantContextFromCredentials(t *testing.T) {
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

	suffix := time.Now().UTC().Format("20060102150405.000000000")
	keyA := "http-tenant-a-" + suffix
	keyB := "http-tenant-b-" + suffix
	if err := store.EnsureDevelopmentTenant(ctx, keyA); err != nil {
		t.Fatal(err)
	}
	if err := store.EnsureDevelopmentTenant(ctx, keyB); err != nil {
		t.Fatal(err)
	}
	tenantA, err := store.Authenticate(ctx, keyA)
	if err != nil {
		t.Fatal(err)
	}
	tenantB, err := store.Authenticate(ctx, keyB)
	if err != nil {
		t.Fatal(err)
	}

	role, err := store.CreateRole(ctx, tenantA, "HTTP profile reader", []string{"profiles:read", "schemas:read"})
	if err != nil {
		t.Fatal(err)
	}
	user, err := store.CreateUser(ctx, tenantA, domain.User{
		OIDCIssuer: "https://identity.http.test", OIDCSubject: "subject-" + suffix,
		Email: "http-operator@example.test", RoleIDs: []string{role.ID},
	})
	if err != nil || user.ID == "" {
		t.Fatalf("create OIDC user: %+v %v", user, err)
	}

	server := httptest.NewServer(NewWithOptions(store, 75, staticTokenVerifier{claimsByToken: map[string]domain.OIDCClaims{
		"tenant-a-oidc-token": {
			Issuer: "https://identity.http.test", Subject: "subject-" + suffix,
			TenantID: tenantA.TenantID, WorkspaceID: tenantA.WorkspaceID, AppID: tenantA.AppID,
		},
		"tenant-b-forged-oidc-token": {
			Issuer: "https://identity.http.test", Subject: "subject-" + suffix,
			TenantID: tenantB.TenantID, WorkspaceID: tenantB.WorkspaceID, AppID: tenantB.AppID,
		},
	}}, "https://app.test"))
	defer server.Close()

	externalID := "http-customer-" + suffix
	occurredAt := time.Now().UTC().Format(time.RFC3339Nano)
	body := `{"events":[{"event_type":"profile.updated","schema_version":1,"external_id":"` + externalID + `",` +
		`"idempotency_key":"` + externalID + `-profile","occurred_at":"` + occurredAt + `",` +
		`"payload":{"attributes":{"tenant":"a"}}}]}`
	response := request(t, server.URL, http.MethodPost, "/v1/events/batch", keyA, body)
	if response.status != http.StatusAccepted {
		t.Fatalf("accept status=%d body=%s", response.status, response.body)
	}
	var tenantAIngest struct {
		EventIDs []string `json:"event_ids"`
	}
	if err := json.Unmarshal([]byte(response.body), &tenantAIngest); err != nil || len(tenantAIngest.EventIDs) != 1 {
		t.Fatalf("tenant A ingest body=%s err=%v", response.body, err)
	}
	response = request(t, server.URL, http.MethodPost, "/v1/events/batch", keyA,
		strings.Replace(body, `"tenant":"a"`, `"tenant":"changed"`, 1))
	if response.status != http.StatusConflict {
		t.Fatalf("tenant A idempotency conflict status=%d body=%s", response.status, response.body)
	}
	response = request(t, server.URL, http.MethodPost, "/v1/events/batch", keyB, body)
	if response.status != http.StatusAccepted {
		t.Fatalf("tenant B same idempotency status=%d body=%s", response.status, response.body)
	}
	var tenantBIngest struct {
		EventIDs []string `json:"event_ids"`
	}
	if err := json.Unmarshal([]byte(response.body), &tenantBIngest); err != nil || len(tenantBIngest.EventIDs) != 1 {
		t.Fatalf("tenant B ingest body=%s err=%v", response.body, err)
	}
	if tenantAIngest.EventIDs[0] == tenantBIngest.EventIDs[0] {
		t.Fatalf("tenant-scoped idempotency returned same event ID across tenants: %s", tenantAIngest.EventIDs[0])
	}

	response = request(t, server.URL, http.MethodGet, "/v1/schemas", "tenant-a-oidc-token", "")
	if response.status != http.StatusOK {
		t.Fatalf("tenant A OIDC schemas status=%d body=%s", response.status, response.body)
	}
	response = request(t, server.URL, http.MethodGet, "/v1/schemas", "tenant-b-forged-oidc-token", "")
	if response.status != http.StatusUnauthorized {
		t.Fatalf("forged cross-tenant OIDC status=%d body=%s", response.status, response.body)
	}

	schemaBody := `{"event_type":"tenant.only","version":1,"compatibility":"none",` +
		`"schema":{"type":"object","required":["value"],"properties":{"value":{"type":"string"}}}}`
	response = request(t, server.URL, http.MethodPost, "/v1/schemas", keyA, schemaBody)
	if response.status != http.StatusCreated {
		t.Fatalf("create schema status=%d body=%s", response.status, response.body)
	}
	response = request(t, server.URL, http.MethodGet, "/v1/schemas", keyB, "")
	if response.status != http.StatusOK {
		t.Fatalf("tenant B list schemas status=%d body=%s", response.status, response.body)
	}
	var listed struct {
		Schemas []domain.EventSchema `json:"schemas"`
	}
	if err := json.Unmarshal([]byte(response.body), &listed); err != nil {
		t.Fatal(err)
	}
	for _, schema := range listed.Schemas {
		if schema.EventType == "tenant.only" {
			t.Fatalf("tenant B saw tenant A schema: %+v", listed.Schemas)
		}
	}
}

type httpResponse struct {
	status int
	body   string
}

func request(t *testing.T, baseURL, method, path, bearer, body string) httpResponse {
	t.Helper()
	var reader *strings.Reader
	if body == "" {
		reader = strings.NewReader("")
	} else {
		reader = strings.NewReader(body)
	}
	req, err := http.NewRequest(method, baseURL+path, reader)
	if err != nil {
		t.Fatal(err)
	}
	if bearer != "" {
		req.Header.Set("Authorization", "Bearer "+bearer)
	}
	if body != "" {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	payload, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatal(err)
	}
	return httpResponse{status: resp.StatusCode, body: string(payload)}
}
