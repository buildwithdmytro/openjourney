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

func ptrStr(s string) *string {
	return &s
}
