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

	"github.com/buildwithdmytro/openjourney/internal/campaigns"
	"github.com/buildwithdmytro/openjourney/internal/channels"
	"github.com/buildwithdmytro/openjourney/internal/config"
	"github.com/buildwithdmytro/openjourney/internal/ports"
	"github.com/buildwithdmytro/openjourney/internal/postgres"
	"github.com/buildwithdmytro/openjourney/internal/extension"
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

	shutdownTelemetry, err := telemetry.Setup(ctx, "openjourney-campaigns-delivery", cfg.ServiceVersion, cfg.OTLPEndpoint)
	if err != nil {
		slog.Error("configure telemetry", "error", err)
		os.Exit(1)
	}
	defer func() {
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = shutdownTelemetry(shutdownCtx)
	}()

	metricsAddr := os.Getenv("OPENJOURNEY_METRICS_ADDRESS")
	if metricsAddr == "" {
		metricsAddr = ":8082"
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
	workerID := fmt.Sprintf("delivery-worker-%s-%d", hostname, os.Getpid())

	var sesAdapter ports.ChannelAdapter = channels.NewSESAdapter()
	if os.Getenv("OPENJOURNEY_MOCK_SES") == "true" {
		sesAdapter = channels.NewFakeAdapter()
	}

	// Build the adapter registry once for this process.
	reg := channels.DefaultRegistry()
	reg.Register("ses", sesAdapter)

	extHost := extension.NewHost(store)
	if err := extension.RegisterChannelProviders(ctx, store, extHost, reg); err != nil {
		slog.Error("failed to register extension channel providers", "error", err)
	}

	deliveryCfg := campaigns.Config{
		TrackingSecretKey: []byte(cfg.TrackingSecretKey),
		TrackingBaseURL:   cfg.TrackingBaseURL,
		Registry:          reg,
	}

	slog.Info("starting campaigns delivery worker", "worker_id", workerID, "watch", watch)

	for {
		processed, err := campaigns.DeliverNext(ctx, store, workerID, deliveryCfg)
		if err != nil {
			slog.Error("delivery error", "error", err)
		}

		if !watch {
			break
		}

		select {
		case <-ctx.Done():
			slog.Info("delivery worker context done, shutting down")
			return
		default:
			if !processed {
				time.Sleep(1 * time.Second)
			}
		}
	}
}
