package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/buildwithdmytro/openjourney/internal/domain"
	journeyflow "github.com/buildwithdmytro/openjourney/internal/journey"
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

func TestJourneyRoleScopesIntegration(t *testing.T) {
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

	key := fmt.Sprintf("journey-scope-%d", time.Now().UnixNano())
	if err := store.EnsureDevelopmentTenant(ctx, key); err != nil {
		t.Fatal(err)
	}
	principal, err := store.Authenticate(ctx, key)
	if err != nil {
		t.Fatal(err)
	}
	role, err := store.CreateRole(ctx, principal, "Journey operator", []string{
		"journeys:read",
		"journeys:write",
		"journeys:publish",
	})
	if err != nil {
		t.Fatalf("CreateRole journey scopes: %v", err)
	}
	if len(role.Permissions) != 3 {
		t.Fatalf("journey role permissions=%v", role.Permissions)
	}
}

func TestExperimentMigrationAndDefaultScopesIntegration(t *testing.T) {
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

	for _, table := range []string{"experiments", "experiment_variants", "experiment_assignments"} {
		var exists bool
		if err := store.pool.QueryRow(ctx, `SELECT to_regclass('public.' || $1) IS NOT NULL`, table).Scan(&exists); err != nil {
			t.Fatal(err)
		}
		if !exists {
			t.Fatalf("table %s does not exist", table)
		}
	}

	rawKey := fmt.Sprintf("experiment-default-scopes-%d", time.Now().UnixNano())
	if err := store.EnsureDevelopmentTenant(ctx, rawKey); err != nil {
		t.Fatal(err)
	}
	principal, err := store.Authenticate(ctx, rawKey)
	if err != nil {
		t.Fatal(err)
	}
	for _, scope := range []string{"experiments:read", "experiments:write", "reports:read"} {
		if !principal.HasScope(scope) {
			t.Fatalf("fresh API key scopes %v do not include %q", principal.Scopes, scope)
		}
	}
	if _, err := store.CreateRole(ctx, principal, "Experiment analyst", []string{
		"experiments:read", "experiments:write", "reports:read",
	}); err != nil {
		t.Fatalf("CreateRole experiment/report scopes: %v", err)
	}
}

func TestDeviceTokensMigrationAndDefaultScopesIntegration(t *testing.T) {
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

	var exists bool
	if err := store.pool.QueryRow(ctx, `SELECT to_regclass('public.device_tokens') IS NOT NULL`).Scan(&exists); err != nil {
		t.Fatal(err)
	}
	if !exists {
		t.Fatalf("table device_tokens does not exist")
	}

	rawKey := fmt.Sprintf("device-tokens-default-scopes-%d", time.Now().UnixNano())
	if err := store.EnsureDevelopmentTenant(ctx, rawKey); err != nil {
		t.Fatal(err)
	}
	principal, err := store.Authenticate(ctx, rawKey)
	if err != nil {
		t.Fatal(err)
	}
	for _, scope := range []string{"device_tokens:read", "device_tokens:write"} {
		if !principal.HasScope(scope) {
			t.Fatalf("fresh API key scopes %v do not include %q", principal.Scopes, scope)
		}
	}
	if _, err := store.CreateRole(ctx, principal, "Mobile dev", []string{
		"device_tokens:read", "device_tokens:write",
	}); err != nil {
		t.Fatalf("CreateRole device_tokens scopes: %v", err)
	}
}

func TestScoringMigrationAndDefaultScopesIntegration_12_2_1(t *testing.T) {
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

	for _, table := range []string{"scoring_models", "scoring_model_versions", "profile_scores"} {
		var exists bool
		if err := store.pool.QueryRow(ctx, `SELECT to_regclass('public.' || $1) IS NOT NULL`, table).Scan(&exists); err != nil {
			t.Fatal(err)
		}
		if !exists {
			t.Fatalf("table %s does not exist", table)
		}
	}

	rawKey := fmt.Sprintf("scoring-default-scopes-%d", time.Now().UnixNano())
	if err := store.EnsureDevelopmentTenant(ctx, rawKey); err != nil {
		t.Fatal(err)
	}
	principal, err := store.Authenticate(ctx, rawKey)
	if err != nil {
		t.Fatal(err)
	}
	for _, scope := range []string{"scoring:read", "scoring:write", "scoring:compute"} {
		if !principal.HasScope(scope) {
			t.Fatalf("fresh API key scopes %v do not include %q", principal.Scopes, scope)
		}
	}
	if _, err := store.CreateRole(ctx, principal, "Scoring analyst", []string{
		"scoring:read", "scoring:write", "scoring:compute",
	}); err != nil {
		t.Fatalf("CreateRole scoring scopes: %v", err)
	}
}

