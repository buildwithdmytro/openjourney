package main

import (
	"context"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"github.com/buildwithdmytro/openjourney/internal/analytics"
	"github.com/buildwithdmytro/openjourney/internal/config"
	"github.com/buildwithdmytro/openjourney/internal/stream"
)

func main() {
	cfg, err := config.Load()
	if err != nil {
		slog.Error("invalid configuration", "error", err)
		os.Exit(1)
	}
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	sink, err := analytics.Open(ctx, cfg.ClickHouseAddress, cfg.ClickHouseDatabase,
		cfg.ClickHouseUsername, cfg.ClickHousePassword)
	if err != nil {
		slog.Error("open ClickHouse", "error", err)
		os.Exit(1)
	}
	defer sink.Close()
	consumer, err := stream.NewConsumer(cfg.KafkaBrokers, "openjourney-analytics-v1", "events.accepted.v1")
	if err != nil {
		slog.Error("open Kafka consumer", "error", err)
		os.Exit(1)
	}
	defer consumer.Close()
	if err := sink.Run(ctx, consumer); err != nil {
		slog.Error("analytics stopped", "error", err)
		os.Exit(1)
	}
}
