package main

import (
	"context"
	"flag"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/buildwithdmytro/openjourney/internal/ai"
	"github.com/buildwithdmytro/openjourney/internal/channels"
	"github.com/buildwithdmytro/openjourney/internal/config"
	"github.com/buildwithdmytro/openjourney/internal/journey"
	"github.com/buildwithdmytro/openjourney/internal/ports"
	"github.com/buildwithdmytro/openjourney/internal/postgres"
	"github.com/buildwithdmytro/openjourney/internal/stages"
	"github.com/buildwithdmytro/openjourney/internal/telemetry"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

func main() {
	var watch bool
	var maxDuration time.Duration
	flag.BoolVar(&watch, "watch", false, "poll until duration expires")
	flag.DurationVar(&maxDuration, "max-duration", time.Hour, "maximum run duration")
	flag.Parse()

	cfg, err := config.Load()
	if err != nil {
		slog.Error("invalid configuration", "error", err)
		os.Exit(1)
	}

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	ctx, cancel := context.WithTimeout(ctx, maxDuration)
	defer cancel()

	shutdownTelemetry, err := telemetry.Setup(ctx, "openjourney-journeys-worker", cfg.ServiceVersion, cfg.OTLPEndpoint)
	if err != nil {
		slog.Error("configure telemetry", "error", err)
		os.Exit(1)
	}
	defer func() {
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = shutdownTelemetry(shutdownCtx)
	}()

	metricsAddr := os.Getenv("OPENJOURNEY_METRICS_ADDRESS_JOURNEYS")
	if metricsAddr == "" {
		metricsAddr = ":8084"
	}
	mux := http.NewServeMux()
	mux.Handle("GET /metrics", promhttp.Handler())
	metricsServer := &http.Server{Addr: metricsAddr, Handler: mux}
	go func() {
		slog.Info("starting metrics server", "addr", metricsAddr)
		if err := metricsServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			slog.Error("metrics server failed", "error", err)
		}
	}()
	defer func() {
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = metricsServer.Shutdown(shutdownCtx)
	}()

	store, err := postgres.Open(ctx, cfg.DatabaseURL)
	if err != nil {
		slog.Error("open database", "error", err)
		os.Exit(1)
	}
	defer store.Close()

	hostname, err := os.Hostname()
	if err != nil {
		hostname = "unknown"
	}
	workerID := fmt.Sprintf("journeys-worker-%s-%d", hostname, os.Getpid())

	var sesAdapter ports.ChannelAdapter = channels.NewSESAdapter()
	if os.Getenv("OPENJOURNEY_MOCK_SES") == "true" {
		sesAdapter = channels.NewFakeAdapter()
	}

	// Build the adapter registry once for this process.
	reg := channels.DefaultRegistry()
	reg.Register("ses", sesAdapter)

	clk := journey.RealClock{}
	aiGateway := ai.NewGateway(store)
	deliveryCfg := journey.Config{
		TrackingSecretKey: []byte(cfg.TrackingSecretKey),
		TrackingBaseURL:   cfg.TrackingBaseURL,
		Registry:          reg,
		Clock:             clk,
	}

	slog.Info("starting journeys worker", "worker_id", workerID, "watch", watch)

	for {
		// Stage transitions are event-backed; the projector remains the profile writer.
		if _, err := stages.ApplyAll(ctx, store); err != nil {
			slog.Error("stage rules error", "error", err)
		}
		if err := journey.EnrollScheduledDue(ctx, store, clk); err != nil {
			slog.Error("scheduled enrollment error", "error", err)
		}

		ticked, err := journey.TickNext(ctx, store, journey.Deps{Clock: clk, AIGateway: aiGateway})
		if err != nil {
			slog.Error("tick error", "error", err)
		}

		delivered, err := journey.DeliverNext(ctx, store, workerID, deliveryCfg)
		if err != nil {
			slog.Error("delivery error", "error", err)
		}

		processed := ticked || delivered

		if !watch {
			break
		}

		select {
		case <-ctx.Done():
			slog.Info("journeys worker context done, shutting down")
			return
		default:
			if !processed {
				time.Sleep(1 * time.Second)
			}
		}
	}
}
