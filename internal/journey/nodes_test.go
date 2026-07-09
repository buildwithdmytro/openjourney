package journey

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/buildwithdmytro/openjourney/internal/domain"
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

type executorMockStore struct {
	mockStore
	evaluatedAudience map[string]bool
	profileInSegment  map[string]bool
	updatedProfileID  string
	updatedAttrs      map[string]any
	acceptedEvents    []domain.Event
}

func (m *executorMockStore) EvaluateAudience(ctx context.Context, p domain.Principal, profileID string, dsl json.RawMessage) (bool, error) {
	return m.evaluatedAudience[string(dsl)], nil
}

func (m *executorMockStore) IsProfileInSegment(ctx context.Context, p domain.Principal, segmentID string, profileID string) (bool, error) {
	return m.profileInSegment[segmentID], nil
}

func (m *executorMockStore) UpdateProfileAttributes(ctx context.Context, p domain.Principal, profileID string, attrs map[string]any) error {
	m.updatedProfileID = profileID
	m.updatedAttrs = attrs
	return nil
}

func (m *executorMockStore) AcceptEvents(ctx context.Context, p domain.Principal, events []domain.Event) ([]string, error) {
	m.acceptedEvents = append(m.acceptedEvents, events...)
	return nil, nil
}

func TestExecuteEntry(t *testing.T) {
	store := &executorMockStore{}
	now := time.Now()
	run := &domain.JourneyRun{ID: "r1", TenantID: "t1", Status: "active"}
	graph := &Graph{
		Nodes: []Node{{ID: "n1", Type: NodeTypeEntry}, {ID: "n2", Type: NodeTypeExit}},
		Edges: []Edge{{From: "n1", To: "n2"}},
	}
	n := &graph.Nodes[0]

	res, err := n.Execute(context.Background(), store, run, graph, now, "advance")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if res.NextNodeID != "n2" {
		t.Errorf("expected NextNodeID 'n2', got %q", res.NextNodeID)
	}
	if res.NextStep == nil || res.NextStep.NodeID != "n2" || !res.NextStep.AvailableAt.Equal(now) {
		t.Errorf("unexpected next step: %+v", res.NextStep)
	}
}

func TestExecuteDelay(t *testing.T) {
	store := &executorMockStore{}
	now := time.Now()
	run := &domain.JourneyRun{ID: "r1", TenantID: "t1", Status: "active"}
	graph := &Graph{
		Nodes: []Node{
			{ID: "n1", Type: NodeTypeDelay, Config: json.RawMessage(`{"duration": "2h"}`)},
			{ID: "n2", Type: NodeTypeExit},
		},
		Edges: []Edge{{From: "n1", To: "n2"}},
	}
	n := &graph.Nodes[0]

	res, err := n.Execute(context.Background(), store, run, graph, now, "advance")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if res.NextNodeID != "n2" {
		t.Errorf("expected NextNodeID 'n2', got %q", res.NextNodeID)
	}
	expectedTime := now.Add(2 * time.Hour)
	if res.NextStep == nil || res.NextStep.NodeID != "n2" || !res.NextStep.AvailableAt.Equal(expectedTime) {
		t.Errorf("expected next step in 2 hours, got: %+v", res.NextStep)
	}
}

func TestExecuteCondition(t *testing.T) {
	store := &executorMockStore{
		evaluatedAudience: map[string]bool{
			`{"field":"country","operator":"equals","value":"US"}`: true,
		},
	}
	now := time.Now()
	run := &domain.JourneyRun{ID: "r1", TenantID: "t1", Status: "active"}
	graph := &Graph{
		Nodes: []Node{
			{ID: "n1", Type: NodeTypeCondition, Config: json.RawMessage(`{"dsl": {"field":"country","operator":"equals","value":"US"}}`)},
			{ID: "n2", Type: NodeTypeExit},
			{ID: "n3", Type: NodeTypeExit},
		},
		Edges: []Edge{
			{From: "n1", To: "n2", Branch: "true"},
			{From: "n1", To: "n3", Branch: "false"},
		},
	}

	// 1. True branch
	res, err := graph.Nodes[0].Execute(context.Background(), store, run, graph, now, "advance")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if res.NextNodeID != "n2" {
		t.Errorf("expected NextNodeID 'n2', got %q", res.NextNodeID)
	}

	// 2. False branch
	store.evaluatedAudience[`{"field":"country","operator":"equals","value":"US"}`] = false
	res, err = graph.Nodes[0].Execute(context.Background(), store, run, graph, now, "advance")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if res.NextNodeID != "n3" {
		t.Errorf("expected NextNodeID 'n3', got %q", res.NextNodeID)
	}
}

