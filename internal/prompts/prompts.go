package prompts

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"

	"github.com/buildwithdmytro/openjourney/internal/domain"
	"github.com/buildwithdmytro/openjourney/internal/ports"
	"github.com/buildwithdmytro/openjourney/internal/publishing"
)

var ErrApproverRequired = publishing.ErrApproverRequired
var ErrBlobStoreRequired = publishing.ErrBlobStoreRequired

type BlobStore interface {
	Put(ctx context.Context, key string, data []byte, contentType string) error
}

// Publish canonicalizes the prompt version, hashes it, puts it in the blob store,
// and inserts/promotes it in the database.
func Publish(ctx context.Context, store ports.Store, blobs BlobStore, p domain.Principal, promptID string, version int, approverUserID string) (domain.PromptVersion, error) {
	if approverUserID == "" {
		return domain.PromptVersion{}, ErrApproverRequired
	}
	if blobs == nil {
		return domain.PromptVersion{}, ErrBlobStoreRequired
	}

	// 1. Get the draft version
	pv, err := store.GetPromptVersionByNumber(ctx, p, promptID, version)
	if err != nil {
		return domain.PromptVersion{}, err
	}

	// 2. Canonicalize prompt version fields to JSON for manifest hashing
	manifest := struct {
		PromptID     string          `json:"prompt_id"`
		Version      int             `json:"version"`
		Template     string          `json:"template"`
		InputSchema  json.RawMessage `json:"input_schema"`
		OutputSchema json.RawMessage `json:"output_schema"`
		Provider     string          `json:"provider"`
		Model        string          `json:"model"`
		Params       json.RawMessage `json:"params"`
		SafetyPolicy json.RawMessage `json:"safety_policy"`
	}{
		PromptID:     pv.PromptID,
		Version:      pv.Version,
		Template:     pv.Template,
		InputSchema:  pv.InputSchema,
		OutputSchema: pv.OutputSchema,
		Provider:     pv.Provider,
		Model:        pv.Model,
		Params:       pv.Params,
		SafetyPolicy: pv.SafetyPolicy,
	}

	data, err := json.Marshal(manifest)
	if err != nil {
		return domain.PromptVersion{}, err
	}

	// 3. Put manifest to blob store
	digest := sha256.Sum256(data)
	manifestKey := fmt.Sprintf("prompts/%s/%s/manifests/%x.json", p.TenantID, promptID, digest)
	if err := blobs.Put(ctx, manifestKey, data, "application/json"); err != nil {
		return domain.PromptVersion{}, fmt.Errorf("put prompt manifest: %w", err)
	}

	// 4. Update status in database
	return store.PublishPromptVersion(ctx, p, promptID, version, approverUserID, manifestKey)
}

// GetUsablePromptVersion fetches a prompt version and ensures it has status='active' and eval_status='passed'
func GetUsablePromptVersion(ctx context.Context, store ports.Store, p domain.Principal, promptID string, version int) (domain.PromptVersion, error) {
	pv, err := store.GetPromptVersionByNumber(ctx, p, promptID, version)
	if err != nil {
		return domain.PromptVersion{}, err
	}
	if pv.Status != "active" || pv.EvalStatus != "passed" {
		return domain.PromptVersion{}, fmt.Errorf("prompt version is not usable (status: %s, eval_status: %s)", pv.Status, pv.EvalStatus)
	}
	return pv, nil
}
