package campaigns

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/buildwithdmytro/openjourney/internal/channels"
	"github.com/buildwithdmytro/openjourney/internal/domain"
	assignment "github.com/buildwithdmytro/openjourney/internal/experiment"
	"github.com/buildwithdmytro/openjourney/internal/ports"
)

type mockStore struct {
	ports.Store
	jobs             map[string]domain.DeliveryJob
	campaigns        map[string]domain.Campaign
	templates        map[string]domain.Template
	identities       map[string]domain.SendingIdentity
	profiles         map[string]domain.Profile
	deliveryAttempts map[string]domain.DeliveryAttempt
	deletedAttempts  []string
	failedJobs       map[string]string
	completedJobs    map[string]bool
	createdAttempts  []domain.DeliveryAttempt
	updatedAttempts  []string
	isSuppressedFunc func(ctx context.Context, p domain.Principal, channel, endpoint string) (bool, error)
	experiments      map[string]domain.Experiment
	assignments      map[string]domain.ExperimentAssignment
	emittedEvents    []domain.Event
	segments         map[string]domain.Segment
	resolvedSegment  map[string][]string
	manifestJobs     map[string][]domain.DeliveryJob
	manifestKeys     map[string]string
	profileEmails    map[string]string
	profilePhones    map[string]string
	deviceTokens     []domain.DeviceToken
	retiredTokens    []string
	consentState     string // "subscribed" or "unsubscribed"
	sentCountSince   int    // for fatigue testing
}

func newMockStore() *mockStore {
	return &mockStore{
		jobs:             make(map[string]domain.DeliveryJob),
		campaigns:        make(map[string]domain.Campaign),
		templates:        make(map[string]domain.Template),
		identities:       make(map[string]domain.SendingIdentity),
		profiles:         make(map[string]domain.Profile),
		deliveryAttempts: make(map[string]domain.DeliveryAttempt),
		failedJobs:       make(map[string]string),
		completedJobs:    make(map[string]bool),
		experiments:      make(map[string]domain.Experiment),
		assignments:      make(map[string]domain.ExperimentAssignment),
		segments:         make(map[string]domain.Segment),
		resolvedSegment:  make(map[string][]string),
		manifestJobs:     make(map[string][]domain.DeliveryJob),
		manifestKeys:     make(map[string]string),
		profileEmails:    make(map[string]string),
		profilePhones:    make(map[string]string),
		consentState:     "subscribed",
		sentCountSince:   0,
	}
}

func (m *mockStore) ClaimDeliveryJob(ctx context.Context, workerID string) (domain.DeliveryJob, bool, error) {
	for _, job := range m.jobs {
		return job, true, nil
	}
	return domain.DeliveryJob{}, false, nil
}

func (m *mockStore) GetCampaignSystem(ctx context.Context, tenantID, campaignID string) (domain.Campaign, error) {
	c, ok := m.campaigns[campaignID]
	if !ok {
		return domain.Campaign{}, errors.New("campaign not found")
	}
	return c, nil
}

func (m *mockStore) GetFirstAppID(ctx context.Context, tenantID, workspaceID string) (string, error) {
	return "app-1", nil
}

func (m *mockStore) GetTemplate(ctx context.Context, p domain.Principal, id string) (domain.Template, error) {
	t, ok := m.templates[id]
	if !ok {
		return domain.Template{}, errors.New("template not found")
	}
	return t, nil
}

func (m *mockStore) GetExperiment(ctx context.Context, p domain.Principal, id string) (domain.Experiment, error) {
	e, ok := m.experiments[id]
	if !ok {
		return domain.Experiment{}, ports.ErrNotFound
	}
	return e, nil
}

func (m *mockStore) AssignExperiment(ctx context.Context, p domain.Principal, experimentID, profileID, variant string) (domain.ExperimentAssignment, error) {
	key := experimentID + ":" + profileID
	if existing, ok := m.assignments[key]; ok {
		return existing, nil
	}
	out := domain.ExperimentAssignment{ExperimentID: experimentID, TenantID: p.TenantID, WorkspaceID: p.WorkspaceID, ProfileID: profileID, Variant: variant}
	m.assignments[key] = out
	return out, nil
}

func (m *mockStore) GetSendingIdentity(ctx context.Context, p domain.Principal, id string) (domain.SendingIdentity, error) {
	i, ok := m.identities[id]
	if !ok {
		return domain.SendingIdentity{}, errors.New("identity not found")
	}
	return i, nil
}

func (m *mockStore) CreateDeliveryAttempt(ctx context.Context, attempt domain.DeliveryAttempt) (bool, error) {
	m.createdAttempts = append(m.createdAttempts, attempt)
	key := attempt.CampaignID + ":" + attempt.ProfileID + ":" + attempt.Channel + ":" + attempt.Endpoint
	if _, ok := m.deliveryAttempts[key]; ok {
		return false, nil
	}
	m.deliveryAttempts[key] = attempt
	return true, nil
}

func (m *mockStore) GetProfileByID(ctx context.Context, tenantID, appID, profileID string) (domain.Profile, error) {
	p, ok := m.profiles[profileID]
	if !ok {
		return domain.Profile{}, errors.New("profile not found")
	}
	return p, nil
}

func (m *mockStore) GetTenantFatigueQuotas(ctx context.Context, p domain.Principal) (int, int, error) {
	return 5, 20, nil
}