func TestExecuteSplitRandom(t *testing.T) {
	store := &executorMockStore{}
	now := time.Now()
	run := &domain.JourneyRun{ID: "r1", TenantID: "t1", ProfileID: "p1", Status: "active"}
	graph := &Graph{
		Nodes: []Node{
			{
				ID:   "n1",
				Type: NodeTypeSplit,
				Config: json.RawMessage(`{
					"mode": "random",
					"branches": [
						{"label": "a", "weight": 50},
						{"label": "b", "weight": 50}
					]
				}`),
			},
			{ID: "n2", Type: NodeTypeExit},
			{ID: "n3", Type: NodeTypeExit},
		},
		Edges: []Edge{
			{From: "n1", To: "n2", Branch: "a"},
			{From: "n1", To: "n3", Branch: "b"},
		},
	}

	res, err := graph.Nodes[0].Execute(context.Background(), store, run, graph, now, "advance")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if res.NextNodeID != "n2" && res.NextNodeID != "n3" {
		t.Errorf("expected branch choice, got %s", res.NextNodeID)
	}
	var stateMap map[string]string
	if err := json.Unmarshal(res.State, &stateMap); err != nil {
		t.Fatalf("failed to unmarshal state: %v", err)
	}
	if stateMap["n1"] == "" {
		t.Errorf("expected state to record split choice")
	}
}

func TestExecuteSplitAudience(t *testing.T) {
	store := &executorMockStore{
		profileInSegment: map[string]bool{
			"seg-1": true,
		},
	}
	now := time.Now()
	run := &domain.JourneyRun{ID: "r1", TenantID: "t1", ProfileID: "p1", Status: "active"}
	graph := &Graph{
		Nodes: []Node{
			{
				ID:   "n1",
				Type: NodeTypeSplit,
				Config: json.RawMessage(`{
					"mode": "audience",
					"branches": [
						{"label": "a", "segment_id": "seg-1"},
						{"label": "b", "segment_id": "seg-2"}
					]
				}`),
			},
			{ID: "n2", Type: NodeTypeExit},
			{ID: "n3", Type: NodeTypeExit},
		},
		Edges: []Edge{
			{From: "n1", To: "n2", Branch: "a"},
			{From: "n1", To: "n3", Branch: "b"},
		},
	}

	res, err := graph.Nodes[0].Execute(context.Background(), store, run, graph, now, "advance")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if res.NextNodeID != "n2" {
		t.Errorf("expected branch 'a' (n2), got %q", res.NextNodeID)
	}
}

func TestExecuteActionProfileUpdate(t *testing.T) {
	store := &executorMockStore{}
	now := time.Now()
	run := &domain.JourneyRun{ID: "r1", TenantID: "t1", ProfileID: "p1", Status: "active"}
	graph := &Graph{
		Nodes: []Node{
			{
				ID:   "n1",
				Type: NodeTypeAction,
				Config: json.RawMessage(`{
					"action": "profile_update",
					"set": {"loyalty": "gold"}
				}`),
			},
			{ID: "n2", Type: NodeTypeExit},
		},
		Edges: []Edge{{From: "n1", To: "n2"}},
	}

	res, err := graph.Nodes[0].Execute(context.Background(), store, run, graph, now, "advance")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if store.updatedProfileID != "p1" || store.updatedAttrs["loyalty"] != "gold" {
		t.Errorf("expected profile update: %+v", store.updatedAttrs)
	}
	if len(store.acceptedEvents) != 1 || store.acceptedEvents[0].Type != "profile.updated" {
		t.Errorf("expected profile.updated event to be emitted")
	}
	if res.NextNodeID != "n2" {
		t.Errorf("expected NextNodeID 'n2', got %q", res.NextNodeID)
	}
}

func TestExecuteGoal(t *testing.T) {
	store := &executorMockStore{}
	now := time.Now()
	run := &domain.JourneyRun{ID: "r1", TenantID: "t1", Status: "active"}
	graph := &Graph{
		Nodes: []Node{
			{ID: "n1", Type: NodeTypeGoal, Config: json.RawMessage(`{"name": "signup"}`)},
			{ID: "n2", Type: NodeTypeExit},
		},
		Edges: []Edge{{From: "n1", To: "n2"}},
	}

	res, err := graph.Nodes[0].Execute(context.Background(), store, run, graph, now, "advance")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !res.GoalReached {
		t.Errorf("expected GoalReached to be true")
	}
	if res.NextNodeID != "n2" {
		t.Errorf("expected NextNodeID 'n2', got %q", res.NextNodeID)
	}
}

