package scoring

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

// Publish canonicalizes the scoring model version, hashes it, puts it in the blob store,
// and inserts/promotes it in the database.
func Publish(ctx context.Context, store ports.Store, blobs BlobStore, p domain.Principal, modelID string, version int, approverUserID string) (domain.ScoringModelVersion, error) {
	if approverUserID == "" {
		return domain.ScoringModelVersion{}, ErrApproverRequired
	}
	if blobs == nil {
		return domain.ScoringModelVersion{}, ErrBlobStoreRequired
	}

	// 1. Get the draft version
	sv, err := store.GetScoringModelVersionByNumber(ctx, p, modelID, version)
	if err != nil {
		return domain.ScoringModelVersion{}, err
	}

	// 2. Canonicalize scoring model version fields to JSON for manifest hashing
	manifest := struct {
		ScoringModelID string          `json:"scoring_model_id"`
		Version        int             `json:"version"`
		ScoreName      string          `json:"score_name"`
		Definition     json.RawMessage `json:"definition"`
		OutputMin      float64         `json:"output_min"`
		OutputMax      float64         `json:"output_max"`
	}{
		ScoringModelID: sv.ScoringModelID,
		Version:        sv.Version,
		ScoreName:      sv.ScoreName,
		Definition:     sv.Definition,
		OutputMin:      sv.OutputMin,
		OutputMax:      sv.OutputMax,
	}

	data, err := json.Marshal(manifest)
	if err != nil {
		return domain.ScoringModelVersion{}, err
	}

	// 3. Put manifest to blob store
	digest := sha256.Sum256(data)
	manifestKey := fmt.Sprintf("scoring/%s/%s/manifests/%x.json", p.TenantID, modelID, digest)
	if err := blobs.Put(ctx, manifestKey, data, "application/json"); err != nil {
		return domain.ScoringModelVersion{}, fmt.Errorf("put scoring model manifest: %w", err)
	}

	// 4. Update status in database
	return store.PublishScoringModelVersion(ctx, p, modelID, version, approverUserID, manifestKey)
}

// GetUsableScoringModelVersion fetches a scoring model version and ensures it has status='active' and eval_status='passed'
func GetUsableScoringModelVersion(ctx context.Context, store ports.Store, p domain.Principal, modelID string, version int) (domain.ScoringModelVersion, error) {
	sv, err := store.GetScoringModelVersionByNumber(ctx, p, modelID, version)
	if err != nil {
		return domain.ScoringModelVersion{}, err
	}
	if sv.Status != "active" || sv.EvalStatus != "passed" {
		return domain.ScoringModelVersion{}, fmt.Errorf("scoring model version is not usable (status: %s, eval_status: %s)", sv.Status, sv.EvalStatus)
	}
	return sv, nil
}
