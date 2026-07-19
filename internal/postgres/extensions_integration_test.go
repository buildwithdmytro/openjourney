package postgres

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/buildwithdmytro/openjourney/internal/domain"
	"github.com/buildwithdmytro/openjourney/internal/extension"
	"github.com/go-jose/go-jose/v4"
)

func TestExtensionsIntegration(t *testing.T) {
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

	// 1. Setup Tenant and Principals
	tenantKey := fmt.Sprintf("ext-tenant-%d", time.Now().UnixNano())
	if err := store.EnsureDevelopmentTenant(ctx, tenantKey); err != nil {
		t.Fatal(err)
	}

	pUser, err := store.Authenticate(ctx, tenantKey)
	if err != nil {
		t.Fatal(err)
	}
	pUser.ActorType = "user"
	pUser.UserID = "00000000-0000-0000-0000-000000000005"

	pAPIKey := pUser
	pAPIKey.ActorType = "api_key"
	pAPIKey.UserID = "" // API keys do not have a UserID

	// 2. Setup signing keys (trusted & untrusted)
	trustedKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	untrustedKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}

	store.SetTrustedPublisherKeys(map[string]any{
		"trusted-kid": &trustedKey.PublicKey,
	})
	t.Setenv("TEST_EXTENSION_HMAC", "test-extension-hmac")

	// 3. Setup manifest and signers
	manifestData := map[string]any{
		"name":         "my-extension",
		"publisher":    "trusted-publisher",
		"version":      1,
		"kind":         "channel_provider",
		"transport":    "remote_http",
		"capabilities": []string{"send"},
	}
	manifestJSON, err := json.Marshal(manifestData)
	if err != nil {
		t.Fatal(err)
	}

	trustedSigner, err := jose.NewSigner(
		jose.SigningKey{Algorithm: jose.RS256, Key: trustedKey},
		(&jose.SignerOptions{}).WithType("JWS").WithHeader("kid", "trusted-kid"),
	)
	if err != nil {
		t.Fatal(err)
	}

	untrustedSigner, err := jose.NewSigner(
		jose.SigningKey{Algorithm: jose.RS256, Key: untrustedKey},
		(&jose.SignerOptions{}).WithType("JWS").WithHeader("kid", "untrusted-kid"),
	)
	if err != nil {
		t.Fatal(err)
	}

	// 4. Create parent Extension in registry
	ext, err := store.CreateExtension(ctx, pUser, domain.Extension{
		Name:      "my-extension",
		Publisher: "trusted-publisher",
		Status:    "installed",
	})
	if err != nil {
		t.Fatalf("CreateExtension: %v", err)
	}

	t.Run("unsigned / invalid JWS signature -> rejected", func(t *testing.T) {
		blobs := &memoryBlobs{objects: map[string][]byte{}}

		// Create extension version with invalid signature
		evDraft, err := store.CreateExtensionVersion(ctx, pUser, domain.ExtensionVersion{
			ExtensionID:     ext.ID,
			Version:         1,
			Kind:            "channel_provider",
			Transport:       "remote_http",
			Manifest:        manifestJSON,
			RequestedScopes: []string{"profiles:read"},
			Signature:       "not-a-valid-jws-signature",
			Status:          "draft",
		})
		if err != nil {
			t.Fatalf("CreateExtensionVersion: %v", err)
		}

		_, err = extension.Publish(ctx, store, blobs, pUser, ext.ID, evDraft.Version, pUser.UserID)
		if err == nil {
			t.Fatal("expected publish to fail with invalid JWS signature, but it succeeded")
		}
	})

	t.Run("wrong key (untrusted kid) -> rejected", func(t *testing.T) {
		blobs := &memoryBlobs{objects: map[string][]byte{}}

		// Sign manifest with untrusted key
		jwsObj, err := untrustedSigner.Sign(manifestJSON)
		if err != nil {
			t.Fatal(err)
		}
		sigStr, err := jwsObj.CompactSerialize()
		if err != nil {
			t.Fatal(err)
		}

		evDraft, err := store.CreateExtensionVersion(ctx, pUser, domain.ExtensionVersion{
			ExtensionID:     ext.ID,
			Version:         2,
			Kind:            "channel_provider",
			Transport:       "remote_http",
			Manifest:        manifestJSON,
			RequestedScopes: []string{"profiles:read"},
			Signature:       sigStr,
			Status:          "draft",
		})
		if err != nil {
			t.Fatalf("CreateExtensionVersion: %v", err)
		}

		_, err = extension.Publish(ctx, store, blobs, pUser, ext.ID, evDraft.Version, pUser.UserID)
		if err == nil {
			t.Fatal("expected publish to fail with untrusted signing key, but it succeeded")
		}
	})

	t.Run("api_key install -> 403 (unauthorized)", func(t *testing.T) {
		blobs := &memoryBlobs{objects: map[string][]byte{}}

		// Sign manifest with trusted key
		jwsObj, err := trustedSigner.Sign(manifestJSON)
		if err != nil {
			t.Fatal(err)
		}
		sigStr, err := jwsObj.CompactSerialize()
		if err != nil {
			t.Fatal(err)
		}

		evDraft, err := store.CreateExtensionVersion(ctx, pUser, domain.ExtensionVersion{
			ExtensionID:     ext.ID,
			Version:         3,
			Kind:            "channel_provider",
			Transport:       "remote_http",
			Manifest:        manifestJSON,
			RequestedScopes: []string{"profiles:read"},
			Signature:       sigStr,
			Status:          "draft",
		})
		if err != nil {
			t.Fatalf("CreateExtensionVersion: %v", err)
		}

		// Try to publish using API key instead of human User principal
		_, err = extension.Publish(ctx, store, blobs, pAPIKey, ext.ID, evDraft.Version, "approver-id")
		if !errors.Is(err, ErrUnauthorized) {
			t.Fatalf("expected ErrUnauthorized (403), got: %v", err)
		}
	})

	t.Run("symmetric publisher signature -> rejected", func(t *testing.T) {
		blobs := &memoryBlobs{objects: map[string][]byte{}}
		if _, err := store.UpsertExtensionConfig(ctx, pUser, domain.ExtensionConfig{
			ExtensionID: ext.ID,
			Config:      json.RawMessage(`{"base_url":"http://example.com","hmac_secret_ref":"TEST_EXTENSION_HMAC"}`),
		}); err != nil {
			t.Fatalf("configure extension HMAC: %v", err)
		}
		symmetricSigner, err := jose.NewSigner(jose.SigningKey{Algorithm: jose.HS256, Key: []byte("symmetric-publisher-key")}, nil)
		if err != nil {
			t.Fatal(err)
		}
		jws, err := symmetricSigner.Sign(manifestJSON)
		if err != nil {
			t.Fatal(err)
		}
		signature, err := jws.CompactSerialize()
		if err != nil {
			t.Fatal(err)
		}
		evDraft, err := store.CreateExtensionVersion(ctx, pUser, domain.ExtensionVersion{
			ExtensionID: ext.ID, Version: 5, Kind: "channel_provider", Transport: "remote_http",
			Manifest: manifestJSON, RequestedScopes: []string{"profiles:read"}, Signature: signature, Status: "draft",
		})
		if err != nil {
			t.Fatal(err)
		}
		if _, err := extension.Publish(ctx, store, blobs, pUser, ext.ID, evDraft.Version, pUser.UserID); err == nil {
			t.Fatal("expected symmetric publisher signature to be rejected")
		}
	})

	t.Run("valid key + human approval -> successful install & immutable version", func(t *testing.T) {
		blobs := &memoryBlobs{objects: map[string][]byte{}}

		// Sign manifest with trusted key
		jwsObj, err := trustedSigner.Sign(manifestJSON)
		if err != nil {
			t.Fatal(err)
		}
		sigStr, err := jwsObj.CompactSerialize()
		if err != nil {
			t.Fatal(err)
		}

		evDraft, err := store.CreateExtensionVersion(ctx, pUser, domain.ExtensionVersion{
			ExtensionID:     ext.ID,
			Version:         4,
			Kind:            "channel_provider",
			Transport:       "remote_http",
			Manifest:        manifestJSON,
			RequestedScopes: []string{"profiles:read"},
			Signature:       sigStr,
			Status:          "draft",
		})
		if err != nil {
			t.Fatalf("CreateExtensionVersion: %v", err)
		}

		// Publish (install) with user principal
		if _, err := store.UpsertExtensionConfig(ctx, pUser, domain.ExtensionConfig{
			ExtensionID:       ext.ID,
			Config:            json.RawMessage(`{"base_url":"http://example.com","hmac_secret_ref":"TEST_EXTENSION_HMAC"}`),
			EndpointAllowlist: []string{"http://example.com"},
		}); err != nil {
			t.Fatalf("configure extension HMAC: %v", err)
		}
		activeVer, err := extension.Publish(ctx, store, blobs, pUser, ext.ID, evDraft.Version, pUser.UserID)
		if err != nil {
			t.Fatalf("Publish: %v", err)
		}

		if activeVer.Status != "active" {
			t.Errorf("expected status 'active', got: %s", activeVer.Status)
		}
		if activeVer.SigningKeyID != "trusted-kid" {
			t.Errorf("expected signing key id 'trusted-kid', got: %s", activeVer.SigningKeyID)
		}
		if activeVer.ManifestKey == "" {
			t.Error("expected manifest key to be populated")
		}

		// Verify it was correctly uploaded to the blob store
		storedManifest, err := blobs.Get(ctx, activeVer.ManifestKey)
		if err != nil {
			t.Fatalf("failed to retrieve manifest from blob store: %v", err)
		}

		var parsedStored map[string]any
		if err := json.Unmarshal(storedManifest, &parsedStored); err != nil {
			t.Fatal(err)
		}
		if parsedStored["name"] != "my-extension" {
			t.Errorf("expected name 'my-extension', got: %v", parsedStored["name"])
		}

		// Verify parent extension was updated to 'enabled' and current_version_id is set
		updatedExt, err := store.GetExtension(ctx, pUser, ext.ID)
		if err != nil {
			t.Fatal(err)
		}
		if updatedExt.Status != "enabled" {
			t.Errorf("expected status 'enabled', got: %s", updatedExt.Status)
		}
		if updatedExt.CurrentVersionID == nil || *updatedExt.CurrentVersionID != activeVer.ID {
			t.Errorf("expected current version ID to match, got: %v", updatedExt.CurrentVersionID)
		}
	})
}

