package telemetry

import (
	"context"
	"errors"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracehttp"
	otelprom "go.opentelemetry.io/otel/exporters/prometheus"
	otelmetric "go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	semconv "go.opentelemetry.io/otel/semconv/v1.30.0"
)

var (
	Meter = otel.Meter("openjourney")

	MessagesSent = mustCounter(Meter.Int64Counter("openjourney_delivery_messages_sent_total",
		otelmetric.WithDescription("Total number of messages successfully sent")))

	Bounces = mustCounter(Meter.Int64Counter("openjourney_delivery_bounces_total",
		otelmetric.WithDescription("Total number of message bounces")))

	Complaints = mustCounter(Meter.Int64Counter("openjourney_delivery_complaints_total",
		otelmetric.WithDescription("Total number of complaints")))

	PolicyRejections = mustCounter(Meter.Int64Counter("openjourney_delivery_policy_rejections_total",
		otelmetric.WithDescription("Total number of policy rejections by decision")))

	JourneyEnrollments = mustCounter(Meter.Int64Counter("openjourney_journey_enrollments_total",
		otelmetric.WithDescription("Total number of journey enrollments")))

	JourneyStepsExecuted = mustCounter(Meter.Int64Counter("openjourney_journey_steps_executed_total",
		otelmetric.WithDescription("Total number of journey steps executed")))

	JourneyMessagesSent = mustCounter(Meter.Int64Counter("openjourney_journey_messages_sent_total",
		otelmetric.WithDescription("Total number of journey messages successfully sent")))

	JourneyPolicyRejections = mustCounter(Meter.Int64Counter("openjourney_journey_policy_rejections_total",
		otelmetric.WithDescription("Total number of journey policy rejections")))

	JourneyExits = mustCounter(Meter.Int64Counter("openjourney_journey_exits_total",
		otelmetric.WithDescription("Total number of journey exits")))

	JourneyDeadLettered = mustCounter(Meter.Int64Counter("openjourney_journey_dead_lettered_total",
		otelmetric.WithDescription("Total number of journey dead-lettered steps or intents")))
)

func mustCounter(c otelmetric.Int64Counter, err error) otelmetric.Int64Counter {
	if err != nil {
		panic(err)
	}
	return c
}


type Shutdown func(context.Context) error

func Setup(ctx context.Context, service, version, endpoint string) (Shutdown, error) {
	res, err := resource.Merge(resource.Default(), resource.NewSchemaless(
		semconv.ServiceName(service), semconv.ServiceVersion(version),
	))
	if err != nil {
		return nil, err
	}
	metricExporter, err := otelprom.New()
	if err != nil {
		return nil, err
	}
	meterProvider := metric.NewMeterProvider(metric.WithResource(res), metric.WithReader(metricExporter))
	otel.SetMeterProvider(meterProvider)
	otel.SetTextMapPropagator(propagation.NewCompositeTextMapPropagator(
		propagation.TraceContext{}, propagation.Baggage{},
	))

	var tracerProvider *sdktrace.TracerProvider
	if endpoint != "" {
		exporter, err := otlptracehttp.New(ctx,
			otlptracehttp.WithEndpoint(endpoint), otlptracehttp.WithInsecure())
		if err != nil {
			return nil, err
		}
		tracerProvider = sdktrace.NewTracerProvider(
			sdktrace.WithResource(res), sdktrace.WithBatcher(exporter),
		)
	} else {
		tracerProvider = sdktrace.NewTracerProvider(sdktrace.WithResource(res))
	}
	otel.SetTracerProvider(tracerProvider)
	return func(ctx context.Context) error {
		return errors.Join(tracerProvider.Shutdown(ctx), meterProvider.Shutdown(ctx))
	}, nil
}