func (m *mockStore) DeleteDeliveryAttempt(ctx context.Context, tenantID, campaignID, profileID, channel, endpoint string) error {
	m.deletedAttempts = append(m.deletedAttempts, profileID)
	key := campaignID + ":" + profileID + ":" + channel + ":" + endpoint
	delete(m.deliveryAttempts, key)
	return nil
}

func (m *mockStore) GetDeliveryAttempt(ctx context.Context, campaignID, profileID, channel, endpoint string) (domain.DeliveryAttempt, error) {
	key := campaignID + ":" + profileID + ":" + channel + ":" + endpoint
	att, ok := m.deliveryAttempts[key]
	if !ok {
		return domain.DeliveryAttempt{}, ports.ErrNotFound
	}
	return att, nil
}

func (m *mockStore) UpdateDeliveryAttempt(ctx context.Context, campaignID, profileID, channel, endpoint, decision, reason, providerMsgID string, policySnapshot []byte, costMicros int64) error {
	m.updatedAttempts = append(m.updatedAttempts, profileID+":"+decision)
	key := campaignID + ":" + profileID + ":" + channel + ":" + endpoint
	if att, ok := m.deliveryAttempts[key]; ok {
		att.Decision = decision
		att.Reason = reason
		att.ProviderMessageID = providerMsgID
		m.deliveryAttempts[key] = att
	}
	return nil
}

func (m *mockStore) SetDeliveryAttemptExperiment(ctx context.Context, tenantID, campaignID, profileID, channel, experimentID, variant string) error {
	prefix := campaignID + ":" + profileID + ":" + channel + ":"
	found := false
	for key, att := range m.deliveryAttempts {
		if strings.HasPrefix(key, prefix) {
			att.ExperimentID, att.Variant = &experimentID, variant
			m.deliveryAttempts[key] = att
			found = true
		}
	}
	if !found {
		return ports.ErrNotFound
	}
	return nil
}

func (m *mockStore) AcceptEvents(ctx context.Context, p domain.Principal, events []domain.Event) ([]string, error) {
	m.emittedEvents = append(m.emittedEvents, events...)
	return []string{"event-id"}, nil
}

func (m *mockStore) CompleteDeliveryJob(ctx context.Context, jobID string) error {
	m.completedJobs[jobID] = true
	return nil
}

func (m *mockStore) FailDeliveryJob(ctx context.Context, jobID string, errMsg string) error {
	m.failedJobs[jobID] = errMsg
	return nil
}

func (m *mockStore) LatestConsent(ctx context.Context, p domain.Principal, profileID, channel, topic string) (domain.Consent, error) {
	return domain.Consent{
		ProfileID:  profileID,
		Channel:    channel,
		Topic:      topic,
		State:      m.consentState,
		OccurredAt: time.Now(),
	}, nil
}

func (m *mockStore) IsSuppressed(ctx context.Context, p domain.Principal, channel, endpoint string) (bool, error) {
	if m.isSuppressedFunc != nil {
		return m.isSuppressedFunc(ctx, p, channel, endpoint)
	}
	return false, nil
}

func (m *mockStore) SentCountSince(ctx context.Context, p domain.Principal, profileID string, since time.Time) (int, error) {
	return m.sentCountSince, nil
}

func (m *mockStore) GetProfileByPhone(ctx context.Context, tenantID string, phone string) (domain.Profile, error) {
	for _, prof := range m.profiles {
		var attrs map[string]any
		if len(prof.Attributes) > 0 && json.Unmarshal(prof.Attributes, &attrs) == nil {
			if attrs["phone"] == phone {
				return prof, nil
			}
		}
	}
	return domain.Profile{}, errors.New("profile not found")
}

func (m *mockStore) ListSendingIdentities(ctx context.Context, p domain.Principal) ([]domain.SendingIdentity, error) {
	var out []domain.SendingIdentity
	for _, iden := range m.identities {
		out = append(out, iden)
	}
	return out, nil
}

func (m *mockStore) RegisterDeviceToken(ctx context.Context, tenantID, workspaceID, appID, profileID, platform, provider, token string) (domain.DeviceToken, error) {
	return domain.DeviceToken{}, nil
}
func (m *mockStore) RetireDeviceToken(ctx context.Context, tenantID, appID, token string) error {
	m.retiredTokens = append(m.retiredTokens, token)
	for i, tok := range m.deviceTokens {
		if tok.Token == token {
			m.deviceTokens[i].Status = "inactive"
		}
	}
	return nil
}
func (m *mockStore) RetireDeviceTokenByID(ctx context.Context, tenantID, id string) error {
	return nil
}
func (m *mockStore) ListActiveDeviceTokens(ctx context.Context, tenantID, workspaceID, profileID string) ([]domain.DeviceToken, error) {
	var out []domain.DeviceToken
	for _, tok := range m.deviceTokens {
		if tok.ProfileID == profileID && tok.Status == "active" {
			out = append(out, tok)
		}
	}
	return out, nil
}
func (m *mockStore) ListDeviceTokensByProfile(ctx context.Context, tenantID, workspaceID, profileID string) ([]domain.DeviceToken, error) {
	return nil, nil
}


func (m *mockStore) GetSendingIdentityByProviderConfig(ctx context.Context, provider string, configKey string, configVal string) (domain.SendingIdentity, error) {
	for _, iden := range m.identities {
		if iden.Provider == provider {
			// Basic JSON search mock
			if strings.Contains(string(iden.Config), fmt.Sprintf("%q:%q", configKey, configVal)) ||
				strings.Contains(string(iden.Config), fmt.Sprintf("%q: %q", configKey, configVal)) {
				return iden, nil
			}
		}
	}
	return domain.SendingIdentity{}, errors.New("identity not found")
}

