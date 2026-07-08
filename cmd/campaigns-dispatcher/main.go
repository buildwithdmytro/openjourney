package main

import (
	"context"
	"flag"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/ClickHouse/clickhouse-go/v2"
	"github.com/buildwithdmytro/openjourney/internal/blob"
	"github.com/buildwithdmytro/openjourney/internal/campaigns"
	"github.com/buildwithdmytro/openjourney/internal/config"
	"github.com/buildwithdmytro/openjourney/internal/postgres"
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

	shutdownTelemetry, err := telemetry.Setup(ctx, "openjourney-campaigns-dispatcher", cfg.ServiceVersion, cfg.OTLPEndpoint)
	if err != nil {
		slog.Error("configure telemetry", "error", err)
		os.Exit(1)
	}
	defer func() {
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = shutdownTelemetry(shutdownCtx)
	}()

	metricsAddr := os.Getenv("OPENJOURNEY_METRICS_ADDRESS_DISPATCHER")
	if metricsAddr == "" {
		metricsAddr = ":8083"
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

	if cfg.ClickHouseAddress != "" {
		chConn, err := clickhouse.Open(&clickhouse.Options{
			Addr:        []string{cfg.ClickHouseAddress},
			Auth:        clickhouse.Auth{Database: cfg.ClickHouseDatabase, Username: cfg.ClickHouseUsername, Password: cfg.ClickHousePassword},
			DialTimeout: 10 * time.Second,
		})
		if err != nil {
			slog.Error("open ClickHouse", "error", err)
			os.Exit(1)
		}
		if err := chConn.Ping(ctx); err != nil {
			slog.Error("ping ClickHouse", "error", err)
			os.Exit(1)
		}
		store.SetClickHouse(chConn)
		defer chConn.Close()
	}

	blobs, err := blob.NewMinIO(ctx, cfg.S3Endpoint, cfg.S3AccessKey, cfg.S3SecretKey, cfg.S3Bucket, cfg.S3UseTLS)
	if err != nil {
		slog.Error("open object store", "error", err)
		os.Exit(1)
	}

	slog.Info("starting campaigns dispatcher", "watch", watch)

	for {
		dispatched, err := campaigns.DispatchNext(ctx, store, blobs)
		if err != nil {
			slog.Error("dispatch error", "error", err)
		}

		if !watch {
			break
		}

		select {
		case <-ctx.Done():
			slog.Info("dispatcher context done, shutting down")
			return
		default:
			if !dispatched {
				time.Sleep(1 * time.Second)
			}
		}
	}
}
