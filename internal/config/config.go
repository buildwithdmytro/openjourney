package config

import (
	"crypto/ecdsa"
	"crypto/ed25519"
	"crypto/rsa"
	"encoding/json"
	"fmt"
	"os"
	"strconv"
	"strings"

	"github.com/go-jose/go-jose/v4"
)

type Config struct {
	HTTPAddress          string
	DatabaseURL          string
	DevelopmentAPIKey    string
	AdminEmail           string
	AdminPassword        string
	SessionTTL           string
	MaxBatchSize         int
	AutoMigrate          bool
	OIDCIssuer           string
	OIDCClientID         string
	CORSAllowedOrigin    string
	KafkaBrokers         string
	S3Endpoint           string
	S3AccessKey          string
	S3SecretKey          string
	S3Bucket             string
	S3UseTLS             bool
	ClickHouseAddress    string
	ClickHouseDatabase   string
	ClickHouseUsername   string
	ClickHousePassword   string
	OTLPEndpoint         string
	ServiceVersion       string
	TrackingSecretKey    string
	TrackingBaseURL      string
	AllowedTopicARNs     string
	TrustedPublisherKeys map[string]any
}

func Load() (Config, error) {
	trustedPublisherKeysRaw := os.Getenv("OPENJOURNEY_TRUSTED_PUBLISHER_KEYS")
	cfg := Config{
		HTTPAddress:          env("OPENJOURNEY_HTTP_ADDRESS", ":8080"),
		DatabaseURL:          env("OPENJOURNEY_DATABASE_URL", "postgres://openjourney:openjourney@localhost:5432/openjourney?sslmode=disable"),
		DevelopmentAPIKey:    os.Getenv("OPENJOURNEY_DEV_API_KEY"),
		AdminEmail:           os.Getenv("OPENJOURNEY_ADMIN_EMAIL"),
		AdminPassword:        os.Getenv("OPENJOURNEY_ADMIN_PASSWORD"),
		SessionTTL:           env("OPENJOURNEY_SESSION_TTL", "12h"),
		MaxBatchSize:         75,
		AutoMigrate:          true,
		OIDCIssuer:           os.Getenv("OPENJOURNEY_OIDC_ISSUER"),
		OIDCClientID:         os.Getenv("OPENJOURNEY_OIDC_CLIENT_ID"),
		CORSAllowedOrigin:    env("OPENJOURNEY_CORS_ALLOWED_ORIGIN", "http://localhost:3000"),
		KafkaBrokers:         os.Getenv("OPENJOURNEY_KAFKA_BROKERS"),
		S3Endpoint:           os.Getenv("OPENJOURNEY_S3_ENDPOINT"),
		S3AccessKey:          os.Getenv("OPENJOURNEY_S3_ACCESS_KEY"),
		S3SecretKey:          os.Getenv("OPENJOURNEY_S3_SECRET_KEY"),
		S3Bucket:             env("OPENJOURNEY_S3_BUCKET", "openjourney"),
		ClickHouseAddress:    os.Getenv("OPENJOURNEY_CLICKHOUSE_ADDRESS"),
		ClickHouseDatabase:   env("OPENJOURNEY_CLICKHOUSE_DATABASE", "openjourney"),
		ClickHouseUsername:   env("OPENJOURNEY_CLICKHOUSE_USERNAME", "default"),
		ClickHousePassword:   os.Getenv("OPENJOURNEY_CLICKHOUSE_PASSWORD"),
		OTLPEndpoint:         os.Getenv("OPENJOURNEY_OTLP_ENDPOINT"),
		ServiceVersion:       env("OPENJOURNEY_SERVICE_VERSION", "dev"),
		TrackingSecretKey:    env("OPENJOURNEY_TRACKING_SECRET_KEY", "change-me-in-production"),
		TrackingBaseURL:      env("OPENJOURNEY_TRACKING_BASE_URL", "http://localhost:8080"),
		AllowedTopicARNs:     env("OPENJOURNEY_ALLOWED_TOPIC_ARNS", ""),
		TrustedPublisherKeys: map[string]any{},
	}
	for key, target := range map[string]*string{
		"OPENJOURNEY_DATABASE_URL":           &cfg.DatabaseURL,
		"OPENJOURNEY_DEV_API_KEY":            &cfg.DevelopmentAPIKey,
		"OPENJOURNEY_ADMIN_PASSWORD":         &cfg.AdminPassword,
		"OPENJOURNEY_S3_ACCESS_KEY":          &cfg.S3AccessKey,
		"OPENJOURNEY_S3_SECRET_KEY":          &cfg.S3SecretKey,
		"OPENJOURNEY_CLICKHOUSE_PASSWORD":    &cfg.ClickHousePassword,
		"OPENJOURNEY_TRUSTED_PUBLISHER_KEYS": &trustedPublisherKeysRaw,
	} {
		value, configured, err := secretEnv(key)
		if err != nil {
			return Config{}, err
		}
		if configured {
			*target = value
		}
	}
	if trustedPublisherKeysRaw != "" {
		keys, err := parseTrustedPublisherKeys(trustedPublisherKeysRaw)
		if err != nil {
			return Config{}, fmt.Errorf("OPENJOURNEY_TRUSTED_PUBLISHER_KEYS: %w", err)
		}
		cfg.TrustedPublisherKeys = keys
	}
	if (cfg.OIDCIssuer == "") != (cfg.OIDCClientID == "") {
		return Config{}, fmt.Errorf("OPENJOURNEY_OIDC_ISSUER and OPENJOURNEY_OIDC_CLIENT_ID must be configured together")
	}
	var err error
	if raw := os.Getenv("OPENJOURNEY_MAX_BATCH_SIZE"); raw != "" {
		cfg.MaxBatchSize, err = strconv.Atoi(raw)
		if err != nil || cfg.MaxBatchSize < 1 || cfg.MaxBatchSize > 1000 {
			return Config{}, fmt.Errorf("OPENJOURNEY_MAX_BATCH_SIZE must be between 1 and 1000")
		}
	}
	if raw := os.Getenv("OPENJOURNEY_AUTO_MIGRATE"); raw != "" {
		cfg.AutoMigrate, err = strconv.ParseBool(raw)
		if err != nil {
			return Config{}, fmt.Errorf("OPENJOURNEY_AUTO_MIGRATE: %w", err)
		}
	}
	if raw := os.Getenv("OPENJOURNEY_S3_USE_TLS"); raw != "" {
		cfg.S3UseTLS, err = strconv.ParseBool(raw)
		if err != nil {
			return Config{}, fmt.Errorf("OPENJOURNEY_S3_USE_TLS: %w", err)
		}
	}
	if cfg.ServiceVersion != "dev" && cfg.TrackingSecretKey == "change-me-in-production" {
		return Config{}, fmt.Errorf("OPENJOURNEY_TRACKING_SECRET_KEY cannot be set to 'change-me-in-production' when OPENJOURNEY_SERVICE_VERSION is %q", cfg.ServiceVersion)
	}
	return cfg, nil
}

