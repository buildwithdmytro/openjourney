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
	"github.com/buildwithdmytro/openjourney/internal/operations"
	"github.com/buildwithdmytro/openjourney/internal/scoring"
)

func TestSegmentsResolution(t *testing.T) {
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

	p, tenantID := setupTestTenant(t, ctx, store)

	p1ID := testUUID(tenantID + "-p-1")
	p2ID := testUUID(tenantID + "-p-2")

	_, err = store.pool.Exec(ctx, `INSERT INTO profiles(id, tenant_id, workspace_id, app_id, external_id, attributes)
		VALUES($4, $1, $2, $3, 'ext-1', '{"country":"US","age":25}'),
		      ($5, $1, $2, $3, 'ext-2', '{"country":"CA","age":30}')`, tenantID, p.WorkspaceID, p.AppID, p1ID, p2ID)
	if err != nil {
		t.Fatalf("insert profiles: %v", err)
	}

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

	count, perLeg, err := store.PreviewSegment(ctx, p, seg.ID)
	if err != nil {
		t.Fatalf("preview segment: %v", err)
	}
	if count != 1 {
		t.Errorf("expected count 1, got %d", count)
	}
	if perLeg["profile_attributes"] != 1 {
		t.Errorf("expected profile attributes count 1, got %v", perLeg)
	}

	ids, err := store.ResolveSegment(ctx, p, seg.ID)
	if err != nil {
		t.Fatalf("resolve segment: %v", err)
	}
	if len(ids) != 1 || ids[0] != p1ID {
		t.Errorf("expected [%q], got %v", p1ID, ids)
	}

	_, _ = store.pool.Exec(ctx, "DELETE FROM tenants WHERE id=$1", tenantID)
}

func TestScoreSegmentDrivesScheduledJourney(t *testing.T) {
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

	p, tenantID := setupTestTenant(t, ctx, store)
	p1ID := testUUID(tenantID + "-score-p-1")
	p2ID := testUUID(tenantID + "-score-p-2")
	_, err = store.pool.Exec(ctx, `INSERT INTO profiles(id, tenant_id, workspace_id, app_id, external_id)
		VALUES($4, $1, $2, $3, 'score-ext-1'), ($5, $1, $2, $3, 'score-ext-2')`,
		tenantID, p.WorkspaceID, p.AppID, p1ID, p2ID)
	if err != nil {
		t.Fatalf("insert profiles: %v", err)
	}

	model, err := store.CreateScoringModel(ctx, p, domain.ScoringModel{Name: "Purchase propensity", Kind: "expression"})
	if err != nil {
		t.Fatalf("create scoring model: %v", err)
	}
	_, err = store.pool.Exec(ctx, `INSERT INTO profile_scores
		(tenant_id, workspace_id, app_id, profile_id, scoring_model_id, score_name, value, model_version)
		VALUES($1, $2, $3, $4, $5, 'purchase_propensity', 0.91, 1),
		      ($1, $2, $3, $6, $5, 'purchase_propensity', 0.12, 1)`,
		tenantID, p.WorkspaceID, p.AppID, p1ID, model.ID, p2ID)
	if err != nil {
		t.Fatalf("insert profile scores: %v", err)
	}

	seg, err := store.CreateSegment(ctx, p, domain.Segment{
		Name: "High propensity",
		DSL:  json.RawMessage(fmt.Sprintf(`{"type":"score","model":"%s","score_name":"purchase_propensity","operator":"greater_than","value":0.8}`, model.ID)),
	})
	if err != nil {
		t.Fatalf("create score segment: %v", err)
	}

	ids, err := store.ResolveSegment(ctx, p, seg.ID)
	if err != nil {
		t.Fatalf("resolve score segment: %v", err)
	}
	if len(ids) != 1 || ids[0] != p1ID {
		t.Fatalf("expected only high-scoring profile %s, got %v", p1ID, ids)
	}

	graph := json.RawMessage(fmt.Sprintf(`{"entry_node_id":"entry","nodes":[{"id":"entry","type":"entry","config":{"trigger":"scheduled","segment_id":"%s","schedule":"*/5 * * * *"}},{"id":"exit","type":"exit"}],"edges":[{"from":"entry","to":"exit"}]}`, seg.ID))
	journey, err := store.CreateJourney(ctx, p, domain.Journey{Name: "Score-triggered journey", Graph: graph})
	if err != nil {
		t.Fatalf("create journey: %v", err)
	}
	if _, err := journeyflow.Publish(ctx, store, &memoryBlobs{objects: map[string][]byte{}}, p, journey.ID, "00000000-0000-0000-0000-000000000001"); err != nil {
		t.Fatalf("publish journey: %v", err)
	}
	clock := journeyflow.NewFakeClock(time.Date(2026, 1, 1, 12, 5, 0, 0, time.UTC))
	if err := journeyflow.EnrollScheduledDue(ctx, store, clock); err != nil {
		t.Fatalf("enroll scheduled journey: %v", err)
	}
	var enrolled int
	if err := store.pool.QueryRow(ctx, `SELECT count(*) FROM journey_runs WHERE tenant_id=$1 AND journey_id=$2`, tenantID, journey.ID).Scan(&enrolled); err != nil {
		t.Fatalf("count enrolled runs: %v", err)
	}
	if enrolled != 1 {
		t.Fatalf("expected one score-triggered run, got %d", enrolled)
	}

	_, _ = store.pool.Exec(ctx, "DELETE FROM tenants WHERE id=$1", tenantID)
}