func TestExperimentBindingsMigrationIntegration(t *testing.T) {
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

	for table, columns := range map[string][]string{
		"campaigns":               {"experiment_id"},
		"delivery_attempts":       {"experiment_id", "variant"},
		"journey_message_intents": {"experiment_id", "variant"},
	} {
		for _, column := range columns {
			var exists bool
			if err := store.pool.QueryRow(ctx, `SELECT EXISTS (
				SELECT 1 FROM information_schema.columns
				WHERE table_schema='public' AND table_name=$1 AND column_name=$2
			)`, table, column).Scan(&exists); err != nil {
				t.Fatal(err)
			}
			if !exists {
				t.Fatalf("column %s.%s does not exist", table, column)
			}
		}
	}

	checks := map[string][]string{
		"delivery_attempts_decision_check": {
			"sent", "suppressed", "no_consent", "fatigued", "render_failed", "send_failed",
			"failed", "holdout", "processing", "provider_sent", "retryable_failed", "event_emitted",
		},
		"journey_message_intents_decision_check": {
			"sent", "suppressed", "no_consent", "fatigued", "render_failed", "send_failed",
			"failed", "holdout", "processing", "provider_sent", "retryable_failed",
		},
	}
	for constraint, decisions := range checks {
		var definition string
		if err := store.pool.QueryRow(ctx, `SELECT pg_get_constraintdef(oid)
			FROM pg_constraint WHERE conname=$1`, constraint).Scan(&definition); err != nil {
			t.Fatalf("read constraint %s: %v", constraint, err)
		}
		for _, decision := range decisions {
			if !strings.Contains(definition, "'"+decision+"'") {
				t.Errorf("constraint %s does not allow %q: %s", constraint, decision, definition)
			}
		}
	}
}

func TestJourneysStoreIntegration(t *testing.T) {
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

	keyA := fmt.Sprintf("journey-store-a-%d", time.Now().UnixNano())
	keyB := fmt.Sprintf("journey-store-b-%d", time.Now().UnixNano())
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

	description := "welcome journey"
	created, err := store.CreateJourney(ctx, tenantA, domain.Journey{
		Name:        "Welcome",
		Description: &description,
		Graph:       json.RawMessage(`{"entry_node_id":"n1"}`),
	})
	if err != nil {
		t.Fatalf("CreateJourney: %v", err)
	}
	if created.Status != "draft" || created.LatestVersion != 0 {
		t.Fatalf("created journey=%+v", created)
	}

	fetched, err := store.GetJourney(ctx, tenantA, created.ID)
	if err != nil {
		t.Fatalf("GetJourney: %v", err)
	}
	var fetchedGraph map[string]any
	if err := json.Unmarshal(fetched.Graph, &fetchedGraph); err != nil {
		t.Fatalf("decode fetched graph: %v", err)
	}
	if fetchedGraph["entry_node_id"] != "n1" {
		t.Fatalf("fetched graph=%s", fetched.Graph)
	}
	if _, err := store.GetJourney(ctx, tenantB, created.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("cross-tenant GetJourney err=%v", err)
	}

	list, err := store.ListJourneys(ctx, tenantA)
	if err != nil {
		t.Fatalf("ListJourneys: %v", err)
	}
	found := false
	for _, item := range list {
		if item.ID == created.ID {
			found = true
		}
		if item.TenantID != tenantA.TenantID || item.WorkspaceID != tenantA.WorkspaceID {
			t.Fatalf("unscoped list item=%+v", item)
		}
	}
	if !found {
		t.Fatalf("created journey missing from list=%+v", list)
	}

	created.Name = "Updated Welcome"
	created.Graph = json.RawMessage(`{"entry_node_id":"n2"}`)
	updated, err := store.UpdateJourney(ctx, tenantA, created)
	if err != nil {
		t.Fatalf("UpdateJourney: %v", err)
	}
	var updatedGraph map[string]any
	if err := json.Unmarshal(updated.Graph, &updatedGraph); err != nil {
		t.Fatalf("decode updated graph: %v", err)
	}
	if updated.Name != "Updated Welcome" || updatedGraph["entry_node_id"] != "n2" {
		t.Fatalf("updated journey=%+v", updated)
	}
	if _, err := store.UpdateJourney(ctx, tenantB, updated); !errors.Is(err, ErrNotFound) {
		t.Fatalf("cross-tenant UpdateJourney err=%v", err)
	}

	if _, err := store.pool.Exec(ctx, `UPDATE journeys SET status='published' WHERE id=$1`, created.ID); err != nil {
		t.Fatal(err)
	}
	updated.Name = "Blocked Edit"
	updated.Status = "published"
	if _, err := store.UpdateJourney(ctx, tenantA, updated); err == nil {
		t.Fatal("published journey edit succeeded")
	}
	updated.Status = "draft"
	reverted, err := store.UpdateJourney(ctx, tenantA, updated)
	if err != nil {
		t.Fatalf("revert published journey to draft: %v", err)
	}
	if reverted.Status != "draft" {
		t.Fatalf("reverted journey status=%s", reverted.Status)
	}
}

