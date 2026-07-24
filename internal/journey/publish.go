package journey

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/buildwithdmytro/openjourney/internal/domain"
	"github.com/buildwithdmytro/openjourney/internal/ports"
	"github.com/buildwithdmytro/openjourney/internal/publishing"
)

var ErrInvalidGraph = errors.New("invalid journey graph")
var ErrApproverRequired = publishing.ErrApproverRequired
var ErrBlobStoreRequired = publishing.ErrBlobStoreRequired

type BlobStore interface {
	Put(ctx context.Context, key string, data []byte, contentType string) error
}

func Publish(ctx context.Context, store ports.Store, blobs BlobStore, p domain.Principal, journeyID, approverUserID string) (domain.JourneyVersion, error) {
	if approverUserID == "" {
		return domain.JourneyVersion{}, ErrApproverRequired
	}
	if blobs == nil {
		return domain.JourneyVersion{}, ErrBlobStoreRequired
	}
	draft, err := store.GetJourney(ctx, p, journeyID)
	if err != nil {
		return domain.JourneyVersion{}, err
	}
	graph, err := ParseGraph(draft.Graph)
	if err != nil {
		return domain.JourneyVersion{}, fmt.Errorf("%w: %v", ErrInvalidGraph, err)
	}
	if err := Validate(graph); err != nil {
		return domain.JourneyVersion{}, fmt.Errorf("%w: %v", ErrInvalidGraph, err)
	}
	data, err := json.Marshal(graph)
	if err != nil {
		return domain.JourneyVersion{}, err
	}

	// Content-addressed manifests are immutable and safe to upload before the
	// database transaction. Concurrent publishes can never overwrite another
	// version's graph, and a failed publication only leaves a deduplicated object
	// that may be referenced by a later retry.
	digest := sha256.Sum256(data)
	manifestKey := fmt.Sprintf("journeys/%s/%s/manifests/%x.json", p.TenantID, journeyID, digest)
	if err := blobs.Put(ctx, manifestKey, data, "application/json"); err != nil {
		return domain.JourneyVersion{}, fmt.Errorf("put journey manifest: %w", err)
	}
	return store.PublishJourney(ctx, p, journeyID, approverUserID, manifestKey)
}
