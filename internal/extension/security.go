package extension

import (
	"encoding/json"
	"errors"
	"fmt"
)

// ValidateRemoteHTTPConfig enforces signed remote-extension configuration.
// Persisted credentials must be references, never raw secret values.
func ValidateRemoteHTTPConfig(transport string, rawConfig json.RawMessage) error {
	if transport != "remote_http" {
		return nil
	}
	var config map[string]any
	if len(rawConfig) == 0 || json.Unmarshal(rawConfig, &config) != nil {
		return errors.New("remote_http config must contain hmac_secret_ref")
	}
	if _, present := config["hmac_secret"]; present {
		return errors.New("remote_http config must use hmac_secret_ref, not raw hmac_secret")
	}
	ref, ok := config["hmac_secret_ref"].(string)
	if !ok || ref == "" {
		return errors.New("remote_http config must contain hmac_secret_ref")
	}
	return nil
}

func validateResolvedRemoteHMAC(config map[string]any) error {
	secret, ok := config["hmac_secret"].(string)
	if !ok || secret == "" {
		return fmt.Errorf("remote_http hmac_secret_ref is not configured")
	}
	return nil
}

// ValidateNativeConnectorConfig enforces that native connectors use secret references, not raw values.
func ValidateNativeConnectorConfig(transport string, rawConfig json.RawMessage) error {
	switch transport {
	case "s3", "clickhouse", "kafka", "webhook":
		// These transports support credential references
	default:
		return nil
	}

	var config map[string]any
	if len(rawConfig) == 0 || json.Unmarshal(rawConfig, &config) != nil {
		return nil // Empty config is allowed
	}

	rawSecretKeys := []string{"access_key", "secret_key", "password", "hmac_secret"}
	for _, key := range rawSecretKeys {
		if _, present := config[key]; present {
			return fmt.Errorf("%s config must use %s_ref, not raw %s", transport, key, key)
		}
	}
	return nil
}

// RedactExtensionConfig removes raw secret values from config before returning to client.
func RedactExtensionConfig(config map[string]any) map[string]any {
	if config == nil {
		return config
	}
	redacted := make(map[string]any)
	for k, v := range config {
		redacted[k] = v
	}
	// Remove any known raw secret keys; only leave *_ref keys
	rawSecretKeys := []string{"access_key", "secret_key", "password", "hmac_secret"}
	for _, key := range rawSecretKeys {
		delete(redacted, key)
	}
	return redacted
}