func TestPublishJourneyIntegration(t *testing.T) {
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

	key := fmt.Sprintf("journey-publish-%d", time.Now().UnixNano())
	if err := store.EnsureDevelopmentTenant(ctx, key); err != nil {
		t.Fatal(err)
	}
	principal, err := store.Authenticate(ctx, key)
	if err != nil {
		t.Fatal(err)
	}
	approverID := "00000000-0000-0000-0000-000000000001"
	blobs := &memoryBlobs{objects: map[string][]byte{}}

	validGraph := json.RawMessage(`{
		"entry_node_id":"n1",
		"nodes":[
			{"id":"n1","type":"entry","config":{"trigger":"event","event_type":"signup.completed"}},
			{"id":"n2","type":"exit","config":{"reason":"completed"}}
		],
		"edges":[{"from":"n1","to":"n2"}]
	}`)
	created, err := store.CreateJourney(ctx, principal, domain.Journey{Name: "Publishable", Graph: validGraph})
	if err != nil {
		t.Fatalf("CreateJourney: %v", err)
	}
	version, err := journeyflow.Publish(ctx, store, blobs, principal, created.ID, approverID)
	if err != nil {
		t.Fatalf("Publish: %v", err)
	}
	if version.Version != 1 || version.ManifestKey == nil || version.EntryKind != "event" || version.EntryEventType == nil || *version.EntryEventType != "signup.completed" {
		t.Fatalf("unexpected version=%+v", version)
	}
	if _, err := blobs.Get(ctx, *version.ManifestKey); err != nil {
		t.Fatalf("manifest missing from blob store: %v", err)
	}
	published, err := store.GetJourney(ctx, principal, created.ID)
	if err != nil {
		t.Fatalf("GetJourney: %v", err)
	}
	if published.Status != "published" || published.LatestVersion != 1 || published.CurrentVersionID == nil || *published.CurrentVersionID != version.ID {
		t.Fatalf("unexpected published journey=%+v", published)
	}

	invalid, err := store.CreateJourney(ctx, principal, domain.Journey{
		Name:  "Invalid",
		Graph: json.RawMessage(`{"entry_node_id":"n1","nodes":[],"edges":[]}`),
	})
	if err != nil {
		t.Fatalf("CreateJourney invalid: %v", err)
	}
	beforeBlobCount := len(blobs.objects)
	_, err = journeyflow.Publish(ctx, store, blobs, principal, invalid.ID, approverID)
	if !errors.Is(err, journeyflow.ErrInvalidGraph) {
		t.Fatalf("expected invalid graph error, got %v", err)
	}
	var versions int
	if err := store.pool.QueryRow(ctx, `SELECT count(*) FROM journey_versions WHERE journey_id=$1`, invalid.ID).Scan(&versions); err != nil {
		t.Fatalf("count versions: %v", err)
	}
	if versions != 0 {
		t.Fatalf("invalid publish created %d version rows", versions)
	}
	if len(blobs.objects) != beforeBlobCount {
		t.Fatalf("invalid publish wrote blob objects")
	}
}

