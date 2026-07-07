package schemas

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"

	jsonschema "github.com/santhosh-tekuri/jsonschema/v5"
)

type Validator struct {
	compiled *jsonschema.Schema
}

func Compile(schema json.RawMessage) (*Validator, error) {
	compiler := jsonschema.NewCompiler()
	if err := compiler.AddResource("schema.json", bytes.NewReader(schema)); err != nil {
		return nil, fmt.Errorf("invalid schema: %w", err)
	}
	compiled, err := compiler.Compile("schema.json")
	if err != nil {
		return nil, fmt.Errorf("invalid schema: %w", err)
	}
	return &Validator{compiled: compiled}, nil
}

func (v *Validator) Validate(payload json.RawMessage) error {
	var value any
	if err := json.Unmarshal(payload, &value); err != nil {
		return err
	}
	if err := v.compiled.Validate(value); err != nil {
		return fmt.Errorf("payload does not match schema: %w", err)
	}
	return nil
}

func Validate(schema, payload json.RawMessage) error {
	validator, err := Compile(schema)
	if err != nil {
		return err
	}
	return validator.Validate(payload)
}

func BackwardCompatible(previous, next json.RawMessage) error {
	var oldSchema, newSchema map[string]any
	if err := json.Unmarshal(previous, &oldSchema); err != nil {
		return err
	}
	if err := json.Unmarshal(next, &newSchema); err != nil {
		return err
	}
	oldProperties, _ := oldSchema["properties"].(map[string]any)
	newProperties, _ := newSchema["properties"].(map[string]any)
	for name, oldProperty := range oldProperties {
		newProperty, exists := newProperties[name]
		if !exists {
			return fmt.Errorf("property %q cannot be removed", name)
		}
		oldType := propertyType(oldProperty)
		newType := propertyType(newProperty)
		if oldType != "" && newType != "" && oldType != newType {
			return fmt.Errorf("property %q type cannot change from %s to %s", name, oldType, newType)
		}
	}
	oldRequired := stringSet(oldSchema["required"])
	for name := range stringSet(newSchema["required"]) {
		if _, existed := oldProperties[name]; !existed {
			return fmt.Errorf("new property %q cannot be required in backward-compatible mode", name)
		}
		if _, wasRequired := oldRequired[name]; !wasRequired {
			return fmt.Errorf("optional property %q cannot become required", name)
		}
	}
	return nil
}

func ValidateDefinition(schema json.RawMessage) error {
	if len(schema) == 0 || !json.Valid(schema) {
		return errors.New("schema must be valid JSON")
	}
	compiler := jsonschema.NewCompiler()
	if err := compiler.AddResource("schema.json", bytes.NewReader(schema)); err != nil {
		return fmt.Errorf("invalid schema: %w", err)
	}
	if _, err := compiler.Compile("schema.json"); err != nil {
		return fmt.Errorf("invalid schema: %w", err)
	}
	var value map[string]any
	if err := json.Unmarshal(schema, &value); err != nil {
		return err
	}
	if schemaType, _ := value["type"].(string); schemaType != "" && schemaType != "object" {
		return errors.New("event schema root type must be object")
	}
	return nil
}

func propertyType(value any) string {
	property, _ := value.(map[string]any)
	valueType, _ := property["type"].(string)
	return valueType
}

func stringSet(value any) map[string]struct{} {
	result := map[string]struct{}{}
	items, _ := value.([]any)
	for _, item := range items {
		if text, ok := item.(string); ok {
			result[text] = struct{}{}
		}
	}
	return result
}
