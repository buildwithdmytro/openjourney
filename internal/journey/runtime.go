package journey

import (
	"context"
	"fmt"
	"log/slog"

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

	now := deps.Clock.Now()

	res, err := node.Execute(ctx, store, &run, graph, now, step.Kind)
	if err != nil {
		slog.Error("failed to execute node", "error", err, "node_id", node.ID, "run_id", step.RunID)
		_ = store.FailJourneyStep(ctx, step.ID, fmt.Sprintf("execute node: %v", err))
		return true, nil
	}

	run.Status = res.NextStatus
	if res.NextNodeID != "" {
		run.CurrentNodeID = res.NextNodeID
	}
	run.CompletedAt = res.CompletedAt
	run.GoalReached = res.GoalReached
	run.State = res.State
	run.WaitEventType = res.WaitEventType
	run.WaitUntil = res.WaitUntil

	err = store.AdvanceRunTx(ctx, run.ID, run, step.ID, res.NextStep, res.Transition)
	if err != nil {
		slog.Error("failed to advance run transaction", "error", err, "run_id", run.ID)
		_ = store.FailJourneyStep(ctx, step.ID, fmt.Sprintf("advance run tx: %v", err))
		return true, nil
	}

	return true, nil
}
