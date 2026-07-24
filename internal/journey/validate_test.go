package journey

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestValidateValidGraph(t *testing.T) {
	graph := canonicalGraph()
	if err := Validate(&graph); err != nil {
		t.Fatalf("Validate returned error: %v", err)
	}
}

func TestValidateMalformedNodeConfigReturnsError(t *testing.T) {
	graph := canonicalGraph()
	graph.Nodes[1].Config = raw(`{"duration":`)

	defer func() {
		if recovered := recover(); recovered != nil {
			t.Fatalf("Validate panicked on malformed config: %v", recovered)
		}
	}()

	if err := Validate(&graph); err == nil {
		t.Fatal("Validate accepted malformed node config")
	}
}

func TestValidateInvalidGraphs(t *testing.T) {
	tests := []struct {
		name    string
		mutate  func(*Graph)
		wantErr string
	}{
		{
			name: "missing entry",
			mutate: func(g *Graph) {
				g.Nodes = g.Nodes[1:]
			},
			wantErr: "expected exactly one entry node",
		},
		{
			name: "dangling edge",
			mutate: func(g *Graph) {
				g.Edges[0].To = "missing"
			},
			wantErr: "edge references missing to node",
		},
		{
			name: "wrong branch labels",
			mutate: func(g *Graph) {
				for i := range g.Edges {
					if g.Edges[i].From == "n3" && g.Edges[i].Branch == "true" {
						g.Edges[i].Branch = "yes"
					}
				}
			},
			wantErr: "unexpected branch label",
		},
		{
			name: "unreachable node",
			mutate: func(g *Graph) {
				g.Nodes = append(g.Nodes, Node{ID: "orphan", Type: NodeTypeExit, Config: raw(`{"reason":"orphan"}`)})
			},
			wantErr: "unreachable node",
		},
		{
			name: "no exit",
			mutate: func(g *Graph) {
				g.Nodes = []Node{
					{ID: "n1", Type: NodeTypeEntry, Config: raw(`{"trigger":"event","event_type":"signup.completed"}`)},
					{ID: "n2", Type: NodeTypeGoal, Config: raw(`{"name":"activated"}`)},
				}
				g.Edges = []Edge{{From: "n1", To: "n2"}, {From: "n2", To: "n1"}}
			},
			wantErr: "graph must have at least one reachable exit node",
		},
		{
			name: "invalid duration",
			mutate: func(g *Graph) {
				g.Nodes[1].Config = raw(`{"duration":"tomorrow"}`)
			},
			wantErr: "invalid duration",
		},
		{
			name: "scheduled entry without segment",
			mutate: func(g *Graph) {
				g.Nodes[0].Config = raw(`{"trigger":"scheduled","schedule":"* * * * *"}`)
			},
			wantErr: "requires segment_id",
		},
		{
			name: "invalid scheduled entry expression",
			mutate: func(g *Graph) {
				g.Nodes[0].Config = raw(`{"trigger":"scheduled","segment_id":"seg-1","schedule":"not a schedule"}`)
			},
			wantErr: "invalid schedule",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			graph := canonicalGraph()
			tc.mutate(&graph)
			err := Validate(&graph)
			if err == nil {
				t.Fatalf("expected error")
			}
			if !strings.Contains(err.Error(), tc.wantErr) {
				t.Fatalf("expected error containing %q, got %v", tc.wantErr, err)
			}
		})
	}
}

