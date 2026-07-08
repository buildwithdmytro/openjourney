package config

import (
	"os"
	"path/filepath"
	"testing"
)

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