func TestExtensionConfigAndGrants_14_0_3(t *testing.T) {
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

	// 1. Setup Tenant and Principals
	tenantKey := fmt.Sprintf("ext-tenant-%d", time.Now().UnixNano())
	if err := store.EnsureDevelopmentTenant(ctx, tenantKey); err != nil {
		t.Fatal(err)
	}

	pUser, err := store.Authenticate(ctx, tenantKey)
	if err != nil {
		t.Fatal(err)
	}
	pUser.ActorType = "user"
	pUser.UserID = "00000000-0000-0000-0000-000000000005"

	// Create parent Extension
	ext, err := store.CreateExtension(ctx, pUser, domain.Extension{
		Name:      "test-config-ext",
		Publisher: "test-publisher",
		Status:    "installed",
	})
	if err != nil {
		t.Fatalf("CreateExtension: %v", err)
	}

	// 2. Test ExtensionConfig CRUD
	t.Run("extension config CRUD", func(t *testing.T) {
		cfg := domain.ExtensionConfig{
			ExtensionID:        ext.ID,
			TenantID:           pUser.TenantID,
			WorkspaceID:        pUser.WorkspaceID,
			Config:             json.RawMessage(`{"api_key_ref": "TEST_KEY_ENV", "base_url": "https://api.test.com"}`),
			EndpointAllowlist:  []string{"https://api.test.com"},
			TimeoutMs:          3000,
			MaxMemoryMb:        128,
			MonthlyBudgetCents: 500,
			RatePerMin:         100,
			Status:             "active",
		}

		// Upsert Config (Insert)
		created, err := store.UpsertExtensionConfig(ctx, pUser, cfg)
		if err != nil {
			t.Fatalf("UpsertExtensionConfig: %v", err)
		}
		if created.TimeoutMs != 3000 || created.MaxMemoryMb != 128 {
			t.Errorf("unexpected fields: %+v", created)
		}

		// Get Config
		fetched, err := store.GetExtensionConfig(ctx, pUser, ext.ID)
		if err != nil {
			t.Fatalf("GetExtensionConfig: %v", err)
		}
		if fetched.TimeoutMs != 3000 {
			t.Errorf("expected 3000, got %d", fetched.TimeoutMs)
		}

		// Upsert Config (Update)
		cfg.TimeoutMs = 4000
		updated, err := store.UpsertExtensionConfig(ctx, pUser, cfg)
		if err != nil {
			t.Fatalf("UpsertExtensionConfig update: %v", err)
		}
		if updated.TimeoutMs != 4000 {
			t.Errorf("expected 4000, got %d", updated.TimeoutMs)
		}

		// Delete Config
		err = store.DeleteExtensionConfig(ctx, pUser, ext.ID)
		if err != nil {
			t.Fatalf("DeleteExtensionConfig: %v", err)
		}

		_, err = store.GetExtensionConfig(ctx, pUser, ext.ID)
		if !errors.Is(err, ErrNotFound) {
			t.Fatalf("expected ErrNotFound after delete, got %v", err)
		}
	})

	// 3. Test ExtensionGrant CRUD & ResolveScopes
	t.Run("extension grants CRUD and ResolveScopes", func(t *testing.T) {
		// Create ExtensionVersion with requested scopes
		trustedKey, err := rsa.GenerateKey(rand.Reader, 2048)
		if err != nil {
			t.Fatal(err)
		}
		store.SetTrustedPublisherKeys(map[string]any{
			"trusted-kid-2": &trustedKey.PublicKey,
		})

		manifestData := map[string]any{
			"name":         "test-config-ext",
			"publisher":    "test-publisher",
			"version":      1,
			"kind":         "channel_provider",
			"transport":    "remote_http",
			"capabilities": []string{"send"},
		}
		manifestJSON, err := json.Marshal(manifestData)
		if err != nil {
			t.Fatal(err)
		}

		trustedSigner, err := jose.NewSigner(
			jose.SigningKey{Algorithm: jose.RS256, Key: trustedKey},
			(&jose.SignerOptions{}).WithType("JWS").WithHeader("kid", "trusted-kid-2"),
		)
		if err != nil {
			t.Fatal(err)
		}

		jwsObj, err := trustedSigner.Sign(manifestJSON)
		if err != nil {
			t.Fatal(err)
		}
		sigStr, err := jwsObj.CompactSerialize()
		if err != nil {
			t.Fatal(err)
		}

		ev, err := store.CreateExtensionVersion(ctx, pUser, domain.ExtensionVersion{
			ExtensionID:     ext.ID,
			Version:         1,
			Kind:            "channel_provider",
			Transport:       "remote_http",
			Manifest:        manifestJSON,
			RequestedScopes: []string{"profiles:read", "campaigns:read", "journeys:write"},
			Signature:       sigStr,
			Status:          "draft",
		})
		if err != nil {
			t.Fatalf("CreateExtensionVersion: %v", err)
		}

		// Create Grants: grant a subset
		grant1, err := store.CreateExtensionGrant(ctx, pUser, domain.ExtensionGrant{
			ExtensionID: ext.ID,
			Scope:       "profiles:read",
			GrantedBy:   pUser.UserID,
		})
		if err != nil {
			t.Fatalf("CreateExtensionGrant 1: %v", err)
		}
		if grant1.Scope != "profiles:read" {
			t.Errorf("expected profiles:read scope, got %s", grant1.Scope)
		}

		_, err = store.CreateExtensionGrant(ctx, pUser, domain.ExtensionGrant{
			ExtensionID: ext.ID,
			Scope:       "campaigns:read",
			GrantedBy:   pUser.UserID,
		})
		if err != nil {
			t.Fatalf("CreateExtensionGrant 2: %v", err)
		}

		// List Grants
		grants, err := store.ListExtensionGrants(ctx, pUser, ext.ID)
		if err != nil {
			t.Fatalf("ListExtensionGrants: %v", err)
		}
		if len(grants) != 2 {
			t.Errorf("expected 2 grants, got %d", len(grants))
		}

		// ResolveScopes intersection:
		// Requested: "profiles:read", "campaigns:read", "journeys:write"
		// Granted: "profiles:read", "campaigns:read"
		// Intersection: "profiles:read", "campaigns:read"
		resolved, err := extension.ResolveScopes(ctx, store, pUser, ext.ID, ev.Version)
		if err != nil {
			t.Fatalf("ResolveScopes: %v", err)
		}

		expected := map[string]bool{
			"profiles:read":  true,
			"campaigns:read": true,
		}
		if len(resolved) != 2 {
			t.Errorf("expected 2 resolved scopes, got %v", resolved)
		}
		for _, s := range resolved {
			if !expected[s] {
				t.Errorf("unexpected resolved scope: %s", s)
			}
		}

		// Delete a grant and verify updated intersection
		err = store.DeleteExtensionGrant(ctx, pUser, ext.ID, "profiles:read")
		if err != nil {
			t.Fatalf("DeleteExtensionGrant: %v", err)
		}

		resolved2, err := extension.ResolveScopes(ctx, store, pUser, ext.ID, ev.Version)
		if err != nil {
			t.Fatalf("ResolveScopes after delete: %v", err)
		}
		if len(resolved2) != 1 || resolved2[0] != "campaigns:read" {
			t.Errorf("expected only campaigns:read, got %v", resolved2)
		}
	})

	// 4. Test ResolveConfigMap secrets resolve via environment / _FILE
	t.Run("ResolveConfigMap secrets resolution", func(t *testing.T) {
		// Clean env just in case
		t.Setenv("MY_TEST_SECRET", "")
		t.Setenv("MY_TEST_SECRET_FILE", "")

		rawConfig := json.RawMessage(`{
			"api_key_ref": "MY_TEST_SECRET",
			"other_field": "hello"
		}`)

		// Case A: env value set
		t.Setenv("MY_TEST_SECRET", "super-secret-value")
		resolved, err := extension.ResolveConfigMap(rawConfig)
		if err != nil {
			t.Fatalf("ResolveConfigMap env: %v", err)
		}
		if resolved["api_key"] != "super-secret-value" {
			t.Errorf("expected api_key to resolve to super-secret-value, got %v", resolved["api_key"])
		}
		if resolved["other_field"] != "hello" {
			t.Errorf("expected other_field to remain, got %v", resolved["other_field"])
		}

		// Case B: file value set
		t.Setenv("MY_TEST_SECRET", "")
		tmpFile, err := os.CreateTemp("", "secret-*")
		if err != nil {
			t.Fatal(err)
		}
		defer os.Remove(tmpFile.Name())
		if _, err := tmpFile.WriteString("secret-from-file\n"); err != nil {
			t.Fatal(err)
		}
		tmpFile.Close()

		t.Setenv("MY_TEST_SECRET_FILE", tmpFile.Name())
		resolved2, err := extension.ResolveConfigMap(rawConfig)
		if err != nil {
			t.Fatalf("ResolveConfigMap file: %v", err)
		}
		if resolved2["api_key"] != "secret-from-file" {
			t.Errorf("expected api_key to resolve to secret-from-file, got %v", resolved2["api_key"])
		}

		// Case C: both env and file set -> error
		t.Setenv("MY_TEST_SECRET", "env-value")
		_, err = extension.ResolveConfigMap(rawConfig)
		if err == nil {
			t.Fatal("expected error when both env and env_FILE are set, but got nil")
		}
	})
}

