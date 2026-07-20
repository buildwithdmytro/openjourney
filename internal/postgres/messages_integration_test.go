package postgres

import (
	"context"
	"encoding/json"
	"os"
	"testing"
	"time"

	"github.com/buildwithdmytro/openjourney/internal/domain"
	"github.com/buildwithdmytro/openjourney/internal/ports"
)

func TestInAppMessageRoundTrip(t *testing.T) {
	databaseURL := os.Getenv("OPENJOURNEY_TEST_DATABASE_URL")
	if databaseURL == "" {
		t.Skip("OPENJOURNEY_TEST_DATABASE_URL is not configured")
	}
	ctx := context.Background()
	store, err := Open(ctx, databaseURL)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	if err := store.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	p, tenantID := setupTestTenant(t, ctx, store)
	profileID := testUUID(tenantID + "-msg-profile")
	if _, err := store.pool.Exec(ctx, `INSERT INTO profiles(id,tenant_id,workspace_id,app_id,external_id,attributes) VALUES($1,$2,$3,$4,'msg-user-1','{}')`, profileID, p.TenantID, p.WorkspaceID, p.AppID); err != nil {
		t.Fatal(err)
	}

	now := time.Now().UTC()
	msgContent := json.RawMessage(`{"title":"Hello","body":"Test message"}`)
	idempotencyKey := "test-idem-key-1"
	msg := domain.InAppMessage{
		MessageType:    "modal",
		Content:        msgContent,
		Rank:           1,
		Categories:     []string{"promotional"},
		StartAt:        now,
		ExpiresAt:      nil,
		IdempotencyKey: &idempotencyKey,
		Status:         "pending",
	}

	created, err := store.CreateInAppMessage(ctx, p.TenantID, p.WorkspaceID, p.AppID, profileID, msg)
	if err != nil {
		t.Fatalf("CreateInAppMessage failed: %v", err)
	}
	if created.ID == "" {
		t.Fatal("expected created message ID, got empty")
	}
	if created.Status != "pending" {
		t.Fatalf("expected status pending, got %s", created.Status)
	}
	if created.TenantID != p.TenantID || created.WorkspaceID != p.WorkspaceID || created.AppID != p.AppID {
		t.Fatal("scoping mismatch: tenant/workspace/app not inherited from profile")
	}
	if created.ProfileID != profileID {
		t.Fatalf("expected profileID %s, got %s", profileID, created.ProfileID)
	}

	fetched, err := store.GetInAppMessage(ctx, p.TenantID, created.ID)
	if err != nil {
		t.Fatalf("GetInAppMessage failed: %v", err)
	}
	if fetched.ID != created.ID {
		t.Fatalf("expected ID %s, got %s", created.ID, fetched.ID)
	}
	if string(fetched.Content) != string(msgContent) {
		t.Fatalf("expected content %s, got %s", msgContent, fetched.Content)
	}
}

