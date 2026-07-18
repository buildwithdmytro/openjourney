package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"strings"
	"testing"

	"github.com/buildwithdmytro/openjourney/internal/domain"
	"github.com/buildwithdmytro/openjourney/internal/operations"
	"github.com/buildwithdmytro/openjourney/internal/scoring"
)

func TestScoringRegistry(t *testing.T) {
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

	// Clean up tables
	if _, err := store.pool.Exec(ctx, "TRUNCATE tenants CASCADE"); err != nil {
		t.Fatal(err)
	}

	// Setup tenant and workspace
	tenantKey := "scoring-tenant"
	if err := store.EnsureDevelopmentTenant(ctx, tenantKey); err != nil {
		t.Fatal(err)
	}
	pUser, err := store.Authenticate(ctx, tenantKey)
	if err != nil {
		t.Fatal(err)
	}
	// Verify user principal structure
	pUser.ActorType = "user"
	pUser.UserID = "00000000-0000-0000-0000-000000000001"

	pAPIKey := pUser
	pAPIKey.ActorType = "api_key"

	pAIAgent := pUser
	pAIAgent.ActorType = "ai_agent"

	// 1. Create a scoring model
	modelDraft := domain.ScoringModel{
		Name: "test-scoring-model",
		Kind: "expression",
	}
	createdModel, err := store.CreateScoringModel(ctx, pUser, modelDraft)
	if err != nil {
		t.Fatalf("failed to create scoring model: %v", err)
	}
	if createdModel.ID == "" {
		t.Fatal("expected non-empty model ID")
	}
	if createdModel.LatestVersion != 0 {
		t.Fatalf("expected latest version to be 0, got %d", createdModel.LatestVersion)
	}

	// Test GetScoringModel
	fetchedModel, err := store.GetScoringModel(ctx, pUser, createdModel.ID)
	if err != nil {
		t.Fatalf("failed to get scoring model: %v", err)
	}
	if fetchedModel.Name != createdModel.Name {
		t.Fatalf("expected model name %q, got %q", createdModel.Name, fetchedModel.Name)
	}

	// Test GetScoringModelByName
	fetchedModelByName, err := store.GetScoringModelByName(ctx, pUser, createdModel.Name)
	if err != nil {
		t.Fatalf("failed to get scoring model by name: %v", err)
	}
	if fetchedModelByName.ID != createdModel.ID {
		t.Fatalf("expected model ID %q, got %q", createdModel.ID, fetchedModelByName.ID)
	}

	// Test ListScoringModels
	allModels, err := store.ListScoringModels(ctx, pUser)
	if err != nil {
		t.Fatalf("failed to list scoring models: %v", err)
	}
	if len(allModels) != 1 {
		t.Fatalf("expected 1 scoring model, got %d", len(allModels))
	}

	// Test UpdateScoringModel
	createdModel.Name = "updated-test-model"
	updatedModel, err := store.UpdateScoringModel(ctx, pUser, createdModel)
	if err != nil {
		t.Fatalf("failed to update model: %v", err)
	}
	if updatedModel.Name != "updated-test-model" {
		t.Fatalf("expected updated name to be 'updated-test-model', got %q", updatedModel.Name)
	}

	// 2. Create Scoring Model Versions
	svDraft1 := domain.ScoringModelVersion{
		ScoringModelID: createdModel.ID,
		ScoreName:      "propensity_score",
		Definition:     json.RawMessage(`{"expr":"profile.age > 18"}`),
		OutputMin:      0.0,
		OutputMax:      1.0,
		ManifestKey:    "dummy-key-1",
	}

	v1, err := store.CreateScoringModelVersion(ctx, pUser, svDraft1)
	if err != nil {
		t.Fatalf("failed to create model version 1: %v", err)
	}
	if v1.Version != 1 {
		t.Fatalf("expected version 1, got %d", v1.Version)
	}
	if v1.EvalStatus != "pending" {
		t.Fatalf("expected initial eval status to be pending, got %q", v1.EvalStatus)
	}
	if v1.Status != "draft" {
		t.Fatalf("expected initial status to be draft, got %q", v1.Status)
	}

	// Fetch parent model to check latest_version incremented
	modelWithV1, err := store.GetScoringModel(ctx, pUser, createdModel.ID)
	if err != nil {
		t.Fatal(err)
	}
	if modelWithV1.LatestVersion != 1 {
		t.Fatalf("expected model latest version to be 1, got %d", modelWithV1.LatestVersion)
	}

	// Fetch by number
	fetchedV1, err := store.GetScoringModelVersionByNumber(ctx, pUser, createdModel.ID, 1)
	if err != nil {
		t.Fatalf("failed to get model version by number: %v", err)
	}
	if fetchedV1.ID != v1.ID {
		t.Fatal("version IDs did not match")
	}

	// List versions
	versions, err := store.ListScoringModelVersions(ctx, pUser, createdModel.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(versions) != 1 {
		t.Fatalf("expected 1 version, got %d", len(versions))
	}

	// 3. Publish Invariants & Gates
	approverID := "00000000-0000-0000-0000-000000000002"
	// Check API key / non-human publisher rejection
	_, err = store.PublishScoringModelVersion(ctx, pAPIKey, createdModel.ID, 1, approverID, "manifest-1")
	if !errors.Is(err, ErrUnauthorized) {
		t.Fatalf("expected ErrUnauthorized for non-human (api_key) publisher, got: %v", err)
	}
	_, err = store.PublishScoringModelVersion(ctx, pAIAgent, createdModel.ID, 1, approverID, "manifest-1")
	if !errors.Is(err, ErrUnauthorized) {
		t.Fatalf("expected ErrUnauthorized for non-human (ai_agent) publisher, got: %v", err)
	}

	// Check eval_status gate (must refuse pending eval)
	_, err = store.PublishScoringModelVersion(ctx, pUser, createdModel.ID, 1, approverID, "manifest-1")
	if err == nil || !strings.Contains(err.Error(), "non-passed eval status") {
		t.Fatalf("expected rejection due to pending eval, got: %v", err)
	}

	// Set eval_status to passed
	err = store.SetScoringModelVersionEvalStatus(ctx, pUser, v1.ID, "passed")
	if err != nil {
		t.Fatalf("failed to set eval status: %v", err)
	}

	// Verify eval status changed
	v1Updated, err := store.GetScoringModelVersion(ctx, pUser, v1.ID)
	if err != nil {
		t.Fatal(err)
	}
	if v1Updated.EvalStatus != "passed" {
		t.Fatalf("expected eval status 'passed', got %q", v1Updated.EvalStatus)
	}

	// Try set invalid eval status
	err = store.SetScoringModelVersionEvalStatus(ctx, pUser, v1.ID, "invalid-status")
	if err == nil {
		t.Fatal("expected error setting invalid eval status")
	}

	// Now publish should succeed
	publishedV1, err := store.PublishScoringModelVersion(ctx, pUser, createdModel.ID, 1, approverID, "manifest-1")
	if err != nil {
		t.Fatalf("failed to publish model version: %v", err)
	}
	if publishedV1.Status != "active" {
		t.Fatalf("expected published version status to be 'active', got %q", publishedV1.Status)
	}
	if publishedV1.PublishedBy == nil || *publishedV1.PublishedBy != approverID {
		t.Fatalf("expected published_by %q", approverID)
	}

	// Check parent model has current_version_id set
	modelPublished, err := store.GetScoringModel(ctx, pUser, createdModel.ID)
	if err != nil {
		t.Fatal(err)
	}
	if modelPublished.CurrentVersionID == nil || *modelPublished.CurrentVersionID != publishedV1.ID {
		t.Fatal("expected parent model to point to current version id")
	}

	// 4. Idempotency Check
	// Publishing again should be a no-op
	publishedV1Again, err := store.PublishScoringModelVersion(ctx, pUser, createdModel.ID, 1, approverID, "manifest-1")
	if err != nil {
		t.Fatalf("expected idempotent success on second publish, got: %v", err)
	}
	if publishedV1Again.ID != publishedV1.ID {
		t.Fatal("expected same version returned")
	}

	// 5. Version Archiving on New Publish
	svDraft2 := domain.ScoringModelVersion{
		ScoringModelID: createdModel.ID,
		ScoreName:      "propensity_score",
		Definition:     json.RawMessage(`{"expr":"profile.age > 21"}`),
		OutputMin:      0.0,
		OutputMax:      1.0,
		ManifestKey:    "dummy-key-2",
	}

	v2, err := store.CreateScoringModelVersion(ctx, pUser, svDraft2)
	if err != nil {
		t.Fatal(err)
	}
	err = store.SetScoringModelVersionEvalStatus(ctx, pUser, v2.ID, "passed")
	if err != nil {
		t.Fatal(err)
	}

	// Publish version 2
	publishedV2, err := store.PublishScoringModelVersion(ctx, pUser, createdModel.ID, 2, approverID, "manifest-2")
	if err != nil {
		t.Fatalf("failed to publish version 2: %v", err)
	}

	// Check version 1 has been archived
	v1Archived, err := store.GetScoringModelVersion(ctx, pUser, v1.ID)
	if err != nil {
		t.Fatal(err)
	}
	if v1Archived.Status != "archived" {
		t.Fatalf("expected version 1 to be archived, got %q", v1Archived.Status)
	}
	if publishedV2.Status != "active" {
		t.Fatalf("expected version 2 to be active, got %q", publishedV2.Status)
	}

	// Check parent model has updated current_version_id to v2
	modelPublished2, err := store.GetScoringModel(ctx, pUser, createdModel.ID)
	if err != nil {
		t.Fatal(err)
	}
	if modelPublished2.CurrentVersionID == nil || *modelPublished2.CurrentVersionID != publishedV2.ID {
		t.Fatal("expected parent model to point to version 2 ID")
	}

	// 6. Test high-level scoring service Publish and GetUsableScoringModelVersion
	blobs := &memoryBlobs{objects: make(map[string][]byte)}

	// Create version 3
	svDraft3 := domain.ScoringModelVersion{
		ScoringModelID: createdModel.ID,
		ScoreName:      "propensity_score",
		Definition:     json.RawMessage(`{"expr":"profile.age > 30"}`),
		OutputMin:      0.0,
		OutputMax:      1.0,
		ManifestKey:    "dummy-key-3",
	}
	v3, err := store.CreateScoringModelVersion(ctx, pUser, svDraft3)
	if err != nil {
		t.Fatal(err)
	}

	// Try to get usable version 3 for invocation => must fail (unevaluated/unpublished)
	_, err = scoring.GetUsableScoringModelVersion(ctx, store, pUser, createdModel.ID, 3)
	if err == nil {
		t.Fatal("expected error getting unevaluated/unpublished version for invocation")
	}

	// Set eval_status of v3 to passed
	err = store.SetScoringModelVersionEvalStatus(ctx, pUser, v3.ID, "passed")
	if err != nil {
		t.Fatal(err)
	}

	// Still unpublished => must fail invocation
	_, err = scoring.GetUsableScoringModelVersion(ctx, store, pUser, createdModel.ID, 3)
	if err == nil {
		t.Fatal("expected error getting unpublished version for invocation")
	}

	// Use scoring.Publish to publish
	publishedV3, err := scoring.Publish(ctx, store, blobs, pUser, createdModel.ID, 3, approverID)
	if err != nil {
		t.Fatalf("service publish failed: %v", err)
	}
	if publishedV3.Status != "active" {
		t.Fatalf("expected version 3 to be active")
	}

	// Check that manifest was frozen in blob store
	if publishedV3.ManifestKey == "" {
		t.Fatal("expected manifest key to be set")
	}
	blobData, err := blobs.Get(ctx, publishedV3.ManifestKey)
	if err != nil {
		t.Fatalf("manifest not found in blob store: %v", err)
	}
	var manifestData struct {
		ScoringModelID string          `json:"scoring_model_id"`
		ScoreName      string          `json:"score_name"`
		Definition     json.RawMessage `json:"definition"`
	}
	if err := json.Unmarshal(blobData, &manifestData); err != nil {
		t.Fatal(err)
	}
	if manifestData.ScoringModelID != createdModel.ID || manifestData.ScoreName != "propensity_score" {
		t.Fatalf("invalid manifest data stored: %+v", manifestData)
	}

	// GetUsableScoringModelVersion now must succeed
	usableV3, err := scoring.GetUsableScoringModelVersion(ctx, store, pUser, createdModel.ID, 3)
	if err != nil {
		t.Fatalf("failed to get usable scoring model version: %v", err)
	}
	if usableV3.ID != v3.ID {
		t.Fatal("expected usable ID to match version 3")
	}

	// 7. Delete Scoring Model
	err = store.DeleteScoringModel(ctx, pUser, createdModel.ID)
	if err != nil {
		t.Fatalf("failed to delete model: %v", err)
	}
	_, err = store.GetScoringModel(ctx, pUser, createdModel.ID)
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("expected ErrNotFound for deleted model, got %v", err)
	}
}

