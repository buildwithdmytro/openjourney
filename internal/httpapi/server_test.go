package httpapi

import (
	"context"
	"crypto/hmac"
	"crypto/sha1"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/buildwithdmytro/openjourney/internal/domain"
	"github.com/buildwithdmytro/openjourney/internal/ports"
	"github.com/buildwithdmytro/openjourney/internal/postgres"
)

type fakeStore struct {
	ports.Store
	accepted                               int
	scopes                                 []string
	oidcPrincipal                          *domain.Principal
	localSession                           domain.AuthSession
	revokedToken                           string
	published                              int
	rollouts                               int
	optimizationApprovals                  int
	runs                                   []domain.JourneyRun
	getTemplateFunc                        func(id string) (domain.Template, error)
	getProfileFunc                         func(externalID string) (domain.Profile, error)
	getProfileByPhoneFunc                  func(tenantID, phone string) (domain.Profile, error)
	getSendingIdentityFunc                 func(id string) (domain.SendingIdentity, error)
	getSendingIdentityByProviderConfigFunc func(provider, configKey, configVal string) (domain.SendingIdentity, error)
	AcceptEventsFunc                       func(ctx context.Context, p domain.Principal, events []domain.Event) ([]string, error)
	registerDeviceTokenFunc                func(ctx context.Context, tenantID, workspaceID, appID, profileID, platform, provider, token string) (domain.DeviceToken, error)
	retireDeviceTokenFunc                  func(ctx context.Context, tenantID, appID, token string) error
	retireDeviceTokenByIDFunc              func(ctx context.Context, tenantID, id string) error
	listActiveDeviceTokensFunc             func(ctx context.Context, tenantID, workspaceID, profileID string) ([]domain.DeviceToken, error)
	listDeviceTokensByProfileFunc          func(ctx context.Context, tenantID, workspaceID, profileID string) ([]domain.DeviceToken, error)
	listAIActivityFunc                     func(ctx context.Context, p domain.Principal, limit int) ([]domain.AIActivity, error)
	listExtensionActivitiesFunc            func(ctx context.Context, p domain.Principal, extensionID string, limit int) ([]domain.ExtensionActivity, error)
	getExtensionHealthFunc                 func(ctx context.Context, p domain.Principal, extensionID string) (domain.ExtensionHealth, error)
}

type fakeBlobStore struct {
	objects map[string][]byte
}

func (f *fakeBlobStore) Put(_ context.Context, key string, data []byte, _ string) error {
	if f.objects == nil {
		f.objects = map[string][]byte{}
	}
	f.objects[key] = data
	return nil
}

func (f *fakeBlobStore) Get(_ context.Context, key string) ([]byte, error) {
	if f.objects == nil {
		return nil, postgres.ErrNotFound
	}
	data, ok := f.objects[key]
	if !ok {
		return nil, postgres.ErrNotFound
	}
	return data, nil
}

func (f *fakeBlobStore) Delete(_ context.Context, key string) error {
	delete(f.objects, key)
	return nil
}

func stringPtr(value string) *string {
	return &value
}