func TestExecuteExit(t *testing.T) {
	store := &executorMockStore{}
	now := time.Now()
	run := &domain.JourneyRun{ID: "r1", TenantID: "t1", Status: "active"}
	graph := &Graph{
		Nodes: []Node{{ID: "n1", Type: NodeTypeExit}},
	}

	res, err := graph.Nodes[0].Execute(context.Background(), store, run, graph, now, "advance")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if res.NextStatus != "completed" {
		t.Errorf("expected NextStatus 'completed', got %q", res.NextStatus)
	}
	if res.CompletedAt == nil || !res.CompletedAt.Equal(now) {
		t.Errorf("expected CompletedAt to be set to now")
	}
}

func TestExecuteWaitEvent(t *testing.T) {
	store := &executorMockStore{}
	now := time.Now()
	run := &domain.JourneyRun{ID: "r1", TenantID: "t1", Status: "active"}
	graph := &Graph{
		Nodes: []Node{
			{
				ID:   "n1",
				Type: NodeTypeWaitEvent,
				Config: json.RawMessage(`{
					"event_type": "email.opened",
					"timeout": "48h"
				}`),
			},
			{ID: "n2", Type: NodeTypeExit},
			{ID: "n3", Type: NodeTypeExit},
		},
		Edges: []Edge{
			{From: "n1", To: "n2", Branch: "success"},
			{From: "n1", To: "n3", Branch: "timeout"},
		},
	}

	res, err := graph.Nodes[0].Execute(context.Background(), store, run, graph, now, "advance")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if res.NextStatus != "waiting" {
		t.Errorf("expected NextStatus 'waiting', got %q", res.NextStatus)
	}
	if res.WaitEventType == nil || *res.WaitEventType != "email.opened" {
		t.Errorf("expected WaitEventType 'email.opened', got %v", res.WaitEventType)
	}
	expectedTimeout := now.Add(48 * time.Hour)
	if res.WaitUntil == nil || !res.WaitUntil.Equal(expectedTimeout) {
		t.Errorf("expected WaitUntil %v, got %v", expectedTimeout, res.WaitUntil)
	}
	if res.NextStep == nil || res.NextStep.NodeID != "n1" || res.NextStep.Kind != "timeout" || !res.NextStep.AvailableAt.Equal(expectedTimeout) {
		t.Errorf("expected next step to be scheduled timeout, got %+v", res.NextStep)
	}

	res, err = graph.Nodes[0].Execute(context.Background(), store, run, graph, now, "timeout")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if res.NextStatus != "active" {
		t.Errorf("expected NextStatus 'active', got %q", res.NextStatus)
	}
	if res.NextNodeID != "n3" {
		t.Errorf("expected NextNodeID 'n3', got %q", res.NextNodeID)
	}
	if res.NextStep == nil || res.NextStep.NodeID != "n3" || res.NextStep.Kind != "advance" {
		t.Errorf("expected next step to advance to n3, got %+v", res.NextStep)
	}
}

func TestExecuteMessage(t *testing.T) {
	store := &executorMockStore{}
	now := time.Now()
	run := &domain.JourneyRun{
		ID:               "r1",
		TenantID:         "t1",
		WorkspaceID:      "w1",
		JourneyID:        "j1",
		JourneyVersionID: "v1",
		ProfileID:        "p1",
		Status:           "active",
	}
	graph := &Graph{
		Nodes: []Node{
			{
				ID:   "n1",
				Type: NodeTypeMessage,
				Config: json.RawMessage(`{
					"template_id": "tmpl-123",
					"channel": "email",
					"transactional": false
				}`),
			},
			{ID: "n2", Type: NodeTypeExit},
		},
		Edges: []Edge{
			{From: "n1", To: "n2"},
		},
	}

	res, err := graph.Nodes[0].Execute(context.Background(), store, run, graph, now, "advance")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if res.NextNodeID != "n2" {
		t.Errorf("expected NextNodeID 'n2', got %q", res.NextNodeID)
	}
	if res.MessageIntent == nil {
		t.Fatalf("expected MessageIntent to be populated, but got nil")
	}
	intent := res.MessageIntent
	if intent.TemplateID != "tmpl-123" {
		t.Errorf("expected TemplateID 'tmpl-123', got %q", intent.TemplateID)
	}
	if intent.Channel != "email" {
		t.Errorf("expected Channel 'email', got %q", intent.Channel)
	}
	if intent.Endpoint != "test@example.com" {
		t.Errorf("expected Endpoint 'test@example.com', got %q", intent.Endpoint)
	}
	if intent.Transactional {
		t.Errorf("expected Transactional to be false")
	}
}


