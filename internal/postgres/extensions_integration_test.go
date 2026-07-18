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

	// 3. Setup manifest and signers
	manifestData := map[string]any{
		"name":        "my-extension",
		"publisher":   "trusted-publisher",
		"version":     1,
		"kind":        "channel_provider",
		"transport":   "remote_http",
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
