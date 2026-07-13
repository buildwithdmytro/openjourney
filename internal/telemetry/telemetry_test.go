package telemetry

import (
	"context"
	"testing"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	otelmetric "go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"
)

func TestExperimentAssignmentCounterIncludesVariant(t *testing.T) {
	ctx := context.Background()
	reader := metric.NewManualReader()
	provider := metric.NewMeterProvider(metric.WithReader(reader))
	previous := otel.GetMeterProvider()
	otel.SetMeterProvider(provider)
	t.Cleanup(func() {
		otel.SetMeterProvider(previous)
		_ = provider.Shutdown(ctx)
	})

	RecordExperimentAssignment(ctx, "treatment")

	var collected metricdata.ResourceMetrics
	if err := reader.Collect(ctx, &collected); err != nil {
		t.Fatal(err)
	}
	for _, scope := range collected.ScopeMetrics {
		for _, measurement := range scope.Metrics {
			if measurement.Name != "openjourney_experiment_assignments_total" {
				continue
			}
			sum, ok := measurement.Data.(metricdata.Sum[int64])
			if !ok || len(sum.DataPoints) != 1 || sum.DataPoints[0].Value != 1 {
				t.Fatalf("assignment counter data = %#v", measurement.Data)
			}
			value, ok := sum.DataPoints[0].Attributes.Value(attribute.Key("variant"))
			if !ok || value.AsString() != "treatment" {
				t.Fatalf("variant label = %v, present=%v", value, ok)
			}
			return
		}
	}
	t.Fatal("assignment counter was not collected")
}

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
