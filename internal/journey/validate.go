package journey

import (
	"errors"
	"fmt"
	"time"
)

const maxAIDecisionTimeout = 5 * time.Second
const maxExtensionTimeout = 10 * time.Second

func Validate(graph *Graph) error {
	if graph == nil {
		return errors.New("graph is required")
	}

	nodes := make(map[string]Node, len(graph.Nodes))
	entryCount := 0
	for _, node := range graph.Nodes {
		if node.ID == "" {
			return errors.New("node id is required")
		}
		if _, exists := nodes[node.ID]; exists {
			return fmt.Errorf("duplicate node id: %s", node.ID)
		}
		if _, err := DecodeConfig(node); err != nil {
			return err
		}
		if node.Type == NodeTypeEntry {
			entryCount++
			if node.ID != graph.EntryNodeID {
				return fmt.Errorf("entry node %s does not match entry_node_id %s", node.ID, graph.EntryNodeID)
			}
			cfgAny, err := DecodeConfig(node)
			if err != nil {
				return fmt.Errorf("entry node %s has invalid config: %w", node.ID, err)
			}
			cfg, ok := cfgAny.(EntryConfig)
			if !ok {
				return fmt.Errorf("entry node %s has invalid config", node.ID)
			}
			if cfg.Trigger == "scheduled" {
				if cfg.SegmentID == "" {
					return fmt.Errorf("entry node %s scheduled trigger requires segment_id", node.ID)
				}
				if _, err := validateSchedule(cfg.Schedule); err != nil {
					return fmt.Errorf("entry node %s has invalid schedule: %w", node.ID, err)
				}
			}
		}
		if err := validateDurations(node); err != nil {
			return err
		}
		nodes[node.ID] = node
	}
	if entryCount != 1 {
		return fmt.Errorf("expected exactly one entry node, got %d", entryCount)
	}
	if _, ok := nodes[graph.EntryNodeID]; !ok {
		return fmt.Errorf("entry_node_id references missing node: %s", graph.EntryNodeID)
	}

	outgoing := make(map[string][]Edge, len(nodes))
	for _, edge := range graph.Edges {
		if _, ok := nodes[edge.From]; !ok {
			return fmt.Errorf("edge references missing from node: %s", edge.From)
		}
		if _, ok := nodes[edge.To]; !ok {
			return fmt.Errorf("edge references missing to node: %s", edge.To)
		}
		outgoing[edge.From] = append(outgoing[edge.From], edge)
	}

	for _, node := range graph.Nodes {
		if err := validateOutgoing(node, outgoing[node.ID]); err != nil {
			return err
		}
	}

	reachable := reachableNodes(graph.EntryNodeID, outgoing)
	for _, node := range graph.Nodes {
		if !reachable[node.ID] {
			return fmt.Errorf("unreachable node: %s", node.ID)
		}
	}
	for _, node := range graph.Nodes {
		if reachable[node.ID] && node.Type == NodeTypeExit {
			return nil
		}
	}
	return errors.New("graph must have at least one reachable exit node")
}

func validateDurations(node Node) error {
	switch node.Type {
	case NodeTypeDelay:
		cfgAny, err := DecodeConfig(node)
		if err != nil {
			return err
		}
		cfg, ok := cfgAny.(DelayConfig)
		if !ok {
			return fmt.Errorf("delay node %s has invalid config", node.ID)
		}
		return validateDuration(node.ID, "duration", cfg.Duration)
	case NodeTypeWaitEvent:
		cfgAny, err := DecodeConfig(node)
		if err != nil {
			return err
		}
		cfg, ok := cfgAny.(WaitConfig)
		if !ok {
			return fmt.Errorf("wait_event node %s has invalid config", node.ID)
		}
		return validateDuration(node.ID, "timeout", cfg.Timeout)
	case NodeTypeAIDecision:
		cfgAny, err := DecodeConfig(node)
		if err != nil {
			return err
		}
		cfg, ok := cfgAny.(AIDecisionConfig)
		if !ok {
			return fmt.Errorf("ai_decision node %s has invalid config", node.ID)
		}
		if cfg.PromptVersionID == "" {
			return fmt.Errorf("ai_decision node %s requires prompt_version_id", node.ID)
		}
		if cfg.TimeoutMS <= 0 {
			return fmt.Errorf("ai_decision node %s requires a positive timeout_ms", node.ID)
		}
		if time.Duration(cfg.TimeoutMS)*time.Millisecond > maxAIDecisionTimeout {
			return fmt.Errorf("ai_decision node %s timeout_ms exceeds maximum of %d", node.ID, maxAIDecisionTimeout/time.Millisecond)
		}
		if cfg.MaxCostCents <= 0 {
			return fmt.Errorf("ai_decision node %s requires a positive max_cost_cents", node.ID)
		}
		return nil
	case NodeTypeExtensionAction, NodeTypeExtensionCondition:
		cfgAny, err := DecodeConfig(node)
		if err != nil {
			return err
		}
		cfg, ok := cfgAny.(ExtensionNodeConfig)
		if !ok {
			return fmt.Errorf("%s node %s has invalid config", node.Type, node.ID)
		}
		if cfg.ExtensionID == "" {
			return fmt.Errorf("%s node %s requires extension_id", node.Type, node.ID)
		}
		if cfg.ExtensionVersion <= 0 {
			return fmt.Errorf("%s node %s requires a positive extension_version", node.Type, node.ID)
		}
		if cfg.TimeoutMS <= 0 {
			return fmt.Errorf("%s node %s requires a positive timeout_ms", node.Type, node.ID)
		}
		if time.Duration(cfg.TimeoutMS)*time.Millisecond > maxExtensionTimeout {
			return fmt.Errorf("%s node %s timeout_ms exceeds maximum of %d", node.Type, node.ID, maxExtensionTimeout/time.Millisecond)
		}
		return nil
	default:
		return nil
	}
}