func (f *fakeStore) Ready(context.Context) error { return nil }
func (f *fakeStore) Authenticate(_ context.Context, key string) (domain.Principal, error) {
	if key != "test-key" && key != "api-key-actor" && key != "ai-agent-actor" {
		return domain.Principal{}, errors.New("unauthorized")
	}
	scopes := f.scopes
	if scopes == nil {
		scopes = []string{"events:write", "profiles:read"}
	}
	principal := domain.Principal{
		TenantID: "tenant", WorkspaceID: "workspace", AppID: "app",
		UserID: "00000000-0000-0000-0000-000000000001", ActorType: "user", Scopes: scopes,
	}
	if key == "api-key-actor" {
		principal.UserID = ""
		principal.ActorType = "api_key"
	} else if key == "ai-agent-actor" {
		principal.UserID = ""
		principal.ActorType = "ai_agent"
	}
	return principal, nil
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
func (f *fakeStore) AcceptEvents(ctx context.Context, p domain.Principal, events []domain.Event) ([]string, error) {
	if f.AcceptEventsFunc != nil {
		return f.AcceptEventsFunc(ctx, p, events)
	}
	f.accepted += len(events)
	return []string{"event-1"}, nil
}
func (f *fakeStore) GetProfile(_ context.Context, _ domain.Principal, externalID string) (domain.Profile, []domain.Consent, error) {
	if f.getProfileFunc != nil {
		prof, err := f.getProfileFunc(externalID)
		return prof, nil, err
	}
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
func (f *fakeStore) CreateAIGenerationRequest(_ context.Context, _ domain.Principal, taskType string, _ json.RawMessage) (domain.AIGenerationRequest, error) {
	return domain.AIGenerationRequest{ID: "generation-1", TaskType: taskType, Status: "pending"}, nil
}
func (f *fakeStore) GetAIGenerationRequest(context.Context, domain.Principal, string) (domain.AIGenerationRequest, error) {
	return domain.AIGenerationRequest{ID: "generation-1", TaskType: "content_draft", Status: "pending"}, nil
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
func (f *fakeStore) EnforceRetention(context.Context, string) (domain.DataRetentionReport, error) {
	return domain.DataRetentionReport{}, nil
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
func (f *fakeStore) ListAIActivity(ctx context.Context, p domain.Principal, limit int) ([]domain.AIActivity, error) {
	if f.listAIActivityFunc != nil {
		return f.listAIActivityFunc(ctx, p, limit)
	}
	return nil, nil
}
func (f *fakeStore) ListExtensionActivities(ctx context.Context, p domain.Principal, extensionID string, limit int) ([]domain.ExtensionActivity, error) {
	if f.listExtensionActivitiesFunc != nil {
		return f.listExtensionActivitiesFunc(ctx, p, extensionID, limit)
	}
	return nil, nil
}
func (f *fakeStore) GetExtensionHealth(ctx context.Context, p domain.Principal, extensionID string) (domain.ExtensionHealth, error) {
	if f.getExtensionHealthFunc != nil {
		return f.getExtensionHealthFunc(ctx, p, extensionID)
	}
	return domain.ExtensionHealth{ExtensionID: extensionID, TenantID: p.TenantID, State: "closed"}, nil
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
	if f.getSendingIdentityFunc != nil {
		return f.getSendingIdentityFunc(id)
	}
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
	if f.getTemplateFunc != nil {
		return f.getTemplateFunc(id)
	}
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
func (f *fakeStore) GetProfileByIDSystem(_ context.Context, _, _, _ string) (domain.Profile, error) {
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
func (f *fakeStore) GetTenantFatigueQuotas(ctx context.Context, p domain.Principal) (int, int, error) {
	return 5, 20, nil
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
func (f *fakeStore) CreateJourney(ctx context.Context, p domain.Principal, j domain.Journey) (domain.Journey, error) {
	j.ID = "journey-1"
	return j, nil
}
func (f *fakeStore) GetJourney(ctx context.Context, p domain.Principal, id string) (domain.Journey, error) {
	if id == "not-found-journey" {
		return domain.Journey{}, postgres.ErrNotFound
	}
	if id == "invalid-journey" {
		return domain.Journey{ID: id, TenantID: p.TenantID, WorkspaceID: p.WorkspaceID, Name: "Invalid Journey", Status: "draft", Graph: json.RawMessage(`{"entry_node_id":"n1","nodes":[],"edges":[]}`)}, nil
	}
	versionID := "version-1"
	return domain.Journey{ID: id, TenantID: p.TenantID, WorkspaceID: p.WorkspaceID, Name: "Test Journey", Status: "draft", CurrentVersionID: &versionID, Graph: json.RawMessage(`{"entry_node_id":"n1","nodes":[{"id":"n1","type":"entry","config":{"trigger":"event","event_type":"signup.completed"}},{"id":"n2","type":"exit","config":{"reason":"completed"}}],"edges":[{"from":"n1","to":"n2"}]}`)}, nil
}
func (f *fakeStore) GetJourneyVersion(ctx context.Context, tenantID, versionID string) (domain.JourneyVersion, error) {
	return domain.JourneyVersion{
		ID: versionID, TenantID: tenantID, Version: 1,
		Graph: json.RawMessage(`{"entry_node_id":"n1","nodes":[{"id":"n1","type":"entry","config":{"trigger":"event","event_type":"signup.completed"}}],"edges":[]}`),
	}, nil
}
func (f *fakeStore) GetJourneyVersionNumber(ctx context.Context, p domain.Principal, journeyID string, version int) (domain.JourneyVersion, error) {
	return domain.JourneyVersion{
		ID: "version-1", JourneyID: journeyID, TenantID: p.TenantID, WorkspaceID: p.WorkspaceID, Version: version,
		Graph: json.RawMessage(`{"entry_node_id":"n1","nodes":[{"id":"n1","type":"entry","config":{"trigger":"event","event_type":"signup.completed"}}],"edges":[]}`),
	}, nil
}
func (f *fakeStore) GetJourneyRunsForProfile(ctx context.Context, tenantID, versionID, profileID string) ([]domain.JourneyRun, error) {
	var out []domain.JourneyRun
	for _, r := range f.runs {
		if r.TenantID == tenantID && r.JourneyVersionID == versionID && r.ProfileID == profileID {
			out = append(out, r)
		}
	}
	return out, nil
}
func (f *fakeStore) GetJourneyRuns(ctx context.Context, p domain.Principal, journeyID string) ([]domain.JourneyRun, error) {
	var out []domain.JourneyRun
	for _, r := range f.runs {
		if r.JourneyID == journeyID && r.TenantID == p.TenantID {
			out = append(out, r)
		}
	}
	return out, nil
}
func (f *fakeStore) GetJourneyTransitions(ctx context.Context, p domain.Principal, runID string) ([]domain.JourneyTransition, error) {
	return []domain.JourneyTransition{}, nil
}
func (f *fakeStore) CreateJourneyRun(ctx context.Context, run domain.JourneyRun) (bool, error) {
	run.ID = "run-" + run.ProfileID
	f.runs = append(f.runs, run)
	return true, nil
}

func (f *fakeStore) EnrollJourneyRun(ctx context.Context, run domain.JourneyRun, step domain.JourneyStep) (string, bool, error) {
	inserted, err := f.CreateJourneyRun(ctx, run)
	return run.ID, inserted, err
}
func (f *fakeStore) InsertJourneyStep(ctx context.Context, step domain.JourneyStep) error {
	return nil
}
func (f *fakeStore) RescheduleJourneyStep(ctx context.Context, stepID string, availableAt time.Time) error {
	return nil
}
func (f *fakeStore) UpdateJourney(ctx context.Context, p domain.Principal, j domain.Journey) (domain.Journey, error) {
	return j, nil
}
func (f *fakeStore) ListJourneys(ctx context.Context, p domain.Principal) ([]domain.Journey, error) {
	return []domain.Journey{{ID: "journey-1", Name: "Test Journey", Status: "draft", CurrentVersionID: ptrString("version-1"), Graph: json.RawMessage(`{}`)}}, nil
}
func (f *fakeStore) PublishJourney(ctx context.Context, p domain.Principal, journeyID string, approverUserID string, manifestKey string) (domain.JourneyVersion, error) {
	f.published++
	return domain.JourneyVersion{
		ID: "version-1", JourneyID: journeyID, TenantID: p.TenantID, WorkspaceID: p.WorkspaceID,
		Version: 1, Graph: json.RawMessage(`{"entry_node_id":"n1"}`), ManifestKey: &manifestKey,
		EntryKind: "event", EntryEventType: stringPtr("signup.completed"), ReentryPolicy: "once",
		MaxReentries: 0, LatePolicy: "run", Status: "active", PublishedBy: &approverUserID,
		PublishedAt: time.Now().UTC(),
	}, nil
}
func (f *fakeStore) RolloutExperiment(_ context.Context, p domain.Principal, experimentID string) (domain.ExperimentRollout, error) {
	f.rollouts++
	return domain.ExperimentRollout{
		ExperimentID: experimentID, WinnerVariant: "treatment", SubjectType: "journey",
		JourneyVersion: &domain.JourneyVersion{ID: "version-2", JourneyID: "journey-1", TenantID: p.TenantID, WorkspaceID: p.WorkspaceID, Version: 2},
	}, nil
}
func (f *fakeStore) ApproveExperimentOptimization(_ context.Context, p domain.Principal, experimentID, proposalID string) (domain.ExperimentVersion, error) {
	f.optimizationApprovals++
	return domain.ExperimentVersion{ID: "experiment-version-2", ExperimentID: experimentID, Version: 2, Seed: "fixed-seed", HoldoutPct: 10, ApprovedBy: p.UserID}, nil
}
func (f *fakeStore) SetJourneyVersionStatus(ctx context.Context, p domain.Principal, journeyID string, version int, status string) error {
	if journeyID == "invalid-journey" {
		return postgres.ErrNotFound
	}
	return nil
}
func (f *fakeStore) CancelJourneyRun(ctx context.Context, p domain.Principal, journeyID string, runID string) error {
	if runID == "invalid-run" {
		return postgres.ErrNotFound
	}
	return nil
}
func (f *fakeStore) GetJourneyDLQ(ctx context.Context, p domain.Principal) ([]domain.JourneyStep, []domain.JourneyMessageIntent, error) {
	return []domain.JourneyStep{{ID: "step-1", RunID: "run-1", Status: "dead"}}, []domain.JourneyMessageIntent{{ID: "intent-1", RunID: "run-2", Status: "dead"}}, nil
}
func (f *fakeStore) RetryJourneyStep(ctx context.Context, p domain.Principal, stepID string) error {
	if stepID == "invalid-step" {
		return postgres.ErrNotFound
	}
	return nil
}
func (f *fakeStore) RetryJourneyMessageIntent(ctx context.Context, p domain.Principal, intentID string) error {
	if intentID == "invalid-intent" {
		return postgres.ErrNotFound
	}
	return nil
}
func (f *fakeStore) GetCampaignSystem(ctx context.Context, tenantID, id string) (domain.Campaign, error) {
	return domain.Campaign{ID: id, TenantID: tenantID, WorkspaceID: "workspace", Status: "sending"}, nil
}
func (f *fakeStore) ClaimScheduledCampaign(ctx context.Context) (domain.Campaign, bool, error) {
	return domain.Campaign{}, false, nil
}
func (f *fakeStore) SaveCampaignManifestAndJobs(ctx context.Context, campaignID string, manifestKey string, recipientCount int, segmentVersion int, templateVersion int, conversionGoal json.RawMessage, attributionWindow string, jobs []domain.DeliveryJob) error {
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
func (f *fakeStore) UpdateDeliveryAttempt(ctx context.Context, campaignID, profileID, channel, endpoint, decision, reason, providerMsgID string, policySnapshot []byte) error {
	return nil
}
func (f *fakeStore) DeleteDeliveryAttempt(ctx context.Context, tenantID, campaignID, profileID, channel, endpoint string) error {
	return nil
}
func (f *fakeStore) GetDeliveryAttempt(ctx context.Context, campaignID, profileID, channel, endpoint string) (domain.DeliveryAttempt, error) {
	return domain.DeliveryAttempt{}, nil
}
func (f *fakeStore) GetProfileEmails(ctx context.Context, tenantID string, profileIDs []string) (map[string]string, error) {
	return map[string]string{}, nil
}
func (f *fakeStore) GetProfilePhones(ctx context.Context, tenantID string, profileIDs []string) (map[string]string, error) {
	return map[string]string{}, nil
}
func (f *fakeStore) GetProfileByPhone(ctx context.Context, tenantID string, phone string) (domain.Profile, error) {
	if f.getProfileByPhoneFunc != nil {
		return f.getProfileByPhoneFunc(tenantID, phone)
	}
	return domain.Profile{}, nil
}
func (f *fakeStore) RegisterDeviceToken(ctx context.Context, tenantID, workspaceID, appID, profileID, platform, provider, token string) (domain.DeviceToken, error) {
	if f.registerDeviceTokenFunc != nil {
		return f.registerDeviceTokenFunc(ctx, tenantID, workspaceID, appID, profileID, platform, provider, token)
	}
	return domain.DeviceToken{}, nil
}
func (f *fakeStore) RetireDeviceToken(ctx context.Context, tenantID, appID, token string) error {
	if f.retireDeviceTokenFunc != nil {
		return f.retireDeviceTokenFunc(ctx, tenantID, appID, token)
	}
	return nil
}
func (f *fakeStore) RetireDeviceTokenByID(ctx context.Context, tenantID, id string) error {
	if f.retireDeviceTokenByIDFunc != nil {
		return f.retireDeviceTokenByIDFunc(ctx, tenantID, id)
	}
	return nil
}
func (f *fakeStore) ListActiveDeviceTokens(ctx context.Context, tenantID, workspaceID, profileID string) ([]domain.DeviceToken, error) {
	if f.listActiveDeviceTokensFunc != nil {
		return f.listActiveDeviceTokensFunc(ctx, tenantID, workspaceID, profileID)
	}
	return nil, nil
}
func (f *fakeStore) ListDeviceTokensByProfile(ctx context.Context, tenantID, workspaceID, profileID string) ([]domain.DeviceToken, error) {
	if f.listDeviceTokensByProfileFunc != nil {
		return f.listDeviceTokensByProfileFunc(ctx, tenantID, workspaceID, profileID)
	}
	return nil, nil
}
func (f *fakeStore) GetSendingIdentityByProviderConfig(ctx context.Context, provider string, configKey string, configVal string) (domain.SendingIdentity, error) {
	if f.getSendingIdentityByProviderConfigFunc != nil {
		return f.getSendingIdentityByProviderConfigFunc(provider, configKey, configVal)
	}
	return domain.SendingIdentity{}, nil
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

func TestListAIActivityRequiresScopeAndIsTenantScoped(t *testing.T) {
	var gotPrincipal domain.Principal
	store := &fakeStore{
		scopes: []string{"ai:read"},
		listAIActivityFunc: func(_ context.Context, p domain.Principal, limit int) ([]domain.AIActivity, error) {
			gotPrincipal = p
			if limit != 25 {
				t.Fatalf("limit=%d, want 25", limit)
			}
			return []domain.AIActivity{{ID: "activity-a", TenantID: p.TenantID, WorkspaceID: p.WorkspaceID}}, nil
		},
	}
	server := New(store, 75)
	request := httptest.NewRequest(http.MethodGet, "/v1/ai/activity?limit=25", nil)
	request.Header.Set("Authorization", "Bearer test-key")
	response := httptest.NewRecorder()
	server.ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
	}
	if gotPrincipal.TenantID != "tenant" || gotPrincipal.WorkspaceID != "workspace" {
		t.Fatalf("activity query was not scoped: %+v", gotPrincipal)
	}
	if !strings.Contains(response.Body.String(), "activity-a") {
		t.Fatalf("unexpected body=%s", response.Body.String())
	}

	request = httptest.NewRequest(http.MethodGet, "/v1/ai/activity", nil)
	request.Header.Set("Authorization", "Bearer test-key")
	response = httptest.NewRecorder()
	server = New(&fakeStore{scopes: []string{"profiles:read"}}, 75)
	server.ServeHTTP(response, request)
	if response.Code != http.StatusForbidden {
		t.Fatalf("without ai:read status=%d body=%s", response.Code, response.Body.String())
	}
}

func TestListExtensionActivityRequiresScopeAndIsTenantWorkspaceScoped(t *testing.T) {
	var gotPrincipal domain.Principal
	store := &fakeStore{
		scopes: []string{"extensions:read"},
		listExtensionActivitiesFunc: func(_ context.Context, p domain.Principal, extensionID string, limit int) ([]domain.ExtensionActivity, error) {
			gotPrincipal = p
			if extensionID != "extension-1" || limit != 25 {
				t.Fatalf("unexpected query: extension=%q limit=%d", extensionID, limit)
			}
			return []domain.ExtensionActivity{{ID: "activity-1", TenantID: p.TenantID, WorkspaceID: p.WorkspaceID, ExtensionID: extensionID}}, nil
		},
		getExtensionHealthFunc: func(_ context.Context, p domain.Principal, extensionID string) (domain.ExtensionHealth, error) {
			return domain.ExtensionHealth{ExtensionID: extensionID, TenantID: p.TenantID, State: "open"}, nil
		},
	}
	server := New(store, 75)
	request := httptest.NewRequest(http.MethodGet, "/v1/extensions/extension-1/activity?limit=25", nil)
	request.Header.Set("Authorization", "Bearer test-key")
	response := httptest.NewRecorder()
	server.ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
	}
	if gotPrincipal.TenantID != "tenant" || gotPrincipal.WorkspaceID != "workspace" {
		t.Fatalf("activity query was not scoped: %+v", gotPrincipal)
	}
	if !strings.Contains(response.Body.String(), `"activity-1"`) || !strings.Contains(response.Body.String(), `"state":"open"`) {
		t.Fatalf("unexpected body=%s", response.Body.String())
	}

	request = httptest.NewRequest(http.MethodGet, "/v1/extensions/extension-1/activity", nil)
	request.Header.Set("Authorization", "Bearer test-key")
	response = httptest.NewRecorder()
	server = New(&fakeStore{scopes: []string{"profiles:read"}}, 75)
	server.ServeHTTP(response, request)
	if response.Code != http.StatusForbidden {
		t.Fatalf("without extensions:read status=%d body=%s", response.Code, response.Body.String())
	}
}

func TestUpdateExtensionEnableRequiresHumanActor(t *testing.T) {
	server := New(&fakeStore{scopes: []string{"extensions:write"}}, 75)
	request := httptest.NewRequest(http.MethodPut, "/v1/extensions/extension-1", strings.NewReader(`{"status":"enabled"}`))
	request.Header.Set("Authorization", "Bearer api-key-actor")
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()
	server.ServeHTTP(response, request)
	if response.Code != http.StatusForbidden || !strings.Contains(response.Body.String(), `"code":"human_approval_required"`) {
		t.Fatalf("expected human approval gate, status=%d body=%s", response.Code, response.Body.String())
	}
}

func TestAIGenerationEnqueueReturnsAcceptedAndStatus(t *testing.T) {
	server := New(&fakeStore{scopes: []string{"ai:invoke"}}, 75)
	request := httptest.NewRequest(http.MethodPost, "/v1/ai/generations", strings.NewReader(`{"task_type":"content_draft","input":{"brief":"win back"}}`))
	request.Header.Set("Authorization", "Bearer test-key")
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()
	server.ServeHTTP(response, request)
	if response.Code != http.StatusAccepted {
		t.Fatalf("enqueue status=%d body=%s", response.Code, response.Body.String())
	}
	if !strings.Contains(response.Body.String(), `"id":"generation-1"`) {
		t.Fatalf("enqueue response=%s", response.Body.String())
	}

	request = httptest.NewRequest(http.MethodGet, "/v1/ai/generations/generation-1", nil)
	request.Header.Set("Authorization", "Bearer test-key")
	response = httptest.NewRecorder()
	server.ServeHTTP(response, request)
	if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), `"status":"pending"`) {
		t.Fatalf("status code=%d body=%s", response.Code, response.Body.String())
	}
}

func TestAIGenerationEndpointsRequireInvokeScope(t *testing.T) {
	server := New(&fakeStore{scopes: []string{"ai:read"}}, 75)
	request := httptest.NewRequest(http.MethodPost, "/v1/ai/generations", strings.NewReader(`{"task_type":"content_draft"}`))
	request.Header.Set("Authorization", "Bearer test-key")
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()
	server.ServeHTTP(response, request)
	if response.Code != http.StatusForbidden {
		t.Fatalf("without ai:invoke status=%d body=%s", response.Code, response.Body.String())
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

func TestJourneyEndpoints(t *testing.T) {
	store := &fakeStore{scopes: []string{"journeys:read", "journeys:write", "journeys:publish"}}
	blobs := &fakeBlobStore{}
	server := NewWithSessionTTL(store, 75, nil, "http://localhost:3000", 12*time.Hour, func(s *Server) {
		s.SetBlobStore(blobs)
	})

	createReq := httptest.NewRequest(http.MethodPost, "/v1/journeys", strings.NewReader(`{"name":"Welcome","graph":{"entry_node_id":"n1"}}`))
	createReq.Header.Set("Authorization", "Bearer test-key")
	createRes := httptest.NewRecorder()
	server.ServeHTTP(createRes, createReq)
	if createRes.Code != http.StatusCreated {
		t.Fatalf("status=%d body=%s", createRes.Code, createRes.Body.String())
	}
	var created domain.Journey
	if err := json.Unmarshal(createRes.Body.Bytes(), &created); err != nil {
		t.Fatalf("decode create body: %v", err)
	}
	if created.ID != "journey-1" || created.Name != "Welcome" {
		t.Fatalf("unexpected create body=%s", createRes.Body.String())
	}

	listReq := httptest.NewRequest(http.MethodGet, "/v1/journeys", nil)
	listReq.Header.Set("Authorization", "Bearer test-key")
	listRes := httptest.NewRecorder()
	server.ServeHTTP(listRes, listReq)
	if listRes.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", listRes.Code, listRes.Body.String())
	}
	var listBody struct {
		Journeys []domain.Journey `json:"journeys"`
	}
	if err := json.Unmarshal(listRes.Body.Bytes(), &listBody); err != nil {
		t.Fatalf("decode list body: %v", err)
	}
	if len(listBody.Journeys) != 1 || listBody.Journeys[0].ID != "journey-1" {
		t.Fatalf("unexpected list body=%s", listRes.Body.String())
	}

	getReq := httptest.NewRequest(http.MethodGet, "/v1/journeys/journey-1", nil)
	getReq.Header.Set("Authorization", "Bearer test-key")
	getRes := httptest.NewRecorder()
	server.ServeHTTP(getRes, getReq)
	if getRes.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", getRes.Code, getRes.Body.String())
	}
	if !strings.Contains(getRes.Body.String(), "Test Journey") {
		t.Fatalf("unexpected get body=%s", getRes.Body.String())
	}

	updateReq := httptest.NewRequest(http.MethodPut, "/v1/journeys/journey-1", strings.NewReader(`{"name":"Updated Welcome","graph":{"entry_node_id":"n2"}}`))
	updateReq.Header.Set("Authorization", "Bearer test-key")
	updateRes := httptest.NewRecorder()
	server.ServeHTTP(updateRes, updateReq)
	if updateRes.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", updateRes.Code, updateRes.Body.String())
	}
	var updated domain.Journey
	if err := json.Unmarshal(updateRes.Body.Bytes(), &updated); err != nil {
		t.Fatalf("decode update body: %v", err)
	}
	if updated.ID != "journey-1" || updated.Name != "Updated Welcome" {
		t.Fatalf("unexpected update body=%s", updateRes.Body.String())
	}

	publishReq := httptest.NewRequest(http.MethodPost, "/v1/journeys/journey-1/publish", strings.NewReader(`{"approver_user_id":"00000000-0000-0000-0000-000000000001"}`))
	publishReq.Header.Set("Authorization", "Bearer test-key")
	publishRes := httptest.NewRecorder()
	server.ServeHTTP(publishRes, publishReq)
	if publishRes.Code != http.StatusCreated {
		t.Fatalf("status=%d body=%s", publishRes.Code, publishRes.Body.String())
	}
	var version domain.JourneyVersion
	if err := json.Unmarshal(publishRes.Body.Bytes(), &version); err != nil {
		t.Fatalf("decode publish body: %v", err)
	}
	if version.JourneyID != "journey-1" || version.Version != 1 || version.ManifestKey == nil {
		t.Fatalf("unexpected publish body=%s", publishRes.Body.String())
	}
	if store.published != 1 {
		t.Fatalf("expected one publish call, got %d", store.published)
	}
	if _, ok := blobs.objects[*version.ManifestKey]; !ok {
		t.Fatalf("manifest was not written to blob store: %+v", blobs.objects)
	}

	// Set status (pause)
	pauseReq := httptest.NewRequest(http.MethodPut, "/v1/journeys/journey-1/versions/1", strings.NewReader(`{"status":"paused"}`))
	pauseReq.Header.Set("Authorization", "Bearer test-key")
	pauseRes := httptest.NewRecorder()
	server.ServeHTTP(pauseRes, pauseReq)
	if pauseRes.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", pauseRes.Code, pauseRes.Body.String())
	}
	var pauseBody map[string]string
	if err := json.Unmarshal(pauseRes.Body.Bytes(), &pauseBody); err != nil {
		t.Fatalf("decode pause body: %v", err)
	}
	if pauseBody["status"] != "paused" {
		t.Fatalf("unexpected status=%v", pauseBody["status"])
	}

	// Set status (invalid status)
	badStatusReq := httptest.NewRequest(http.MethodPut, "/v1/journeys/journey-1/versions/1", strings.NewReader(`{"status":"invalid"}`))
	badStatusReq.Header.Set("Authorization", "Bearer test-key")
	badStatusRes := httptest.NewRecorder()
	server.ServeHTTP(badStatusRes, badStatusReq)
	if badStatusRes.Code != http.StatusBadRequest {
		t.Fatalf("expected Bad Request for invalid status, got %d", badStatusRes.Code)
	}

	// Set status (non-integer version)
	badVerReq := httptest.NewRequest(http.MethodPut, "/v1/journeys/journey-1/versions/abc", strings.NewReader(`{"status":"paused"}`))
	badVerReq.Header.Set("Authorization", "Bearer test-key")
	badVerRes := httptest.NewRecorder()
	server.ServeHTTP(badVerRes, badVerReq)
	if badVerRes.Code != http.StatusBadRequest {
		t.Fatalf("expected Bad Request for non-integer version, got %d", badVerRes.Code)
	}

	// Cancel Run (success)
	cancelReq := httptest.NewRequest(http.MethodPost, "/v1/journeys/journey-1/runs/run-1/cancel", nil)
	cancelReq.Header.Set("Authorization", "Bearer test-key")
	cancelRes := httptest.NewRecorder()
	server.ServeHTTP(cancelRes, cancelReq)
	if cancelRes.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", cancelRes.Code, cancelRes.Body.String())
	}
	var cancelBody map[string]string
	if err := json.Unmarshal(cancelRes.Body.Bytes(), &cancelBody); err != nil {
		t.Fatalf("decode cancel body: %v", err)
	}
	if cancelBody["status"] != "canceled" {
		t.Fatalf("unexpected status=%v", cancelBody["status"])
	}

	// Cancel Run (not found)
	badCancelReq := httptest.NewRequest(http.MethodPost, "/v1/journeys/journey-1/runs/invalid-run/cancel", nil)
	badCancelReq.Header.Set("Authorization-key", "Bearer test-key")
	badCancelReq.Header.Set("Authorization", "Bearer test-key")
	badCancelRes := httptest.NewRecorder()
	server.ServeHTTP(badCancelRes, badCancelReq)
	if badCancelRes.Code != http.StatusNotFound {
		t.Fatalf("expected Not Found for invalid run, got %d", badCancelRes.Code)
	}

	// DLQ inspection (success)
	dlqReq := httptest.NewRequest(http.MethodGet, "/v1/journeys/dlq", nil)
	dlqReq.Header.Set("Authorization", "Bearer test-key")
	dlqRes := httptest.NewRecorder()
	server.ServeHTTP(dlqRes, dlqReq)
	if dlqRes.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", dlqRes.Code, dlqRes.Body.String())
	}
	var dlqBody struct {
		Steps   []domain.JourneyStep          `json:"steps"`
		Intents []domain.JourneyMessageIntent `json:"intents"`
	}
	if err := json.Unmarshal(dlqRes.Body.Bytes(), &dlqBody); err != nil {
		t.Fatalf("decode dlq body: %v", err)
	}
	if len(dlqBody.Steps) != 1 || dlqBody.Steps[0].ID != "step-1" {
		t.Fatalf("unexpected steps in dlq: %+v", dlqBody.Steps)
	}
	if len(dlqBody.Intents) != 1 || dlqBody.Intents[0].ID != "intent-1" {
		t.Fatalf("unexpected intents in dlq: %+v", dlqBody.Intents)
	}

	// DLQ retry step (success)
	retryStepReq := httptest.NewRequest(http.MethodPost, "/v1/journeys/dlq/step/step-1/retry", nil)
	retryStepReq.Header.Set("Authorization", "Bearer test-key")
	retryStepRes := httptest.NewRecorder()
	server.ServeHTTP(retryStepRes, retryStepReq)
	if retryStepRes.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", retryStepRes.Code, retryStepRes.Body.String())
	}
	var retryStepBody map[string]string
	if err := json.Unmarshal(retryStepRes.Body.Bytes(), &retryStepBody); err != nil {
		t.Fatalf("decode retry body: %v", err)
	}
	if retryStepBody["status"] != "pending" {
		t.Fatalf("unexpected status=%v", retryStepBody["status"])
	}

	// DLQ retry step (not found)
	badRetryStepReq := httptest.NewRequest(http.MethodPost, "/v1/journeys/dlq/step/invalid-step/retry", nil)
	badRetryStepReq.Header.Set("Authorization", "Bearer test-key")
	badRetryStepRes := httptest.NewRecorder()
	server.ServeHTTP(badRetryStepRes, badRetryStepReq)
	if badRetryStepRes.Code != http.StatusNotFound {
		t.Fatalf("expected Not Found for invalid step retry, got %d", badRetryStepRes.Code)
	}

	// DLQ retry intent (success)
	retryIntentReq := httptest.NewRequest(http.MethodPost, "/v1/journeys/dlq/intent/intent-1/retry", nil)
	retryIntentReq.Header.Set("Authorization", "Bearer test-key")
	retryIntentRes := httptest.NewRecorder()
	server.ServeHTTP(retryIntentRes, retryIntentReq)
	if retryIntentRes.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", retryIntentRes.Code, retryIntentRes.Body.String())
	}

	// DLQ retry (invalid kind)
	badKindReq := httptest.NewRequest(http.MethodPost, "/v1/journeys/dlq/invalid-kind/intent-1/retry", nil)
	badKindReq.Header.Set("Authorization", "Bearer test-key")
	badKindRes := httptest.NewRecorder()
	server.ServeHTTP(badKindRes, badKindReq)
	if badKindRes.Code != http.StatusBadRequest {
		t.Fatalf("expected Bad Request for invalid kind, got %d", badKindRes.Code)
	}

	// Backfill (success)
	backfillReq := httptest.NewRequest(http.MethodPost, "/v1/journeys/journey-1/backfill", strings.NewReader(`{"segment_id":"segment-1","approver_user_id":"00000000-0000-0000-0000-000000000001"}`))
	backfillReq.Header.Set("Authorization", "Bearer test-key")
	backfillRes := httptest.NewRecorder()
	server.ServeHTTP(backfillRes, backfillReq)
	if backfillRes.Code != http.StatusOK {
		t.Fatalf("expected 200 for backfill success, got %d body=%s", backfillRes.Code, backfillRes.Body.String())
	}
	var backfillBody map[string]int
	if err := json.Unmarshal(backfillRes.Body.Bytes(), &backfillBody); err != nil {
		t.Fatalf("decode backfill body: %v", err)
	}
	if backfillBody["enrolled_count"] != 2 {
		t.Fatalf("expected enrolled_count=2, got %d", backfillBody["enrolled_count"])
	}

	// Backfill (missing segment_id)
	badBackfillReq1 := httptest.NewRequest(http.MethodPost, "/v1/journeys/journey-1/backfill", strings.NewReader(`{"approver_user_id":"00000000-0000-0000-0000-000000000001"}`))
	badBackfillReq1.Header.Set("Authorization", "Bearer test-key")
	badBackfillRes1 := httptest.NewRecorder()
	server.ServeHTTP(badBackfillRes1, badBackfillReq1)
	if badBackfillRes1.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for missing segment_id, got %d", badBackfillRes1.Code)
	}

	// Backfill approval is derived from the authenticated user.
	badBackfillReq2 := httptest.NewRequest(http.MethodPost, "/v1/journeys/journey-1/backfill", strings.NewReader(`{"segment_id":"segment-1"}`))
	badBackfillReq2.Header.Set("Authorization", "Bearer test-key")
	badBackfillRes2 := httptest.NewRecorder()
	server.ServeHTTP(badBackfillRes2, badBackfillReq2)
	if badBackfillRes2.Code != http.StatusOK {
		t.Fatalf("expected 200 with principal-derived approval, got %d", badBackfillRes2.Code)
	}

	// Backfill (not found)
	badBackfillReq3 := httptest.NewRequest(http.MethodPost, "/v1/journeys/not-found-journey/backfill", strings.NewReader(`{"segment_id":"segment-1","approver_user_id":"00000000-0000-0000-0000-000000000001"}`))
	badBackfillReq3.Header.Set("Authorization", "Bearer test-key")
	badBackfillRes3 := httptest.NewRecorder()
	server.ServeHTTP(badBackfillRes3, badBackfillReq3)
	if badBackfillRes3.Code != http.StatusNotFound {
		t.Fatalf("expected 404 for not found journey, got %d", badBackfillRes3.Code)
	}

	// List Runs (success)
	runsReq := httptest.NewRequest(http.MethodGet, "/v1/journeys/journey-1/runs", nil)
	runsReq.Header.Set("Authorization", "Bearer test-key")
	runsRes := httptest.NewRecorder()
	server.ServeHTTP(runsRes, runsReq)
	if runsRes.Code != http.StatusOK {
		t.Fatalf("expected 200 for runs list, got %d body=%s", runsRes.Code, runsRes.Body.String())
	}

	// List Transitions (success)
	transReq := httptest.NewRequest(http.MethodGet, "/v1/journeys/journey-1/runs/run-1/transitions", nil)
	transReq.Header.Set("Authorization", "Bearer test-key")
	transRes := httptest.NewRecorder()
	server.ServeHTTP(transRes, transReq)
	if transRes.Code != http.StatusOK {
		t.Fatalf("expected 200 for transitions list, got %d body=%s", transRes.Code, transRes.Body.String())
	}

	// Get Journey Version (success)
	verReq := httptest.NewRequest(http.MethodGet, "/v1/journeys/journey-1/versions/1", nil)
	verReq.Header.Set("Authorization", "Bearer test-key")
	verRes := httptest.NewRecorder()
	server.ServeHTTP(verRes, verReq)
	if verRes.Code != http.StatusOK {
		t.Fatalf("expected 200 for version get, got %d body=%s", verRes.Code, verRes.Body.String())
	}
}

