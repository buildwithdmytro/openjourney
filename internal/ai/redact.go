package ai

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/buildwithdmytro/openjourney/internal/domain"
)

var ErrRedactionDenied = errors.New("AI payload contains a field that cannot be sent to a model")

const redactedValue = "[REDACTED]"

// Redact applies the field-classification policy to JSON-compatible payloads.
// Fields without a classification are masked, so a newly introduced field can
// never leave the service merely because its policy was not configured yet.
func Redact(payload any, classifications []domain.FieldClassification, purpose string) (any, error) {
	_ = purpose // Purpose-specific policy expansion is intentionally fail-closed.
	raw, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("marshal payload for redaction: %w", err)
	}
	var value any
	if err := json.Unmarshal(raw, &value); err != nil {
		return nil, fmt.Errorf("decode payload for redaction: %w", err)
	}
	policies := make(map[string]domain.FieldClassification, len(classifications))
	for _, classification := range classifications {
		path := normalizeRedactionPath(classification.FieldPath)
		if path != "" {
			policies[path] = classification
		}
	}
	return redactValue(value, "", policies)
}

func normalizeRedactionPath(path string) string {
	path = strings.TrimSpace(path)
	path = strings.TrimPrefix(path, "attributes.")
	return strings.TrimPrefix(path, "profile.")
}

func redactValue(value any, path string, policies map[string]domain.FieldClassification) (any, error) {
	switch typed := value.(type) {
	case map[string]any:
		out := make(map[string]any, len(typed))
		for key, child := range typed {
			childPath := key
			if path != "" {
				childPath = path + "." + key
			}
			policy, classified := policies[childPath]
			if !classified {
				out[key] = redactedValue
				continue
			}
			redacted, err := redactField(child, childPath, policy)
			if err != nil {
				return nil, err
			}
			out[key] = redacted
		}
		return out, nil
	case []any:
		out := make([]any, len(typed))
		for i, child := range typed {
			redacted, err := redactValue(child, path, policies)
			if err != nil {
				return nil, err
			}
			out[i] = redacted
		}
		return out, nil
	default:
		return value, nil
	}
}

func redactField(value any, path string, policy domain.FieldClassification) (any, error) {
	if policy.Classification == "restricted" || policy.SendToModel == "deny" {
		return nil, fmt.Errorf("%w: %s", ErrRedactionDenied, path)
	}
	if isEmailOrPhone(path) || policy.SendToModel == "redact" {
		return redactedValue, nil
	}
	if policy.Classification == "confidential" || policy.SendToModel == "tokenize" {
		return tokenizeValue(value), nil
	}
	return redactValue(value, path, nil)
}

func isEmailOrPhone(path string) bool {
	field := path[strings.LastIndex(path, ".")+1:]
	return strings.EqualFold(field, "email") || strings.EqualFold(field, "phone") || strings.EqualFold(field, "phone_number")
}

func tokenizeValue(value any) string {
	sum := sha256.Sum256([]byte(fmt.Sprint(value)))
	return "[TOKEN:" + hex.EncodeToString(sum[:]) + "]"
}
