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
