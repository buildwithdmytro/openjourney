package postgres

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestAppendOnlyVersionTables_MigrationSQL is a non-gated test that asserts
// migration 063 contains the required triggers and REVOKE statements for F5 and F6.
func TestAppendOnlyVersionTables_MigrationSQL(t *testing.T) {
	migPath := filepath.Join("migrations", "063_append_only_version_tables.sql")
	data, err := os.ReadFile(migPath)
	if err != nil {
		migPath = filepath.Join("internal", "postgres", "migrations", "063_append_only_version_tables.sql")
		data, err = os.ReadFile(migPath)
		if err != nil {
			t.Fatalf("failed to read 063 migration: %v", err)
		}
	}
	content := string(data)

	// F5 checks: prompt_versions + scoring_model_versions must have trigger + REVOKE
	requiredChecks := []struct {
		table      string
		mustContain []string
	}{
		{
			table: "prompt_versions",
			mustContain: []string{
				"prompt_versions_block_mutation",
				"BEFORE UPDATE OR DELETE ON prompt_versions",
				"REVOKE UPDATE, DELETE ON prompt_versions FROM PUBLIC",
			},
		},
		{
			table: "scoring_model_versions",
			mustContain: []string{
				"scoring_model_versions_block_mutation",
				"BEFORE UPDATE OR DELETE ON scoring_model_versions",
				"REVOKE UPDATE, DELETE ON scoring_model_versions FROM PUBLIC",
			},
		},
		{
			table: "ai_activity",
			mustContain: []string{
				"REVOKE UPDATE, DELETE ON ai_activity FROM PUBLIC",
			},
		},
		{
			table: "identity_merges",
			mustContain: []string{
				"REVOKE UPDATE, DELETE ON identity_merges FROM PUBLIC",
			},
		},
		{
			table: "experiment_versions",
			mustContain: []string{
				"REVOKE UPDATE, DELETE ON experiment_versions FROM PUBLIC",
			},
		},
	}

	for _, check := range requiredChecks {
		for _, substr := range check.mustContain {
			if !strings.Contains(content, substr) {
				t.Errorf("migration 063 missing required protection for %s: expected to find %q", check.table, substr)
			}
		}
	}
}

