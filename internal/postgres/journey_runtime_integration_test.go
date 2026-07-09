package postgres

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/buildwithdmytro/openjourney/internal/domain"
	journeyflow "github.com/buildwithdmytro/openjourney/internal/journey"
)

func TestJourneyRuntimeIntegration(t *testing.T) {
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

	key := fmt.Sprintf("journey-runtime-%d", time.Now().UnixNano())
	if err := store.EnsureDevelopmentTenant(ctx, key); err != nil {
		t.Fatal(err)
	}
	p, err := store.Authenticate(ctx, key)
	if err != nil {
		t.Fatal(err)
	}

	appID, err := store.GetFirstAppID(ctx, p.TenantID, p.WorkspaceID)
	if err != nil {
		t.Fatalf("GetFirstAppID: %v", err)
	}
	p.AppID = appID

	// 1. Create a profile using AcceptEvents and ProjectEvent
	profileExtID := "profile-runtime-test"
	events := []domain.Event{
		{
			Type: "profile.updated", SchemaVersion: 1, ExternalID: profileExtID,
			IdempotencyKey: "identify-runtime-test", OccurredAt: time.Now().UTC(),
			Payload: json.RawMessage(`{"attributes":{"country":"US", "email":"runtime@example.com"}}`),
		},
	}
	_, err = store.AcceptEvents(ctx, p, events)
	if err != nil {
		t.Fatalf("AcceptEvents: %v", err)
	}
	job, found, err := store.ClaimProjectionJob(ctx)
	if err != nil || !found {
		t.Fatalf("claim projection job: %v", err)
	}
	if err := store.ProjectEvent(ctx, job); err != nil {
		t.Fatalf("project event: %v", err)
	}

	// Fetch the created profile to get its UUID
	profile, _, err := store.GetProfile(ctx, p, profileExtID)
	if err != nil {
		t.Fatalf("GetProfile: %v", err)
	}

	// 2. Create a journey and version
	validGraph := json.RawMessage(`{
		"entry_node_id":"n1",
		"nodes":[
			{"id":"n1","type":"entry","config":{"trigger":"event","event_type":"signup.completed"}},
			{"id":"n2","type":"exit","config":{"reason":"completed"}}
		],
		"edges":[{"from":"n1","to":"n2"}]
	}`)
	created, err := store.CreateJourney(ctx, p, domain.Journey{Name: "Runtime Journey", Graph: validGraph})
	if err != nil {
		t.Fatalf("CreateJourney: %v", err)
	}
	blobs := &memoryBlobs{objects: map[string][]byte{}}
	approverID := "00000000-0000-0000-0000-000000000001"
	version, err := journeyflow.Publish(ctx, store, blobs, p, created.ID, approverID)
	if err != nil {
		t.Fatalf("Publish: %v", err)
	}

	// 3. Test CreateJourneyRun
	run := domain.JourneyRun{
		TenantID:          p.TenantID,
		WorkspaceID:       p.WorkspaceID,
		JourneyID:         created.ID,
		JourneyVersionID:  version.ID,
		ProfileID:         profile.ID,
		EntryKey:          "event-triggered-key",
		ReentrySequence:   0,
		Status:            "active",
		CurrentNodeID:     "n1",
		State:             json.RawMessage("{}"),
	}
	inserted, err := store.CreateJourneyRun(ctx, run)
	if err != nil {
		t.Fatalf("CreateJourneyRun: %v", err)
	}
	if !inserted {
		t.Fatalf("expected run to be inserted")
	}

	// Test ON CONFLICT DO NOTHING (should return false)
	insertedAgain, err := store.CreateJourneyRun(ctx, run)
	if err != nil {
		t.Fatalf("CreateJourneyRun second time: %v", err)
	}
	if insertedAgain {
		t.Fatalf("expected run not to be inserted second time")
	}

	// Fetch run
	var fetchedRun domain.JourneyRun
	err = store.pool.QueryRow(ctx, `SELECT id FROM journey_runs WHERE tenant_id=$1 AND journey_version_id=$2 AND profile_id=$3`, p.TenantID, version.ID, profile.ID).Scan(&fetchedRun.ID)
	if err != nil {
		t.Fatalf("select run ID: %v", err)
	}

	run.ID = fetchedRun.ID
	fetchedRun, err = store.GetJourneyRun(ctx, p, run.ID)
	if err != nil {
		t.Fatalf("GetJourneyRun: %v", err)
	}
	if fetchedRun.CurrentNodeID != "n1" {
		t.Errorf("expected current node n1, got %s", fetchedRun.CurrentNodeID)
	}

	// 4. Test UpdateJourneyRun
	fetchedRun.CurrentNodeID = "n2"
	fetchedRun.Status = "completed"
	completedAt := time.Now().UTC()
	fetchedRun.CompletedAt = &completedAt
	updatedRun, err := store.UpdateJourneyRun(ctx, p, fetchedRun)
	if err != nil {
		t.Fatalf("UpdateJourneyRun: %v", err)
	}
	if updatedRun.CurrentNodeID != "n2" || updatedRun.Status != "completed" || updatedRun.CompletedAt == nil {
		t.Errorf("unexpected updated run fields: %+v", updatedRun)
	}

	// 5. Test Journey Steps (durable queue)
	step := domain.JourneyStep{
		RunID:       run.ID,
		TenantID:    p.TenantID,
		NodeID:      "n1",
		Kind:        "advance",
		Status:      "pending",
		AvailableAt: time.Now().Add(-10 * time.Second),
	}
	err = store.InsertJourneyStep(ctx, step)
	if err != nil {
		t.Fatalf("InsertJourneyStep: %v", err)
	}

	claimedStep, claimed, err := store.ClaimJourneyStep(ctx)
	if err != nil {
		t.Fatalf("ClaimJourneyStep: %v", err)
	}
	if !claimed {
		t.Fatalf("expected step to be claimed")
	}
	if claimedStep.NodeID != "n1" || claimedStep.Status != "processing" {
		t.Errorf("unexpected claimed step fields: %+v", claimedStep)
	}

	// Complete step
	err = store.CompleteJourneyStep(ctx, claimedStep.ID)
	if err != nil {
		t.Fatalf("CompleteJourneyStep: %v", err)
	}

	// Fail step
	step2 := domain.JourneyStep{
		RunID:       run.ID,
		TenantID:    p.TenantID,
		NodeID:      "n2",
		Kind:        "advance",
		Status:      "pending",
		AvailableAt: time.Now().Add(-10 * time.Second),
	}
	err = store.InsertJourneyStep(ctx, step2)
	if err != nil {
		t.Fatalf("InsertJourneyStep 2: %v", err)
	}
	claimedStep2, claimed2, err := store.ClaimJourneyStep(ctx)
	if err != nil || !claimed2 {
		t.Fatalf("ClaimJourneyStep 2: %v, claimed=%v", err, claimed2)
	}
	err = store.FailJourneyStep(ctx, claimedStep2.ID, "some error")
	if err != nil {
		t.Fatalf("FailJourneyStep: %v", err)
	}

	// 6. Test RecordTransition
	trans := domain.JourneyTransition{
		RunID:      run.ID,
		TenantID:   p.TenantID,
		NodeType:   "entry",
		Outcome:    "advanced",
		Detail:     json.RawMessage(`{"from":"n1","to":"n2"}`),
	}
	err = store.RecordTransition(ctx, trans)
	if err != nil {
		t.Fatalf("RecordTransition: %v", err)
	}

	// 7. Test AdvanceRunTx
	step3 := domain.JourneyStep{
		RunID:       run.ID,
		TenantID:    p.TenantID,
		NodeID:      "n3",
		Kind:        "advance",
		Status:      "pending",
		AvailableAt: time.Now().Add(-10 * time.Second),
	}
	err = store.InsertJourneyStep(ctx, step3)
	if err != nil {
		t.Fatalf("InsertJourneyStep 3: %v", err)
	}
	claimedStep3, claimed3, err := store.ClaimJourneyStep(ctx)
	if err != nil || !claimed3 {
		t.Fatalf("ClaimJourneyStep 3: %v, claimed=%v", err, claimed3)
	}

	runForAdvance := fetchedRun
	runForAdvance.CurrentNodeID = "n4"
	nextStep := &domain.JourneyStep{
		RunID:       run.ID,
		TenantID:    p.TenantID,
		NodeID:      "n4",
		Kind:        "advance",
		Status:      "pending",
		AvailableAt: time.Now().Add(5 * time.Minute),
	}
	transForAdvance := domain.JourneyTransition{
		RunID:    run.ID,
		TenantID: p.TenantID,
		FromNode: ptrStr("n3"),
		ToNode:   ptrStr("n4"),
		NodeType: "delay",
		Outcome:  "waited",
	}

	err = store.AdvanceRunTx(ctx, run.ID, runForAdvance, claimedStep3.ID, nextStep, transForAdvance)
	if err != nil {
		t.Fatalf("AdvanceRunTx: %v", err)
	}

	// Clean up
	_, _ = store.pool.Exec(ctx, "DELETE FROM tenants WHERE id=$1", p.TenantID)
}