func TestJourneyEndpointsRequireScopes(t *testing.T) {
	server := New(&fakeStore{scopes: []string{"journeys:read"}}, 75)
	request := httptest.NewRequest(http.MethodPost, "/v1/journeys", strings.NewReader(`{"name":"Welcome"}`))
	request.Header.Set("Authorization", "Bearer test-key")
	response := httptest.NewRecorder()
	server.ServeHTTP(response, request)
	if response.Code != http.StatusForbidden {
		t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
	}
}

func TestPublishJourneyRejectsInvalidGraph(t *testing.T) {
	store := &fakeStore{scopes: []string{"journeys:publish"}}
	blobs := &fakeBlobStore{}
	server := NewWithSessionTTL(store, 75, nil, "http://localhost:3000", 12*time.Hour, func(s *Server) {
		s.SetBlobStore(blobs)
	})
	request := httptest.NewRequest(http.MethodPost, "/v1/journeys/invalid-journey/publish", strings.NewReader(`{"approver_user_id":"00000000-0000-0000-0000-000000000001"}`))
	request.Header.Set("Authorization", "Bearer test-key")
	response := httptest.NewRecorder()
	server.ServeHTTP(response, request)
	if response.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
	}
	if store.published != 0 {
		t.Fatalf("invalid graph should not be published")
	}
	if len(blobs.objects) != 0 {
		t.Fatalf("invalid graph should not write blobs: %+v", blobs.objects)
	}
}

