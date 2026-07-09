package journey

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/buildwithdmytro/openjourney/internal/domain"
	"github.com/buildwithdmytro/openjourney/internal/ports"
)

var ErrInvalidGraph = errors.New("invalid journey graph")
var ErrApproverRequired = errors.New("approver user id is required")
var ErrBlobStoreRequired = errors.New("blob store is required")

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

	version := draft.LatestVersion + 1
	manifestKey := fmt.Sprintf("journeys/%s/%s/v%d.json", p.TenantID, journeyID, version)
	if err := blobs.Put(ctx, manifestKey, data, "application/json"); err != nil {
		return domain.JourneyVersion{}, fmt.Errorf("put journey manifest: %w", err)
	}
	return store.PublishJourney(ctx, p, journeyID, approverUserID, manifestKey)
}