func parseTrustedPublisherKeys(raw string) (map[string]any, error) {
	var encoded map[string]json.RawMessage
	if err := json.Unmarshal([]byte(raw), &encoded); err != nil {
		return nil, fmt.Errorf("expected a JSON object of key IDs to public JWKs: %w", err)
	}
	keys := make(map[string]any, len(encoded))
	for kid, data := range encoded {
		if strings.TrimSpace(kid) == "" {
			return nil, fmt.Errorf("publisher key ID must not be empty")
		}
		var jwk jose.JSONWebKey
		if err := json.Unmarshal(data, &jwk); err != nil {
			return nil, fmt.Errorf("key %q: invalid JWK: %w", kid, err)
		}
		if !jwk.IsPublic() {
			return nil, fmt.Errorf("key %q: only public asymmetric keys are allowed", kid)
		}
		switch jwk.Key.(type) {
		case *rsa.PublicKey, *ecdsa.PublicKey, ed25519.PublicKey:
			keys[kid] = jwk.Key
		default:
			return nil, fmt.Errorf("key %q: only RSA, ECDSA, and Ed25519 public keys are allowed", kid)
		}
	}
	return keys, nil
}

func env(key, fallback string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return fallback
}

func secretEnv(key string) (string, bool, error) {
	value := os.Getenv(key)
	file := os.Getenv(key + "_FILE")
	if value != "" && file != "" {
		return "", false, fmt.Errorf("%s and %s_FILE cannot both be set", key, key)
	}
	if file == "" {
		return value, value != "", nil
	}
	content, err := os.ReadFile(file)
	if err != nil {
		return "", false, fmt.Errorf("read %s_FILE: %w", key, err)
	}
	return strings.TrimSpace(string(content)), true, nil
}
