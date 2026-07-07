package main

import (
	"context"
	"flag"
	"log/slog"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/buildwithdmytro/openjourney/internal/config"
	"github.com/buildwithdmytro/openjourney/internal/dispatcher"
	"github.com/buildwithdmytro/openjourney/internal/postgres"
	"github.com/buildwithdmytro/openjourney/internal/stream"
)

func main() {
	var maxItems int
	var maxDuration time.Duration
	var watch bool
	flag.IntVar(&maxItems, "max-items", 10000, "maximum events to publish")
	flag.DurationVar(&maxDuration, "max-duration", time.Hour, "maximum run duration")
	flag.BoolVar(&watch, "watch", false, "poll until duration expires")
	flag.Parse()

	cfg, err := config.Load()
	if err != nil {
		slog.Error("invalid configuration", "error", err)
		os.Exit(1)
	}
	if cfg.KafkaBrokers == "" {
		slog.Error("OPENJOURNEY_KAFKA_BROKERS is required")
		os.Exit(1)
	}
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	ctx, cancel := context.WithTimeout(ctx, maxDuration)
	defer cancel()
	store, err := postgres.Open(ctx, cfg.DatabaseURL)
	if err != nil {
		slog.Error("open database", "error", err)
		os.Exit(1)
	}
	defer store.Close()
	publisher, err := stream.NewPublisher(cfg.KafkaBrokers)
	if err != nil {
		slog.Error("open Kafka publisher", "error", err)
		os.Exit(1)
	}
	defer publisher.Close()
	processed, err := dispatcher.Drain(ctx, store, publisher, maxItems, watch)
	if err != nil {
		slog.Error("dispatcher failed", "processed", processed, "error", err)
		os.Exit(1)
	}
	slog.Info("dispatcher complete", "processed", processed)
}
