package campaigns

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/buildwithdmytro/openjourney/internal/channels"
	"github.com/buildwithdmytro/openjourney/internal/domain"
	"github.com/buildwithdmytro/openjourney/internal/ports"
)

type mockStore struct {
	ports.Store
	jobs                   map[string]domain.DeliveryJob
	campaigns              map[string]domain.Campaign
	templates              map[string]domain.Template
	identities             map[string]domain.SendingIdentity
	profiles               map[string]domain.Profile
	deliveryAttempts       map[string]domain.DeliveryAttempt
	deletedAttempts        []string
	failedJobs             map[string]string
	completedJobs          map[string]bool
	createdAttempts        []domain.DeliveryAttempt
	updatedAttempts        []string
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

func (m *mockStore) GetSendingIdentity(ctx context.Context, p domain.Principal, id string) (domain.SendingIdentity, error) {
	i, ok := m.identities[id]
	if !ok {
		return domain.SendingIdentity{}, errors.New("identity not found")
	}
	return i, nil
}

func (m *mockStore) CreateDeliveryAttempt(ctx context.Context, attempt domain.DeliveryAttempt) (bool, error) {
	m.createdAttempts = append(m.createdAttempts, attempt)
	key := attempt.CampaignID + ":" + attempt.ProfileID + ":" + attempt.Channel
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

func (m *mockStore) DeleteDeliveryAttempt(ctx context.Context, tenantID, campaignID, profileID, channel string) error {
	m.deletedAttempts = append(m.deletedAttempts, profileID)
	key := campaignID + ":" + profileID + ":" + channel
	delete(m.deliveryAttempts, key)
	return nil
}

func (m *mockStore) UpdateDeliveryAttempt(ctx context.Context, campaignID, profileID, channel, decision, reason, providerMsgID string, policySnapshot []byte) error {
	m.updatedAttempts = append(m.updatedAttempts, profileID+":"+decision)
	key := campaignID + ":" + profileID + ":" + channel
	if att, ok := m.deliveryAttempts[key]; ok {
		att.Decision = decision
		att.Reason = reason
		att.ProviderMessageID = providerMsgID
		m.deliveryAttempts[key] = att
	}
	return nil
}

func (m *mockStore) AcceptEvents(ctx context.Context, p domain.Principal, events []domain.Event) ([]string, error) {
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
		State:      "subscribed",
		OccurredAt: time.Now(),
	}, nil
}

func (m *mockStore) IsSuppressed(ctx context.Context, p domain.Principal, channel, endpoint string) (bool, error) {
	return false, nil
}

func (m *mockStore) SentCountSince(ctx context.Context, p domain.Principal, profileID string, since time.Time) (int, error) {
	return 0, nil
}

type testAdapter struct {
	err error
}

func (a *testAdapter) Send(ctx context.Context, msg ports.RenderedMessage) (string, error) {
	return "msg-123", a.err
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

	if len(store.deletedAttempts) != 1 || store.deletedAttempts[0] != profID {
		t.Errorf("expected attempt for profile %s to be deleted, got: %v", profID, store.deletedAttempts)
	}

	if _, ok := store.completedJobs[jobID]; ok {
		t.Errorf("expected job %s to not be completed", jobID)
	}
	if _, ok := store.failedJobs[jobID]; !ok {
		t.Errorf("expected job %s to be failed/requeued", jobID)
	}
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