func TestInBoxFetchFiltering(t *testing.T) {
	databaseURL := os.Getenv("OPENJOURNEY_TEST_DATABASE_URL")
	if databaseURL == "" {
		t.Skip("OPENJOURNEY_TEST_DATABASE_URL is not configured")
	}
	ctx := context.Background()
	store, err := Open(ctx, databaseURL)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	if err := store.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	p, tenantID := setupTestTenant(t, ctx, store)
	profileID := testUUID(tenantID + "-msg-inbox-profile")
	if _, err := store.pool.Exec(ctx, `INSERT INTO profiles(id,tenant_id,workspace_id,app_id,external_id,attributes) VALUES($1,$2,$3,$4,'inbox-user','{}')`, profileID, p.TenantID, p.WorkspaceID, p.AppID); err != nil {
		t.Fatal(err)
	}

	now := time.Now().UTC()
	msgContent := json.RawMessage(`{"title":"Test"}`)
	msgContent2 := json.RawMessage(`{"title":"Test 2"}`)
	msgContent3 := json.RawMessage(`{"title":"Test 3"}`)

	// Create: a dismissed message (should not appear)
	dismissedKey := "dismissed-msg"
	dismissed, err := store.CreateInAppMessage(ctx, p.TenantID, p.WorkspaceID, p.AppID, profileID, domain.InAppMessage{
		MessageType:    "modal",
		Content:        msgContent,
		Rank:           0,
		Categories:     nil,
		StartAt:        now.Add(-1 * time.Hour),
		IdempotencyKey: &dismissedKey,
		Status:         "dismissed",
		DismissedAt:    &now,
	})
	if err != nil {
		t.Fatal(err)
	}

	// Create: an expired message (should not appear)
	expiredKey := "expired-msg"
	expired := now.Add(-1 * time.Hour)
	_, err = store.CreateInAppMessage(ctx, p.TenantID, p.WorkspaceID, p.AppID, profileID, domain.InAppMessage{
		MessageType:    "card",
		Content:        msgContent2,
		Rank:           0,
		Categories:     nil,
		StartAt:        now.Add(-2 * time.Hour),
		ExpiresAt:      &expired,
		IdempotencyKey: &expiredKey,
		Status:         "pending",
	})
	if err != nil {
		t.Fatal(err)
	}

	// Create: a valid message (should appear with rank 10)
	validKey := "valid-msg"
	valid, err := store.CreateInAppMessage(ctx, p.TenantID, p.WorkspaceID, p.AppID, profileID, domain.InAppMessage{
		MessageType:    "modal",
		Content:        msgContent3,
		Rank:           10,
		Categories:     []string{"marketing"},
		StartAt:        now,
		ExpiresAt:      nil,
		IdempotencyKey: &validKey,
		Status:         "pending",
	})
	if err != nil {
		t.Fatal(err)
	}

	// Fetch inbox
	inbox, err := store.ListInboxForProfile(ctx, p.TenantID, p.AppID, profileID, 100)
	if err != nil {
		t.Fatalf("ListInboxForProfile failed: %v", err)
	}

	if len(inbox) != 1 {
		t.Fatalf("expected 1 message in inbox, got %d", len(inbox))
	}
	if inbox[0].ID != valid.ID {
		t.Fatalf("expected valid message ID %s, got %s", valid.ID, inbox[0].ID)
	}
	if inbox[0].Rank != 10 {
		t.Fatalf("expected rank 10, got %d", inbox[0].Rank)
	}

	// Verify dismissed message does not appear
	if dismissed.ID == inbox[0].ID {
		t.Fatal("dismissed message appeared in inbox")
	}
}

func TestInAppMessageIdempotency(t *testing.T) {
	databaseURL := os.Getenv("OPENJOURNEY_TEST_DATABASE_URL")
	if databaseURL == "" {
		t.Skip("OPENJOURNEY_TEST_DATABASE_URL is not configured")
	}
	ctx := context.Background()
	store, err := Open(ctx, databaseURL)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	if err := store.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	p, tenantID := setupTestTenant(t, ctx, store)
	profileID := testUUID(tenantID + "-msg-idempotency")
	if _, err := store.pool.Exec(ctx, `INSERT INTO profiles(id,tenant_id,workspace_id,app_id,external_id,attributes) VALUES($1,$2,$3,$4,'idempotency-user','{}')`, profileID, p.TenantID, p.WorkspaceID, p.AppID); err != nil {
		t.Fatal(err)
	}

	now := time.Now().UTC()
	content := json.RawMessage(`{"title":"Test"}`)
	idempotencyKey := "idem-key-test"

	msg1, err := store.CreateInAppMessage(ctx, p.TenantID, p.WorkspaceID, p.AppID, profileID, domain.InAppMessage{
		MessageType:    "modal",
		Content:        content,
		Rank:           5,
		Categories:     nil,
		StartAt:        now,
		IdempotencyKey: &idempotencyKey,
		Status:         "pending",
	})
	if err != nil {
		t.Fatalf("first create failed: %v", err)
	}

	msg2, err := store.CreateInAppMessage(ctx, p.TenantID, p.WorkspaceID, p.AppID, profileID, domain.InAppMessage{
		MessageType:    "modal",
		Content:        content,
		Rank:           5,
		Categories:     nil,
		StartAt:        now,
		IdempotencyKey: &idempotencyKey,
		Status:         "pending",
	})
	if err != nil {
		t.Fatalf("second create (duplicate) failed: %v", err)
	}

	if msg1.ID != msg2.ID {
		t.Fatalf("idempotent insert returned different IDs: %s vs %s", msg1.ID, msg2.ID)
	}
}

