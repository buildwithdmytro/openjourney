package postgres

import (
	"context"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/buildwithdmytro/openjourney/internal/domain"
)

func TestConnectorPipelineStoreRoundTrip(t *testing.T) {
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

	key := fmt.Sprintf("connector-pipeline-%d", time.Now().UnixNano())
	if err := store.EnsureDevelopmentTenant(ctx, key); err != nil {
		t.Fatal(err)
	}
	p, err := store.Authenticate(ctx, key)
	if err != nil {
		t.Fatal(err)
	}
	ext, err := store.CreateExtension(ctx, p, domain.Extension{Name: "connector-" + key, Publisher: "test"})
	if err != nil {
		t.Fatal(err)
	}
	want := domain.ConnectorPipeline{
		AppID: p.AppID, ConnectorExtensionID: ext.ID, Name: "pipeline-" + key,
		Direction: "source", ScheduleEnabled: true,
	}
	created, err := store.CreateConnectorPipeline(ctx, p, want)
	if err != nil {
		t.Fatal(err)
	}
	got, err := store.GetConnectorPipeline(ctx, p, created.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.ID != created.ID || got.Name != want.Name || got.Direction != want.Direction || !got.ScheduleEnabled {
		t.Fatalf("pipeline round trip mismatch: got %+v, want %+v", got, want)
	}
	list, err := store.ListConnectorPipelines(ctx, p)
	if err != nil {
		t.Fatal(err)
	}
	if len(list) == 0 {
		t.Fatal("pipeline missing from list")
	}
}
