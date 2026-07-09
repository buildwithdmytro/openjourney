package journey

import (
	"strings"
	"testing"
)

func TestDecodeConfigSupportedNodes(t *testing.T) {
	tests := []struct {
		name     string
		node     Node
		wantType any
		assert   func(t *testing.T, cfg any)
	}{
		{
			name: "entry",
			node: Node{Type: NodeTypeEntry, Config: []byte(`{"trigger":"event","event_type":"signup.completed","reentry_policy":"once","late_policy":"run"}`)},
			assert: func(t *testing.T, cfg any) {
				got := cfg.(EntryConfig)
				if got.Trigger != "event" || got.EventType != "signup.completed" || got.ReentryPolicy != "once" || got.LatePolicy != "run" {
					t.Fatalf("unexpected entry config: %+v", got)
				}
			},
		},
		{
			name: "delay",
			node: Node{Type: NodeTypeDelay, Config: []byte(`{"duration":"1h"}`)},
			assert: func(t *testing.T, cfg any) {
				if got := cfg.(DelayConfig); got.Duration != "1h" {
					t.Fatalf("unexpected delay config: %+v", got)
				}
			},
		},
		{
			name: "condition",
			node: Node{Type: NodeTypeCondition, Config: []byte(`{"dsl":{"type":"profile_attribute","field":"country","operator":"equals","value":"US"}}`)},
			assert: func(t *testing.T, cfg any) {
				if got := cfg.(ConditionConfig); !strings.Contains(string(got.DSL), `"country"`) {
					t.Fatalf("unexpected condition config: %s", got.DSL)
				}
			},
		},
		{
			name: "split",
			node: Node{Type: NodeTypeSplit, Config: []byte(`{"mode":"random","branches":[{"label":"a","weight":50},{"label":"b","weight":50}]}`)},
			assert: func(t *testing.T, cfg any) {
				got := cfg.(SplitConfig)
				if got.Mode != "random" || len(got.Branches) != 2 || got.Branches[0].Label != "a" || got.Branches[0].Weight != 50 {
					t.Fatalf("unexpected split config: %+v", got)
				}
			},
		},
		{
			name: "message",
			node: Node{Type: NodeTypeMessage, Config: []byte(`{"template_id":"tmpl-1","channel":"email","transactional":true}`)},
			assert: func(t *testing.T, cfg any) {
				got := cfg.(MessageConfig)
				if got.TemplateID != "tmpl-1" || got.Channel != "email" || !got.Transactional {
					t.Fatalf("unexpected message config: %+v", got)
				}
			},
		},
		{
			name: "wait_event",
			node: Node{Type: NodeTypeWaitEvent, Config: []byte(`{"event_type":"email.opened","timeout":"72h"}`)},
			assert: func(t *testing.T, cfg any) {
				got := cfg.(WaitConfig)
				if got.EventType != "email.opened" || got.Timeout != "72h" {
					t.Fatalf("unexpected wait config: %+v", got)
				}
			},
		},
		{
			name: "action",
			node: Node{Type: NodeTypeAction, Config: []byte(`{"action":"profile_update","set":{"stage":"engaged"}}`)},
			assert: func(t *testing.T, cfg any) {
				got := cfg.(ActionConfig)
				if got.Action != "profile_update" || got.Set["stage"] != "engaged" {
					t.Fatalf("unexpected action config: %+v", got)
				}
			},
		},
		{
			name: "goal",
			node: Node{Type: NodeTypeGoal, Config: []byte(`{"name":"activated"}`)},
			assert: func(t *testing.T, cfg any) {
				if got := cfg.(GoalConfig); got.Name != "activated" {
					t.Fatalf("unexpected goal config: %+v", got)
				}
			},
		},
		{
			name: "exit",
			node: Node{Type: NodeTypeExit, Config: []byte(`{"reason":"completed"}`)},
			assert: func(t *testing.T, cfg any) {
				if got := cfg.(ExitConfig); got.Reason != "completed" {
					t.Fatalf("unexpected exit config: %+v", got)
				}
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			cfg, err := DecodeConfig(tc.node)
			if err != nil {
				t.Fatalf("DecodeConfig returned error: %v", err)
			}
			tc.assert(t, cfg)
		})
	}
}

func TestDecodeConfigRejectsUnsupportedNodes(t *testing.T) {
	for _, nodeType := range []string{"ai_decision", "feature_flag", "nested_journey", "webhook_action", "integration_action", "experiment", "holdout"} {
		t.Run(nodeType, func(t *testing.T) {
			_, err := DecodeConfig(Node{Type: nodeType, Config: []byte(`{}`)})
			if err == nil {
				t.Fatalf("expected unsupported node type error")
			}
			if !strings.Contains(err.Error(), "unsupported node type") {
				t.Fatalf("expected unsupported node type error, got %v", err)
			}
		})
	}
}

func TestParseGraphDecodesNodeConfigs(t *testing.T) {
	graph, err := ParseGraph([]byte(`{
		"entry_node_id": "n1",
		"nodes": [
			{"id":"n1","type":"entry","config":{"trigger":"event","event_type":"signup.completed"}},
			{"id":"n2","type":"exit","config":{"reason":"completed"}}
		],
		"edges": [{"from":"n1","to":"n2"}]
	}`))
	if err != nil {
		t.Fatalf("ParseGraph returned error: %v", err)
	}
	if graph.EntryNodeID != "n1" || len(graph.Nodes) != 2 || len(graph.Edges) != 1 {
		t.Fatalf("unexpected graph: %+v", graph)
	}
}

func TestParseGraphRejectsUnsupportedNode(t *testing.T) {
	_, err := ParseGraph([]byte(`{
		"entry_node_id": "n1",
		"nodes": [
			{"id":"n1","type":"entry","config":{"trigger":"event","event_type":"signup.completed"}},
			{"id":"n2","type":"ai_decision","config":{}}
		],
		"edges": [{"from":"n1","to":"n2"}]
	}`))
	if err == nil {
		t.Fatalf("expected unsupported node type error")
	}
	if !strings.Contains(err.Error(), "unsupported node type: ai_decision") {
		t.Fatalf("unexpected error: %v", err)
	}
}