func validateDuration(nodeID, field, raw string) error {
	duration, err := time.ParseDuration(raw)
	if err != nil {
		return fmt.Errorf("node %s has invalid %s: %w", nodeID, field, err)
	}
	if duration < 0 {
		return fmt.Errorf("node %s has negative %s", nodeID, field)
	}
	return nil
}

func validateOutgoing(node Node, edges []Edge) error {
	switch node.Type {
	case NodeTypeEntry, NodeTypeDelay, NodeTypeAction, NodeTypeMessage, NodeTypeGoal:
		if len(edges) != 1 {
			return fmt.Errorf("%s node %s must have exactly one outgoing edge", node.Type, node.ID)
		}
		if edges[0].Branch != "" {
			return fmt.Errorf("%s node %s outgoing edge must be unlabeled", node.Type, node.ID)
		}
	case NodeTypeCondition:
		return validateExactBranches(node, edges, []string{"true", "false"})
	case NodeTypeSplit:
		cfgAny, err := DecodeConfig(node)
		if err != nil {
			return err
		}
		cfg, ok := cfgAny.(SplitConfig)
		if !ok {
			return fmt.Errorf("split node %s has invalid config", node.ID)
		}
		labels := make([]string, 0, len(cfg.Branches))
		for _, branch := range cfg.Branches {
			labels = append(labels, branch.Label)
		}
		return validateExactBranches(node, edges, labels)
	case NodeTypeWaitEvent:
		return validateExactBranches(node, edges, []string{"success", "timeout"})
	case NodeTypeAIDecision:
		cfgAny, err := DecodeConfig(node)
		if err != nil {
			return err
		}
		cfg, ok := cfgAny.(AIDecisionConfig)
		if !ok {
			return fmt.Errorf("ai_decision node %s has invalid config", node.ID)
		}
		if cfg.Fallback == "" {
			return fmt.Errorf("ai_decision node %s requires fallback", node.ID)
		}
		labels := append([]string(nil), cfg.Branches...)
		fallbackDeclared := false
		for _, label := range labels {
			if label == cfg.Fallback {
				fallbackDeclared = true
				break
			}
		}
		if !fallbackDeclared {
			return fmt.Errorf("ai_decision node %s fallback branch %q is not declared", node.ID, cfg.Fallback)
		}
		return validateExactBranches(node, edges, labels)
	case NodeTypeExtensionAction, NodeTypeExtensionCondition:
		cfgAny, err := DecodeConfig(node)
		if err != nil {
			return err
		}
		cfg, ok := cfgAny.(ExtensionNodeConfig)
		if !ok {
			return fmt.Errorf("%s node %s has invalid config", node.Type, node.ID)
		}
		if cfg.Fallback == "" {
			return fmt.Errorf("%s node %s requires fallback", node.Type, node.ID)
		}
		labels := append([]string(nil), cfg.Branches...)
		fallbackDeclared := false
		for _, label := range labels {
			if label == cfg.Fallback {
				fallbackDeclared = true
				break
			}
		}
		if !fallbackDeclared {
			return fmt.Errorf("%s node %s fallback branch %q is not declared", node.Type, node.ID, cfg.Fallback)
		}
		return validateExactBranches(node, edges, labels)
	case NodeTypeExit:
		if len(edges) != 0 {
			return fmt.Errorf("exit node %s must not have outgoing edges", node.ID)
		}
	default:
		if _, err := DecodeConfig(node); err != nil {
			return err
		}
	}
	return nil
}

func validateExactBranches(node Node, edges []Edge, labels []string) error {
	if len(edges) != len(labels) {
		return fmt.Errorf("%s node %s must have exactly %d outgoing edges", node.Type, node.ID, len(labels))
	}
	want := make(map[string]bool, len(labels))
	for _, label := range labels {
		if label == "" {
			return fmt.Errorf("%s node %s has empty branch label in config", node.Type, node.ID)
		}
		if want[label] {
			return fmt.Errorf("%s node %s has duplicate branch label: %s", node.Type, node.ID, label)
		}
		want[label] = true
	}
	for _, edge := range edges {
		if !want[edge.Branch] {
			return fmt.Errorf("%s node %s has unexpected branch label: %s", node.Type, node.ID, edge.Branch)
		}
		delete(want, edge.Branch)
	}
	if len(want) != 0 {
		for label := range want {
			return fmt.Errorf("%s node %s is missing branch label: %s", node.Type, node.ID, label)
		}
	}
	return nil
}

func reachableNodes(entryNodeID string, outgoing map[string][]Edge) map[string]bool {
	reachable := map[string]bool{entryNodeID: true}
	queue := []string{entryNodeID}
	for len(queue) > 0 {
		current := queue[0]
		queue = queue[1:]
		for _, edge := range outgoing[current] {
			if reachable[edge.To] {
				continue
			}
			reachable[edge.To] = true
			queue = append(queue, edge.To)
		}
	}
	return reachable
}
