package ports

import (
	"context"
	"time"

	"github.com/buildwithdmytro/openjourney/internal/domain"
)

type Store interface {
	Ready(context.Context) error
	Authenticate(context.Context, string) (domain.Principal, error)
	AuthenticateOIDC(context.Context, domain.OIDCClaims) (domain.Principal, error)
	CreateLocalSession(context.Context, string, string, time.Duration) (domain.AuthSession, error)
	RevokeLocalSession(context.Context, string) error
	AcceptEvents(context.Context, domain.Principal, []domain.Event) ([]string, error)
	GetProfile(context.Context, domain.Principal, string) (domain.Profile, []domain.Consent, error)
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
