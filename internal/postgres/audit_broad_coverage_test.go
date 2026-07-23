package postgres

import (
	"context"
	"os"
	"strings"
	"testing"

	"github.com/buildwithdmytro/openjourney/internal/domain"
)

func TestGovernedMutationsBroadenAuditCoverage(t *testing.T) {
	databaseURL := os.Getenv("OPENJOURNEY_TEST_DATABASE_URL")
	if databaseURL == "" {
		t.Skip("OPENJOURNEY_TEST_DATABASE_URL is not configured")
	}

	ctx := context.Background()
	store, err := Open(ctx, databaseURL)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer store.Close()

	if err := store.Migrate(ctx); err != nil {
		t.Fatalf("Migrate: %v", err)
	}

	p, _ := setupTestTenant(t, ctx, store)

	// 1. Role mutations
	role, err := store.CreateRole(ctx, p, "AuditRoleTest", []string{"journeys:read"})
	if err != nil {
		t.Fatalf("CreateRole: %v", err)
	}
	role, err = store.UpdateRole(ctx, p, role.ID, "AuditRoleTestUpdated", []string{"journeys:read", "journeys:write"})
	if err != nil {
		t.Fatalf("UpdateRole: %v", err)
	}
	if err := store.DeleteRole(ctx, p, role.ID); err != nil {
		t.Fatalf("DeleteRole: %v", err)
	}

	// 2. Team mutations
	team, err := store.CreateTeam(ctx, p, domain.Team{Name: "AuditTeamTest", Description: "Test team"})
	if err != nil {
		t.Fatalf("CreateTeam: %v", err)
	}
	team, err = store.UpdateTeam(ctx, p, domain.Team{ID: team.ID, Name: "AuditTeamTestUpdated", Description: "Updated"})
	if err != nil {
		t.Fatalf("UpdateTeam: %v", err)
	}
	if err := store.DeleteTeam(ctx, p, team.ID); err != nil {
		t.Fatalf("DeleteTeam: %v", err)
	}

	// 3. User & SCIM / SAML mutations
	r, _ := store.CreateRole(ctx, p, "UserRole", []string{"*"})
	u, err := store.CreateUser(ctx, p, domain.User{
		Email:       "audituser@example.com",
		Password:    "SecretPass123!",
		DisplayName: "Audit User",
		RoleIDs:     []string{r.ID},
	})
	if err != nil {
		t.Fatalf("CreateUser: %v", err)
	}

	samlProvider, err := store.CreateSAMLProvider(ctx, p, domain.SAMLProvider{
		IDPEntityID: "https://idp.audit.test/entity",
		IDPSSOURL:   "https://idp.audit.test/sso",
		IDPCert:     "-----BEGIN CERTIFICATE-----\nMIIC...\n-----END CERTIFICATE-----",
		SPEntityID:  "https://sp.audit.test/metadata",
		Enabled:     true,
		Status:      "active",
	})
	if err != nil {
		t.Fatalf("CreateSAMLProvider: %v", err)
	}

	_, err = store.UpsertSAMLUserAndCreateSession(ctx, p.TenantID, samlProvider.IDPEntityID, "saml-nameid-1", "saml@example.com", "SAML User")
	if err != nil {
		t.Fatalf("UpsertSAMLUserAndCreateSession: %v", err)
	}

	scimUser, err := store.CreateSCIMUser(ctx, p, domain.User{
		OIDCSubject: "scim-user-1",
		Email:       "scim@example.com",
		DisplayName: "SCIM User",
	}, true)
	if err != nil {
		t.Fatalf("CreateSCIMUser: %v", err)
	}

	_, err = store.UpdateSCIMUser(ctx, p, scimUser.ID, domain.User{
		OIDCSubject: "scim-user-1",
		Email:       "scim-updated@example.com",
		DisplayName: "SCIM User Updated",
	}, false)
	if err != nil {
		t.Fatalf("UpdateSCIMUser: %v", err)
	}

	scimGroup, err := store.CreateSCIMGroup(ctx, p, domain.SCIMGroup{DisplayName: "SCIM Audit Group"})
	if err != nil {
		t.Fatalf("CreateSCIMGroup: %v", err)
	}

	scimGroup, err = store.UpdateSCIMGroup(ctx, p, scimGroup.ID, domain.SCIMGroup{DisplayName: "SCIM Audit Group Updated"})
	if err != nil {
		t.Fatalf("UpdateSCIMGroup: %v", err)
	}

	if err := store.DeleteSCIMGroup(ctx, p, scimGroup.ID); err != nil {
		t.Fatalf("DeleteSCIMGroup: %v", err)
	}

	// 4. Maker-checker policy
	if _, err := store.SetMakerCheckerPolicy(ctx, p, "journeys", true); err != nil {
		t.Fatalf("SetMakerCheckerPolicy: %v", err)
	}

	// 5. DSR Actions
	req, err := store.CreatePrivacyRequest(ctx, p, u.ID, "export")
	if err != nil {
		t.Fatalf("CreatePrivacyRequest: %v", err)
	}
	if _, err := store.VerifyPrivacyRequest(ctx, p, req.ID, req.VerificationToken); err != nil {
		t.Fatalf("VerifyPrivacyRequest: %v", err)
	}
	if err := store.CompletePrivacyExport(ctx, req.ID, "artifact/key/123"); err != nil {
		t.Fatalf("CompletePrivacyExport: %v", err)
	}

	delReq, err := store.CreatePrivacyRequest(ctx, p, u.ID, "delete")
	if err != nil {
		t.Fatalf("CreatePrivacyRequest delete: %v", err)
	}
	if _, err := store.VerifyPrivacyRequest(ctx, p, delReq.ID, delReq.VerificationToken); err != nil {
		t.Fatalf("VerifyPrivacyRequest delete: %v", err)
	}
	if _, err := store.DeletePrivacyData(ctx, delReq.ID); err != nil {
		t.Fatalf("DeletePrivacyData: %v", err)
	}

	// Verify all expected actions were recorded in audit_events
	events, err := store.ListAuditEvents(ctx, p, 100)
	if err != nil {
		t.Fatalf("ListAuditEvents: %v", err)
	}

	expectedActions := []string{
		"role.create", "role.update", "role.delete",
		"team.create", "team.update", "team.delete",
		"user.create", "saml_provider.create", "saml.login",
		"scim.user.create", "scim.user.update",
		"scim.group.create", "scim.group.update", "scim.group.delete",
		"maker_checker.set_policy",
		"privacy.export", "privacy.export.complete",
		"privacy.delete", "privacy.delete.complete",
	}

	actionsMap := make(map[string]bool)
	for _, ev := range events {
		actionsMap[ev.Action] = true
		// Verify no sensitive PII like password plaintexts leak into metadata JSON string representation
		metadataStr := string(ev.Metadata)
		if strings.Contains(metadataStr, "SecretPass123!") {
			t.Fatalf("PII password leak detected in audit metadata for event %s: %s", ev.Action, metadataStr)
		}
	}

	for _, action := range expectedActions {
		if !actionsMap[action] {
			t.Errorf("expected audit action %q to be emitted, but was not found in logged events", action)
		}
	}
}
