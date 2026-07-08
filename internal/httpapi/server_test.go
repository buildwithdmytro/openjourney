package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/buildwithdmytro/openjourney/internal/domain"
	"github.com/buildwithdmytro/openjourney/internal/postgres"
)

type fakeStore struct {
	accepted      int
	scopes        []string
	oidcPrincipal *domain.Principal
	localSession  domain.AuthSession
	revokedToken  string
}

func (f *fakeStore) Ready(context.Context) error { return nil }
func (f *fakeStore) Authenticate(_ context.Context, key string) (domain.Principal, error) {
	if key != "test-key" {
		return domain.Principal{}, errors.New("unauthorized")
	}
	scopes := f.scopes
	if scopes == nil {
		scopes = []string{"events:write", "profiles:read"}
	}
	return domain.Principal{
		TenantID: "tenant", WorkspaceID: "workspace", AppID: "app",
		Scopes: scopes,
	}, nil
}
func (f *fakeStore) AuthenticateOIDC(context.Context, domain.OIDCClaims) (domain.Principal, error) {
	if f.oidcPrincipal != nil {
		return *f.oidcPrincipal, nil
	}
	return domain.Principal{}, errors.New("unauthorized")
}
func (f *fakeStore) CreateLocalSession(context.Context, string, string, time.Duration) (domain.AuthSession, error) {
	if f.localSession.AccessToken != "" {
		return f.localSession, nil
	}
	return domain.AuthSession{}, postgres.ErrUnauthorized
}
func (f *fakeStore) RevokeLocalSession(_ context.Context, token string) error {
	f.revokedToken = token
	return nil
}
func (f *fakeStore) AcceptEvents(_ context.Context, _ domain.Principal, events []domain.Event) ([]string, error) {
	f.accepted += len(events)
	return []string{"event-1"}, nil
}
func (f *fakeStore) GetProfile(context.Context, domain.Principal, string) (domain.Profile, []domain.Consent, error) {
	return domain.Profile{ID: "profile-1", Attributes: json.RawMessage(`{}`)}, nil, nil
}
func (f *fakeStore) ClaimProjectionJob(context.Context) (domain.AcceptedEvent, bool, error) {
	return domain.AcceptedEvent{}, false, nil
}
func (f *fakeStore) ProjectEvent(context.Context, domain.AcceptedEvent) error { return nil }
func (f *fakeStore) FailProjectionJob(context.Context, string, error) error   { return nil }
func (f *fakeStore) ValidateEventSchema(context.Context, domain.Principal, domain.Event) error {
	return nil
}
func (f *fakeStore) ListEventSchemas(context.Context, domain.Principal) ([]domain.EventSchema, error) {
	return nil, nil
}
func (f *fakeStore) CreateEventSchema(_ context.Context, _ domain.Principal, schema domain.EventSchema) (domain.EventSchema, error) {
	schema.ID = "schema-1"
	return schema, nil
}
func (f *fakeStore) ListAPIKeys(context.Context, domain.Principal) ([]domain.APIKey, error) {
	return nil, nil
}
func (f *fakeStore) CreateAPIKey(_ context.Context, _ domain.Principal, name string, scopes []string, _ *time.Time) (domain.APIKey, string, error) {
	return domain.APIKey{ID: "key-1", Name: name, Scopes: scopes}, "secret", nil
}
func (f *fakeStore) RevokeAPIKey(context.Context, domain.Principal, string) error { return nil }
func (f *fakeStore) CreatePrivacyRequest(_ context.Context, _ domain.Principal, externalID, kind string) (domain.PrivacyRequest, error) {
	return domain.PrivacyRequest{ID: "privacy-1", ExternalID: externalID, RequestType: kind}, nil
}
func (f *fakeStore) GetPrivacyRequest(context.Context, domain.Principal, string) (domain.PrivacyRequest, error) {
	return domain.PrivacyRequest{ID: "privacy-1"}, nil
}
func (f *fakeStore) QueueStatus(context.Context, domain.Principal) ([]domain.QueueStatus, error) {
	return nil, nil
}
func (f *fakeStore) ListDeadLetters(context.Context, domain.Principal, string, int) ([]domain.DeadLetterItem, error) {
	return []domain.DeadLetterItem{{Queue: "projection", ID: "event-1", Kind: "profile.updated"}}, nil
}
func (f *fakeStore) RetryDeadLetter(context.Context, domain.Principal, string, string) error {
	return nil
}
func (f *fakeStore) DiscardDeadLetter(context.Context, domain.Principal, string, string) error {
	return nil
}
func (f *fakeStore) ClaimOutboxEvent(context.Context) (domain.OutboxEvent, bool, error) {
	return domain.OutboxEvent{}, false, nil
}
func (f *fakeStore) CompleteOutboxEvent(context.Context, string) error    { return nil }
func (f *fakeStore) FailOutboxEvent(context.Context, string, error) error { return nil }
func (f *fakeStore) ClaimOperationJob(context.Context) (domain.OperationJob, bool, error) {
	return domain.OperationJob{}, false, nil
}
func (f *fakeStore) CompleteOperationJob(context.Context, string) error    { return nil }
func (f *fakeStore) FailOperationJob(context.Context, string, error) error { return nil }
func (f *fakeStore) ExportPrivacyData(context.Context, string) (domain.PrivacyData, error) {
	return domain.PrivacyData{}, nil
}
func (f *fakeStore) CompletePrivacyExport(context.Context, string, string) error { return nil }
func (f *fakeStore) DeletePrivacyData(context.Context, string) ([]string, error) { return nil, nil }
func (f *fakeStore) EnforceRetention(context.Context, string) (domain.RetentionReport, error) {
	return domain.RetentionReport{}, nil
}
func (f *fakeStore) VerifyReplay(context.Context, domain.Principal) (domain.ReplayReport, error) {
	return domain.ReplayReport{Match: true}, nil
}
func (f *fakeStore) ListRoles(context.Context, domain.Principal) ([]domain.Role, error) {
	return nil, nil
}
func (f *fakeStore) CreateRole(_ context.Context, _ domain.Principal, name string, permissions []string) (domain.Role, error) {
	return domain.Role{ID: "role-1", Name: name, Permissions: permissions}, nil
}
func (f *fakeStore) ListUsers(context.Context, domain.Principal) ([]domain.User, error) {
	return nil, nil
}
func (f *fakeStore) CreateUser(_ context.Context, _ domain.Principal, user domain.User) (domain.User, error) {
	user.ID = "user-1"
	return user, nil
}
func (f *fakeStore) ListAuditEvents(context.Context, domain.Principal, int) ([]domain.AuditEvent, error) {
	return nil, nil
}
func (f *fakeStore) CreateSegment(_ context.Context, _ domain.Principal, seg domain.Segment) (domain.Segment, error) {
	seg.ID = "segment-1"
	return seg, nil
}
func (f *fakeStore) GetSegment(_ context.Context, _ domain.Principal, id string) (domain.Segment, error) {
	return domain.Segment{ID: id, Name: "Test Segment", DSL: json.RawMessage(`{}`)}, nil
}
func (f *fakeStore) UpdateSegment(_ context.Context, _ domain.Principal, seg domain.Segment) (domain.Segment, error) {
	return seg, nil
}
func (f *fakeStore) ListSegments(_ context.Context, _ domain.Principal) ([]domain.Segment, error) {
	return []domain.Segment{{ID: "segment-1", Name: "Test Segment", DSL: json.RawMessage(`{}`)}}, nil
}
func (f *fakeStore) SetSegmentMembers(_ context.Context, _ domain.Principal, _ string, _ []domain.SegmentMember) error {
	return nil
}
func (f *fakeStore) PreviewSegment(_ context.Context, _ domain.Principal, _ string) (int, map[string]int, error) {
	return 42, map[string]int{"profile_attributes": 30, "consent": 10, "event_history": 2}, nil
}
func (f *fakeStore) ResolveSegment(_ context.Context, _ domain.Principal, _ string) ([]string, error) {
	return []string{"profile-1", "profile-2"}, nil
}
func (f *fakeStore) CreateSendingIdentity(_ context.Context, _ domain.Principal, iden domain.SendingIdentity) (domain.SendingIdentity, error) {
	iden.ID = "iden-1"
	return iden, nil
}
func (f *fakeStore) GetSendingIdentity(_ context.Context, _ domain.Principal, id string) (domain.SendingIdentity, error) {
	return domain.SendingIdentity{ID: id, Channel: "email"}, nil
}
func (f *fakeStore) ListSendingIdentities(_ context.Context, _ domain.Principal) ([]domain.SendingIdentity, error) {
	return []domain.SendingIdentity{{ID: "iden-1", Channel: "email"}}, nil
}
func (f *fakeStore) CreateTemplate(_ context.Context, _ domain.Principal, tmpl domain.Template) (domain.Template, error) {
	tmpl.ID = "tmpl-1"
	return tmpl, nil
}
func (f *fakeStore) GetTemplate(_ context.Context, _ domain.Principal, id string) (domain.Template, error) {
	return domain.Template{ID: id, Name: "Test Template", Channel: "email"}, nil
}
func (f *fakeStore) UpdateTemplate(_ context.Context, _ domain.Principal, tmpl domain.Template) (domain.Template, error) {
	return tmpl, nil
}
func (f *fakeStore) ListTemplates(_ context.Context, _ domain.Principal) ([]domain.Template, error) {
	return []domain.Template{{ID: "tmpl-1", Name: "Test Template", Channel: "email"}}, nil
}
func (f *fakeStore) UpsertTrackedLink(_ context.Context, _ string, _ string, _ string) (string, error) {
	return "link-123", nil
}
func (f *fakeStore) GetProfileByID(_ context.Context, _, _, _ string) (domain.Profile, error) {
	return domain.Profile{ID: "profile-1", ExternalID: "user-1", Attributes: json.RawMessage(`{}`)}, nil
}
func (f *fakeStore) IsSuppressed(ctx context.Context, p domain.Principal, channel, endpoint string) (bool, error) {
	return false, nil
}
func (f *fakeStore) SuppressEndpoint(ctx context.Context, p domain.Principal, channel, endpoint, reason string) error {
	return nil
}
func (f *fakeStore) RemoveSuppression(ctx context.Context, p domain.Principal, channel, endpoint string) error {
	return nil
}
func (f *fakeStore) ListSuppressions(ctx context.Context, p domain.Principal) ([]domain.Suppression, error) {
	return []domain.Suppression{}, nil
}
func (f *fakeStore) LatestConsent(ctx context.Context, p domain.Principal, profileID, channel, topic string) (domain.Consent, error) {
	return domain.Consent{ProfileID: profileID, Channel: channel, Topic: topic, State: "subscribed"}, nil
}
func (f *fakeStore) SentCountSince(ctx context.Context, p domain.Principal, profileID string, since time.Time) (int, error) {
	return 0, nil
}

