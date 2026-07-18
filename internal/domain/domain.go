package domain

import (
	"bytes"
	"encoding/json"
	"errors"
	"strings"
	"time"
)

type Principal struct {
	TenantID    string
	WorkspaceID string
	AppID       string
	KeyID       string
	UserID      string
	ActorType   string
	Scopes      []string
}

func (p Principal) HasScope(required string) bool {
	for _, scope := range p.Scopes {
		if scope == required || scope == "*" {
			return true
		}
	}
	return false
}

type Event struct {
	Type               string          `json:"event_type"`
	SchemaVersion      int             `json:"schema_version"`
	ExternalID         string          `json:"external_id,omitempty"`
	AnonymousID        string          `json:"anonymous_id,omitempty"`
	IdempotencyKey     string          `json:"idempotency_key"`
	OccurredAt         time.Time       `json:"occurred_at"`
	Source             string          `json:"source,omitempty"`
	SourceEventID      string          `json:"source_event_id,omitempty"`
	CorrelationID      string          `json:"correlation_id,omitempty"`
	CausationID        string          `json:"causation_id,omitempty"`
	Traceparent        string          `json:"traceparent,omitempty"`
	DataClassification string          `json:"data_classification,omitempty"`
	ConsentContext     json.RawMessage `json:"consent_context,omitempty"`
	Payload            json.RawMessage `json:"payload"`
}

func (e Event) Validate(now time.Time) error {
	if strings.TrimSpace(e.Type) == "" {
		return errors.New("event_type is required")
	}
	if e.SchemaVersion < 1 {
		return errors.New("schema_version must be at least 1")
	}
	if e.ExternalID == "" && e.AnonymousID == "" {
		return errors.New("external_id or anonymous_id is required")
	}
	if strings.TrimSpace(e.IdempotencyKey) == "" {
		return errors.New("idempotency_key is required")
	}
	if e.OccurredAt.IsZero() {
		return errors.New("occurred_at is required")
	}
	if e.OccurredAt.After(now.Add(24 * time.Hour)) {
		return errors.New("occurred_at cannot be more than 24 hours in the future")
	}
	if len(e.Payload) == 0 || !json.Valid(e.Payload) {
		return errors.New("payload must be valid JSON")
	}
	if len(e.ConsentContext) > 0 && (!json.Valid(e.ConsentContext) ||
		!bytes.HasPrefix(bytes.TrimSpace(e.ConsentContext), []byte("{"))) {
		return errors.New("consent_context must be a JSON object")
	}
	if e.DataClassification != "" && e.DataClassification != "public" &&
		e.DataClassification != "internal" && e.DataClassification != "confidential" &&
		e.DataClassification != "restricted" {
		return errors.New("data_classification must be public, internal, confidential, or restricted")
	}
	if !bytes.HasPrefix(bytes.TrimSpace(e.Payload), []byte("{")) {
		return errors.New("payload must be a JSON object")
	}
	switch e.Type {
	case "profile.updated":
		var body struct {
			Attributes map[string]any `json:"attributes"`
		}
		if err := json.Unmarshal(e.Payload, &body); err != nil || body.Attributes == nil {
			return errors.New("profile.updated payload requires an attributes object")
		}
	case "consent.changed":
		var body struct {
			Channel string `json:"channel"`
			State   string `json:"state"`
		}
		if err := json.Unmarshal(e.Payload, &body); err != nil || strings.TrimSpace(body.Channel) == "" {
			return errors.New("consent.changed payload requires channel")
		}
		if body.State != "subscribed" && body.State != "unsubscribed" {
			return errors.New("consent.changed state must be subscribed or unsubscribed")
		}
	case "identity.alias":
		var body struct {
			Namespace string `json:"namespace"`
			Value     string `json:"value"`
		}
		if err := json.Unmarshal(e.Payload, &body); err != nil ||
			strings.TrimSpace(body.Namespace) == "" || strings.TrimSpace(body.Value) == "" {
			return errors.New("identity.alias requires namespace and value")
		}
	case "identity.merge":
		var body struct {
			SourceExternalID string `json:"source_external_id"`
		}
		if err := json.Unmarshal(e.Payload, &body); err != nil ||
			strings.TrimSpace(body.SourceExternalID) == "" || strings.TrimSpace(e.ExternalID) == "" {
			return errors.New("identity.merge requires event external_id as target and source_external_id")
		}
	case "email.sent":
		var body struct {
			TemplateID string `json:"template_id"`
			DispatchID string `json:"dispatch_id"`
			Channel    string `json:"channel"`
		}
		if err := json.Unmarshal(e.Payload, &body); err != nil ||
			strings.TrimSpace(body.TemplateID) == "" ||
			strings.TrimSpace(body.DispatchID) == "" ||
			strings.TrimSpace(body.Channel) == "" {
			return errors.New("email.sent payload requires template_id, dispatch_id, and channel")
		}
	case "email.opened":
		var body struct {
			TemplateID string `json:"template_id"`
			DispatchID string `json:"dispatch_id"`
		}
		if err := json.Unmarshal(e.Payload, &body); err != nil ||
			strings.TrimSpace(body.TemplateID) == "" ||
			strings.TrimSpace(body.DispatchID) == "" {
			return errors.New("email.opened payload requires template_id and dispatch_id")
		}
	case "link.clicked":
		var body struct {
			TemplateID string `json:"template_id"`
			DispatchID string `json:"dispatch_id"`
			URL        string `json:"url"`
		}
		if err := json.Unmarshal(e.Payload, &body); err != nil ||
			strings.TrimSpace(body.TemplateID) == "" ||
			strings.TrimSpace(body.DispatchID) == "" ||
			strings.TrimSpace(body.URL) == "" {
			return errors.New("link.clicked payload requires template_id, dispatch_id, and url")
		}
	case "message.sent":
		var body struct {
			CampaignID string `json:"campaign_id"`
			JourneyID  string `json:"journey_id"`
			Channel    string `json:"channel"`
			Endpoint   string `json:"endpoint"`
		}
		if err := json.Unmarshal(e.Payload, &body); err != nil ||
			(strings.TrimSpace(body.CampaignID) == "" && strings.TrimSpace(body.JourneyID) == "") ||
			strings.TrimSpace(body.Channel) == "" ||
			strings.TrimSpace(body.Endpoint) == "" {
			return errors.New("message.sent payload requires campaign_id or journey_id, channel, and endpoint")
		}
	case "message.delivered":
		var body struct {
			CampaignID string `json:"campaign_id"`
			Endpoint   string `json:"endpoint"`
		}
		if err := json.Unmarshal(e.Payload, &body); err != nil ||
			strings.TrimSpace(body.CampaignID) == "" ||
			strings.TrimSpace(body.Endpoint) == "" {
			return errors.New("message.delivered payload requires campaign_id and endpoint")
		}
	case "message.bounced":
		var body struct {
			CampaignID string `json:"campaign_id"`
			Endpoint   string `json:"endpoint"`
		}
		if err := json.Unmarshal(e.Payload, &body); err != nil ||
			strings.TrimSpace(body.CampaignID) == "" ||
			strings.TrimSpace(body.Endpoint) == "" {
			return errors.New("message.bounced payload requires campaign_id and endpoint")
		}
	case "message.complained":
		var body struct {
			CampaignID string `json:"campaign_id"`
			Endpoint   string `json:"endpoint"`
		}
		if err := json.Unmarshal(e.Payload, &body); err != nil ||
			strings.TrimSpace(body.CampaignID) == "" ||
			strings.TrimSpace(body.Endpoint) == "" {
			return errors.New("message.complained payload requires campaign_id and endpoint")
		}
	}
	return nil
}

