package channels

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/buildwithdmytro/openjourney/internal/domain"
	"github.com/buildwithdmytro/openjourney/internal/ports"
	"github.com/stretchr/testify/require"
)

// mockInAppStore provides a minimal mock Store for testing the InAppAdapter.
type mockInAppStore struct {
	createInAppCalls int
	lastMessage      domain.InAppMessage
}

func (m *mockInAppStore) GetProfileByIDSystem(ctx context.Context, tenantID, workspaceID, profileID string) (domain.Profile, error) {
	return domain.Profile{
		ID:          profileID,
		ExternalID:  "",
		AnonymousID: "",
	}, nil
}

func (m *mockInAppStore) GetProfileAppID(ctx context.Context, tenantID, workspaceID, profileID string) (string, error) {
	return "app-123", nil
}

func (m *mockInAppStore) CreateInAppMessage(ctx context.Context, tenantID, workspaceID, appID, profileID string, msg domain.InAppMessage) (domain.InAppMessage, error) {
	m.createInAppCalls++
	m.lastMessage = msg
	msg.ID = "msg-123"
	msg.TenantID = tenantID
	msg.WorkspaceID = workspaceID
	msg.AppID = appID
	msg.ProfileID = profileID
	msg.CreatedAt = time.Now().UTC()
	msg.UpdatedAt = time.Now().UTC()
	return msg, nil
}