// TestAppendOnlyVersionTables_DBIntegration exercises the trigger guards and RTBF erasure
// path against a real Postgres database when OPENJOURNEY_TEST_DATABASE_URL is set.
func TestAppendOnlyVersionTables_DBIntegration(t *testing.T) {
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

	// Helper to insert minimal test tenant and workspace
	var tenantID, workspaceID string
	err = store.pool.QueryRow(ctx, `
		INSERT INTO tenants (name) VALUES ('append_only_test')
		RETURNING id::text
	`).Scan(&tenantID)
	if err != nil {
		t.Fatalf("inserting test tenant failed: %v", err)
	}

	err = store.pool.QueryRow(ctx, `
		INSERT INTO workspaces (tenant_id, name) VALUES ($1::uuid, 'append_only_ws')
		RETURNING id::text
	`, tenantID).Scan(&workspaceID)
	if err != nil {
		t.Fatalf("inserting test workspace failed: %v", err)
	}

	// 1. prompt_versions trigger test
	t.Run("prompt_versions append-only", func(t *testing.T) {
		var promptID string
		err := store.pool.QueryRow(ctx, `
			INSERT INTO prompts (tenant_id, workspace_id, name, task_type)
			VALUES ($1::uuid, $2::uuid, 'test_prompt', 'content_draft')
			RETURNING id::text
		`, tenantID, workspaceID).Scan(&promptID)
		if err != nil {
			t.Fatalf("inserting prompt failed: %v", err)
		}

		var pvID string
		err = store.pool.QueryRow(ctx, `
			INSERT INTO prompt_versions (prompt_id, tenant_id, version, template, input_schema, output_schema, provider, model, manifest_key)
			VALUES ($1::uuid, $2::uuid, 1, 'tpl', '{}', '{}', 'prov', 'mod', 'mk')
			RETURNING id::text
		`, promptID, tenantID).Scan(&pvID)
		if err != nil {
			t.Fatalf("inserting prompt_version failed: %v", err)
		}

		// Attempt UPDATE
		_, err = store.pool.Exec(ctx, `UPDATE prompt_versions SET template='modified' WHERE id=$1::uuid`, pvID)
		if err == nil || !strings.Contains(err.Error(), "prompt_versions is append-only") {
			t.Errorf("expected UPDATE on prompt_versions to be rejected with append-only error, got: %v", err)
		}

		// Attempt DELETE
		_, err = store.pool.Exec(ctx, `DELETE FROM prompt_versions WHERE id=$1::uuid`, pvID)
		if err == nil || !strings.Contains(err.Error(), "prompt_versions is append-only") {
			t.Errorf("expected DELETE on prompt_versions to be rejected with append-only error, got: %v", err)
		}
	})

	// 2. scoring_model_versions trigger test
	t.Run("scoring_model_versions append-only", func(t *testing.T) {
		var modelID string
		err := store.pool.QueryRow(ctx, `
			INSERT INTO scoring_models (tenant_id, workspace_id, name, kind)
			VALUES ($1::uuid, $2::uuid, 'test_scoring_model', 'expression')
			RETURNING id::text
		`, tenantID, workspaceID).Scan(&modelID)
		if err != nil {
			t.Fatalf("inserting scoring model failed: %v", err)
		}

		var smvID string
		err = store.pool.QueryRow(ctx, `
			INSERT INTO scoring_model_versions (scoring_model_id, tenant_id, version, score_name, definition, manifest_key)
			VALUES ($1::uuid, $2::uuid, 1, 'score1', '{}', 'mk')
			RETURNING id::text
		`, modelID, tenantID).Scan(&smvID)
		if err != nil {
			t.Fatalf("inserting scoring_model_version failed: %v", err)
		}

		// Attempt UPDATE
		_, err = store.pool.Exec(ctx, `UPDATE scoring_model_versions SET score_name='mod' WHERE id=$1::uuid`, smvID)
		if err == nil || !strings.Contains(err.Error(), "scoring_model_versions is append-only") {
			t.Errorf("expected UPDATE on scoring_model_versions to be rejected with append-only error, got: %v", err)
		}

		// Attempt DELETE
		_, err = store.pool.Exec(ctx, `DELETE FROM scoring_model_versions WHERE id=$1::uuid`, smvID)
		if err == nil || !strings.Contains(err.Error(), "scoring_model_versions is append-only") {
			t.Errorf("expected DELETE on scoring_model_versions to be rejected with append-only error, got: %v", err)
		}
	})

	// 3. identity_merges RTBF erasure and append-only trigger test
	t.Run("identity_merges append-only and RTBF erasure", func(t *testing.T) {
		var mergeID string
		err := store.pool.QueryRow(ctx, `
			INSERT INTO identity_merges (tenant_id, app_id, source_event_id, source_profile_id, target_profile_id, policy_version, winner_policy, actor_type)
			VALUES ($1::uuid, gen_random_uuid(), gen_random_uuid(), gen_random_uuid(), gen_random_uuid(), '1.0', 'deterministic', 'system')
			RETURNING id::text
		`, tenantID).Scan(&mergeID)
		if err != nil {
			t.Fatalf("inserting identity_merge failed: %v", err)
		}

		// Disallowed UPDATE (changing policy_version)
		_, err = store.pool.Exec(ctx, `UPDATE identity_merges SET policy_version='2.0' WHERE id=$1::uuid`, mergeID)
		if err == nil || !strings.Contains(err.Error(), "identity_merges is append-only") {
			t.Errorf("expected disallowed UPDATE on identity_merges to fail, got: %v", err)
		}

		// Allowed UPDATE (updating undone_at)
		_, err = store.pool.Exec(ctx, `UPDATE identity_merges SET undone_at=now() WHERE id=$1::uuid`, mergeID)
		if err != nil {
			t.Errorf("expected allowed undone_at UPDATE on identity_merges to succeed, got: %v", err)
		}

		// Disallowed DELETE without erasure GUC
		_, err = store.pool.Exec(ctx, `DELETE FROM identity_merges WHERE id=$1::uuid`, mergeID)
		if err == nil || !strings.Contains(err.Error(), "identity_merges can only be deleted during RTBF erasure") {
			t.Errorf("expected DELETE without erasure GUC to fail, got: %v", err)
		}

		// Allowed DELETE with openjourney.erasure='on'
		tx, err := store.pool.Begin(ctx)
		if err != nil {
			t.Fatalf("Begin tx failed: %v", err)
		}
		defer tx.Rollback(ctx)

		if _, err := tx.Exec(ctx, `SET LOCAL openjourney.erasure = 'on'`); err != nil {
			t.Fatalf("SET LOCAL openjourney.erasure failed: %v", err)
		}

		if _, err := tx.Exec(ctx, `DELETE FROM identity_merges WHERE id=$1::uuid`, mergeID); err != nil {
			t.Errorf("expected DELETE with erasure GUC='on' to succeed, got: %v", err)
		}

		if err := tx.Commit(ctx); err != nil {
			t.Fatalf("Commit tx failed: %v", err)
		}
	})
}