type AcceptedEvent struct {
	ID                 string
	Principal          Principal
	Type               string
	SchemaVersion      int
	ExternalID         string
	AnonymousID        string
	IdempotencyKey     string
	OccurredAt         time.Time
	ReceivedAt         time.Time
	Source             string
	SourceEventID      string
	CorrelationID      string
	CausationID        string
	Traceparent        string
	DataClassification string
	ConsentContext     json.RawMessage
	Payload            json.RawMessage
}

type Profile struct {
	ID          string          `json:"id"`
	ExternalID  string          `json:"external_id,omitempty"`
	AnonymousID string          `json:"anonymous_id,omitempty"`
	Attributes  json.RawMessage `json:"attributes"`
	Version     int64           `json:"version"`
	UpdatedAt   time.Time       `json:"updated_at"`
}

type Consent struct {
	ProfileID  string    `json:"profile_id"`
	Channel    string    `json:"channel"`
	Topic      string    `json:"topic"`
	State      string    `json:"state"`
	OccurredAt time.Time `json:"occurred_at"`
}

type DeviceToken struct {
	ID          string    `json:"id"`
	TenantID    string    `json:"tenant_id"`
	WorkspaceID string    `json:"workspace_id"`
	AppID       string    `json:"app_id"`
	ProfileID   string    `json:"profile_id"`
	Platform    string    `json:"platform"` // ios, android, web
	Provider    string    `json:"provider"` // fcm, apns, http, fake
	Token       string    `json:"token"`
	Status      string    `json:"status"` // active, retired
	LastSeenAt  time.Time `json:"last_seen_at"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
}

type EventSchema struct {
	ID            string          `json:"id"`
	EventType     string          `json:"event_type"`
	Version       int             `json:"version"`
	Schema        json.RawMessage `json:"schema"`
	Status        string          `json:"status"`
	Compatibility string          `json:"compatibility"`
	CreatedAt     time.Time       `json:"created_at"`
}

type Segment struct {
	ID          string          `json:"id"`
	TenantID    string          `json:"tenant_id"`
	WorkspaceID string          `json:"workspace_id"`
	Name        string          `json:"name"`
	Description string          `json:"description,omitempty"`
	Type        string          `json:"type"`   // static, dynamic, snapshot
	Status      string          `json:"status"` // draft, active, archived
	DSL         json.RawMessage `json:"dsl"`
	Version     int             `json:"version"`
	CreatedAt   time.Time       `json:"created_at"`
	UpdatedAt   time.Time       `json:"updated_at"`
}

type SegmentMember struct {
	SegmentID  string    `json:"segment_id"`
	ProfileID  string    `json:"profile_id"`
	TenantID   string    `json:"tenant_id"`
	Membership string    `json:"membership"` // include, exclude
	CreatedAt  time.Time `json:"created_at"`
}

type SendingIdentity struct {
	ID          string          `json:"id"`
	TenantID    string          `json:"tenant_id"`
	WorkspaceID string          `json:"workspace_id"`
	Channel     string          `json:"channel"` // email, webhook
	FromAddress *string         `json:"from_address,omitempty"`
	FromName    *string         `json:"from_name,omitempty"`
	ReplyTo     *string         `json:"reply_to,omitempty"`
	Provider    string          `json:"provider"` // ses, webhook
	Config      json.RawMessage `json:"config"`
	MaxSendRate int             `json:"max_send_rate"`
	Verified    bool            `json:"verified"`
	CreatedAt   time.Time       `json:"created_at"`
}

type Template struct {
	ID                string            `json:"id"`
	TenantID          string            `json:"tenant_id"`
	WorkspaceID       string            `json:"workspace_id"`
	Name              string            `json:"name"`
	Channel           string            `json:"channel"` // email, webhook
	SubjectTemplate   *string           `json:"subject_template,omitempty"`
	HTMLTemplate      *string           `json:"html_template,omitempty"`
	TextTemplate      *string           `json:"text_template,omitempty"`
	BodyTemplate      *string           `json:"body_template,omitempty"`
	TitleTemplate     *string           `json:"title_template,omitempty"`
	PushData          map[string]string `json:"push_data,omitempty"`
	SendingIdentityID *string           `json:"sending_identity_id,omitempty"`
	Version           int               `json:"version"`
	CreatedAt         time.Time         `json:"created_at"`
	UpdatedAt         time.Time         `json:"updated_at"`
}

type APIKey struct {
	ID         string     `json:"id"`
	Name       string     `json:"name"`
	Scopes     []string   `json:"scopes"`
	ExpiresAt  *time.Time `json:"expires_at,omitempty"`
	RevokedAt  *time.Time `json:"revoked_at,omitempty"`
	LastUsedAt *time.Time `json:"last_used_at,omitempty"`
	CreatedAt  time.Time  `json:"created_at"`
}

type PrivacyRequest struct {
	ID          string     `json:"id"`
	ExternalID  string     `json:"external_id"`
	RequestType string     `json:"request_type"`
	Status      string     `json:"status"`
	ArtifactKey string     `json:"artifact_key,omitempty"`
	Error       string     `json:"error,omitempty"`
	CreatedAt   time.Time  `json:"created_at"`
	CompletedAt *time.Time `json:"completed_at,omitempty"`
}

type QueueStatus struct {
	Queue      string `json:"queue"`
	Pending    int64  `json:"pending"`
	Processing int64  `json:"processing"`
	Dead       int64  `json:"dead"`
}

type DeadLetterItem struct {
	Queue     string          `json:"queue"`
	ID        string          `json:"id"`
	SubjectID string          `json:"subject_id,omitempty"`
	Kind      string          `json:"kind"`
	Attempts  int             `json:"attempts"`
	LastError string          `json:"last_error,omitempty"`
	Payload   json.RawMessage `json:"payload,omitempty"`
	CreatedAt time.Time       `json:"created_at"`
}

type OIDCClaims struct {
	Issuer      string
	Subject     string
	Email       string
	Name        string
	TenantID    string
	WorkspaceID string
	AppID       string
}

type OutboxEvent struct {
	ID           string
	TenantID     string
	Topic        string
	PartitionKey string
	EventID      string
	Payload      []byte
}

type OperationJob struct {
	ID          string
	TenantID    string
	WorkspaceID string
	Type        string
	Payload     json.RawMessage
}

type AIGenerationRequest struct {
	ID          string     `json:"id"`
	TenantID    string     `json:"tenant_id,omitempty"`
	WorkspaceID string     `json:"workspace_id,omitempty"`
	RequestedBy string     `json:"requested_by,omitempty"`
	TaskType    string     `json:"task_type"`
	Status      string     `json:"status"`
	ResultRef   string     `json:"result_ref,omitempty"`
	Error       string     `json:"error,omitempty"`
	CreatedAt   time.Time  `json:"created_at"`
	CompletedAt *time.Time `json:"completed_at,omitempty"`
}

// AIGenerationJob contains the caller context needed by the asynchronous worker.
type AIGenerationJob struct {
	ID          string
	TenantID    string
	WorkspaceID string
	RequestedBy string
}

type PrivacyData struct {
	RequestID  string          `json:"request_id"`
	TenantID   string          `json:"tenant_id"`
	Profile    Profile         `json:"profile"`
	Consents   []Consent       `json:"consents"`
	Events     json.RawMessage `json:"events"`
	ExportedAt time.Time       `json:"exported_at"`
}

type ReplayReport struct {
	Match          bool   `json:"match"`
	LiveChecksum   string `json:"live_checksum"`
	ReplayChecksum string `json:"replay_checksum"`
	EventCount     int    `json:"event_count"`
	ProfileCount   int    `json:"profile_count"`
}

type RetentionReport struct {
	TenantID      string    `json:"tenant_id"`
	RetentionDays int       `json:"retention_days"`
	Cutoff        time.Time `json:"cutoff"`
	DeletedEvents int64     `json:"deleted_events"`
}

type Role struct {
	ID          string    `json:"id"`
	Name        string    `json:"name"`
	Permissions []string  `json:"permissions"`
	System      bool      `json:"system"`
	CreatedAt   time.Time `json:"created_at"`
}

type User struct {
	ID          string    `json:"id"`
	OIDCIssuer  string    `json:"oidc_issuer"`
	OIDCSubject string    `json:"oidc_subject"`
	Email       string    `json:"email,omitempty"`
	DisplayName string    `json:"display_name,omitempty"`
	Password    string    `json:"password,omitempty"`
	Local       bool      `json:"local"`
	RoleIDs     []string  `json:"role_ids"`
	CreatedAt   time.Time `json:"created_at"`
}

type AuthSession struct {
	AccessToken string    `json:"access_token"`
	TokenType   string    `json:"token_type"`
	ExpiresAt   time.Time `json:"expires_at"`
}

type AuditEvent struct {
	ID           string          `json:"id"`
	ActorType    string          `json:"actor_type"`
	ActorID      string          `json:"actor_id"`
	Action       string          `json:"action"`
	ResourceType string          `json:"resource_type"`
	ResourceID   string          `json:"resource_id,omitempty"`
	Metadata     json.RawMessage `json:"metadata"`
	OccurredAt   time.Time       `json:"occurred_at"`
}

type Suppression struct {
	ID            string    `json:"id"`
	TenantID      string    `json:"tenant_id"`
	Channel       string    `json:"channel"`
	Endpoint      string    `json:"endpoint"`
	Reason        string    `json:"reason"`
	SourceEventID *string   `json:"source_event_id,omitempty"`
	CreatedAt     time.Time `json:"created_at"`
}

type Recipient struct {
	ProfileID string `json:"profile_id"`
	Endpoint  string `json:"endpoint"`
}

type Campaign struct {
	ID                string          `json:"id"`
	TenantID          string          `json:"tenant_id"`
	WorkspaceID       string          `json:"workspace_id"`
	Name              string          `json:"name"`
	Description       *string         `json:"description,omitempty"`
	SegmentID         string          `json:"segment_id"`
	TemplateID        string          `json:"template_id"`
	ExperimentID      *string         `json:"experiment_id,omitempty"`
	ConversionGoal    json.RawMessage `json:"conversion_goal,omitempty"`
	AttributionWindow *string         `json:"attribution_window,omitempty"`
	Status            string          `json:"status"` // draft, scheduled, building, sending, paused, completed, failed, archived
	ScheduledAt       *time.Time      `json:"scheduled_at,omitempty"`
	ManifestKey       *string         `json:"manifest_key,omitempty"`
	SegmentVersion    int             `json:"segment_version"`
	TemplateVersion   int             `json:"template_version"`
	EvaluatedAt       *time.Time      `json:"evaluated_at,omitempty"`
	RecipientCount    int             `json:"recipient_count"`
	CreatedAt         time.Time       `json:"created_at"`
	UpdatedAt         time.Time       `json:"updated_at"`
}

type Experiment struct {
	ID             string              `json:"id"`
	TenantID       string              `json:"tenant_id"`
	WorkspaceID    string              `json:"workspace_id"`
	Name           string              `json:"name"`
	Description    *string             `json:"description,omitempty"`
	SubjectType    string              `json:"subject_type"` // campaign, journey
	Status         string              `json:"status"`       // draft, running, completed, archived
	Method         string              `json:"method"`       // frequentist
	Seed           string              `json:"seed"`
	HoldoutPct     int                 `json:"holdout_pct"`
	PrimaryGoal    json.RawMessage     `json:"primary_goal,omitempty"`
	GuardrailGoals json.RawMessage     `json:"guardrail_goals"`
	WinnerVariant  *string             `json:"winner_variant,omitempty"`
	Variants       []ExperimentVariant `json:"variants,omitempty"`
	CreatedAt      time.Time           `json:"created_at"`
	UpdatedAt      time.Time           `json:"updated_at"`
}

type ExperimentVariant struct {
	ID           string    `json:"id"`
	ExperimentID string    `json:"experiment_id"`
	TenantID     string    `json:"tenant_id"`
	Label        string    `json:"label"`
	Weight       int       `json:"weight"`
	IsControl    bool      `json:"is_control"`
	TemplateID   *string   `json:"template_id,omitempty"`
	CreatedAt    time.Time `json:"created_at"`
}

// OptimizationProposal is an advisory recommendation derived from a live
// experiment report. It never changes experiment assignment by itself.
type OptimizationProposal struct {
	ID              string          `json:"id"`
	TenantID        string          `json:"tenant_id"`
	WorkspaceID     string          `json:"workspace_id"`
	ExperimentID    string          `json:"experiment_id"`
	Kind            string          `json:"kind"`
	ReportSnapshot  json.RawMessage `json:"report_snapshot"`
	ProposedWeights json.RawMessage `json:"proposed_weights,omitempty"`
	WinnerVariant   *string         `json:"winner_variant,omitempty"`
	Rationale       string          `json:"rationale"`
	Status          string          `json:"status"`
	ApprovedBy      *string         `json:"approved_by,omitempty"`
	ApprovedAt      *time.Time      `json:"approved_at,omitempty"`
	CreatedAt       time.Time       `json:"created_at"`
}

type ExperimentAssignment struct {
	ExperimentID string    `json:"experiment_id"`
	TenantID     string    `json:"tenant_id"`
	WorkspaceID  string    `json:"workspace_id"`
	ProfileID    string    `json:"profile_id"`
	Variant      string    `json:"variant"`
	AssignedAt   time.Time `json:"assigned_at"`
}

type Journey struct {
	ID               string          `json:"id"`
	TenantID         string          `json:"tenant_id"`
	WorkspaceID      string          `json:"workspace_id"`
	Name             string          `json:"name"`
	Description      *string         `json:"description,omitempty"`
	Status           string          `json:"status"` // draft, published, archived
	Graph            json.RawMessage `json:"graph"`
	LatestVersion    int             `json:"latest_version"`
	CurrentVersionID *string         `json:"current_version_id,omitempty"`
	CreatedAt        time.Time       `json:"created_at"`
	UpdatedAt        time.Time       `json:"updated_at"`
}

type JourneyVersion struct {
	ID                string          `json:"id"`
	JourneyID         string          `json:"journey_id"`
	TenantID          string          `json:"tenant_id"`
	WorkspaceID       string          `json:"workspace_id"`
	Version           int             `json:"version"`
	Graph             json.RawMessage `json:"graph"`
	ManifestKey       *string         `json:"manifest_key,omitempty"`
	EntryKind         string          `json:"entry_kind"` // event, scheduled
	EntryEventType    *string         `json:"entry_event_type,omitempty"`
	EntrySegmentID    *string         `json:"entry_segment_id,omitempty"`
	EntrySchedule     *string         `json:"entry_schedule,omitempty"`
	ReentryPolicy     string          `json:"reentry_policy"` // once, always, after_exit
	MaxReentries      int             `json:"max_reentries"`
	LatePolicy        string          `json:"late_policy"` // run, skip, reschedule
	ConversionGoal    json.RawMessage `json:"conversion_goal,omitempty"`
	AttributionWindow *string         `json:"attribution_window,omitempty"`
	Status            string          `json:"status"` // active, paused, archived
	PublishedBy       *string         `json:"published_by,omitempty"`
	PublishedAt       time.Time       `json:"published_at"`
}

type DeliveryJob struct {
	ID           string      `json:"id"`
	CampaignID   string      `json:"campaign_id"`
	TenantID     string      `json:"tenant_id"`
	Shard        int         `json:"shard"`
	Status       string      `json:"status"` // pending, processing, completed, failed, dead
	Recipients   []Recipient `json:"recipients"`
	Attempts     int         `json:"attempts"`
	AvailableAt  time.Time   `json:"available_at"`
	LockedUntil  *time.Time  `json:"locked_until,omitempty"`
	ErrorMessage *string     `json:"error_message,omitempty"`
	CreatedAt    time.Time   `json:"created_at"`
	UpdatedAt    time.Time   `json:"updated_at"`
}

type DeliveryAttempt struct {
	ID                string          `json:"id"`
	CampaignID        string          `json:"campaign_id"`
	TenantID          string          `json:"tenant_id"`
	ExperimentID      *string         `json:"experiment_id,omitempty"`
	Variant           string          `json:"variant,omitempty"`
	ProfileID         string          `json:"profile_id"`
	Channel           string          `json:"channel"`
	Endpoint          string          `json:"endpoint"`
	Decision          string          `json:"decision"` // sent, suppressed, no_consent, fatigued, render_failed, send_failed, failed
	Reason            string          `json:"reason,omitempty"`
	ProviderMessageID string          `json:"provider_message_id,omitempty"`
	PolicySnapshot    json.RawMessage `json:"policy_snapshot,omitempty"`
	AttemptedAt       time.Time       `json:"attempted_at"`
	CreatedAt         time.Time       `json:"created_at"`
}

// ReportCount records event/disposition rows (Total) and distinct non-null
// profiles represented by those rows (Unique).
type ReportCount struct {
	Total  int64 `json:"total"`
	Unique int64 `json:"unique"`
}

// ReportFunnel is the canonical source funnel. Targeted counts every disposition
// row, while the decision fields count their exact delivery decision. Engagement
// and conversion fields are projection-maintained facts, never raw event scans.
type ReportFunnel struct {
	Targeted     ReportCount `json:"targeted"`
	Sent         ReportCount `json:"sent"`
	Suppressed   ReportCount `json:"suppressed"`
	NoConsent    ReportCount `json:"no_consent"`
	Fatigued     ReportCount `json:"fatigued"`
	RenderFailed ReportCount `json:"render_failed"`
	SendFailed   ReportCount `json:"send_failed"`
	Failed       ReportCount `json:"failed"`
	Holdout      ReportCount `json:"holdout"`
	Delivered    ReportCount `json:"delivered"`
	Opened       ReportCount `json:"opened"`
	Clicked      ReportCount `json:"clicked"`
	Converted    ReportCount `json:"converted"`
}

// ReportDeliverability counts projected bounce/complaint facts. Rates use
// total sent dispositions as the denominator and are zero when nothing was sent.
type ReportDeliverability struct {
	Bounced       ReportCount `json:"bounced"`
	Complained    ReportCount `json:"complained"`
	BounceRate    float64     `json:"bounce_rate"`
	ComplaintRate float64     `json:"complaint_rate"`
}

type CampaignReport struct {
	CampaignID     string               `json:"campaign_id"`
	Funnel         ReportFunnel         `json:"funnel"`
	Deliverability ReportDeliverability `json:"deliverability"`
}

type JourneyReport struct {
	JourneyID      string               `json:"journey_id"`
	Funnel         ReportFunnel         `json:"funnel"`
	Deliverability ReportDeliverability `json:"deliverability"`
}

type ExperimentReport struct {
	ExperimentID  string                    `json:"experiment_id"`
	WinnerVariant *string                   `json:"winner_variant,omitempty"`
	Variants      []ExperimentVariantReport `json:"variants"`
}

// ExperimentRollout identifies the separately approved resource version that
// is pinned to an experiment's advisory winner.
type ExperimentRollout struct {
	ExperimentID   string          `json:"experiment_id"`
	WinnerVariant  string          `json:"winner_variant"`
	SubjectType    string          `json:"subject_type"`
	Campaign       *Campaign       `json:"campaign,omitempty"`
	JourneyVersion *JourneyVersion `json:"journey_version,omitempty"`
}

type ExperimentVariantReport struct {
	Label       string                `json:"label"`
	IsControl   bool                  `json:"is_control"`
	Sent        int64                 `json:"sent"`
	Conversions int64                 `json:"conversions"`
	Rate        float64               `json:"rate"`
	Uplift      float64               `json:"uplift"`
	ZScore      float64               `json:"z_score"`
	PValue      float64               `json:"p_value"`
	CILow       float64               `json:"ci_low"`
	CIHigh      float64               `json:"ci_high"`
	Guardrails  []ExperimentGuardrail `json:"guardrails"`
}

type ExperimentGuardrail struct {
	GoalName    string  `json:"goal_name"`
	Conversions int64   `json:"conversions"`
	Rate        float64 `json:"rate"`
}

type JourneyRun struct {
	ID                string          `json:"id"`
	TenantID          string          `json:"tenant_id"`
	WorkspaceID       string          `json:"workspace_id"`
	JourneyID         string          `json:"journey_id"`
	JourneyVersionID  string          `json:"journey_version_id"`
	ProfileID         string          `json:"profile_id"`
	SubjectExternalID *string         `json:"subject_external_id,omitempty"`
	EntryKey          string          `json:"entry_key"`
	ReentrySequence   int             `json:"reentry_sequence"`
	Status            string          `json:"status"`
	CurrentNodeID     string          `json:"current_node_id"`
	State             json.RawMessage `json:"state"`
	WaitEventType     *string         `json:"wait_event_type,omitempty"`
	WaitUntil         *time.Time      `json:"wait_until,omitempty"`
	GoalReached       bool            `json:"goal_reached"`
	EnteredAt         time.Time       `json:"entered_at"`
	UpdatedAt         time.Time       `json:"updated_at"`
	CompletedAt       *time.Time      `json:"completed_at,omitempty"`
}

type JourneyStep struct {
	ID           string     `json:"id"`
	RunID        string     `json:"run_id"`
	TenantID     string     `json:"tenant_id"`
	NodeID       string     `json:"node_id"`
	Kind         string     `json:"kind"`
	Status       string     `json:"status"`
	Attempts     int        `json:"attempts"`
	AvailableAt  time.Time  `json:"available_at"`
	LockedUntil  *time.Time `json:"locked_until,omitempty"`
	ErrorMessage *string    `json:"error_message,omitempty"`
	CreatedAt    time.Time  `json:"created_at"`
	UpdatedAt    time.Time  `json:"updated_at"`
}

type JourneyTransition struct {
	ID         string          `json:"id"`
	RunID      string          `json:"run_id"`
	TenantID   string          `json:"tenant_id"`
	FromNode   *string         `json:"from_node,omitempty"`
	ToNode     *string         `json:"to_node,omitempty"`
	NodeType   string          `json:"node_type"`
	Outcome    string          `json:"outcome"`
	Detail     json.RawMessage `json:"detail"`
	OccurredAt time.Time       `json:"occurred_at"`
}

type JourneyMessageIntent struct {
	ID                string          `json:"id"`
	RunID             string          `json:"run_id"`
	TenantID          string          `json:"tenant_id"`
	WorkspaceID       string          `json:"workspace_id"`
	JourneyID         string          `json:"journey_id"`
	JourneyVersionID  string          `json:"journey_version_id"`
	NodeID            string          `json:"node_id"`
	ProfileID         string          `json:"profile_id"`
	ExperimentID      *string         `json:"experiment_id,omitempty"`
	Variant           string          `json:"variant,omitempty"`
	TemplateID        string          `json:"template_id"`
	Channel           string          `json:"channel"`
	Endpoint          string          `json:"endpoint"`
	Transactional     bool            `json:"transactional"`
	Status            string          `json:"status"`
	Attempts          int             `json:"attempts"`
	AvailableAt       time.Time       `json:"available_at"`
	LockedUntil       *time.Time      `json:"locked_until,omitempty"`
	Decision          *string         `json:"decision,omitempty"`
	Reason            *string         `json:"reason,omitempty"`
	ProviderMessageID *string         `json:"provider_message_id,omitempty"`
	PolicySnapshot    json.RawMessage `json:"policy_snapshot"`
	ErrorMessage      *string         `json:"error_message,omitempty"`
	CreatedAt         time.Time       `json:"created_at"`
	UpdatedAt         time.Time       `json:"updated_at"`
}

type AIProviderConfig struct {
	ID                 string          `json:"id"`
	TenantID           string          `json:"tenant_id"`
	WorkspaceID        string          `json:"workspace_id"`
	Provider           string          `json:"provider"`
	IsDefault          bool            `json:"is_default"`
	Config             json.RawMessage `json:"config"`
	EndpointAllowlist  []string        `json:"endpoint_allowlist"`
	FallbackProvider   *string         `json:"fallback_provider,omitempty"`
	MonthlyBudgetCents int64           `json:"monthly_budget_cents"`
	Status             string          `json:"status"`
	CreatedAt          time.Time       `json:"created_at"`
	UpdatedAt          time.Time       `json:"updated_at"`
}

type AIBudgetUsage struct {
	TenantID     string    `json:"tenant_id"`
	WorkspaceID  string    `json:"workspace_id"`
	Period       string    `json:"period"` // YYYY-MM
	CostCents    int64     `json:"cost_cents"`
	InputTokens  int64     `json:"input_tokens"`
	OutputTokens int64     `json:"output_tokens"`
	UpdatedAt    time.Time `json:"updated_at"`
}

type AIActivity struct {
	ID              string          `json:"id"`
	TenantID        string          `json:"tenant_id"`
	WorkspaceID     string          `json:"workspace_id"`
	ActorUserID     *string         `json:"actor_user_id,omitempty"`
	Action          string          `json:"action"`
	Provider        string          `json:"provider"`
	Model           string          `json:"model"`
	PromptVersionID *string         `json:"prompt_version_id,omitempty"`
	RetrievalRefs   json.RawMessage `json:"retrieval_refs"`
	ToolCalls       json.RawMessage `json:"tool_calls"`
	Classification  *string         `json:"classification,omitempty"`
	InputTokens     int             `json:"input_tokens"`
	OutputTokens    int             `json:"output_tokens"`
	CostCents       int64           `json:"cost_cents"`
	LatencyMs       int             `json:"latency_ms"`
	PolicyDecision  string          `json:"policy_decision"`
	ApproverUserID  *string         `json:"approver_user_id,omitempty"`
	OutputRef       *string         `json:"output_ref,omitempty"`
	CreatedAt       time.Time       `json:"created_at"`
}

type Prompt struct {
	ID               string    `json:"id"`
	TenantID         string    `json:"tenant_id"`
	WorkspaceID      string    `json:"workspace_id"`
	Name             string    `json:"name"`
	TaskType         string    `json:"task_type"` // content_draft, audience_dsl, journey_draft, performance_summary, moderation
	CurrentVersionID *string   `json:"current_version_id,omitempty"`
	LatestVersion    int       `json:"latest_version"`
	CreatedAt        time.Time `json:"created_at"`
	UpdatedAt        time.Time `json:"updated_at"`
}

type PromptVersion struct {
	ID           string          `json:"id"`
	PromptID     string          `json:"prompt_id"`
	TenantID     string          `json:"tenant_id"`
	Version      int             `json:"version"`
	Template     string          `json:"template"`
	InputSchema  json.RawMessage `json:"input_schema"`
	OutputSchema json.RawMessage `json:"output_schema"`
	Provider     string          `json:"provider"`
	Model        string          `json:"model"`
	Params       json.RawMessage `json:"params"`
	SafetyPolicy json.RawMessage `json:"safety_policy"`
	ManifestKey  string          `json:"manifest_key"`
	Status       string          `json:"status"`      // draft, active, archived
	EvalStatus   string          `json:"eval_status"` // pending, passed, failed
	PublishedBy  *string         `json:"published_by,omitempty"`
	PublishedAt  *time.Time      `json:"published_at,omitempty"`
	CreatedAt    time.Time       `json:"created_at"`
}

type FieldClassification struct {
	ID             string    `json:"id"`
	TenantID       string    `json:"tenant_id"`
	WorkspaceID    string    `json:"workspace_id"`
	EntityType     string    `json:"entity_type"`
	FieldPath      string    `json:"field_path"`
	Classification string    `json:"classification"`
	SendToModel    string    `json:"send_to_model"`
	CreatedAt      time.Time `json:"created_at"`
}

type EvalDataset struct {
	ID          string    `json:"id"`
	TenantID    string    `json:"tenant_id"`
	WorkspaceID string    `json:"workspace_id"`
	TaskType    string    `json:"task_type"`
	Name        string    `json:"name"`
	CreatedAt   time.Time `json:"created_at"`
}

type EvalCase struct {
	ID           string          `json:"id"`
	DatasetID    string          `json:"dataset_id"`
	TenantID     string          `json:"tenant_id"`
	Input        json.RawMessage `json:"input"`
	Expectations json.RawMessage `json:"expectations"`
}

type EvalRun struct {
	ID              string    `json:"id"`
	PromptVersionID string    `json:"prompt_version_id"`
	TenantID        string    `json:"tenant_id"`
	DatasetID       string    `json:"dataset_id"`
	Passed          int       `json:"passed"`
	Failed          int       `json:"failed"`
	Verdict         string    `json:"verdict"`
	CreatedAt       time.Time `json:"created_at"`
}

type ScoringModel struct {
	ID               string    `json:"id"`
	TenantID         string    `json:"tenant_id"`
	WorkspaceID      string    `json:"workspace_id"`
	Name             string    `json:"name"`
	Kind             string    `json:"kind"` // expression, llm
	CurrentVersionID *string   `json:"current_version_id,omitempty"`
	LatestVersion    int       `json:"latest_version"`
	CreatedAt        time.Time `json:"created_at"`
	UpdatedAt        time.Time `json:"updated_at"`
}

type ScoringModelVersion struct {
	ID             string          `json:"id"`
	ScoringModelID string          `json:"scoring_model_id"`
	TenantID       string          `json:"tenant_id"`
	Version        int             `json:"version"`
	ScoreName      string          `json:"score_name"`
	Definition     json.RawMessage `json:"definition"`
	OutputMin      float64         `json:"output_min"`
	OutputMax      float64         `json:"output_max"`
	ManifestKey    string          `json:"manifest_key"`
	Status         string          `json:"status"`      // draft, active, archived
	EvalStatus     string          `json:"eval_status"` // pending, passed, failed
	PublishedBy    *string         `json:"published_by,omitempty"`
	PublishedAt    *time.Time      `json:"published_at,omitempty"`
	CreatedAt      time.Time       `json:"created_at"`
}

type ProfileScore struct {
	TenantID       string    `json:"tenant_id"`
	WorkspaceID    string    `json:"workspace_id"`
	AppID          string    `json:"app_id"`
	ProfileID      string    `json:"profile_id"`
	ScoringModelID string    `json:"scoring_model_id"`
	ScoreName      string    `json:"score_name"`
	Value          float64   `json:"value"`
	ModelVersion   int       `json:"model_version"`
	ComputedAt     time.Time `json:"computed_at"`
}

type ScoringRequest struct {
	ID             string     `json:"id"`
	TenantID       string     `json:"tenant_id,omitempty"`
	WorkspaceID    string     `json:"workspace_id,omitempty"`
	RequestedBy    string     `json:"requested_by,omitempty"`
	ScoringModelID string     `json:"scoring_model_id"`
	SegmentID      string     `json:"segment_id"`
	Status         string     `json:"status"`
	Error          string     `json:"error,omitempty"`
	CreatedAt      time.Time  `json:"created_at"`
	CompletedAt    *time.Time `json:"completed_at,omitempty"`
}

type ScoringJob struct {
	ID             string
	TenantID       string
	WorkspaceID    string
	RequestedBy    string
	ScoringModelID string
	SegmentID      string
}
