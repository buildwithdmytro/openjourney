package render

import (
	"os"
	"strings"
	"testing"
)

func TestResolveAuthSecret_AllowlistExfiltrationPrevention(t *testing.T) {
	// Set test environment variables
	os.Setenv("DATABASE_URL", "postgres://admin:secretpass@db.internal:5432/production")
	os.Setenv("CC_SECRET_API_KEY", "valid-cc-secret-value-999")
	defer func() {
		os.Unsetenv("DATABASE_URL")
		os.Unsetenv("CC_SECRET_API_KEY")
	}()

	fetcher := NewDefaultConnectedContentFetcher(nil, nil)

	// 1. Attempting to resolve DATABASE_URL must fail and NOT return the database URL (F3 secret exfiltration prevention)
	secret, err := fetcher.resolveAuthSecret("DATABASE_URL")
	if err == nil {
		t.Fatalf("expected error when resolving non-allowlisted env var DATABASE_URL, but got secret %q", secret)
	}
	if !strings.Contains(err.Error(), "auth_secret_ref must match pattern") {
		t.Errorf("expected allowlist error message, got: %v", err)
	}
	if secret != "" {
		t.Errorf("expected empty string for rejected secret ref, got %q", secret)
	}

	// 2. Attempting to resolve secret: prefix must fail
	secret, err = fetcher.resolveAuthSecret("secret:raw_secret_value")
	if err == nil {
		t.Fatalf("expected error for secret: prefix, got secret %q", secret)
	}

	// 3. Attempting to resolve random env var without CC_SECRET_ prefix must fail
	secret, err = fetcher.resolveAuthSecret("AWS_SECRET_ACCESS_KEY")
	if err == nil {
		t.Fatalf("expected error for AWS_SECRET_ACCESS_KEY, got secret %q", secret)
	}

	// 4. Resolving valid CC_SECRET_* ref must succeed
	secret, err = fetcher.resolveAuthSecret("CC_SECRET_API_KEY")
	if err != nil {
		t.Fatalf("expected success for CC_SECRET_API_KEY, got error: %v", err)
	}
	if secret != "valid-cc-secret-value-999" {
		t.Errorf("expected 'valid-cc-secret-value-999', got %q", secret)
	}

	// 5. Empty ref returns empty string with no error
	secret, err = fetcher.resolveAuthSecret("")
	if err != nil {
		t.Fatalf("expected no error for empty ref, got: %v", err)
	}
	if secret != "" {
		t.Errorf("expected empty string, got %q", secret)
	}
}
