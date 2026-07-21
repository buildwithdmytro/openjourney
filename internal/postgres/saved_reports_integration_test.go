package postgres

import (
	"context"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/buildwithdmytro/openjourney/internal/domain"
)

func TestSavedReportsCRUDAndWorkspaceIsolation(t *testing.T) {
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

	key := fmt.Sprintf("saved-reports-%d", time.Now().UnixNano())
	if err := store.EnsureDevelopmentTenant(ctx, key); err != nil {
		t.Fatal(err)
	}
	p, err := store.Authenticate(ctx, key)
	if err != nil {
		t.Fatal(err)
	}

	// Create a saved report
	createInput := domain.SavedReport{
		Name:       "Campaign Q1 Analysis",
		ReportType: "funnel",
		Query: domain.ReportQuery{
			Start:       time.Now().AddDate(0, -3, 0),
			End:         time.Now(),
			Granularity: "week",
			Dimensions:  []string{"channel", "variant"},
			Filters:     map[string]string{"channel": "email"},
		},
	}

	created, err := store.CreateSavedReport(ctx, p, createInput)
	if err != nil {
		t.Fatalf("create saved report: %v", err)
	}
	if created.ID == "" {
		t.Fatal("created report should have an ID")
	}
	if created.Name != createInput.Name {
		t.Fatalf("expected name %q, got %q", createInput.Name, created.Name)
	}
	if created.ReportType != createInput.ReportType {
		t.Fatalf("expected report_type %q, got %q", createInput.ReportType, created.ReportType)
	}

	// Get the saved report
	fetched, err := store.GetSavedReport(ctx, p, created.ID)
	if err != nil {
		t.Fatalf("get saved report: %v", err)
	}
	if fetched.ID != created.ID {
		t.Fatalf("expected ID %q, got %q", created.ID, fetched.ID)
	}

	// List saved reports
	list, err := store.ListSavedReports(ctx, p)
	if err != nil {
		t.Fatalf("list saved reports: %v", err)
	}
	if len(list) != 1 {
		t.Fatalf("expected 1 report, got %d", len(list))
	}

	// Create another report
	createInput2 := domain.SavedReport{
		Name:       "Retention Analysis",
		ReportType: "retention",
		Query: domain.ReportQuery{
			Start:       time.Now().AddDate(0, -6, 0),
			End:         time.Now(),
			Granularity: "month",
		},
	}
	_, err = store.CreateSavedReport(ctx, p, createInput2)
	if err != nil {
		t.Fatalf("create second saved report: %v", err)
	}

	// List again should return 2
	list, err = store.ListSavedReports(ctx, p)
	if err != nil {
		t.Fatalf("list saved reports again: %v", err)
	}
	if len(list) != 2 {
		t.Fatalf("expected 2 reports, got %d", len(list))
	}

	// Delete a saved report
	err = store.DeleteSavedReport(ctx, p, created.ID)
	if err != nil {
		t.Fatalf("delete saved report: %v", err)
	}

	// Verify it's gone
	_, err = store.GetSavedReport(ctx, p, created.ID)
	if err != ErrNotFound {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}

	// List should now return 1
	list, err = store.ListSavedReports(ctx, p)
	if err != nil {
		t.Fatalf("list after delete: %v", err)
	}
	if len(list) != 1 {
		t.Fatalf("expected 1 report after delete, got %d", len(list))
	}

	// Delete non-existent should return ErrNotFound
	err = store.DeleteSavedReport(ctx, p, "non-existent-id")
	if err != ErrNotFound {
		t.Fatalf("expected ErrNotFound for non-existent, got %v", err)
	}

	// Test workspace isolation
	var ws2ID string
	if err := store.pool.QueryRow(ctx, `INSERT INTO workspaces (tenant_id,name)
		VALUES ($1,'Workspace 2') RETURNING id`, p.TenantID).Scan(&ws2ID); err != nil {
		t.Fatalf("create workspace 2: %v", err)
	}

	p2 := domain.Principal{
		TenantID:    p.TenantID,
		WorkspaceID: ws2ID,
		UserID:      p.UserID,
	}

	// Create a report in workspace 2
	createInput3 := domain.SavedReport{
		Name:       "Report in WS2",
		ReportType: "cost",
		Query:      domain.ReportQuery{},
	}
	created3, err := store.CreateSavedReport(ctx, p2, createInput3)
	if err != nil {
		t.Fatalf("create report in ws2: %v", err)
	}

	// Principal 1 should not see workspace 2's report
	_, err = store.GetSavedReport(ctx, p, created3.ID)
	if err != ErrNotFound {
		t.Fatalf("expected ErrNotFound for cross-workspace access, got %v", err)
	}

	// But principal 2 should see it
	_, err = store.GetSavedReport(ctx, p2, created3.ID)
	if err != nil {
		t.Fatalf("get report in ws2: %v", err)
	}
}