func TestAIGatewaySchema_11_1_1(t *testing.T) {
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

	// 1. Verify we can insert and query from ai_provider_configs table
	var tenantID, workspaceID string
	err = store.pool.QueryRow(ctx, "INSERT INTO tenants(name) VALUES('AI Test Tenant') RETURNING id").Scan(&tenantID)
	if err != nil {
		t.Fatalf("failed to insert tenant: %v", err)
	}
	err = store.pool.QueryRow(ctx, "INSERT INTO workspaces(tenant_id, name) VALUES($1, 'AI Test Workspace') RETURNING id", tenantID).Scan(&workspaceID)
	if err != nil {
		t.Fatalf("failed to insert workspace: %v", err)
	}

	_, err = store.pool.Exec(ctx, `
		INSERT INTO ai_provider_configs (tenant_id, workspace_id, provider, is_default, config, endpoint_allowlist)
		VALUES ($1, $2, 'fake', true, '{"api_key_ref": "FAKE_KEY"}'::jsonb, ARRAY['localhost:8080'])
	`, tenantID, workspaceID)
	if err != nil {
		t.Fatalf("failed to insert into ai_provider_configs: %v", err)
	}

	var provider string
	err = store.pool.QueryRow(ctx, "SELECT provider FROM ai_provider_configs WHERE tenant_id = $1", tenantID).Scan(&provider)
	if err != nil || provider != "fake" {
		t.Fatalf("failed to query ai_provider_configs: err=%v, provider=%s", err, provider)
	}

	// 2. Verify a fresh key carries the new scopes
	devTenantKey := "ai-schema-test-dev-key"
	err = store.EnsureDevelopmentTenant(ctx, devTenantKey)
	if err != nil {
		t.Fatalf("failed to EnsureDevelopmentTenant: %v", err)
	}
	principal, err := store.Authenticate(ctx, devTenantKey)
	if err != nil {
		t.Fatalf("failed to Authenticate: %v", err)
	}

	expectedScopes := []string{"ai:read", "ai:configure", "ai:invoke", "prompts:read", "prompts:write"}
	for _, s := range expectedScopes {
		if !principal.HasScope(s) {
			t.Errorf("fresh key is missing scope: %s", s)
		}
	}

	// 3. Verify widened check constraint on operation_jobs
	_, err = store.pool.Exec(ctx, `
		INSERT INTO operation_jobs (tenant_id, workspace_id, job_type, payload)
		VALUES ($1, $2, 'ai.generate', '{}'::jsonb)
	`, tenantID, workspaceID)
	if err != nil {
		t.Errorf("expected job_type 'ai.generate' to be allowed, but insert failed: %v", err)
	}

	_, err = store.pool.Exec(ctx, `
		INSERT INTO operation_jobs (tenant_id, workspace_id, job_type, payload)
		VALUES ($1, $2, 'ai.invalid_type', '{}'::jsonb)
	`, tenantID, workspaceID)
	if err == nil {
		t.Errorf("expected job_type 'ai.invalid_type' to be rejected by the CHECK constraint, but insert succeeded")
	} else if !strings.Contains(err.Error(), "operation_jobs_job_type_check") {
		t.Errorf("expected check constraint error 'operation_jobs_job_type_check', got: %v", err)
	}
}

