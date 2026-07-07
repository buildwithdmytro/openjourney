package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/buildwithdmytro/openjourney/internal/domain"
	"github.com/buildwithdmytro/openjourney/internal/operations"
	"github.com/buildwithdmytro/openjourney/internal/projector"
)

type memoryBlobs struct {
	mu      sync.Mutex
	objects map[string][]byte
}

func (m *memoryBlobs) Put(_ context.Context, key string, data []byte, _ string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.objects[key] = append([]byte(nil), data...)
	return nil
}
func (m *memoryBlobs) Get(_ context.Context, key string) ([]byte, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	value, exists := m.objects[key]
	if !exists {
		return nil, errors.New("not found")
	}
	return append([]byte(nil), value...), nil
}
func (m *memoryBlobs) Delete(_ context.Context, key string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.objects, key)
	return nil
}

func TestPlatformKernelIntegration(t *testing.T) {
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
	if _, err := store.pool.Exec(ctx, "TRUNCATE tenants CASCADE"); err != nil {
		t.Fatal(err)
	}

	keyA, keyB := "integration-a", "integration-b"
	if err := store.EnsureDevelopmentTenant(ctx, keyA); err != nil {
		t.Fatal(err)
	}
	if err := store.EnsureDevelopmentTenant(ctx, keyB); err != nil {
		t.Fatal(err)
	}
	tenantA, err := store.Authenticate(ctx, keyA)
	if err != nil {
		t.Fatal(err)
	}
	tenantB, err := store.Authenticate(ctx, keyB)
	if err != nil {
		t.Fatal(err)
	}
	if tenantA.TenantID == tenantB.TenantID {
		t.Fatal("development tenants were not isolated")
	}
	if err := store.EnsureLocalAdmin(ctx, "Admin@Example.Test", "correct horse battery staple"); err != nil {
		t.Fatalf("ensure local admin: %v", err)
	}
	localSession, err := store.CreateLocalSession(ctx, "admin@example.test", "correct horse battery staple", time.Hour)
	if err != nil || localSession.AccessToken == "" {
		t.Fatalf("create local session=%+v err=%v", localSession, err)
	}
	localPrincipal, err := store.Authenticate(ctx, localSession.AccessToken)
	if err != nil || localPrincipal.ActorType != "user" || !localPrincipal.HasScope("*") {
		t.Fatalf("local session principal=%+v err=%v", localPrincipal, err)
	}
	if err := store.RevokeLocalSession(ctx, localSession.AccessToken); err != nil {
		t.Fatalf("revoke local session: %v", err)
	}
	if _, err := store.Authenticate(ctx, localSession.AccessToken); !errors.Is(err, ErrUnauthorized) {
		t.Fatalf("revoked local session err=%v", err)
	}
	role, err := store.CreateRole(ctx, tenantA, "Profile reader", []string{"profiles:read"})
	if err != nil {
		t.Fatal(err)
	}
	ingestRole, err := store.CreateRole(ctx, tenantA, "Event writer", []string{"events:write"})
	if err != nil {
		t.Fatal(err)
	}
	user, err := store.CreateUser(ctx, tenantA, domain.User{
		OIDCIssuer: "https://identity.example.test", OIDCSubject: "subject-1",
		Email: "operator@example.test", RoleIDs: []string{role.ID},
	})
	if err != nil || user.ID == "" {
		t.Fatalf("create OIDC user: %+v %v", user, err)
	}
	oidcPrincipal, err := store.AuthenticateOIDC(ctx, domain.OIDCClaims{
		Issuer: "https://identity.example.test", Subject: "subject-1",
		TenantID: tenantA.TenantID, WorkspaceID: tenantA.WorkspaceID, AppID: tenantA.AppID,
	})
	if err != nil || !oidcPrincipal.HasScope("profiles:read") || oidcPrincipal.HasScope("events:write") {
		t.Fatalf("OIDC principal=%+v err=%v", oidcPrincipal, err)
	}
	if _, err := store.AuthenticateOIDC(ctx, domain.OIDCClaims{
		Issuer: "https://identity.example.test", Subject: "subject-1",
		TenantID: tenantB.TenantID, WorkspaceID: tenantB.WorkspaceID, AppID: tenantB.AppID,
	}); !errors.Is(err, ErrUnauthorized) {
		t.Fatalf("cross-tenant OIDC authentication err=%v", err)
	}
	ingestUser, err := store.CreateUser(ctx, tenantA, domain.User{
		OIDCIssuer: "https://identity.example.test", OIDCSubject: "subject-writer",
		Email: "writer@example.test", RoleIDs: []string{ingestRole.ID},
	})
	if err != nil || ingestUser.ID == "" {
		t.Fatalf("create OIDC ingestion user: %+v %v", ingestUser, err)
	}
	oidcWriter, err := store.AuthenticateOIDC(ctx, domain.OIDCClaims{
		Issuer: "https://identity.example.test", Subject: "subject-writer",
		TenantID: tenantA.TenantID, WorkspaceID: tenantA.WorkspaceID, AppID: tenantA.AppID,
	})
	if err != nil || !oidcWriter.HasScope("events:write") || oidcWriter.ActorType != "user" || oidcWriter.UserID == "" {
		t.Fatalf("OIDC writer principal=%+v err=%v", oidcWriter, err)
	}
	scopedKey, rawKey, err := store.CreateAPIKey(ctx, tenantA, "Profile reader key", []string{"profiles:read"}, nil)
	if err != nil || scopedKey.ID == "" || rawKey == "" {
		t.Fatalf("create API key: key=%+v raw=%q err=%v", scopedKey, rawKey, err)
	}
	if _, err := store.Authenticate(ctx, rawKey); err != nil {
		t.Fatalf("authenticate scoped API key: %v", err)
	}
	listedKeys, err := store.ListAPIKeys(ctx, tenantA)
	if err != nil {
		t.Fatalf("list API keys: %v", err)
	}
	var scopedKeyLastUsed bool
	for _, key := range listedKeys {
		if key.ID == scopedKey.ID && key.LastUsedAt != nil {
			scopedKeyLastUsed = true
		}
	}
	if !scopedKeyLastUsed {
		t.Fatalf("used API key did not expose last_used_at: %+v", listedKeys)
	}
	if err := store.RevokeAPIKey(ctx, tenantA, scopedKey.ID); err != nil {
		t.Fatalf("revoke API key: %v", err)
	}

	schema, err := store.CreateEventSchema(ctx, tenantA, domain.EventSchema{
		EventType: "product.viewed", Version: 1, Compatibility: "backward",
		Schema: json.RawMessage(`{"type":"object","required":["sku"],"properties":{"sku":{"type":"string"}}}`),
	})
	if err != nil || schema.ID == "" {
		t.Fatalf("create schema: %v", err)
	}
	custom := event("product.viewed", "customer-a", "custom-1", `{"sku":"123"}`)
	if err := store.ValidateEventSchema(ctx, tenantA, custom); err != nil {
		t.Fatalf("valid custom event rejected: %v", err)
	}
	custom.Payload = json.RawMessage(`{"sku":123}`)
	if err := store.ValidateEventSchema(ctx, tenantA, custom); err == nil {
		t.Fatal("invalid custom event accepted")
	}

	events := []domain.Event{
		event("profile.updated", "", "anonymous-1", `{"attributes":{"first_name":"Ada"}}`),
		{
			Type: "profile.updated", SchemaVersion: 1, ExternalID: "customer-a", AnonymousID: "browser-a",
			IdempotencyKey: "identify-a", OccurredAt: time.Now().UTC(),
			Payload: json.RawMessage(`{"attributes":{"plan":"pro"}}`),
		},
		event("consent.changed", "customer-a", "consent-a",
			`{"channel":"email","topic":"marketing","state":"subscribed","evidence":{"form":"signup"}}`),
	}
	events[0].AnonymousID = "browser-a"
	ids, err := store.AcceptEvents(ctx, tenantA, events)
	if err != nil {
		t.Fatal(err)
	}
	repeated, err := store.AcceptEvents(ctx, tenantA, events[:1])
	if err != nil {
		t.Fatal(err)
	}
	if ids[0] != repeated[0] {
		t.Fatalf("idempotency mismatch: %s != %s", ids[0], repeated[0])
	}
	conflicting := events[0]
	conflicting.Payload = json.RawMessage(`{"attributes":{"first_name":"Grace"}}`)
	if _, err := store.AcceptEvents(ctx, tenantA, []domain.Event{conflicting}); !errors.Is(err, ErrIdempotencyConflict) {
		t.Fatalf("idempotency conflict err=%v", err)
	}
	if count, err := projector.Drain(ctx, store, 10, false); err != nil || count < 3 {
		t.Fatalf("projection drain count=%d err=%v", count, err)
	}
	oidcIngestEvent := event("profile.updated", "oidc-writer-customer", "oidc-writer-profile", `{"attributes":{"source":"oidc"}}`)
	if _, err := store.AcceptEvents(ctx, oidcWriter, []domain.Event{oidcIngestEvent}); err != nil {
		t.Fatalf("OIDC writer accept events: %v", err)
	}
	if count, err := projector.Drain(ctx, store, 1, false); err != nil || count != 1 {
		t.Fatalf("OIDC writer projection drain count=%d err=%v", count, err)
	}
	profile, consents, err := store.GetProfile(ctx, tenantA, "customer-a")
	if err != nil {
		t.Fatal(err)
	}
	var attributes map[string]any
	_ = json.Unmarshal(profile.Attributes, &attributes)
	if attributes["first_name"] != "Ada" || attributes["plan"] != "pro" {
		t.Fatalf("identity projection lost attributes: %+v", attributes)
	}
	if len(consents) != 1 || consents[0].State != "subscribed" {
		t.Fatalf("consent projection=%+v", consents)
	}
	if _, _, err := store.GetProfile(ctx, tenantB, "customer-a"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("cross-tenant profile read err=%v", err)
	}
	dlqEvent := event("profile.updated", "dlq-user", "dlq-projection", `{"attributes":{"broken":true}}`)
	dlqIDs, err := store.AcceptEvents(ctx, tenantA, []domain.Event{dlqEvent})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.pool.Exec(ctx, `UPDATE projection_jobs
		SET status='dead',attempts=10,last_error='projection failed'
		WHERE event_id=$1`, dlqIDs[0]); err != nil {
		t.Fatal(err)
	}
	tenantBEvent := event("profile.updated", "tenant-b-dlq", "tenant-b-dlq", `{"attributes":{"hidden":true}}`)
	tenantBDLQIDs, err := store.AcceptEvents(ctx, tenantB, []domain.Event{tenantBEvent})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.pool.Exec(ctx, `UPDATE projection_jobs
		SET status='dead',attempts=10,last_error='hidden'
		WHERE event_id=$1`, tenantBDLQIDs[0]); err != nil {
		t.Fatal(err)
	}
	deadLetters, err := store.ListDeadLetters(ctx, tenantA, "projection", 100)
	if err != nil {
		t.Fatal(err)
	}
	if len(deadLetters) != 1 || deadLetters[0].ID != dlqIDs[0] || deadLetters[0].SubjectID != "dlq-user" {
		t.Fatalf("tenant-scoped projection DLQ=%+v", deadLetters)
	}
	if err := store.RetryDeadLetter(ctx, tenantA, "projection", dlqIDs[0]); err != nil {
		t.Fatal(err)
	}
	var retriedStatus string
	var retriedAttempts int
	if err := store.pool.QueryRow(ctx, `SELECT status,attempts FROM projection_jobs WHERE event_id=$1`, dlqIDs[0]).
		Scan(&retriedStatus, &retriedAttempts); err != nil {
		t.Fatal(err)
	}
	if retriedStatus != "pending" || retriedAttempts != 0 {
		t.Fatalf("retried projection job status=%s attempts=%d", retriedStatus, retriedAttempts)
	}
	if count, err := projector.Drain(ctx, store, 1, false); err != nil || count != 1 {
		t.Fatalf("retried projection drain count=%d err=%v", count, err)
	}
	var outboxID string
	if err := store.pool.QueryRow(ctx, `SELECT id FROM outbox_events WHERE tenant_id=$1 LIMIT 1`, tenantA.TenantID).
		Scan(&outboxID); err != nil {
		t.Fatal(err)
	}
	if _, err := store.pool.Exec(ctx, `UPDATE outbox_events
		SET status='dead',attempts=10,last_error='publish failed' WHERE id=$1`, outboxID); err != nil {
		t.Fatal(err)
	}
	if err := store.DiscardDeadLetter(ctx, tenantA, "outbox", outboxID); err != nil {
		t.Fatal(err)
	}
	var outboxStatus string
	if err := store.pool.QueryRow(ctx, `SELECT status FROM outbox_events WHERE id=$1`, outboxID).Scan(&outboxStatus); err != nil {
		t.Fatal(err)
	}
	if outboxStatus != "published" {
		t.Fatalf("discarded outbox status=%s", outboxStatus)
	}
	if err := store.DiscardDeadLetter(ctx, tenantA, "projection", tenantBDLQIDs[0]); !errors.Is(err, ErrNotFound) {
		t.Fatalf("cross-tenant DLQ discard err=%v", err)
	}

	report, err := store.VerifyReplay(ctx, tenantA)
	if err != nil || !report.Match {
		t.Fatalf("replay report=%+v err=%v", report, err)
	}

	if _, err := store.pool.Exec(ctx, `UPDATE tenant_quotas SET retention_days=1 WHERE tenant_id=$1`, tenantA.TenantID); err != nil {
		t.Fatal(err)
	}
	retainedProfileID := profile.ID
	if _, err := store.pool.Exec(ctx, `UPDATE accepted_events SET received_at=now()-interval '48 hours'
		WHERE id=$1`, ids[2]); err != nil {
		t.Fatal(err)
	}
	retentionPayload, _ := json.Marshal(map[string]string{"tenant_id": tenantA.TenantID})
	if _, err := store.pool.Exec(ctx, `INSERT INTO operation_jobs(tenant_id,workspace_id,job_type,payload)
		VALUES($1,$2,'retention.enforce',$3)`, tenantA.TenantID, tenantA.WorkspaceID, retentionPayload); err != nil {
		t.Fatal(err)
	}
	blobs := &memoryBlobs{objects: map[string][]byte{}}
	if count, err := operations.Drain(ctx, store, blobs, 1, false); err != nil || count != 1 {
		t.Fatalf("retention drain count=%d err=%v", count, err)
	}
	var retainedEventExists bool
	if err := store.pool.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM accepted_events WHERE id=$1)`, ids[2]).
		Scan(&retainedEventExists); err != nil {
		t.Fatal(err)
	}
	if retainedEventExists {
		t.Fatal("retention did not delete expired accepted event")
	}
	var retainedConsents, nullSourceConsents int
	if err := store.pool.QueryRow(ctx, `SELECT count(*),count(*) FILTER (WHERE source_event_id IS NULL)
		FROM consent_ledger WHERE tenant_id=$1 AND profile_id=$2`, tenantA.TenantID, retainedProfileID).
		Scan(&retainedConsents, &nullSourceConsents); err != nil {
		t.Fatal(err)
	}
	if retainedConsents != 1 || nullSourceConsents != 1 {
		t.Fatalf("retention should preserve consent projection and null event reference: consents=%d null_sources=%d",
			retainedConsents, nullSourceConsents)
	}

	// A processing lease is safely reclaimed after expiry.
	leaseEvent := event("profile.updated", "lease-user", "lease-event", `{"attributes":{"ok":true}}`)
	leaseIDs, err := store.AcceptEvents(ctx, tenantA, []domain.Event{leaseEvent})
	if err != nil {
		t.Fatal(err)
	}
	if _, found, err := store.ClaimProjectionJob(ctx); err != nil || !found {
		t.Fatalf("claim lease found=%v err=%v", found, err)
	}
	if _, err := store.pool.Exec(ctx, `UPDATE projection_jobs SET locked_until=now()-interval '1 second'
		WHERE event_id=$1`, leaseIDs[0]); err != nil {
		t.Fatal(err)
	}
	reclaimed, found, err := store.ClaimProjectionJob(ctx)
	if err != nil || !found || reclaimed.ID != leaseIDs[0] {
		t.Fatalf("reclaim=%+v found=%v err=%v", reclaimed, found, err)
	}
	if err := store.ProjectEvent(ctx, reclaimed); err != nil {
		t.Fatal(err)
	}
	ordered := []domain.Event{
		event("profile.updated", "ordered-user", "ordered-1", `{"attributes":{"step":1}}`),
		event("profile.updated", "ordered-user", "ordered-2", `{"attributes":{"step":2}}`),
	}
	if _, err := store.AcceptEvents(ctx, tenantA, ordered); err != nil {
		t.Fatal(err)
	}
	first, found, err := store.ClaimProjectionJob(ctx)
	if err != nil || !found {
		t.Fatalf("first ordered claim found=%v err=%v", found, err)
	}
	if _, found, err := store.ClaimProjectionJob(ctx); err != nil || found {
		t.Fatalf("later subject event was concurrently claimable: found=%v err=%v", found, err)
	}
	if err := store.ProjectEvent(ctx, first); err != nil {
		t.Fatal(err)
	}
	second, found, err := store.ClaimProjectionJob(ctx)
	if err != nil || !found {
		t.Fatalf("second ordered claim found=%v err=%v", found, err)
	}
	if err := store.ProjectEvent(ctx, second); err != nil {
		t.Fatal(err)
	}
	orderedProfile, _, err := store.GetProfile(ctx, tenantA, "ordered-user")
	var orderedAttributes map[string]any
	_ = json.Unmarshal(orderedProfile.Attributes, &orderedAttributes)
	if err != nil || orderedAttributes["step"] != float64(2) {
		t.Fatalf("ordered profile=%s err=%v", orderedProfile.Attributes, err)
	}

	exportRequest, err := store.CreatePrivacyRequest(ctx, tenantA, "customer-a", "export")
	if err != nil {
		t.Fatal(err)
	}
	if count, err := operations.Drain(ctx, store, blobs, 1, false); err != nil || count != 1 {
		t.Fatalf("export drain count=%d err=%v", count, err)
	}
	exported, err := store.GetPrivacyRequest(ctx, tenantA, exportRequest.ID)
	if err != nil || exported.Status != "complete" || exported.ArtifactKey == "" {
		t.Fatalf("export request=%+v err=%v", exported, err)
	}
	if _, err := blobs.Get(ctx, exported.ArtifactKey); err != nil {
		t.Fatal("privacy export artifact missing")
	}

	deleteRequest, err := store.CreatePrivacyRequest(ctx, tenantA, "customer-a", "delete")
	if err != nil {
		t.Fatal(err)
	}
	if count, err := operations.Drain(ctx, store, blobs, 1, false); err != nil || count != 1 {
		t.Fatalf("delete drain count=%d err=%v", count, err)
	}
	deleted, err := store.GetPrivacyRequest(ctx, tenantA, deleteRequest.ID)
	if err != nil || deleted.Status != "complete" {
		t.Fatalf("delete request=%+v err=%v", deleted, err)
	}
	if _, _, err := store.GetProfile(ctx, tenantA, "customer-a"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("deleted profile remains err=%v", err)
	}
	var tombstones int
	if err := store.pool.QueryRow(ctx, `SELECT count(*) FROM accepted_events
		WHERE tenant_id=$1 AND event_type='privacy.deleted'`, tenantA.TenantID).Scan(&tombstones); err != nil {
		t.Fatal(err)
	}
	if tombstones != 1 {
		t.Fatalf("privacy tombstones=%d", tombstones)
	}

	auditEvents, err := store.ListAuditEvents(ctx, tenantA, 100)
	if err != nil {
		t.Fatal(err)
	}
	assertAuditActions(t, auditEvents, map[string]string{
		"schema.create":  "event_schema",
		"api_key.create": "api_key",
		"api_key.revoke": "api_key",
		"role.create":    "role",
		"user.create":    "user",
		"events.accept":  "event_batch",
		"privacy.export": "privacy_request",
		"privacy.delete": "privacy_request",
	})
	assertAuditActor(t, auditEvents, "events.accept", "user", oidcWriter.UserID)
}

func event(eventType, externalID, idempotency, payload string) domain.Event {
	return domain.Event{
		Type: eventType, SchemaVersion: 1, ExternalID: externalID,
		IdempotencyKey: idempotency, OccurredAt: time.Now().UTC(),
		Payload: json.RawMessage(payload),
	}
}

func assertAuditActions(t *testing.T, events []domain.AuditEvent, expected map[string]string) {
	t.Helper()
	seen := map[string]string{}
	for _, event := range events {
		if _, exists := expected[event.Action]; exists {
			seen[event.Action] = event.ResourceType
		}
	}
	for action, resourceType := range expected {
		if seen[action] != resourceType {
			t.Fatalf("missing audit action %s/%s in %+v", action, resourceType, events)
		}
	}
}

func assertAuditActor(t *testing.T, events []domain.AuditEvent, action, actorType, actorID string) {
	t.Helper()
	for _, event := range events {
		if event.Action == action && event.ActorType == actorType && event.ActorID == actorID {
			return
		}
	}
	t.Fatalf("missing audit actor %s/%s for action %s in %+v", actorType, actorID, action, events)
}

func TestMain(m *testing.M) {
	if os.Getenv("OPENJOURNEY_TEST_DATABASE_URL") != "" {
		fmt.Println("running PostgreSQL integration tests")
	}
	os.Exit(m.Run())
}
