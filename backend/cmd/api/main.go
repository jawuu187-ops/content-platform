// Command api serves the public HTTP API.
//
// It is deliberately thin: configuration, logging, persistence wiring and the
// HTTP server live here, while every behaviour lives in a package that can be
// tested without a process.
package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/jawuu187-ops/content-platform/backend/internal/httpapi"
	"github.com/jawuu187-ops/content-platform/backend/internal/identity"
	"github.com/jawuu187-ops/content-platform/backend/internal/platform/config"
	"github.com/jawuu187-ops/content-platform/backend/internal/platform/database"
	"github.com/jawuu187-ops/content-platform/backend/internal/platform/logging"
)

// Set with -ldflags at build time by the release workflow.
var (
	version = "dev"
	commit  = "none"
	builtAt = "unknown"
)

func main() {
	if err := run(); err != nil {
		slog.Error("api: fatal", slog.Any("error", err))
		os.Exit(1)
	}
}

func run() error {
	cfg, err := config.Load("api")
	if err != nil {
		return fmt.Errorf("load config: %w", err)
	}

	logger := logging.New(logging.Options{
		Service:   cfg.ServiceName,
		Env:       string(cfg.Env),
		Level:     cfg.LogLevel,
		Format:    cfg.LogFormat,
		Version:   version,
		CommitSHA: commit,
	})
	slog.SetDefault(logger)

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	var db *database.DB
	if cfg.DatabaseURL != "" {
		db, err = database.Open(ctx, database.Options{
			URL:              cfg.DatabaseURL,
			MaxConns:         cfg.DatabaseMaxConns,
			MinConns:         cfg.DatabaseMinConns,
			MaxConnLifetime:  cfg.DatabaseMaxConnTTL,
			StatementTimeout: cfg.DatabaseStatementTimeout,
			ApplicationName:  cfg.ServiceName,
		})
		if err != nil {
			return fmt.Errorf("open database: %w", err)
		}
		defer db.Close()
		logger.Info("database connected")
	} else {
		logger.Warn("DATABASE_URL is not set: starting without persistence (readiness will report not_configured)")
	}

	var identityService *identity.Service
	if db != nil {
		identityService = identity.New(db.Pool())
	}

	handler := httpapi.New(httpapi.Deps{
		Config:   cfg,
		Logger:   logger,
		Database: db,
		Identity: identityService,
		Build:    httpapi.BuildInfo{Version: version, Commit: commit, BuiltAt: builtAt},
	})

	srv := &http.Server{
		Addr:              cfg.HTTPAddress,
		Handler:           handler,
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       cfg.ReadTimeout,
		WriteTimeout:      cfg.WriteTimeout,
		IdleTimeout:       cfg.IdleTimeout,
		ErrorLog:          slog.NewLogLogger(logger.Handler(), slog.LevelWarn),
	}

	serveErr := make(chan error, 1)
	go func() {
		logger.Info("http server listening", slog.String("address", cfg.HTTPAddress))
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			serveErr <- err
			return
		}
		serveErr <- nil
	}()

	select {
	case err := <-serveErr:
		if err != nil {
			return fmt.Errorf("serve: %w", err)
		}
		return nil
	case <-ctx.Done():
		logger.Info("shutdown signal received", slog.Duration("timeout", cfg.ShutdownTimeout))
	}

	shutdownCtx, cancel := context.WithTimeout(context.Background(), cfg.ShutdownTimeout)
	defer cancel()
	if err := srv.Shutdown(shutdownCtx); err != nil {
		return fmt.Errorf("graceful shutdown: %w", err)
	}
	logger.Info("shutdown complete")
	return nil
}
