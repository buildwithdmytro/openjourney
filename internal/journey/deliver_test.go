package journey

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/buildwithdmytro/openjourney/internal/channels"
	"github.com/buildwithdmytro/openjourney/internal/domain"
)

func (m *mockStore) GetFirstAppID(ctx context.Context, tenantID, workspaceID string) (string, error) {
	return "app-1", nil
}

type appLookupErrorStore struct{ *mockStore }

func (s *appLookupErrorStore) GetFirstAppID(context.Context, string, string) (string, error) {
	return "", errors.New("no application")
}

func TestDeliverNextDoesNotFallbackToSyntheticApp(t *testing.T) {
	base := newMockStore()
	base.intents = append(base.intents, testPendingIntent())
	processed, err := DeliverNext(context.Background(), &appLookupErrorStore{base}, "worker-1", Config{})
	if !processed || err != nil {
		t.Fatalf("processed=%v err=%v", processed, err)
	}
	if got := base.intents[0]; got.Status != "failed" || got.ErrorMessage == nil || !strings.Contains(*got.ErrorMessage, "resolve app") {
		t.Fatalf("expected deterministic app lookup failure, got %+v", got)
	}
}

func TestDeliverNext_AcceptEventsFailureRetriesWithoutDuplicateSend(t *testing.T) {
	store := newMockStore()
	adapter := channels.NewFakeAdapter()
	store.intents = append(store.intents, testPendingIntent())
	store.profile = testDeliveryProfile()
	store.acceptEventsErr = errors.New("event store unavailable")

	processed, err := DeliverNext(context.Background(), store, "worker-1", Config{FakeAdapter: adapter})
	if !processed || err == nil {
		t.Fatalf("expected processed event publication error, got processed=%v err=%v", processed, err)
	}
	got := store.intents[0]
	if got.Status != "pending" || got.Decision == nil || *got.Decision != "provider_sent" {
		t.Fatalf("expected pending/provider_sent intent, got status=%q decision=%v", got.Status, got.Decision)
	}
	if len(adapter.GetSends()) != 1 {
		t.Fatalf("expected one provider send, got %d", len(adapter.GetSends()))
	}

	store.acceptEventsErr = nil
	store.suppressed = true
	processed, err = DeliverNext(context.Background(), store, "worker-1", Config{FakeAdapter: adapter})
	if !processed || err != nil {
		t.Fatalf("expected successful reconciliation, got processed=%v err=%v", processed, err)
	}
	if len(adapter.GetSends()) != 1 {
		t.Fatalf("reconciliation duplicated provider send; got %d sends", len(adapter.GetSends()))
	}
	got = store.intents[0]
	if got.Status != "completed" || got.Decision == nil || *got.Decision != "sent" {
		t.Fatalf("expected completed/sent intent, got status=%q decision=%v", got.Status, got.Decision)
	}
	if store.acceptEventsCalls != 2 {
		t.Fatalf("expected two event publication attempts, got %d", store.acceptEventsCalls)
	}
}

func TestDeliverNext_ProviderSentEmissionFailureDeadLetters(t *testing.T) {
	store := newMockStore()
	adapter := channels.NewFakeAdapter()
	store.intents = append(store.intents, testPendingIntent())
	store.profile = testDeliveryProfile()
	store.acceptEventsErr = errors.New("event store unavailable")

	for attempt := 1; attempt <= 3; attempt++ {
		processed, err := DeliverNext(context.Background(), store, "worker-1", Config{FakeAdapter: adapter})
		if !processed || err == nil {
			t.Fatalf("attempt %d: expected publication error, got processed=%v err=%v", attempt, processed, err)
		}
	}
	got := store.intents[0]
	if got.Status != "dead" || got.Decision == nil || *got.Decision != "provider_sent" {
		t.Fatalf("expected dead/provider_sent intent, got status=%q decision=%v", got.Status, got.Decision)
	}
	if len(adapter.GetSends()) != 1 {
		t.Fatalf("reconciliation duplicated provider send; got %d sends", len(adapter.GetSends()))
	}
}