func (f *fakeStore) CreateCampaign(ctx context.Context, p domain.Principal, c domain.Campaign) (domain.Campaign, error) {
	c.ID = "campaign-1"
	return c, nil
}
func (f *fakeStore) GetCampaign(ctx context.Context, p domain.Principal, id string) (domain.Campaign, error) {
	return domain.Campaign{ID: id, Name: "Test Campaign", Status: "draft"}, nil
}
func (f *fakeStore) UpdateCampaign(ctx context.Context, p domain.Principal, c domain.Campaign) (domain.Campaign, error) {
	return c, nil
}
func (f *fakeStore) ListCampaigns(ctx context.Context, p domain.Principal) ([]domain.Campaign, error) {
	return []domain.Campaign{{ID: "campaign-1", Name: "Test Campaign", Status: "draft"}}, nil
}
func (f *fakeStore) GetCampaignSystem(ctx context.Context, tenantID, id string) (domain.Campaign, error) {
	return domain.Campaign{ID: id, TenantID: tenantID, WorkspaceID: "workspace", Status: "sending"}, nil
}
func (f *fakeStore) ClaimScheduledCampaign(ctx context.Context) (domain.Campaign, bool, error) {
	return domain.Campaign{}, false, nil
}
func (f *fakeStore) SaveCampaignManifestAndJobs(ctx context.Context, campaignID string, manifestKey string, recipientCount int, segmentVersion int, templateVersion int, jobs []domain.DeliveryJob) error {
	return nil
}
func (f *fakeStore) ClaimDeliveryJob(ctx context.Context, workerID string) (domain.DeliveryJob, bool, error) {
	return domain.DeliveryJob{}, false, nil
}
func (f *fakeStore) CompleteDeliveryJob(ctx context.Context, jobID string) error {
	return nil
}
func (f *fakeStore) FailDeliveryJob(ctx context.Context, jobID string, errMsg string) error {
	return nil
}
func (f *fakeStore) CreateDeliveryAttempt(ctx context.Context, attempt domain.DeliveryAttempt) (bool, error) {
	return true, nil
}
func (f *fakeStore) UpdateDeliveryAttempt(ctx context.Context, campaignID, profileID, channel, decision, reason, providerMsgID string, policySnapshot []byte) error {
	return nil
}
func (f *fakeStore) DeleteDeliveryAttempt(ctx context.Context, tenantID, campaignID, profileID, channel string) error {
	return nil
}
func (f *fakeStore) GetProfileEmails(ctx context.Context, tenantID string, profileIDs []string) (map[string]string, error) {
	return map[string]string{}, nil
}
func (f *fakeStore) GetFirstAppID(ctx context.Context, tenantID, workspaceID string) (string, error) {
	return "app-1", nil
}
func TestAcceptEvents(t *testing.T) {
	store := &fakeStore{}
	server := New(store, 75)
	body := `{"events":[{"event_type":"profile.updated","schema_version":1,"external_id":"u1",
		"idempotency_key":"k1","occurred_at":"2025-01-01T00:00:00Z","payload":{"attributes":{"name":"Ada"}}}]}`
	request := httptest.NewRequest(http.MethodPost, "/v1/events/batch", strings.NewReader(body))
	request.Header.Set("Authorization", "Bearer test-key")
	response := httptest.NewRecorder()
	server.ServeHTTP(response, request)
	if response.Code != http.StatusAccepted {
		t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
	}
	if store.accepted != 1 {
		t.Fatalf("accepted=%d", store.accepted)
	}
}

