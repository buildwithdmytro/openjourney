package main

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/buildwithdmytro/openjourney/internal/auth"
	"github.com/buildwithdmytro/openjourney/internal/config"
	"github.com/buildwithdmytro/openjourney/internal/httpapi"
	"github.com/buildwithdmytro/openjourney/internal/ports"
	"github.com/buildwithdmytro/openjourney/internal/postgres"
	"github.com/buildwithdmytro/openjourney/internal/telemetry"
)

func main() {
	cfg, err := config.Load()
	if err != nil {
		slog.Error("invalid configuration", "error", err)
		os.Exit(1)
	}
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	shutdownTelemetry, err := telemetry.Setup(ctx, "openjourney-api", cfg.ServiceVersion, cfg.OTLPEndpoint)
	if err != nil {
		slog.Error("configure telemetry", "error", err)
		os.Exit(1)
	}
	defer func() {
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = shutdownTelemetry(shutdownCtx)
	}()

	store, err := postgres.Open(ctx, cfg.DatabaseURL)
	if err != nil {
		slog.Error("open database", "error", err)
		os.Exit(1)
	}
	defer store.Close()
	if cfg.AutoMigrate {
		if err := store.Migrate(ctx); err != nil {
			slog.Error("migrate database", "error", err)
			os.Exit(1)
		}
	}
	if err := store.EnsureDevelopmentTenant(ctx, cfg.DevelopmentAPIKey); err != nil {
		slog.Error("bootstrap development tenant", "error", err)
		os.Exit(1)
	}
	if cfg.AdminEmail != "" || cfg.AdminPassword != "" {
		if err := store.EnsureLocalAdmin(ctx, cfg.AdminEmail, cfg.AdminPassword); err != nil {
			slog.Error("bootstrap local admin", "error", err)
			os.Exit(1)
		}
	}

	var tokenVerifier ports.TokenVerifier
	if cfg.OIDCIssuer != "" {
		tokenVerifier, err = auth.NewOIDCVerifier(ctx, cfg.OIDCIssuer, cfg.OIDCClientID)
		if err != nil {
			slog.Error("configure OIDC", "error", err)
			os.Exit(1)
		}
	}
	sessionTTL, err := time.ParseDuration(cfg.SessionTTL)
	if err != nil {
		slog.Error("invalid session TTL", "error", err)
		os.Exit(1)
	}

	server := &http.Server{
		Addr:              cfg.HTTPAddress,
		Handler: httpapi.NewWithSessionTTL(store, cfg.MaxBatchSize, tokenVerifier, cfg.CORSAllowedOrigin, sessionTTL,
				func(s *httpapi.Server) {
					s.SetTracking([]byte(cfg.TrackingSecretKey), cfg.TrackingBaseURL)
				}),
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       15 * time.Second,
		WriteTimeout:      30 * time.Second,
		IdleTimeout:       60 * time.Second,
	}
	go func() {
		slog.Info("api listening", "address", cfg.HTTPAddress)
		if err := server.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			slog.Error("api stopped", "error", err)
			stop()
		}
	}()
	<-ctx.Done()
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := server.Shutdown(shutdownCtx); err != nil {
		slog.Error("api shutdown", "error", err)
	}
}
