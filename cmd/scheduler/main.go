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
	"github.com/buildwithdmytro/openjourney/internal/postgres"
	"github.com/buildwithdmytro/openjourney/internal/scheduler"
)

func main() {
	var maxItems int
	var maxDuration time.Duration
	var watch bool
	flag.IntVar(&maxItems, "max-items", 1000, "maximum pipelines to enqueue")
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
	processed, err := scheduler.Drain(ctx, store, maxItems, watch)
	if err != nil {
		slog.Error("scheduler failed", "processed", processed, "error", err)
		os.Exit(1)
	}
	slog.Info("scheduler complete", "processed", processed)
}