func (m *mockStore) GetProfileEmails(ctx context.Context, tenantID string, profileIDs []string) (map[string]string, error) {
	out := make(map[string]string)
	for _, id := range profileIDs {
		if email, ok := m.profileEmails[id]; ok {
			out[id] = email
		}
	}
	return out, nil
}

func (m *mockStore) GetProfilePhones(ctx context.Context, tenantID string, profileIDs []string) (map[string]string, error) {
	out := make(map[string]string)
	for _, id := range profileIDs {
		if phone, ok := m.profilePhones[id]; ok {
			out[id] = phone
		}
	}
	return out, nil
}

func (m *mockStore) ClaimScheduledCampaign(ctx context.Context) (domain.Campaign, bool, error) {
	for _, c := range m.campaigns {
		if c.Status == "scheduled" {
			// Update status to processing/dispatching to mock actual claim behavior
			c.Status = "dispatching"
			m.campaigns[c.ID] = c
			return c, true, nil
		}
	}
	return domain.Campaign{}, false, nil
}

func (m *mockStore) ResolveSegment(ctx context.Context, p domain.Principal, segmentID string) ([]string, error) {
	return m.resolvedSegment[segmentID], nil
}

func (m *mockStore) GetSegment(ctx context.Context, p domain.Principal, id string) (domain.Segment, error) {
	seg, ok := m.segments[id]
	if !ok {
		return domain.Segment{}, errors.New("segment not found")
	}
	return seg, nil
}

func (m *mockStore) SaveCampaignManifestAndJobs(ctx context.Context, campaignID string, manifestKey string, recipientCount int, segmentVersion int, templateVersion int, conversionGoal json.RawMessage, attributionWindow string, jobs []domain.DeliveryJob) error {
	m.manifestKeys[campaignID] = manifestKey
	m.manifestJobs[campaignID] = jobs
	return nil
}

func (m *mockStore) UpdateCampaign(ctx context.Context, p domain.Principal, c domain.Campaign) (domain.Campaign, error) {
	m.campaigns[c.ID] = c
	return c, nil
}

type testAdapter struct {
	err      error
	messages []ports.RenderedMessage
}

func (a *testAdapter) Send(ctx context.Context, msg ports.RenderedMessage) (string, int64, error) {
	a.messages = append(a.messages, msg)
	return "msg-123", 0, a.err
}

func (a *testAdapter) ValidateConfig(identity domain.SendingIdentity) error {
	return nil
}

func TestDeliverNext_RetryableError(t *testing.T) {
	store := newMockStore()

	campID := "camp-1"
	tmplID := "tmpl-1"
	profID := "prof-1"
	jobID := "job-1"

	store.campaigns[campID] = domain.Campaign{
		ID:          campID,
		TenantID:    "tenant-1",
		WorkspaceID: "workspace-1",
		TemplateID:  tmplID,
	}

	htmlTmpl := "hello world"
	store.templates[tmplID] = domain.Template{
		ID:           tmplID,
		Channel:      "email",
		HTMLTemplate: &htmlTmpl,
	}

	store.profiles[profID] = domain.Profile{
		ID:         profID,
		ExternalID: "ext-1",
	}

	store.jobs[jobID] = domain.DeliveryJob{
		ID:         jobID,
		CampaignID: campID,
		TenantID:   "tenant-1",
		Recipients: []domain.Recipient{
			{
				ProfileID: profID,
				Endpoint:  "test@example.com",
			},
		},
	}

	retryableErr := &channels.DeliveryError{
		Err:       errors.New("rate limit exceeded"),
		Retryable: true,
	}

	cfg := Config{
		Adapter: &testAdapter{err: retryableErr},
	}

	processed, err := DeliverNext(context.Background(), store, "worker-1", cfg)
	if !processed {
		t.Fatalf("expected processed=true, got false")
	}
	if err == nil || !strings.Contains(err.Error(), "transient delivery failure") {
		t.Fatalf("expected transient delivery failure error, got: %v", err)
	}

	if len(store.deletedAttempts) != 0 {
		t.Errorf("expected no deleted attempts, got: %v", store.deletedAttempts)
	}

	foundRetryableFailed := false
	for _, up := range store.updatedAttempts {
		if up == profID+":retryable_failed" {
			foundRetryableFailed = true
		}
	}
	if !foundRetryableFailed {
		t.Errorf("expected attempt for profile %s to transition to retryable_failed, got: %v", profID, store.updatedAttempts)
	}

	if _, ok := store.completedJobs[jobID]; ok {
		t.Errorf("expected job %s to not be completed", jobID)
	}
	if _, ok := store.failedJobs[jobID]; !ok {
		t.Errorf("expected job %s to be failed/requeued", jobID)
	}
}