func TestAcceptEventsRequiresAuthentication(t *testing.T) {
	server := New(&fakeStore{}, 75)
	request := httptest.NewRequest(http.MethodPost, "/v1/events/batch", strings.NewReader(`{"events":[]}`))
	response := httptest.NewRecorder()
	server.ServeHTTP(response, request)
	if response.Code != http.StatusUnauthorized {
		t.Fatalf("status=%d", response.Code)
	}
}

func TestAcceptEventsRequiresScope(t *testing.T) {
	server := New(&fakeStore{scopes: []string{"profiles:read"}}, 75)
	request := httptest.NewRequest(http.MethodPost, "/v1/events/batch", strings.NewReader(`{"events":[]}`))
	request.Header.Set("Authorization", "Bearer test-key")
	response := httptest.NewRecorder()
	server.ServeHTTP(response, request)
	if response.Code != http.StatusForbidden {
		t.Fatalf("status=%d", response.Code)
	}
}

func TestLocalLoginReturnsSession(t *testing.T) {
	expiresAt := time.Now().Add(time.Hour).UTC()
	server := New(&fakeStore{localSession: domain.AuthSession{
		AccessToken: "session-token", TokenType: "Bearer", ExpiresAt: expiresAt,
	}}, 75)
	request := httptest.NewRequest(http.MethodPost, "/v1/auth/login",
		strings.NewReader(`{"email":"admin@example.test","password":"correct horse battery staple"}`))
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()
	server.ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
	}
	if !strings.Contains(response.Body.String(), "session-token") {
		t.Fatalf("unexpected body=%s", response.Body.String())
	}
}

