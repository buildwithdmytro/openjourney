// Package publishing contains the common immutable-publication seam used by
// versioned resources such as forms and landing pages.
package publishing

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"

	"github.com/buildwithdmytro/openjourney/internal/domain"
)

var ErrHumanActorRequired = errors.New("publishing requires an authenticated user")
var ErrBlobStoreRequired = errors.New("blob store is required")
var ErrSelfApproval = errors.New("self approval forbidden: creator cannot approve their own draft")


// BlobStore is deliberately the small write-only interface needed before a
// resource transaction. The full ports.BlobStore also satisfies it.
type BlobStore interface {
	Put(context.Context, string, []byte, string) error
}

// Commit promotes the immutable manifest in the resource store. Implementors
// must lock the draft and return the already-published version when the
// manifest is published again; this makes retries idempotent.
type Commit[Published any] func(context.Context, domain.Principal, string, string, string) (Published, error)

// Publish canonicalizes a draft, freezes its bytes in a content-addressed
// blob, and then lets the resource store create/promote its immutable version.
// The human check lives here so every form/page caller gets the same gate.
func Publish[Draft any, Published any](ctx context.Context, p domain.Principal, resourceID, namespace string, draft Draft, blobs BlobStore, canonicalize func(Draft) ([]byte, error), commit Commit[Published]) (Published, error) {
	var zero Published
	if p.ActorType != "user" || p.UserID == "" {
		return zero, ErrHumanActorRequired
	}
	if blobs == nil {
		return zero, ErrBlobStoreRequired
	}
	if resourceID == "" || namespace == "" {
		return zero, errors.New("resource id and namespace are required")
	}
	if canonicalize == nil || commit == nil {
		return zero, errors.New("canonicalizer and commit function are required")
	}
	data, err := canonicalize(draft)
	if err != nil {
		return zero, fmt.Errorf("canonicalize %s: %w", namespace, err)
	}
	digest := sha256.Sum256(data)
	key := fmt.Sprintf("%s/%s/%s/manifests/%x.json", namespace, p.TenantID, resourceID, digest)
	if err := blobs.Put(ctx, key, data, "application/json"); err != nil {
		return zero, fmt.Errorf("put %s manifest: %w", namespace, err)
	}
	return commit(ctx, p, resourceID, p.UserID, key)
}