func TestListAIActivityTenantWorkspaceScope_11_6_3(t *testing.T) {
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

	var tenantA, workspaceA, tenantB, workspaceB string
	if err := store.pool.QueryRow(ctx, "INSERT INTO tenants(name) VALUES($1) RETURNING id", "activity-a-"+time.Now().Format("150405.000000000")).Scan(&tenantA); err != nil {
		t.Fatal(err)
	}
	if err := store.pool.QueryRow(ctx, "INSERT INTO workspaces(tenant_id,name) VALUES($1,$2) RETURNING id", tenantA, "workspace-a").Scan(&workspaceA); err != nil {
		t.Fatal(err)
	}
	if err := store.pool.QueryRow(ctx, "INSERT INTO tenants(name) VALUES($1) RETURNING id", "activity-b-"+time.Now().Format("150405.000000000")).Scan(&tenantB); err != nil {
		t.Fatal(err)
	}
	if err := store.pool.QueryRow(ctx, "INSERT INTO workspaces(tenant_id,name) VALUES($1,$2) RETURNING id", tenantB, "workspace-b").Scan(&workspaceB); err != nil {
		t.Fatal(err)
	}
	_, err = store.pool.Exec(ctx, `INSERT INTO ai_activity
		(tenant_id,workspace_id,action,provider,model,policy_decision)
		VALUES($1,$2,'ai.allowed','fake','fake-model','allowed'),($3,$4,'ai.denied','fake','fake-model','denied_policy')`,
		tenantA, workspaceA, tenantB, workspaceB)
	if err != nil {
		t.Fatal(err)
	}

	items, err := store.ListAIActivity(ctx, domain.Principal{TenantID: tenantA, WorkspaceID: workspaceA}, 100)
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 1 || items[0].Action != "ai.allowed" || items[0].TenantID != tenantA || items[0].WorkspaceID != workspaceA {
		t.Fatalf("unexpected scoped activity: %+v", items)
	}
	otherWorkspaceItems, err := store.ListAIActivity(ctx, domain.Principal{TenantID: tenantA, WorkspaceID: workspaceB}, 100)
	if err != nil {
		t.Fatal(err)
	}
	if len(otherWorkspaceItems) != 0 {
		t.Fatalf("activity leaked across workspace: %+v", otherWorkspaceItems)
	}
}

func TestAIActivityHardening_12_0_2(t *testing.T) {
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

	// 1. Verify invalid policy_decision is rejected
	_, err = store.pool.Exec(ctx, `INSERT INTO ai_activity
		(tenant_id, workspace_id, action, provider, model, policy_decision)
		VALUES($1, $2, 'ai.generate', 'fake', 'fake-model', 'invalid_decision')`,
		tenantID, p.WorkspaceID)
	if err == nil {
		t.Fatal("expected insert with invalid policy_decision to fail CHECK constraint")
	}

	// 2. Verify all valid policy_decision values are accepted
	validDecisions := []string{
		"allowed", "denied_policy", "denied_budget", "denied_scope",
		"denied_input", "schema_reject", "execution_error",
	}
	for _, decision := range validDecisions {
		var activityID string
		err = store.pool.QueryRow(ctx, `INSERT INTO ai_activity
			(tenant_id, workspace_id, action, provider, model, policy_decision)
			VALUES($1, $2, 'ai.generate', 'fake', 'fake-model', $3) RETURNING id`,
			tenantID, p.WorkspaceID, decision).Scan(&activityID)
		if err != nil {
			t.Fatalf("expected policy_decision %q to be accepted, but got err: %v", decision, err)
		}

		// 3. Try to UPDATE and expect it to fail due to trigger
		_, err = store.pool.Exec(ctx, `UPDATE ai_activity SET action = 'ai.modified' WHERE id = $1`, activityID)
		if err == nil {
			t.Fatal("expected UPDATE on ai_activity to fail due to trigger")
		} else if !strings.Contains(err.Error(), "ai_activity is append-only") {
			t.Fatalf("expected 'ai_activity is append-only' error on UPDATE, got: %v", err)
		}

		// 4. Try to DELETE and expect it to fail due to trigger
		_, err = store.pool.Exec(ctx, `DELETE FROM ai_activity WHERE id = $1`, activityID)
		if err == nil {
			t.Fatal("expected DELETE on ai_activity to fail due to trigger")
		} else if !strings.Contains(err.Error(), "ai_activity is append-only") {
			t.Fatalf("expected 'ai_activity is append-only' error on DELETE, got: %v", err)
		}
	}
}