func TestValidateEntryNodeMustMatchEntryNodeID(t *testing.T) {
	graph := canonicalGraph()
	graph.EntryNodeID = "n2"
	err := Validate(&graph)
	if err == nil {
		t.Fatalf("expected entry mismatch error")
	}
	if !strings.Contains(err.Error(), "does not match entry_node_id") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestValidateSplitBranchesMatchConfig(t *testing.T) {
	graph := canonicalGraph()
	for i := range graph.Edges {
		if graph.Edges[i].From == "n4" && graph.Edges[i].Branch == "b" {
			graph.Edges = append(graph.Edges[:i], graph.Edges[i+1:]...)
			break
		}
	}
	err := Validate(&graph)
	if err == nil {
		t.Fatalf("expected split branch error")
	}
	if !strings.Contains(err.Error(), "split node n4 must have exactly 2 outgoing edges") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestValidateAIDecisionRequiresPinnedPromptBoundedCallAndFallback(t *testing.T) {
	base := func(config string) Graph {
		return Graph{
			EntryNodeID: "entry",
			Nodes: []Node{
				{ID: "entry", Type: NodeTypeEntry, Config: raw(`{"trigger":"event","event_type":"signup.completed"}`)},
				{ID: "decision", Type: NodeTypeAIDecision, Config: raw(config)},
				{ID: "yes", Type: NodeTypeExit, Config: raw(`{"reason":"yes"}`)},
				{ID: "no", Type: NodeTypeExit, Config: raw(`{"reason":"no"}`)},
			},
			Edges: []Edge{
				{From: "entry", To: "decision"},
				{From: "decision", To: "yes", Branch: "yes"},
				{From: "decision", To: "no", Branch: "no"},
			},
		}
	}

	tests := []struct {
		name    string
		config  string
		wantErr string
	}{
		{
			name:    "missing prompt version",
			config:  `{"timeout_ms":100,"max_cost_cents":10,"branches":["yes","no"],"fallback":"no"}`,
			wantErr: "requires prompt_version_id",
		},
		{
			name:    "timeout too large",
			config:  `{"prompt_version_id":"pv-1","timeout_ms":5001,"max_cost_cents":10,"branches":["yes","no"],"fallback":"no"}`,
			wantErr: "exceeds maximum",
		},
		{
			name:    "missing fallback branch",
			config:  `{"prompt_version_id":"pv-1","timeout_ms":100,"max_cost_cents":10,"branches":["yes","no"],"fallback":"other"}`,
			wantErr: "fallback branch",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			graph := base(tc.config)
			if err := Validate(&graph); err == nil || !strings.Contains(err.Error(), tc.wantErr) {
				t.Fatalf("Validate error = %v, want %q", err, tc.wantErr)
			}
		})
	}

	valid := base(`{"prompt_version_id":"pv-1","timeout_ms":5000,"max_cost_cents":10,"branches":["yes","no"],"fallback":"no"}`)
	if err := Validate(&valid); err != nil {
		t.Fatalf("bounded ai_decision should validate: %v", err)
	}
}

func canonicalGraph() Graph {
	return Graph{
		EntryNodeID: "n1",
		Nodes: []Node{
			{ID: "n1", Type: NodeTypeEntry, Config: raw(`{"trigger":"event","event_type":"signup.completed"}`)},
			{ID: "n2", Type: NodeTypeDelay, Config: raw(`{"duration":"1h"}`)},
			{ID: "n3", Type: NodeTypeCondition, Config: raw(`{"dsl":{"type":"profile_attribute","field":"country","operator":"equals","value":"US"}}`)},
			{ID: "n4", Type: NodeTypeSplit, Config: raw(`{"mode":"random","branches":[{"label":"a","weight":50},{"label":"b","weight":50}]}`)},
			{ID: "n5", Type: NodeTypeMessage, Config: raw(`{"template_id":"tmpl-1","transactional":false}`)},
			{ID: "n6", Type: NodeTypeWaitEvent, Config: raw(`{"event_type":"email.opened","timeout":"72h"}`)},
			{ID: "n7", Type: NodeTypeAction, Config: raw(`{"action":"profile_update","set":{"stage":"engaged"}}`)},
			{ID: "n8", Type: NodeTypeGoal, Config: raw(`{"name":"activated"}`)},
			{ID: "n9", Type: NodeTypeExit, Config: raw(`{"reason":"completed"}`)},
		},
		Edges: []Edge{
			{From: "n1", To: "n2"},
			{From: "n2", To: "n3"},
			{From: "n3", To: "n4", Branch: "true"},
			{From: "n3", To: "n9", Branch: "false"},
			{From: "n4", To: "n5", Branch: "a"},
			{From: "n4", To: "n9", Branch: "b"},
			{From: "n5", To: "n6"},
			{From: "n6", To: "n7", Branch: "success"},
			{From: "n6", To: "n9", Branch: "timeout"},
			{From: "n7", To: "n8"},
			{From: "n8", To: "n9"},
		},
	}
}

func raw(value string) json.RawMessage {
	return json.RawMessage(value)
}