func TestPublishJourneyRequiresPublishScope(t *testing.T) {
	server := New(&fakeStore{scopes: []string{"journeys:write"}}, 75)
	request := httptest.NewRequest(http.MethodPost, "/v1/journeys/journey-1/publish", strings.NewReader(`{"approver_user_id":"00000000-0000-0000-0000-000000000001"}`))
	request.Header.Set("Authorization", "Bearer test-key")
	response := httptest.NewRecorder()
	server.ServeHTTP(response, request)
	if response.Code != http.StatusForbidden {
		t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
	}
}

func TestJourneyPublishAndBackfillRequireHumanActor(t *testing.T) {
	store := &fakeStore{scopes: []string{"journeys:write", "journeys:publish"}}
	server := NewWithSessionTTL(store, 75, nil, "http://localhost:3000", 12*time.Hour)

	for _, tc := range []struct {
		name  string
		token string
		path  string
		body  string
	}{
		{name: "publish api_key", token: "api-key-actor", path: "/v1/journeys/journey-1/publish", body: `{}`},
		{name: "publish ai_agent", token: "ai-agent-actor", path: "/v1/journeys/journey-1/publish", body: `{}`},
		{name: "backfill api_key", token: "api-key-actor", path: "/v1/journeys/journey-1/backfill", body: `{"segment_id":"segment-1"}`},
		{name: "backfill ai_agent", token: "ai-agent-actor", path: "/v1/journeys/journey-1/backfill", body: `{"segment_id":"segment-1"}`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			request := httptest.NewRequest(http.MethodPost, tc.path, strings.NewReader(tc.body))
			request.Header.Set("Authorization", "Bearer "+tc.token)
			response := httptest.NewRecorder()

			server.ServeHTTP(response, request)

			if response.Code != http.StatusForbidden {
				t.Fatalf("expected non-human actor to be rejected with 403, got %d body=%s", response.Code, response.Body.String())
			}
			if !strings.Contains(response.Body.String(), `"code":"human_approval_required"`) {
				t.Fatalf("expected human approval error, body=%s", response.Body.String())
			}
		})
	}

	if store.published != 0 {
		t.Fatalf("API-key actor bypassed publish gate: published=%d", store.published)
	}
	if len(store.runs) != 0 {
		t.Fatalf("API-key actor bypassed backfill gate: runs=%d", len(store.runs))
	}
}

