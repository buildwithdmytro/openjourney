package publishing_test

import (
	"testing"

	"github.com/buildwithdmytro/openjourney/internal/extension"
	"github.com/buildwithdmytro/openjourney/internal/journey"
	"github.com/buildwithdmytro/openjourney/internal/postgres"
	"github.com/buildwithdmytro/openjourney/internal/prompts"
	"github.com/buildwithdmytro/openjourney/internal/publishing"
	"github.com/buildwithdmytro/openjourney/internal/scoring"
)

func TestPublishingSentinelsHaveOneCanonicalIdentity(t *testing.T) {
	if extension.ErrApproverRequired != publishing.ErrApproverRequired ||
		extension.ErrBlobStoreRequired != publishing.ErrBlobStoreRequired ||
		prompts.ErrApproverRequired != publishing.ErrApproverRequired ||
		prompts.ErrBlobStoreRequired != publishing.ErrBlobStoreRequired ||
		journey.ErrApproverRequired != publishing.ErrApproverRequired ||
		journey.ErrBlobStoreRequired != publishing.ErrBlobStoreRequired ||
		scoring.ErrApproverRequired != publishing.ErrApproverRequired ||
		scoring.ErrBlobStoreRequired != publishing.ErrBlobStoreRequired ||
		postgres.ErrSelfApproval != publishing.ErrSelfApproval {
		t.Fatal("sentinel errors must be aliases of the canonical publishing errors")
	}
}
