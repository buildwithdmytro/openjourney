package main

import (
	"context"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"github.com/buildwithdmytro/openjourney/internal/archive"
	"github.com/buildwithdmytro/openjourney/internal/blob"
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
	blobs, err := blob.NewMinIO(ctx, cfg.S3Endpoint, cfg.S3AccessKey, cfg.S3SecretKey, cfg.S3Bucket, cfg.S3UseTLS)
	if err != nil {
		slog.Error("open object store", "error", err)
		os.Exit(1)
	}
	consumer, err := stream.NewConsumer(cfg.KafkaBrokers, "openjourney-archive-v1", "events.accepted.v1")
	if err != nil {
		slog.Error("open Kafka consumer", "error", err)
		os.Exit(1)
	}
	defer consumer.Close()
	if err := archive.Run(ctx, consumer, blobs); err != nil {
		slog.Error("archive stopped", "error", err)
		os.Exit(1)
	}
}