func TestExperimentRolloutRequiresHumanActorAndReturnsNewVersion(t *testing.T) {
	store := &fakeStore{scopes: []string{"experiments:write"}}
	server := NewWithSessionTTL(store, 75, nil, "http://localhost:3000", 12*time.Hour)

	apiKeyRequest := httptest.NewRequest(http.MethodPost, "/v1/experiments/experiment-1/rollout", nil)
	apiKeyRequest.Header.Set("Authorization", "Bearer api-key-actor")
	apiKeyResponse := httptest.NewRecorder()
	server.ServeHTTP(apiKeyResponse, apiKeyRequest)
	if apiKeyResponse.Code != http.StatusForbidden {
		t.Fatalf("non-user status=%d body=%s", apiKeyResponse.Code, apiKeyResponse.Body.String())
	}
	if !strings.Contains(apiKeyResponse.Body.String(), `"code":"human_approval_required"`) || store.rollouts != 0 {
		t.Fatalf("non-user bypassed rollout gate: calls=%d body=%s", store.rollouts, apiKeyResponse.Body.String())
	}

	aiAgentRequest := httptest.NewRequest(http.MethodPost, "/v1/experiments/experiment-1/rollout", nil)
	aiAgentRequest.Header.Set("Authorization", "Bearer ai-agent-actor")
	aiAgentResponse := httptest.NewRecorder()
	server.ServeHTTP(aiAgentResponse, aiAgentRequest)
	if aiAgentResponse.Code != http.StatusForbidden {
		t.Fatalf("non-user status=%d body=%s", aiAgentResponse.Code, aiAgentResponse.Body.String())
	}
	if !strings.Contains(aiAgentResponse.Body.String(), `"code":"human_approval_required"`) || store.rollouts != 0 {
		t.Fatalf("non-user bypassed rollout gate: calls=%d body=%s", store.rollouts, aiAgentResponse.Body.String())
	}

	userRequest := httptest.NewRequest(http.MethodPost, "/v1/experiments/experiment-1/rollout", nil)
	userRequest.Header.Set("Authorization", "Bearer test-key")
	userResponse := httptest.NewRecorder()
	server.ServeHTTP(userResponse, userRequest)
	if userResponse.Code != http.StatusCreated {
		t.Fatalf("user status=%d body=%s", userResponse.Code, userResponse.Body.String())
	}
	var rollout domain.ExperimentRollout
	if err := json.Unmarshal(userResponse.Body.Bytes(), &rollout); err != nil {
		t.Fatal(err)
	}
	if store.rollouts != 1 || rollout.JourneyVersion == nil || rollout.JourneyVersion.Version != 2 {
		t.Fatalf("rollout calls=%d response=%+v", store.rollouts, rollout)
	}
}

func TestExperimentOptimizationApprovalRequiresHumanActor(t *testing.T) {
	store := &fakeStore{scopes: []string{"experiments:write"}}
	server := NewWithSessionTTL(store, 75, nil, "http://localhost:3000", 12*time.Hour)
	for _, key := range []string{"api-key-actor", "ai-agent-actor"} {
		req := httptest.NewRequest(http.MethodPost, "/v1/experiments/experiment-1/optimize/proposal-1/approve", nil)
		req.Header.Set("Authorization", "Bearer "+key)
		res := httptest.NewRecorder()
		server.ServeHTTP(res, req)
		if res.Code != http.StatusForbidden || !strings.Contains(res.Body.String(), `"code":"human_approval_required"`) {
			t.Fatalf("%s approval status=%d body=%s", key, res.Code, res.Body.String())
		}
	}
	req := httptest.NewRequest(http.MethodPost, "/v1/experiments/experiment-1/optimize/proposal-1/approve", nil)
	req.Header.Set("Authorization", "Bearer test-key")
	res := httptest.NewRecorder()
	server.ServeHTTP(res, req)
	if res.Code != http.StatusCreated || store.optimizationApprovals != 1 {
		t.Fatalf("human approval status=%d approvals=%d body=%s", res.Code, store.optimizationApprovals, res.Body.String())
	}
}

