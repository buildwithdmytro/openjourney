package postgres

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
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

type transitionIdentity struct {
	FromNode string `json:"from_node"`
	ToNode   string `json:"to_node"`
	NodeType string `json:"node_type"`
	Outcome  string `json:"outcome"`
}

func getRunTransitions(ctx context.Context, store *Store, runID string) ([]transitionIdentity, error) {
	rows, err := store.pool.Query(ctx, `
		SELECT COALESCE(from_node, ''), COALESCE(to_node, ''), node_type, outcome
		FROM journey_transitions
		WHERE run_id = $1
		ORDER BY occurred_at ASC, id ASC
	`, runID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var res []transitionIdentity
	for rows.Next() {
		var t transitionIdentity
		if err := rows.Scan(&t.FromNode, &t.ToNode, &t.NodeType, &t.Outcome); err != nil {
			return nil, err
		}
		res = append(res, t)
	}
	return res, nil
}

func computeTransitionsHash(transitions []transitionIdentity) (string, error) {
	data, err := json.Marshal(transitions)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:]), nil
}

func runReplay(ctx context.Context, store *Store, versionID string, p domain.Principal, extID string) (string, error) {
	// Create profile
	events := []domain.Event{
		{
			Type: "profile.updated", SchemaVersion: 1, ExternalID: extID,
			IdempotencyKey: "rep-id-" + extID, OccurredAt: time.Now().UTC(),
			Payload: json.RawMessage(`{"attributes":{"country":"CA", "email":"` + extID + `@example.com"}}`),
		},
		{
			Type: "consent.changed", SchemaVersion: 1, ExternalID: extID,
			IdempotencyKey: "rep-consent-" + extID, OccurredAt: time.Now().UTC(),
			Payload: json.RawMessage(`{"channel":"email","topic":"marketing","state":"subscribed"}`),
		},
	}
	_, err := store.AcceptEvents(ctx, p, events)
	if err != nil {
		return "", fmt.Errorf("accept events: %w", err)
	}
	_, err = projector.Drain(ctx, store, 2, false)
	if err != nil {
		return "", fmt.Errorf("projector drain: %w", err)
	}

	prof, _, err := store.GetProfile(ctx, p, extID)
	if err != nil {
		return "", fmt.Errorf("get profile: %w", err)
	}

	// Enroll participant
	enrollEvents := []domain.Event{
		{
			Type: "user.signup", SchemaVersion: 1, ExternalID: extID,
			IdempotencyKey: "rep-enroll-" + extID, OccurredAt: time.Now().UTC(),
			Payload: json.RawMessage(`{}`),
		},
	}
	_, err = store.AcceptEvents(ctx, p, enrollEvents)
	if err != nil {
		return "", fmt.Errorf("accept enroll: %w", err)
	}
	_, err = projector.Drain(ctx, store, 1, false)
	if err != nil {
		return "", fmt.Errorf("projector drain enroll: %w", err)
	}

	runs, err := store.GetJourneyRunsForProfile(ctx, p.TenantID, versionID, prof.ID)
	if err != nil {
		return "", fmt.Errorf("get runs: %w", err)
	}
	if len(runs) != 1 {
		return "", fmt.Errorf("expected 1 run, got %d", len(runs))
	}
	run := runs[0]

	clk := journeyflow.NewFakeClock(time.Now().UTC())
	deps := journeyflow.Deps{Clock: clk}

	// Step 1: entry (n1)
	if err := makePendingStepDue(ctx, store, run.ID); err != nil {
		return "", err
	}
	processed, err := journeyflow.TickNext(ctx, store, deps)
	if err != nil {
		return "", fmt.Errorf("TickNext entry (n1): %w", err)
	}
	if !processed {
		return "", fmt.Errorf("expected n1 to be processed")
	}

	// Step 2: delay (n2)
	if err := makePendingStepDue(ctx, store, run.ID); err != nil {
		return "", err
	}
	processed, err = journeyflow.TickNext(ctx, store, deps)
	if err != nil {
		return "", fmt.Errorf("TickNext delay (n2): %w", err)
	}
	if !processed {
		return "", fmt.Errorf("expected n2 to be processed")
	}

	// Step 3: condition (n3) (advance clock by 1h)
	clk.Advance(1 * time.Hour)
	if err := makePendingStepDue(ctx, store, run.ID); err != nil {
		return "", err
	}
	processed, err = journeyflow.TickNext(ctx, store, deps)
	if err != nil {
		return "", fmt.Errorf("TickNext condition (n3): %w", err)
	}
	if !processed {
		return "", fmt.Errorf("expected n3 to be processed")
	}

	// Step 4: message (n4)
	if err := makePendingStepDue(ctx, store, run.ID); err != nil {
		return "", err
	}
	processed, err = journeyflow.TickNext(ctx, store, deps)
	if err != nil {
		return "", fmt.Errorf("TickNext message (n4): %w", err)
	}
	if !processed {
		return "", fmt.Errorf("expected n4 to be processed")
	}

	// Step 5: exit (n5)
	if err := makePendingStepDue(ctx, store, run.ID); err != nil {
		return "", err
	}
	processed, err = journeyflow.TickNext(ctx, store, deps)
	if err != nil {
		return "", fmt.Errorf("TickNext exit (n5): %w", err)
	}
	if !processed {
		return "", fmt.Errorf("expected n5 to be processed")
	}

	return run.ID, nil
}

