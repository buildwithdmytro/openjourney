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
	GetProfileAppID(ctx context.Context, tenantID, workspaceID, profileID string) (string, error)
	CreateForm(context.Context, domain.Principal, domain.Form) (domain.Form, error)
	GetForm(context.Context, domain.Principal, string) (domain.Form, error)
	UpdateForm(context.Context, domain.Principal, domain.Form) (domain.Form, error)
	ListForms(context.Context, domain.Principal) ([]domain.Form, error)
	PublishForm(context.Context, domain.Principal, string, string, string, json.RawMessage) (domain.FormVersion, error)
	// Public form capture uses these methods without authenticating the visitor.
	GetPublishedForm(context.Context, string) (domain.Form, domain.FormVersion, error)
	RecordFormSubmission(context.Context, domain.Principal, string, int, json.RawMessage, json.RawMessage, string) error
	CreateLandingPage(context.Context, domain.Principal, domain.LandingPage) (domain.LandingPage, error)
	GetLandingPage(context.Context, domain.Principal, string) (domain.LandingPage, error)
	UpdateLandingPage(context.Context, domain.Principal, domain.LandingPage) (domain.LandingPage, error)
	ListLandingPages(context.Context, domain.Principal) ([]domain.LandingPage, error)
	PublishLandingPage(context.Context, domain.Principal, string, string, string, json.RawMessage) (domain.PageVersion, error)
	GetPublishedLandingPage(context.Context, string) (domain.LandingPage, domain.PageVersion, error)
	CreateAsset(context.Context, domain.Principal, domain.Asset) (domain.Asset, error)
	ListAssets(context.Context, domain.Principal) ([]domain.Asset, error)
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
	CreateAIGenerationRequest(context.Context, domain.Principal, string, json.RawMessage) (domain.AIGenerationRequest, error)
	GetAIGenerationRequest(context.Context, domain.Principal, string) (domain.AIGenerationRequest, error)
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
	EnforceRetention(context.Context, string) (domain.DataRetentionReport, error)
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
	CreateCompany(context.Context, domain.Principal, domain.Company, []domain.CompanyMember) (domain.Company, error)
	GetCompany(context.Context, domain.Principal, string) (domain.Company, error)
	UpdateCompany(context.Context, domain.Principal, domain.Company, []domain.CompanyMember) (domain.Company, error)
	ListCompanies(context.Context, domain.Principal) ([]domain.Company, error)
	CreateConnectorPipeline(context.Context, domain.Principal, domain.ConnectorPipeline) (domain.ConnectorPipeline, error)
	ListConnectorPipelines(context.Context, domain.Principal) ([]domain.ConnectorPipeline, error)
	GetConnectorPipeline(context.Context, domain.Principal, string) (domain.ConnectorPipeline, error)
	ListConnectorRuns(context.Context, domain.Principal, string) ([]domain.ConnectorRun, error)
	GetConnectorPipelineVersion(context.Context, domain.Principal, string) (domain.ConnectorPipelineVersion, error)
	UpdateConnectorPipeline(context.Context, domain.Principal, domain.ConnectorPipeline) (domain.ConnectorPipeline, error)
	PublishConnectorPipeline(context.Context, domain.Principal, string, string, string, json.RawMessage, string) (domain.ConnectorPipelineVersion, error)
	RecordConnectorRun(context.Context, domain.ConnectorRun) error
	ReplayConnectorRun(context.Context, domain.Principal, string) (string, error)

	CreateSendingIdentity(context.Context, domain.Principal, domain.SendingIdentity) (domain.SendingIdentity, error)
	GetSendingIdentity(context.Context, domain.Principal, string) (domain.SendingIdentity, error)
	ListSendingIdentities(context.Context, domain.Principal) ([]domain.SendingIdentity, error)

	CreateTemplate(context.Context, domain.Principal, domain.Template) (domain.Template, error)
	GetTemplate(context.Context, domain.Principal, string) (domain.Template, error)
	UpdateTemplate(context.Context, domain.Principal, domain.Template) (domain.Template, error)
	ListTemplates(context.Context, domain.Principal) ([]domain.Template, error)
	UpsertTrackedLink(ctx context.Context, tenantID string, templateID string, originalURL string) (string, error)
	CreateShortLink(context.Context, domain.Principal, domain.ShortLink) (domain.ShortLink, error)
	ListShortLinks(context.Context, domain.Principal) ([]domain.ShortLink, error)
	GetShortLinkBySlug(context.Context, string) (domain.ShortLink, error)

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

	RegisterDeviceToken(ctx context.Context, tenantID, workspaceID, appID, profileID, platform, provider, token string) (domain.DeviceToken, error)
	RetireDeviceToken(ctx context.Context, tenantID, appID, token string) error
	RetireDeviceTokenByID(ctx context.Context, tenantID, id string) error
	ListActiveDeviceTokens(ctx context.Context, tenantID, workspaceID, profileID string) ([]domain.DeviceToken, error)
	ListDeviceTokensByProfile(ctx context.Context, tenantID, workspaceID, profileID string) ([]domain.DeviceToken, error)

	CreateInAppMessage(ctx context.Context, tenantID, workspaceID, appID, profileID string, msg domain.InAppMessage) (domain.InAppMessage, error)
	GetInAppMessage(ctx context.Context, tenantID, msgID string) (domain.InAppMessage, error)
	ListInboxForProfile(ctx context.Context, tenantID, appID, profileID string, limit int) ([]domain.InAppMessage, error)
	ListInAppMessages(ctx context.Context, p domain.Principal, appID string) ([]domain.InAppMessage, error)

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

	CreateFeatureFlag(ctx context.Context, p domain.Principal, flag domain.FeatureFlag) (domain.FeatureFlag, error)
	GetFeatureFlag(ctx context.Context, p domain.Principal, id string) (domain.FeatureFlag, error)
	UpdateFeatureFlag(ctx context.Context, p domain.Principal, flag domain.FeatureFlag) (domain.FeatureFlag, error)
	ListFeatureFlags(ctx context.Context, p domain.Principal) ([]domain.FeatureFlag, error)
	ListActiveFlags(ctx context.Context, tenantID, appID, environment string) ([]domain.FeatureFlag, error)
	PublishFeatureFlag(ctx context.Context, p domain.Principal, flagID string, approverUserID string, manifestKey string) (domain.FeatureFlagVersion, error)

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
	AdvanceRunTx(ctx context.Context, runID string, run domain.JourneyRun, stepID string, nextStep *domain.JourneyStep, trans domain.JourneyTransition, messageIntents []domain.JourneyMessageIntent) error
	ClaimJourneyMessageIntent(ctx context.Context, workerID string) (domain.JourneyMessageIntent, bool, error)
	UpdateJourneyMessageIntent(ctx context.Context, intent domain.JourneyMessageIntent) error

	ClaimScheduledCampaign(ctx context.Context) (domain.Campaign, bool, error)
	SaveCampaignManifestAndJobs(ctx context.Context, campaignID string, manifestKey string, recipientCount int, segmentVersion int, templateVersion int, conversionGoal json.RawMessage, attributionWindow string, jobs []domain.DeliveryJob) error
	ClaimDeliveryJob(ctx context.Context, workerID string) (domain.DeliveryJob, bool, error)
	CompleteDeliveryJob(ctx context.Context, jobID string) error
	FailDeliveryJob(ctx context.Context, jobID string, errMsg string) error
	CreateDeliveryAttempt(ctx context.Context, attempt domain.DeliveryAttempt) (bool, error)
	UpdateDeliveryAttempt(ctx context.Context, campaignID, profileID, channel, endpoint, decision, reason, providerMsgID string, policySnapshot []byte, costMicros int64) error
	DeleteDeliveryAttempt(ctx context.Context, tenantID, campaignID, profileID, channel, endpoint string) error
	GetDeliveryAttempt(ctx context.Context, campaignID, profileID, channel, endpoint string) (domain.DeliveryAttempt, error)
	CampaignReport(ctx context.Context, p domain.Principal, campaignID string, query domain.ReportQuery) (domain.CampaignReport, error)
	JourneyReport(ctx context.Context, p domain.Principal, journeyID string, query domain.ReportQuery) (domain.JourneyReport, error)
	ExperimentReport(ctx context.Context, p domain.Principal, experimentID string, query domain.ReportQuery) (domain.ExperimentReport, error)
	FunnelOverTimeReport(ctx context.Context, p domain.Principal, campaignID string, query domain.ReportQuery) (domain.FunnelOverTimeReport, error)
	RetentionReport(ctx context.Context, p domain.Principal, campaignID string, query domain.ReportQuery) (domain.RetentionReport, error)
	GetOverview(ctx context.Context, p domain.Principal) (domain.Overview, error)
	ProposeExperimentOptimization(ctx context.Context, p domain.Principal, experimentID string) (domain.OptimizationProposal, error)
	ApproveExperimentOptimization(ctx context.Context, p domain.Principal, experimentID, proposalID string) (domain.ExperimentVersion, error)
	RolloutExperiment(ctx context.Context, p domain.Principal, experimentID string) (domain.ExperimentRollout, error)

	CreateSavedReport(ctx context.Context, p domain.Principal, report domain.SavedReport) (domain.SavedReport, error)
	GetSavedReport(ctx context.Context, p domain.Principal, id string) (domain.SavedReport, error)
	ListSavedReports(ctx context.Context, p domain.Principal) ([]domain.SavedReport, error)
	DeleteSavedReport(ctx context.Context, p domain.Principal, id string) error

	CreateCatalog(ctx context.Context, p domain.Principal, cat domain.Catalog) (domain.Catalog, error)
	GetCatalog(ctx context.Context, p domain.Principal, id string) (domain.Catalog, error)
	ListCatalogs(ctx context.Context, p domain.Principal) ([]domain.Catalog, error)
	UpdateCatalog(ctx context.Context, p domain.Principal, cat domain.Catalog) (domain.Catalog, error)
	DeleteCatalog(ctx context.Context, p domain.Principal, id string) error
	GetCatalogItem(ctx context.Context, p domain.Principal, catalogID, itemKey string) (domain.CatalogItem, error)
	ListCatalogItems(ctx context.Context, p domain.Principal, catalogID string, limit int) ([]domain.CatalogItem, error)

	CreateConnectedContentSource(ctx context.Context, p domain.Principal, src domain.ConnectedContentSource) (domain.ConnectedContentSource, error)
	GetConnectedContentSource(ctx context.Context, p domain.Principal, id string) (domain.ConnectedContentSource, error)
	ListConnectedContentSources(ctx context.Context, p domain.Principal) ([]domain.ConnectedContentSource, error)
	UpdateConnectedContentSource(ctx context.Context, p domain.Principal, src domain.ConnectedContentSource) (domain.ConnectedContentSource, error)
	DeleteConnectedContentSource(ctx context.Context, p domain.Principal, id string) error

	CreateAIProviderConfig(ctx context.Context, p domain.Principal, cfg domain.AIProviderConfig) (domain.AIProviderConfig, error)
	GetAIProviderConfig(ctx context.Context, p domain.Principal, id string) (domain.AIProviderConfig, error)
	GetDefaultAIProviderConfig(ctx context.Context, p domain.Principal) (domain.AIProviderConfig, error)
	UpdateAIProviderConfig(ctx context.Context, p domain.Principal, cfg domain.AIProviderConfig) (domain.AIProviderConfig, error)
	ListAIProviderConfigs(ctx context.Context, p domain.Principal) ([]domain.AIProviderConfig, error)
	DeleteAIProviderConfig(ctx context.Context, p domain.Principal, id string) error

	GetAIBudgetUsage(ctx context.Context, tenantID, workspaceID string, period string) (domain.AIBudgetUsage, error)
	IncrementAIBudgetUsage(ctx context.Context, tenantID, workspaceID string, period string, costCents, inputTokens, outputTokens int64) error
	ListAIActivity(ctx context.Context, p domain.Principal, limit int) ([]domain.AIActivity, error)

	CreatePrompt(ctx context.Context, p domain.Principal, prompt domain.Prompt) (domain.Prompt, error)
	GetPrompt(ctx context.Context, p domain.Principal, id string) (domain.Prompt, error)
	GetPromptByName(ctx context.Context, p domain.Principal, name string) (domain.Prompt, error)
	ListPrompts(ctx context.Context, p domain.Principal) ([]domain.Prompt, error)
	UpdatePrompt(ctx context.Context, p domain.Principal, prompt domain.Prompt) (domain.Prompt, error)
	DeletePrompt(ctx context.Context, p domain.Principal, id string) error

	CreatePromptVersion(ctx context.Context, p domain.Principal, pv domain.PromptVersion) (domain.PromptVersion, error)
	GetPromptVersion(ctx context.Context, p domain.Principal, id string) (domain.PromptVersion, error)
	GetPromptVersionByNumber(ctx context.Context, p domain.Principal, promptID string, version int) (domain.PromptVersion, error)
	ListPromptVersions(ctx context.Context, p domain.Principal, promptID string) ([]domain.PromptVersion, error)
	PublishPromptVersion(ctx context.Context, p domain.Principal, promptID string, version int, approverUserID string, manifestKey string) (domain.PromptVersion, error)
	SetPromptVersionEvalStatus(ctx context.Context, p domain.Principal, id string, evalStatus string) error

	CreateScoringModel(ctx context.Context, p domain.Principal, model domain.ScoringModel) (domain.ScoringModel, error)
	GetScoringModel(ctx context.Context, p domain.Principal, id string) (domain.ScoringModel, error)
	GetScoringModelByName(ctx context.Context, p domain.Principal, name string) (domain.ScoringModel, error)
	ListScoringModels(ctx context.Context, p domain.Principal) ([]domain.ScoringModel, error)
	UpdateScoringModel(ctx context.Context, p domain.Principal, model domain.ScoringModel) (domain.ScoringModel, error)
	DeleteScoringModel(ctx context.Context, p domain.Principal, id string) error

	CreateScoringModelVersion(ctx context.Context, p domain.Principal, sv domain.ScoringModelVersion) (domain.ScoringModelVersion, error)
	GetScoringModelVersion(ctx context.Context, p domain.Principal, id string) (domain.ScoringModelVersion, error)
	GetScoringModelVersionByNumber(ctx context.Context, p domain.Principal, modelID string, version int) (domain.ScoringModelVersion, error)
	ListScoringModelVersions(ctx context.Context, p domain.Principal, modelID string) ([]domain.ScoringModelVersion, error)
	PublishScoringModelVersion(ctx context.Context, p domain.Principal, modelID string, version int, approverUserID string, manifestKey string) (domain.ScoringModelVersion, error)
	SetScoringModelVersionEvalStatus(ctx context.Context, p domain.Principal, id string, evalStatus string) error

	CreateScoringRequest(ctx context.Context, p domain.Principal, scoringModelID string, segmentID string) (domain.ScoringRequest, error)
	GetScoringRequest(ctx context.Context, p domain.Principal, id string) (domain.ScoringRequest, error)
	GetScoringJob(ctx context.Context, id string) (domain.ScoringJob, error)
	MarkScoringProcessing(ctx context.Context, id string) error
	CompleteScoring(ctx context.Context, id string) error
	FailScoring(ctx context.Context, id string, message string) error
	UpsertProfileScores(ctx context.Context, scores []domain.ProfileScore) error
	ListProfileScores(ctx context.Context, p domain.Principal, profileID string) ([]domain.ProfileScore, error)
	GetEventCount(ctx context.Context, tenantID, workspaceID, externalID, anonymousID, eventType string, days int) (int64, error)

	CreateFieldClassification(ctx context.Context, p domain.Principal, classification domain.FieldClassification) (domain.FieldClassification, error)
	GetFieldClassification(ctx context.Context, p domain.Principal, id string) (domain.FieldClassification, error)
	ListFieldClassifications(ctx context.Context, p domain.Principal, entityType string) ([]domain.FieldClassification, error)
	UpdateFieldClassification(ctx context.Context, p domain.Principal, classification domain.FieldClassification) (domain.FieldClassification, error)
	DeleteFieldClassification(ctx context.Context, p domain.Principal, id string) error

	CreateEvalDataset(ctx context.Context, p domain.Principal, dataset domain.EvalDataset) (domain.EvalDataset, error)
	GetEvalDataset(ctx context.Context, p domain.Principal, id string) (domain.EvalDataset, error)
	ListEvalDatasets(ctx context.Context, p domain.Principal) ([]domain.EvalDataset, error)
	UpdateEvalDataset(ctx context.Context, p domain.Principal, dataset domain.EvalDataset) (domain.EvalDataset, error)
	DeleteEvalDataset(ctx context.Context, p domain.Principal, id string) error
	CreateEvalCase(ctx context.Context, p domain.Principal, evalCase domain.EvalCase) (domain.EvalCase, error)
	GetEvalCase(ctx context.Context, p domain.Principal, id string) (domain.EvalCase, error)
	ListEvalCases(ctx context.Context, p domain.Principal, datasetID string) ([]domain.EvalCase, error)
	UpdateEvalCase(ctx context.Context, p domain.Principal, evalCase domain.EvalCase) (domain.EvalCase, error)
	DeleteEvalCase(ctx context.Context, p domain.Principal, id string) error
	CreateEvalRun(ctx context.Context, p domain.Principal, run domain.EvalRun) (domain.EvalRun, error)
	GetEvalRun(ctx context.Context, p domain.Principal, id string) (domain.EvalRun, error)
	ListEvalRuns(ctx context.Context, p domain.Principal, datasetID string) ([]domain.EvalRun, error)

	// Extensions registry
	CreateExtension(ctx context.Context, p domain.Principal, ext domain.Extension) (domain.Extension, error)
	GetExtension(ctx context.Context, p domain.Principal, id string) (domain.Extension, error)
	GetExtensionByName(ctx context.Context, p domain.Principal, name string) (domain.Extension, error)
	ListExtensions(ctx context.Context, p domain.Principal) ([]domain.Extension, error)
	ListActiveChannelProvidersSystem(ctx context.Context) ([]domain.Extension, error)
	UpdateExtension(ctx context.Context, p domain.Principal, ext domain.Extension) (domain.Extension, error)
	DeleteExtension(ctx context.Context, p domain.Principal, id string) (domain.Extension, error)

	CreateExtensionVersion(ctx context.Context, p domain.Principal, ev domain.ExtensionVersion) (domain.ExtensionVersion, error)
	GetExtensionVersion(ctx context.Context, p domain.Principal, id string) (domain.ExtensionVersion, error)
	GetExtensionVersionByNumber(ctx context.Context, p domain.Principal, extensionID string, version int) (domain.ExtensionVersion, error)
	ListExtensionVersions(ctx context.Context, p domain.Principal, extensionID string) ([]domain.ExtensionVersion, error)
	PublishExtensionVersion(ctx context.Context, p domain.Principal, extensionID string, version int, approverUserID string, manifestKey string) (domain.ExtensionVersion, error)

	// Extension Configs & Grants CRUD
	UpsertExtensionConfig(ctx context.Context, p domain.Principal, cfg domain.ExtensionConfig) (domain.ExtensionConfig, error)
	GetExtensionConfig(ctx context.Context, p domain.Principal, extensionID string) (domain.ExtensionConfig, error)
	DeleteExtensionConfig(ctx context.Context, p domain.Principal, extensionID string) error

	CreateExtensionGrant(ctx context.Context, p domain.Principal, grant domain.ExtensionGrant) (domain.ExtensionGrant, error)
	ListExtensionGrants(ctx context.Context, p domain.Principal, extensionID string) ([]domain.ExtensionGrant, error)
	DeleteExtensionGrant(ctx context.Context, p domain.Principal, extensionID string, scope string) error

	// Extension Activity, Health & Subscriptions
	RecordExtensionActivity(ctx context.Context, p domain.Principal, act domain.ExtensionActivity) (domain.ExtensionActivity, error)
	ListExtensionActivities(ctx context.Context, p domain.Principal, extensionID string, limit int) ([]domain.ExtensionActivity, error)
	GetExtensionHealth(ctx context.Context, p domain.Principal, extensionID string) (domain.ExtensionHealth, error)
	UpdateExtensionHealth(ctx context.Context, p domain.Principal, health domain.ExtensionHealth) (domain.ExtensionHealth, error)
	UpsertExtensionSubscriptions(ctx context.Context, p domain.Principal, extensionID string, eventTypes []string) error
	ListExtensionSubscriptions(ctx context.Context, p domain.Principal, extensionID string) ([]string, error)
	GetExtensionBudgetUsage(ctx context.Context, tenantID, workspaceID, extensionID, period string) (int64, error)
	GetExtensionInvocationCountLastMin(ctx context.Context, tenantID, workspaceID, extensionID string) (int, error)
	ListActiveIngestionTransforms(ctx context.Context, p domain.Principal, eventType string) ([]domain.Extension, error)
}

// AIActivityRecorder is implemented by stores that persist the immutable AI
// activity record and its corresponding ai.action audit event.
type AIActivityRecorder interface {
	RecordAIActivity(context.Context, domain.Principal, domain.AIActivity) (domain.AIActivity, error)
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