func TestBatchScoringJob_12_5_1(t *testing.T) {
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
	pUser := p
	pUser.ActorType = "user"
	pUser.UserID = "00000000-0000-0000-0000-000000000001"

	p1ID := testUUID(tenantID + "-p-1")
	p2ID := testUUID(tenantID + "-p-2")

	_, err = store.pool.Exec(ctx, `INSERT INTO profiles(id, tenant_id, workspace_id, app_id, external_id, attributes)
		VALUES($4, $1, $2, $3, 'ext-1', '{"country":"US","age":25}'),
		      ($5, $1, $2, $3, 'ext-2', '{"country":"CA","age":30}')`, tenantID, p.WorkspaceID, p.AppID, p1ID, p2ID)
	if err != nil {
		t.Fatalf("insert profiles: %v", err)
	}

	// Insert some events to test event aggregate counting in expression env
	// Events for profile 1: 3 clicks within 30 days
	_, err = store.pool.Exec(ctx, `INSERT INTO accepted_events (tenant_id, workspace_id, app_id, event_type, schema_version, external_id, idempotency_key, occurred_at, payload)
		VALUES ($1, $2, $3, 'click', 1, 'ext-1', 'click-1', now(), '{}'),
		       ($1, $2, $3, 'click', 1, 'ext-1', 'click-2', now() - interval '10 days', '{}'),
		       ($1, $2, $3, 'click', 1, 'ext-1', 'click-3', now() - interval '40 days', '{}')`, tenantID, p.WorkspaceID, p.AppID)
	if err != nil {
		t.Fatalf("insert events: %v", err)
	}

	// Create a segment of all profiles
	seg, err := store.CreateSegment(ctx, p, domain.Segment{
		Name: "All Users",
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

	// Create expression scoring model
	model, err := store.CreateScoringModel(ctx, pUser, domain.ScoringModel{
		Name: "propensity-model",
		Kind: "expression",
	})
	if err != nil {
		t.Fatalf("create model: %v", err)
	}

	// Create and Publish scoring model version
	svDraft := domain.ScoringModelVersion{
		ScoringModelID: model.ID,
		ScoreName:      "propensity_score",
		Definition:     json.RawMessage(`{"expr":"profile.age * 2.0 + events.click.count_30d"}`),
		OutputMin:      0.0,
		OutputMax:      100.0,
		ManifestKey:    "dummy-manifest-key",
	}

	v1, err := store.CreateScoringModelVersion(ctx, pUser, svDraft)
	if err != nil {
		t.Fatalf("create version: %v", err)
	}

	err = store.SetScoringModelVersionEvalStatus(ctx, pUser, v1.ID, "passed")
	if err != nil {
		t.Fatalf("set eval passed: %v", err)
	}

	blobs := &memoryBlobs{objects: make(map[string][]byte)}
	_, err = scoring.Publish(ctx, store, blobs, pUser, model.ID, 1, "00000000-0000-0000-0000-000000000002")
	if err != nil {
		t.Fatalf("publish: %v", err)
	}

	// 1. Enqueue scores.compute job
	req, err := store.CreateScoringRequest(ctx, pUser, model.ID, seg.ID)
	if err != nil {
		t.Fatalf("create scoring request: %v", err)
	}
	if req.Status != "pending" {
		t.Fatalf("expected pending scoring request status, got %q", req.Status)
	}

	// Verify job is enqueued in operation_jobs
	var jobCount int
	err = store.pool.QueryRow(ctx, "SELECT count(*) FROM operation_jobs WHERE tenant_id=$1 AND job_type='scores.compute'", tenantID).Scan(&jobCount)
	if err != nil || jobCount != 1 {
		t.Fatalf("expected 1 enqueued job, got %d (err: %v)", jobCount, err)
	}

	// 2. Run operations Drain to execute the scoring job
	processed, err := operations.DrainWithGateway(ctx, store, blobs, nil, 10, false)
	if err != nil {
		t.Fatalf("operations.Drain: %v", err)
	}
	if processed != 1 {
		t.Fatalf("expected 1 job processed, got %d", processed)
	}

	// Verify request is completed
	reqCompleted, err := store.GetScoringRequest(ctx, pUser, req.ID)
	if err != nil {
		t.Fatalf("GetScoringRequest: %v", err)
	}
	if reqCompleted.Status != "complete" {
		t.Fatalf("expected status complete, got %q (err: %s)", reqCompleted.Status, reqCompleted.Error)
	}

	// Verify profile_scores has been updated
	// For profile 1 (US, age 25, clicks in 30d = 2): 25*2 + 2 = 52
	var val float64
	var modelVer int
	err = store.pool.QueryRow(ctx, "SELECT value, model_version FROM profile_scores WHERE tenant_id=$1 AND profile_id=$2 AND scoring_model_id=$3 AND score_name=$4",
		tenantID, p1ID, model.ID, "propensity_score").Scan(&val, &modelVer)
	if err != nil {
		t.Fatalf("query profile score: %v", err)
	}
	if val != 52.0 {
		t.Fatalf("expected score 52.0, got %f", val)
	}
	if modelVer != 1 {
		t.Fatalf("expected model version 1, got %d", modelVer)
	}

	// 3. Score twice to verify idempotency (upsert on PK)
	req2, err := store.CreateScoringRequest(ctx, pUser, model.ID, seg.ID)
	if err != nil {
		t.Fatalf("create scoring request 2: %v", err)
	}

	processed2, err := operations.DrainWithGateway(ctx, store, blobs, nil, 10, false)
	if err != nil {
		t.Fatalf("operations.Drain 2: %v", err)
	}
	if processed2 != 1 {
		t.Fatalf("expected 1 job processed in second run, got %d", processed2)
	}

	req2Completed, err := store.GetScoringRequest(ctx, pUser, req2.ID)
	if err != nil {
		t.Fatal(err)
	}
	if req2Completed.Status != "complete" {
		t.Fatalf("expected status complete for second run, got %q", req2Completed.Status)
	}

	// Count how many rows exist for this profile and model in profile_scores
	var scoreRowsCount int
	err = store.pool.QueryRow(ctx, "SELECT count(*) FROM profile_scores WHERE tenant_id=$1 AND profile_id=$2 AND scoring_model_id=$3 AND score_name=$4",
		tenantID, p1ID, model.ID, "propensity_score").Scan(&scoreRowsCount)
	if err != nil {
		t.Fatalf("count profile score rows: %v", err)
	}
	if scoreRowsCount != 1 {
		t.Fatalf("expected exactly 1 row (idempotency check failed), got %d", scoreRowsCount)
	}

	_, _ = store.pool.Exec(ctx, "DELETE FROM tenants WHERE id=$1", tenantID)
}

