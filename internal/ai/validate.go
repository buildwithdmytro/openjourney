package ai

import (
	"encoding/json"
	"fmt"

	"github.com/buildwithdmytro/openjourney/internal/schemas"
)

// ValidateOutput validates the content against the output JSON schema and the optional domain validator.
func ValidateOutput(content []byte, outputSchema json.RawMessage, domainValidator func([]byte) error) error {
	if len(outputSchema) > 0 {
		if err := schemas.Validate(outputSchema, content); err != nil {
			return fmt.Errorf("schema validation failed: %w", err)
		}
	}
	if domainValidator != nil {
		if err := domainValidator(content); err != nil {
			return fmt.Errorf("domain validation failed: %w", err)
		}
	}
	return nil
}