func TestJourneyEventTriggeredEnrollmentIntegration(t *testing.T) {
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

	key := fmt.Sprintf("journey-event-%d", time.Now().UnixNano())
	if err := store.EnsureDevelopmentTenant(ctx, key); err != nil {
		t.Fatal(err)
	}
	p, err := store.Authenticate(ctx, key)
	if err != nil {
		t.Fatal(err)
	}
	appID, err := store.GetFirstAppID(ctx, p.TenantID, p.WorkspaceID)
	if err != nil {
		t.Fatal(err)
	}
	p.AppID = appID

	profileExtID := "profile-enroll-test"
	events := []domain.Event{
		{
			Type: "profile.updated", SchemaVersion: 1, ExternalID: profileExtID,
			IdempotencyKey: "identify-enroll-test", OccurredAt: time.Now().UTC(),
			Payload: json.RawMessage(`{"attributes":{"country":"US"}}`),
		},
	}
	_, err = store.AcceptEvents(ctx, p, events)
	if err != nil {
		t.Fatal(err)
	}
	job, _, _ := store.ClaimProjectionJob(ctx)
	_ = store.ProjectEvent(ctx, job)

	profile, _, _ := store.GetProfile(ctx, p, profileExtID)

	validGraph := json.RawMessage(`{
		"entry_node_id":"n1",
		"nodes":[
			{"id":"n1","type":"entry","config":{"trigger":"event","event_type":"order.created"}},
			{"id":"n2","type":"exit","config":{"reason":"completed"}}
		],
		"edges":[{"from":"n1","to":"n2"}]
	}`)
	created, err := store.CreateJourney(ctx, p, domain.Journey{Name: "Event-Triggered", Graph: validGraph})
	if err != nil {
		t.Fatal(err)
	}
	blobs := &memoryBlobs{objects: map[string][]byte{}}
	approverID := "00000000-0000-0000-0000-000000000001"
	version, err := journeyflow.Publish(ctx, store, blobs, p, created.ID, approverID)
	if err != nil {
		t.Fatal(err)
	}

	triggerEvent := []domain.Event{
		{
			Type: "order.created", SchemaVersion: 1, ExternalID: profileExtID,
			IdempotencyKey: "order-1", OccurredAt: time.Now().UTC(),
			Payload: json.RawMessage(`{}`),
		},
	}
	_, err = store.AcceptEvents(ctx, p, triggerEvent)
	if err != nil {
		t.Fatal(err)
	}
	job2, found2, err := store.ClaimProjectionJob(ctx)
	if err != nil || !found2 {
		t.Fatalf("ClaimProjectionJob 2: err=%v, found=%v", err, found2)
	}
	if err := store.ProjectEvent(ctx, job2); err != nil {
		t.Fatalf("ProjectEvent: %v", err)
	}

	runs, err := store.GetJourneyRunsForProfile(ctx, p.TenantID, version.ID, profile.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(runs) != 1 {
		t.Fatalf("expected 1 run, got %d", len(runs))
	}
	if runs[0].CurrentNodeID != "n1" || runs[0].Status != "active" {
		t.Errorf("unexpected run state: %+v", runs[0])
	}

	_, err = store.AcceptEvents(ctx, p, triggerEvent)
	if err != nil {
		t.Fatal(err)
	}
	_, found3, err := store.ClaimProjectionJob(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if found3 {
		t.Fatalf("expected duplicate event not to create a new projection job")
	}
	runs, _ = store.GetJourneyRunsForProfile(ctx, p.TenantID, version.ID, profile.ID)
	if len(runs) != 1 {
		t.Errorf("expected still 1 run on duplicate event, got %d", len(runs))
	}

	_, _ = store.pool.Exec(ctx, "DELETE FROM tenants WHERE id=$1", p.TenantID)
}

func TestJourneyWaitEventResolutionIntegration(t *testing.T) {
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

	key := fmt.Sprintf("journey-wait-%d", time.Now().UnixNano())
	if err := store.EnsureDevelopmentTenant(ctx, key); err != nil {
		t.Fatal(err)
	}
	p, err := store.Authenticate(ctx, key)
	if err != nil {
		t.Fatal(err)
	}
	appID, err := store.GetFirstAppID(ctx, p.TenantID, p.WorkspaceID)
	if err != nil {
		t.Fatal(err)
	}
	p.AppID = appID

	profileExtID := "profile-wait-test"
	events := []domain.Event{
		{
			Type: "profile.updated", SchemaVersion: 1, ExternalID: profileExtID,
			IdempotencyKey: "identify-wait-test", OccurredAt: time.Now().UTC(),
			Payload: json.RawMessage(`{"attributes":{"country":"US"}}`),
		},
	}
	_, err = store.AcceptEvents(ctx, p, events)
	if err != nil {
		t.Fatal(err)
	}
	job, _, _ := store.ClaimProjectionJob(ctx)
	_ = store.ProjectEvent(ctx, job)

	profile, _, _ := store.GetProfile(ctx, p, profileExtID)

	validGraph := json.RawMessage(`{
		"entry_node_id":"n1",
		"nodes":[
			{"id":"n1","type":"entry","config":{"trigger":"event","event_type":"signup"}},
			{"id":"n2","type":"wait_event","config":{"event_type":"email.opened","timeout":"24h"}},
			{"id":"n3","type":"exit","config":{"reason":"success"}},
			{"id":"n4","type":"exit","config":{"reason":"timeout"}}
		],
		"edges":[
			{"from":"n1","to":"n2"},
			{"from":"n2","to":"n3","branch":"success"},
			{"from":"n2","to":"n4","branch":"timeout"}
		]
	}`)
	created, _ := store.CreateJourney(ctx, p, domain.Journey{Name: "Wait Journey", Graph: validGraph})
	blobs := &memoryBlobs{objects: map[string][]byte{}}
	version, _ := journeyflow.Publish(ctx, store, blobs, p, created.ID, "00000000-0000-0000-0000-000000000001")

	run := domain.JourneyRun{
		TenantID:          p.TenantID,
		WorkspaceID:       p.WorkspaceID,
		JourneyID:         created.ID,
		JourneyVersionID:  version.ID,
		ProfileID:         profile.ID,
		SubjectExternalID: &profileExtID,
		EntryKey:          "manual-entry",
		Status:            "active",
		CurrentNodeID:     "n1",
	}
	_, _ = store.CreateJourneyRun(ctx, run)
	var fetchedRun domain.JourneyRun
	_ = store.pool.QueryRow(ctx, `SELECT id FROM journey_runs WHERE tenant_id=$1 AND profile_id=$2`, p.TenantID, profile.ID).Scan(&fetchedRun.ID)
	
	step := domain.JourneyStep{
		RunID:       fetchedRun.ID,
		TenantID:    p.TenantID,
		NodeID:      "n1",
		Kind:        "advance",
		Status:      "pending",
		AvailableAt: time.Now(),
	}
	_ = store.InsertJourneyStep(ctx, step)

	deps := journeyflow.Deps{Clock: journeyflow.RealClock{}}
	processed, err := journeyflow.TickNext(ctx, store, deps)
	if err != nil || !processed {
		t.Fatalf("TickNext entry: processed=%v, err=%v", processed, err)
	}

	processed, err = journeyflow.TickNext(ctx, store, deps)
	if err != nil || !processed {
		t.Fatalf("TickNext wait: processed=%v, err=%v", processed, err)
	}

	fetchedRun, _ = store.GetJourneyRun(ctx, p, fetchedRun.ID)
	if fetchedRun.Status != "waiting" || fetchedRun.WaitEventType == nil || *fetchedRun.WaitEventType != "email.opened" {
		t.Errorf("expected waiting at n2, got status=%s event=%v", fetchedRun.Status, fetchedRun.WaitEventType)
	}

	awaitedEvent := []domain.Event{
		{
			Type: "email.opened", SchemaVersion: 1, ExternalID: profileExtID,
			IdempotencyKey: "open-1", OccurredAt: time.Now().UTC(),
			Payload: json.RawMessage(`{}`),
		},
	}
	_, err = store.AcceptEvents(ctx, p, awaitedEvent)
	if err != nil {
		t.Fatal(err)
	}
	job2, _, _ := store.ClaimProjectionJob(ctx)
	if err := store.ProjectEvent(ctx, job2); err != nil {
		t.Fatalf("ProjectEvent wait resolve: %v", err)
	}

	fetchedRun, _ = store.GetJourneyRun(ctx, p, fetchedRun.ID)
	if fetchedRun.Status != "active" || fetchedRun.CurrentNodeID != "n3" || fetchedRun.WaitEventType != nil {
		t.Errorf("expected run active at n3, got status=%s node=%s", fetchedRun.Status, fetchedRun.CurrentNodeID)
	}

	_, _ = store.pool.Exec(ctx, "DELETE FROM tenants WHERE id=$1", p.TenantID)
}

func TestJourneyWaitEventTimeoutIntegration(t *testing.T) {
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

	key := fmt.Sprintf("journey-timeout-%d", time.Now().UnixNano())
	if err := store.EnsureDevelopmentTenant(ctx, key); err != nil {
		t.Fatal(err)
	}
	p, err := store.Authenticate(ctx, key)
	if err != nil {
		t.Fatal(err)
	}
	appID, err := store.GetFirstAppID(ctx, p.TenantID, p.WorkspaceID)
	if err != nil {
		t.Fatal(err)
	}
	p.AppID = appID

	profileExtID := "profile-timeout-test"
	events := []domain.Event{
		{
			Type: "profile.updated", SchemaVersion: 1, ExternalID: profileExtID,
			IdempotencyKey: "identify-timeout-test", OccurredAt: time.Now().UTC(),
			Payload: json.RawMessage(`{"attributes":{"country":"US"}}`),
		},
	}
	_, err = store.AcceptEvents(ctx, p, events)
	if err != nil {
		t.Fatal(err)
	}
	job, _, _ := store.ClaimProjectionJob(ctx)
	_ = store.ProjectEvent(ctx, job)

	profile, _, _ := store.GetProfile(ctx, p, profileExtID)

	validGraph := json.RawMessage(`{
		"entry_node_id":"n1",
		"nodes":[
			{"id":"n1","type":"entry","config":{"trigger":"event","event_type":"signup"}},
			{"id":"n2","type":"wait_event","config":{"event_type":"email.opened","timeout":"24h"}},
			{"id":"n3","type":"exit","config":{"reason":"success"}},
			{"id":"n4","type":"exit","config":{"reason":"timeout"}}
		],
		"edges":[
			{"from":"n1","to":"n2"},
			{"from":"n2","to":"n3","branch":"success"},
			{"from":"n2","to":"n4","branch":"timeout"}
		]
	}`)
	created, _ := store.CreateJourney(ctx, p, domain.Journey{Name: "Timeout Journey", Graph: validGraph})
	blobs := &memoryBlobs{objects: map[string][]byte{}}
	version, _ := journeyflow.Publish(ctx, store, blobs, p, created.ID, "00000000-0000-0000-0000-000000000001")

	run := domain.JourneyRun{
		TenantID:          p.TenantID,
		WorkspaceID:       p.WorkspaceID,
		JourneyID:         created.ID,
		JourneyVersionID:  version.ID,
		ProfileID:         profile.ID,
		SubjectExternalID: &profileExtID,
		EntryKey:          "manual-entry",
		Status:            "active",
		CurrentNodeID:     "n1",
	}
	_, _ = store.CreateJourneyRun(ctx, run)
	var fetchedRun domain.JourneyRun
	_ = store.pool.QueryRow(ctx, `SELECT id FROM journey_runs WHERE tenant_id=$1 AND profile_id=$2`, p.TenantID, profile.ID).Scan(&fetchedRun.ID)
	
	step := domain.JourneyStep{
		RunID:       fetchedRun.ID,
		TenantID:    p.TenantID,
		NodeID:      "n1",
		Kind:        "advance",
		Status:      "pending",
		AvailableAt: time.Now(),
	}
	_ = store.InsertJourneyStep(ctx, step)

	deps := journeyflow.Deps{Clock: journeyflow.RealClock{}}
	processed, err := journeyflow.TickNext(ctx, store, deps)
	if err != nil || !processed {
		t.Fatalf("TickNext entry: processed=%v, err=%v", processed, err)
	}

	processed, err = journeyflow.TickNext(ctx, store, deps)
	if err != nil || !processed {
		t.Fatalf("TickNext wait: processed=%v, err=%v", processed, err)
	}

	fetchedRun, _ = store.GetJourneyRun(ctx, p, fetchedRun.ID)
	if fetchedRun.Status != "waiting" || fetchedRun.WaitEventType == nil || *fetchedRun.WaitEventType != "email.opened" {
		t.Errorf("expected waiting at n2, got status=%s event=%v", fetchedRun.Status, fetchedRun.WaitEventType)
	}

	// Update the timeout step's available_at to the past so it is due
	_, err = store.pool.Exec(ctx, `
		UPDATE journey_steps
		SET available_at = now() - interval '1 hour'
		WHERE run_id = $1 AND kind = 'timeout' AND status = 'pending'
	`, fetchedRun.ID)
	if err != nil {
		t.Fatalf("failed to update step available_at: %v", err)
	}

	// Now run TickNext to process the timeout step
	processed, err = journeyflow.TickNext(ctx, store, deps)
	if err != nil || !processed {
		t.Fatalf("TickNext timeout: processed=%v, err=%v", processed, err)
	}

	fetchedRun, _ = store.GetJourneyRun(ctx, p, fetchedRun.ID)
	if fetchedRun.Status != "active" || fetchedRun.CurrentNodeID != "n4" || fetchedRun.WaitEventType != nil {
		t.Errorf("expected run active at n4 after timeout, got status=%s node=%s", fetchedRun.Status, fetchedRun.CurrentNodeID)
	}

	// Clean up
	_, _ = store.pool.Exec(ctx, "DELETE FROM tenants WHERE id=$1", p.TenantID)
}

func ptrStr(s string) *string {
	return &s
}
