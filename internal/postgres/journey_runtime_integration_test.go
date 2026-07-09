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

	err = store.AdvanceRunTx(ctx, run.ID, runForAdvance, claimedStep3.ID, nextStep, transForAdvance, nil)
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

func TestJourneyScheduledEntryIntegration(t *testing.T) {
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

	key := fmt.Sprintf("journey-sched-%d", time.Now().UnixNano())
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

	// 1. Insert two profiles, but only one matches the segment
	var pS1ID string
	err = store.pool.QueryRow(ctx, `
		INSERT INTO profiles(id, tenant_id, workspace_id, app_id, external_id, attributes)
		VALUES(gen_random_uuid(), $1, $2, $3, 'ext-s1', '{"country":"US","age":25}')
		RETURNING id
	`, p.TenantID, p.WorkspaceID, p.AppID).Scan(&pS1ID)
	if err != nil {
		t.Fatalf("insert profile p-s1: %v", err)
	}

	_, err = store.pool.Exec(ctx, `
		INSERT INTO profiles(id, tenant_id, workspace_id, app_id, external_id, attributes)
		VALUES(gen_random_uuid(), $1, $2, $3, 'ext-s2', '{"country":"CA","age":30}')
	`, p.TenantID, p.WorkspaceID, p.AppID)
	if err != nil {
		t.Fatalf("insert profile p-s2: %v", err)
	}

	// 2. Create the segment
	seg, err := store.CreateSegment(ctx, p, domain.Segment{
		Name: "US Users",
		DSL: json.RawMessage(`{
			"type": "profile_attribute",
			"field": "country",
			"operator": "equals",
			"value": "US"
		}`),
	})
	if err != nil {
		t.Fatalf("create segment: %v", err)
	}

	// 3. Create scheduled entry journey
	validGraph := json.RawMessage(fmt.Sprintf(`{
		"entry_node_id":"n1",
		"nodes":[
			{"id":"n1","type":"entry","config":{"trigger":"scheduled","segment_id":"%s","schedule":"*/5 * * * *"}},
			{"id":"n2","type":"exit","config":{"reason":"success"}}
		],
		"edges":[
			{"from":"n1","to":"n2"}
		]
	}`, seg.ID))
	created, _ := store.CreateJourney(ctx, p, domain.Journey{Name: "Scheduled Entry Journey", Graph: validGraph})
	blobs := &memoryBlobs{objects: map[string][]byte{}}
	version, _ := journeyflow.Publish(ctx, store, blobs, p, created.ID, "00000000-0000-0000-0000-000000000001")

	// Verify that the version's fields were inferred correctly on publish
	if version.EntryKind != "scheduled" {
		t.Errorf("expected entry_kind='scheduled', got %s", version.EntryKind)
	}
	if version.EntrySegmentID == nil || *version.EntrySegmentID != seg.ID {
		t.Errorf("expected entry_segment_id=%s, got %v", seg.ID, version.EntrySegmentID)
	}
	if version.EntrySchedule == nil || *version.EntrySchedule != "*/5 * * * *" {
		t.Errorf("expected entry_schedule='*/5 * * * *', got %v", version.EntrySchedule)
	}

	// 4. Enroll scheduled due
	// Let's use a clock set to minute 5, which matches "*/5 * * * *"
	clock := journeyflow.NewFakeClock(time.Date(2026, 1, 1, 12, 5, 0, 0, time.UTC))
	err = journeyflow.EnrollScheduledDue(ctx, store, clock)
	if err != nil {
		t.Fatalf("unexpected EnrollScheduledDue error: %v", err)
	}

	// 5. Assertions
	// We expect exactly 1 run for p-s1 ('p-s1' is 'profile-1' segment member, 'p-s2' Canada user is not)
	var runCount int
	err = store.pool.QueryRow(ctx, `SELECT count(*) FROM journey_runs WHERE tenant_id=$1 AND journey_id=$2`, p.TenantID, created.ID).Scan(&runCount)
	if err != nil {
		t.Fatal(err)
	}
	if runCount != 1 {
		t.Errorf("expected exactly 1 journey run, got %d", runCount)
	}

	var run domain.JourneyRun
	err = store.pool.QueryRow(ctx, `SELECT id, profile_id, status FROM journey_runs WHERE tenant_id=$1 AND journey_id=$2`, p.TenantID, created.ID).Scan(&run.ID, &run.ProfileID, &run.Status)
	if err != nil {
		t.Fatal(err)
	}
	if run.ProfileID != pS1ID {
		t.Errorf("expected run to be for profile %s, got %s", pS1ID, run.ProfileID)
	}
	if run.Status != "active" {
		t.Errorf("expected run status to be active, got %s", run.Status)
	}

	// Verify step exists for run
	var stepCount int
	err = store.pool.QueryRow(ctx, `SELECT count(*) FROM journey_steps WHERE tenant_id=$1 AND run_id=$2 AND status='pending'`, p.TenantID, run.ID).Scan(&stepCount)
	if err != nil {
		t.Fatal(err)
	}
	if stepCount != 1 {
		t.Errorf("expected exactly 1 pending step for the run, got %d", stepCount)
	}

	// 6. Running it again at the same minute should NOT create duplicate runs due to effectively-once entry_key
	err = journeyflow.EnrollScheduledDue(ctx, store, clock)
	if err != nil {
		t.Fatalf("unexpected EnrollScheduledDue error on duplicate: %v", err)
	}
	err = store.pool.QueryRow(ctx, `SELECT count(*) FROM journey_runs WHERE tenant_id=$1 AND journey_id=$2`, p.TenantID, created.ID).Scan(&runCount)
	if err != nil {
		t.Fatal(err)
	}
	if runCount != 1 {
		t.Errorf("expected exactly 1 journey run (no duplicates), got %d", runCount)
	}

	// Clean up
	_, _ = store.pool.Exec(ctx, "DELETE FROM tenants WHERE id=$1", p.TenantID)
}

