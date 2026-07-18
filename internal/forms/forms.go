// Package forms validates form drafts and produces the immutable definition
// stored in a published form version.
package forms

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/buildwithdmytro/openjourney/internal/schemas"
)

type Field struct {
	Key        string         `json:"key"`
	Type       string         `json:"type"`
	Required   bool           `json:"required,omitempty"`
	Validation map[string]any `json:"validation,omitempty"`
	Consent    bool           `json:"consent,omitempty"`
	MapsTo     string         `json:"maps_to,omitempty"`
}

type Draft struct {
	Fields        []Field        `json:"fields"`
	SubmitActions map[string]any `json:"submit_actions,omitempty"`
}

type Definition struct {
	Fields        []Field         `json:"fields"`
	Schema        json.RawMessage `json:"schema"`
	SubmitActions map[string]any  `json:"submit_actions,omitempty"`
}

func CanonicalizeDraft(raw json.RawMessage) ([]byte, error) {
	var draft Draft
	if len(raw) == 0 {
		return nil, errors.New("draft is required")
	}
	if err := json.Unmarshal(raw, &draft); err != nil {
		return nil, fmt.Errorf("invalid draft: %w", err)
	}
	properties := make(map[string]any, len(draft.Fields))
	required := make([]string, 0)
	seen := make(map[string]struct{}, len(draft.Fields))
	for _, field := range draft.Fields {
		key := strings.TrimSpace(field.Key)
		if key == "" {
			return nil, errors.New("field key is required")
		}
		if _, ok := seen[key]; ok {
			return nil, fmt.Errorf("duplicate field key: %s", key)
		}
		seen[key] = struct{}{}
		property := map[string]any{}
		switch field.Type {
		case "text", "email":
			property["type"] = "string"
			if field.Type == "email" {
				property["format"] = "email"
			}
		case "number":
			property["type"] = "number"
		case "integer":
			property["type"] = "integer"
		case "boolean":
			property["type"] = "boolean"
		default:
			return nil, fmt.Errorf("unsupported field type %q", field.Type)
		}
		for key, value := range field.Validation {
			property[key] = value
		}
		properties[key] = property
		if field.Required {
			required = append(required, key)
		}
	}
	schema := map[string]any{"type": "object", "properties": properties, "additionalProperties": false}
	if len(required) > 0 {
		schema["required"] = required
	}
	schemaBytes, err := json.Marshal(schema)
	if err != nil {
		return nil, err
	}
	if _, err := schemas.Compile(schemaBytes); err != nil {
		return nil, err
	}
	definition := Definition{Fields: draft.Fields, Schema: schemaBytes, SubmitActions: draft.SubmitActions}
	return json.Marshal(definition)
}

func ValidateSubmission(definition json.RawMessage, payload json.RawMessage) error {
	var def Definition
	if err := json.Unmarshal(definition, &def); err != nil {
		return err
	}
	return schemas.Validate(def.Schema, payload)
}

// FieldsFromDefinition exposes the frozen field mapping to capture adapters
// without allowing them to mutate the published definition.
func FieldsFromDefinition(definition json.RawMessage) []Field {
	var def Definition
	if json.Unmarshal(definition, &def) != nil {
		return nil
	}
	return def.Fields
}
