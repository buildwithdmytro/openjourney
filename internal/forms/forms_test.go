package forms

import (
	"encoding/json"
	"testing"
)

func TestCanonicalizeDraftCompilesTypedSchema(t *testing.T) {
	draft := json.RawMessage(`{"fields":[{"key":"email","type":"email","required":true},{"key":"age","type":"integer"}]}`)
	definition, err := CanonicalizeDraft(draft)
	if err != nil {
		t.Fatalf("canonicalize: %v", err)
	}
	var out Definition
	if err := json.Unmarshal(definition, &out); err != nil {
		t.Fatal(err)
	}
	if err := ValidateSubmission(definition, json.RawMessage(`{"email":"a@example.com","age":4}`)); err != nil {
		t.Fatalf("valid submission rejected: %v", err)
	}
	if err := ValidateSubmission(definition, json.RawMessage(`{"email":"not-an-email"}`)); err == nil {
		t.Fatal("invalid email accepted")
	}
}