func TestLocalLoginRejectsInvalidCredentials(t *testing.T) {
	server := New(&fakeStore{}, 75)
	request := httptest.NewRequest(http.MethodPost, "/v1/auth/login",
		strings.NewReader(`{"email":"admin@example.test","password":"wrong"}`))
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()
	server.ServeHTTP(response, request)
	if response.Code != http.StatusUnauthorized {
		t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
	}
}

func TestLocalLogoutRevokesSession(t *testing.T) {
	store := &fakeStore{}
	server := New(store, 75)
	request := httptest.NewRequest(http.MethodPost, "/v1/auth/logout", nil)
	request.Header.Set("Authorization", "Bearer ojs_session")
	response := httptest.NewRecorder()
	server.ServeHTTP(response, request)
	if response.Code != http.StatusNoContent {
		t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
	}
	if store.revokedToken != "ojs_session" {
		t.Fatalf("revoked token=%q", store.revokedToken)
	}
}

type fakeTokenVerifier struct{}

func (fakeTokenVerifier) Verify(context.Context, string) (domain.OIDCClaims, error) {
	return domain.OIDCClaims{Issuer: "issuer", Subject: "subject"}, nil
}

func TestOIDCFallbackUsesRoleScopes(t *testing.T) {
	principal := domain.Principal{TenantID: "tenant", WorkspaceID: "workspace", AppID: "app",
		UserID: "user", ActorType: "user", Scopes: []string{"profiles:read"}}
	server := NewWithOptions(&fakeStore{oidcPrincipal: &principal}, 75, fakeTokenVerifier{}, "https://app.test")
	request := httptest.NewRequest(http.MethodGet, "/v1/profiles/customer", nil)
	request.Header.Set("Authorization", "Bearer signed.jwt.token")
	response := httptest.NewRecorder()
	server.ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
	}
}

