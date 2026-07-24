package render

import (
	"context"
	"testing"

	"github.com/buildwithdmytro/openjourney/internal/domain"
	"github.com/buildwithdmytro/openjourney/internal/ports"
)

type ssrfTestStore struct {
	ports.Store
	activities []domain.ExtensionActivity
}

func (s *ssrfTestStore) ListConnectedContentSources(context.Context, domain.Principal) ([]domain.ConnectedContentSource, error) {
	return []domain.ConnectedContentSource{{
		ID:          "private-source",
		AllowedHost: "127.0.0.1",
		Enabled:     true,
	}}, nil
}

func (s *ssrfTestStore) RecordExtensionActivity(_ context.Context, _ domain.Principal, activity domain.ExtensionActivity) (domain.ExtensionActivity, error) {
	s.activities = append(s.activities, activity)
	return activity, nil
}

func TestFetcherSSRFBlockRecordsExactDecision(t *testing.T) {
	store := &ssrfTestStore{}
	fetcher := NewDefaultConnectedContentFetcher(store, nil)

	result, err := fetcher.Fetch(context.Background(), domain.Principal{
		TenantID:    "tenant-1",
		WorkspaceID: "workspace-1",
	}, "http://127.0.0.1:8080/internal-data", 300)
	if err != nil {
		t.Fatalf("Fetch returned unexpected error: %v", err)
	}
	if result != nil {
		t.Fatalf("expected private-IP fetch to be blocked, got %#v", result)
	}
	if len(store.activities) != 1 {
		t.Fatalf("expected one audit activity, got %d", len(store.activities))
	}
	if got := store.activities[0].PolicyDecision; got != "ssrf_blocked" {
		t.Fatalf("expected exact SSRF audit decision %q, got %q", "ssrf_blocked", got)
	}
}
