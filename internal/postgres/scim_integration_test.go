package postgres

import (
	"context"
	"fmt"
	"os"
	"testing"

	"github.com/buildwithdmytro/openjourney/internal/domain"
	"github.com/stretchr/testify/require"
)

func TestSCIMGroupsProvisionTeamsAndSyncScopes(t *testing.T) {
	databaseURL := os.Getenv("OPENJOURNEY_TEST_DATABASE_URL")
	if databaseURL == "" {
		t.Skip("OPENJOURNEY_TEST_DATABASE_URL is not configured")
	}

	ctx := context.Background()
	store, err := Open(ctx, databaseURL)
	require.NoError(t, err)
	defer store.Close()
	require.NoError(t, store.Migrate(ctx))

	p, _ := setupTestTenant(t, ctx, store)
	role, err := store.CreateRole(ctx, p, "scim-test-role", []string{"campaigns:write"})
	require.NoError(t, err)

	user, err := store.CreateUser(ctx, p, domain.User{
		OIDCIssuer: "scim-test-issuer", OIDCSubject: fmt.Sprintf("%s-subject", t.Name()), Email: "scim-member@example.test",
	})
	require.NoError(t, err)

	team, err := store.CreateTeam(ctx, p, domain.Team{
		Name: "Engineering", RoleIDs: []string{role.ID},
	})
	require.NoError(t, err)

	// User not yet in team -> does not have campaigns:write
	principal, err := store.AuthenticateOIDC(ctx, domain.OIDCClaims{
		Issuer: "scim-test-issuer", Subject: fmt.Sprintf("%s-subject", t.Name()),
		TenantID: p.TenantID, WorkspaceID: p.WorkspaceID, AppID: p.AppID,
	})
	require.NoError(t, err)
	require.False(t, principal.HasScope("campaigns:write"))

	// Provision SCIM Group matching team name with user as member
	scimPrincipal := domain.Principal{TenantID: p.TenantID, WorkspaceID: p.WorkspaceID, ActorType: "scim"}
	scimGroup, err := store.CreateSCIMGroup(ctx, scimPrincipal, domain.SCIMGroup{
		DisplayName: "Engineering",
		Members:     []domain.SCIMGroupMember{{Value: user.ID}},
	})
	require.NoError(t, err)
	require.Equal(t, team.ID, scimGroup.ID)
	require.Len(t, scimGroup.Members, 1)

	// User now in team via SCIM -> inherits campaigns:write
	principal, err = store.AuthenticateOIDC(ctx, domain.OIDCClaims{
		Issuer: "scim-test-issuer", Subject: fmt.Sprintf("%s-subject", t.Name()),
		TenantID: p.TenantID, WorkspaceID: p.WorkspaceID, AppID: p.AppID,
	})
	require.NoError(t, err)
	require.True(t, principal.HasScope("campaigns:write"))

	// Patch SCIM Group to remove user
	_, err = store.PatchSCIMGroup(ctx, scimPrincipal, scimGroup.ID, domain.SCIMGroupPatch{
		Operations: []domain.SCIMGroupOperation{
			{Op: "remove", Value: []domain.SCIMGroupMember{{Value: user.ID}}},
		},
	})
	require.NoError(t, err)

	// User no longer in team -> campaigns:write revoked
	principal, err = store.AuthenticateOIDC(ctx, domain.OIDCClaims{
		Issuer: "scim-test-issuer", Subject: fmt.Sprintf("%s-subject", t.Name()),
		TenantID: p.TenantID, WorkspaceID: p.WorkspaceID, AppID: p.AppID,
	})
	require.NoError(t, err)
	require.False(t, principal.HasScope("campaigns:write"))
}