func TestDeliverNext_RetryableAndReconcile(t *testing.T) {
	store := newMockStore()

	campID := "camp-1"
	tmplID := "tmpl-1"
	profID := "prof-1"
	jobID := "job-1"

	store.campaigns[campID] = domain.Campaign{
		ID:          campID,
		TenantID:    "tenant-1",
		WorkspaceID: "workspace-1",
		TemplateID:  tmplID,
	}

	htmlTmpl := "hello world"
	store.templates[tmplID] = domain.Template{
		ID:           tmplID,
		Channel:      "email",
		HTMLTemplate: &htmlTmpl,
	}

	store.profiles[profID] = domain.Profile{
		ID:         profID,
		ExternalID: "ext-1",
	}

	store.jobs[jobID] = domain.DeliveryJob{
		ID:         jobID,
		CampaignID: campID,
		TenantID:   "tenant-1",
		Recipients: []domain.Recipient{
			{
				ProfileID: profID,
				Endpoint:  "test@example.com",
			},
		},
	}

	// 1. Simulate a previous run that reached 'provider_sent' state (e.g. sent message to SES, received providerMsgID, but crashed before emitting event)
	key := campID + ":" + profID + ":email:" + "test@example.com"
	store.deliveryAttempts[key] = domain.DeliveryAttempt{
		CampaignID:        campID,
		TenantID:          "tenant-1",
		ProfileID:         profID,
		Channel:           "email",
		Endpoint:          "test@example.com",
		Decision:          "provider_sent",
		ProviderMessageID: "ses-12345",
	}

	// Create adapter that counts number of actual sends
	sendCount := 0
	adapter := &countingAdapter{sendCount: &sendCount}
	cfg := Config{
		Adapter: adapter,
	}

	processed, err := DeliverNext(context.Background(), store, "worker-1", cfg)
	if !processed {
		t.Fatalf("expected processed")
	}
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Verify that Send was NOT called on the adapter (reconciled!)
	if sendCount != 0 {
		t.Errorf("expected 0 sends, got: %d", sendCount)
	}

	// Verify that the final status is 'sent'
	att := store.deliveryAttempts[key]
	if att.Decision != "sent" {
		t.Errorf("expected state to transition to 'sent', got: %s", att.Decision)
	}
	if att.ProviderMessageID != "ses-12345" {
		t.Errorf("expected provider message ID to be preserved, got: %s", att.ProviderMessageID)
	}
}

type countingAdapter struct {
	sendCount *int
}

func (a *countingAdapter) Send(ctx context.Context, msg ports.RenderedMessage) (string, int64, error) {
	*a.sendCount++
	return "ses-999", 0, nil
}

func (a *countingAdapter) ValidateConfig(identity domain.SendingIdentity) error {
	return nil
}

func TestDeliverNext_PermanentError(t *testing.T) {
	store := newMockStore()

	campID := "camp-1"
	tmplID := "tmpl-1"
	profID := "prof-1"
	jobID := "job-1"

	store.campaigns[campID] = domain.Campaign{
		ID:          campID,
		TenantID:    "tenant-1",
		WorkspaceID: "workspace-1",
		TemplateID:  tmplID,
	}

	htmlTmpl := "hello world"
	store.templates[tmplID] = domain.Template{
		ID:           tmplID,
		Channel:      "email",
		HTMLTemplate: &htmlTmpl,
	}

	store.profiles[profID] = domain.Profile{
		ID:         profID,
		ExternalID: "ext-1",
	}

	store.jobs[jobID] = domain.DeliveryJob{
		ID:         jobID,
		CampaignID: campID,
		TenantID:   "tenant-1",
		Recipients: []domain.Recipient{
			{
				ProfileID: profID,
				Endpoint:  "test@example.com",
			},
		},
	}

	permanentErr := &channels.DeliveryError{
		Err:       errors.New("invalid address"),
		Retryable: false,
	}

	cfg := Config{
		Adapter: &testAdapter{err: permanentErr},
	}

	processed, err := DeliverNext(context.Background(), store, "worker-1", cfg)
	if !processed {
		t.Fatalf("expected processed=true, got false")
	}
	if err != nil {
		t.Fatalf("expected no DeliverNext error for permanent failure, got: %v", err)
	}

	if len(store.deletedAttempts) != 0 {
		t.Errorf("expected no deleted attempts, got: %v", store.deletedAttempts)
	}

	foundSendFailed := false
	for _, up := range store.updatedAttempts {
		if up == profID+":send_failed" {
			foundSendFailed = true
		}
	}
	if !foundSendFailed {
		t.Errorf("expected attempt to be updated to send_failed, got: %v", store.updatedAttempts)
	}

	if _, ok := store.completedJobs[jobID]; !ok {
		t.Errorf("expected job %s to be completed", jobID)
	}
}

func TestDeliverNext_EffectivelyOnceSkip(t *testing.T) {
	store := newMockStore()

	campID := "camp-1"
	tmplID := "tmpl-1"
	profID := "prof-1"
	jobID := "job-1"

	store.campaigns[campID] = domain.Campaign{
		ID:          campID,
		TenantID:    "tenant-1",
		WorkspaceID: "workspace-1",
		TemplateID:  tmplID,
	}

	htmlTmpl := "hello world"
	store.templates[tmplID] = domain.Template{
		ID:           tmplID,
		Channel:      "email",
		HTMLTemplate: &htmlTmpl,
	}

	store.profiles[profID] = domain.Profile{
		ID:         profID,
		ExternalID: "ext-1",
	}

	store.jobs[jobID] = domain.DeliveryJob{
		ID:         jobID,
		CampaignID: campID,
		TenantID:   "tenant-1",
		Recipients: []domain.Recipient{
			{
				ProfileID: profID,
				Endpoint:  "test@example.com",
			},
		},
	}

	// Seed the delivery attempts map so CreateDeliveryAttempt returns false (already exists)
	key := campID + ":" + profID + ":email:" + "test@example.com"
	store.deliveryAttempts[key] = domain.DeliveryAttempt{
		CampaignID: campID,
		ProfileID:  profID,
		Channel:    "email",
	}

	adapter := &testAdapter{}
	cfg := Config{
		Adapter: adapter,
	}

	processed, err := DeliverNext(context.Background(), store, "worker-1", cfg)
	if !processed {
		t.Fatalf("expected processed=true")
	}
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Since it was skipped, updatedAttempts should be empty (no updates to sent, suppressed, failed, etc.)
	if len(store.updatedAttempts) != 0 {
		t.Errorf("expected no updated attempts, got: %v", store.updatedAttempts)
	}

	if _, ok := store.completedJobs[jobID]; !ok {
		t.Errorf("expected job %s to be completed", jobID)
	}
}