func TestCreateInAppMessageMismatchedTenant(t *testing.T) {
	databaseURL := os.Getenv("OPENJOURNEY_TEST_DATABASE_URL")
	if databaseURL == "" {
		t.Skip("OPENJOURNEY_TEST_DATABASE_URL is not configured")
	}
	ctx := context.Background()
	store, err := Open(ctx, databaseURL)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	if err := store.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	p, tenantID := setupTestTenant(t, ctx, store)
	otherP, _ := setupTestTenant(t, ctx, store)

	profileID := testUUID(tenantID + "-msg-mismatch-profile")
	if _, err := store.pool.Exec(ctx, `INSERT INTO profiles(id,tenant_id,workspace_id,app_id,external_id,attributes) VALUES($1,$2,$3,$4,'mismatch-user','{}')`, profileID, p.TenantID, p.WorkspaceID, p.AppID); err != nil {
		t.Fatal(err)
	}

	now := time.Now().UTC()
	content := json.RawMessage(`{"title":"Test"}`)

	_, err = store.CreateInAppMessage(ctx, otherP.TenantID, p.WorkspaceID, p.AppID, profileID, domain.InAppMessage{
		MessageType:    "modal",
		Content:        content,
		Rank:           5,
		Categories:     nil,
		StartAt:        now,
		Status:         "pending",
	})

	if err == nil {
		t.Fatal("expected error for mismatched tenant, got nil")
	}
}

func TestListInAppMessages(t *testing.T) {
	databaseURL := os.Getenv("OPENJOURNEY_TEST_DATABASE_URL")
	if databaseURL == "" {
		t.Skip("OPENJOURNEY_TEST_DATABASE_URL is not configured")
	}
	ctx := context.Background()
	store, err := Open(ctx, databaseURL)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	if err := store.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	p, tenantID := setupTestTenant(t, ctx, store)
	profile1ID := testUUID(tenantID + "-msg-list-profile1")
	profile2ID := testUUID(tenantID + "-msg-list-profile2")

	if _, err := store.pool.Exec(ctx, `INSERT INTO profiles(id,tenant_id,workspace_id,app_id,external_id,attributes) VALUES($1,$2,$3,$4,'list-user-1','{}')`, profile1ID, p.TenantID, p.WorkspaceID, p.AppID); err != nil {
		t.Fatal(err)
	}
	if _, err := store.pool.Exec(ctx, `INSERT INTO profiles(id,tenant_id,workspace_id,app_id,external_id,attributes) VALUES($1,$2,$3,$4,'list-user-2','{}')`, profile2ID, p.TenantID, p.WorkspaceID, p.AppID); err != nil {
		t.Fatal(err)
	}

	now := time.Now().UTC()
	content := json.RawMessage(`{"title":"Test"}`)

	key1 := "list-msg-1"
	key2 := "list-msg-2"

	_, err = store.CreateInAppMessage(ctx, p.TenantID, p.WorkspaceID, p.AppID, profile1ID, domain.InAppMessage{
		MessageType:    "modal",
		Content:        content,
		Rank:           5,
		Categories:     nil,
		StartAt:        now,
		IdempotencyKey: &key1,
		Status:         "pending",
	})
	if err != nil {
		t.Fatal(err)
	}

	_, err = store.CreateInAppMessage(ctx, p.TenantID, p.WorkspaceID, p.AppID, profile2ID, domain.InAppMessage{
		MessageType:    "card",
		Content:        content,
		Rank:           10,
		Categories:     nil,
		StartAt:        now,
		IdempotencyKey: &key2,
		Status:         "pending",
	})
	if err != nil {
		t.Fatal(err)
	}

	messages, err := store.ListInAppMessages(ctx, p, p.AppID)
	if err != nil {
		t.Fatalf("ListInAppMessages failed: %v", err)
	}

	if len(messages) != 2 {
		t.Fatalf("expected 2 messages, got %d", len(messages))
	}
}

func TestGetInAppMessageNotFound(t *testing.T) {
	databaseURL := os.Getenv("OPENJOURNEY_TEST_DATABASE_URL")
	if databaseURL == "" {
		t.Skip("OPENJOURNEY_TEST_DATABASE_URL is not configured")
	}
	ctx := context.Background()
	store, err := Open(ctx, databaseURL)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	if err := store.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	p, _ := setupTestTenant(t, ctx, store)

	_, err = store.GetInAppMessage(ctx, p.TenantID, "00000000-0000-0000-0000-000000000000")
	if err != ports.ErrNotFound {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}
}

