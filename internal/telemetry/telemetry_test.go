package telemetry

import (
	"context"
	"testing"

	"go.opentelemetry.io/otel/attribute"
	otelmetric "go.opentelemetry.io/otel/metric"
)

func TestJourneyTelemetryCounters(t *testing.T) {
	ctx := context.Background()

	// Ensure all counters are initialized and do not panic when Add is called.
	if JourneyEnrollments == nil {
		t.Error("JourneyEnrollments is nil")
	} else {
		JourneyEnrollments.Add(ctx, 1, otelmetric.WithAttributes(
			attribute.String("tenant_id", "t1"),
			attribute.String("journey_id", "j1"),
		))
	}

	if JourneyStepsExecuted == nil {
		t.Error("JourneyStepsExecuted is nil")
	} else {
		JourneyStepsExecuted.Add(ctx, 1, otelmetric.WithAttributes(
			attribute.String("tenant_id", "t1"),
			attribute.String("journey_id", "j1"),
			attribute.String("node_type", "delay"),
		))
	}

	if JourneyMessagesSent == nil {
		t.Error("JourneyMessagesSent is nil")
	} else {
		JourneyMessagesSent.Add(ctx, 1, otelmetric.WithAttributes(
			attribute.String("tenant_id", "t1"),
			attribute.String("journey_id", "j1"),
			attribute.String("channel", "email"),
		))
	}

	if JourneyPolicyRejections == nil {
		t.Error("JourneyPolicyRejections is nil")
	} else {
		JourneyPolicyRejections.Add(ctx, 1, otelmetric.WithAttributes(
			attribute.String("tenant_id", "t1"),
			attribute.String("journey_id", "j1"),
			attribute.String("decision", "suppressed"),
			attribute.String("channel", "email"),
		))
	}

	if JourneyExits == nil {
		t.Error("JourneyExits is nil")
	} else {
		JourneyExits.Add(ctx, 1, otelmetric.WithAttributes(
			attribute.String("tenant_id", "t1"),
			attribute.String("journey_id", "j1"),
		))
	}

	if JourneyDeadLettered == nil {
		t.Error("JourneyDeadLettered is nil")
	} else {
		JourneyDeadLettered.Add(ctx, 1, otelmetric.WithAttributes(
			attribute.String("tenant_id", "t1"),
			attribute.String("type", "step"),
		))
	}
}