func TestDeliverNext_PolicyRejection(t *testing.T) {
	// 1. Suppression case
	{
		store := newMockStore()
		campID := "camp-1"
		tmplID := "tmpl-1"
		profID := "prof-1"
		jobID := "job-1"

		store.campaigns[campID] = domain.Campaign{
			ID:          campID,
			TenantID:    "tenant-1",
			WorkspaceID: "workspace-1",
			TemplateID:  tmplID,
		}

		htmlTmpl := "hello world"
		store.templates[tmplID] = domain.Template{
			ID:           tmplID,
			Channel:      "email",
			HTMLTemplate: &htmlTmpl,
		}

		store.profiles[profID] = domain.Profile{
			ID:         profID,
			ExternalID: "ext-1",
		}

		store.jobs[jobID] = domain.DeliveryJob{
			ID:         jobID,
			CampaignID: campID,
			TenantID:   "tenant-1",
			Recipients: []domain.Recipient{
				{
					ProfileID: profID,
					Endpoint:  "test@example.com",
				},
			},
		}

		// Mock IsSuppressed helper on store
		store.isSuppressedFunc = func(ctx context.Context, p domain.Principal, channel, endpoint string) (bool, error) {
			return true, nil
		}

		adapter := &testAdapter{}
		cfg := Config{
			Adapter: adapter,
		}

		processed, err := DeliverNext(context.Background(), store, "worker-1", cfg)
		if !processed || err != nil {
			t.Fatalf("unexpected err: %v", err)
		}

		foundSuppressed := false
		for _, up := range store.updatedAttempts {
			if up == profID+":suppressed" {
				foundSuppressed = true
			}
		}
		if !foundSuppressed {
			t.Errorf("expected attempt to be updated to suppressed, got: %v", store.updatedAttempts)
		}
	}
}

func TestDeliverNext_RenderFailure(t *testing.T) {
	store := newMockStore()

	campID := "camp-1"
	tmplID := "tmpl-1"
	profID := "prof-1"
	jobID := "job-1"

	store.campaigns[campID] = domain.Campaign{
		ID:          campID,
		TenantID:    "tenant-1",
		WorkspaceID: "workspace-1",
		TemplateID:  tmplID,
	}

	// Invalid Liquid tag will trigger render failure
	htmlTmpl := "{% invalid_tag %}"
	store.templates[tmplID] = domain.Template{
		ID:           tmplID,
		Channel:      "email",
		HTMLTemplate: &htmlTmpl,
	}

	store.profiles[profID] = domain.Profile{
		ID:         profID,
		ExternalID: "ext-1",
	}

	store.jobs[jobID] = domain.DeliveryJob{
		ID:         jobID,
		CampaignID: campID,
		TenantID:   "tenant-1",
		Recipients: []domain.Recipient{
			{
				ProfileID: profID,
				Endpoint:  "test@example.com",
			},
		},
	}

	adapter := &testAdapter{}
	cfg := Config{
		Adapter: adapter,
	}

	processed, err := DeliverNext(context.Background(), store, "worker-1", cfg)
	if !processed || err != nil {
		t.Fatalf("unexpected err: %v", err)
	}

	foundRenderFailed := false
	for _, up := range store.updatedAttempts {
		if up == profID+":render_failed" {
			foundRenderFailed = true
		}
	}
	if !foundRenderFailed {
		t.Errorf("expected attempt to be updated to render_failed, got: %v", store.updatedAttempts)
	}
}

