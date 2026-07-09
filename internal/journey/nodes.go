package journey

import (
	"encoding/json"
	"fmt"
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
	Mode     string        `json:"mode"`
	Branches []SplitBranch `json:"branches"`
}

type SplitBranch struct {
	Label     string `json:"label"`
	Weight    int    `json:"weight,omitempty"`
	SegmentID string `json:"segment_id,omitempty"`
}

type MessageConfig struct {
	TemplateID    string `json:"template_id"`
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