func TestDeliverNext_ProviderSentPersistenceFailureStopsBeforeEvent(t *testing.T) {
	store := newMockStore()
	adapter := channels.NewFakeAdapter()
	store.intents = append(store.intents, testPendingIntent())
	store.profile = testDeliveryProfile()
	store.updateIntentHook = func(intent domain.JourneyMessageIntent) error {
		if intent.Decision != nil && *intent.Decision == "provider_sent" {
			return errors.New("database unavailable")
		}
		return nil
	}

	processed, err := DeliverNext(context.Background(), store, "worker-1", Config{FakeAdapter: adapter})
	if !processed || err == nil {
		t.Fatalf("expected provider_sent persistence error, got processed=%v err=%v", processed, err)
	}
	if len(adapter.GetSends()) != 1 {
		t.Fatalf("expected provider to be called once, got %d", len(adapter.GetSends()))
	}
	if store.acceptEventsCalls != 0 {
		t.Fatalf("event must not be published before provider_sent is durable; got %d calls", store.acceptEventsCalls)
	}
}

func testPendingIntent() domain.JourneyMessageIntent {
	return domain.JourneyMessageIntent{
		ID: "intent-durable", RunID: "run-1", TenantID: "tenant-1", WorkspaceID: "workspace-1",
		JourneyID: "journey-1", JourneyVersionID: "version-1", NodeID: "node-2",
		ProfileID: "profile-1", TemplateID: "template-1", Channel: "email",
		Endpoint: "test@example.com", Status: "pending",
	}
}

func testDeliveryProfile() *domain.Profile {
	return &domain.Profile{ID: "profile-1", ExternalID: "ext-1", Attributes: json.RawMessage(`{"name":"World"}`)}
}

func (m *mockStore) GetTemplate(ctx context.Context, p domain.Principal, id string) (domain.Template, error) {
	subj := "Hello {{name}}"
	html := "Body {{name}}"
	return domain.Template{
		ID:              id,
		Channel:         "email",
		SubjectTemplate: &subj,
		HTMLTemplate:    &html,
	}, nil
}

func (m *mockStore) GetTenantFatigueQuotas(ctx context.Context, p domain.Principal) (int, int, error) {
	return 5, 20, nil
}

func (m *mockStore) IsSuppressed(ctx context.Context, p domain.Principal, channel, endpoint string) (bool, error) {
	return m.suppressed, nil
}

func (m *mockStore) LatestConsent(ctx context.Context, p domain.Principal, profileID, channel, topic string) (domain.Consent, error) {
	return domain.Consent{
		State: "subscribed",
	}, nil
}

func (m *mockStore) SentCountSince(ctx context.Context, p domain.Principal, profileID string, since time.Time) (int, error) {
	return 0, nil
}

func (m *mockStore) GetSendingIdentity(ctx context.Context, p domain.Principal, id string) (domain.SendingIdentity, error) {
	return domain.SendingIdentity{
		Channel:     "email",
		Provider:    "fake",
		MaxSendRate: 10,
	}, nil
}

func (m *mockStore) GetTenantQuietHours(ctx context.Context, p domain.Principal) (*int, *int, string, error) {
	return m.quietHoursStart, m.quietHoursEnd, m.defaultTimezone, nil
}