func TestGovernanceCloseout_12_11_3(t *testing.T) {
	store := &fakeStore{scopes: []string{"experiments:write"}}
	server := NewWithSessionTTL(store, 75, nil, "http://localhost:3000", 12*time.Hour)

	for _, actor := range []string{"api-key-actor", "ai-agent-actor"} {
		t.Run(actor+" cannot approve", func(t *testing.T) {
			req := httptest.NewRequest(http.MethodPost, "/v1/experiments/experiment-1/optimize/proposal-1/approve", nil)
			req.Header.Set("Authorization", "Bearer "+actor)
			res := httptest.NewRecorder()
			server.ServeHTTP(res, req)
			if res.Code != http.StatusForbidden || !strings.Contains(res.Body.String(), `"code":"human_approval_required"`) {
				t.Fatalf("actor=%s status=%d body=%s; optimization approval must require a human", actor, res.Code, res.Body.String())
			}
		})
	}

	if store.optimizationApprovals != 0 {
		t.Fatalf("non-human actor bypassed approval gate: calls=%d", store.optimizationApprovals)
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

func ptrString(s string) *string {
	return &s
}

func TestTemplatePreviewSMS(t *testing.T) {
	store := &fakeStore{scopes: []string{"templates:read", "profiles:read"}}
	server := New(store, 75)

	// Set up mock template and profile
	store.getTemplateFunc = func(id string) (domain.Template, error) {
		textTmpl := "Hello {{ profile.attributes.first_name }}! Welcome to openjourney 🚀."
		return domain.Template{
			ID:           id,
			Name:         "SMS Template",
			Channel:      "sms",
			TextTemplate: &textTmpl,
			Version:      1,
		}, nil
	}

	store.getProfileFunc = func(externalID string) (domain.Profile, error) {
		return domain.Profile{
			ID:         "prof-123",
			ExternalID: externalID,
			Attributes: json.RawMessage(`{"first_name": "Alice"}`),
		}, nil
	}

	bodyJSON := `{"external_id":"user-123"}`
	previewReq := httptest.NewRequest(http.MethodPost, "/v1/templates/tmpl-sms/preview", strings.NewReader(bodyJSON))
	previewReq.Header.Set("Authorization", "Bearer test-key")
	previewReq.Header.Set("Content-Type", "application/json")
	previewRes := httptest.NewRecorder()

	server.ServeHTTP(previewRes, previewReq)
	if previewRes.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", previewRes.Code, previewRes.Body.String())
	}

	var resp struct {
		Subject      string `json:"subject"`
		Body         string `json:"body"`
		SmsEncoding  string `json:"sms_encoding"`
		SmsCharCount int    `json:"sms_char_count"`
		SmsSegments  int    `json:"sms_segments"`
		Warning      string `json:"warning"`
	}

	if err := json.Unmarshal(previewRes.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}

	expectedBody := "Hello Alice! Welcome to openjourney 🚀."
	if resp.Body != expectedBody {
		t.Errorf("expected body %q, got %q", expectedBody, resp.Body)
	}

	if resp.SmsEncoding != "UCS-2" {
		t.Errorf("expected encoding UCS-2, got %q", resp.SmsEncoding)
	}

	if resp.SmsCharCount != 38 {
		t.Errorf("expected char count 38, got %d", resp.SmsCharCount)
	}

	if resp.SmsSegments != 1 {
		t.Errorf("expected 1 segment, got %d", resp.SmsSegments)
	}

	if !strings.Contains(resp.Warning, "contains non-GSM-7 characters") {
		t.Errorf("expected UCS-2 warning, got %q", resp.Warning)
	}
}

func TestTemplatePreviewPush(t *testing.T) {
	store := &fakeStore{scopes: []string{"templates:read", "profiles:read"}}
	server := New(store, 75)

	store.getTemplateFunc = func(id string) (domain.Template, error) {
		titleTmpl := "Hello {{ profile.attributes.first_name }}!"
		bodyTmpl := "Welcome to push notifications."
		return domain.Template{
			ID:            id,
			Name:          "Push Template",
			Channel:       "push",
			TitleTemplate: &titleTmpl,
			BodyTemplate:  &bodyTmpl,
			PushData: map[string]string{
				"deep_link": "https://example.com/promo?name={{ profile.attributes.first_name }}",
			},
			Version: 1,
		}, nil
	}

	store.getProfileFunc = func(externalID string) (domain.Profile, error) {
		return domain.Profile{
			ID:         "prof-123",
			ExternalID: externalID,
			Attributes: json.RawMessage(`{"first_name": "Bob"}`),
		}, nil
	}

	bodyJSON := `{"external_id":"user-456"}`
	previewReq := httptest.NewRequest(http.MethodPost, "/v1/templates/tmpl-push/preview", strings.NewReader(bodyJSON))
	previewReq.Header.Set("Authorization", "Bearer test-key")
	previewReq.Header.Set("Content-Type", "application/json")
	previewRes := httptest.NewRecorder()

	server.ServeHTTP(previewRes, previewReq)
	if previewRes.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", previewRes.Code, previewRes.Body.String())
	}

	var resp struct {
		Title    string            `json:"title"`
		Body     string            `json:"body"`
		PushData map[string]string `json:"push_data"`
	}

	if err := json.Unmarshal(previewRes.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}

	if resp.Title != "Hello Bob!" {
		t.Errorf("expected title 'Hello Bob!', got %q", resp.Title)
	}
	if resp.Body != "Welcome to push notifications." {
		t.Errorf("expected body, got %q", resp.Body)
	}
	if resp.PushData["deep_link"] != "https://example.com/promo?name=Bob" {
		t.Errorf("expected rendered deep_link, got %q", resp.PushData["deep_link"])
	}
}

func TestTemplatePreviewInApp(t *testing.T) {
	store := &fakeStore{scopes: []string{"templates:read", "profiles:read"}}
	server := New(store, 75)

	store.getTemplateFunc = func(id string) (domain.Template, error) {
		titleTmpl := "Welcome {{ profile.attributes.first_name }}!"
		bodyTmpl := "Check out this exclusive offer."
		return domain.Template{
			ID:            id,
			Name:          "InApp Template",
			Channel:       "in_app",
			TitleTemplate: &titleTmpl,
			BodyTemplate:  &bodyTmpl,
			Version:       1,
		}, nil
	}

	store.getProfileFunc = func(externalID string) (domain.Profile, error) {
		return domain.Profile{
			ID:         "prof-456",
			ExternalID: externalID,
			Attributes: json.RawMessage(`{"first_name": "Alice"}`),
		}, nil
	}

	bodyJSON := `{"external_id":"user-789"}`
	previewReq := httptest.NewRequest(http.MethodPost, "/v1/templates/tmpl-inapp/preview", strings.NewReader(bodyJSON))
	previewReq.Header.Set("Authorization", "Bearer test-key")
	previewReq.Header.Set("Content-Type", "application/json")
	previewRes := httptest.NewRecorder()

	server.ServeHTTP(previewRes, previewReq)
	if previewRes.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", previewRes.Code, previewRes.Body.String())
	}

	var resp struct {
		Title string `json:"title"`
		Body  string `json:"body"`
	}

	if err := json.Unmarshal(previewRes.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}

	if resp.Title != "Welcome Alice!" {
		t.Errorf("expected title 'Welcome Alice!', got %q", resp.Title)
	}
	if resp.Body != "Check out this exclusive offer." {
		t.Errorf("expected body 'Check out this exclusive offer.', got %q", resp.Body)
	}
}