func TestListDeadLettersRequiresOperationsRead(t *testing.T) {
	server := New(&fakeStore{scopes: []string{"operations:read"}}, 75)
	request := httptest.NewRequest(http.MethodGet, "/v1/operations/dlq?queue=projection", nil)
	request.Header.Set("Authorization", "Bearer test-key")
	response := httptest.NewRecorder()
	server.ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
	}
	if !strings.Contains(response.Body.String(), "event-1") {
		t.Fatalf("unexpected body=%s", response.Body.String())
	}
}

func TestRetryDeadLetterRequiresOperationsWrite(t *testing.T) {
	server := New(&fakeStore{scopes: []string{"operations:read"}}, 75)
	request := httptest.NewRequest(http.MethodPost, "/v1/operations/dlq/projection/event-1/retry", nil)
	request.Header.Set("Authorization", "Bearer test-key")
	response := httptest.NewRecorder()
	server.ServeHTTP(response, request)
	if response.Code != http.StatusForbidden {
		t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
	}

	server = New(&fakeStore{scopes: []string{"operations:write"}}, 75)
	response = httptest.NewRecorder()
	server.ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
	}
}

func TestCORSAllowsOnlyConfiguredOrigin(t *testing.T) {
	server := NewWithOptions(&fakeStore{}, 75, nil, "https://app.test")
	allowed := httptest.NewRequest(http.MethodOptions, "/v1/events/batch", nil)
	allowed.Header.Set("Origin", "https://app.test")
	allowedResponse := httptest.NewRecorder()
	server.ServeHTTP(allowedResponse, allowed)
	if allowedResponse.Header().Get("Access-Control-Allow-Origin") != "https://app.test" {
		t.Fatal("configured origin was not allowed")
	}
	denied := httptest.NewRequest(http.MethodOptions, "/v1/events/batch", nil)
	denied.Header.Set("Origin", "https://attacker.test")
	deniedResponse := httptest.NewRecorder()
	server.ServeHTTP(deniedResponse, denied)
	if deniedResponse.Header().Get("Access-Control-Allow-Origin") != "" {
		t.Fatal("unconfigured origin was allowed")
	}
}