func TestDeliverNext_ExperimentVariantsAndHoldout(t *testing.T) {
	store := newMockStore()
	experimentID := "experiment-1"
	controlTemplateID, variantTemplateID := "template-control", "template-b"
	controlSubject, variantSubject := "control", "variant-b"
	store.templates[controlTemplateID] = domain.Template{ID: controlTemplateID, Channel: "email", SubjectTemplate: &controlSubject}
	store.templates[variantTemplateID] = domain.Template{ID: variantTemplateID, Channel: "email", SubjectTemplate: &variantSubject}
	store.experiments[experimentID] = domain.Experiment{
		ID: experimentID, Seed: "campaign-seed", HoldoutPct: 10,
		Variants: []domain.ExperimentVariant{
			{Label: "control", Weight: 50},
			{Label: "b", Weight: 50, TemplateID: &variantTemplateID},
		},
	}
	store.campaigns["campaign-1"] = domain.Campaign{ID: "campaign-1", TenantID: "tenant-1", WorkspaceID: "workspace-1", TemplateID: controlTemplateID, ExperimentID: &experimentID}

	job := domain.DeliveryJob{ID: "job-1", CampaignID: "campaign-1", TenantID: "tenant-1"}
	expected := map[string]int{}
	variants := []assignment.Variant{{Label: "control", Weight: 50}, {Label: "b", Weight: 50}}
	for i := 0; i < 500; i++ {
		profileID := fmt.Sprintf("profile-%03d", i)
		store.profiles[profileID] = domain.Profile{ID: profileID, ExternalID: "external-" + profileID}
		job.Recipients = append(job.Recipients, domain.Recipient{ProfileID: profileID, Endpoint: profileID + "@example.com"})
		label, _ := assignment.Assign("campaign-seed", profileID, variants, 10)
		expected[label]++
	}
	store.jobs[job.ID] = job
	adapter := &testAdapter{}
	processed, err := DeliverNext(context.Background(), store, "worker-1", Config{Adapter: adapter})
	if err != nil || !processed {
		t.Fatalf("DeliverNext = %v, %v", processed, err)
	}

	actual := map[string]int{}
	for _, attempt := range store.deliveryAttempts {
		actual[attempt.Variant]++
		if attempt.ExperimentID == nil || *attempt.ExperimentID != experimentID {
			t.Fatalf("attempt missing experiment stamp: %+v", attempt)
		}
		if attempt.Variant == "holdout" && attempt.Decision != "holdout" {
			t.Fatalf("holdout decision = %q", attempt.Decision)
		}
	}
	for _, label := range []string{"control", "b", "holdout"} {
		if expected[label] == 0 || actual[label] != expected[label] {
			t.Fatalf("%s assignments = %d, want deterministic %d", label, actual[label], expected[label])
		}
	}
	if len(adapter.messages) != len(job.Recipients)-expected["holdout"] {
		t.Fatalf("sends = %d, want %d", len(adapter.messages), len(job.Recipients)-expected["holdout"])
	}
	for _, message := range adapter.messages {
		profileID := strings.TrimSuffix(message.Endpoint, "@example.com")
		assigned := store.assignments[experimentID+":"+profileID].Variant
		wantSubject := controlSubject
		if assigned == "b" {
			wantSubject = variantSubject
		}
		if message.Subject != wantSubject {
			t.Fatalf("profile %s variant %s subject = %q, want %q", profileID, assigned, message.Subject, wantSubject)
		}
	}
	if len(store.emittedEvents) != len(adapter.messages) {
		t.Fatalf("message.sent events = %d, sends = %d", len(store.emittedEvents), len(adapter.messages))
	}
	for _, event := range store.emittedEvents {
		var payload map[string]any
		if err := json.Unmarshal(event.Payload, &payload); err != nil {
			t.Fatal(err)
		}
		if payload["experiment_id"] != experimentID || (payload["variant"] != "control" && payload["variant"] != "b") {
			t.Fatalf("event payload stamps = %v", payload)
		}
	}
}

func TestDeliverNext_SMSCampaign(t *testing.T) {
	store := newMockStore()

	campID := "camp-sms-1"
	tmplID := "tmpl-sms-1"
	profID := "prof-sms-1"
	jobID := "job-sms-1"

	store.campaigns[campID] = domain.Campaign{
		ID:          campID,
		TenantID:    "tenant-1",
		WorkspaceID: "workspace-1",
		TemplateID:  tmplID,
	}

	bodyTmpl := "Hello {{ profile.attributes.first_name }}!"
	store.templates[tmplID] = domain.Template{
		ID:           tmplID,
		Channel:      "sms",
		TextTemplate: &bodyTmpl,
	}

	store.profiles[profID] = domain.Profile{
		ID:         profID,
		ExternalID: "ext-sms-1",
	}

	store.jobs[jobID] = domain.DeliveryJob{
		ID:         jobID,
		CampaignID: campID,
		TenantID:   "tenant-1",
		Recipients: []domain.Recipient{
			{
				ProfileID: profID,
				Endpoint:  "+15555550100",
			},
		},
	}

	adapter := &testAdapter{}
	cfg := Config{
		Adapter: adapter,
	}

	processed, err := DeliverNext(context.Background(), store, "worker-1", cfg)
	if !processed {
		t.Fatalf("expected processed=true")
	}
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Verify delivery attempt
	attemptKey := campID + ":" + profID + ":sms:" + "+15555550100"
	attempt, ok := store.deliveryAttempts[attemptKey]
	if !ok {
		t.Fatalf("expected delivery attempt to be created")
	}
	if attempt.Channel != "sms" {
		t.Errorf("expected attempt.Channel = sms, got %q", attempt.Channel)
	}
	if attempt.Endpoint != "+15555550100" {
		t.Errorf("expected attempt.Endpoint = +15555550100, got %q", attempt.Endpoint)
	}
	if attempt.Decision != "sent" {
		t.Errorf("expected attempt.Decision = sent, got %q", attempt.Decision)
	}

	// Verify message sent via adapter
	if len(adapter.messages) != 1 {
		t.Fatalf("expected 1 sent message, got %d", len(adapter.messages))
	}
	msg := adapter.messages[0]
	if msg.Channel != "sms" {
		t.Errorf("expected msg.Channel = sms, got %q", msg.Channel)
	}
	if msg.Endpoint != "+15555550100" {
		t.Errorf("expected msg.Endpoint = +15555550100, got %q", msg.Endpoint)
	}

	// Verify emitted event
	if len(store.emittedEvents) != 1 {
		t.Fatalf("expected 1 emitted event, got %d", len(store.emittedEvents))
	}
	event := store.emittedEvents[0]
	if event.Type != "message.sent" {
		t.Errorf("expected event.Type = message.sent, got %q", event.Type)
	}
	var payload map[string]any
	if err := json.Unmarshal(event.Payload, &payload); err != nil {
		t.Fatal(err)
	}
	if payload["channel"] != "sms" {
		t.Errorf("expected payload channel = sms, got %v", payload["channel"])
	}
	if payload["endpoint"] != "+15555550100" {
		t.Errorf("expected payload endpoint = +15555550100, got %v", payload["endpoint"])
	}
}

