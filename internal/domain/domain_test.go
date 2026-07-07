package domain

import (
	"encoding/json"
	"testing"
	"time"
)

func TestEventValidate(t *testing.T) {
	now := time.Now()
	valid := Event{
		Type: "profile.updated", SchemaVersion: 1, ExternalID: "user-1",
		IdempotencyKey: "request-1", OccurredAt: now, Payload: json.RawMessage(`{"attributes":{}}`),
	}
	if err := valid.Validate(now); err != nil {
		t.Fatalf("valid event rejected: %v", err)
	}
	cases := []struct {
		name string
		edit func(*Event)
	}{
		{"missing type", func(e *Event) { e.Type = "" }},
		{"missing subject", func(e *Event) { e.ExternalID = "" }},
		{"missing idempotency", func(e *Event) { e.IdempotencyKey = "" }},
		{"future timestamp", func(e *Event) { e.OccurredAt = now.Add(25 * time.Hour) }},
		{"invalid payload", func(e *Event) { e.Payload = json.RawMessage(`{`) }},
		{"non-object payload", func(e *Event) { e.Payload = json.RawMessage(`[]`) }},
		{"invalid profile payload", func(e *Event) { e.Payload = json.RawMessage(`{}`) }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			event := valid
			tc.edit(&event)
			if err := event.Validate(now); err == nil {
				t.Fatal("expected validation error")
			}
		})
	}
}
