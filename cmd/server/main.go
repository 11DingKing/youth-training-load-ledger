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

	"github.com/11DingKing/youth-training-load-ledger/internal/activity"
	"github.com/11DingKing/youth-training-load-ledger/internal/auth"
	"github.com/11DingKing/youth-training-load-ledger/internal/clock"
	"github.com/11DingKing/youth-training-load-ledger/internal/config"
	"github.com/11DingKing/youth-training-load-ledger/internal/domain"
	"github.com/11DingKing/youth-training-load-ledger/internal/httpapi"
	"github.com/11DingKing/youth-training-load-ledger/internal/planning"
	"github.com/11DingKing/youth-training-load-ledger/internal/profile"
	"github.com/11DingKing/youth-training-load-ledger/internal/risk"
	"github.com/11DingKing/youth-training-load-ledger/internal/store"
	"github.com/11DingKing/youth-training-load-ledger/internal/worker"
)

func main() {
	if err := run(); err != nil {
		slog.Error("server stopped", "error", err)
		os.Exit(1)
	}
}

func run() error {
	cfg, err := config.Load()
	if err != nil {
		return fmt.Errorf("load config: %w", err)
	}
	logger := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo}))
	slog.SetDefault(logger)
	rootCtx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	database, err := store.Open(rootCtx, cfg.DatabasePath)
	if err != nil {
		return err
	}
	defer database.Close()
	realClock := clock.Real{}
	authService := auth.NewService(database, realClock, cfg.SessionTTL)
	if err = ensureBootstrapAdvisor(rootCtx, database, authService, cfg); err != nil {
		return err
	}
	profileService := profile.NewService(database, realClock)
	planningService := planning.NewService(database, realClock)
	activityService := activity.NewService(database, realClock)
	riskService := risk.NewService(database, realClock)
	runner := worker.New(database, realClock, workerOwner(), cfg.WorkerPoll, cfg.WorkerLease, logger)
	if err = runner.Register("risk_notification", worker.HandlerFunc(func(ctx context.Context, job domain.WorkerJob) error {
		logger.InfoContext(ctx, "risk notification delivered", "job_id", job.ID, "payload", job.Payload)
		return nil
	})); err != nil {
		return err
	}
	workerDone := make(chan error, 1)
	go func() { workerDone <- runner.Run(rootCtx) }()
	api := httpapi.New(httpapi.Dependencies{
		Store: database, Auth: authService, Profiles: profileService, Planning: planningService,
		Activities: activityService, Risks: riskService, Logger: logger, MaxBodyBytes: cfg.MaxBodyBytes,
	})
	httpServer := &http.Server{
		Addr: cfg.HTTPAddr, Handler: api.Handler(), ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout: 15 * time.Second, WriteTimeout: 30 * time.Second, IdleTimeout: 60 * time.Second,
	}
	serverDone := make(chan error, 1)
	go func() {
		logger.Info("server listening", "address", cfg.HTTPAddr)
		serverDone <- httpServer.ListenAndServe()
	}()
	select {
	case err = <-serverDone:
		if !errors.Is(err, http.ErrServerClosed) {
			return fmt.Errorf("serve HTTP: %w", err)
		}
	case err = <-workerDone:
		if !errors.Is(err, context.Canceled) {
			return fmt.Errorf("run worker: %w", err)
		}
	case <-rootCtx.Done():
	}
	shutdownCtx, cancel := context.WithTimeout(context.Background(), cfg.ShutdownTimeout)
	defer cancel()
	if err = httpServer.Shutdown(shutdownCtx); err != nil {
		return fmt.Errorf("shutdown HTTP: %w", err)
	}
	return nil
}

func ensureBootstrapAdvisor(ctx context.Context, database *store.Store, service *auth.Service, cfg config.Config) error {
	if _, err := database.UserByEmail(ctx, cfg.BootstrapAdmin); err == nil {
		return nil
	} else if !errors.Is(err, domain.ErrNotFound) {
		return err
	}
	_, err := service.Register(ctx, cfg.BootstrapAdmin, "Bootstrap Health Advisor", cfg.BootstrapPassword, domain.RoleAdvisor)
	if err != nil {
		return fmt.Errorf("create bootstrap advisor: %w", err)
	}
	return nil
}

func workerOwner() string {
	hostname, err := os.Hostname()
	if err != nil || hostname == "" {
		hostname = "unknown-host"
	}
	return fmt.Sprintf("%s-%d", hostname, os.Getpid())
}