func TestDeliverNext_PushCampaign(t *testing.T) {
	store := newMockStore()

	campID := "camp-push-1"
	tmplID := "tmpl-push-1"
	profID := "prof-push-1"
	jobID := "job-push-1"

	store.campaigns[campID] = domain.Campaign{
		ID:          campID,
		TenantID:    "tenant-1",
		WorkspaceID: "workspace-1",
		TemplateID:  tmplID,
	}

	bodyTmpl := "Hello {{ profile.attributes.first_name }}!"
	titleTmpl := "Greeting"
	store.templates[tmplID] = domain.Template{
		ID:            tmplID,
		Channel:       "push",
		BodyTemplate:  &bodyTmpl,
		TitleTemplate: &titleTmpl,
	}

	store.profiles[profID] = domain.Profile{
		ID:         profID,
		ExternalID: "ext-push-1",
	}

	// Active device tokens for resolving the provider
	store.deviceTokens = []domain.DeviceToken{
		{ProfileID: profID, Token: "token-push-100", Provider: "fcm", Status: "active"},
	}

	store.jobs[jobID] = domain.DeliveryJob{
		ID:         jobID,
		CampaignID: campID,
		TenantID:   "tenant-1",
		Recipients: []domain.Recipient{
			{
				ProfileID: profID,
				Endpoint:  "token-push-100",
			},
		},
	}

	adapter := &testAdapter{}
	cfg := Config{
		Adapter: adapter,
	}

	processed, err := DeliverNext(context.Background(), store, "worker-1", cfg)
	if !processed {
		t.Fatalf("expected processed=true")
	}
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Verify delivery attempt
	attemptKey := campID + ":" + profID + ":push:" + "token-push-100"
	attempt, ok := store.deliveryAttempts[attemptKey]
	if !ok {
		t.Fatalf("expected delivery attempt to be created")
	}
	if attempt.Channel != "push" {
		t.Errorf("expected attempt.Channel = push, got %q", attempt.Channel)
	}
	if attempt.Endpoint != "token-push-100" {
		t.Errorf("expected attempt.Endpoint = token-push-100, got %q", attempt.Endpoint)
	}
	if attempt.Decision != "sent" {
		t.Errorf("expected attempt.Decision = sent, got %q", attempt.Decision)
	}

	// Verify message sent via adapter
	if len(adapter.messages) != 1 {
		t.Fatalf("expected 1 sent message, got %d", len(adapter.messages))
	}
	msg := adapter.messages[0]
	if msg.Channel != "push" {
		t.Errorf("expected msg.Channel = push, got %q", msg.Channel)
	}
	if msg.Endpoint != "token-push-100" {
		t.Errorf("expected msg.Endpoint = token-push-100, got %q", msg.Endpoint)
	}
	if msg.Identity.Provider != "fcm" {
		t.Errorf("expected provider fcm resolved from token, got %q", msg.Identity.Provider)
	}
}

func TestDeliverNext_InvalidTokenRetirement_Campaign(t *testing.T) {
	store := newMockStore()

	campID := "camp-invalid-1"
	tmplID := "tmpl-invalid-1"
	profID := "prof-invalid-1"
	jobID := "job-invalid-1"
	badToken := "stale-token-999"

	bodyTmpl := "Hello {{name}}"
	titleTmpl := "Title"
	store.campaigns[campID] = domain.Campaign{
		ID:          campID,
		TenantID:    "tenant-1",
		WorkspaceID: "workspace-1",
		TemplateID:  tmplID,
	}
	store.templates[tmplID] = domain.Template{
		ID:            tmplID,
		Channel:       "push",
		BodyTemplate:  &bodyTmpl,
		TitleTemplate: &titleTmpl,
	}
	store.profiles[profID] = domain.Profile{
		ID:         profID,
		ExternalID: "ext-invalid-1",
	}
	store.deviceTokens = []domain.DeviceToken{
		{ProfileID: profID, Token: badToken, Provider: "fcm", Status: "active"},
	}
	store.jobs[jobID] = domain.DeliveryJob{
		ID:         jobID,
		CampaignID: campID,
		TenantID:   "tenant-1",
		Recipients: []domain.Recipient{
			{ProfileID: profID, Endpoint: badToken},
		},
	}

	// Adapter returns an InvalidToken error
	adapter := channels.NewFakeAdapter()
	adapter.SendErr = &channels.DeliveryError{
		Err:          fmt.Errorf("UNREGISTERED"),
		Retryable:    false,
		InvalidToken: true,
	}

	cfg := Config{FakeAdapter: adapter}

	processed, err := DeliverNext(context.Background(), store, "worker-1", cfg)
	if err != nil {
		t.Fatalf("expected no error from caller, got %v", err)
	}
	if !processed {
		t.Fatalf("expected processed=true")
	}

	// Token should be retired
	if len(store.retiredTokens) != 1 || store.retiredTokens[0] != badToken {
		t.Errorf("expected token %q retired, got %v", badToken, store.retiredTokens)
	}

	// UpdateDeliveryAttempt should have been called with 'failed'
	foundFailed := false
	for _, entry := range store.updatedAttempts {
		if entry == profID+":failed" {
			foundFailed = true
		}
	}
	if !foundFailed {
		t.Errorf("expected 'failed' decision in updatedAttempts, got %v", store.updatedAttempts)
	}

	// Adapter should not have recorded any successful send
	if len(adapter.GetSends()) != 0 {
		t.Errorf("expected 0 sends, got %d", len(adapter.GetSends()))
	}
}

