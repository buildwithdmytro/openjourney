package main

import (
	"context"
	"flag"
	"log/slog"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/buildwithdmytro/openjourney/internal/blob"
	"github.com/buildwithdmytro/openjourney/internal/config"
	"github.com/buildwithdmytro/openjourney/internal/postgres"
	"github.com/buildwithdmytro/openjourney/internal/projector"
)

func main() {
	var maxItems int
	var maxDuration time.Duration
	var afterClaimDelay time.Duration
	var watch bool
	flag.IntVar(&maxItems, "max-items", 1000, "maximum jobs to process before exiting")
	flag.DurationVar(&maxDuration, "max-duration", 30*time.Second, "maximum run duration")
	flag.DurationVar(&afterClaimDelay, "after-claim-delay", 0, "optional delay after claiming a job, used by termination smoke tests")
	flag.BoolVar(&watch, "watch", false, "continue polling until the duration expires")
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
	store.SetBlobStore(blobs)

	processed, err := projector.DrainWithOptions(ctx, store, maxItems, watch, projector.Options{
		AfterClaimDelay: afterClaimDelay,
	})
	if err != nil {
		slog.Error("worker failed", "processed", processed, "error", err)
		os.Exit(1)
	}
	slog.Info("worker complete", "processed", processed)
}
