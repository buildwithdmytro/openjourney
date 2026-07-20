package postgres

import (
	"context"
	"fmt"
	"os"
	"sync"
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

func TestClaimDueConnectorPipelineEnqueuesOnce(t *testing.T) {
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
	key := fmt.Sprintf("scheduler-%d", time.Now().UnixNano())
	if err := store.EnsureDevelopmentTenant(ctx, key); err != nil {
		t.Fatal(err)
	}
	p, err := store.Authenticate(ctx, key)
	if err != nil {
		t.Fatal(err)
	}
	ext, err := store.CreateExtension(ctx, p, domain.Extension{Name: "scheduler-connector-" + key, Publisher: "test"})
	if err != nil {
		t.Fatal(err)
	}
	interval := 60
	pipeline, err := store.CreateConnectorPipeline(ctx, p, domain.ConnectorPipeline{
		AppID: p.AppID, ConnectorExtensionID: ext.ID, Name: "pipeline-" + key,
		Direction: "source", Status: "enabled", ScheduleEnabled: true,
		ScheduleIntervalSeconds: &interval,
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.pool.Exec(ctx, `UPDATE connector_pipelines SET status='enabled', next_run_at=now()-interval '1 second' WHERE id=$1`, pipeline.ID); err != nil {
		t.Fatal(err)
	}

	results := make(chan bool, 2)
	var wg sync.WaitGroup
	for range 2 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			found, claimErr := store.ClaimDueConnectorPipeline(ctx)
			if claimErr != nil {
				t.Errorf("claim due pipeline: %v", claimErr)
			}
			results <- found
		}()
	}
	wg.Wait()
	close(results)
	claimed := 0
	for found := range results {
		if found {
			claimed++
		}
	}
	if claimed != 1 {
		t.Fatalf("claimed %d times, want exactly once", claimed)
	}
	var jobs int
	if err := store.pool.QueryRow(ctx, `SELECT count(*) FROM operation_jobs WHERE workspace_id=$1 AND job_type='warehouse.sync' AND payload->>'pipeline_id'=$2`, p.WorkspaceID, pipeline.ID).Scan(&jobs); err != nil {
		t.Fatal(err)
	}
	if jobs != 1 {
		t.Fatalf("enqueued %d jobs, want one", jobs)
	}
	var nextRun time.Time
	if err := store.pool.QueryRow(ctx, `SELECT next_run_at FROM connector_pipelines WHERE id=$1`, pipeline.ID).Scan(&nextRun); err != nil {
		t.Fatal(err)
	}
	if !nextRun.After(time.Now()) {
		t.Fatalf("next_run_at did not advance: %s", nextRun)
	}
}