func TestSMSCallbackTwilioSignature(t *testing.T) {
	store := &fakeStore{}
	server := New(store, 75)

	mockIdentity := domain.SendingIdentity{
		ID:       "iden-sms-1",
		TenantID: "tenant-sms",
		Provider: "twilio",
		Config:   json.RawMessage(`{"account_sid":"AC123", "auth_token":"my-secret-token"}`),
	}

	store.getSendingIdentityFunc = func(id string) (domain.SendingIdentity, error) {
		if id == "iden-sms-1" {
			return mockIdentity, nil
		}
		return domain.SendingIdentity{}, errors.New("not found")
	}

	store.getSendingIdentityByProviderConfigFunc = func(provider, configKey, configVal string) (domain.SendingIdentity, error) {
		if provider == "twilio" && configKey == "account_sid" && configVal == "AC123" {
			return mockIdentity, nil
		}
		return domain.SendingIdentity{}, errors.New("not found")
	}

	t.Run("unsigned request returns 4xx", func(t *testing.T) {
		body := "AccountSid=AC123&From=%2B15555550100&Body=STOP"
		req := httptest.NewRequest(http.MethodPost, "/v1/callbacks/sms/twilio", strings.NewReader(body))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		res := httptest.NewRecorder()

		server.ServeHTTP(res, req)
		if res.Code != http.StatusForbidden {
			t.Errorf("expected 403 Forbidden, got %d", res.Code)
		}
	})

	t.Run("correct signature returns 200 OK via global AccountSid lookup", func(t *testing.T) {
		requestURL := "http://example.com/v1/callbacks/sms/twilio"
		data := requestURL + "AccountSidAC123BodySTOPFrom+15555550100"
		mac := hmac.New(sha1.New, []byte("my-secret-token"))
		mac.Write([]byte(data))
		signature := base64.StdEncoding.EncodeToString(mac.Sum(nil))

		body := "AccountSid=AC123&From=%2B15555550100&Body=STOP"
		req := httptest.NewRequest(http.MethodPost, "/v1/callbacks/sms/twilio", strings.NewReader(body))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		req.Header.Set("X-Twilio-Signature", signature)
		req.Host = "example.com"
		res := httptest.NewRecorder()

		server.ServeHTTP(res, req)
		if res.Code != http.StatusOK {
			t.Errorf("expected 200 OK, got %d body=%s", res.Code, res.Body.String())
		}
	})

	t.Run("correct signature returns 200 OK via query params lookup", func(t *testing.T) {
		requestURL := "http://example.com/v1/callbacks/sms/twilio?tenant_id=tenant-sms&sending_identity_id=iden-sms-1"
		data := requestURL + "AccountSidAC123BodySTOPFrom+15555550100"
		mac := hmac.New(sha1.New, []byte("my-secret-token"))
		mac.Write([]byte(data))
		signature := base64.StdEncoding.EncodeToString(mac.Sum(nil))

		body := "AccountSid=AC123&From=%2B15555550100&Body=STOP"
		req := httptest.NewRequest(http.MethodPost, "/v1/callbacks/sms/twilio?tenant_id=tenant-sms&sending_identity_id=iden-sms-1", strings.NewReader(body))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		req.Header.Set("X-Twilio-Signature", signature)
		req.Host = "example.com"
		res := httptest.NewRecorder()

		server.ServeHTTP(res, req)
		if res.Code != http.StatusOK {
			t.Errorf("expected 200 OK, got %d body=%s", res.Code, res.Body.String())
		}
	})
}

func TestSMSCallbackSTOPSTART(t *testing.T) {
	store := &fakeStore{}
	server := New(store, 75)

	mockIdentity := domain.SendingIdentity{
		ID:       "iden-sms-1",
		TenantID: "tenant-sms",
		Provider: "twilio",
		Config:   json.RawMessage(`{"account_sid":"AC123", "auth_token":"my-secret-token"}`),
	}

	store.getSendingIdentityFunc = func(id string) (domain.SendingIdentity, error) {
		if id == "iden-sms-1" {
			return mockIdentity, nil
		}
		return domain.SendingIdentity{}, errors.New("not found")
	}

	store.getSendingIdentityByProviderConfigFunc = func(provider, configKey, configVal string) (domain.SendingIdentity, error) {
		if provider == "twilio" && configKey == "account_sid" && configVal == "AC123" {
			return mockIdentity, nil
		}
		return domain.SendingIdentity{}, errors.New("not found")
	}

	store.getProfileByPhoneFunc = func(tenantID, phone string) (domain.Profile, error) {
		if phone == "+15555550100" {
			return domain.Profile{
				ID:         "prof-123",
				ExternalID: "ext-123",
			}, nil
		}
		return domain.Profile{}, errors.New("not found")
	}

	var acceptedEvents []domain.Event
	store.AcceptEventsFunc = func(ctx context.Context, p domain.Principal, events []domain.Event) ([]string, error) {
		acceptedEvents = append(acceptedEvents, events...)
		return []string{"event-1"}, nil
	}

	t.Run("STOP keyword emits consent.changed unsubscribed", func(t *testing.T) {
		acceptedEvents = nil
		requestURL := "http://example.com/v1/callbacks/sms/twilio"
		data := requestURL + "AccountSidAC123BodySTOPFrom+15555550100MessageSidSM999"
		mac := hmac.New(sha1.New, []byte("my-secret-token"))
		mac.Write([]byte(data))
		signature := base64.StdEncoding.EncodeToString(mac.Sum(nil))

		body := "AccountSid=AC123&From=%2B15555550100&Body=STOP&MessageSid=SM999"
		req := httptest.NewRequest(http.MethodPost, "/v1/callbacks/sms/twilio", strings.NewReader(body))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		req.Header.Set("X-Twilio-Signature", signature)
		req.Host = "example.com"
		res := httptest.NewRecorder()

		server.ServeHTTP(res, req)
		if res.Code != http.StatusOK {
			t.Fatalf("expected 200 OK, got %d body=%s", res.Code, res.Body.String())
		}

		if len(acceptedEvents) != 1 {
			t.Fatalf("expected 1 accepted event, got %d", len(acceptedEvents))
		}
		event := acceptedEvents[0]
		if event.Type != "consent.changed" {
			t.Errorf("expected event.Type = consent.changed, got %q", event.Type)
		}
		if event.ExternalID != "ext-123" {
			t.Errorf("expected ExternalID = ext-123, got %q", event.ExternalID)
		}
		var payload map[string]any
		if err := json.Unmarshal(event.Payload, &payload); err != nil {
			t.Fatal(err)
		}
		if payload["channel"] != "sms" {
			t.Errorf("expected channel = sms, got %v", payload["channel"])
		}
		if payload["state"] != "unsubscribed" {
			t.Errorf("expected state = unsubscribed, got %v", payload["state"])
		}
	})

	t.Run("START keyword emits consent.changed subscribed", func(t *testing.T) {
		acceptedEvents = nil
		requestURL := "http://example.com/v1/callbacks/sms/twilio"
		data := requestURL + "AccountSidAC123BodySTARTFrom+15555550100MessageSidSM888"
		mac := hmac.New(sha1.New, []byte("my-secret-token"))
		mac.Write([]byte(data))
		signature := base64.StdEncoding.EncodeToString(mac.Sum(nil))

		body := "AccountSid=AC123&From=%2B15555550100&Body=START&MessageSid=SM888"
		req := httptest.NewRequest(http.MethodPost, "/v1/callbacks/sms/twilio", strings.NewReader(body))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		req.Header.Set("X-Twilio-Signature", signature)
		req.Host = "example.com"
		res := httptest.NewRecorder()

		server.ServeHTTP(res, req)
		if res.Code != http.StatusOK {
			t.Fatalf("expected 200 OK, got %d body=%s", res.Code, res.Body.String())
		}

		if len(acceptedEvents) != 1 {
			t.Fatalf("expected 1 accepted event, got %d", len(acceptedEvents))
		}
		event := acceptedEvents[0]
		if event.Type != "consent.changed" {
			t.Errorf("expected event.Type = consent.changed, got %q", event.Type)
		}
		var payload map[string]any
		if err := json.Unmarshal(event.Payload, &payload); err != nil {
			t.Fatal(err)
		}
		if payload["state"] != "subscribed" {
			t.Errorf("expected state = subscribed, got %v", payload["state"])
		}
	})
}

