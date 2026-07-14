package ports

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/buildwithdmytro/openjourney/internal/domain"
)

var ErrNotFound = errors.New("not found")

type Store interface {
	Ready(context.Context) error
	Authenticate(context.Context, string) (domain.Principal, error)
	AuthenticateOIDC(context.Context, domain.OIDCClaims) (domain.Principal, error)
	CreateLocalSession(context.Context, string, string, time.Duration) (domain.AuthSession, error)
	RevokeLocalSession(context.Context, string) error
	AcceptEvents(context.Context, domain.Principal, []domain.Event) ([]string, error)
	GetProfile(context.Context, domain.Principal, string) (domain.Profile, []domain.Consent, error)
	GetProfileByID(ctx context.Context, tenantID, appID, profileID string) (domain.Profile, error)
	GetProfileByIDSystem(ctx context.Context, tenantID, workspaceID, profileID string) (domain.Profile, error)
	ClaimProjectionJob(context.Context) (domain.AcceptedEvent, bool, error)
	ProjectEvent(context.Context, domain.AcceptedEvent) error
	FailProjectionJob(context.Context, string, error) error
	ValidateEventSchema(context.Context, domain.Principal, domain.Event) error
	ListEventSchemas(context.Context, domain.Principal) ([]domain.EventSchema, error)
	CreateEventSchema(context.Context, domain.Principal, domain.EventSchema) (domain.EventSchema, error)
	ListAPIKeys(context.Context, domain.Principal) ([]domain.APIKey, error)
	CreateAPIKey(context.Context, domain.Principal, string, []string, *time.Time) (domain.APIKey, string, error)
	RevokeAPIKey(context.Context, domain.Principal, string) error
	CreatePrivacyRequest(context.Context, domain.Principal, string, string) (domain.PrivacyRequest, error)
	GetPrivacyRequest(context.Context, domain.Principal, string) (domain.PrivacyRequest, error)
	QueueStatus(context.Context, domain.Principal) ([]domain.QueueStatus, error)
	ListDeadLetters(context.Context, domain.Principal, string, int) ([]domain.DeadLetterItem, error)
	RetryDeadLetter(context.Context, domain.Principal, string, string) error
	DiscardDeadLetter(context.Context, domain.Principal, string, string) error
	ClaimOutboxEvent(context.Context) (domain.OutboxEvent, bool, error)
	CompleteOutboxEvent(context.Context, string) error
	FailOutboxEvent(context.Context, string, error) error
	ClaimOperationJob(context.Context) (domain.OperationJob, bool, error)
	CompleteOperationJob(context.Context, string) error
	FailOperationJob(context.Context, string, error) error
	ExportPrivacyData(context.Context, string) (domain.PrivacyData, error)
	CompletePrivacyExport(context.Context, string, string) error
	DeletePrivacyData(context.Context, string) ([]string, error)
	EnforceRetention(context.Context, string) (domain.RetentionReport, error)
	VerifyReplay(context.Context, domain.Principal) (domain.ReplayReport, error)
	ListRoles(context.Context, domain.Principal) ([]domain.Role, error)
	CreateRole(context.Context, domain.Principal, string, []string) (domain.Role, error)
	ListUsers(context.Context, domain.Principal) ([]domain.User, error)
	CreateUser(context.Context, domain.Principal, domain.User) (domain.User, error)
	ListAuditEvents(context.Context, domain.Principal, int) ([]domain.AuditEvent, error)
	CreateSegment(context.Context, domain.Principal, domain.Segment) (domain.Segment, error)
	GetSegment(context.Context, domain.Principal, string) (domain.Segment, error)
	UpdateSegment(context.Context, domain.Principal, domain.Segment) (domain.Segment, error)
	ListSegments(context.Context, domain.Principal) ([]domain.Segment, error)
	SetSegmentMembers(context.Context, domain.Principal, string, []domain.SegmentMember) error
	IsProfileInSegment(ctx context.Context, p domain.Principal, segmentID string, profileID string) (bool, error)
	UpdateProfileAttributes(ctx context.Context, p domain.Principal, profileID string, attrs map[string]any) error
	PreviewSegment(context.Context, domain.Principal, string) (int, map[string]int, error)
	ResolveSegment(context.Context, domain.Principal, string) ([]string, error)

	CreateSendingIdentity(context.Context, domain.Principal, domain.SendingIdentity) (domain.SendingIdentity, error)
	GetSendingIdentity(context.Context, domain.Principal, string) (domain.SendingIdentity, error)
	ListSendingIdentities(context.Context, domain.Principal) ([]domain.SendingIdentity, error)

	CreateTemplate(context.Context, domain.Principal, domain.Template) (domain.Template, error)
	GetTemplate(context.Context, domain.Principal, string) (domain.Template, error)
	UpdateTemplate(context.Context, domain.Principal, domain.Template) (domain.Template, error)
	ListTemplates(context.Context, domain.Principal) ([]domain.Template, error)
	UpsertTrackedLink(ctx context.Context, tenantID string, templateID string, originalURL string) (string, error)

	IsSuppressed(ctx context.Context, p domain.Principal, channel, endpoint string) (bool, error)
	SuppressEndpoint(ctx context.Context, p domain.Principal, channel, endpoint, reason string) error
	RemoveSuppression(ctx context.Context, p domain.Principal, channel, endpoint string) error
	ListSuppressions(ctx context.Context, p domain.Principal) ([]domain.Suppression, error)
	LatestConsent(ctx context.Context, p domain.Principal, profileID, channel, topic string) (domain.Consent, error)
	SentCountSince(ctx context.Context, p domain.Principal, profileID string, since time.Time) (int, error)
	GetTenantFatigueQuotas(ctx context.Context, p domain.Principal) (int, int, error)
	GetTenantQuietHours(ctx context.Context, p domain.Principal) (*int, *int, string, error)
	GetProfileEmails(ctx context.Context, tenantID string, profileIDs []string) (map[string]string, error)
	GetProfilePhones(ctx context.Context, tenantID string, profileIDs []string) (map[string]string, error)
	GetProfileByPhone(ctx context.Context, tenantID string, phone string) (domain.Profile, error)
	GetSendingIdentityByProviderConfig(ctx context.Context, provider string, configKey string, configVal string) (domain.SendingIdentity, error)
	GetFirstAppID(ctx context.Context, tenantID, workspaceID string) (string, error)

	CreateCampaign(ctx context.Context, p domain.Principal, c domain.Campaign) (domain.Campaign, error)
	GetCampaign(ctx context.Context, p domain.Principal, id string) (domain.Campaign, error)
	GetCampaignSystem(ctx context.Context, tenantID, id string) (domain.Campaign, error)
	UpdateCampaign(ctx context.Context, p domain.Principal, c domain.Campaign) (domain.Campaign, error)
	ListCampaigns(ctx context.Context, p domain.Principal) ([]domain.Campaign, error)
	CreateExperiment(ctx context.Context, p domain.Principal, experiment domain.Experiment) (domain.Experiment, error)
	GetExperiment(ctx context.Context, p domain.Principal, id string) (domain.Experiment, error)
	UpdateExperiment(ctx context.Context, p domain.Principal, experiment domain.Experiment) (domain.Experiment, error)
	ListExperiments(ctx context.Context, p domain.Principal) ([]domain.Experiment, error)
	AssignExperiment(ctx context.Context, p domain.Principal, experimentID, profileID, variant string) (domain.ExperimentAssignment, error)
	SetDeliveryAttemptExperiment(ctx context.Context, tenantID, campaignID, profileID, channel, experimentID, variant string) error
	CreateJourney(ctx context.Context, p domain.Principal, j domain.Journey) (domain.Journey, error)
	GetJourney(ctx context.Context, p domain.Principal, id string) (domain.Journey, error)
	UpdateJourney(ctx context.Context, p domain.Principal, j domain.Journey) (domain.Journey, error)
	ListJourneys(ctx context.Context, p domain.Principal) ([]domain.Journey, error)
	PublishJourney(ctx context.Context, p domain.Principal, journeyID string, approverUserID string, manifestKey string) (domain.JourneyVersion, error)
	GetJourneyVersion(ctx context.Context, tenantID, versionID string) (domain.JourneyVersion, error)
	GetJourneyVersionNumber(ctx context.Context, p domain.Principal, journeyID string, version int) (domain.JourneyVersion, error)
	SetJourneyVersionStatus(ctx context.Context, p domain.Principal, journeyID string, version int, status string) error
	ListActiveScheduledJourneyVersions(ctx context.Context) ([]domain.JourneyVersion, error)
	EvaluateAudience(ctx context.Context, p domain.Principal, profileID string, dsl json.RawMessage) (bool, error)
	CreateJourneyRun(ctx context.Context, run domain.JourneyRun) (bool, error)
	EnrollJourneyRun(ctx context.Context, run domain.JourneyRun, initialStep domain.JourneyStep) (string, bool, error)
	GetJourneyRun(ctx context.Context, p domain.Principal, runID string) (domain.JourneyRun, error)
	GetJourneyRunSystem(ctx context.Context, tenantID, runID string) (domain.JourneyRun, error)
	GetJourneyRunsForProfile(ctx context.Context, tenantID, versionID, profileID string) ([]domain.JourneyRun, error)
	GetJourneyRuns(ctx context.Context, p domain.Principal, journeyID string) ([]domain.JourneyRun, error)
	GetJourneyTransitions(ctx context.Context, p domain.Principal, runID string) ([]domain.JourneyTransition, error)
	UpdateJourneyRun(ctx context.Context, p domain.Principal, run domain.JourneyRun) (domain.JourneyRun, error)
	CancelJourneyRun(ctx context.Context, p domain.Principal, journeyID string, runID string) error
	GetJourneyDLQ(ctx context.Context, p domain.Principal) ([]domain.JourneyStep, []domain.JourneyMessageIntent, error)
	RetryJourneyStep(ctx context.Context, p domain.Principal, stepID string) error
	RetryJourneyMessageIntent(ctx context.Context, p domain.Principal, intentID string) error
	ClaimJourneyStep(ctx context.Context) (domain.JourneyStep, bool, error)
	CompleteJourneyStep(ctx context.Context, stepID string) error
	FailJourneyStep(ctx context.Context, stepID string, errMsg string) error
	RescheduleJourneyStep(ctx context.Context, stepID string, availableAt time.Time) error
	InsertJourneyStep(ctx context.Context, step domain.JourneyStep) error
	RecordTransition(ctx context.Context, trans domain.JourneyTransition) error
	AdvanceRunTx(ctx context.Context, runID string, run domain.JourneyRun, stepID string, nextStep *domain.JourneyStep, trans domain.JourneyTransition, messageIntent *domain.JourneyMessageIntent) error
	ClaimJourneyMessageIntent(ctx context.Context, workerID string) (domain.JourneyMessageIntent, bool, error)
	UpdateJourneyMessageIntent(ctx context.Context, intent domain.JourneyMessageIntent) error

	ClaimScheduledCampaign(ctx context.Context) (domain.Campaign, bool, error)
	SaveCampaignManifestAndJobs(ctx context.Context, campaignID string, manifestKey string, recipientCount int, segmentVersion int, templateVersion int, conversionGoal json.RawMessage, attributionWindow string, jobs []domain.DeliveryJob) error
	ClaimDeliveryJob(ctx context.Context, workerID string) (domain.DeliveryJob, bool, error)
	CompleteDeliveryJob(ctx context.Context, jobID string) error
	FailDeliveryJob(ctx context.Context, jobID string, errMsg string) error
	CreateDeliveryAttempt(ctx context.Context, attempt domain.DeliveryAttempt) (bool, error)
	UpdateDeliveryAttempt(ctx context.Context, campaignID, profileID, channel, decision, reason, providerMsgID string, policySnapshot []byte) error
	DeleteDeliveryAttempt(ctx context.Context, tenantID, campaignID, profileID, channel string) error
	GetDeliveryAttempt(ctx context.Context, campaignID, profileID, channel string) (domain.DeliveryAttempt, error)
	CampaignReport(ctx context.Context, p domain.Principal, campaignID string) (domain.CampaignReport, error)
	JourneyReport(ctx context.Context, p domain.Principal, journeyID string) (domain.JourneyReport, error)
	ExperimentReport(ctx context.Context, p domain.Principal, experimentID string) (domain.ExperimentReport, error)
	RolloutExperiment(ctx context.Context, p domain.Principal, experimentID string) (domain.ExperimentRollout, error)
}

type TokenVerifier interface {
	Verify(context.Context, string) (domain.OIDCClaims, error)
}

type EventPublisher interface {
	Publish(context.Context, domain.OutboxEvent) error
	Close()
}

type BlobStore interface {
	Put(context.Context, string, []byte, string) error
	Get(context.Context, string) ([]byte, error)
	Delete(context.Context, string) error
}