func TestCardFetchOrderingByRank(t *testing.T) {
	databaseURL := os.Getenv("OPENJOURNEY_TEST_DATABASE_URL")
	if databaseURL == "" {
		t.Skip("OPENJOURNEY_TEST_DATABASE_URL is not configured")
	}
	ctx := context.Background()
	store, err := Open(ctx, databaseURL)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	if err := store.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	p, tenantID := setupTestTenant(t, ctx, store)
	profileID := testUUID(tenantID + "-msg-rank-profile")
	if _, err := store.pool.Exec(ctx, `INSERT INTO profiles(id,tenant_id,workspace_id,app_id,external_id,attributes) VALUES($1,$2,$3,$4,'rank-user','{}')`, profileID, p.TenantID, p.WorkspaceID, p.AppID); err != nil {
		t.Fatal(err)
	}

	now := time.Now().UTC()
	msgContent := json.RawMessage(`{"title":"Card"}`)

	// Create cards with different ranks: 5, 20, 10
	key1 := "rank-msg-5"
	_, err = store.CreateInAppMessage(ctx, p.TenantID, p.WorkspaceID, p.AppID, profileID, domain.InAppMessage{
		MessageType:    "card",
		Content:        msgContent,
		Rank:           5,
		Categories:     nil,
		StartAt:        now,
		IdempotencyKey: &key1,
		Status:         "pending",
	})
	if err != nil {
		t.Fatal(err)
	}

	key2 := "rank-msg-20"
	msg2, err := store.CreateInAppMessage(ctx, p.TenantID, p.WorkspaceID, p.AppID, profileID, domain.InAppMessage{
		MessageType:    "card",
		Content:        msgContent,
		Rank:           20,
		Categories:     nil,
		StartAt:        now,
		IdempotencyKey: &key2,
		Status:         "pending",
	})
	if err != nil {
		t.Fatal(err)
	}

	key3 := "rank-msg-10"
	msg3, err := store.CreateInAppMessage(ctx, p.TenantID, p.WorkspaceID, p.AppID, profileID, domain.InAppMessage{
		MessageType:    "card",
		Content:        msgContent,
		Rank:           10,
		Categories:     nil,
		StartAt:        now,
		IdempotencyKey: &key3,
		Status:         "pending",
	})
	if err != nil {
		t.Fatal(err)
	}

	// Fetch inbox - should return in rank DESC order: 20, 10, 5
	inbox, err := store.ListInboxForProfile(ctx, p.TenantID, p.AppID, profileID, 100)
	if err != nil {
		t.Fatalf("ListInboxForProfile failed: %v", err)
	}

	if len(inbox) != 3 {
		t.Fatalf("expected 3 messages in inbox, got %d", len(inbox))
	}

	// Verify ordering: rank 20, then 10, then 5
	if inbox[0].ID != msg2.ID || inbox[0].Rank != 20 {
		t.Fatalf("first message should be rank 20, got rank %d (ID %s)", inbox[0].Rank, inbox[0].ID)
	}
	if inbox[1].ID != msg3.ID || inbox[1].Rank != 10 {
		t.Fatalf("second message should be rank 10, got rank %d (ID %s)", inbox[1].Rank, inbox[1].ID)
	}
	// msg1 has rank 5, checked implicitly via message count
}

func TestCardFetchExcludesOutOfWindow(t *testing.T) {
	databaseURL := os.Getenv("OPENJOURNEY_TEST_DATABASE_URL")
	if databaseURL == "" {
		t.Skip("OPENJOURNEY_TEST_DATABASE_URL is not configured")
	}
	ctx := context.Background()
	store, err := Open(ctx, databaseURL)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	if err := store.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	p, tenantID := setupTestTenant(t, ctx, store)
	profileID := testUUID(tenantID + "-msg-window-profile")
	if _, err := store.pool.Exec(ctx, `INSERT INTO profiles(id,tenant_id,workspace_id,app_id,external_id,attributes) VALUES($1,$2,$3,$4,'window-user','{}')`, profileID, p.TenantID, p.WorkspaceID, p.AppID); err != nil {
		t.Fatal(err)
	}

	now := time.Now().UTC()
	msgContent := json.RawMessage(`{"title":"Card"}`)

	// Create a message that starts in the future (out-of-window)
	futureStart := now.Add(1 * time.Hour)
	futureKey := "future-msg"
	_, err = store.CreateInAppMessage(ctx, p.TenantID, p.WorkspaceID, p.AppID, profileID, domain.InAppMessage{
		MessageType:    "card",
		Content:        msgContent,
		Rank:           10,
		Categories:     nil,
		StartAt:        futureStart,
		IdempotencyKey: &futureKey,
		Status:         "pending",
	})
	if err != nil {
		t.Fatal(err)
	}

	// Create a valid (in-window) message
	validKey := "valid-window-msg"
	_, err = store.CreateInAppMessage(ctx, p.TenantID, p.WorkspaceID, p.AppID, profileID, domain.InAppMessage{
		MessageType:    "card",
		Content:        msgContent,
		Rank:           5,
		Categories:     nil,
		StartAt:        now,
		IdempotencyKey: &validKey,
		Status:         "pending",
	})
	if err != nil {
		t.Fatal(err)
	}

	// Fetch inbox - should only return the valid message
	inbox, err := store.ListInboxForProfile(ctx, p.TenantID, p.AppID, profileID, 100)
	if err != nil {
		t.Fatalf("ListInboxForProfile failed: %v", err)
	}

	if len(inbox) != 1 {
		t.Fatalf("expected 1 message in inbox, got %d", len(inbox))
	}
}

