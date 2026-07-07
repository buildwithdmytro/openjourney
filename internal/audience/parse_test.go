package audience

import (
	"testing"
)

func TestParseValid(t *testing.T) {
	data := []byte(`{
		"logic": "and",
		"conditions": [
			{
				"type": "profile_attribute",
				"field": "country",
				"operator": "equals",
				"value": "US"
			},
			{
				"type": "event_history",
				"event_type": "purchase",
				"operator": "has_occurred",
				"time_window_days": 30,
				"min_count": 2
			},
			{
				"type": "consent",
				"channel": "email",
				"topic": "marketing",
				"state": "subscribed"
			}
		]
	}`)

	node, err := Parse(data)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}

	andNode, ok := node.(*And)
	if !ok {
		t.Fatalf("expected *And node, got %T", node)
	}

	if len(andNode.Conditions) != 3 {
		t.Fatalf("expected 3 conditions, got %d", len(andNode.Conditions))
	}

	pa, ok := andNode.Conditions[0].(*ProfileAttribute)
	if !ok || pa.Field != "country" || pa.Operator != "equals" || pa.Value != "US" {
		t.Fatalf("unexpected profile attribute condition: %+v", andNode.Conditions[0])
	}

	eh, ok := andNode.Conditions[1].(*EventHistory)
	if !ok || eh.EventType != "purchase" || eh.Operator != "has_occurred" || eh.TimeWindowDays != 30 || eh.MinCount != 2 {
		t.Fatalf("unexpected event history condition: %+v", andNode.Conditions[1])
	}

	c, ok := andNode.Conditions[2].(*Consent)
	if !ok || c.Channel != "email" || c.Topic != "marketing" || c.State != "subscribed" {
		t.Fatalf("unexpected consent condition: %+v", andNode.Conditions[2])
	}
}

func TestParseInvalid(t *testing.T) {
	tests := []struct {
		name string
		json string
	}{
		{
			name: "unknown logic operator",
			json: `{"logic": "xor", "conditions": []}`,
		},
		{
			name: "missing profile_attribute field",
			json: `{"type": "profile_attribute", "operator": "equals", "value": "US"}`,
		},
		{
			name: "unknown profile_attribute operator",
			json: `{"type": "profile_attribute", "field": "country", "operator": "matches", "value": "US"}`,
		},
		{
			name: "negative event_history window",
			json: `{"type": "event_history", "event_type": "purchase", "operator": "has_occurred", "time_window_days": -1}`,
		},
		{
			name: "unknown consent state",
			json: `{"type": "consent", "channel": "email", "topic": "marketing", "state": "opt_in"}`,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			_, err := Parse([]byte(tc.json))
			if err == nil {
				t.Fatalf("expected error for case: %s", tc.name)
			}
		})
	}
}
