package postgres

import (
	"context"
	"os"
	"strings"
	"testing"
)

func TestAnalyticsFactsMigrationIntegration(t *testing.T) {
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

	for _, table := range []string{"engagement_facts", "conversion_facts"} {
		var exists bool
		if err := store.pool.QueryRow(ctx, `SELECT to_regclass('public.' || $1) IS NOT NULL`, table).Scan(&exists); err != nil {
			t.Fatal(err)
		}
		if !exists {
			t.Errorf("table %s does not exist", table)
		}
	}

	columns := map[string][]string{
		"campaigns":        {"conversion_goal", "attribution_window"},
		"journey_versions": {"conversion_goal", "attribution_window"},
		"experiments":      {"primary_goal"},
	}
	for table, names := range columns {
		for _, name := range names {
			var dataType string
			if err := store.pool.QueryRow(ctx, `
				SELECT data_type FROM information_schema.columns
				WHERE table_schema='public' AND table_name=$1 AND column_name=$2`, table, name).Scan(&dataType); err != nil {
				t.Fatalf("column %s.%s: %v", table, name, err)
			}
			if name == "attribution_window" && dataType != "interval" {
				t.Errorf("%s.%s type = %s, want interval", table, name, dataType)
			}
			if name != "attribution_window" && dataType != "jsonb" {
				t.Errorf("%s.%s type = %s, want jsonb", table, name, dataType)
			}
		}
	}

	checks := map[string][]string{
		"engagement_facts_source_type_check": {"campaign", "journey"},
		"engagement_facts_event_type_check":  {"delivered", "opened", "clicked", "bounced", "complained"},
		"conversion_facts_source_type_check": {"campaign", "journey"},
	}
	for constraint, values := range checks {
		var definition string
		if err := store.pool.QueryRow(ctx, `SELECT pg_get_constraintdef(oid) FROM pg_constraint WHERE conname=$1`, constraint).Scan(&definition); err != nil {
			t.Fatalf("constraint %s: %v", constraint, err)
		}
		for _, value := range values {
			if !strings.Contains(definition, "'"+value+"'") {
				t.Errorf("constraint %s does not enumerate %q: %s", constraint, value, definition)
			}
		}
	}

	for _, constraint := range []string{"engagement_facts_source_event_id_event_type_key", "conversion_facts_source_event_id_goal_name_key"} {
		var definition string
		if err := store.pool.QueryRow(ctx, `SELECT pg_get_constraintdef(oid) FROM pg_constraint WHERE conname=$1`, constraint).Scan(&definition); err != nil {
			t.Fatalf("idempotency constraint %s: %v", constraint, err)
		}
		if !strings.HasPrefix(definition, "UNIQUE") {
			t.Errorf("constraint %s = %s, want UNIQUE", constraint, definition)
		}
	}

	for _, index := range []string{"engagement_facts_report_idx", "conversion_facts_report_idx"} {
		var exists bool
		if err := store.pool.QueryRow(ctx, `SELECT to_regclass('public.' || $1) IS NOT NULL`, index).Scan(&exists); err != nil {
			t.Fatal(err)
		}
		if !exists {
			t.Errorf("index %s does not exist", index)
		}
	}
}