func TestJourneysReplayCompatibility(t *testing.T) {
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

	key := fmt.Sprintf("journey-rep-%d", time.Now().UnixNano())
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

	// Create sending identity and template
	iden, err := store.CreateSendingIdentity(ctx, p, domain.SendingIdentity{
		Channel:     "email",
		FromName:    ptr("Sender"),
		FromAddress: ptr("sender@example.com"),
		Provider:    "ses",
		MaxSendRate: 10,
	})
	if err != nil {
		t.Fatalf("create identity: %v", err)
	}

	htmlTmpl := "Hello {{ email }}!"
	tmpl, err := store.CreateTemplate(ctx, p, domain.Template{
		Name:              "Rep Journey Template",
		Channel:           "email",
		HTMLTemplate:      &htmlTmpl,
		SendingIdentityID: &iden.ID,
	})
	if err != nil {
		t.Fatalf("create template: %v", err)
	}

	// Setup a simple condition -> message graph
	graphJSON := fmt.Sprintf(`{
		"entry_node_id": "n1",
		"nodes": [
			{ "id": "n1", "type": "entry", "config": { "trigger": "event", "event_type": "user.signup" } },
			{ "id": "n2", "type": "delay", "config": { "duration": "1h" } },
			{ "id": "n3", "type": "condition", "config": { "dsl": { "type": "profile_attribute", "field": "country", "operator": "equals", "value": "CA" } } },
			{ "id": "n4", "type": "message", "config": { "template_id": "%s", "transactional": true } },
			{ "id": "n5", "type": "exit", "config": { "reason": "completed" } }
		],
		"edges": [
			{ "from": "n1", "to": "n2" },
			{ "from": "n2", "to": "n3" },
			{ "from": "n3", "to": "n4", "branch": "true" },
			{ "from": "n3", "to": "n5", "branch": "false" },
			{ "from": "n4", "to": "n5" }
		]
	}`, tmpl.ID)

	// Create journey and publish it
	journey, err := store.CreateJourney(ctx, p, domain.Journey{
		Name:  "Replay Compatibility Journey",
		Graph: json.RawMessage(graphJSON),
	})
	if err != nil {
		t.Fatalf("create journey: %v", err)
	}

	blobs := &memoryBlobs{objects: map[string][]byte{}}
	approverID := "00000000-0000-0000-0000-000000000001"
	version, err := journeyflow.Publish(ctx, store, blobs, p, journey.ID, approverID)
	if err != nil {
		t.Fatalf("publish: %v", err)
	}

	// Replay 1
	runID1, err := runReplay(ctx, store, version.ID, p, "cust-rep-1")
	if err != nil {
		t.Fatalf("replay 1 failed: %v", err)
	}

	// Replay 2 (with identical inputs / setup)
	runID2, err := runReplay(ctx, store, version.ID, p, "cust-rep-2")
	if err != nil {
		t.Fatalf("replay 2 failed: %v", err)
	}

	// Retrieve transitions
	transitions1, err := getRunTransitions(ctx, store, runID1)
	if err != nil {
		t.Fatalf("get transitions 1: %v", err)
	}
	transitions2, err := getRunTransitions(ctx, store, runID2)
	if err != nil {
		t.Fatalf("get transitions 2: %v", err)
	}

	// Ensure we have the correct number of transitions
	if len(transitions1) < 5 {
		t.Errorf("expected at least 5 transitions in replay, got %d", len(transitions1))
	}

	// Compute hashes
	hash1, err := computeTransitionsHash(transitions1)
	if err != nil {
		t.Fatalf("hash 1: %v", err)
	}
	hash2, err := computeTransitionsHash(transitions2)
	if err != nil {
		t.Fatalf("hash 2: %v", err)
	}

	t.Logf("Replay 1 Transitions Hash: %s", hash1)
	t.Logf("Replay 2 Transitions Hash: %s", hash2)

	if hash1 != hash2 {
		t.Errorf("expected transition hashes to be identical, got run1=%s and run2=%s", hash1, hash2)
	} else {
		t.Log("SUCCESS: Two replays of the same version + identical inputs produced byte-identical transition sequences.")
	}
}