func TestJourneyMessageNodeExecutorIntegration(t *testing.T) {
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

	key := fmt.Sprintf("journey-msg-%d", time.Now().UnixNano())
	if err := store.EnsureDevelopmentTenant(ctx, key); err != nil {
		t.Fatal(err)
	}
	p, err := store.Authenticate(ctx, key)
	if err != nil {
		t.Fatal(err)
	}

	_, _ = store.pool.Exec(ctx, "DELETE FROM journey_steps WHERE tenant_id=$1", p.TenantID)
	_, _ = store.pool.Exec(ctx, "DELETE FROM journey_runs WHERE tenant_id=$1", p.TenantID)
	_, _ = store.pool.Exec(ctx, "DELETE FROM journey_message_intents WHERE tenant_id=$1", p.TenantID)

	t.Cleanup(func() {
		_, _ = store.pool.Exec(context.Background(), "DELETE FROM journey_steps WHERE tenant_id=$1", p.TenantID)
		_, _ = store.pool.Exec(context.Background(), "DELETE FROM journey_runs WHERE tenant_id=$1", p.TenantID)
		_, _ = store.pool.Exec(context.Background(), "DELETE FROM journey_message_intents WHERE tenant_id=$1", p.TenantID)
		_, _ = store.pool.Exec(context.Background(), "DELETE FROM tenants WHERE id=$1", p.TenantID)
	})
	appID, err := store.GetFirstAppID(ctx, p.TenantID, p.WorkspaceID)
	if err != nil {
		t.Fatal(err)
	}
	p.AppID = appID

	// 1. Create a Template
	tmpl, err := store.CreateTemplate(ctx, p, domain.Template{
		Name:            "Test Message Node Template",
		SubjectTemplate: ptrStr("Hello journey-flow!"),
		HTMLTemplate:    ptrStr("Welcome to the journey."),
		Channel:         "email",
	})
	if err != nil {
		t.Fatalf("CreateTemplate: %v", err)
	}

	// 2. Create a Profile with an email attribute
	events := []domain.Event{
		{
			Type: "profile.updated", SchemaVersion: 1, ExternalID: "p-msg-node",
			IdempotencyKey: "identify-msg-node", OccurredAt: time.Now().UTC(),
			Payload: json.RawMessage(`{"attributes":{"email":"user@example.com"}}`),
		},
	}
	_, err = store.AcceptEvents(ctx, p, events)
	if err != nil {
		t.Fatal(err)
	}
	job, _, _ := store.ClaimProjectionJob(ctx)
	_ = store.ProjectEvent(ctx, job)

	var profileID string
	err = store.pool.QueryRow(ctx, `SELECT id FROM profiles WHERE tenant_id=$1 AND external_id='p-msg-node'`, p.TenantID).Scan(&profileID)
	if err != nil {
		t.Fatal(err)
	}

	// 3. Create and Publish a Journey with NodeTypeMessage
	graphJSON := []byte(fmt.Sprintf(`{
		"entry_node_id": "n1",
		"nodes": [
			{"id": "n1", "type": "entry", "config": {"trigger": "event", "event_type": "user.signup"}},
			{"id": "n2", "type": "message", "config": {"template_id": "%s", "channel": "email", "transactional": false}},
			{"id": "n3", "type": "exit", "config": {}}
		],
		"edges": [
			{"from": "n1", "to": "n2"},
			{"from": "n2", "to": "n3"}
		]
	}`, tmpl.ID))

	journey, err := store.CreateJourney(ctx, p, domain.Journey{
		Name: "Message Node Journey Integration",
	})
	if err != nil {
		t.Fatal(err)
	}

	_, err = store.pool.Exec(ctx, `UPDATE journeys SET graph=$1, status='published' WHERE id=$2`, graphJSON, journey.ID)
	if err != nil {
		t.Fatal(err)
	}

	version, err := store.PublishJourney(ctx, p, journey.ID, "00000000-0000-0000-0000-000000000001", "manifest-key")
	if err != nil {
		t.Fatal(err)
	}

	// 4. Create a Journey Run and pending step on the message node (n2)
	run := domain.JourneyRun{
		TenantID:         p.TenantID,
		WorkspaceID:      p.WorkspaceID,
		JourneyID:        journey.ID,
		JourneyVersionID: version.ID,
		ProfileID:        profileID,
		Status:           "active",
		CurrentNodeID:    "n2",
		EntryKey:         "test-msg-key",
	}
	inserted, err := store.CreateJourneyRun(ctx, run)
	if err != nil || !inserted {
		t.Fatalf("CreateJourneyRun failed: %v, inserted=%v", err, inserted)
	}

	var runID string
	err = store.pool.QueryRow(ctx, `SELECT id FROM journey_runs WHERE tenant_id=$1 AND entry_key='test-msg-key'`, p.TenantID).Scan(&runID)
	if err != nil {
		t.Fatalf("failed to fetch run ID: %v", err)
	}

	step := domain.JourneyStep{
		RunID:       runID,
		TenantID:    p.TenantID,
		NodeID:      "n2",
		Kind:        "advance",
		Status:      "pending",
		AvailableAt: time.Now().Add(-5 * time.Second),
	}
	err = store.InsertJourneyStep(ctx, step)
	if err != nil {
		t.Fatal(err)
	}

	// 5. Execute TickNext
	clock := journeyflow.NewFakeClock(time.Now())
	processed, err := journeyflow.TickNext(ctx, store, journeyflow.Deps{Clock: clock})
	if err != nil {
		t.Fatalf("TickNext: %v", err)
	}
	if !processed {
		t.Fatalf("expected step to be processed")
	}

	// 6. Assert exactly one row was added to journey_message_intents
	var intent domain.JourneyMessageIntent
	err = store.pool.QueryRow(ctx, `SELECT id, run_id, tenant_id, workspace_id, journey_id, journey_version_id, node_id, profile_id, template_id, channel, endpoint, transactional, status, attempts, policy_snapshot
		FROM journey_message_intents WHERE tenant_id=$1 AND run_id=$2`, p.TenantID, runID).
		Scan(&intent.ID, &intent.RunID, &intent.TenantID, &intent.WorkspaceID, &intent.JourneyID, &intent.JourneyVersionID, &intent.NodeID, &intent.ProfileID, &intent.TemplateID, &intent.Channel, &intent.Endpoint, &intent.Transactional, &intent.Status, &intent.Attempts, &intent.PolicySnapshot)
	if err != nil {
		t.Fatalf("failed to fetch message intent: %v", err)
	}

	if intent.NodeID != "n2" {
		t.Errorf("expected NodeID 'n2', got %q", intent.NodeID)
	}
	if intent.ProfileID != profileID {
		t.Errorf("expected ProfileID %q, got %q", profileID, intent.ProfileID)
	}
	if intent.TemplateID != tmpl.ID {
		t.Errorf("expected TemplateID %q, got %q", tmpl.ID, intent.TemplateID)
	}
	if intent.Channel != "email" {
		t.Errorf("expected Channel 'email', got %q", intent.Channel)
	}
	if intent.Endpoint != "user@example.com" {
		t.Errorf("expected Endpoint 'user@example.com', got %q", intent.Endpoint)
	}
	if intent.Transactional {
		t.Errorf("expected Transactional to be false")
	}
	if intent.Status != "pending" {
		t.Errorf("expected Status 'pending', got %q", intent.Status)
	}

	// 7. Verify journey run was advanced to exit node (n3)
	fetchedRun, err := store.GetJourneyRunSystem(ctx, p.TenantID, runID)
	if err != nil {
		t.Fatal(err)
	}
	if fetchedRun.CurrentNodeID != "n3" {
		t.Errorf("expected run to advance to 'n3', got %q", fetchedRun.CurrentNodeID)
	}

	// Clean up
	_, _ = store.pool.Exec(ctx, "DELETE FROM tenants WHERE id=$1", p.TenantID)
}

