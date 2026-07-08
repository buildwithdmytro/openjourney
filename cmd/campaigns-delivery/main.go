package main

import (
	"context"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/buildwithdmytro/openjourney/internal/campaigns"
	"github.com/buildwithdmytro/openjourney/internal/channels"
	"github.com/buildwithdmytro/openjourney/internal/config"
	"github.com/buildwithdmytro/openjourney/internal/ports"
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

	hostname, err := os.Hostname()
	if err != nil {
		hostname = "unknown"
	}
	workerID := fmt.Sprintf("delivery-worker-%s-%d", hostname, os.Getpid())

	var sesAdapter ports.ChannelAdapter = channels.NewSESAdapter()
	if os.Getenv("OPENJOURNEY_MOCK_SES") == "true" {
		sesAdapter = channels.NewFakeAdapter()
	}
	webhookAdapter := channels.NewWebhookAdapter()
	fakeAdapter := channels.NewFakeAdapter()

	deliveryCfg := campaigns.Config{
		TrackingSecretKey: []byte(cfg.TrackingSecretKey),
		TrackingBaseURL:   cfg.TrackingBaseURL,
		SESAdapter:        sesAdapter,
		WebhookAdapter:    webhookAdapter,
		FakeAdapter:       fakeAdapter,
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
