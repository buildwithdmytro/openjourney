package postgres

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestTruncateGuardAuditTables_MigrationSQL is a non-gated test that asserts
// migration 064 contains the required BEFORE TRUNCATE triggers for audit_events
// and all append-only tables (F7).
func TestTruncateGuardAuditTables_MigrationSQL(t *testing.T) {
	migPath := filepath.Join("migrations", "064_truncate_guard_audit_tables.sql")
	data, err := os.ReadFile(migPath)
	if err != nil {
		migPath = filepath.Join("internal", "postgres", "migrations", "064_truncate_guard_audit_tables.sql")
		data, err = os.ReadFile(migPath)
		if err != nil {
			t.Fatalf("failed to read 064 migration: %v", err)
		}
	}
	content := string(data)

	requiredTables := []string{
		"audit_events",
		"connector_runs",
		"ai_activity",
		"identity_merges",
		"experiment_versions",
		"prompt_versions",
		"scoring_model_versions",
		"ai_agent_runs",
		"extension_activity",
		"metric_definitions",
		"connector_pipeline_versions",
		"feature_flag_versions",
	}

	if !strings.Contains(content, "CREATE OR REPLACE FUNCTION append_only_block_truncate()") {
		t.Errorf("migration 064 missing append_only_block_truncate function definition")
	}

	for _, tbl := range requiredTables {
		triggerCheck := "BEFORE TRUNCATE ON " + tbl
		if !strings.Contains(content, triggerCheck) {
			t.Errorf("migration 064 missing BEFORE TRUNCATE trigger for table %s: expected to find %q", tbl, triggerCheck)
		}
	}
}

// TestTruncateGuardAuditTables_DBIntegration verifies that TRUNCATE statements on audit_events
// and other append-only tables are rejected by PostgreSQL when OPENJOURNEY_TEST_DATABASE_URL is set.
func TestTruncateGuardAuditTables_DBIntegration(t *testing.T) {
	databaseURL := os.Getenv("OPENJOURNEY_TEST_DATABASE_URL")
	if databaseURL == "" {
		t.Skip("OPENJOURNEY_TEST_DATABASE_URL is not configured")
	}

	ctx := context.Background()
	store, err := Open(ctx, databaseURL)
	if err != nil {
		t.Fatalf("Open failed: %v", err)
	}
	defer store.Close()

	if err := store.Migrate(ctx); err != nil {
		t.Fatalf("Migrate failed: %v", err)
	}

	tablesToTest := []string{
		"audit_events",
		"connector_runs",
		"ai_activity",
		"identity_merges",
		"experiment_versions",
		"prompt_versions",
		"scoring_model_versions",
		"ai_agent_runs",
		"extension_activity",
		"metric_definitions",
		"connector_pipeline_versions",
		"feature_flag_versions",
	}

	for _, tbl := range tablesToTest {
		t.Run(tbl+" TRUNCATE rejection", func(t *testing.T) {
			_, err := store.pool.Exec(ctx, "TRUNCATE "+tbl)
			if err == nil {
				t.Errorf("expected TRUNCATE %s to be rejected, but it succeeded", tbl)
			} else if !strings.Contains(err.Error(), "append-only") && !strings.Contains(err.Error(), "cannot be truncated") {
				t.Errorf("expected error for TRUNCATE %s to mention append-only, got: %v", tbl, err)
			}
		})
	}
}
