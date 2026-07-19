package config

import (
	"crypto/rand"
	"crypto/rsa"
	"encoding/base64"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/go-jose/go-jose/v4"
)

func TestLoadReadsAsymmetricTrustedPublisherJWKs(t *testing.T) {
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	jwk := jose.JSONWebKey{Key: &key.PublicKey}
	encoded, err := json.Marshal(map[string]jose.JSONWebKey{"operator": jwk})
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv("OPENJOURNEY_TRUSTED_PUBLISHER_KEYS", string(encoded))
	cfg, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := cfg.TrustedPublisherKeys["operator"].(*rsa.PublicKey); !ok {
		t.Fatalf("trusted key type=%T, want *rsa.PublicKey", cfg.TrustedPublisherKeys["operator"])
	}
}

func TestLoadRejectsSymmetricTrustedPublisherKey(t *testing.T) {
	raw := base64.RawURLEncoding.EncodeToString([]byte("symmetric-key"))
	t.Setenv("OPENJOURNEY_TRUSTED_PUBLISHER_KEYS", `{"operator":{"kty":"oct","k":"`+raw+`"}}`)
	if _, err := Load(); err == nil {
		t.Fatal("expected symmetric publisher key to be rejected")
	}
}

func TestLoadReadsDockerSecretFiles(t *testing.T) {
	path := filepath.Join(t.TempDir(), "database-url")
	if err := os.WriteFile(path, []byte("postgres://secret\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("OPENJOURNEY_DATABASE_URL", "")
	t.Setenv("OPENJOURNEY_DATABASE_URL_FILE", path)
	cfg, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.DatabaseURL != "postgres://secret" {
		t.Fatalf("database URL=%q", cfg.DatabaseURL)
	}
}

func TestLoadRejectsValueAndSecretFileTogether(t *testing.T) {
	t.Setenv("OPENJOURNEY_DEV_API_KEY", "value")
	t.Setenv("OPENJOURNEY_DEV_API_KEY_FILE", "/tmp/secret")
	if _, err := Load(); err == nil {
		t.Fatal("expected conflicting secret configuration error")
	}
}

func TestLoadRejectsDefaultTrackingSecretInProduction(t *testing.T) {
	t.Setenv("OPENJOURNEY_SERVICE_VERSION", "prod")
	t.Setenv("OPENJOURNEY_TRACKING_SECRET_KEY", "change-me-in-production")
	if _, err := Load(); err == nil {
		t.Fatal("expected error for default tracking secret key in non-dev version")
	}

	t.Setenv("OPENJOURNEY_TRACKING_SECRET_KEY", "custom-secret-key")
	cfg, err := Load()
	if err != nil {
		t.Fatalf("unexpected error when tracking secret key is configured: %v", err)
	}
	if cfg.TrackingSecretKey != "custom-secret-key" {
		t.Fatalf("expected tracking key 'custom-secret-key', got %q", cfg.TrackingSecretKey)
	}
}
