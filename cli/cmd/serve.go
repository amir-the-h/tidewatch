package cmd

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/amir-the-h/iran-watcher/internal/api"
	"github.com/amir-the-h/iran-watcher/internal/config"
	"github.com/amir-the-h/iran-watcher/internal/inference"
	"github.com/amir-the-h/iran-watcher/internal/model"
	"github.com/amir-the-h/iran-watcher/internal/queue"
	"github.com/amir-the-h/iran-watcher/internal/store"
	"github.com/amir-the-h/iran-watcher/internal/stream"
	"github.com/amir-the-h/iran-watcher/internal/worker"
	"github.com/amir-the-h/iran-watcher/migrations"
	"github.com/spf13/cobra"
)

var configPath string

var serveCmd = &cobra.Command{
	Use:   "serve",
	Short: "Start the iran-watcher server",
	RunE:  runServe,
}

func init() {
	serveCmd.Flags().StringVar(&configPath, "config", "configs/iran-watcher.yaml", "path to YAML config file")
	rootCmd.AddCommand(serveCmd)
}

func runServe(cmd *cobra.Command, _ []string) error {
	cfg, err := config.Load(configPath)
	if err != nil {
		return fmt.Errorf("failed to load config: %w", err)
	}

	level := parseLogLevel(cfg.Log.Level)
	opts := &slog.HandlerOptions{Level: level}
	var handler slog.Handler
	if cfg.Log.Format == "json" {
		handler = slog.NewJSONHandler(os.Stderr, opts)
	} else {
		handler = slog.NewTextHandler(os.Stderr, opts)
	}
	logger := slog.New(handler)
	slog.SetDefault(logger)

	if cfg.X.BearerToken == "" {
		return fmt.Errorf("X bearer token is required; set via config or IRAN_WATCHER_X_BEARER_TOKEN env var")
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	s, err := store.NewSQLiteStore(cfg.SQLite.Path, migrations.FS)
	if err != nil {
		return fmt.Errorf("failed to open sqlite: %w", err)
	}
	defer func() {
		if closeErr := s.Close(); closeErr != nil {
			logger.Error("sqlite close error", "error", closeErr)
		}
	}()

	q := queue.NewRedisQueue(cfg.Redis.Addr, cfg.Redis.Password, cfg.Redis.DB)

	reg := inference.NewRegistry()
	allModels, err := s.ListModels(ctx)
	if err != nil {
		return fmt.Errorf("failed to list models: %w", err)
	}

	activeModels := make([]*model.Model, 0, len(allModels))
	for _, m := range allModels {
		if m.Active {
			activeModels = append(activeModels, m)
		}
	}

	factory := func(m *model.Model) (inference.Pipeline, error) {
		p := inference.NewONNXPipeline(m.Name)
		return p, p.Load(m.Path)
	}

	if err := reg.Load(ctx, activeModels, factory); err != nil {
		return fmt.Errorf("failed to load inference registry: %w", err)
	}

	rules := stream.NewRuleManager(cfg.X.BearerToken, nil)
	streamClient := stream.NewClient(cfg.X.BearerToken, s, q, rules, nil, logger)

	workerCount := cfg.Worker.Count
	if workerCount == 0 {
		workerCount = len(activeModels)
	}
	if workerCount < 1 {
		workerCount = 1
	}

	pool := worker.NewPool(workerCount, q, q, s, reg, nil, logger)
	srv := api.NewServerWithConcreteRules(cfg.API.Bind, s, q, reg, rules, streamClient, cfg.X.Tier, logger)

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)

	errCh := make(chan error, 2)

	logger.Info("serve: stream is disconnected; use API or CLI to connect", "component", "stream")

	go func() {
		logger.Info("serve: starting worker pool", "component", "worker", "count", workerCount)
		if err := pool.Start(ctx); err != nil {
			errCh <- fmt.Errorf("worker: %w", err)
		}
	}()

	go func() {
		logger.Info("serve: starting api server", "component", "api", "bind", cfg.API.Bind)
		if err := srv.Start(ctx); err != nil {
			errCh <- fmt.Errorf("api: %w", err)
		}
	}()

	select {
	case sig := <-sigCh:
		logger.Info("serve: received signal, shutting down", "signal", sig)
	case err := <-errCh:
		logger.Error("serve: component error, shutting down", "error", err)
	}

	cancel()

	streamClient.Disconnect()

	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer shutdownCancel()

	if err := srv.Shutdown(shutdownCtx); err != nil && !errors.Is(err, context.DeadlineExceeded) {
		logger.Error("serve: api server shutdown error", "error", err)
	}

	logger.Info("serve: shutdown complete")
	return nil
}

func parseLogLevel(level string) slog.Level {
	switch level {
	case "debug":
		return slog.LevelDebug
	case "warn":
		return slog.LevelWarn
	case "error":
		return slog.LevelError
	default:
		return slog.LevelInfo
	}
}