func TestJourneyDeliveryIntegration(t *testing.T) {
	databaseURL := os.Getenv("OPENJOURNEY_TEST_DATABASE_URL")
	if databaseURL == "" {
		t.Skip("skipping integration test, OPENJOURNEY_TEST_DATABASE_URL not set")
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

	// 1. Setup Tenant and Workspace
	key := fmt.Sprintf("journey-delivery-%d", time.Now().UnixNano())
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

	// Clean up existing intents/runs for safety
	_, _ = store.pool.Exec(ctx, "DELETE FROM journey_message_intents WHERE tenant_id=$1", p.TenantID)
	_, _ = store.pool.Exec(ctx, "DELETE FROM journey_steps WHERE tenant_id=$1", p.TenantID)
	_, _ = store.pool.Exec(ctx, "DELETE FROM journey_runs WHERE tenant_id=$1", p.TenantID)
	_, _ = store.pool.Exec(ctx, "DELETE FROM templates WHERE tenant_id=$1", p.TenantID)
	_, _ = store.pool.Exec(ctx, "DELETE FROM profiles WHERE tenant_id=$1", p.TenantID)

	// 2. Create Profile and Template
	events := []domain.Event{
		{
			Type: "profile.updated", SchemaVersion: 1, ExternalID: "p-delivery-node",
			IdempotencyKey: "identify-delivery-node", OccurredAt: time.Now().UTC(),
			Payload: json.RawMessage(`{"attributes":{"email":"user@example.com"}}`),
		},
	}
	_, err = store.AcceptEvents(ctx, p, events)
	if err != nil {
		t.Fatal(err)
	}
	job, _, _ := store.ClaimProjectionJob(ctx)
	_ = store.ProjectEvent(ctx, job)

	var profileID string
	err = store.pool.QueryRow(ctx, `SELECT id FROM profiles WHERE tenant_id=$1 AND external_id='p-delivery-node'`, p.TenantID).Scan(&profileID)
	if err != nil {
		t.Fatal(err)
	}

	tmpl := domain.Template{
		Name:            "delivery-temp",
		Channel:         "email",
		SubjectTemplate: ptrStr("Hello"),
		HTMLTemplate:    ptrStr("Body"),
	}
	tmpl, err = store.CreateTemplate(ctx, p, tmpl)
	if err != nil {
		t.Fatal(err)
	}

	// 3. Create Journey and Published version
	j, err := store.CreateJourney(ctx, p, domain.Journey{
		Name: "Delivery Journey Integration",
	})
	if err != nil {
		t.Fatal(err)
	}

	graphJSON := []byte(fmt.Sprintf(`{
		"entry_node_id": "n1",
		"nodes": [
			{"id": "n1", "type": "entry", "config": {"trigger": "event", "event_type": "user.signup"}},
			{"id": "n2", "type": "message", "config": {"template_id": "%s", "channel": "email", "transactional": false}},
			{"id": "n3", "type": "exit", "config": {}}
		],
		"edges": [
			{"from": "n1", "to": "n2"},
			{"from": "n2", "to": "n3"}
		]
	}`, tmpl.ID))

	_, err = store.pool.Exec(ctx, `UPDATE journeys SET graph=$1, status='published' WHERE id=$2`, graphJSON, j.ID)
	if err != nil {
		t.Fatal(err)
	}

	v, err := store.PublishJourney(ctx, p, j.ID, "00000000-0000-0000-0000-000000000001", "manifest-key")
	if err != nil {
		t.Fatal(err)
	}

	// 4. Create Active Run
	run := domain.JourneyRun{
		TenantID:         p.TenantID,
		WorkspaceID:      p.WorkspaceID,
		JourneyID:        j.ID,
		JourneyVersionID: v.ID,
		ProfileID:        profileID,
		EntryKey:         "event-delivery-123",
		ReentrySequence:  0,
		Status:           "active",
		CurrentNodeID:    "n2",
	}
	_, err = store.CreateJourneyRun(ctx, run)
	if err != nil {
		t.Fatal(err)
	}

	var runID string
	err = store.pool.QueryRow(ctx, `SELECT id FROM journey_runs WHERE tenant_id=$1 AND entry_key='event-delivery-123'`, p.TenantID).Scan(&runID)
	if err != nil {
		t.Fatalf("failed to fetch run ID: %v", err)
	}
	run.ID = runID

	// 5. Insert Message Intent
	intent := domain.JourneyMessageIntent{
		RunID:            runID,
		TenantID:         p.TenantID,
		WorkspaceID:      p.WorkspaceID,
		JourneyID:        j.ID,
		JourneyVersionID: v.ID,
		NodeID:           "n2",
		ProfileID:        profileID,
		TemplateID:       tmpl.ID,
		Channel:          "email",
		Endpoint:         "user@example.com",
		Status:           "pending",
	}

	step := domain.JourneyStep{
		RunID:       runID,
		TenantID:    p.TenantID,
		NodeID:      "n2",
		Kind:        "advance",
		Status:      "processing",
		AvailableAt: time.Now(),
	}
	err = store.InsertJourneyStep(ctx, step)
	if err != nil {
		t.Fatal(err)
	}

	var stepID string
	err = store.pool.QueryRow(ctx, `SELECT id FROM journey_steps WHERE run_id=$1 AND node_id='n2'`, runID).Scan(&stepID)
	if err != nil {
		t.Fatalf("failed to fetch step ID: %v", err)
	}

	trans := domain.JourneyTransition{
		RunID:    runID,
		TenantID: p.TenantID,
		FromNode: ptrStr("n1"),
		ToNode:   ptrStr("n2"),
		NodeType: "message",
		Outcome:  "advanced",
	}

	err = store.AdvanceRunTx(ctx, runID, run, stepID, nil, trans, &intent)
	if err != nil {
		t.Fatalf("failed to advance and insert intent: %v", err)
	}

	// 6. Claim Message Intent
	claimed, found, err := store.ClaimJourneyMessageIntent(ctx, "worker-1")
	if err != nil {
		t.Fatalf("failed to claim journey message intent: %v", err)
	}
	if !found {
		t.Fatal("expected message intent to be found")
	}

	if claimed.Status != "processing" {
		t.Errorf("expected claimed status to be 'processing', got %q", claimed.Status)
	}
	if claimed.Attempts != 1 {
		t.Errorf("expected attempts to be 1, got %d", claimed.Attempts)
	}
	if claimed.Endpoint != "user@example.com" {
		t.Errorf("expected endpoint to be 'user@example.com', got %q", claimed.Endpoint)
	}

	// 7. Update Message Intent
	dec := "sent"
	claimed.Decision = &dec
	claimed.Status = "completed"
	err = store.UpdateJourneyMessageIntent(ctx, claimed)
	if err != nil {
		t.Fatalf("failed to update journey message intent: %v", err)
	}

	// Verify update in DB
	var dbStatus, dbDecision string
	err = store.pool.QueryRow(ctx, "SELECT status, decision FROM journey_message_intents WHERE run_id=$1 AND node_id=$2", runID, "n2").Scan(&dbStatus, &dbDecision)
	if err != nil {
		t.Fatal(err)
	}

	if dbStatus != "completed" {
		t.Errorf("expected status 'completed', got %q", dbStatus)
	}
	if dbDecision != "sent" {
		t.Errorf("expected decision 'sent', got %q", dbDecision)
	}

	// Clean up
	_, _ = store.pool.Exec(ctx, "DELETE FROM tenants WHERE id=$1", p.TenantID)
}

func TestSentCountSince_FatigueAcrossChannels(t *testing.T) {
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

	key := fmt.Sprintf("journey-fatigue-%d", time.Now().UnixNano())
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

	// Create a profile
	events := []domain.Event{
		{
			Type: "profile.updated", SchemaVersion: 1, ExternalID: "p-fatigue-test",
			IdempotencyKey: "identify-fatigue-test", OccurredAt: time.Now().UTC(),
			Payload: json.RawMessage(`{"attributes":{"email":"fatigue@example.com"}}`),
		},
	}
	_, err = store.AcceptEvents(ctx, p, events)
	if err != nil {
		t.Fatal(err)
	}
	job, _, _ := store.ClaimProjectionJob(ctx)
	_ = store.ProjectEvent(ctx, job)

	var profileID string
	err = store.pool.QueryRow(ctx, `SELECT id FROM profiles WHERE tenant_id=$1 AND external_id='p-fatigue-test'`, p.TenantID).Scan(&profileID)
	if err != nil {
		t.Fatal(err)
	}

	// 1. Initially, sent count is 0
	count, err := store.SentCountSince(ctx, p, profileID, time.Now().Add(-24*time.Hour))
	if err != nil {
		t.Fatalf("SentCountSince: %v", err)
	}
	if count != 0 {
		t.Errorf("expected 0 count initially, got %d", count)
	}

	// Create a template
	tmpl := domain.Template{
		Name:            "fatigue-temp",
		Channel:         "email",
		SubjectTemplate: ptrStr("Hello"),
		HTMLTemplate:    ptrStr("Body"),
	}
	tmpl, err = store.CreateTemplate(ctx, p, tmpl)
	if err != nil {
		t.Fatal(err)
	}

	// Create a segment
	seg, err := store.CreateSegment(ctx, p, domain.Segment{
		Name: "Mock Segment",
	})
	if err != nil {
		t.Fatal(err)
	}

	// 2. Insert into delivery_attempts (representing a campaign send)
	campaignID := "00000000-0000-0000-0000-000000000002"
	// Create a mock campaign to satisfy foreign key constraints
	_, err = store.pool.Exec(ctx, `INSERT INTO campaigns (id, tenant_id, workspace_id, name, segment_id, template_id, status) VALUES ($1, $2, $3, $4, $5, $6, $7)`,
		campaignID, p.TenantID, p.WorkspaceID, "Mock Campaign", seg.ID, tmpl.ID, "sending")
	if err != nil {
		t.Fatal(err)
	}

	_, err = store.pool.Exec(ctx, `INSERT INTO delivery_attempts 
		(id, tenant_id, campaign_id, profile_id, channel, endpoint, decision, attempted_at)
		VALUES (gen_random_uuid(), $1, $2, $3, 'email', 'fatigue@example.com', 'sent', now())`,
		p.TenantID, campaignID, profileID)
	if err != nil {
		t.Fatal(err)
	}

	// 3. SentCountSince should now return 1 (campaign send)
	count, err = store.SentCountSince(ctx, p, profileID, time.Now().Add(-24*time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	if count != 1 {
		t.Errorf("expected count to be 1 after campaign send, got %d", count)
	}

	// 4. Create a mock journey, run, and message intent (representing a journey send)

	j, err := store.CreateJourney(ctx, p, domain.Journey{Name: "Fatigue Journey"})
	if err != nil {
		t.Fatal(err)
	}

	graphJSON := []byte(fmt.Sprintf(`{
		"entry_node_id": "n1",
		"nodes": [
			{"id": "n1", "type": "entry", "config": {"trigger": "event", "event_type": "user.signup"}},
			{"id": "n2", "type": "message", "config": {"template_id": "%s", "channel": "email", "transactional": false}},
			{"id": "n3", "type": "exit", "config": {}}
		],
		"edges": [
			{"from": "n1", "to": "n2"},
			{"from": "n2", "to": "n3"}
		]
	}`, tmpl.ID))
	_, err = store.pool.Exec(ctx, `UPDATE journeys SET graph=$1, status='published' WHERE id=$2`, graphJSON, j.ID)
	if err != nil {
		t.Fatal(err)
	}

	v, err := store.PublishJourney(ctx, p, j.ID, "00000000-0000-0000-0000-000000000001", "manifest-key")
	if err != nil {
		t.Fatal(err)
	}

	run := domain.JourneyRun{
		TenantID:         p.TenantID,
		WorkspaceID:      p.WorkspaceID,
		JourneyID:        j.ID,
		JourneyVersionID: v.ID,
		ProfileID:        profileID,
		EntryKey:         "fatigue-entry-123",
		ReentrySequence:  0,
		Status:           "active",
		CurrentNodeID:    "n2",
	}
	_, err = store.CreateJourneyRun(ctx, run)
	if err != nil {
		t.Fatal(err)
	}

	var runID string
	err = store.pool.QueryRow(ctx, `SELECT id FROM journey_runs WHERE tenant_id=$1 AND entry_key='fatigue-entry-123'`, p.TenantID).Scan(&runID)
	if err != nil {
		t.Fatal(err)
	}
	run.ID = runID

	intent := domain.JourneyMessageIntent{
		RunID:            runID,
		TenantID:         p.TenantID,
		WorkspaceID:      p.WorkspaceID,
		JourneyID:        j.ID,
		JourneyVersionID: v.ID,
		NodeID:           "n2",
		ProfileID:        profileID,
		TemplateID:       tmpl.ID,
		Channel:          "email",
		Endpoint:         "fatigue@example.com",
		Status:           "pending",
	}

	step := domain.JourneyStep{
		RunID:       runID,
		TenantID:    p.TenantID,
		NodeID:      "n2",
		Kind:        "advance",
		Status:      "processing",
		AvailableAt: time.Now(),
	}
	err = store.InsertJourneyStep(ctx, step)
	if err != nil {
		t.Fatal(err)
	}

	var stepID string
	err = store.pool.QueryRow(ctx, `SELECT id FROM journey_steps WHERE run_id=$1 AND node_id='n2'`, runID).Scan(&stepID)
	if err != nil {
		t.Fatal(err)
	}

	trans := domain.JourneyTransition{
		RunID:    runID,
		TenantID: p.TenantID,
		FromNode: ptrStr("n1"),
		ToNode:   ptrStr("n2"),
		NodeType: "message",
		Outcome:  "advanced",
	}

	err = store.AdvanceRunTx(ctx, runID, run, stepID, nil, trans, &intent)
	if err != nil {
		t.Fatal(err)
	}

	// Claim the intent and mark as 'sent'
	claimed, found, err := store.ClaimJourneyMessageIntent(ctx, "worker-fatigue")
	if err != nil || !found {
		t.Fatalf("failed to claim: %v, found: %v", err, found)
	}

	dec := "sent"
	claimed.Decision = &dec
	claimed.Status = "completed"
	err = store.UpdateJourneyMessageIntent(ctx, claimed)
	if err != nil {
		t.Fatal(err)
	}

	// 5. SentCountSince should now return 2 (1 campaign send + 1 journey send)
	count, err = store.SentCountSince(ctx, p, profileID, time.Now().Add(-24*time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	if count != 2 {
		t.Errorf("expected count to be 2 (campaign + journey sends), got %d", count)
	}

	// Clean up
	_, _ = store.pool.Exec(ctx, "DELETE FROM tenants WHERE id=$1", p.TenantID)
}

func TestClaimJourneyMessageIntent_PriorityAndFairness(t *testing.T) {
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

	// 1. Setup Tenant and Workspace A
	keyA := fmt.Sprintf("tenant-fairness-a-%d", time.Now().UnixNano())
	if err := store.EnsureDevelopmentTenant(ctx, keyA); err != nil {
		t.Fatal(err)
	}
	pA, err := store.Authenticate(ctx, keyA)
	if err != nil {
		t.Fatal(err)
	}
	appIDA, err := store.GetFirstAppID(ctx, pA.TenantID, pA.WorkspaceID)
	if err != nil {
		t.Fatal(err)
	}
	pA.AppID = appIDA

	// Setup Tenant and Workspace B
	keyB := fmt.Sprintf("tenant-fairness-b-%d", time.Now().UnixNano())
	if err := store.EnsureDevelopmentTenant(ctx, keyB); err != nil {
		t.Fatal(err)
	}
	pB, err := store.Authenticate(ctx, keyB)
	if err != nil {
		t.Fatal(err)
	}
	appIDB, err := store.GetFirstAppID(ctx, pB.TenantID, pB.WorkspaceID)
	if err != nil {
		t.Fatal(err)
	}
	pB.AppID = appIDB

	// Cleanup at the end
	defer func() {
		_, _ = store.pool.Exec(ctx, "DELETE FROM tenants WHERE id IN ($1, $2)", pA.TenantID, pB.TenantID)
	}()

	// Create profiles
	var profileIDA, profileIDB string
	err = store.pool.QueryRow(ctx, `INSERT INTO profiles (tenant_id, workspace_id, app_id, external_id) VALUES ($1, $2, $3, $4) RETURNING id`,
		pA.TenantID, pA.WorkspaceID, pA.AppID, "prof-a").Scan(&profileIDA)
	if err != nil {
		t.Fatal(err)
	}
	err = store.pool.QueryRow(ctx, `INSERT INTO profiles (tenant_id, workspace_id, app_id, external_id) VALUES ($1, $2, $3, $4) RETURNING id`,
		pB.TenantID, pB.WorkspaceID, pB.AppID, "prof-b").Scan(&profileIDB)
	if err != nil {
		t.Fatal(err)
	}

	// Create templates
	var templateIDA, templateIDB string
	err = store.pool.QueryRow(ctx, `INSERT INTO templates (tenant_id, workspace_id, name, channel, html_template) VALUES ($1, $2, 'tA', 'email', 'HTML') RETURNING id`,
		pA.TenantID, pA.WorkspaceID).Scan(&templateIDA)
	if err != nil {
		t.Fatal(err)
	}
	err = store.pool.QueryRow(ctx, `INSERT INTO templates (tenant_id, workspace_id, name, channel, html_template) VALUES ($1, $2, 'tB', 'email', 'HTML') RETURNING id`,
		pB.TenantID, pB.WorkspaceID).Scan(&templateIDB)
	if err != nil {
		t.Fatal(err)
	}

	// Create journeys
	var journeyIDA, journeyIDB string
	err = store.pool.QueryRow(ctx, `INSERT INTO journeys (tenant_id, workspace_id, name) VALUES ($1, $2, 'jA') RETURNING id`,
		pA.TenantID, pA.WorkspaceID).Scan(&journeyIDA)
	if err != nil {
		t.Fatal(err)
	}
	err = store.pool.QueryRow(ctx, `INSERT INTO journeys (tenant_id, workspace_id, name) VALUES ($1, $2, 'jB') RETURNING id`,
		pB.TenantID, pB.WorkspaceID).Scan(&journeyIDB)
	if err != nil {
		t.Fatal(err)
	}

	// Create journey versions
	var verIDA, verIDB string
	err = store.pool.QueryRow(ctx, `INSERT INTO journey_versions (journey_id, tenant_id, workspace_id, version, graph, entry_kind, reentry_policy) VALUES ($1, $2, $3, 1, '{}'::jsonb, 'event', 'once') RETURNING id`,
		journeyIDA, pA.TenantID, pA.WorkspaceID).Scan(&verIDA)
	if err != nil {
		t.Fatal(err)
	}
	err = store.pool.QueryRow(ctx, `INSERT INTO journey_versions (journey_id, tenant_id, workspace_id, version, graph, entry_kind, reentry_policy) VALUES ($1, $2, $3, 1, '{}'::jsonb, 'event', 'once') RETURNING id`,
		journeyIDB, pB.TenantID, pB.WorkspaceID).Scan(&verIDB)
	if err != nil {
		t.Fatal(err)
	}

	// Create journey runs
	var runIDA, runIDB string
	err = store.pool.QueryRow(ctx, `INSERT INTO journey_runs (tenant_id, workspace_id, journey_id, journey_version_id, profile_id, entry_key, current_node_id) VALUES ($1, $2, $3, $4, $5, 'entryA', 'node') RETURNING id`,
		pA.TenantID, pA.WorkspaceID, journeyIDA, verIDA, profileIDA).Scan(&runIDA)
	if err != nil {
		t.Fatal(err)
	}
	err = store.pool.QueryRow(ctx, `INSERT INTO journey_runs (tenant_id, workspace_id, journey_id, journey_version_id, profile_id, entry_key, current_node_id) VALUES ($1, $2, $3, $4, $5, 'entryB', 'node') RETURNING id`,
		pB.TenantID, pB.WorkspaceID, journeyIDB, verIDB, profileIDB).Scan(&runIDB)
	if err != nil {
		t.Fatal(err)
	}

	// PART 1: Priority
	// Insert 1 marketing intent and 1 transactional intent for Tenant B.
	// Make sure the marketing one is created FIRST (older available_at), but transactional should be claimed first.
	now := time.Now()
	var marketingIntentID, transactionalIntentID string
	err = store.pool.QueryRow(ctx, `INSERT INTO journey_message_intents 
		(run_id, tenant_id, workspace_id, journey_id, journey_version_id, node_id, profile_id, template_id, channel, endpoint, transactional, status, available_at)
		VALUES ($1, $2, $3, $4, $5, 'node-m', $6, $7, 'email', 'm@example.com', false, 'pending', $8) RETURNING id`,
		runIDB, pB.TenantID, pB.WorkspaceID, journeyIDB, verIDB, profileIDB, templateIDB, now.Add(-10*time.Minute)).Scan(&marketingIntentID)
	if err != nil {
		t.Fatal(err)
	}

	err = store.pool.QueryRow(ctx, `INSERT INTO journey_message_intents 
		(run_id, tenant_id, workspace_id, journey_id, journey_version_id, node_id, profile_id, template_id, channel, endpoint, transactional, status, available_at)
		VALUES ($1, $2, $3, $4, $5, 'node-t', $6, $7, 'email', 't@example.com', true, 'pending', $8) RETURNING id`,
		runIDB, pB.TenantID, pB.WorkspaceID, journeyIDB, verIDB, profileIDB, templateIDB, now.Add(-5*time.Minute)).Scan(&transactionalIntentID)
	if err != nil {
		t.Fatal(err)
	}

	// Claim 1: Should be the transactional one
	claimed1, found1, err := store.ClaimJourneyMessageIntent(ctx, "worker-fairness")
	if err != nil {
		t.Fatal(err)
	}
	if !found1 {
		t.Fatal("expected to claim a message intent")
	}
	if claimed1.ID != transactionalIntentID {
		t.Errorf("expected to claim transactional intent %s, got %s", transactionalIntentID, claimed1.ID)
	}

	// Set claimed1 to completed
	_, err = store.pool.Exec(ctx, `UPDATE journey_message_intents SET status='completed' WHERE id=$1`, claimed1.ID)
	if err != nil {
		t.Fatal(err)
	}

	// Claim 2: Should be the marketing one
	claimed2, found2, err := store.ClaimJourneyMessageIntent(ctx, "worker-fairness")
	if err != nil {
		t.Fatal(err)
	}
	if !found2 {
		t.Fatal("expected to claim marketing message intent")
	}
	if claimed2.ID != marketingIntentID {
		t.Errorf("expected to claim marketing intent %s, got %s", marketingIntentID, claimed2.ID)
	}

	// Set claimed2 to completed
	_, err = store.pool.Exec(ctx, `UPDATE journey_message_intents SET status='completed' WHERE id=$1`, claimed2.ID)
	if err != nil {
		t.Fatal(err)
	}

	// PART 2: Fairness / In-flight cap
	// Insert 10 in-flight (processing) intents for Tenant A, and 1 pending intent for Tenant A.
	// Insert 1 pending intent for Tenant B.
	// Because Tenant A is at the in-flight cap of 10, the claim should skip Tenant A's pending intent
	// and claim Tenant B's pending intent instead!

	// 10 in-flight for Tenant A
	for i := 0; i < 10; i++ {
		_, err = store.pool.Exec(ctx, `INSERT INTO journey_message_intents 
			(run_id, tenant_id, workspace_id, journey_id, journey_version_id, node_id, profile_id, template_id, channel, endpoint, transactional, status, locked_until, available_at)
			VALUES ($1, $2, $3, $4, $5, $6, $7, $8, 'email', 'a-processing@example.com', true, 'processing', $9, $10)`,
			runIDA, pA.TenantID, pA.WorkspaceID, journeyIDA, verIDA, fmt.Sprintf("node-a-inflight-%d", i), profileIDA, templateIDA, now.Add(5*time.Minute), now.Add(-10*time.Minute))
		if err != nil {
			t.Fatal(err)
		}
	}

	// 1 pending for Tenant A (created older/first, so normally ordered first)
	var pendingA string
	err = store.pool.QueryRow(ctx, `INSERT INTO journey_message_intents 
		(run_id, tenant_id, workspace_id, journey_id, journey_version_id, node_id, profile_id, template_id, channel, endpoint, transactional, status, available_at)
		VALUES ($1, $2, $3, $4, $5, 'node-a-pending', $6, $7, 'email', 'a-pending@example.com', true, 'pending', $8) RETURNING id`,
		runIDA, pA.TenantID, pA.WorkspaceID, journeyIDA, verIDA, profileIDA, templateIDA, now.Add(-20*time.Minute)).Scan(&pendingA)
	if err != nil {
		t.Fatal(err)
	}

	// 1 pending for Tenant B
	var pendingB string
	err = store.pool.QueryRow(ctx, `INSERT INTO journey_message_intents 
		(run_id, tenant_id, workspace_id, journey_id, journey_version_id, node_id, profile_id, template_id, channel, endpoint, transactional, status, available_at)
		VALUES ($1, $2, $3, $4, $5, 'node-b-pending', $6, $7, 'email', 'b-pending@example.com', true, 'pending', $8) RETURNING id`,
		runIDB, pB.TenantID, pB.WorkspaceID, journeyIDB, verIDB, profileIDB, templateIDB, now.Add(-5*time.Minute)).Scan(&pendingB)
	if err != nil {
		t.Fatal(err)
	}

	// Claim: Should skip pendingA and return pendingB because Tenant A has 10 in-flight!
	claimed3, found3, err := store.ClaimJourneyMessageIntent(ctx, "worker-fairness")
	if err != nil {
		t.Fatal(err)
	}
	if !found3 {
		t.Fatal("expected to claim a message intent under fairness check")
	}
	if claimed3.ID != pendingB {
		t.Errorf("fairness check failed: expected to claim pendingB (%s), got %s (pendingA was %s)", pendingB, claimed3.ID, pendingA)
	}
}

func ptrStr(s string) *string {
	return &s
}