func TestDeliverNext_Success(t *testing.T) {
	store := newMockStore()
	fakeAdapter := channels.NewFakeAdapter()
	cfg := Config{
		FakeAdapter: fakeAdapter,
	}

	// Create an intent
	intent := domain.JourneyMessageIntent{
		ID:               "intent-1",
		RunID:            "run-1",
		TenantID:         "tenant-1",
		WorkspaceID:      "workspace-1",
		JourneyID:        "journey-1",
		JourneyVersionID: "version-1",
		NodeID:           "node-2",
		ProfileID:        "profile-1",
		TemplateID:       "template-1",
		Channel:          "email",
		Endpoint:         "test@example.com",
		Status:           "pending",
	}
	store.intents = append(store.intents, intent)

	// Add profile attributes
	store.profile = &domain.Profile{
		ID:         "profile-1",
		ExternalID: "ext-1",
		Attributes: json.RawMessage(`{"name":"World"}`),
	}

	processed, err := DeliverNext(context.Background(), store, "worker-1", cfg)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if !processed {
		t.Fatalf("expected processed to be true")
	}

	// Verify that the intent is updated to completed/sent
	if len(store.intents) == 0 {
		t.Fatalf("expected intents to be updated")
	}
	updatedIntent := store.intents[0]
	if updatedIntent.Status != "completed" {
		t.Errorf("expected status completed, got %s", updatedIntent.Status)
	}
	if updatedIntent.Decision == nil || *updatedIntent.Decision != "sent" {
		t.Errorf("expected decision sent, got %v", updatedIntent.Decision)
	}

	// Verify send through fake adapter
	sentMsgs := fakeAdapter.GetSends()
	if len(sentMsgs) != 1 {
		t.Fatalf("expected 1 message sent, got %d", len(sentMsgs))
	}
	msg := sentMsgs[0]
	if msg.Subject != "Hello World" {
		t.Errorf("expected rendered subject Hello World, got %s", msg.Subject)
	}
	if msg.HTML != "Body World" {
		t.Errorf("expected rendered html Body World, got %s", msg.HTML)
	}
}

func TestDeliverNext_TransactionalBypass(t *testing.T) {
	store := newMockStore()
	fakeAdapter := channels.NewFakeAdapter()
	cfg := Config{
		FakeAdapter: fakeAdapter,
	}

	// Create a transactional intent
	intent := domain.JourneyMessageIntent{
		ID:               "intent-1",
		RunID:            "run-1",
		TenantID:         "tenant-1",
		WorkspaceID:      "workspace-1",
		JourneyID:        "journey-1",
		JourneyVersionID: "version-1",
		NodeID:           "node-2",
		ProfileID:        "profile-1",
		TemplateID:       "template-1",
		Channel:          "email",
		Endpoint:         "test@example.com",
		Status:           "pending",
		Transactional:    true, // Transactional should bypass consent & fatigue!
	}
	store.intents = append(store.intents, intent)

	store.profile = &domain.Profile{
		ID:         "profile-1",
		ExternalID: "ext-1",
		Attributes: json.RawMessage(`{"name":"World"}`),
	}

	processed, err := DeliverNext(context.Background(), store, "worker-1", cfg)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if !processed {
		t.Fatalf("expected processed to be true")
	}

	updatedIntent := store.intents[0]
	if updatedIntent.Status != "completed" {
		t.Errorf("expected status completed, got %s", updatedIntent.Status)
	}
	if updatedIntent.Decision == nil || *updatedIntent.Decision != "sent" {
		t.Errorf("expected decision sent, got %v", updatedIntent.Decision)
	}
}

