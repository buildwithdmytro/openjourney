package main

import (
	"context"
	"flag"
	"log/slog"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/buildwithdmytro/openjourney/internal/ai"
	"github.com/buildwithdmytro/openjourney/internal/blob"
	"github.com/buildwithdmytro/openjourney/internal/config"
	"github.com/buildwithdmytro/openjourney/internal/extension"
	"github.com/buildwithdmytro/openjourney/internal/operations"
	"github.com/buildwithdmytro/openjourney/internal/postgres"
)

func main() {
	var maxItems int
	var maxDuration time.Duration
	var watch bool
	flag.IntVar(&maxItems, "max-items", 1000, "maximum operations to process")
	flag.DurationVar(&maxDuration, "max-duration", time.Hour, "maximum run duration")
	flag.BoolVar(&watch, "watch", false, "poll until duration expires")
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
	store, err := postgres.Open(ctx, cfg.DatabaseURL)
	if err != nil {
		slog.Error("open database", "error", err)
		os.Exit(1)
	}
	defer store.Close()
	blobs, err := blob.NewMinIO(ctx, cfg.S3Endpoint, cfg.S3AccessKey, cfg.S3SecretKey, cfg.S3Bucket, cfg.S3UseTLS)
	if err != nil {
		slog.Error("open object store", "error", err)
		os.Exit(1)
	}
	extHost := extension.NewHost(store)
	extHost.SetBlobStore(blobs)
	processed, err := operations.DrainWithGatewayAndExtensions(ctx, store, blobs, ai.NewGateway(store), extHost, maxItems, watch)
	if err != nil {
		slog.Error("operations worker failed", "processed", processed, "error", err)
		os.Exit(1)
	}
	slog.Info("operations worker complete", "processed", processed)
}
