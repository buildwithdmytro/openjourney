package schemas

import (
	"encoding/json"
	"testing"
)

func TestValidate(t *testing.T) {
	schema := json.RawMessage(`{"type":"object","required":["amount"],"properties":{"amount":{"type":"number"}}}`)
	if err := Validate(schema, json.RawMessage(`{"amount":12.5}`)); err != nil {
		t.Fatalf("valid payload rejected: %v", err)
	}
	if err := Validate(schema, json.RawMessage(`{"amount":"12.5"}`)); err == nil {
		t.Fatal("invalid payload accepted")
	}
}

func TestBackwardCompatible(t *testing.T) {
	previous := json.RawMessage(`{"type":"object","properties":{"amount":{"type":"number"}}}`)
	compatible := json.RawMessage(`{"type":"object","properties":{"amount":{"type":"number"},"currency":{"type":"string"}}}`)
	if err := BackwardCompatible(previous, compatible); err != nil {
		t.Fatalf("compatible schema rejected: %v", err)
	}
	incompatible := json.RawMessage(`{"type":"object","properties":{"amount":{"type":"string"}}}`)
	if err := BackwardCompatible(previous, incompatible); err == nil {
		t.Fatal("incompatible schema accepted")
	}
}