func TestRepeatedImpressionsUpdateTimestamp(t *testing.T) {
	databaseURL := os.Getenv("OPENJOURNEY_TEST_DATABASE_URL")
	if databaseURL == "" {
		t.Skip("OPENJOURNEY_TEST_DATABASE_URL is not configured")
	}
	ctx := context.Background()
	store, err := Open(ctx, databaseURL)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	if err := store.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	p, tenantID := setupTestTenant(t, ctx, store)
	profileID := testUUID(tenantID + "-msg-repeat-profile")
	if _, err := store.pool.Exec(ctx, `INSERT INTO profiles(id,tenant_id,workspace_id,app_id,external_id,attributes) VALUES($1,$2,$3,$4,'repeat-user','{}')`, profileID, p.TenantID, p.WorkspaceID, p.AppID); err != nil {
		t.Fatal(err)
	}

	now := time.Now().UTC()
	msgContent := json.RawMessage(`{"title":"Card"}`)
	key := "repeat-impression-msg"
	msg, err := store.CreateInAppMessage(ctx, p.TenantID, p.WorkspaceID, p.AppID, profileID, domain.InAppMessage{
		MessageType:    "card",
		Content:        msgContent,
		Rank:           5,
		StartAt:        now,
		IdempotencyKey: &key,
		Status:         "delivered",
	})
	if err != nil {
		t.Fatal(err)
	}

	// First impression
	payload1, _ := json.Marshal(map[string]string{"message_id": msg.ID})
	_, err = store.AcceptEvents(ctx, domain.Principal{TenantID: p.TenantID, ActorType: "public"}, []domain.Event{
		{Type: "message.impression", Payload: payload1},
	})
	if err != nil {
		t.Fatal(err)
	}

	// Verify first impression set displayed_at
	updated1, err := store.GetInAppMessage(ctx, p.TenantID, msg.ID)
	if err != nil {
		t.Fatal(err)
	}
	if updated1.DisplayedAt == nil {
		t.Fatal("expected displayed_at to be set after first impression")
	}
	if updated1.Status != "displayed" {
		t.Fatalf("expected status 'displayed', got '%s'", updated1.Status)
	}
	firstTimestamp := *updated1.DisplayedAt

	// Wait and send second impression
	time.Sleep(100 * time.Millisecond)
	payload2, _ := json.Marshal(map[string]string{"message_id": msg.ID})
	_, err = store.AcceptEvents(ctx, domain.Principal{TenantID: p.TenantID, ActorType: "public"}, []domain.Event{
		{Type: "message.impression", Payload: payload2},
	})
	if err != nil {
		t.Fatal(err)
	}

	// Verify second impression updated displayed_at
	updated2, err := store.GetInAppMessage(ctx, p.TenantID, msg.ID)
	if err != nil {
		t.Fatal(err)
	}
	if updated2.DisplayedAt == nil {
		t.Fatal("expected displayed_at to be set after second impression")
	}
	if updated2.Status != "displayed" {
		t.Fatalf("expected status 'displayed', got '%s'", updated2.Status)
	}
	secondTimestamp := *updated2.DisplayedAt

	if firstTimestamp.After(secondTimestamp) {
		t.Fatalf("expected second timestamp to be after first, got first=%v second=%v", firstTimestamp, secondTimestamp)
	}

	// Dismiss and verify no more impressions are recorded
	dismissPayload, _ := json.Marshal(map[string]string{"message_id": msg.ID})
	_, err = store.AcceptEvents(ctx, domain.Principal{TenantID: p.TenantID, ActorType: "public"}, []domain.Event{
		{Type: "message.dismissed", Payload: dismissPayload},
	})
	if err != nil {
		t.Fatal(err)
	}

	dismissed, err := store.GetInAppMessage(ctx, p.TenantID, msg.ID)
	if err != nil {
		t.Fatal(err)
	}
	if dismissed.Status != "dismissed" {
		t.Fatalf("expected status 'dismissed', got '%s'", dismissed.Status)
	}
	dismissedTimestamp := *dismissed.DisplayedAt

	// Try another impression - should not update displayed_at
	time.Sleep(100 * time.Millisecond)
	payload3, _ := json.Marshal(map[string]string{"message_id": msg.ID})
	_, err = store.AcceptEvents(ctx, domain.Principal{TenantID: p.TenantID, ActorType: "public"}, []domain.Event{
		{Type: "message.impression", Payload: payload3},
	})
	if err != nil {
		t.Fatal(err)
	}

	afterDismissal, err := store.GetInAppMessage(ctx, p.TenantID, msg.ID)
	if err != nil {
		t.Fatal(err)
	}
	if afterDismissal.Status != "dismissed" {
		t.Fatalf("expected status to remain 'dismissed', got '%s'", afterDismissal.Status)
	}
	if !dismissedTimestamp.Equal(*afterDismissal.DisplayedAt) {
		t.Fatal("expected displayed_at to not change after dismissal")
	}
}

