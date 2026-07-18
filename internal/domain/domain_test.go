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
		{"invalid email.sent payload", func(e *Event) {
			e.Type = "email.sent"
			e.Payload = json.RawMessage(`{"template_id":"t1"}`)
		}},
		{"invalid email.opened payload", func(e *Event) {
			e.Type = "email.opened"
			e.Payload = json.RawMessage(`{"dispatch_id":"d1"}`)
		}},
		{"invalid link.clicked payload", func(e *Event) {
			e.Type = "link.clicked"
			e.Payload = json.RawMessage(`{"template_id":"t1","dispatch_id":"d1"}`)
		}},
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

	t.Run("valid email.sent", func(t *testing.T) {
		e := valid
		e.Type = "email.sent"
		e.Payload = json.RawMessage(`{"template_id":"t1","dispatch_id":"d1","channel":"email"}`)
		if err := e.Validate(now); err != nil {
			t.Fatalf("expected valid, got %v", err)
		}
	})

	t.Run("valid link.clicked", func(t *testing.T) {
		e := valid
		e.Type = "link.clicked"
		e.Payload = json.RawMessage(`{"template_id":"t1","dispatch_id":"d1","url":"http://x.com"}`)
		if err := e.Validate(now); err != nil {
			t.Fatalf("expected valid, got %v", err)
		}
	})

	t.Run("valid journey message.sent", func(t *testing.T) {
		e := valid
		e.Type = "message.sent"
		e.Payload = json.RawMessage(`{"journey_id":"journey-1","channel":"email","endpoint":"user@example.com"}`)
		if err := e.Validate(now); err != nil {
			t.Fatalf("expected journey message.sent to be valid, got %v", err)
		}
	})

	t.Run("message.sent requires a campaign or journey", func(t *testing.T) {
		e := valid
		e.Type = "message.sent"
		e.Payload = json.RawMessage(`{"channel":"email","endpoint":"user@example.com"}`)
		if err := e.Validate(now); err == nil {
			t.Fatal("expected message.sent without campaign_id or journey_id to be invalid")
		}
	})

	t.Run("ai.action is a built-in event", func(t *testing.T) {
		e := valid
		e.Type = "ai.action"
		e.Payload = json.RawMessage(`{"activity_id":"activity-1","policy_decision":"allowed"}`)
		if err := e.Validate(now); err != nil {
			t.Fatalf("expected ai.action to be valid, got %v", err)
		}
	})
}