func TestDeliverNext_InAppSuppression(t *testing.T) {
	store := newMockStore()
	campID := "camp-1"
	tmplID := "tmpl-in-app"
	profID := "prof-1"
	jobID := "job-1"

	store.campaigns[campID] = domain.Campaign{
		ID:          campID,
		TenantID:    "tenant-1",
		WorkspaceID: "workspace-1",
		TemplateID:  tmplID,
	}

	titleTmpl := "In-App Title"
	bodyTmpl := "In-App Body"
	store.templates[tmplID] = domain.Template{
		ID:            tmplID,
		Channel:       "in_app",
		TitleTemplate: &titleTmpl,
		BodyTemplate:  &bodyTmpl,
	}

	store.profiles[profID] = domain.Profile{
		ID:         profID,
		ExternalID: "ext-1",
	}

	store.jobs[jobID] = domain.DeliveryJob{
		ID:         jobID,
		CampaignID: campID,
		TenantID:   "tenant-1",
		Recipients: []domain.Recipient{
			{
				ProfileID: profID,
				Endpoint:  profID, // For in-app, endpoint is profile_id
			},
		},
	}

	// Mark profile as suppressed
	store.isSuppressedFunc = func(ctx context.Context, p domain.Principal, channel, endpoint string) (bool, error) {
		return true, nil
	}

	adapter := &testAdapter{}
	cfg := Config{
		Adapter: adapter,
	}

	processed, err := DeliverNext(context.Background(), store, "worker-1", cfg)
	if !processed || err != nil {
		t.Fatalf("unexpected err: %v", err)
	}

	foundSuppressed := false
	for _, up := range store.updatedAttempts {
		if up == profID+":suppressed" {
			foundSuppressed = true
		}
	}
	if !foundSuppressed {
		t.Errorf("expected in-app attempt to be updated to suppressed, got: %v", store.updatedAttempts)
	}
}

func TestDeliverNext_InAppFatigue(t *testing.T) {
	store := newMockStore()
	campID := "camp-1"
	tmplID := "tmpl-in-app"
	profID := "prof-1"
	jobID := "job-1"

	store.campaigns[campID] = domain.Campaign{
		ID:          campID,
		TenantID:    "tenant-1",
		WorkspaceID: "workspace-1",
		TemplateID:  tmplID,
	}

	titleTmpl := "In-App Title"
	bodyTmpl := "In-App Body"
	store.templates[tmplID] = domain.Template{
		ID:            tmplID,
		Channel:       "in_app",
		TitleTemplate: &titleTmpl,
		BodyTemplate:  &bodyTmpl,
	}

	store.profiles[profID] = domain.Profile{
		ID:         profID,
		ExternalID: "ext-1",
	}

	store.jobs[jobID] = domain.DeliveryJob{
		ID:         jobID,
		CampaignID: campID,
		TenantID:   "tenant-1",
		Recipients: []domain.Recipient{
			{
				ProfileID: profID,
				Endpoint:  profID,
			},
		},
	}

	// Mark profile as fatigued (already has 5 sends, which is the 24h cap)
	store.sentCountSince = 5

	adapter := &testAdapter{}
	cfg := Config{
		Adapter: adapter,
	}

	processed, err := DeliverNext(context.Background(), store, "worker-1", cfg)
	if !processed || err != nil {
		t.Fatalf("unexpected err: %v", err)
	}

	foundFatigued := false
	for _, up := range store.updatedAttempts {
		if up == profID+":fatigued" {
			foundFatigued = true
		}
	}
	if !foundFatigued {
		t.Errorf("expected in-app attempt to be updated to fatigued, got: %v", store.updatedAttempts)
	}
}

func TestDeliverNext_InAppNoConsent(t *testing.T) {
	store := newMockStore()
	campID := "camp-1"
	tmplID := "tmpl-in-app"
	profID := "prof-1"
	jobID := "job-1"

	store.campaigns[campID] = domain.Campaign{
		ID:          campID,
		TenantID:    "tenant-1",
		WorkspaceID: "workspace-1",
		TemplateID:  tmplID,
	}

	titleTmpl := "In-App Title"
	bodyTmpl := "In-App Body"
	store.templates[tmplID] = domain.Template{
		ID:            tmplID,
		Channel:       "in_app",
		TitleTemplate: &titleTmpl,
		BodyTemplate:  &bodyTmpl,
	}

	store.profiles[profID] = domain.Profile{
		ID:         profID,
		ExternalID: "ext-1",
	}

	store.jobs[jobID] = domain.DeliveryJob{
		ID:         jobID,
		CampaignID: campID,
		TenantID:   "tenant-1",
		Recipients: []domain.Recipient{
			{
				ProfileID: profID,
				Endpoint:  profID,
			},
		},
	}

	// Mark profile as unsubscribed
	store.consentState = "unsubscribed"

	adapter := &testAdapter{}
	cfg := Config{
		Adapter: adapter,
	}

	processed, err := DeliverNext(context.Background(), store, "worker-1", cfg)
	if !processed || err != nil {
		t.Fatalf("unexpected err: %v", err)
	}

	foundNoConsent := false
	for _, up := range store.updatedAttempts {
		if up == profID+":no_consent" {
			foundNoConsent = true
		}
	}
	if !foundNoConsent {
		t.Errorf("expected in-app attempt to be updated to no_consent, got: %v", store.updatedAttempts)
	}
}