func TestExpireInAppMessages(t *testing.T) {
	databaseURL := os.Getenv("OPENJOURNEY_TEST_DATABASE_URL")
	if databaseURL == "" {
		t.Skip("OPENJOURNEY_TEST_DATABASE_URL is not configured")
	}
	ctx := context.Background()
	store, err := Open(ctx, databaseURL)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	if err := store.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	p, tenantID := setupTestTenant(t, ctx, store)
	profileID := testUUID(tenantID + "-msg-expire-profile")
	if _, err := store.pool.Exec(ctx, `INSERT INTO profiles(id,tenant_id,workspace_id,app_id,external_id,attributes) VALUES($1,$2,$3,$4,'expire-user','{}')`, profileID, p.TenantID, p.WorkspaceID, p.AppID); err != nil {
		t.Fatal(err)
	}

	now := time.Now().UTC()
	msgContent := json.RawMessage(`{"title":"Card"}`)

	// Create an expired card (expires_at in the past)
	expiredKey := "expired-msg"
	expiredCard, err := store.CreateInAppMessage(ctx, p.TenantID, p.WorkspaceID, p.AppID, profileID, domain.InAppMessage{
		MessageType:    "card",
		Content:        msgContent,
		Rank:           5,
		StartAt:        now.Add(-2 * time.Hour),
		ExpiresAt:      &now,
		IdempotencyKey: &expiredKey,
		Status:         "delivered",
	})
	if err != nil {
		t.Fatal(err)
	}

	// Create a valid (not expired) card
	validKey := "valid-msg"
	futureExpire := now.Add(1 * time.Hour)
	_, err = store.CreateInAppMessage(ctx, p.TenantID, p.WorkspaceID, p.AppID, profileID, domain.InAppMessage{
		MessageType:    "card",
		Content:        msgContent,
		Rank:           10,
		StartAt:        now,
		ExpiresAt:      &futureExpire,
		IdempotencyKey: &validKey,
		Status:         "delivered",
	})
	if err != nil {
		t.Fatal(err)
	}

	// Run the expiry sweep
	if err := store.ExpireInAppMessages(ctx, p.TenantID, 100); err != nil {
		t.Fatal(err)
	}

	// Verify expired card is marked as expired
	expiredAfter, err := store.GetInAppMessage(ctx, p.TenantID, expiredCard.ID)
	if err != nil {
		t.Fatal(err)
	}
	if expiredAfter.Status != "expired" {
		t.Fatalf("expected status 'expired', got '%s'", expiredAfter.Status)
	}

	// Verify it's excluded from inbox fetch
	inbox, err := store.ListInboxForProfile(ctx, p.TenantID, p.AppID, profileID, 100)
	if err != nil {
		t.Fatal(err)
	}
	if len(inbox) != 1 {
		t.Fatalf("expected 1 message in inbox after expiry, got %d", len(inbox))
	}
}
