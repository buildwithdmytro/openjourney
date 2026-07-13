package journey

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/buildwithdmytro/openjourney/internal/domain"
	"github.com/buildwithdmytro/openjourney/internal/experiment"
	"github.com/buildwithdmytro/openjourney/internal/ports"
)

const (
	NodeTypeEntry     = "entry"
	NodeTypeDelay     = "delay"
	NodeTypeCondition = "condition"
	NodeTypeSplit     = "split"
	NodeTypeMessage   = "message"
	NodeTypeWaitEvent = "wait_event"
	NodeTypeAction    = "action"
	NodeTypeGoal      = "goal"
	NodeTypeExit      = "exit"
)

type EntryConfig struct {
	Trigger       string `json:"trigger"`
	EventType     string `json:"event_type,omitempty"`
	SegmentID     string `json:"segment_id,omitempty"`
	Schedule      string `json:"schedule,omitempty"`
	ReentryPolicy string `json:"reentry_policy,omitempty"`
	MaxReentries  int    `json:"max_reentries,omitempty"`
	LatePolicy    string `json:"late_policy,omitempty"`
}

type DelayConfig struct {
	Duration string `json:"duration"`
}

type ConditionConfig struct {
	DSL json.RawMessage `json:"dsl"`
}

type SplitConfig struct {
	Mode         string        `json:"mode"`
	ExperimentID string        `json:"experiment_id,omitempty"`
	Branches     []SplitBranch `json:"branches"`
}

type SplitBranch struct {
	Label     string `json:"label"`
	Weight    int    `json:"weight,omitempty"`
	SegmentID string `json:"segment_id,omitempty"`
}

type MessageConfig struct {
	TemplateID    string `json:"template_id"`
	ExperimentID  string `json:"experiment_id,omitempty"`
	Channel       string `json:"channel,omitempty"`
	Transactional bool   `json:"transactional"`
}

type WaitConfig struct {
	EventType string `json:"event_type"`
	Timeout   string `json:"timeout"`
}

type ActionConfig struct {
	Action string         `json:"action"`
	Set    map[string]any `json:"set,omitempty"`
}

type GoalConfig struct {
	Name string `json:"name"`
}

type ExitConfig struct {
	Reason string `json:"reason"`
}

func ParseGraph(data []byte) (*Graph, error) {
	var graph Graph
	if err := json.Unmarshal(data, &graph); err != nil {
		return nil, err
	}
	for _, node := range graph.Nodes {
		if _, err := DecodeConfig(node); err != nil {
			return nil, err
		}
	}
	return &graph, nil
}

func DecodeConfig(node Node) (any, error) {
	switch node.Type {
	case NodeTypeEntry:
		var cfg EntryConfig
		return cfg, decodeNodeConfig(node, &cfg)
	case NodeTypeDelay:
		var cfg DelayConfig
		return cfg, decodeNodeConfig(node, &cfg)
	case NodeTypeCondition:
		var cfg ConditionConfig
		return cfg, decodeNodeConfig(node, &cfg)
	case NodeTypeSplit:
		var cfg SplitConfig
		return cfg, decodeNodeConfig(node, &cfg)
	case NodeTypeMessage:
		var cfg MessageConfig
		return cfg, decodeNodeConfig(node, &cfg)
	case NodeTypeWaitEvent:
		var cfg WaitConfig
		return cfg, decodeNodeConfig(node, &cfg)
	case NodeTypeAction:
		// Action side effects are deliberately idempotent-at-least-once: the profile
		// merge is idempotent for the same `set` map and AcceptEvents deduplicates the
		// deterministic run+node key. A worker crash before AdvanceRunTx may replay
		// both operations, but cannot create a second accepted event or a different
		// profile result.
		var cfg ActionConfig
		return cfg, decodeNodeConfig(node, &cfg)
	case NodeTypeGoal:
		var cfg GoalConfig
		return cfg, decodeNodeConfig(node, &cfg)
	case NodeTypeExit:
		var cfg ExitConfig
		return cfg, decodeNodeConfig(node, &cfg)
	case "ai_decision", "feature_flag", "nested_journey", "webhook_action", "integration_action", "experiment", "holdout":
		return nil, fmt.Errorf("unsupported node type: %s", node.Type)
	default:
		return nil, fmt.Errorf("unknown node type: %s", node.Type)
	}
}

func decodeNodeConfig(node Node, dest any) error {
	if len(node.Config) == 0 {
		node.Config = json.RawMessage("{}")
	}
	if err := json.Unmarshal(node.Config, dest); err != nil {
		return fmt.Errorf("decode %s node config: %w", node.Type, err)
	}
	return nil
}