// Stub out other methods
func (m *mockInAppStore) Close() error { return nil }
func (m *mockInAppStore) Ping(ctx context.Context) error { return nil }
func (m *mockInAppStore) CreateProfile(ctx context.Context, p domain.Principal, profile domain.Profile) (domain.Profile, error) {
	return domain.Profile{}, nil
}
func (m *mockInAppStore) UpdateProfile(ctx context.Context, p domain.Principal, profile domain.Profile) (domain.Profile, error) {
	return domain.Profile{}, nil
}
func (m *mockInAppStore) ListProfiles(ctx context.Context, p domain.Principal) ([]domain.Profile, error) {
	return nil, nil
}
func (m *mockInAppStore) GetProfileIdentity(ctx context.Context, tenantID, externalID string) (domain.Profile, error) {
	return domain.Profile{}, nil
}
func (m *mockInAppStore) UpdateProfileIdentity(ctx context.Context, tenantID string, p domain.Profile) (domain.Profile, error) {
	return domain.Profile{}, nil
}
func (m *mockInAppStore) DeleteProfilesByTenant(ctx context.Context, tenantID string) error {
	return nil
}
func (m *mockInAppStore) CreateIdentityMerge(ctx context.Context, sourceEventID, sourceProfileID, targetProfileID, tenantID string, metadata json.RawMessage) (domain.IdentityMerge, error) {
	return domain.IdentityMerge{}, nil
}
func (m *mockInAppStore) AcceptEvents(ctx context.Context, tenantID string, events []domain.Event) ([]domain.AcceptedEvent, error) {
	return nil, nil
}
func (m *mockInAppStore) ListEvents(ctx context.Context, p domain.Principal, filter domain.ListEventsFilter) ([]domain.Event, error) {
	return nil, nil
}
func (m *mockInAppStore) GetEvent(ctx context.Context, p domain.Principal, id string) (domain.Event, error) {
	return domain.Event{}, nil
}
func (m *mockInAppStore) ListAcceptedEvents(ctx context.Context, p domain.Principal, filter domain.ListAcceptedEventsFilter) ([]domain.AcceptedEvent, error) {
	return nil, nil
}
func (m *mockInAppStore) ProjectEvent(ctx context.Context, e domain.AcceptedEvent) error {
	return nil
}
func (m *mockInAppStore) CreateSendingIdentity(ctx context.Context, p domain.Principal, si domain.SendingIdentity) (domain.SendingIdentity, error) {
	return domain.SendingIdentity{}, nil
}
func (m *mockInAppStore) GetSendingIdentity(ctx context.Context, p domain.Principal, id string) (domain.SendingIdentity, error) {
	return domain.SendingIdentity{}, nil
}
func (m *mockInAppStore) ListSendingIdentities(ctx context.Context, p domain.Principal, channel string) ([]domain.SendingIdentity, error) {
	return nil, nil
}
func (m *mockInAppStore) ListSendingIdentitiesForApp(ctx context.Context, tenantID, appID, channel string) ([]domain.SendingIdentity, error) {
	return nil, nil
}
func (m *mockInAppStore) GetSendingIdentityByExternalID(ctx context.Context, p domain.Principal, externalID string) (domain.SendingIdentity, error) {
	return domain.SendingIdentity{}, nil
}
func (m *mockInAppStore) UpdateSendingIdentity(ctx context.Context, p domain.Principal, si domain.SendingIdentity) (domain.SendingIdentity, error) {
	return domain.SendingIdentity{}, nil
}
func (m *mockInAppStore) DeleteSendingIdentity(ctx context.Context, p domain.Principal, id string) error {
	return nil
}
func (m *mockInAppStore) UpdateTemplateDisplay(ctx context.Context, tenantID, id string, displayName string) error {
	return nil
}
func (m *mockInAppStore) CreateTemplate(ctx context.Context, p domain.Principal, t domain.Template) (domain.Template, error) {
	return domain.Template{}, nil
}
func (m *mockInAppStore) GetTemplate(ctx context.Context, p domain.Principal, id string) (domain.Template, error) {
	return domain.Template{}, nil
}
func (m *mockInAppStore) ListTemplates(ctx context.Context, p domain.Principal) ([]domain.Template, error) {
	return nil, nil
}
func (m *mockInAppStore) UpdateTemplate(ctx context.Context, p domain.Principal, t domain.Template) (domain.Template, error) {
	return domain.Template{}, nil
}
func (m *mockInAppStore) DeleteTemplate(ctx context.Context, p domain.Principal, id string) error {
	return nil
}
func (m *mockInAppStore) ListDeviceTokens(ctx context.Context, p domain.Principal, platform string) ([]domain.DeviceToken, error) {
	return nil, nil
}
func (m *mockInAppStore) CreateDeviceToken(ctx context.Context, tenantID, workspaceID, appID, profileID, platform, provider, token string) (domain.DeviceToken, error) {
	return domain.DeviceToken{}, nil
}
func (m *mockInAppStore) RetireDeviceTokenByID(ctx context.Context, tenantID, id string) error {
	return nil
}
func (m *mockInAppStore) ListActiveDeviceTokens(ctx context.Context, tenantID, workspaceID, profileID string) ([]domain.DeviceToken, error) {
	return nil, nil
}
func (m *mockInAppStore) ListDeviceTokensByProfile(ctx context.Context, tenantID, workspaceID, profileID string) ([]domain.DeviceToken, error) {
	return nil, nil
}
func (m *mockInAppStore) CreateInAppMessage(ctx context.Context, tenantID, workspaceID, appID, profileID string, msg domain.InAppMessage) (domain.InAppMessage, error) {
	m.createInAppCalls++
	m.lastMessage = msg
	msg.ID = "msg-123"
	msg.TenantID = tenantID
	msg.WorkspaceID = workspaceID
	msg.AppID = appID
	msg.ProfileID = profileID
	msg.CreatedAt = time.Now().UTC()
	msg.UpdatedAt = time.Now().UTC()
	return msg, nil
}
func (m *mockInAppStore) GetInAppMessage(ctx context.Context, tenantID, msgID string) (domain.InAppMessage, error) {
	return domain.InAppMessage{}, nil
}
func (m *mockInAppStore) ListInboxForProfile(ctx context.Context, tenantID, appID, profileID string, limit int) ([]domain.InAppMessage, error) {
	return nil, nil
}
func (m *mockInAppStore) ListInAppMessages(ctx context.Context, p domain.Principal, appID string) ([]domain.InAppMessage, error) {
	return nil, nil
}
func (m *mockInAppStore) CreateCampaign(ctx context.Context, p domain.Principal, c domain.Campaign) (domain.Campaign, error) {
	return domain.Campaign{}, nil
}
func (m *mockInAppStore) GetCampaign(ctx context.Context, p domain.Principal, id string) (domain.Campaign, error) {
	return domain.Campaign{}, nil
}
func (m *mockInAppStore) GetCampaignSystem(ctx context.Context, tenantID, id string) (domain.Campaign, error) {
	return domain.Campaign{}, nil
}
func (m *mockInAppStore) UpdateCampaign(ctx context.Context, p domain.Principal, c domain.Campaign) (domain.Campaign, error) {
	return domain.Campaign{}, nil
}
func (m *mockInAppStore) ListCampaigns(ctx context.Context, p domain.Principal) ([]domain.Campaign, error) {
	return nil, nil
}
func (m *mockInAppStore) CreateExperiment(ctx context.Context, p domain.Principal, experiment domain.Experiment) (domain.Experiment, error) {
	return domain.Experiment{}, nil
}
func (m *mockInAppStore) GetExperiment(ctx context.Context, p domain.Principal, id string) (domain.Experiment, error) {
	return domain.Experiment{}, nil
}
func (m *mockInAppStore) ListExperiments(ctx context.Context, p domain.Principal) ([]domain.Experiment, error) {
	return nil, nil
}
func (m *mockInAppStore) UpdateExperiment(ctx context.Context, p domain.Principal, e domain.Experiment) (domain.Experiment, error) {
	return domain.Experiment{}, nil
}
func (m *mockInAppStore) CreateApp(ctx context.Context, p domain.Principal, app domain.App) (domain.App, error) {
	return domain.App{}, nil
}
func (m *mockInAppStore) GetApp(ctx context.Context, p domain.Principal, id string) (domain.App, error) {
	return domain.App{}, nil
}
func (m *mockInAppStore) ListApps(ctx context.Context, p domain.Principal) ([]domain.App, error) {
	return nil, nil
}
func (m *mockInAppStore) UpdateApp(ctx context.Context, p domain.Principal, app domain.App) (domain.App, error) {
	return domain.App{}, nil
}
func (m *mockInAppStore) DeleteApp(ctx context.Context, p domain.Principal, id string) error {
	return nil
}
func (m *mockInAppStore) GetAppByName(ctx context.Context, tenantID, appName string) (domain.App, error) {
	return domain.App{}, nil
}
func (m *mockInAppStore) GetFirstAppID(ctx context.Context, tenantID string) (string, error) {
	return "", nil
}
func (m *mockInAppStore) UpdateDeliveryAttempt(ctx context.Context, tenantID, id string, status string, data json.RawMessage) (domain.DeliveryAttempt, error) {
	return domain.DeliveryAttempt{}, nil
}
func (m *mockInAppStore) GetDeliveryAttempt(ctx context.Context, tenantID, id string) (domain.DeliveryAttempt, error) {
	return domain.DeliveryAttempt{}, nil
}
func (m *mockInAppStore) ListDeliveryAttempts(ctx context.Context, p domain.Principal) ([]domain.DeliveryAttempt, error) {
	return nil, nil
}
func (m *mockInAppStore) ClaimDueJourneyMessages(ctx context.Context, limit int) ([]domain.JourneyMessageIntent, error) {
	return nil, nil
}
func (m *mockInAppStore) UpdateJourneyMessageStatus(ctx context.Context, intent domain.JourneyMessageIntent, status string) error {
	return nil
}
func (m *mockInAppStore) CreateJourneyRun(ctx context.Context, tenantID, journeyID string, profileID string, sourceEventID string) (domain.JourneyRun, error) {
	return domain.JourneyRun{}, nil
}
func (m *mockInAppStore) GetJourneyRun(ctx context.Context, tenantID, id string) (domain.JourneyRun, error) {
	return domain.JourneyRun{}, nil
}
func (m *mockInAppStore) GetJourneyRunInternal(ctx context.Context, tenantID, journeyID, profileID string) (*domain.JourneyRunInternal, error) {
	return nil, nil
}
func (m *mockInAppStore) UpdateJourneyRun(ctx context.Context, tenantID string, run *domain.JourneyRun) error {
	return nil
}
func (m *mockInAppStore) CreateJourney(ctx context.Context, p domain.Principal, j domain.Journey) (domain.Journey, error) {
	return domain.Journey{}, nil
}
func (m *mockInAppStore) GetJourney(ctx context.Context, p domain.Principal, id string) (domain.Journey, error) {
	return domain.Journey{}, nil
}
func (m *mockInAppStore) GetJourneySystem(ctx context.Context, tenantID, id string) (domain.Journey, error) {
	return domain.Journey{}, nil
}
func (m *mockInAppStore) ListJourneys(ctx context.Context, p domain.Principal) ([]domain.Journey, error) {
	return nil, nil
}
func (m *mockInAppStore) PublishJourney(ctx context.Context, p domain.Principal, id string) error {
	return nil
}
func (m *mockInAppStore) UnpublishJourney(ctx context.Context, p domain.Principal, id string) error {
	return nil
}
func (m *mockInAppStore) GetConsentLedger(ctx context.Context, tenantID, profileID string) (domain.ConsentLedger, error) {
	return domain.ConsentLedger{}, nil
}
func (m *mockInAppStore) UpdateConsentLedger(ctx context.Context, tenantID, profileID string, ledger domain.ConsentLedger) error {
	return nil
}
func (m *mockInAppStore) GetSuppressions(ctx context.Context, p domain.Principal) ([]domain.Suppression, error) {
	return nil, nil
}
func (m *mockInAppStore) CreateSuppression(ctx context.Context, p domain.Principal, s domain.Suppression) (domain.Suppression, error) {
	return domain.Suppression{}, nil
}
func (m *mockInAppStore) CreateExperimentResult(ctx context.Context, p domain.Principal, er domain.ExperimentResult) (domain.ExperimentResult, error) {
	return domain.ExperimentResult{}, nil
}
func (m *mockInAppStore) ListExperimentResults(ctx context.Context, p domain.Principal) ([]domain.ExperimentResult, error) {
	return nil, nil
}
func (m *mockInAppStore) ListExperimentResultsByExperiment(ctx context.Context, tenantID, experimentID string) ([]domain.ExperimentResult, error) {
	return nil, nil
}