func TestSegmentsEndpoints(t *testing.T) {
	server := New(&fakeStore{scopes: []string{"segments:read", "segments:write"}}, 75)

	// Create
	createReq := httptest.NewRequest(http.MethodPost, "/v1/segments", strings.NewReader(`{"name":"New Seg","type":"dynamic"}`))
	createReq.Header.Set("Authorization", "Bearer test-key")
	createRes := httptest.NewRecorder()
	server.ServeHTTP(createRes, createReq)
	if createRes.Code != http.StatusCreated {
		t.Fatalf("status=%d body=%s", createRes.Code, createRes.Body.String())
	}

	// List
	listReq := httptest.NewRequest(http.MethodGet, "/v1/segments", nil)
	listReq.Header.Set("Authorization", "Bearer test-key")
	listRes := httptest.NewRecorder()
	server.ServeHTTP(listRes, listReq)
	if listRes.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", listRes.Code, listRes.Body.String())
	}
	if !strings.Contains(listRes.Body.String(), "Test Segment") {
		t.Fatalf("expected Test Segment in body=%s", listRes.Body.String())
	}
	var listBody struct {
		Segments []domain.Segment `json:"segments"`
	}
	if err := json.Unmarshal(listRes.Body.Bytes(), &listBody); err != nil {
		t.Fatalf("decode list body: %v", err)
	}
	if len(listBody.Segments) != 1 || listBody.Segments[0].ID != "segment-1" {
		t.Fatalf("unexpected list body=%s", listRes.Body.String())
	}

	// Get
	getReq := httptest.NewRequest(http.MethodGet, "/v1/segments/segment-1", nil)
	getReq.Header.Set("Authorization", "Bearer test-key")
	getRes := httptest.NewRecorder()
	server.ServeHTTP(getRes, getReq)
	if getRes.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", getRes.Code, getRes.Body.String())
	}

	// Update
	updateReq := httptest.NewRequest(http.MethodPut, "/v1/segments/segment-1", strings.NewReader(`{"name":"Updated Seg"}`))
	updateReq.Header.Set("Authorization", "Bearer test-key")
	updateRes := httptest.NewRecorder()
	server.ServeHTTP(updateRes, updateReq)
	if updateRes.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", updateRes.Code, updateRes.Body.String())
	}

	// Set Members
	membersReq := httptest.NewRequest(http.MethodPut, "/v1/segments/segment-1/members", strings.NewReader(`[{"profile_id":"p-1","membership":"include"}]`))
	membersReq.Header.Set("Authorization", "Bearer test-key")
	membersRes := httptest.NewRecorder()
	server.ServeHTTP(membersRes, membersReq)
	if membersRes.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", membersRes.Code, membersRes.Body.String())
	}

	// Preview
	previewReq := httptest.NewRequest(http.MethodPost, "/v1/segments/segment-1/preview", nil)
	previewReq.Header.Set("Authorization", "Bearer test-key")
	previewRes := httptest.NewRecorder()
	server.ServeHTTP(previewRes, previewReq)
	if previewRes.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", previewRes.Code, previewRes.Body.String())
	}
	if !strings.Contains(previewRes.Body.String(), `"count":42`) {
		t.Fatalf("unexpected preview body=%s", previewRes.Body.String())
	}
}

func TestTemplateListEndpointsUseResponseEnvelopes(t *testing.T) {
	server := New(&fakeStore{scopes: []string{"templates:read"}}, 75)

	templatesReq := httptest.NewRequest(http.MethodGet, "/v1/templates", nil)
	templatesReq.Header.Set("Authorization", "Bearer test-key")
	templatesRes := httptest.NewRecorder()
	server.ServeHTTP(templatesRes, templatesReq)
	if templatesRes.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", templatesRes.Code, templatesRes.Body.String())
	}
	var templatesBody struct {
		Templates []domain.Template `json:"templates"`
	}
	if err := json.Unmarshal(templatesRes.Body.Bytes(), &templatesBody); err != nil {
		t.Fatalf("decode templates body: %v", err)
	}
	if len(templatesBody.Templates) != 1 || templatesBody.Templates[0].ID != "tmpl-1" {
		t.Fatalf("unexpected templates body=%s", templatesRes.Body.String())
	}

	identitiesReq := httptest.NewRequest(http.MethodGet, "/v1/sending-identities", nil)
	identitiesReq.Header.Set("Authorization", "Bearer test-key")
	identitiesRes := httptest.NewRecorder()
	server.ServeHTTP(identitiesRes, identitiesReq)
	if identitiesRes.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", identitiesRes.Code, identitiesRes.Body.String())
	}
	var identitiesBody struct {
		Identities []domain.SendingIdentity `json:"identities"`
	}
	if err := json.Unmarshal(identitiesRes.Body.Bytes(), &identitiesBody); err != nil {
		t.Fatalf("decode identities body: %v", err)
	}
	if len(identitiesBody.Identities) != 1 || identitiesBody.Identities[0].ID != "iden-1" {
		t.Fatalf("unexpected identities body=%s", identitiesRes.Body.String())
	}
}