func TestDeliverNext_QuietHours(t *testing.T) {
	// 1. Marketing message within quiet hours should be deferred
	{
		store := newMockStore()
		fakeAdapter := channels.NewFakeAdapter()
		// Setup quiet hours from 22:00 to 08:00 UTC
		store.quietHoursStart = ptrInt(22)
		store.quietHoursEnd = ptrInt(8)
		store.defaultTimezone = "UTC"

		cfg := Config{
			FakeAdapter: fakeAdapter,
			Clock:       NewFakeClock(time.Date(2026, 7, 9, 23, 0, 0, 0, time.UTC)), // 23:00 UTC is inside quiet hours
		}

		intent := domain.JourneyMessageIntent{
			ID:               "intent-1",
			RunID:            "run-1",
			TenantID:         "tenant-1",
			WorkspaceID:      "workspace-1",
			JourneyID:        "journey-1",
			JourneyVersionID: "version-1",
			NodeID:           "node-2",
			ProfileID:        "profile-1",
			TemplateID:       "template-1",
			Channel:          "email",
			Endpoint:         "test@example.com",
			Status:           "pending",
			Attempts:         1,
			Transactional:    false, // Marketing message
		}
		store.intents = append(store.intents, intent)

		store.profile = &domain.Profile{
			ID:         "profile-1",
			ExternalID: "ext-1",
			Attributes: json.RawMessage(`{"name":"World"}`),
		}

		processed, err := DeliverNext(context.Background(), store, "worker-1", cfg)
		if err != nil {
			t.Fatalf("expected no error, got %v", err)
		}
		if !processed {
			t.Fatalf("expected processed to be true")
		}

		updatedIntent := store.intents[0]
		if updatedIntent.Status != "pending" {
			t.Errorf("expected status pending (deferred), got %s", updatedIntent.Status)
		}
		if updatedIntent.Attempts != 1 {
			t.Errorf("expected attempts to be decremented back to 1, got %d", updatedIntent.Attempts)
		}
		// Expect deferral to next open time (tomorrow morning at 08:00 UTC)
		expectedOpen := time.Date(2026, 7, 10, 8, 0, 0, 0, time.UTC)
		if !updatedIntent.AvailableAt.Equal(expectedOpen) {
			t.Errorf("expected AvailableAt %v, got %v", expectedOpen, updatedIntent.AvailableAt)
		}

		// Adapter should not have sent any message
		if len(fakeAdapter.GetSends()) != 0 {
			t.Errorf("expected no messages sent, got %d", len(fakeAdapter.GetSends()))
		}
	}

	// 2. Transactional message within quiet hours should bypass quiet hours and go through
	{
		store := newMockStore()
		fakeAdapter := channels.NewFakeAdapter()
		store.quietHoursStart = ptrInt(22)
		store.quietHoursEnd = ptrInt(8)
		store.defaultTimezone = "UTC"

		cfg := Config{
			FakeAdapter: fakeAdapter,
			Clock:       NewFakeClock(time.Date(2026, 7, 9, 23, 0, 0, 0, time.UTC)), // 23:00 UTC is inside quiet hours
		}

		intent := domain.JourneyMessageIntent{
			ID:               "intent-2",
			RunID:            "run-1",
			TenantID:         "tenant-1",
			WorkspaceID:      "workspace-1",
			JourneyID:        "journey-1",
			JourneyVersionID: "version-1",
			NodeID:           "node-2",
			ProfileID:        "profile-1",
			TemplateID:       "template-1",
			Channel:          "email",
			Endpoint:         "test@example.com",
			Status:           "pending",
			Attempts:         1,
			Transactional:    true, // Transactional message bypasses quiet hours!
		}
		store.intents = append(store.intents, intent)

		store.profile = &domain.Profile{
			ID:         "profile-1",
			ExternalID: "ext-1",
			Attributes: json.RawMessage(`{"name":"World"}`),
		}

		processed, err := DeliverNext(context.Background(), store, "worker-1", cfg)
		if err != nil {
			t.Fatalf("expected no error, got %v", err)
		}
		if !processed {
			t.Fatalf("expected processed to be true")
		}

		updatedIntent := store.intents[0]
		if updatedIntent.Status != "completed" {
			t.Errorf("expected status completed, got %s", updatedIntent.Status)
		}
		if updatedIntent.Decision == nil || *updatedIntent.Decision != "sent" {
			t.Errorf("expected decision sent, got %v", updatedIntent.Decision)
		}

		// Adapter should have sent the message
		if len(fakeAdapter.GetSends()) != 1 {
			t.Errorf("expected exactly 1 message sent, got %d", len(fakeAdapter.GetSends()))
		}
	}
}