func TestSavedReportNameUniqueness(t *testing.T) {
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

	key := fmt.Sprintf("saved-reports-unique-%d", time.Now().UnixNano())
	if err := store.EnsureDevelopmentTenant(ctx, key); err != nil {
		t.Fatal(err)
	}
	p, err := store.Authenticate(ctx, key)
	if err != nil {
		t.Fatal(err)
	}

	// Create a saved report with a specific name
	createInput := domain.SavedReport{
		Name:       "Unique Report Name",
		ReportType: "funnel",
		Query:      domain.ReportQuery{},
	}
	_, err = store.CreateSavedReport(ctx, p, createInput)
	if err != nil {
		t.Fatalf("create first report: %v", err)
	}

	// Try to create another with the same name in the same workspace
	_, err = store.CreateSavedReport(ctx, p, createInput)
	if err == nil {
		t.Fatal("expected error for duplicate name, got nil")
	}
	if err.Error() != "saved report name already exists in this workspace" {
		t.Fatalf("unexpected error message: %v", err)
	}
}

func TestSavedReportQueryRoundTrip(t *testing.T) {
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

	key := fmt.Sprintf("saved-reports-query-%d", time.Now().UnixNano())
	if err := store.EnsureDevelopmentTenant(ctx, key); err != nil {
		t.Fatal(err)
	}
	p, err := store.Authenticate(ctx, key)
	if err != nil {
		t.Fatal(err)
	}

	// Create with a complex query
	startTime := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	endTime := time.Date(2024, 12, 31, 23, 59, 59, 0, time.UTC)

	createInput := domain.SavedReport{
		Name:       "Complex Query Report",
		ReportType: "growth",
		Query: domain.ReportQuery{
			Start:       startTime,
			End:         endTime,
			Granularity: "day",
			Dimensions:  []string{"channel", "provider"},
			Filters: map[string]string{
				"channel":  "sms",
				"provider": "twilio",
			},
		},
	}

	created, err := store.CreateSavedReport(ctx, p, createInput)
	if err != nil {
		t.Fatalf("create report: %v", err)
	}

	// Fetch and verify the query is preserved
	fetched, err := store.GetSavedReport(ctx, p, created.ID)
	if err != nil {
		t.Fatalf("get report: %v", err)
	}

	if fetched.Query.Start != startTime {
		t.Fatalf("expected start %v, got %v", startTime, fetched.Query.Start)
	}
	if fetched.Query.End != endTime {
		t.Fatalf("expected end %v, got %v", endTime, fetched.Query.End)
	}
	if fetched.Query.Granularity != "day" {
		t.Fatalf("expected granularity 'day', got %q", fetched.Query.Granularity)
	}
	if len(fetched.Query.Dimensions) != 2 {
		t.Fatalf("expected 2 dimensions, got %d", len(fetched.Query.Dimensions))
	}
	if fetched.Query.Filters["channel"] != "sms" {
		t.Fatalf("expected channel 'sms', got %q", fetched.Query.Filters["channel"])
	}
}
