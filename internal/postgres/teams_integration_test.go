package postgres

import (
	"context"
	"fmt"
	"os"
	"testing"

	"github.com/buildwithdmytro/openjourney/internal/domain"
	"github.com/stretchr/testify/require"
)

func TestTeamRoundTripAndTenantIsolation(t *testing.T) {
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
	other, _ := setupTestTenant(t, ctx, store)
	role, err := store.CreateRole(ctx, p, "team-reader", []string{"profiles:read"})
	require.NoError(t, err)
	user, err := store.CreateUser(ctx, other, domain.User{OIDCIssuer: "test", OIDCSubject: fmt.Sprintf("%s-user", t.Name()), Email: "team-member@example.test"})
	require.NoError(t, err)
	otherRole, err := store.CreateRole(ctx, other, "other-role", []string{"profiles:read"})
	require.NoError(t, err)

	_, err = store.CreateTeam(ctx, p, domain.Team{Name: "cross-tenant", MemberIDs: []string{user.ID}, RoleIDs: []string{otherRole.ID}})
	require.Error(t, err)

	localUser, err := store.CreateUser(ctx, p, domain.User{OIDCIssuer: "test", OIDCSubject: fmt.Sprintf("%s-local-user", t.Name()), Email: "local-member@example.test"})
	require.NoError(t, err)
	team, err := store.CreateTeam(ctx, p, domain.Team{Name: "Growth", Description: "Growth operators", MemberIDs: []string{localUser.ID}, RoleIDs: []string{role.ID}})
	require.NoError(t, err)
	require.Equal(t, p.TenantID, team.TenantID)
	require.Equal(t, p.WorkspaceID, team.WorkspaceID)
	require.Equal(t, []string{localUser.ID}, team.MemberIDs)
	require.Equal(t, []string{role.ID}, team.RoleIDs)

	got, err := store.GetTeam(ctx, p, team.ID)
	require.NoError(t, err)
	require.Equal(t, team, got)
	listed, err := store.ListTeams(ctx, p)
	require.NoError(t, err)
	require.Contains(t, listed, team)

	team.Name = "Growth Updated"
	team.MemberIDs = nil
	updated, err := store.UpdateTeam(ctx, p, team)
	require.NoError(t, err)
	require.Equal(t, "Growth Updated", updated.Name)
	require.Empty(t, updated.MemberIDs)
	require.NoError(t, store.DeleteTeam(ctx, p, team.ID))
}
