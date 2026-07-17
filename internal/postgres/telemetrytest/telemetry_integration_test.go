package telemetrytest

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/buildwithdmytro/openjourney/internal/domain"
	"github.com/buildwithdmytro/openjourney/internal/postgres"
	"github.com/buildwithdmytro/openjourney/internal/projector"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"
)

// telemetry_integration_test.go — Milestone 10.7.5
//
// DB-gated integration test asserting that database operations
// successfully record the corresponding OpenTelemetry metrics:
//  1. Bounces/Complaints (including channel attributes)
//  2. openjourney_push_tokens_retired_total
//  3. openjourney_sms_opt_outs_total

func TestTelemetryMetricsRecording(t *testing.T) {
	databaseURL := os.Getenv("OPENJOURNEY_TEST_DATABASE_URL")
	if databaseURL == "" {
		t.Skip("OPENJOURNEY_TEST_DATABASE_URL is not configured")
	}
	ctx := context.Background()
	store, err := postgres.Open(ctx, databaseURL)
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer store.Close()

	key := fmt.Sprintf("telemetry-test-%d", time.Now().UnixNano())
	if err := store.EnsureDevelopmentTenant(ctx, key); err != nil {
		t.Fatalf("ensure tenant: %v", err)
	}
	p, err := store.Authenticate(ctx, key)
	if err != nil {
		t.Fatalf("authenticate: %v", err)
	}
	appID, err := store.GetFirstAppID(ctx, p.TenantID, p.WorkspaceID)
	if err != nil {
		t.Fatalf("get first app ID: %v", err)
	}
	p.AppID = appID

	// Set up manual OTEL metric reader
	reader := metric.NewManualReader()
	provider := metric.NewMeterProvider(metric.WithReader(reader))
	previous := otel.GetMeterProvider()
	otel.SetMeterProvider(provider)
	t.Cleanup(func() {
		otel.SetMeterProvider(previous)
		_ = provider.Shutdown(ctx)
	})

	// -----------------------------------------------------------------------
	// 1. Assert Bounces & Complaints metrics (from AcceptEvents)
	// -----------------------------------------------------------------------
	t.Run("Bounces and Complaints", func(t *testing.T) {
		events := []domain.Event{
			{
				Type:           "message.bounced",
				SchemaVersion:  1,
				ExternalID:     "telemetry-rec-1",
				IdempotencyKey: key + "-bounce-1",
				OccurredAt:     time.Now().UTC(),
				Payload:        json.RawMessage(`{"channel":"push","endpoint":"push-token-1","bounce_type":"permanent"}`),
			},
			{
				Type:           "message.complained",
				SchemaVersion:  1,
				ExternalID:     "telemetry-rec-2",
				IdempotencyKey: key + "-complaint-1",
				OccurredAt:     time.Now().UTC(),
				Payload:        json.RawMessage(`{"channel":"sms","endpoint":"+15005551000"}`),
			},
		}

		_, err := store.AcceptEvents(ctx, p, events)
		if err != nil {
			t.Fatalf("AcceptEvents bounce/complaint: %v", err)
		}
		_, err = projector.Drain(ctx, store, len(events), false)
		if err != nil {
			t.Fatalf("projector drain bounce/complaint: %v", err)
		}

		var collected metricdata.ResourceMetrics
		if err := reader.Collect(ctx, &collected); err != nil {
			t.Fatalf("OTEL collect: %v", err)
		}

		foundBounce := false
		foundComplaint := false
		for _, scope := range collected.ScopeMetrics {
			for _, measurement := range scope.Metrics {
				switch measurement.Name {
				case "openjourney_delivery_bounces_total":
					sum, ok := measurement.Data.(metricdata.Sum[int64])
					if ok && len(sum.DataPoints) > 0 {
						for _, dp := range sum.DataPoints {
							chanVal, _ := dp.Attributes.Value(attribute.Key("channel"))
							if chanVal.AsString() == "push" {
								foundBounce = true
							}
						}
					}
				case "openjourney_delivery_complaints_total":
					sum, ok := measurement.Data.(metricdata.Sum[int64])
					if ok && len(sum.DataPoints) > 0 {
						for _, dp := range sum.DataPoints {
							chanVal, _ := dp.Attributes.Value(attribute.Key("channel"))
							if chanVal.AsString() == "sms" {
								foundComplaint = true
							}
						}
					}
				}
			}
		}

		if !foundBounce {
			t.Error("expected openjourney_delivery_bounces_total with channel=push to be recorded")
		}
		if !foundComplaint {
			t.Error("expected openjourney_delivery_complaints_total with channel=sms to be recorded")
		}
	})

	// -----------------------------------------------------------------------
	// 2. Assert openjourney_push_tokens_retired_total
	// -----------------------------------------------------------------------
	t.Run("Push tokens retired", func(t *testing.T) {
		var profileID string
		err := store.Pool().QueryRow(ctx,
			`INSERT INTO profiles (tenant_id, workspace_id, app_id, external_id)
			 VALUES ($1, $2, $3, 'telemetry-push-rec') RETURNING id`,
			p.TenantID, p.WorkspaceID, p.AppID,
		).Scan(&profileID)
		if err != nil {
			t.Fatalf("create test profile: %v", err)
		}

		pushToken := "telemetry-push-token-val"
		_, err = store.RegisterDeviceToken(ctx, p.TenantID, p.WorkspaceID, p.AppID, profileID, "android", "fcm", pushToken)
		if err != nil {
			t.Fatalf("RegisterDeviceToken: %v", err)
		}

		// Retire token
		err = store.RetireDeviceToken(ctx, p.TenantID, p.AppID, pushToken)
		if err != nil {
			t.Fatalf("RetireDeviceToken: %v", err)
		}

		var collected metricdata.ResourceMetrics
		if err := reader.Collect(ctx, &collected); err != nil {
			t.Fatalf("OTEL collect: %v", err)
		}

		foundRetired := false
		for _, scope := range collected.ScopeMetrics {
			for _, measurement := range scope.Metrics {
				if measurement.Name == "openjourney_push_tokens_retired_total" {
					sum, ok := measurement.Data.(metricdata.Sum[int64])
					if ok && len(sum.DataPoints) > 0 {
						foundRetired = true
					}
				}
			}
		}

		if !foundRetired {
			t.Error("expected openjourney_push_tokens_retired_total to be recorded")
		}
	})

	// -----------------------------------------------------------------------
	// 3. Assert openjourney_sms_opt_outs_total
	// -----------------------------------------------------------------------
	t.Run("SMS opt-outs", func(t *testing.T) {
		var profileID string
		err := store.Pool().QueryRow(ctx,
			`INSERT INTO profiles (tenant_id, workspace_id, app_id, external_id, attributes)
			 VALUES ($1, $2, $3, 'telemetry-sms-rec', '{"phone":"+15005550202"}') RETURNING id`,
			p.TenantID, p.WorkspaceID, p.AppID,
		).Scan(&profileID)
		if err != nil {
			t.Fatalf("create test profile: %v", err)
		}

		// Accept a consent.changed unsubscribed event
		events := []domain.Event{
			{
				Type:           "consent.changed",
				SchemaVersion:  1,
				ExternalID:     "telemetry-sms-rec",
				IdempotencyKey: key + "-telemetry-opt-out",
				OccurredAt:     time.Now().UTC(),
				Payload:        json.RawMessage(`{"channel": "sms", "topic": "marketing", "state": "unsubscribed", "evidence": {}}`),
			},
		}
		_, err = store.AcceptEvents(ctx, p, events)
		if err != nil {
			t.Fatalf("AcceptEvents consent: %v", err)
		}
		_, err = projector.Drain(ctx, store, len(events), false)
		if err != nil {
			t.Fatalf("projector drain consent: %v", err)
		}

		var collected metricdata.ResourceMetrics
		if err := reader.Collect(ctx, &collected); err != nil {
			t.Fatalf("OTEL collect: %v", err)
		}

		foundOptOut := false
		for _, scope := range collected.ScopeMetrics {
			for _, measurement := range scope.Metrics {
				if measurement.Name == "openjourney_sms_opt_outs_total" {
					sum, ok := measurement.Data.(metricdata.Sum[int64])
					if ok && len(sum.DataPoints) > 0 {
						foundOptOut = true
					}
				}
			}
		}

		if !foundOptOut {
			t.Error("expected openjourney_sms_opt_outs_total to be recorded")
		}
	})
}