type ExecutionResult struct {
	NextNodeID    string
	NextStep      *domain.JourneyStep
	Transition    domain.JourneyTransition
	NextStatus    string
	CompletedAt   *time.Time
	GoalReached   bool
	WaitEventType *string
	WaitUntil     *time.Time
	State         json.RawMessage
	MessageIntent *domain.JourneyMessageIntent
}

func (n *Node) Execute(ctx context.Context, store ports.Store, run *domain.JourneyRun, graph *Graph, now time.Time, stepKind string) (ExecutionResult, error) {
	var nextNodeID string
	var nextStep *domain.JourneyStep
	var trans domain.JourneyTransition
	var nextStatus = run.Status
	var completedAt = run.CompletedAt
	var goalReached = run.GoalReached
	var waitEventType *string
	var waitUntil *time.Time
	var nextState = run.State
	var messageIntent *domain.JourneyMessageIntent

	switch n.Type {
	case NodeTypeEntry:
		nxt, err := findNextNode(graph, n.ID, "")
		if err != nil {
			return ExecutionResult{}, fmt.Errorf("find successor: %w", err)
		}
		nextNodeID = nxt
		nextStep = &domain.JourneyStep{
			RunID:       run.ID,
			TenantID:    run.TenantID,
			NodeID:      nextNodeID,
			Kind:        "advance",
			Status:      "pending",
			AvailableAt: now,
		}
		trans = domain.JourneyTransition{
			RunID:    run.ID,
			TenantID: run.TenantID,
			FromNode: &n.ID,
			ToNode:   &nextNodeID,
			NodeType: NodeTypeEntry,
			Outcome:  "advanced",
			Detail:   json.RawMessage("{}"),
		}

	case NodeTypeDelay:
		var cfg DelayConfig
		if err := decodeNodeConfig(*n, &cfg); err != nil {
			return ExecutionResult{}, err
		}
		dur, err := time.ParseDuration(cfg.Duration)
		if err != nil {
			return ExecutionResult{}, fmt.Errorf("invalid delay duration %q: %w", cfg.Duration, err)
		}
		nxt, err := findNextNode(graph, n.ID, "")
		if err != nil {
			return ExecutionResult{}, fmt.Errorf("find successor: %w", err)
		}
		nextNodeID = nxt
		nextStep = &domain.JourneyStep{
			RunID:       run.ID,
			TenantID:    run.TenantID,
			NodeID:      nextNodeID,
			Kind:        "advance",
			Status:      "pending",
			AvailableAt: now.Add(dur),
		}
		trans = domain.JourneyTransition{
			RunID:    run.ID,
			TenantID: run.TenantID,
			FromNode: &n.ID,
			ToNode:   &nextNodeID,
			NodeType: NodeTypeDelay,
			Outcome:  "waited",
			Detail:   json.RawMessage("{}"),
		}

	case NodeTypeCondition:
		var cfg ConditionConfig
		if err := decodeNodeConfig(*n, &cfg); err != nil {
			return ExecutionResult{}, err
		}
		p := domain.Principal{TenantID: run.TenantID, WorkspaceID: run.WorkspaceID}
		matched, err := store.EvaluateAudience(ctx, p, run.ProfileID, cfg.DSL)
		if err != nil {
			return ExecutionResult{}, fmt.Errorf("evaluate condition: %w", err)
		}

		branch := "false"
		if matched {
			branch = "true"
		}
		nxt, err := findNextNode(graph, n.ID, branch)
		if err != nil {
			return ExecutionResult{}, fmt.Errorf("find successor branch %q: %w", branch, err)
		}
		nextNodeID = nxt
		nextStep = &domain.JourneyStep{
			RunID:       run.ID,
			TenantID:    run.TenantID,
			NodeID:      nextNodeID,
			Kind:        "advance",
			Status:      "pending",
			AvailableAt: now,
		}
		trans = domain.JourneyTransition{
			RunID:    run.ID,
			TenantID: run.TenantID,
			FromNode: &n.ID,
			ToNode:   &nextNodeID,
			NodeType: NodeTypeCondition,
			Outcome:  "branch:" + branch,
			Detail:   json.RawMessage("{}"),
		}

	case NodeTypeSplit:
		var cfg SplitConfig
		if err := decodeNodeConfig(*n, &cfg); err != nil {
			return ExecutionResult{}, err
		}
		var branch string
		if cfg.ExperimentID != "" {
			p := domain.Principal{TenantID: run.TenantID, WorkspaceID: run.WorkspaceID}
			exp, err := store.GetExperiment(ctx, p, cfg.ExperimentID)
			if err != nil {
				return ExecutionResult{}, fmt.Errorf("get split experiment: %w", err)
			}
			variants := make([]experiment.Variant, 0, len(exp.Variants))
			for _, candidate := range exp.Variants {
				variants = append(variants, experiment.Variant{Label: candidate.Label, Weight: candidate.Weight})
			}
			computed, _ := experiment.Assign(exp.Seed, run.ProfileID, variants, exp.HoldoutPct)
			stored, err := store.AssignExperiment(ctx, p, exp.ID, run.ProfileID, computed)
			if err != nil {
				return ExecutionResult{}, fmt.Errorf("record split experiment assignment: %w", err)
			}
			branch = stored.Variant
		} else if cfg.Mode == "random" {
			bucket := experiment.BucketOf(run.ProfileID+":"+n.ID, 100)
			var cumulative uint64
			for _, br := range cfg.Branches {
				cumulative += uint64(br.Weight)
				if bucket < cumulative {
					branch = br.Label
					break
				}
			}
			if branch == "" && len(cfg.Branches) > 0 {
				branch = cfg.Branches[len(cfg.Branches)-1].Label
			}
		} else if cfg.Mode == "audience" {
			p := domain.Principal{TenantID: run.TenantID, WorkspaceID: run.WorkspaceID}
			for _, br := range cfg.Branches {
				if br.SegmentID == "" {
					branch = br.Label
					continue
				}
				matched, err := store.IsProfileInSegment(ctx, p, br.SegmentID, run.ProfileID)
				if err != nil {
					return ExecutionResult{}, fmt.Errorf("check segment membership: %w", err)
				}
				if matched {
					branch = br.Label
					break
				}
			}
			if branch == "" && len(cfg.Branches) > 0 {
				branch = cfg.Branches[len(cfg.Branches)-1].Label
			}
		} else {
			return ExecutionResult{}, fmt.Errorf("unsupported split mode %q", cfg.Mode)
		}

		nxt, err := findNextNode(graph, n.ID, branch)
		if err != nil {
			return ExecutionResult{}, fmt.Errorf("find successor branch %q: %w", branch, err)
		}
		nextNodeID = nxt
		nextStep = &domain.JourneyStep{
			RunID:       run.ID,
			TenantID:    run.TenantID,
			NodeID:      nextNodeID,
			Kind:        "advance",
			Status:      "pending",
			AvailableAt: now,
		}

		stateMap := make(map[string]any)
		if len(run.State) > 0 && string(run.State) != "{}" && string(run.State) != "null" {
			if err := json.Unmarshal(run.State, &stateMap); err != nil {
				return ExecutionResult{}, fmt.Errorf("unmarshal state: %w", err)
			}
		}
		stateMap[n.ID] = branch
		stateBytes, err := json.Marshal(stateMap)
		if err != nil {
			return ExecutionResult{}, fmt.Errorf("marshal state: %w", err)
		}
		nextState = json.RawMessage(stateBytes)

		trans = domain.JourneyTransition{
			RunID:    run.ID,
			TenantID: run.TenantID,
			FromNode: &n.ID,
			ToNode:   &nextNodeID,
			NodeType: NodeTypeSplit,
			Outcome:  "branch:" + branch,
			Detail:   json.RawMessage("{}"),
		}

	case NodeTypeAction:
		var cfg ActionConfig
		if err := decodeNodeConfig(*n, &cfg); err != nil {
			return ExecutionResult{}, err
		}
		if cfg.Action != "profile_update" {
			return ExecutionResult{}, fmt.Errorf("unsupported action type: %s", cfg.Action)
		}
		p := domain.Principal{TenantID: run.TenantID, WorkspaceID: run.WorkspaceID}
		err := store.UpdateProfileAttributes(ctx, p, run.ProfileID, cfg.Set)
		if err != nil {
			return ExecutionResult{}, fmt.Errorf("update profile attributes: %w", err)
		}

		payloadBytes, err := json.Marshal(map[string]any{"attributes": cfg.Set})
		if err != nil {
			return ExecutionResult{}, fmt.Errorf("marshal event payload: %w", err)
		}
		events := []domain.Event{
			{
				Type:           "profile.updated",
				SchemaVersion:  1,
				ExternalID:     run.ID,
				IdempotencyKey: fmt.Sprintf("journey-action:%s:%s", run.ID, n.ID),
				OccurredAt:     now,
				Payload:        payloadBytes,
			},
		}
		_, err = store.AcceptEvents(ctx, p, events)
		if err != nil {
			return ExecutionResult{}, fmt.Errorf("emit profile.updated event: %w", err)
		}

		nxt, err := findNextNode(graph, n.ID, "")
		if err != nil {
			return ExecutionResult{}, fmt.Errorf("find successor: %w", err)
		}
		nextNodeID = nxt
		nextStep = &domain.JourneyStep{
			RunID:       run.ID,
			TenantID:    run.TenantID,
			NodeID:      nextNodeID,
			Kind:        "advance",
			Status:      "pending",
			AvailableAt: now,
		}
		trans = domain.JourneyTransition{
			RunID:    run.ID,
			TenantID: run.TenantID,
			FromNode: &n.ID,
			ToNode:   &nextNodeID,
			NodeType: NodeTypeAction,
			Outcome:  "advanced",
			Detail:   json.RawMessage("{}"),
		}

	case NodeTypeGoal:
		goalReached = true
		nxt, err := findNextNode(graph, n.ID, "")
		if err != nil {
			return ExecutionResult{}, fmt.Errorf("find successor: %w", err)
		}
		nextNodeID = nxt
		nextStep = &domain.JourneyStep{
			RunID:       run.ID,
			TenantID:    run.TenantID,
			NodeID:      nextNodeID,
			Kind:        "advance",
			Status:      "pending",
			AvailableAt: now,
		}
		trans = domain.JourneyTransition{
			RunID:    run.ID,
			TenantID: run.TenantID,
			FromNode: &n.ID,
			ToNode:   &nextNodeID,
			NodeType: NodeTypeGoal,
			Outcome:  "goal_reached",
			Detail:   json.RawMessage("{}"),
		}

	case NodeTypeExit:
		nextStatus = "completed"
		completedAt = &now
		trans = domain.JourneyTransition{
			RunID:    run.ID,
			TenantID: run.TenantID,
			FromNode: &n.ID,
			ToNode:   nil,
			NodeType: NodeTypeExit,
			Outcome:  "exited",
			Detail:   json.RawMessage("{}"),
		}

	case NodeTypeMessage:
		var cfg MessageConfig
		if err := decodeNodeConfig(*n, &cfg); err != nil {
			return ExecutionResult{}, err
		}
		templateID := cfg.TemplateID
		var experimentID *string
		variant := ""
		holdout := false
		if cfg.ExperimentID != "" {
			p := domain.Principal{TenantID: run.TenantID, WorkspaceID: run.WorkspaceID}
			exp, err := store.GetExperiment(ctx, p, cfg.ExperimentID)
			if err != nil {
				return ExecutionResult{}, fmt.Errorf("get message experiment: %w", err)
			}
			variants := make([]experiment.Variant, 0, len(exp.Variants))
			for _, candidate := range exp.Variants {
				variants = append(variants, experiment.Variant{Label: candidate.Label, Weight: candidate.Weight})
			}
			computed, _ := experiment.Assign(exp.Seed, run.ProfileID, variants, exp.HoldoutPct)
			stored, err := store.AssignExperiment(ctx, p, exp.ID, run.ProfileID, computed)
			if err != nil {
				return ExecutionResult{}, fmt.Errorf("record message experiment assignment: %w", err)
			}
			variant = stored.Variant
			holdout = variant == "holdout"
			experimentID = &exp.ID
			for _, candidate := range exp.Variants {
				if candidate.Label == variant && candidate.TemplateID != nil && *candidate.TemplateID != "" {
					templateID = *candidate.TemplateID
					break
				}
			}
		}
		profile, err := store.GetProfileByIDSystem(ctx, run.TenantID, run.WorkspaceID, run.ProfileID)
		if err != nil {
			return ExecutionResult{}, fmt.Errorf("get profile for message node: %w", err)
		}
		var attrs map[string]any
		if len(profile.Attributes) > 0 && string(profile.Attributes) != "null" {
			if err := json.Unmarshal(profile.Attributes, &attrs); err != nil {
				return ExecutionResult{}, fmt.Errorf("unmarshal profile attributes: %w", err)
			}
		}
		if attrs == nil {
			attrs = make(map[string]any)
		}
		channel := cfg.Channel
		if channel == "" {
			channel = "email"
		}
		var endpoint string
		if channel == "email" {
			endpoint, _ = attrs["email"].(string)
		} else if channel == "sms" {
			endpoint, _ = attrs["phone"].(string)
		} else {
			endpoint, _ = attrs[channel].(string)
		}

		nxt, err := findNextNode(graph, n.ID, "")
		if err != nil {
			return ExecutionResult{}, fmt.Errorf("find successor: %w", err)
		}
		nextNodeID = nxt
		nextStep = &domain.JourneyStep{
			RunID:       run.ID,
			TenantID:    run.TenantID,
			NodeID:      nextNodeID,
			Kind:        "advance",
			Status:      "pending",
			AvailableAt: now,
		}
		trans = domain.JourneyTransition{
			RunID:    run.ID,
			TenantID: run.TenantID,
			FromNode: &n.ID,
			ToNode:   &nextNodeID,
			NodeType: NodeTypeMessage,
			Outcome:  "intent_created",
			Detail:   json.RawMessage("{}"),
		}

		messageIntent = &domain.JourneyMessageIntent{
			RunID:            run.ID,
			TenantID:         run.TenantID,
			WorkspaceID:      run.WorkspaceID,
			JourneyID:        run.JourneyID,
			JourneyVersionID: run.JourneyVersionID,
			NodeID:           n.ID,
			ProfileID:        run.ProfileID,
			ExperimentID:     experimentID,
			Variant:          variant,
			TemplateID:       templateID,
			Channel:          channel,
			Endpoint:         endpoint,
			Transactional:    cfg.Transactional,
			Status:           "pending",
			AvailableAt:      now,
			PolicySnapshot:   json.RawMessage("{}"),
		}
		if holdout {
			decision, reason := "holdout", "experiment holdout"
			messageIntent.Status = "completed"
			messageIntent.Decision = &decision
			messageIntent.Reason = &reason
			trans.Outcome = "holdout"
		}

	case NodeTypeWaitEvent:
		var cfg WaitConfig
		if err := decodeNodeConfig(*n, &cfg); err != nil {
			return ExecutionResult{}, err
		}
		duration, err := time.ParseDuration(cfg.Timeout)
		if err != nil {
			return ExecutionResult{}, fmt.Errorf("invalid timeout duration %q: %w", cfg.Timeout, err)
		}
		timeoutAt := now.Add(duration)

		if stepKind == "timeout" {
			nxt, err := findNextNode(graph, n.ID, "timeout")
			if err != nil {
				return ExecutionResult{}, fmt.Errorf("find successor branch timeout: %w", err)
			}
			nextNodeID = nxt
			nextStep = &domain.JourneyStep{
				RunID:       run.ID,
				TenantID:    run.TenantID,
				NodeID:      nextNodeID,
				Kind:        "advance",
				Status:      "pending",
				AvailableAt: now,
			}
			trans = domain.JourneyTransition{
				RunID:    run.ID,
				TenantID: run.TenantID,
				FromNode: &n.ID,
				ToNode:   &nextNodeID,
				NodeType: NodeTypeWaitEvent,
				Outcome:  "timeout",
				Detail:   json.RawMessage("{}"),
			}
			nextStatus = "active"
		} else {
			nextStatus = "waiting"
			waitEventType = &cfg.EventType
			waitUntil = &timeoutAt
			nextNodeID = n.ID
			nextStep = &domain.JourneyStep{
				RunID:       run.ID,
				TenantID:    run.TenantID,
				NodeID:      n.ID,
				Kind:        "timeout",
				Status:      "pending",
				AvailableAt: timeoutAt,
			}
			trans = domain.JourneyTransition{
				RunID:    run.ID,
				TenantID: run.TenantID,
				FromNode: &n.ID,
				ToNode:   nil,
				NodeType: NodeTypeWaitEvent,
				Outcome:  "waiting",
				Detail:   json.RawMessage("{}"),
			}
		}

	default:
		return ExecutionResult{}, fmt.Errorf("unsupported node type: %s", n.Type)
	}

	return ExecutionResult{
		NextNodeID:    nextNodeID,
		NextStep:      nextStep,
		Transition:    trans,
		NextStatus:    nextStatus,
		CompletedAt:   completedAt,
		GoalReached:   goalReached,
		WaitEventType: waitEventType,
		WaitUntil:     waitUntil,
		State:         nextState,
		MessageIntent: messageIntent,
	}, nil
}

func findNextNode(graph *Graph, fromID string, branch string) (string, error) {
	for _, edge := range graph.Edges {
		if edge.From == fromID && edge.Branch == branch {
			return edge.To, nil
		}
	}
	return "", fmt.Errorf("no edge from %s with branch %q", fromID, branch)
}