func TestSMSCallbackDLR(t *testing.T) {
	store := &fakeStore{}
	server := New(store, 75)

	mockIdentity := domain.SendingIdentity{
		ID:       "iden-sms-1",
		TenantID: "tenant-sms",
		Provider: "twilio",
		Config:   json.RawMessage(`{"account_sid":"AC123", "auth_token":"my-secret-token"}`),
	}

	store.getSendingIdentityFunc = func(id string) (domain.SendingIdentity, error) {
		if id == "iden-sms-1" {
			return mockIdentity, nil
		}
		return domain.SendingIdentity{}, errors.New("not found")
	}

	store.getSendingIdentityByProviderConfigFunc = func(provider, configKey, configVal string) (domain.SendingIdentity, error) {
		if provider == "twilio" && configKey == "account_sid" && configVal == "AC123" {
			return mockIdentity, nil
		}
		return domain.SendingIdentity{}, errors.New("not found")
	}

	store.getProfileByPhoneFunc = func(tenantID, phone string) (domain.Profile, error) {
		if phone == "+15555550100" {
			return domain.Profile{
				ID:         "prof-123",
				ExternalID: "ext-123",
			}, nil
		}
		return domain.Profile{}, errors.New("not found")
	}

	var acceptedEvents []domain.Event
	store.AcceptEventsFunc = func(ctx context.Context, p domain.Principal, events []domain.Event) ([]string, error) {
		acceptedEvents = append(acceptedEvents, events...)
		return []string{"event-1"}, nil
	}

	t.Run("delivered DLR emits message.delivered", func(t *testing.T) {
		acceptedEvents = nil
		requestURL := "http://example.com/v1/callbacks/sms/twilio"
		// Parameters sorted alphabetically:
		// AccountSid=AC123
		// MessageSid=SM001
		// MessageStatus=delivered
		// To=+15555550100
		data := requestURL + "AccountSidAC123MessageSidSM001MessageStatusdeliveredTo+15555550100"
		mac := hmac.New(sha1.New, []byte("my-secret-token"))
		mac.Write([]byte(data))
		signature := base64.StdEncoding.EncodeToString(mac.Sum(nil))

		body := "AccountSid=AC123&MessageSid=SM001&MessageStatus=delivered&To=%2B15555550100"
		req := httptest.NewRequest(http.MethodPost, "/v1/callbacks/sms/twilio", strings.NewReader(body))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		req.Header.Set("X-Twilio-Signature", signature)
		req.Host = "example.com"
		res := httptest.NewRecorder()

		server.ServeHTTP(res, req)
		if res.Code != http.StatusOK {
			t.Fatalf("expected 200 OK, got %d body=%s", res.Code, res.Body.String())
		}

		if len(acceptedEvents) != 1 {
			t.Fatalf("expected 1 accepted event, got %d", len(acceptedEvents))
		}
		event := acceptedEvents[0]
		if event.Type != "message.delivered" {
			t.Errorf("expected event.Type = message.delivered, got %q", event.Type)
		}
		if event.ExternalID != "ext-123" {
			t.Errorf("expected ExternalID = ext-123, got %q", event.ExternalID)
		}
		var payload map[string]any
		if err := json.Unmarshal(event.Payload, &payload); err != nil {
			t.Fatal(err)
		}
		if payload["channel"] != "sms" {
			t.Errorf("expected channel = sms, got %v", payload["channel"])
		}
		if payload["provider_message_id"] != "SM001" {
			t.Errorf("expected provider_message_id = SM001, got %v", payload["provider_message_id"])
		}
	})

	t.Run("permanent failure DLR emits message.bounced", func(t *testing.T) {
		acceptedEvents = nil
		requestURL := "http://example.com/v1/callbacks/sms/twilio"
		// Parameters sorted alphabetically:
		// AccountSid=AC123
		// ErrorCode=30006
		// MessageSid=SM002
		// MessageStatus=failed
		// To=+15555550100
		data := requestURL + "AccountSidAC123ErrorCode30006MessageSidSM002MessageStatusfailedTo+15555550100"
		mac := hmac.New(sha1.New, []byte("my-secret-token"))
		mac.Write([]byte(data))
		signature := base64.StdEncoding.EncodeToString(mac.Sum(nil))

		body := "AccountSid=AC123&MessageSid=SM002&MessageStatus=failed&To=%2B15555550100&ErrorCode=30006"
		req := httptest.NewRequest(http.MethodPost, "/v1/callbacks/sms/twilio", strings.NewReader(body))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		req.Header.Set("X-Twilio-Signature", signature)
		req.Host = "example.com"
		res := httptest.NewRecorder()

		server.ServeHTTP(res, req)
		if res.Code != http.StatusOK {
			t.Fatalf("expected 200 OK, got %d body=%s", res.Code, res.Body.String())
		}

		if len(acceptedEvents) != 1 {
			t.Fatalf("expected 1 accepted event, got %d", len(acceptedEvents))
		}
		event := acceptedEvents[0]
		if event.Type != "message.bounced" {
			t.Errorf("expected event.Type = message.bounced, got %q", event.Type)
		}
		var payload map[string]any
		if err := json.Unmarshal(event.Payload, &payload); err != nil {
			t.Fatal(err)
		}
		if payload["bounce_type"] != "permanent" {
			t.Errorf("expected bounce_type = permanent, got %v", payload["bounce_type"])
		}
		if payload["error_code"] != "30006" {
			t.Errorf("expected error_code = 30006, got %v", payload["error_code"])
		}
	})

	t.Run("transient failure DLR emits message.failed", func(t *testing.T) {
		acceptedEvents = nil
		requestURL := "http://example.com/v1/callbacks/sms/twilio"
		// Parameters sorted alphabetically:
		// AccountSid=AC123
		// ErrorCode=30008
		// MessageSid=SM003
		// MessageStatus=failed
		// To=+15555550100
		data := requestURL + "AccountSidAC123ErrorCode30008MessageSidSM003MessageStatusfailedTo+15555550100"
		mac := hmac.New(sha1.New, []byte("my-secret-token"))
		mac.Write([]byte(data))
		signature := base64.StdEncoding.EncodeToString(mac.Sum(nil))

		body := "AccountSid=AC123&MessageSid=SM003&MessageStatus=failed&To=%2B15555550100&ErrorCode=30008"
		req := httptest.NewRequest(http.MethodPost, "/v1/callbacks/sms/twilio", strings.NewReader(body))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		req.Header.Set("X-Twilio-Signature", signature)
		req.Host = "example.com"
		res := httptest.NewRecorder()

		server.ServeHTTP(res, req)
		if res.Code != http.StatusOK {
			t.Fatalf("expected 200 OK, got %d body=%s", res.Code, res.Body.String())
		}

		if len(acceptedEvents) != 1 {
			t.Fatalf("expected 1 accepted event, got %d", len(acceptedEvents))
		}
		event := acceptedEvents[0]
		if event.Type != "message.failed" {
			t.Errorf("expected event.Type = message.failed, got %q", event.Type)
		}
	})
}

func TestDeviceTokensAPI(t *testing.T) {
	store := &fakeStore{
		scopes: []string{"device_tokens:write"},
	}
	server := New(store, 75)

	t.Run("POST /v1/device-tokens registers token", func(t *testing.T) {
		store.registerDeviceTokenFunc = func(ctx context.Context, tenantID, workspaceID, appID, profileID, platform, provider, token string) (domain.DeviceToken, error) {
			if tenantID != "tenant" || workspaceID != "workspace" || appID != "app" {
				return domain.DeviceToken{}, errors.New("invalid principal values")
			}
			return domain.DeviceToken{
				ID:          "token-id-1",
				TenantID:    tenantID,
				WorkspaceID: workspaceID,
				AppID:       appID,
				ProfileID:   profileID,
				Platform:    platform,
				Provider:    provider,
				Token:       token,
				Status:      "active",
			}, nil
		}

		body := `{"profile_id":"prof-123", "platform":"ios", "provider":"fcm", "token":"token-val"}`
		req := httptest.NewRequest(http.MethodPost, "/v1/device-tokens", strings.NewReader(body))
		req.Header.Set("Authorization", "Bearer test-key")
		req.Header.Set("Content-Type", "application/json")
		res := httptest.NewRecorder()

		server.ServeHTTP(res, req)
		if res.Code != http.StatusCreated {
			t.Fatalf("expected 201 Created, got %d body=%s", res.Code, res.Body.String())
		}

		var resp domain.DeviceToken
		if err := json.Unmarshal(res.Body.Bytes(), &resp); err != nil {
			t.Fatal(err)
		}
		if resp.ID != "token-id-1" || resp.Token != "token-val" {
			t.Errorf("unexpected registered token: %+v", resp)
		}
	})

	t.Run("DELETE /v1/device-tokens/{id} deactivates token", func(t *testing.T) {
		calledID := ""
		store.retireDeviceTokenByIDFunc = func(ctx context.Context, tenantID, id string) error {
			if tenantID != "tenant" {
				return errors.New("invalid tenant")
			}
			calledID = id
			return nil
		}

		req := httptest.NewRequest(http.MethodDelete, "/v1/device-tokens/token-id-1", nil)
		req.Header.Set("Authorization", "Bearer test-key")
		res := httptest.NewRecorder()

		server.ServeHTTP(res, req)
		if res.Code != http.StatusNoContent {
			t.Fatalf("expected 204 No Content, got %d body=%s", res.Code, res.Body.String())
		}
		if calledID != "token-id-1" {
			t.Errorf("expected retire for ID %q, got %q", "token-id-1", calledID)
		}
	})

	t.Run("POST /v1/device-tokens/sync reconciles tokens", func(t *testing.T) {
		// Mock active list to return: [token-existing, token-to-drop]
		store.listActiveDeviceTokensFunc = func(ctx context.Context, tenantID, workspaceID, profileID string) ([]domain.DeviceToken, error) {
			return []domain.DeviceToken{
				{Token: "token-existing", Status: "active"},
				{Token: "token-to-drop", Status: "active"},
			}, nil
		}

		var registered []string
		store.registerDeviceTokenFunc = func(ctx context.Context, tenantID, workspaceID, appID, profileID, platform, provider, token string) (domain.DeviceToken, error) {
			registered = append(registered, token)
			return domain.DeviceToken{Token: token, Status: "active"}, nil
		}

		var retired []string
		store.retireDeviceTokenFunc = func(ctx context.Context, tenantID, appID, token string) error {
			retired = append(retired, token)
			return nil
		}

		// Client sends token-existing (refresh) and token-new (register)
		body := `{
			"profile_id": "prof-123",
			"tokens": [
				{"token": "token-existing", "platform": "ios", "provider": "fcm"},
				{"token": "token-new", "platform": "android", "provider": "fcm"}
			]
		}`
		req := httptest.NewRequest(http.MethodPost, "/v1/device-tokens/sync", strings.NewReader(body))
		req.Header.Set("Authorization", "Bearer test-key")
		req.Header.Set("Content-Type", "application/json")
		res := httptest.NewRecorder()

		server.ServeHTTP(res, req)
		if res.Code != http.StatusOK {
			t.Fatalf("expected 200 OK, got %d body=%s", res.Code, res.Body.String())
		}

		// Assertions:
		// token-existing and token-new should have been registered/refreshed
		if len(registered) != 2 || registered[0] != "token-existing" || registered[1] != "token-new" {
			t.Errorf("unexpected registered tokens list: %v", registered)
		}
		// token-to-drop should have been retired
		if len(retired) != 1 || retired[0] != "token-to-drop" {
			t.Errorf("expected retired list to contain token-to-drop, got %v", retired)
		}
	})
}
