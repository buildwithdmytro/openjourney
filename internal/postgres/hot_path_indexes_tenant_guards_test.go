package postgres

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func readPostgresSourceForHotPathTest(t *testing.T, name string) string {
	t.Helper()
	path := filepath.Join("internal", "postgres", name)
	if _, err := os.Stat(path); err != nil {
		path = name
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", name, err)
	}
	return string(data)
}

func TestHotPathIndexesAndTenantGuards(t *testing.T) {
	migration := readPostgresSourceForHotPathTest(t, "migrations/066_hot_path_indexes_tenant_guards.sql")
	for _, want := range []string{
		"CREATE INDEX IF NOT EXISTS inapp_messages_expiry_idx",
		"ON inapp_messages (tenant_id, expires_at)",
		"CREATE INDEX IF NOT EXISTS inapp_messages_admin_list_idx",
		"ON inapp_messages (tenant_id, workspace_id, app_id, created_at DESC)",
		"CREATE INDEX IF NOT EXISTS feature_flags_workspace_list_idx",
		"ON feature_flags (tenant_id, workspace_id, environment, key)",
	} {
		if !strings.Contains(migration, want) {
			t.Errorf("migration missing %q", want)
		}
	}

	saml := readPostgresSourceForHotPathTest(t, "saml.go")
	if !strings.Contains(saml, "UPDATE users SET email=COALESCE(NULLIF($1,''), email)") ||
		!strings.Contains(saml, "WHERE tenant_id=$3 AND id=$4`") {
		t.Error("SAML user update is missing its in-statement tenant guard")
	}

	scim := readPostgresSourceForHotPathTest(t, "scim.go")
	if strings.Count(scim, "DELETE FROM team_members WHERE team_id") != 0 {
		t.Error("SCIM membership delete still relies on an unscoped team_id predicate")
	}
	for _, want := range []string{
		"DELETE FROM team_members tm USING teams t",
		"t.tenant_id = $1 AND tm.team_id = $2",
		"t.tenant_id = $1 AND tm.team_id = $2 AND tm.user_id = $3",
	} {
		if !strings.Contains(scim, want) {
			t.Errorf("SCIM delete is missing %q", want)
		}
	}
}
