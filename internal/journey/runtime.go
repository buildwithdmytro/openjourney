package journey

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"time"

	"github.com/buildwithdmytro/openjourney/internal/ai"
	"github.com/buildwithdmytro/openjourney/internal/domain"
	"github.com/buildwithdmytro/openjourney/internal/ports"
	"github.com/buildwithdmytro/openjourney/internal/telemetry"
	"go.opentelemetry.io/otel/attribute"
	otelmetric "go.opentelemetry.io/otel/metric"
)

type Deps struct {
	Clock         Clock
	LateThreshold time.Duration
	AIGateway     *ai.Gateway
	ExtensionHost ExtensionHost
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
		failStep(ctx, store, step, fmt.Sprintf("failed to get run: %v", err))
		return true, nil
	}

	version, err := store.GetJourneyVersion(ctx, run.TenantID, run.JourneyVersionID)
	if err != nil {
		slog.Error("failed to get journey version", "error", err, "version_id", run.JourneyVersionID)
		failStep(ctx, store, step, fmt.Sprintf("failed to get version: %v", err))
		return true, nil
	}

	graph, err := ParseGraph(version.Graph)
	if err != nil {
		slog.Error("failed to parse journey graph", "error", err, "run_id", step.RunID)
		failStep(ctx, store, step, fmt.Sprintf("failed to parse graph: %v", err))
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
		failStep(ctx, store, step, fmt.Sprintf("node %s not found in graph", step.NodeID))
		return true, nil
	}

	now := deps.Clock.Now()

	lateThreshold := deps.LateThreshold
	if lateThreshold == 0 {
		lateThreshold = 24 * time.Hour
	}

	if !step.AvailableAt.IsZero() && now.Sub(step.AvailableAt) > lateThreshold {
		slog.Warn("journey step is stale/late", "step_id", step.ID, "run_id", step.RunID, "node_id", step.NodeID, "available_at", step.AvailableAt, "policy", version.LatePolicy)
		switch version.LatePolicy {
		case "skip":
			var nextNodeID string
			for _, edge := range graph.Edges {
				if edge.From == node.ID {
					nextNodeID = edge.To
					break
				}
			}
			var nextStep *domain.JourneyStep
			if nextNodeID != "" {
				nextStep = &domain.JourneyStep{
					RunID:       run.ID,
					TenantID:    run.TenantID,
					NodeID:      nextNodeID,
					Kind:        "advance",
					Status:      "pending",
					AvailableAt: now,
				}
				run.CurrentNodeID = nextNodeID
				run.Status = "active"
			} else {
				run.Status = "completed"
				run.CompletedAt = &now
			}
			trans := domain.JourneyTransition{
				RunID:    run.ID,
				TenantID: run.TenantID,
				FromNode: &node.ID,
				ToNode:   &nextNodeID,
				NodeType: node.Type,
				Outcome:  "skipped",
				Detail:   json.RawMessage(`{"reason":"late"}`),
			}
			err = store.AdvanceRunTx(ctx, run.ID, run, step.ID, nextStep, trans, nil)
			if err != nil {
				slog.Error("failed to skip and advance run", "error", err, "run_id", run.ID)
				failStep(ctx, store, step, fmt.Sprintf("skip advance run tx: %v", err))
			} else {
				telemetry.JourneyStepsExecuted.Add(ctx, 1, otelmetric.WithAttributes(
					attribute.String("tenant_id", run.TenantID),
					attribute.String("journey_id", run.JourneyID),
					attribute.String("node_type", node.Type),
				))
				if run.Status == "completed" || run.Status == "exited" {
					telemetry.JourneyExits.Add(ctx, 1, otelmetric.WithAttributes(
						attribute.String("tenant_id", run.TenantID),
						attribute.String("journey_id", run.JourneyID),
					))
				}
			}
			return true, nil

		case "reschedule":
			var delay time.Duration
			if node.Type == NodeTypeDelay {
				var cfg DelayConfig
				if err := decodeNodeConfig(*node, &cfg); err == nil {
					if d, err := time.ParseDuration(cfg.Duration); err == nil {
						delay = d
					}
				}
			}
			if delay == 0 {
				delay = lateThreshold
			}
			rescheduleTime := now.Add(delay)
			err = store.RescheduleJourneyStep(ctx, step.ID, rescheduleTime)
			if err != nil {
				slog.Error("failed to reschedule journey step", "error", err, "step_id", step.ID)
				failStep(ctx, store, step, fmt.Sprintf("reschedule: %v", err))
			}
			return true, nil

		case "run":
			// Proceed normally
		default:
			// Proceed normally
		}
	}

	res, err := node.ExecuteWithGateway(ctx, store, &run, graph, now, step.Kind, deps.AIGateway, deps.ExtensionHost)
	if err != nil {
		slog.Error("failed to execute node", "error", err, "node_id", node.ID, "run_id", step.RunID)
		failStep(ctx, store, step, fmt.Sprintf("execute node: %v", err))
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

	err = store.AdvanceRunTx(ctx, run.ID, run, step.ID, res.NextStep, res.Transition, res.MessageIntents)
	if err != nil {
		slog.Error("failed to advance run transaction", "error", err, "run_id", run.ID)
		failStep(ctx, store, step, fmt.Sprintf("advance run tx: %v", err))
		return true, nil
	}

	telemetry.JourneyStepsExecuted.Add(ctx, 1, otelmetric.WithAttributes(
		attribute.String("tenant_id", run.TenantID),
		attribute.String("journey_id", run.JourneyID),
		attribute.String("node_type", node.Type),
	))

	if run.Status == "completed" || run.Status == "exited" {
		telemetry.JourneyExits.Add(ctx, 1, otelmetric.WithAttributes(
			attribute.String("tenant_id", run.TenantID),
			attribute.String("journey_id", run.JourneyID),
		))
	}

	return true, nil
}

func failStep(ctx context.Context, store ports.Store, step domain.JourneyStep, msg string) {
	if step.Attempts >= 10 {
		telemetry.JourneyDeadLettered.Add(ctx, 1, otelmetric.WithAttributes(
			attribute.String("tenant_id", step.TenantID),
			attribute.String("type", "step"),
		))
	}
	_ = store.FailJourneyStep(ctx, step.ID, msg)
}
