package postgres

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/buildwithdmytro/openjourney/internal/channels"
	"github.com/buildwithdmytro/openjourney/internal/domain"
	journeyflow "github.com/buildwithdmytro/openjourney/internal/journey"
	"github.com/buildwithdmytro/openjourney/internal/projector"
)

func makePendingStepDue(ctx context.Context, store *Store, runID string) error {
	_, err := store.pool.Exec(ctx, `
		UPDATE journey_steps
		SET available_at = now() - interval '5 seconds'
		WHERE run_id = $1 AND status = 'pending'
	`, runID)
	return err
}

func makeIntentsDue(ctx context.Context, store *Store, runID string) error {
	_, err := store.pool.Exec(ctx, `
		UPDATE journey_message_intents
		SET available_at = now() - interval '5 seconds'
		WHERE run_id = $1
	`, runID)
	return err
}

func TestJourneysFakeClockEndToEnd(t *testing.T) {
	databaseURL := os.Getenv("OPENJOURNEY_TEST_DATABASE_URL")
	if databaseURL == "" {
		t.Skip("OPENJOURNEY_TEST_DATABASE_URL is not configured")
	}
	ctx := context.Background()
	store, err := Open(ctx, databaseURL)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer store.Close()

	key := fmt.Sprintf("journey-e2e-%d", time.Now().UnixNano())
	if err := store.EnsureDevelopmentTenant(ctx, key); err != nil {
		t.Fatalf("EnsureDevelopmentTenant: %v", err)
	}
	p, err := store.Authenticate(ctx, key)
	if err != nil {
		t.Fatalf("Authenticate: %v", err)
	}
	appID, err := store.GetFirstAppID(ctx, p.TenantID, p.WorkspaceID)
	if err != nil {
		t.Fatalf("GetFirstAppID: %v", err)
	}
	p.AppID = appID

	// 1. Create a profile and project its attributes & consent
	events := []domain.Event{
		{
			Type: "profile.updated", SchemaVersion: 1, ExternalID: "cust-j-e2e-1",
			IdempotencyKey: "id-j-e2e-1", OccurredAt: time.Now().UTC(),
			Payload: json.RawMessage(`{"attributes":{"country":"US", "email":"e2e-j-rec@example.com"}}`),
		},
		{
			Type: "consent.changed", SchemaVersion: 1, ExternalID: "cust-j-e2e-1",
			IdempotencyKey: "consent-j-e2e-1", OccurredAt: time.Now().UTC(),
			Payload: json.RawMessage(`{"channel":"email","topic":"marketing","state":"subscribed"}`),
		},
	}
	_, err = store.AcceptEvents(ctx, p, events)
	if err != nil {
		t.Fatalf("accept events: %v", err)
	}

	// Project the events
	_, err = projector.Drain(ctx, store, 2, false)
	if err != nil {
		t.Fatalf("projector drain: %v", err)
	}

	// Get profile to verify creation and find the profile ID
	prof, _, err := store.GetProfile(ctx, p, "cust-j-e2e-1")
	if err != nil {
		t.Fatalf("get profile: %v", err)
	}

	// Create dependent sending identity and template
	iden, err := store.CreateSendingIdentity(ctx, p, domain.SendingIdentity{
		Channel:     "email",
		FromAddress: ptr("sender@example.com"),
		FromName:    ptr("Sender"),
		Provider:    "ses",
		MaxSendRate: 10,
	})
	if err != nil {
		t.Fatalf("create sending identity: %v", err)
	}

	htmlTmpl := "Hello {{ email }}!"
	tmpl, err := store.CreateTemplate(ctx, p, domain.Template{
		Name:              "E2E Journey Template",
		Channel:           "email",
		HTMLTemplate:      &htmlTmpl,
		SendingIdentityID: &iden.ID,
	})
	if err != nil {
		t.Fatalf("create template: %v", err)
	}

	// Setup our graph JSON
	graphJSON := fmt.Sprintf(`{
		"entry_node_id": "n1",
		"nodes": [
			{ "id": "n1", "type": "entry", "config": { "trigger": "event", "event_type": "signup.completed" } },
			{ "id": "n2", "type": "delay", "config": { "duration": "1h" } },
			{ "id": "n3", "type": "condition", "config": { "dsl": { "type": "profile_attribute", "field": "country", "operator": "equals", "value": "US" } } },
			{ "id": "n4", "type": "split", "config": { "mode": "random", "branches": [ { "label": "a", "weight": 100 } ] } },
			{ "id": "n5", "type": "message", "config": { "template_id": "%s", "transactional": true } },
			{ "id": "n6", "type": "wait_event", "config": { "event_type": "email.opened", "timeout": "2h" } },
			{ "id": "n7", "type": "goal", "config": { "name": "activated" } },
			{ "id": "n8", "type": "exit", "config": { "reason": "completed" } }
		],
		"edges": [
			{ "from": "n1", "to": "n2" },
			{ "from": "n2", "to": "n3" },
			{ "from": "n3", "to": "n4", "branch": "true" },
			{ "from": "n3", "to": "n8", "branch": "false" },
			{ "from": "n4", "to": "n5", "branch": "a" },
			{ "from": "n5", "to": "n6" },
			{ "from": "n6", "to": "n7", "branch": "timeout" },
			{ "from": "n6", "to": "n8", "branch": "success" },
			{ "from": "n7", "to": "n8" }
		]
	}`, tmpl.ID)

	// 2. Create the journey
	journey, err := store.CreateJourney(ctx, p, domain.Journey{
		Name:  "E2E Journey with Fake Clock",
		Graph: json.RawMessage(graphJSON),
	})
	if err != nil {
		t.Fatalf("create journey: %v", err)
	}

	// 3. Publish the journey (frozen into a version)
	blobs := &memoryBlobs{objects: map[string][]byte{}}
	approverID := "00000000-0000-0000-0000-000000000001"
	version, err := journeyflow.Publish(ctx, store, blobs, p, journey.ID, approverID)
	if err != nil {
		t.Fatalf("publish journey: %v", err)
	}

	// 4. Enroll participant by accepting a triggering event
	startTime := time.Now().UTC().Truncate(time.Second)
	clk := journeyflow.NewFakeClock(startTime)
	deps := journeyflow.Deps{Clock: clk}

	enrollEvents := []domain.Event{
		{
			Type: "signup.completed", SchemaVersion: 1, ExternalID: "cust-j-e2e-1",
			IdempotencyKey: "trigger-enroll-1", OccurredAt: startTime,
			Payload: json.RawMessage(`{}`),
		},
	}
	_, err = store.AcceptEvents(ctx, p, enrollEvents)
	if err != nil {
		t.Fatalf("accept enroll events: %v", err)
	}

	// Project trigger event (runs ProjectEvent which matches signup.completed and enrolls)
	_, err = projector.Drain(ctx, store, 1, false)
	if err != nil {
		t.Fatalf("projector drain for enroll: %v", err)
	}

	// Retrieve run to assert enrollment
	runs, err := store.GetJourneyRunsForProfile(ctx, p.TenantID, version.ID, prof.ID)
	if err != nil {
		t.Fatalf("get journey runs: %v", err)
	}
	if len(runs) != 1 {
		t.Fatalf("expected exactly 1 run, got %d", len(runs))
	}
	run := runs[0]
	if run.CurrentNodeID != "n1" || run.Status != "active" {
		t.Fatalf("expected run to be active on node n1, got status %q node %q", run.Status, run.CurrentNodeID)
	}

	// Make sure the step is ready in DB
	if err := makePendingStepDue(ctx, store, run.ID); err != nil {
		t.Fatalf("makePendingStepDue: %v", err)
	}

	// --- STEP 1: Process entry node (n1) ---
	processed, err := journeyflow.TickNext(ctx, store, deps)
	if err != nil {
		t.Fatalf("TickNext entry (n1): %v", err)
	}
	if !processed {
		t.Fatalf("expected n1 to be processed")
	}

	// Run should now be active on n2 (delay)
	run, _ = store.GetJourneyRun(ctx, p, run.ID)
	if run.CurrentNodeID != "n2" {
		t.Fatalf("expected run to be on node n2, got %q", run.CurrentNodeID)
	}

	// Make sure the step is ready in DB
	if err := makePendingStepDue(ctx, store, run.ID); err != nil {
		t.Fatalf("makePendingStepDue: %v", err)
	}

	// --- STEP 2: Process delay node (n2) ---
	processed, err = journeyflow.TickNext(ctx, store, deps)
	if err != nil {
		t.Fatalf("TickNext delay (n2): %v", err)
	}
	if !processed {
		t.Fatalf("expected n2 to be processed")
	}

	// Node executes delay, so successor (n3 condition) is scheduled at now + 1h
	run, _ = store.GetJourneyRun(ctx, p, run.ID)
	if run.CurrentNodeID != "n3" {
		t.Fatalf("expected run to be on node n3, got %q", run.CurrentNodeID)
	}

	// Try to tick: nothing should process since n3 step is in the future (available_at = startTime + 1h)
	processed, err = journeyflow.TickNext(ctx, store, deps)
	if err != nil {
		t.Fatalf("TickNext future: %v", err)
	}
	if processed {
		t.Fatalf("expected no steps to process as n3 is in the future")
	}

	// Update n3's available_at in database to make it due relative to database now()
	if err := makePendingStepDue(ctx, store, run.ID); err != nil {
		t.Fatalf("makePendingStepDue: %v", err)
	}

	// Advance clock by 1 hour (now = startTime + 1h)
	clk.Advance(1 * time.Hour)

	// --- STEP 3: Process condition (n3) ---
	processed, err = journeyflow.TickNext(ctx, store, deps)
	if err != nil {
		t.Fatalf("TickNext condition (n3): %v", err)
	}
	if !processed {
		t.Fatalf("expected n3 to be processed")
	}

	// Our profile has country=US, so branch true -> goes to split (n4)
	run, _ = store.GetJourneyRun(ctx, p, run.ID)
	if run.CurrentNodeID != "n4" {
		t.Fatalf("expected run to be on node n4, got %q", run.CurrentNodeID)
	}

	// Make sure the step is ready in DB
	if err := makePendingStepDue(ctx, store, run.ID); err != nil {
		t.Fatalf("makePendingStepDue: %v", err)
	}

	// --- STEP 4: Process split (n4) ---
	processed, err = journeyflow.TickNext(ctx, store, deps)
	if err != nil {
		t.Fatalf("TickNext split (n4): %v", err)
	}
	if !processed {
		t.Fatalf("expected n4 to be processed")
	}

	// Random split with 100% weight branch a -> goes to message (n5)
	run, _ = store.GetJourneyRun(ctx, p, run.ID)
	if run.CurrentNodeID != "n5" {
		t.Fatalf("expected run to be on node n5, got %q", run.CurrentNodeID)
	}

	// Make sure the step is ready in DB
	if err := makePendingStepDue(ctx, store, run.ID); err != nil {
		t.Fatalf("makePendingStepDue: %v", err)
	}

	// --- STEP 5: Process message (n5) ---
	processed, err = journeyflow.TickNext(ctx, store, deps)
	if err != nil {
		t.Fatalf("TickNext message (n5): %v", err)
	}
	if !processed {
		t.Fatalf("expected n5 to be processed")
	}

	// Message executes immediately, schedules wait_event (n6)
	run, _ = store.GetJourneyRun(ctx, p, run.ID)
	if run.CurrentNodeID != "n6" {
		t.Fatalf("expected run to be on node n6, got %q", run.CurrentNodeID)
	}

	// Make sure the step is ready in DB
	if err := makePendingStepDue(ctx, store, run.ID); err != nil {
		t.Fatalf("makePendingStepDue: %v", err)
	}

	// --- STEP 6: Process wait_event (n6) ---
	processed, err = journeyflow.TickNext(ctx, store, deps)
	if err != nil {
		t.Fatalf("TickNext wait_event (n6): %v", err)
	}
	if !processed {
		t.Fatalf("expected n6 to be processed")
	}

	// Wait event parks: run becomes "waiting", next timeout step is at now + 2h
	run, _ = store.GetJourneyRun(ctx, p, run.ID)
	if run.Status != "waiting" {
		t.Fatalf("expected run status 'waiting', got %q", run.Status)
	}

	// Try ticking: nothing should process
	processed, err = journeyflow.TickNext(ctx, store, deps)
	if err != nil {
		t.Fatalf("TickNext waiting: %v", err)
	}
	if processed {
		t.Fatalf("expected no steps to process as timeout is in the future")
	}

	// Update timeout step's available_at in database to make it due relative to database now()
	if err := makePendingStepDue(ctx, store, run.ID); err != nil {
		t.Fatalf("makePendingStepDue: %v", err)
	}

	// Advance clock by 2 hours (now = startTime + 3h)
	clk.Advance(2 * time.Hour)

	// --- STEP 7: Process wait timeout ---
	processed, err = journeyflow.TickNext(ctx, store, deps)
	if err != nil {
		t.Fatalf("TickNext timeout (n6): %v", err)
	}
	if !processed {
		t.Fatalf("expected timeout to be processed")
	}

	// Timeout advances down branch "timeout" -> goes to goal (n7)
	run, _ = store.GetJourneyRun(ctx, p, run.ID)
	if run.CurrentNodeID != "n7" || run.Status != "active" {
		t.Fatalf("expected run to be active on node n7, got status %q node %q", run.Status, run.CurrentNodeID)
	}

	// Make sure the step is ready in DB
	if err := makePendingStepDue(ctx, store, run.ID); err != nil {
		t.Fatalf("makePendingStepDue: %v", err)
	}

	// --- STEP 8: Process goal (n7) ---
	processed, err = journeyflow.TickNext(ctx, store, deps)
	if err != nil {
		t.Fatalf("TickNext goal (n7): %v", err)
	}
	if !processed {
		t.Fatalf("expected n7 to be processed")
	}

	// Goal reached should be true, transitions to exit (n8)
	run, _ = store.GetJourneyRun(ctx, p, run.ID)
	if !run.GoalReached {
		t.Errorf("expected goal_reached to be true")
	}
	if run.CurrentNodeID != "n8" {
		t.Fatalf("expected run to be on node n8, got %q", run.CurrentNodeID)
	}

	// Make sure the step is ready in DB
	if err := makePendingStepDue(ctx, store, run.ID); err != nil {
		t.Fatalf("makePendingStepDue: %v", err)
	}

	// --- STEP 9: Process exit (n8) ---
	processed, err = journeyflow.TickNext(ctx, store, deps)
	if err != nil {
		t.Fatalf("TickNext exit (n8): %v", err)
	}
	if !processed {
		t.Fatalf("expected n8 to be processed")
	}

	// Run status should be completed
	run, _ = store.GetJourneyRun(ctx, p, run.ID)
	if run.Status != "completed" {
		t.Errorf("expected run status 'completed', got %q", run.Status)
	}

	// --- ASSERTIONS ON TRANSITIONS ---
	var count int
	err = store.pool.QueryRow(ctx, `SELECT COUNT(*) FROM journey_transitions WHERE run_id = $1`, run.ID).Scan(&count)
	if err != nil {
		t.Fatalf("query transitions count: %v", err)
	}
	if count < 8 {
		t.Errorf("expected at least 8 transitions, got %d", count)
	}

	// --- ASSERTIONS ON INTENT AND DELIVERY ---
	// Assert exactly one row was added to journey_message_intents
	var intentCount int
	err = store.pool.QueryRow(ctx, `SELECT COUNT(*) FROM journey_message_intents WHERE run_id = $1`, run.ID).Scan(&intentCount)
	if err != nil {
		t.Fatalf("query intent count: %v", err)
	}
	if intentCount != 1 {
		t.Fatalf("expected exactly 1 message intent, got %d", intentCount)
	}

	// Make sure the message intent is ready in DB
	if err := makeIntentsDue(ctx, store, run.ID); err != nil {
		t.Fatalf("makeIntentsDue: %v", err)
	}

	// Run DeliverNext (e2e delivery)
	fakeAdapter := channels.NewFakeAdapter()
	deliveryCfg := journeyflow.Config{
		TrackingSecretKey: []byte("tracking-secret-key-12345"),
		TrackingBaseURL:   "http://localhost:8080",
		Adapter:           fakeAdapter,
		FakeAdapter:       fakeAdapter,
		Clock:             clk,
	}

	delivered, err := journeyflow.DeliverNext(ctx, store, "worker-1", deliveryCfg)
	if err != nil {
		t.Fatalf("DeliverNext: %v", err)
	}
	if !delivered {
		t.Fatal("expected to deliver a message intent")
	}

	// Verify intent is completed
	var intentStatus, decision string
	err = store.pool.QueryRow(ctx, `SELECT status, decision FROM journey_message_intents WHERE run_id = $1`, run.ID).Scan(&intentStatus, &decision)
	if err != nil {
		t.Fatalf("query intent status: %v", err)
	}
	if intentStatus != "completed" {
		t.Errorf("expected intent status 'completed', got %q", intentStatus)
	}
	if decision != "sent" {
		t.Errorf("expected intent decision 'sent', got %q", decision)
	}

	// Verify message.sent event was emitted (should be in accepted_events)
	var eventCount int
	err = store.pool.QueryRow(ctx, `SELECT COUNT(*) FROM accepted_events WHERE tenant_id = $1 AND event_type = 'message.sent'`, p.TenantID).Scan(&eventCount)
	if err != nil {
		t.Fatalf("query accepted_events count: %v", err)
	}
	if eventCount != 1 {
		t.Errorf("expected exactly 1 message.sent event, got %d", eventCount)
	}
}