func TestInAppAdapter_Send(t *testing.T) {
	ctx := context.Background()
	store := &mockInAppStore{}
	adapter := NewInAppAdapter(store)

	msg := ports.RenderedMessage{
		Channel:        "in_app",
		Endpoint:       "profile-123",
		Subject:        "Test Subject",
		Title:          "Test Title",
		HTML:           "<p>Test HTML</p>",
		Text:           "Test Text",
		Body:           "Test Body",
		Identity:       domain.SendingIdentity{Channel: "in_app", Provider: "inapp"},
		IdempotencyKey: "key-123",
		Data: map[string]string{
			"key": "value",
		},
	}

	providerID, err := adapter.Send(ctx, msg)

	require.NoError(t, err)
	require.Equal(t, "msg-123", providerID)
	require.Equal(t, 1, store.createInAppCalls)

	// Verify the message was created with correct content
	lastMsg := store.lastMessage
	require.Equal(t, "modal", lastMsg.MessageType)
	require.Equal(t, "delivered", lastMsg.Status)
	require.NotNil(t, lastMsg.DeliveredAt)

	// Verify content is properly marshaled
	var content map[string]interface{}
	err = json.Unmarshal(lastMsg.Content, &content)
	require.NoError(t, err)
	require.Equal(t, "Test Subject", content["subject"])
	require.Equal(t, "Test Title", content["title"])
	require.Equal(t, "<p>Test HTML</p>", content["html"])
	require.Equal(t, "Test Text", content["text"])
	require.Equal(t, "Test Body", content["body"])
	require.NotNil(t, content["data"])
}

