package journey

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/buildwithdmytro/openjourney/internal/ai"
	"github.com/buildwithdmytro/openjourney/internal/domain"
	"github.com/buildwithdmytro/openjourney/internal/experiment"
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
	for _, nodeType := range []string{"feature_flag", "nested_journey", "webhook_action", "integration_action", "experiment", "holdout"} {
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
			{"id":"n2","type":"feature_flag","config":{}}
		],
		"edges": [{"from":"n1","to":"n2"}]
	}`))
	if err == nil {
		t.Fatalf("expected unsupported node type error")
	}
	if !strings.Contains(err.Error(), "unsupported node type: feature_flag") {
		t.Fatalf("unexpected error: %v", err)
	}
}

type decisionTestProvider struct {
	generate func(context.Context, ai.GenerateRequest) (*ai.GenerateResponse, error)
}

func (p decisionTestProvider) Generate(ctx context.Context, req ai.GenerateRequest) (*ai.GenerateResponse, error) {
	return p.generate(ctx, req)
}
func (decisionTestProvider) Embed(context.Context, ai.EmbedRequest) (*ai.EmbedResponse, error) {
	return &ai.EmbedResponse{}, nil
}
func (decisionTestProvider) Moderate(context.Context, ai.ModerateRequest) (*ai.ModerateResponse, error) {
	return &ai.ModerateResponse{}, nil
}

func (m *mockStore) GetDefaultAIProviderConfig(context.Context, domain.Principal) (domain.AIProviderConfig, error) {
	return domain.AIProviderConfig{Provider: "fake", Status: "active", Config: json.RawMessage(`{}`)}, nil
}
func (m *mockStore) GetPromptVersion(context.Context, domain.Principal, string) (domain.PromptVersion, error) {
	return domain.PromptVersion{Status: "active", EvalStatus: "passed", Provider: "fake", Model: "fake-model"}, nil
}
func (m *mockStore) GetAIBudgetUsage(context.Context, string, string, string) (domain.AIBudgetUsage, error) {
	return domain.AIBudgetUsage{}, nil
}
func (m *mockStore) IncrementAIBudgetUsage(context.Context, string, string, string, int64, int64, int64) error {
	return nil
}
func (m *mockStore) RecordAIActivity(_ context.Context, _ domain.Principal, activity domain.AIActivity) (domain.AIActivity, error) {
	activity.ID = "activity-1"
	return activity, nil
}

func TestAIDecisionTimeoutUsesFallbackAndValidOutputUsesBranch(t *testing.T) {
	newGateway := func(provider ai.ModelProvider) *ai.Gateway {
		store := newMockStore()
		g := ai.NewGateway(store)
		g.SetProviderFactory(func(ai.ProviderProfile) ai.ModelProvider { return provider })
		return g
	}
	graph := &Graph{
		Nodes: []Node{{ID: "decision", Type: NodeTypeAIDecision, Config: json.RawMessage(`{"prompt_version_id":"pv-1","prompt":"choose","timeout_ms":10,"max_cost_cents":10,"branches":["yes","no"],"fallback":"no"}`)}, {ID: "yes", Type: NodeTypeExit, Config: json.RawMessage(`{"reason":"yes"}`)}, {ID: "no", Type: NodeTypeExit, Config: json.RawMessage(`{"reason":"no"}`)}},
		Edges: []Edge{{From: "decision", To: "yes", Branch: "yes"}, {From: "decision", To: "no", Branch: "no"}},
	}
	run := &domain.JourneyRun{ID: "run-1", TenantID: "tenant-1", WorkspaceID: "workspace-1", ProfileID: "profile-1", Status: "active"}
	slow := decisionTestProvider{generate: func(ctx context.Context, _ ai.GenerateRequest) (*ai.GenerateResponse, error) {
		<-ctx.Done()
		return nil, ctx.Err()
	}}
	res, err := graph.Nodes[0].ExecuteWithGateway(context.Background(), newMockStore(), run, graph, time.Now(), "advance", newGateway(slow), nil)
	if err != nil || res.NextNodeID != "no" || res.Transition.Outcome != "branch:no" {
		t.Fatalf("timeout did not use fallback: result=%+v err=%v", res, err)
	}
	valid := decisionTestProvider{generate: func(context.Context, ai.GenerateRequest) (*ai.GenerateResponse, error) {
		return &ai.GenerateResponse{Content: `{"branch":"yes"}`, Usage: ai.Usage{CostCents: 1}}, nil
	}}
	res, err = graph.Nodes[0].ExecuteWithGateway(context.Background(), newMockStore(), run, graph, time.Now(), "advance", newGateway(valid), nil)
	if err != nil || res.NextNodeID != "yes" || res.Transition.Outcome != "branch:yes" {
		t.Fatalf("valid output did not use model branch: result=%+v err=%v", res, err)
	}
}

type executorMockStore struct {
	mockStore
	evaluatedAudience map[string]bool
	profileInSegment  map[string]bool
	updatedProfileID  string
	updatedAttrs      map[string]any
	acceptedEvents    []domain.Event
	experiments       map[string]domain.Experiment
	assignments       []domain.ExperimentAssignment
}

func (m *executorMockStore) GetExperiment(_ context.Context, _ domain.Principal, id string) (domain.Experiment, error) {
	return m.experiments[id], nil
}

func (m *executorMockStore) AssignExperiment(_ context.Context, p domain.Principal, experimentID, profileID, variant string) (domain.ExperimentAssignment, error) {
	assignment := domain.ExperimentAssignment{ExperimentID: experimentID, TenantID: p.TenantID, WorkspaceID: p.WorkspaceID, ProfileID: profileID, Variant: variant}
	m.assignments = append(m.assignments, assignment)
	return assignment, nil
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

func TestExecuteExperimentSplitMatchesSharedAssignment(t *testing.T) {
	exp := domain.Experiment{ID: "exp-1", Seed: "journey-seed", Variants: []domain.ExperimentVariant{{Label: "control", Weight: 40}, {Label: "variant", Weight: 60}}}
	store := &executorMockStore{experiments: map[string]domain.Experiment{exp.ID: exp}}
	run := &domain.JourneyRun{ID: "r1", TenantID: "t1", WorkspaceID: "w1", ProfileID: "profile-42", Status: "active"}
	graph := &Graph{
		Nodes: []Node{
			{ID: "split", Type: NodeTypeSplit, Config: json.RawMessage(`{"experiment_id":"exp-1","branches":[{"label":"control"},{"label":"variant"}]}`)},
			{ID: "control-node", Type: NodeTypeExit}, {ID: "variant-node", Type: NodeTypeExit},
		},
		Edges: []Edge{{From: "split", To: "control-node", Branch: "control"}, {From: "split", To: "variant-node", Branch: "variant"}},
	}

	res, err := graph.Nodes[0].Execute(context.Background(), store, run, graph, time.Now(), "advance")
	if err != nil {
		t.Fatalf("execute experiment split: %v", err)
	}
	want, _ := experiment.Assign(exp.Seed, run.ProfileID, []experiment.Variant{{Label: "control", Weight: 40}, {Label: "variant", Weight: 60}}, 0)
	if len(store.assignments) != 1 || store.assignments[0].Variant != want {
		t.Fatalf("recorded assignments = %+v, want variant %q", store.assignments, want)
	}
	if (want == "control" && res.NextNodeID != "control-node") || (want == "variant" && res.NextNodeID != "variant-node") {
		t.Fatalf("next node = %q for assignment %q", res.NextNodeID, want)
	}
}

func TestExecuteExperimentMessageSelectsTemplateStampsAndHoldsOut(t *testing.T) {
	variantTemplate := "variant-template"
	exp := domain.Experiment{ID: "exp-1", Seed: "seed", Variants: []domain.ExperimentVariant{{Label: "variant", Weight: 100, TemplateID: &variantTemplate}}}
	store := &executorMockStore{experiments: map[string]domain.Experiment{exp.ID: exp}}
	run := &domain.JourneyRun{ID: "r1", TenantID: "t1", WorkspaceID: "w1", JourneyID: "j1", JourneyVersionID: "jv1", ProfileID: "p1", Status: "active"}
	graph := &Graph{Nodes: []Node{{ID: "message", Type: NodeTypeMessage, Config: json.RawMessage(`{"template_id":"base-template","experiment_id":"exp-1"}`)}, {ID: "exit", Type: NodeTypeExit}}, Edges: []Edge{{From: "message", To: "exit"}}}

	res, err := graph.Nodes[0].Execute(context.Background(), store, run, graph, time.Now(), "advance")
	if err != nil {
		t.Fatalf("execute experiment message: %v", err)
	}
	if len(res.MessageIntents) != 1 || res.MessageIntents[0].TemplateID != variantTemplate || res.MessageIntents[0].ExperimentID == nil || *res.MessageIntents[0].ExperimentID != exp.ID || res.MessageIntents[0].Variant != "variant" {
		t.Fatalf("variant intent not selected/stamped: %+v", res.MessageIntents)
	}

	exp.HoldoutPct = 100
	store.experiments[exp.ID] = exp
	res, err = graph.Nodes[0].Execute(context.Background(), store, run, graph, time.Now(), "advance")
	if err != nil {
		t.Fatalf("execute holdout message: %v", err)
	}
	if len(res.MessageIntents) != 1 || res.MessageIntents[0].Status != "completed" || res.MessageIntents[0].Decision == nil || *res.MessageIntents[0].Decision != "holdout" || res.MessageIntents[0].Variant != "holdout" {
		t.Fatalf("holdout intent should be terminal and unsendable: %+v", res.MessageIntents)
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
	firstKey := store.acceptedEvents[0].IdempotencyKey
	if _, err := graph.Nodes[0].Execute(context.Background(), store, run, graph, now, "advance"); err != nil {
		t.Fatalf("replay action: %v", err)
	}
	if len(store.acceptedEvents) != 2 || store.acceptedEvents[1].IdempotencyKey != firstKey {
		t.Fatalf("action replay must use deterministic event key %q: %+v", firstKey, store.acceptedEvents)
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
	if len(res.MessageIntents) != 1 {
		t.Fatalf("expected 1 MessageIntent, but got %d", len(res.MessageIntents))
	}
	intent := res.MessageIntents[0]
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

type mockExtensionHost struct {
	invoke func(ctx context.Context, principal domain.Principal, extensionID string, invocation string, input json.RawMessage) (json.RawMessage, string, error)
}

func (m *mockExtensionHost) Invoke(ctx context.Context, principal domain.Principal, extensionID string, invocation string, input json.RawMessage) (json.RawMessage, string, error) {
	if m.invoke != nil {
		return m.invoke(ctx, principal, extensionID, invocation, input)
	}
	return nil, "", nil
}

func TestExtensionNodesExecution_Success(t *testing.T) {
	graph := &Graph{
		Nodes: []Node{
			{
				ID:   "n1",
				Type: NodeTypeExtensionAction,
				Config: json.RawMessage(`{
					"extension_id": "ext-123",
					"extension_version": 1,
					"timeout_ms": 1000,
					"branches": ["yes", "no"],
					"fallback": "no",
					"config": {"foo": "bar"}
				}`),
			},
			{ID: "yes", Type: NodeTypeExit},
			{ID: "no", Type: NodeTypeExit},
		},
		Edges: []Edge{
			{From: "n1", To: "yes", Branch: "yes"},
			{From: "n1", To: "no", Branch: "no"},
		},
	}

	run := &domain.JourneyRun{
		ID:          "run-1",
		TenantID:    "tenant-1",
		WorkspaceID: "workspace-1",
		ProfileID:   "profile-1",
		Status:      "active",
	}

	var capturedInput json.RawMessage
	host := &mockExtensionHost{
		invoke: func(ctx context.Context, p domain.Principal, extID string, invocation string, input json.RawMessage) (json.RawMessage, string, error) {
			capturedInput = input
			if extID != "ext-123" {
				return nil, "", fmt.Errorf("unexpected extID: %s", extID)
			}
			if invocation != "decide" {
				return nil, "", fmt.Errorf("unexpected invocation: %s", invocation)
			}
			return json.RawMessage(`{"branch": "yes"}`), "act-uuid-123", nil
		},
	}

	res, err := graph.Nodes[0].ExecuteWithGateway(context.Background(), newMockStore(), run, graph, time.Now(), "advance", nil, host)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if res.NextNodeID != "yes" {
		t.Errorf("expected NextNodeID 'yes', got %q", res.NextNodeID)
	}

	var detail map[string]string
	if err := json.Unmarshal(res.Transition.Detail, &detail); err != nil {
		t.Fatalf("failed to decode detail: %v", err)
	}
	if detail["extension_activity_id"] != "act-uuid-123" {
		t.Errorf("expected activity ID 'act-uuid-123', got %q", detail["extension_activity_id"])
	}

	var inputMap map[string]any
	if err := json.Unmarshal(capturedInput, &inputMap); err != nil {
		t.Fatalf("failed to decode captured input: %v", err)
	}
	if inputMap["profile_id"] != "profile-1" {
		t.Errorf("expected profile_id 'profile-1', got %v", inputMap["profile_id"])
	}
	cfgVal := inputMap["config"].(map[string]any)
	if cfgVal["foo"] != "bar" {
		t.Errorf("expected config.foo = 'bar', got %v", cfgVal["foo"])
	}
}

func TestExtensionNodesExecution_FallbackOnError(t *testing.T) {
	graph := &Graph{
		Nodes: []Node{
			{
				ID:   "n1",
				Type: NodeTypeExtensionCondition,
				Config: json.RawMessage(`{
					"extension_id": "ext-123",
					"extension_version": 2,
					"timeout_ms": 500,
					"branches": ["yes", "no"],
					"fallback": "no"
				}`),
			},
			{ID: "yes", Type: NodeTypeExit},
			{ID: "no", Type: NodeTypeExit},
		},
		Edges: []Edge{
			{From: "n1", To: "yes", Branch: "yes"},
			{From: "n1", To: "no", Branch: "no"},
		},
	}

	run := &domain.JourneyRun{
		ID:          "run-1",
		TenantID:    "tenant-1",
		WorkspaceID: "workspace-1",
		ProfileID:   "profile-1",
		Status:      "active",
	}

	// Host returns an error, should fallback to fallback branch deterministically without failing the step.
	host := &mockExtensionHost{
		invoke: func(ctx context.Context, p domain.Principal, extID string, invocation string, input json.RawMessage) (json.RawMessage, string, error) {
			return nil, "act-err-999", fmt.Errorf("remote extension unavailable")
		},
	}

	res, err := graph.Nodes[0].ExecuteWithGateway(context.Background(), newMockStore(), run, graph, time.Now(), "advance", nil, host)
	if err != nil {
		t.Fatalf("unexpected execution error: %v", err)
	}

	if res.NextNodeID != "no" {
		t.Errorf("expected NextNodeID to be fallback 'no', got %q", res.NextNodeID)
	}

	var detail map[string]string
	_ = json.Unmarshal(res.Transition.Detail, &detail)
	if detail["extension_activity_id"] != "act-err-999" {
		t.Errorf("expected activity ID 'act-err-999', got %q", detail["extension_activity_id"])
	}
}

func TestExtensionNodesExecution_FallbackOnUnconfigured(t *testing.T) {
	graph := &Graph{
		Nodes: []Node{
			{
				ID:   "n1",
				Type: NodeTypeExtensionAction,
				Config: json.RawMessage(`{
					"extension_id": "ext-123",
					"extension_version": 1,
					"timeout_ms": 1000,
					"branches": ["yes", "no"],
					"fallback": "yes"
				}`),
			},
			{ID: "yes", Type: NodeTypeExit},
			{ID: "no", Type: NodeTypeExit},
		},
		Edges: []Edge{
			{From: "n1", To: "yes", Branch: "yes"},
			{From: "n1", To: "no", Branch: "no"},
		},
	}

	run := &domain.JourneyRun{
		ID:          "run-1",
		TenantID:    "tenant-1",
		WorkspaceID: "workspace-1",
		ProfileID:   "profile-1",
		Status:      "active",
	}

	// Passing nil host (unconfigured) should immediately fallback to Fallback branch.
	res, err := graph.Nodes[0].ExecuteWithGateway(context.Background(), newMockStore(), run, graph, time.Now(), "advance", nil, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if res.NextNodeID != "yes" {
		t.Errorf("expected NextNodeID to be fallback 'yes', got %q", res.NextNodeID)
	}
}

func TestExtensionNodesExecution_Validation(t *testing.T) {
	tests := []struct {
		name    string
		graph   *Graph
		wantErr string
	}{
		{
			name: "missing extension_id",
			graph: &Graph{
				EntryNodeID: "n1",
				Nodes: []Node{
					{ID: "n1", Type: NodeTypeEntry, Config: json.RawMessage(`{"trigger":"event","event_type":"a"}`)},
					{ID: "n2", Type: NodeTypeExtensionAction, Config: json.RawMessage(`{"extension_version":1,"timeout_ms":1000,"branches":["ok"],"fallback":"ok"}`)},
					{ID: "ok", Type: NodeTypeExit},
				},
				Edges: []Edge{
					{From: "n1", To: "n2"},
					{From: "n2", To: "ok", Branch: "ok"},
				},
			},
			wantErr: "requires extension_id",
		},
		{
			name: "invalid timeout",
			graph: &Graph{
				EntryNodeID: "n1",
				Nodes: []Node{
					{ID: "n1", Type: NodeTypeEntry, Config: json.RawMessage(`{"trigger":"event","event_type":"a"}`)},
					{ID: "n2", Type: NodeTypeExtensionAction, Config: json.RawMessage(`{"extension_id":"ext-1","extension_version":1,"timeout_ms":20000,"branches":["ok"],"fallback":"ok"}`)},
					{ID: "ok", Type: NodeTypeExit},
				},
				Edges: []Edge{
					{From: "n1", To: "n2"},
					{From: "n2", To: "ok", Branch: "ok"},
				},
			},
			wantErr: "timeout_ms exceeds maximum",
		},
		{
			name: "fallback branch not declared",
			graph: &Graph{
				EntryNodeID: "n1",
				Nodes: []Node{
					{ID: "n1", Type: NodeTypeEntry, Config: json.RawMessage(`{"trigger":"event","event_type":"a"}`)},
					{ID: "n2", Type: NodeTypeExtensionAction, Config: json.RawMessage(`{"extension_id":"ext-1","extension_version":1,"timeout_ms":1000,"branches":["ok"],"fallback":"err"}`)},
					{ID: "ok", Type: NodeTypeExit},
				},
				Edges: []Edge{
					{From: "n1", To: "n2"},
					{From: "n2", To: "ok", Branch: "ok"},
				},
			},
			wantErr: "fallback branch \"err\" is not declared",
		},
		{
			name: "correct validation",
			graph: &Graph{
				EntryNodeID: "n1",
				Nodes: []Node{
					{ID: "n1", Type: NodeTypeEntry, Config: json.RawMessage(`{"trigger":"event","event_type":"a"}`)},
					{ID: "n2", Type: NodeTypeExtensionAction, Config: json.RawMessage(`{"extension_id":"ext-1","extension_version":1,"timeout_ms":1000,"branches":["ok"],"fallback":"ok"}`)},
					{ID: "ok", Type: NodeTypeExit},
				},
				Edges: []Edge{
					{From: "n1", To: "n2"},
					{From: "n2", To: "ok", Branch: "ok"},
				},
			},
			wantErr: "",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := Validate(tc.graph)
			if tc.wantErr == "" {
				if err != nil {
					t.Fatalf("expected no validation error, got %v", err)
				}
			} else {
				if err == nil {
					t.Fatal("expected validation error, got nil")
				}
				if !strings.Contains(err.Error(), tc.wantErr) {
					t.Fatalf("expected error containing %q, got %v", tc.wantErr, err)
				}
			}
		})
	}
}