func TestScoreTriggeredE2E_12_11_1(t *testing.T) {
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
	pUser := p
	pUser.ActorType = "user"
	pUser.UserID = "00000000-0000-0000-0000-000000000001"
	p1ID := testUUID(tenantID + "-score-e2e-p-1")
	p2ID := testUUID(tenantID + "-score-e2e-p-2")
	if _, err := store.pool.Exec(ctx, `INSERT INTO profiles(id, tenant_id, workspace_id, app_id, external_id, attributes)
		VALUES($4, $1, $2, $3, 'score-e2e-1', '{"country":"US","age":25}'),
		      ($5, $1, $2, $3, 'score-e2e-2', '{"country":"CA","age":30}')`,
		tenantID, p.WorkspaceID, p.AppID, p1ID, p2ID); err != nil {
		t.Fatal(err)
	}

	source, err := store.CreateSegment(ctx, p, domain.Segment{
		Name: "Score E2E source",
		DSL:  json.RawMessage(`{"type":"profile_attribute","field":"country","operator":"equals","value":"US"}`),
	})
	if err != nil {
		t.Fatal(err)
	}
	model, err := store.CreateScoringModel(ctx, p, domain.ScoringModel{Name: "Score E2E model", Kind: "expression"})
	if err != nil {
		t.Fatal(err)
	}
	version, err := store.CreateScoringModelVersion(ctx, pUser, domain.ScoringModelVersion{
		ScoringModelID: model.ID,
		ScoreName:      "purchase_propensity",
		Definition:     json.RawMessage(`{"expr":"profile.age / 100.0"}`),
		OutputMax:      1,
		ManifestKey:    "score-e2e-manifest",
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := store.SetScoringModelVersionEvalStatus(ctx, pUser, version.ID, "passed"); err != nil {
		t.Fatal(err)
	}
	if _, err := scoring.Publish(ctx, store, &memoryBlobs{objects: map[string][]byte{}}, pUser, model.ID, version.Version, pUser.UserID); err != nil {
		t.Fatal(err)
	}

	request, err := store.CreateScoringRequest(ctx, pUser, model.ID, source.ID)
	if err != nil {
		t.Fatal(err)
	}
	if processed, err := operations.DrainWithGateway(ctx, store, &memoryBlobs{objects: map[string][]byte{}}, nil, 10, false); err != nil || processed != 1 {
		t.Fatalf("drain scores.compute: processed=%d err=%v", processed, err)
	}
	completed, err := store.GetScoringRequest(ctx, pUser, request.ID)
	if err != nil {
		t.Fatal(err)
	}
	if completed.Status != "complete" {
		t.Fatalf("scoring request status=%q error=%q", completed.Status, completed.Error)
	}

	scoreSegment, err := store.CreateSegment(ctx, p, domain.Segment{
		Name: "Score E2E audience",
		DSL:  json.RawMessage(fmt.Sprintf(`{"type":"score","model":"%s","score_name":"purchase_propensity","operator":"greater_than","value":0.2}`, model.ID)),
	})
	if err != nil {
		t.Fatal(err)
	}
	ids, err := store.ResolveSegment(ctx, p, scoreSegment.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(ids) != 1 || ids[0] != p1ID {
		t.Fatalf("resolved score audience=%v, want [%s]", ids, p1ID)
	}

	graph := json.RawMessage(fmt.Sprintf(`{"entry_node_id":"entry","nodes":[{"id":"entry","type":"entry","config":{"trigger":"scheduled","segment_id":"%s","schedule":"*/5 * * * *"}},{"id":"exit","type":"exit"}],"edges":[{"from":"entry","to":"exit"}]}`, scoreSegment.ID))
	journey, err := store.CreateJourney(ctx, p, domain.Journey{Name: "Score E2E journey", Graph: graph})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := journeyflow.Publish(ctx, store, &memoryBlobs{objects: map[string][]byte{}}, p, journey.ID, pUser.UserID); err != nil {
		t.Fatal(err)
	}
	if err := journeyflow.EnrollScheduledDue(ctx, store, journeyflow.NewFakeClock(time.Date(2026, 1, 1, 12, 5, 0, 0, time.UTC))); err != nil {
		t.Fatal(err)
	}
	var enrolledProfile string
	if err := store.pool.QueryRow(ctx, `SELECT profile_id FROM journey_runs WHERE tenant_id=$1 AND journey_id=$2`, tenantID, journey.ID).Scan(&enrolledProfile); err != nil {
		t.Fatal(err)
	}
	if enrolledProfile != p1ID {
		t.Fatalf("enrolled profile=%s, want %s", enrolledProfile, p1ID)
	}
	var enrolledCount int
	if err := store.pool.QueryRow(ctx, `SELECT count(*) FROM journey_runs WHERE tenant_id=$1 AND journey_id=$2`, tenantID, journey.ID).Scan(&enrolledCount); err != nil {
		t.Fatal(err)
	}
	if enrolledCount != len(ids) {
		t.Fatalf("enrolled count=%d, resolved count=%d", enrolledCount, len(ids))
	}
}