func TestInAppAdapter_SendIdempotency(t *testing.T) {
	ctx := context.Background()
	store := &mockInAppStore{}
	adapter := NewInAppAdapter(store)

	msg := ports.RenderedMessage{
		Channel:        "in_app",
		Endpoint:       "profile-123",
		Title:          "Test Title",
		Body:           "Test Body",
		Identity:       domain.SendingIdentity{Channel: "in_app", Provider: "inapp"},
		IdempotencyKey: "key-123",
	}

	providerID1, err := adapter.Send(ctx, msg)
	require.NoError(t, err)

	providerID2, err := adapter.Send(ctx, msg)
	require.NoError(t, err)

	// Both should return the same provider ID (idempotency key matches)
	require.Equal(t, providerID1, providerID2)
	// But the CreateInAppMessage should be called twice (the DB handles the conflict)
	require.Equal(t, 2, store.createInAppCalls)
}

func TestInAppAdapter_InvalidChannel(t *testing.T) {
	ctx := context.Background()
	store := &mockInAppStore{}
	adapter := NewInAppAdapter(store)

	msg := ports.RenderedMessage{
		Channel:  "email",
		Endpoint: "profile-123",
		Identity: domain.SendingIdentity{Channel: "email"},
	}

	_, err := adapter.Send(ctx, msg)
	require.Error(t, err)
	require.Contains(t, err.Error(), "invalid channel for in-app")
}

func TestInAppAdapter_EmptyEndpoint(t *testing.T) {
	ctx := context.Background()
	store := &mockInAppStore{}
	adapter := NewInAppAdapter(store)

	msg := ports.RenderedMessage{
		Channel:  "in_app",
		Endpoint: "",
		Identity: domain.SendingIdentity{Channel: "in_app"},
	}

	_, err := adapter.Send(ctx, msg)
	require.Error(t, err)
	require.Contains(t, err.Error(), "empty profile endpoint")
}

func TestInAppAdapter_ValidateConfig(t *testing.T) {
	ctx := context.Background()
	store := &mockInAppStore{}
	adapter := NewInAppAdapter(store)

	tests := []struct {
		name    string
		identity domain.SendingIdentity
		wantErr bool
	}{
		{
			name: "valid config",
			identity: domain.SendingIdentity{
				Channel:  "in_app",
				Provider: "inapp",
			},
			wantErr: false,
		},
		{
			name: "invalid channel",
			identity: domain.SendingIdentity{
				Channel:  "email",
				Provider: "inapp",
			},
			wantErr: true,
		},
		{
			name: "invalid provider",
			identity: domain.SendingIdentity{
				Channel:  "in_app",
				Provider: "webhook",
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := adapter.ValidateConfig(tt.identity)
			if tt.wantErr {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
			}
		})
	}
}
