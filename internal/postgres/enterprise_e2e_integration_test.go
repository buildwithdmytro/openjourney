package postgres

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"os"
	"testing"

	"github.com/buildwithdmytro/openjourney/internal/domain"
	"github.com/stretchr/testify/require"
)

// TestEnterpriseE2E composes the enterprise identity and governance paths in
// the order an administrator would use them. The SAML assertion crypto itself
// is covered by the HTTP SAML security checkpoint; this test verifies that a
// successful SAML session reaches the same team-derived authorization path as
// every other authenticated session.
func TestEnterpriseE2E(t *testing.T) {
	databaseURL := os.Getenv("OPENJOURNEY_TEST_DATABASE_URL")
	if databaseURL == "" {
		t.Skip("OPENJOURNEY_TEST_DATABASE_URL is not configured")
	}

	ctx := context.Background()
	store, err := Open(ctx, databaseURL)
	require.NoError(t, err)
	defer store.Close()
	require.NoError(t, store.Migrate(ctx))

	admin, _ := setupTestTenant(t, ctx, store)
	admin.Scopes = []string{"*"}
	role, err := store.CreateRole(ctx, admin, "enterprise-e2e-role", []string{"campaigns:write"})
	require.NoError(t, err)

	// Provision the SCIM credential and user, then map a SCIM group to a team
	// carrying the role.
	scimToken := "enterprise-e2e-scim-token"
	tokenHash := sha256.Sum256([]byte(scimToken))
	_, err = store.pool.Exec(ctx, `INSERT INTO scim_tokens (tenant_id, token_hash, description)
		VALUES ($1, $2, 'enterprise e2e')`, admin.TenantID, hex.EncodeToString(tokenHash[:]))
	require.NoError(t, err)
	scimPrincipal, err := store.AuthenticateSCIM(ctx, scimToken)
	require.NoError(t, err)
	scimUser, err := store.CreateSCIMUser(ctx, scimPrincipal, domain.User{
		OIDCSubject: "enterprise-user", Email: "enterprise@example.test",
	}, true)
	require.NoError(t, err)

	team, err := store.CreateSCIMGroup(ctx, scimPrincipal, domain.SCIMGroup{
		DisplayName: "Enterprise Operators",
		Members:     []domain.SCIMGroupMember{{Value: scimUser.ID}},
	})
	require.NoError(t, err)
	_, err = store.pool.Exec(ctx, `INSERT INTO team_roles (team_id, role_id) VALUES ($1, $2) ON CONFLICT DO NOTHING`, team.ID, role.ID)
	require.NoError(t, err)

	// The SAML provider maps the IdP entity ID and NameID into the existing
	// identity/session model. Add that federated identity to the SCIM team so
	// the minted bearer session must receive the team's campaign scope.
	provider, err := store.CreateSAMLProvider(ctx, admin, domain.SAMLProvider{
		IDPEntityID: "https://idp.enterprise.example",
		IDPSSOURL:   "https://idp.enterprise.example/sso",
		IDPCert:     "test-certificate",
		SPEntityID:  "https://openjourney.example/saml",
		Enabled:     true,
		Status:      "active",
	})
	require.NoError(t, err)
	samlSession, err := store.UpsertSAMLUserAndCreateSession(ctx, admin.TenantID, provider.IDPEntityID,
		"enterprise-user", "enterprise@example.test", "Enterprise User")
	require.NoError(t, err)
	samlPrincipal, err := store.Authenticate(ctx, samlSession.AccessToken)
	require.NoError(t, err)
	_, err = store.PatchSCIMGroup(ctx, scimPrincipal, team.ID, domain.SCIMGroupPatch{
		Operations: []domain.SCIMGroupOperation{{Op: "add", Value: []domain.SCIMGroupMember{{Value: samlPrincipal.UserID}}}},
	})
	require.NoError(t, err)
	samlPrincipal, err = store.Authenticate(ctx, samlSession.AccessToken)
	require.NoError(t, err)
	require.True(t, samlPrincipal.HasScope("campaigns:write"), "SAML session must inherit the SCIM team role")

	// Maker-checker is enforced using the authenticated principal, and a
	// separate SAML-authenticated principal can approve the draft.
	_, err = store.SetMakerCheckerPolicy(ctx, admin, "journeys", true)
	require.NoError(t, err)
	journey, err := store.CreateJourney(ctx, samlPrincipal, domain.Journey{Name: "Enterprise governed journey"})
	require.NoError(t, err)
	_, err = store.PublishJourney(ctx, samlPrincipal, journey.ID, samlPrincipal.UserID, "enterprise-manifest")
	require.ErrorIs(t, err, ErrSelfApproval)

	checkerSession, err := store.UpsertSAMLUserAndCreateSession(ctx, admin.TenantID, provider.IDPEntityID,
		"enterprise-checker", "checker@example.test", "Enterprise Checker")
	require.NoError(t, err)
	checker, err := store.Authenticate(ctx, checkerSession.AccessToken)
	require.NoError(t, err)
	_, err = store.PublishJourney(ctx, checker, journey.ID, checker.UserID, "enterprise-manifest")
	require.NoError(t, err)

	// DSR verification is required before processing; completion of both export
	// and erasure is auditable.
	exportReq, err := store.CreatePrivacyRequest(ctx, samlPrincipal, "enterprise-subject", "export")
	require.NoError(t, err)
	_, err = store.VerifyPrivacyRequest(ctx, samlPrincipal, exportReq.ID, exportReq.VerificationToken)
	require.NoError(t, err)
	require.NoError(t, store.CompletePrivacyExport(ctx, exportReq.ID, "exports/enterprise-subject.json"))

	deleteReq, err := store.CreatePrivacyRequest(ctx, samlPrincipal, "enterprise-subject", "delete")
	require.NoError(t, err)
	_, err = store.VerifyPrivacyRequest(ctx, samlPrincipal, deleteReq.ID, deleteReq.VerificationToken)
	require.NoError(t, err)
	_, err = store.DeletePrivacyData(ctx, deleteReq.ID)
	require.NoError(t, err)

	events, err := store.ListAuditEvents(ctx, samlPrincipal, 200)
	require.NoError(t, err)
	actions := make(map[string]bool, len(events))
	for _, event := range events {
		actions[event.Action] = true
	}
	for _, action := range []string{"scim.user.create", "scim.group.create", "saml.login", "journey.publish", "privacy.export.complete", "privacy.delete.complete"} {
		require.Truef(t, actions[action], "missing enterprise audit action %q", action)
	}
}