func TestExtensionActivityHardening_14_10_3(t *testing.T) {
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

	p, _ := setupTestTenant(t, ctx, store)
	ext, err := store.CreateExtension(ctx, p, domain.Extension{
		Name:      "audit-test-extension",
		Publisher: "audit-test-publisher",
		Status:    "enabled",
	})
	if err != nil {
		t.Fatal(err)
	}

	allowed, err := store.RecordExtensionActivity(ctx, p, domain.ExtensionActivity{
		ExtensionID: ext.ID, ExtensionVersion: 1, Kind: "ingestion_transform",
		Invocation: "transform", PolicyDecision: "allowed",
	})
	if err != nil {
		t.Fatalf("record allowed invocation: %v", err)
	}
	denied, err := store.RecordExtensionActivity(ctx, p, domain.ExtensionActivity{
		ExtensionID: ext.ID, ExtensionVersion: 1, Kind: "ingestion_transform",
		Invocation: "transform", PolicyDecision: "denied_scope",
	})
	if err != nil {
		t.Fatalf("record denied invocation: %v", err)
	}

	var count int
	if err := store.pool.QueryRow(ctx, `SELECT count(*) FROM extension_activity WHERE tenant_id=$1 AND workspace_id=$2 AND extension_id=$3`,
		p.TenantID, p.WorkspaceID, ext.ID).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 2 {
		t.Fatalf("expected exactly one audit row for each invocation, got %d", count)
	}

	for _, activityID := range []string{allowed.ID, denied.ID} {
		if _, err := store.pool.Exec(ctx, `UPDATE extension_activity SET invocation='tampered' WHERE id=$1`, activityID); err == nil {
			t.Fatal("expected UPDATE of extension_activity to be rejected")
		}
		if _, err := store.pool.Exec(ctx, `DELETE FROM extension_activity WHERE id=$1`, activityID); err == nil {
			t.Fatal("expected DELETE of extension_activity to be rejected")
		}
	}
}