func TestAIProviderConfigCRUD_11_1_3(t *testing.T) {
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

	// Create test tenant/workspace
	var tenantID, workspaceID string
	err = store.pool.QueryRow(ctx, "INSERT INTO tenants(name) VALUES('AI Config CRUD Tenant') RETURNING id").Scan(&tenantID)
	if err != nil {
		t.Fatalf("failed to insert tenant: %v", err)
	}
	err = store.pool.QueryRow(ctx, "INSERT INTO workspaces(tenant_id, name) VALUES($1, 'AI Config CRUD Workspace') RETURNING id", tenantID).Scan(&workspaceID)
	if err != nil {
		t.Fatalf("failed to insert workspace: %v", err)
	}

	p := domain.Principal{
		TenantID:    tenantID,
		WorkspaceID: workspaceID,
	}

	// 1. Create a config with a secret in the JSON config
	configWithSecret := []byte(`{"api_key_ref": "CLAUDE_API_KEY", "api_key": "supersecretvalue123", "default_model": "claude-3-opus-20240229"}`)
	cfg := domain.AIProviderConfig{
		Provider:           "anthropic",
		IsDefault:          true,
		Config:             configWithSecret,
		EndpointAllowlist:  []string{"api.anthropic.com"},
		MonthlyBudgetCents: 5000,
		Status:             "active",
	}

	created, err := store.CreateAIProviderConfig(ctx, p, cfg)
	if err != nil {
		t.Fatalf("failed to CreateAIProviderConfig: %v", err)
	}

	if created.ID == "" {
		t.Errorf("expected generated UUID, got empty")
	}

	// Verify that the secret "api_key" is redacted in the returned struct
	var createdJSON map[string]any
	if err := json.Unmarshal(created.Config, &createdJSON); err != nil {
		t.Fatalf("failed to unmarshal created config: %v", err)
	}
	if _, ok := createdJSON["api_key"]; ok {
		t.Errorf("api_key should have been redacted/removed from config, but is present")
	}
	if createdJSON["api_key_ref"] != "CLAUDE_API_KEY" {
		t.Errorf("expected api_key_ref to be CLAUDE_API_KEY, got %v", createdJSON["api_key_ref"])
	}

	// 2. Get the config by ID and verify redaction
	fetched, err := store.GetAIProviderConfig(ctx, p, created.ID)
	if err != nil {
		t.Fatalf("failed to GetAIProviderConfig: %v", err)
	}

	var fetchedJSON map[string]any
	if err := json.Unmarshal(fetched.Config, &fetchedJSON); err != nil {
		t.Fatalf("failed to unmarshal fetched config: %v", err)
	}
	if _, ok := fetchedJSON["api_key"]; ok {
		t.Errorf("api_key should have been redacted in Get, but is present")
	}

	// 3. Get Default AI Provider Config
	defaultCfg, err := store.GetDefaultAIProviderConfig(ctx, p)
	if err != nil {
		t.Fatalf("failed to GetDefaultAIProviderConfig: %v", err)
	}
	if defaultCfg.ID != created.ID {
		t.Errorf("expected default config ID %s, got %s", created.ID, defaultCfg.ID)
	}

	// 4. Create a second config and set it as default (should unset the first default)
	cfg2 := domain.AIProviderConfig{
		Provider:           "openai",
		IsDefault:          true,
		Config:             []byte(`{"api_key": "openai-secret", "api_key_ref": "OPENAI_API_KEY"}`),
		EndpointAllowlist:  []string{"api.openai.com"},
		MonthlyBudgetCents: 10000,
		Status:             "active",
	}

	created2, err := store.CreateAIProviderConfig(ctx, p, cfg2)
	if err != nil {
		t.Fatalf("failed to Create second config: %v", err)
	}

	// First config should not be default anymore
	fetched1Again, err := store.GetAIProviderConfig(ctx, p, created.ID)
	if err != nil {
		t.Fatal(err)
	}
	if fetched1Again.IsDefault {
		t.Errorf("first config should have been updated to not default")
	}

	// GetDefault should now return the second config
	defaultCfg2, err := store.GetDefaultAIProviderConfig(ctx, p)
	if err != nil {
		t.Fatalf("failed to GetDefaultAIProviderConfig: %v", err)
	}
	if defaultCfg2.ID != created2.ID {
		t.Errorf("expected default config ID to switch to %s, got %s", created2.ID, defaultCfg2.ID)
	}

	// 5. Update the first config to make it default again
	fetched1Again.IsDefault = true
	fetched1Again.Config = []byte(`{"api_key": "updatedsecret", "api_key_ref": "CLAUDE_API_KEY"}`)
	updated, err := store.UpdateAIProviderConfig(ctx, p, fetched1Again)
	if err != nil {
		t.Fatalf("failed to UpdateAIProviderConfig: %v", err)
	}

	var updatedJSON map[string]any
	if err := json.Unmarshal(updated.Config, &updatedJSON); err != nil {
		t.Fatalf("failed to unmarshal updated config: %v", err)
	}
	if _, ok := updatedJSON["api_key"]; ok {
		t.Errorf("api_key should have been redacted in Update, but is present")
	}

	// Fetch default config again, should be first config ID
	defaultCfg3, err := store.GetDefaultAIProviderConfig(ctx, p)
	if err != nil {
		t.Fatal(err)
	}
	if defaultCfg3.ID != created.ID {
		t.Errorf("expected default config ID to switch back to %s, got %s", created.ID, defaultCfg3.ID)
	}

	// 6. List configs
	list, err := store.ListAIProviderConfigs(ctx, p)
	if err != nil {
		t.Fatalf("failed to ListAIProviderConfigs: %v", err)
	}
	if len(list) != 2 {
		t.Errorf("expected 2 configs in list, got %d", len(list))
	}
	for _, item := range list {
		var itemJSON map[string]any
		_ = json.Unmarshal(item.Config, &itemJSON)
		if _, ok := itemJSON["api_key"]; ok {
			t.Errorf("api_key should be redacted in List for id %s", item.ID)
		}
	}

	// 7. Delete config
	err = store.DeleteAIProviderConfig(ctx, p, created.ID)
	if err != nil {
		t.Fatalf("failed to DeleteAIProviderConfig: %v", err)
	}

	_, err = store.GetAIProviderConfig(ctx, p, created.ID)
	if !errors.Is(err, ErrNotFound) {
		t.Errorf("expected ErrNotFound for deleted config, got %v", err)
	}
}