func TestJourneysDeterminism(t *testing.T) {
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

	key := fmt.Sprintf("journey-det-%d", time.Now().UnixNano())
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

	// Create profile
	profileExtID := "profile-det-test"
	events := []domain.Event{
		{
			Type: "profile.updated", SchemaVersion: 1, ExternalID: profileExtID,
			IdempotencyKey: "identify-det-test", OccurredAt: time.Now().UTC(),
			Payload: json.RawMessage(`{"attributes":{"country":"US"}}`),
		},
	}
	_, err = store.AcceptEvents(ctx, p, events)
	if err != nil {
		t.Fatalf("accept profile: %v", err)
	}
	_, err = projector.Drain(ctx, store, 1, false)
	if err != nil {
		t.Fatalf("projector drain: %v", err)
	}

	profile, _, err := store.GetProfile(ctx, p, profileExtID)
	if err != nil {
		t.Fatalf("get profile: %v", err)
	}

	// Setup a wait_event journey: entry -> wait_event -> completed / timeout
	graphJSON := `{
		"entry_node_id": "n1",
		"nodes": [
			{ "id": "n1", "type": "entry", "config": { "trigger": "event", "event_type": "user.signup" } },
			{ "id": "n2", "type": "wait_event", "config": { "event_type": "email.opened", "timeout": "1h" } },
			{ "id": "n3", "type": "exit", "config": { "reason": "completed" } },
			{ "id": "n4", "type": "exit", "config": { "reason": "timed_out" } }
		],
		"edges": [
			{ "from": "n1", "to": "n2" },
			{ "from": "n2", "to": "n3", "branch": "success" },
			{ "from": "n2", "to": "n4", "branch": "timeout" }
		]
	}`

	journey, err := store.CreateJourney(ctx, p, domain.Journey{
		Name:  "Determinism Journey",
		Graph: json.RawMessage(graphJSON),
	})
	if err != nil {
		t.Fatalf("create journey: %v", err)
	}

	blobs := &memoryBlobs{objects: map[string][]byte{}}
	approverID := "00000000-0000-0000-0000-000000000001"
	version, err := journeyflow.Publish(ctx, store, blobs, p, journey.ID, approverID)
	if err != nil {
		t.Fatalf("publish: %v", err)
	}

	clk := journeyflow.NewFakeClock(time.Now().UTC())
	deps := journeyflow.Deps{Clock: clk}

	// ==========================================
	// CASE 1: Duplicate entry event
	// ==========================================
	triggerEvent := []domain.Event{
		{
			Type: "user.signup", SchemaVersion: 1, ExternalID: profileExtID,
			IdempotencyKey: "signup-1", OccurredAt: time.Now().UTC(),
			Payload: json.RawMessage(`{}`),
		},
	}
	_, err = store.AcceptEvents(ctx, p, triggerEvent)
	if err != nil {
		t.Fatalf("accept signup: %v", err)
	}
	_, err = projector.Drain(ctx, store, 1, false)
	if err != nil {
		t.Fatalf("projector drain signup: %v", err)
	}

	runs, err := store.GetJourneyRunsForProfile(ctx, p.TenantID, version.ID, profile.ID)
	if err != nil {
		t.Fatalf("get runs: %v", err)
	}
	if len(runs) != 1 {
		t.Fatalf("expected exactly 1 run after first signup event, got %d", len(runs))
	}
	runID := runs[0].ID

	// Ingest exact duplicate signup event
	_, err = store.AcceptEvents(ctx, p, triggerEvent)
	if err != nil {
		t.Fatalf("accept duplicate signup: %v", err)
	}
	// Run projector and verify no duplicate run was created
	_, err = projector.Drain(ctx, store, 1, false)
	if err != nil {
		t.Fatalf("projector drain duplicate signup: %v", err)
	}
	runs, _ = store.GetJourneyRunsForProfile(ctx, p.TenantID, version.ID, profile.ID)
	if len(runs) != 1 {
		t.Errorf("expected exactly 1 run after duplicate signup event, got %d", len(runs))
	}

	// ==========================================
	// CASE 2: Reordered / late event
	// ==========================================
	// Prior to run entering the wait_event node (it's currently at n1), we receive and project
	// the wait_event's trigger event 'email.opened'
	lateEvent := []domain.Event{
		{
			Type: "email.opened", SchemaVersion: 1, ExternalID: profileExtID,
			IdempotencyKey: "opened-late", OccurredAt: time.Now().UTC(),
			Payload: json.RawMessage(`{}`),
		},
	}
	_, err = store.AcceptEvents(ctx, p, lateEvent)
	if err != nil {
		t.Fatalf("accept late event: %v", err)
	}
	_, err = projector.Drain(ctx, store, 1, false)
	if err != nil {
		t.Fatalf("projector drain late event: %v", err)
	}

	// Verify run state: should still be at n1 and not prematurely transitioned
	run, _ := store.GetJourneyRun(ctx, p, runID)
	if run.CurrentNodeID != "n1" || run.Status != "active" {
		t.Errorf("expected run to remain at n1 on late event, but got node=%s, status=%s", run.CurrentNodeID, run.Status)
	}

	// Now transition run to n2 (wait_event)
	if err := makePendingStepDue(ctx, store, runID); err != nil {
		t.Fatalf("makePendingStepDue: %v", err)
	}
	processed, err := journeyflow.TickNext(ctx, store, deps)
	if err != nil || !processed {
		t.Fatalf("TickNext entry (n1): processed=%v, err=%v", processed, err)
	}

	// Execute wait_event (n2) advance step to park the run in 'waiting' status
	if err := makePendingStepDue(ctx, store, runID); err != nil {
		t.Fatalf("makePendingStepDue: %v", err)
	}
	processed, err = journeyflow.TickNext(ctx, store, deps)
	if err != nil || !processed {
		t.Fatalf("TickNext wait_event (n2) advance: processed=%v, err=%v", processed, err)
	}

	// Run should now be waiting at n2
	run, _ = store.GetJourneyRun(ctx, p, runID)
	if run.CurrentNodeID != "n2" || run.Status != "waiting" {
		t.Errorf("expected run to be waiting at n2, got node=%s, status=%s", run.CurrentNodeID, run.Status)
	}

	// Project another email.opened event (valid event now that we are waiting)
	validOpenedEvent := []domain.Event{
		{
			Type: "email.opened", SchemaVersion: 1, ExternalID: profileExtID,
			IdempotencyKey: "opened-valid", OccurredAt: time.Now().UTC(),
			Payload: json.RawMessage(`{}`),
		},
	}
	_, err = store.AcceptEvents(ctx, p, validOpenedEvent)
	if err != nil {
		t.Fatalf("accept valid event: %v", err)
	}
	_, err = projector.Drain(ctx, store, 1, false)
	if err != nil {
		t.Fatalf("projector drain valid event: %v", err)
	}

	// Run should now be resolved to success branch (n3)
	run, _ = store.GetJourneyRun(ctx, p, runID)
	if run.CurrentNodeID != "n3" || run.Status != "active" {
		t.Errorf("expected run to transition to n3, got node=%s, status=%s", run.CurrentNodeID, run.Status)
	}

	// Complete the run by transitioning exit node (n3)
	if err := makePendingStepDue(ctx, store, runID); err != nil {
		t.Fatalf("makePendingStepDue: %v", err)
	}
	processed, err = journeyflow.TickNext(ctx, store, deps)
	if err != nil || !processed {
		t.Fatalf("TickNext exit (n3): processed=%v, err=%v", processed, err)
	}
	run, _ = store.GetJourneyRun(ctx, p, runID)
	if run.Status != "completed" {
		t.Errorf("expected run to be completed, got status=%s", run.Status)
	}

	// ==========================================
	// CASE 3: Awaited event + timeout racing
	// ==========================================
	// Let's enroll a brand new participant to test the race condition
	profileExtID2 := "profile-det-test-2"
	events2 := []domain.Event{
		{
			Type: "profile.updated", SchemaVersion: 1, ExternalID: profileExtID2,
			IdempotencyKey: "identify-det-test-2", OccurredAt: time.Now().UTC(),
			Payload: json.RawMessage(`{"attributes":{"country":"US"}}`),
		},
	}
	_, err = store.AcceptEvents(ctx, p, events2)
	if err != nil {
		t.Fatalf("accept profile 2: %v", err)
	}
	_, err = projector.Drain(ctx, store, 1, false)
	if err != nil {
		t.Fatalf("projector drain profile 2: %v", err)
	}

	profile2, _, _ := store.GetProfile(ctx, p, profileExtID2)

	triggerEvent2 := []domain.Event{
		{
			Type: "user.signup", SchemaVersion: 1, ExternalID: profileExtID2,
			IdempotencyKey: "signup-2", OccurredAt: time.Now().UTC(),
			Payload: json.RawMessage(`{}`),
		},
	}
	_, err = store.AcceptEvents(ctx, p, triggerEvent2)
	if err != nil {
		t.Fatalf("accept signup 2: %v", err)
	}
	_, err = projector.Drain(ctx, store, 1, false)
	if err != nil {
		t.Fatalf("projector drain signup 2: %v", err)
	}

	runs2, _ := store.GetJourneyRunsForProfile(ctx, p.TenantID, version.ID, profile2.ID)
	runID2 := runs2[0].ID

	// Advance entry node (n1)
	if err := makePendingStepDue(ctx, store, runID2); err != nil {
		t.Fatalf("makePendingStepDue: %v", err)
	}
	processed, err = journeyflow.TickNext(ctx, store, deps)
	if err != nil || !processed {
		t.Fatalf("TickNext entry (n1): processed=%v, err=%v", processed, err)
	}

	// Execute wait_event (n2) advance step to park the run in 'waiting' status
	if err := makePendingStepDue(ctx, store, runID2); err != nil {
		t.Fatalf("makePendingStepDue: %v", err)
	}
	processed, err = journeyflow.TickNext(ctx, store, deps)
	if err != nil || !processed {
		t.Fatalf("TickNext wait_event (n2) advance: processed=%v, err=%v", processed, err)
	}

	// Run 2 is now waiting at n2 (wait_event)
	run2, _ := store.GetJourneyRun(ctx, p, runID2)
	if run2.CurrentNodeID != "n2" || run2.Status != "waiting" {
		t.Fatalf("expected run 2 to be waiting at n2, got node=%s, status=%s", run2.CurrentNodeID, run2.Status)
	}

	// Simulating Race Case: Event resolves first, then timeout triggers.
	// 1. We ingest and project 'email.opened' event to resolve the wait_event.
	openedEvent2 := []domain.Event{
		{
			Type: "email.opened", SchemaVersion: 1, ExternalID: profileExtID2,
			IdempotencyKey: "opened-valid-2", OccurredAt: time.Now().UTC(),
			Payload: json.RawMessage(`{}`),
		},
	}
	_, err = store.AcceptEvents(ctx, p, openedEvent2)
	if err != nil {
		t.Fatalf("accept opened event 2: %v", err)
	}
	_, err = projector.Drain(ctx, store, 1, false)
	if err != nil {
		t.Fatalf("projector drain opened event 2: %v", err)
	}

	// Verify run 2 was successfully updated to active success branch (n3)
	run2, _ = store.GetJourneyRun(ctx, p, runID2)
	if run2.CurrentNodeID != "n3" || run2.Status != "active" {
		t.Errorf("expected run 2 to transition to n3 on event, got node=%s, status=%s", run2.CurrentNodeID, run2.Status)
	}

	// Process the exit node (n3) first so that it is complete and the run becomes "completed"
	if err := makePendingStepDue(ctx, store, runID2); err != nil {
		t.Fatalf("makePendingStepDue: %v", err)
	}
	processed, err = journeyflow.TickNext(ctx, store, deps)
	if err != nil || !processed {
		t.Fatalf("TickNext exit (n3): processed=%v, err=%v", processed, err)
	}

	// Verify run is completed
	run2, _ = store.GetJourneyRun(ctx, p, runID2)
	if run2.Status != "completed" {
		t.Fatalf("expected run 2 to be completed, got status=%s", run2.Status)
	}

	// 2. Now simulate the worker trying to execute the stale timeout step (which is scheduled for 1h in the future).
	// We make it due and advance the clock.
	clk.Advance(2 * time.Hour)
	// Query and find any pending steps for runID2 of kind timeout.
	var timeoutStepID string
	err = store.pool.QueryRow(ctx, `SELECT id FROM journey_steps WHERE run_id = $1 AND kind = 'timeout'`, runID2).Scan(&timeoutStepID)
	if err != nil {
		t.Fatalf("query timeout step: %v", err)
	}

	// Mark the timeout step as due directly
	_, err = store.pool.Exec(ctx, `UPDATE journey_steps SET available_at = now() - interval '5 seconds' WHERE id = $1`, timeoutStepID)
	if err != nil {
		t.Fatalf("make timeout step due: %v", err)
	}

	// Call TickNext to let the runtime handle the timeout step
	processed, err = journeyflow.TickNext(ctx, store, deps)
	if err != nil {
		t.Fatalf("TickNext timeout: %v", err)
	}

	// Verify run state did NOT change and is still completed/n3
	run2, _ = store.GetJourneyRun(ctx, p, runID2)
	if run2.CurrentNodeID != "n3" || run2.Status != "completed" {
		t.Errorf("RACE FAILED: timeout execution modified completed run state, got node=%s, status=%s", run2.CurrentNodeID, run2.Status)
	}
	t.Log("SUCCESS: Timeout racing on already resolved wait_event node was safely and deterministically ignored.")
}


