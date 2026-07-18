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
			},
			{
				"type": "score",
				"model": "model-1",
				"score_name": "purchase_propensity",
				"operator": "greater_than",
				"value": 0.85
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

	if len(andNode.Conditions) != 4 {
		t.Fatalf("expected 4 conditions, got %d", len(andNode.Conditions))
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

	sc, ok := andNode.Conditions[3].(*Score)
	if !ok || sc.Model != "model-1" || sc.ScoreName != "purchase_propensity" || sc.Operator != "greater_than" || sc.Value != 0.85 {
		t.Fatalf("unexpected score condition: %+v", andNode.Conditions[3])
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
		{
			name: "missing score model",
			json: `{"type": "score", "score_name": "purchase_propensity", "operator": "greater_than", "value": 0.85}`,
		},
		{
			name: "missing score name",
			json: `{"type": "score", "model": "model-1", "operator": "greater_than", "value": 0.85}`,
		},
		{
			name: "unknown score operator",
			json: `{"type": "score", "model": "model-1", "score_name": "purchase_propensity", "operator": "in", "value": 0.85}`,
		},
		{
			name: "missing score value",
			json: `{"type": "score", "model": "model-1", "score_name": "purchase_propensity", "operator": "greater_than"}`,
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
