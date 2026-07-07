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
	"github.com/buildwithdmytro/openjourney/internal/campaigns"
	"github.com/buildwithdmytro/openjourney/internal/config"
	"github.com/buildwithdmytro/openjourney/internal/postgres"
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