func TestAIBudgetUsage_11_1_4(t *testing.T) {
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

	// Insert dummy tenant and workspace to satisfy foreign key constraints if needed, but wait:
	// ai_budget_usage table does NOT have foreign key constraints to tenants/workspaces!
	// Let's verify by looking at the schema in docs:
	// CREATE TABLE IF NOT EXISTS ai_budget_usage (
	//     tenant_id uuid NOT NULL,
	//     workspace_id uuid NOT NULL,
	// ...
	// Since tenant_id and workspace_id are uuid but do not REFERENCES tenants/workspaces in the migration:
	// Wait! Let's insert a real tenant and workspace just to be safe and avoid type/constraint issues.
	var tID, wID string
	err = store.pool.QueryRow(ctx, "INSERT INTO tenants(name) VALUES('AI Budget Tenant') RETURNING id").Scan(&tID)
	if err != nil {
		t.Fatalf("failed to insert tenant: %v", err)
	}
	err = store.pool.QueryRow(ctx, "INSERT INTO workspaces(tenant_id, name) VALUES($1, 'AI Budget Workspace') RETURNING id", tID).Scan(&wID)
	if err != nil {
		t.Fatalf("failed to insert workspace: %v", err)
	}

	period := "2026-07"

	// 1. Get budget usage - should return zeroed usage
	usage, err := store.GetAIBudgetUsage(ctx, tID, wID, period)
	if err != nil {
		t.Fatalf("failed to GetAIBudgetUsage: %v", err)
	}
	if usage.CostCents != 0 || usage.InputTokens != 0 || usage.OutputTokens != 0 {
		t.Errorf("expected zeroed budget usage, got %+v", usage)
	}

	// 2. Increment budget usage
	err = store.IncrementAIBudgetUsage(ctx, tID, wID, period, 50, 1000, 2000)
	if err != nil {
		t.Fatalf("failed to IncrementAIBudgetUsage: %v", err)
	}

	usage, err = store.GetAIBudgetUsage(ctx, tID, wID, period)
	if err != nil {
		t.Fatalf("failed to GetAIBudgetUsage after increment: %v", err)
	}
	if usage.CostCents != 50 || usage.InputTokens != 1000 || usage.OutputTokens != 2000 {
		t.Errorf("unexpected usage values: %+v", usage)
	}

	// 3. Increment again
	err = store.IncrementAIBudgetUsage(ctx, tID, wID, period, 25, 500, 500)
	if err != nil {
		t.Fatalf("failed to IncrementAIBudgetUsage second time: %v", err)
	}

	usage, err = store.GetAIBudgetUsage(ctx, tID, wID, period)
	if err != nil {
		t.Fatalf("failed to GetAIBudgetUsage after second increment: %v", err)
	}
	if usage.CostCents != 75 || usage.InputTokens != 1500 || usage.OutputTokens != 2500 {
		t.Errorf("unexpected accumulated usage values: %+v", usage)
	}
}

