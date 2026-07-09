package journey

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"

	"github.com/buildwithdmytro/openjourney/internal/domain"
	"github.com/buildwithdmytro/openjourney/internal/ports"
)

type Deps struct {
	Clock Clock
}

func TickNext(ctx context.Context, store ports.Store, deps Deps) (bool, error) {
	step, claimed, err := store.ClaimJourneyStep(ctx)
	if err != nil {
		return false, fmt.Errorf("claim journey step: %w", err)
	}
	if !claimed {
		return false, nil
	}

	slog.Info("processing journey step", "step_id", step.ID, "run_id", step.RunID, "node_id", step.NodeID, "kind", step.Kind)

	run, err := store.GetJourneyRunSystem(ctx, step.TenantID, step.RunID)
	if err != nil {
		slog.Error("failed to get journey run for step", "error", err, "run_id", step.RunID)
		_ = store.FailJourneyStep(ctx, step.ID, fmt.Sprintf("failed to get run: %v", err))
		return true, nil
	}

	version, err := store.GetJourneyVersion(ctx, run.TenantID, run.JourneyVersionID)
	if err != nil {
		slog.Error("failed to get journey version", "error", err, "version_id", run.JourneyVersionID)
		_ = store.FailJourneyStep(ctx, step.ID, fmt.Sprintf("failed to get version: %v", err))
		return true, nil
	}

	graph, err := ParseGraph(version.Graph)
	if err != nil {
		slog.Error("failed to parse journey graph", "error", err, "run_id", step.RunID)
		_ = store.FailJourneyStep(ctx, step.ID, fmt.Sprintf("failed to parse graph: %v", err))
		return true, nil
	}

	// Find node
	var node *Node
	for i := range graph.Nodes {
		if graph.Nodes[i].ID == step.NodeID {
			node = &graph.Nodes[i]
			break
		}
	}
	if node == nil {
		slog.Error("node not found in graph", "node_id", step.NodeID, "run_id", step.RunID)
		_ = store.FailJourneyStep(ctx, step.ID, fmt.Sprintf("node %s not found in graph", step.NodeID))
		return true, nil
	}

	var nextNodeID string
	var nextStep *domain.JourneyStep
	var trans domain.JourneyTransition
	var nextStatus = run.Status
	var completedAt = run.CompletedAt

	now := deps.Clock.Now()

	switch node.Type {
	case NodeTypeEntry:
		nxt, err := findNextNode(graph, node.ID, "")
		if err != nil {
			slog.Error("failed to find entry successor", "error", err, "node_id", node.ID)
			_ = store.FailJourneyStep(ctx, step.ID, fmt.Sprintf("entry successor: %v", err))
			return true, nil
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
			FromNode: &node.ID,
			ToNode:   &nextNodeID,
			NodeType: "entry",
			Outcome:  "advanced",
			Detail:   json.RawMessage("{}"),
		}

	case NodeTypeExit:
		nextStatus = "completed"
		completedAt = &now
		trans = domain.JourneyTransition{
			RunID:    run.ID,
			TenantID: run.TenantID,
			FromNode: &node.ID,
			ToNode:   nil,
			NodeType: "exit",
			Outcome:  "completed",
			Detail:   json.RawMessage("{}"),
		}

	default:
		slog.Error("unsupported node type in runtime skeleton", "type", node.Type)
		_ = store.FailJourneyStep(ctx, step.ID, fmt.Sprintf("unsupported node type: %s", node.Type))
		return true, nil
	}

	run.Status = nextStatus
	if nextNodeID != "" {
		run.CurrentNodeID = nextNodeID
	}
	run.CompletedAt = completedAt

	err = store.AdvanceRunTx(ctx, run.ID, run, step.ID, nextStep, trans)
	if err != nil {
		slog.Error("failed to advance run transaction", "error", err, "run_id", run.ID)
		_ = store.FailJourneyStep(ctx, step.ID, fmt.Sprintf("advance run tx: %v", err))
		return true, nil
	}

	return true, nil
}

func findNextNode(graph *Graph, fromID string, branch string) (string, error) {
	for _, edge := range graph.Edges {
		if edge.From == fromID && edge.Branch == branch {
			return edge.To, nil
		}
	}
	return "", fmt.Errorf("no edge from %s with branch %q", fromID, branch)
}
