package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"strings"
	"testing"

	"github.com/buildwithdmytro/openjourney/internal/domain"
	"github.com/buildwithdmytro/openjourney/internal/prompts"
)

func TestPromptsRegistry(t *testing.T) {
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
	tenantKey := "prompt-tenant"
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

	// 1. Create a prompt
	promptDraft := domain.Prompt{
		Name:     "test-prompt",
		TaskType: "content_draft",
	}
	createdPrompt, err := store.CreatePrompt(ctx, pUser, promptDraft)
	if err != nil {
		t.Fatalf("failed to create prompt: %v", err)
	}
	if createdPrompt.ID == "" {
		t.Fatal("expected non-empty prompt ID")
	}
	if createdPrompt.LatestVersion != 0 {
		t.Fatalf("expected latest version to be 0, got %d", createdPrompt.LatestVersion)
	}

	// Test GetPrompt
	fetchedPrompt, err := store.GetPrompt(ctx, pUser, createdPrompt.ID)
	if err != nil {
		t.Fatalf("failed to get prompt: %v", err)
	}
	if fetchedPrompt.Name != createdPrompt.Name {
		t.Fatalf("expected prompt name %q, got %q", createdPrompt.Name, fetchedPrompt.Name)
	}

	// Test GetPromptByName
	fetchedPromptByName, err := store.GetPromptByName(ctx, pUser, createdPrompt.Name)
	if err != nil {
		t.Fatalf("failed to get prompt by name: %v", err)
	}
	if fetchedPromptByName.ID != createdPrompt.ID {
		t.Fatalf("expected prompt ID %q, got %q", createdPrompt.ID, fetchedPromptByName.ID)
	}

	// Test ListPrompts
	allPrompts, err := store.ListPrompts(ctx, pUser)
	if err != nil {
		t.Fatalf("failed to list prompts: %v", err)
	}
	if len(allPrompts) != 1 {
		t.Fatalf("expected 1 prompt, got %d", len(allPrompts))
	}

	// Test UpdatePrompt
	createdPrompt.Name = "updated-test-prompt"
	updatedPrompt, err := store.UpdatePrompt(ctx, pUser, createdPrompt)
	if err != nil {
		t.Fatalf("failed to update prompt: %v", err)
	}
	if updatedPrompt.Name != "updated-test-prompt" {
		t.Fatalf("expected updated name to be 'updated-test-prompt', got %q", updatedPrompt.Name)
	}

	// 2. Create Prompt Versions
	pvDraft1 := domain.PromptVersion{
		PromptID:     createdPrompt.ID,
		Template:     "Hello {{name}}",
		InputSchema:  json.RawMessage(`{"type":"object"}`),
		OutputSchema: json.RawMessage(`{"type":"object"}`),
		Provider:     "fake",
		Model:        "fake-model",
		Params:       json.RawMessage(`{}`),
		SafetyPolicy: json.RawMessage(`{}`),
		ManifestKey:  "dummy-key-1",
	}

	v1, err := store.CreatePromptVersion(ctx, pUser, pvDraft1)
	if err != nil {
		t.Fatalf("failed to create prompt version 1: %v", err)
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

	// Fetch parent prompt to check latest_version incremented
	promptWithV1, err := store.GetPrompt(ctx, pUser, createdPrompt.ID)
	if err != nil {
		t.Fatal(err)
	}
	if promptWithV1.LatestVersion != 1 {
		t.Fatalf("expected prompt latest version to be 1, got %d", promptWithV1.LatestVersion)
	}

	// Fetch by number
	fetchedV1, err := store.GetPromptVersionByNumber(ctx, pUser, createdPrompt.ID, 1)
	if err != nil {
		t.Fatalf("failed to get prompt version by number: %v", err)
	}
	if fetchedV1.ID != v1.ID {
		t.Fatal("version IDs did not match")
	}

	// List versions
	versions, err := store.ListPromptVersions(ctx, pUser, createdPrompt.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(versions) != 1 {
		t.Fatalf("expected 1 version, got %d", len(versions))
	}

	// 3. Publish Invariants & Gates
	approverID := "00000000-0000-0000-0000-000000000002"
	// Check API key / non-human publisher rejection
	_, err = store.PublishPromptVersion(ctx, pAPIKey, createdPrompt.ID, 1, approverID, "manifest-1")
	if !errors.Is(err, ErrUnauthorized) {
		t.Fatalf("expected ErrUnauthorized for non-human (api_key) publisher, got: %v", err)
	}

	// Check eval_status gate (must refuse pending eval)
	_, err = store.PublishPromptVersion(ctx, pUser, createdPrompt.ID, 1, approverID, "manifest-1")
	if err == nil || !strings.Contains(err.Error(), "non-passed eval status") {
		t.Fatalf("expected rejection due to pending eval, got: %v", err)
	}

	// Set eval_status to passed
	err = store.SetPromptVersionEvalStatus(ctx, pUser, v1.ID, "passed")
	if err != nil {
		t.Fatalf("failed to set eval status: %v", err)
	}

	// Verify eval status changed
	v1Updated, err := store.GetPromptVersion(ctx, pUser, v1.ID)
	if err != nil {
		t.Fatal(err)
	}
	if v1Updated.EvalStatus != "passed" {
		t.Fatalf("expected eval status 'passed', got %q", v1Updated.EvalStatus)
	}

	// Try set invalid eval status
	err = store.SetPromptVersionEvalStatus(ctx, pUser, v1.ID, "invalid-status")
	if err == nil {
		t.Fatal("expected error setting invalid eval status")
	}

	// Now publish should succeed
	publishedV1, err := store.PublishPromptVersion(ctx, pUser, createdPrompt.ID, 1, approverID, "manifest-1")
	if err != nil {
		t.Fatalf("failed to publish prompt version: %v", err)
	}
	if publishedV1.Status != "active" {
		t.Fatalf("expected published version status to be 'active', got %q", publishedV1.Status)
	}
	if publishedV1.PublishedBy == nil || *publishedV1.PublishedBy != approverID {
		t.Fatalf("expected published_by %q", approverID)
	}

	// Check parent prompt has current_version_id set
	promptPublished, err := store.GetPrompt(ctx, pUser, createdPrompt.ID)
	if err != nil {
		t.Fatal(err)
	}
	if promptPublished.CurrentVersionID == nil || *promptPublished.CurrentVersionID != publishedV1.ID {
		t.Fatal("expected parent prompt to point to current version id")
	}

	// 4. Idempotency Check
	// Publishing again should be a no-op
	publishedV1Again, err := store.PublishPromptVersion(ctx, pUser, createdPrompt.ID, 1, approverID, "manifest-1")
	if err != nil {
		t.Fatalf("expected idempotent success on second publish, got: %v", err)
	}
	if publishedV1Again.ID != publishedV1.ID {
		t.Fatal("expected same version returned")
	}

	// 5. Version Archiving on New Publish
	pvDraft2 := domain.PromptVersion{
		PromptID:     createdPrompt.ID,
		Template:     "Hello again {{name}}",
		InputSchema:  json.RawMessage(`{"type":"object"}`),
		OutputSchema: json.RawMessage(`{"type":"object"}`),
		Provider:     "fake",
		Model:        "fake-model",
		Params:       json.RawMessage(`{}`),
		SafetyPolicy: json.RawMessage(`{}`),
		ManifestKey:  "dummy-key-2",
	}

	v2, err := store.CreatePromptVersion(ctx, pUser, pvDraft2)
	if err != nil {
		t.Fatal(err)
	}
	err = store.SetPromptVersionEvalStatus(ctx, pUser, v2.ID, "passed")
	if err != nil {
		t.Fatal(err)
	}

	// Publish version 2
	publishedV2, err := store.PublishPromptVersion(ctx, pUser, createdPrompt.ID, 2, approverID, "manifest-2")
	if err != nil {
		t.Fatalf("failed to publish version 2: %v", err)
	}

	// Check version 1 has been archived
	v1Archived, err := store.GetPromptVersion(ctx, pUser, v1.ID)
	if err != nil {
		t.Fatal(err)
	}
	if v1Archived.Status != "archived" {
		t.Fatalf("expected version 1 to be archived, got %q", v1Archived.Status)
	}
	if publishedV2.Status != "active" {
		t.Fatalf("expected version 2 to be active, got %q", publishedV2.Status)
	}

	// Check parent prompt has updated current_version_id to v2
	promptPublished2, err := store.GetPrompt(ctx, pUser, createdPrompt.ID)
	if err != nil {
		t.Fatal(err)
	}
	if promptPublished2.CurrentVersionID == nil || *promptPublished2.CurrentVersionID != publishedV2.ID {
		t.Fatal("expected parent prompt to point to version 2 ID")
	}

	// 6. Test high-level prompts service Publish and GetUsablePromptVersion
	blobs := &memoryBlobs{objects: make(map[string][]byte)}

	// Create version 3
	pvDraft3 := domain.PromptVersion{
		PromptID:     createdPrompt.ID,
		Template:     "Hello third {{name}}",
		InputSchema:  json.RawMessage(`{"type":"object"}`),
		OutputSchema: json.RawMessage(`{"type":"object"}`),
		Provider:     "fake",
		Model:        "fake-model",
		Params:       json.RawMessage(`{}`),
		SafetyPolicy: json.RawMessage(`{}`),
		ManifestKey:  "dummy-key-3",
	}
	v3, err := store.CreatePromptVersion(ctx, pUser, pvDraft3)
	if err != nil {
		t.Fatal(err)
	}

	// Try to get usable version 3 for invocation => must fail (unevaluated/unpublished)
	_, err = prompts.GetUsablePromptVersion(ctx, store, pUser, createdPrompt.ID, 3)
	if err == nil {
		t.Fatal("expected error getting unevaluated/unpublished version for invocation")
	}

	// Set eval_status of v3 to passed
	err = store.SetPromptVersionEvalStatus(ctx, pUser, v3.ID, "passed")
	if err != nil {
		t.Fatal(err)
	}

	// Still unpublished => must fail invocation
	_, err = prompts.GetUsablePromptVersion(ctx, store, pUser, createdPrompt.ID, 3)
	if err == nil {
		t.Fatal("expected error getting unpublished version for invocation")
	}

	// Use prompts.Publish to publish
	publishedV3, err := prompts.Publish(ctx, store, blobs, pUser, createdPrompt.ID, 3, approverID)
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
		PromptID string `json:"prompt_id"`
		Template string `json:"template"`
	}
	if err := json.Unmarshal(blobData, &manifestData); err != nil {
		t.Fatal(err)
	}
	if manifestData.PromptID != createdPrompt.ID || manifestData.Template != "Hello third {{name}}" {
		t.Fatalf("invalid manifest data stored: %+v", manifestData)
	}

	// GetUsablePromptVersion now must succeed
	usableV3, err := prompts.GetUsablePromptVersion(ctx, store, pUser, createdPrompt.ID, 3)
	if err != nil {
		t.Fatalf("failed to get usable prompt version: %v", err)
	}
	if usableV3.ID != v3.ID {
		t.Fatal("expected usable ID to match version 3")
	}

	// 7. Delete Prompt
	err = store.DeletePrompt(ctx, pUser, createdPrompt.ID)
	if err != nil {
		t.Fatalf("failed to delete prompt: %v", err)
	}
	_, err = store.GetPrompt(ctx, pUser, createdPrompt.ID)
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("expected ErrNotFound for deleted prompt, got %v", err)
	}
}
