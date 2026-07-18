package extension

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/buildwithdmytro/openjourney/internal/domain"
	"github.com/buildwithdmytro/openjourney/internal/ports"
)

var ErrApproverRequired = errors.New("approver user id is required")
var ErrBlobStoreRequired = errors.New("blob store is required")

type BlobStore interface {
	Put(ctx context.Context, key string, data []byte, contentType string) error
}

// Publish retrieves a draft extension version, canonicalizes its manifest,
// uploads it to the blob store, verifies the publisher JWS signature, and
// publishes the version.
func Publish(ctx context.Context, store ports.Store, blobs BlobStore, p domain.Principal, extensionID string, version int, approverUserID string) (domain.ExtensionVersion, error) {
	if approverUserID == "" {
		return domain.ExtensionVersion{}, ErrApproverRequired
	}
	if blobs == nil {
		return domain.ExtensionVersion{}, ErrBlobStoreRequired
	}

	// 1. Fetch draft version
	ev, err := store.GetExtensionVersionByNumber(ctx, p, extensionID, version)
	if err != nil {
		return domain.ExtensionVersion{}, err
	}

	// 2. Canonicalize the manifest fields for manifest hashing
	var canonicalMap any
	if err := json.Unmarshal(ev.Manifest, &canonicalMap); err != nil {
		return domain.ExtensionVersion{}, fmt.Errorf("unmarshal manifest: %w", err)
	}

	data, err := json.Marshal(canonicalMap)
	if err != nil {
		return domain.ExtensionVersion{}, err
	}

	// 3. Put manifest to blob store (immutable content-addressed freeze)
	digest := sha256.Sum256(data)
	manifestKey := fmt.Sprintf("extensions/%s/%s/manifests/%x.json", p.TenantID, extensionID, digest)
	if err := blobs.Put(ctx, manifestKey, data, "application/json"); err != nil {
		return domain.ExtensionVersion{}, fmt.Errorf("put extension manifest: %w", err)
	}

	// 4. Update status and verify signature in database
	return store.PublishExtensionVersion(ctx, p, extensionID, version, approverUserID, manifestKey)
}