func TestAIRegistrySchema_11_3_1(t *testing.T) {
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

	var tenantID, workspaceID string
	err = store.pool.QueryRow(ctx, "INSERT INTO tenants(name) VALUES('AI Registry Schema Tenant') RETURNING id").Scan(&tenantID)
	if err != nil {
		t.Fatalf("failed to insert tenant: %v", err)
	}
	err = store.pool.QueryRow(ctx, "INSERT INTO workspaces(tenant_id, name) VALUES($1, 'AI Registry Schema Workspace') RETURNING id", tenantID).Scan(&workspaceID)
	if err != nil {
		t.Fatalf("failed to insert workspace: %v", err)
	}

	// 1. Insert into prompts table
	var promptID string
	err = store.pool.QueryRow(ctx, `
		INSERT INTO prompts (tenant_id, workspace_id, name, task_type)
		VALUES ($1, $2, 'test-prompt', 'content_draft') RETURNING id
	`, tenantID, workspaceID).Scan(&promptID)
	if err != nil {
		t.Fatalf("failed to insert prompt: %v", err)
	}

	// Verify task_type CHECK constraint on prompts
	_, err = store.pool.Exec(ctx, `
		INSERT INTO prompts (tenant_id, workspace_id, name, task_type)
		VALUES ($1, $2, 'test-prompt-invalid', 'invalid_task_type')
	`, tenantID, workspaceID)
	if err == nil {
		t.Errorf("expected invalid task_type to be rejected, but it succeeded")
	}

	// 2. Insert into prompt_versions table
	var versionID string
	err = store.pool.QueryRow(ctx, `
		INSERT INTO prompt_versions (prompt_id, tenant_id, version, template, input_schema, output_schema, provider, model, manifest_key, status, eval_status)
		VALUES ($1, $2, 1, 'System template', '{}'::jsonb, '{}'::jsonb, 'fake', 'fake-model', 'manifest-123', 'draft', 'pending')
		RETURNING id
	`, promptID, tenantID).Scan(&versionID)
	if err != nil {
		t.Fatalf("failed to insert prompt_version: %v", err)
	}

	// Verify status CHECK constraint on prompt_versions
	_, err = store.pool.Exec(ctx, `
		INSERT INTO prompt_versions (prompt_id, tenant_id, version, template, input_schema, output_schema, provider, model, manifest_key, status, eval_status)
		VALUES ($1, $2, 2, 'System template', '{}'::jsonb, '{}'::jsonb, 'fake', 'fake-model', 'manifest-123', 'invalid_status', 'pending')
	`, promptID, tenantID)
	if err == nil {
		t.Errorf("expected invalid status to be rejected, but it succeeded")
	}

	// Verify eval_status CHECK constraint on prompt_versions
	_, err = store.pool.Exec(ctx, `
		INSERT INTO prompt_versions (prompt_id, tenant_id, version, template, input_schema, output_schema, provider, model, manifest_key, status, eval_status)
		VALUES ($1, $2, 3, 'System template', '{}'::jsonb, '{}'::jsonb, 'fake', 'fake-model', 'manifest-123', 'draft', 'invalid_eval_status')
	`, promptID, tenantID)
	if err == nil {
		t.Errorf("expected invalid eval_status to be rejected, but it succeeded")
	}
}

func TestMain(m *testing.M) {
	databaseURL := os.Getenv("OPENJOURNEY_TEST_DATABASE_URL")
	if databaseURL != "" {
		fmt.Println("running PostgreSQL integration tests")
		ctx := context.Background()
		store, err := Open(ctx, databaseURL)
		if err == nil {
			_ = store.Migrate(ctx)
			_, _ = store.pool.Exec(ctx, `TRUNCATE tenants, workspaces, applications, api_keys, accepted_events, 
				projection_jobs, profiles, segments, templates, campaigns, tenant_quotas, quota_windows, 
				identity_aliases, journey_runs, journey_steps, journey_transitions, journey_message_intents CASCADE`)
			store.Close()
		}
	}
	os.Exit(m.Run())
}
