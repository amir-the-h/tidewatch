package main

import (
	"context"
	"errors"
	"flag"
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
)

func main() {
	// 1. Parse flags.
	configPath := flag.String("config", "configs/iran-watcher.yaml", "path to YAML config file")
	flag.Parse()

	// 2. Load config.
	cfg, err := config.Load(*configPath)
	if err != nil {
		slog.Error("main: failed to load config", "error", err)
		os.Exit(1)
	}

	// 3. Init logger (before validation so all startup errors use the configured format/level).
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

	// 4. Validate bearer token.
	if err := validateConfig(cfg); err != nil {
		logger.Error("main: config validation failed", "error", err)
		os.Exit(1)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// 5. Connect SQLite (logger is ready from step 3).
	s, err := store.NewSQLiteStore(cfg.SQLite.Path, migrations.FS)
	if err != nil {
		logger.Error("main: failed to open sqlite", "error", err)
		os.Exit(1)
	}
	defer func() {
		if closeErr := s.Close(); closeErr != nil {
			logger.Error("main: sqlite close error", "error", closeErr)
		}
	}()

	// 6. Connect Redis.
	q := queue.NewRedisQueue(cfg.Redis.Addr, cfg.Redis.Password, cfg.Redis.DB)

	// 7. Load active ONNX models into registry.
	reg := inference.NewRegistry()
	allModels, err := s.ListModels(ctx)
	if err != nil {
		logger.Error("main: failed to list models", "error", err)
		os.Exit(1)
	}

	// Filter only active models.
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
		logger.Error("main: failed to load inference registry", "error", err)
		os.Exit(1)
	}

	// 8. Create stream.RuleManager and stream.Client.
	rules := stream.NewRuleManager(cfg.X.BearerToken, nil)
	streamClient := stream.NewClient(cfg.X.BearerToken, s, q, rules, logger)

	// 9. Determine worker count.
	workerCount := cfg.Worker.Count
	if workerCount == 0 {
		workerCount = len(activeModels)
	}
	if workerCount < 1 {
		workerCount = 1
	}

	// 10. Create worker pool.
	pool := worker.NewPool(workerCount, q, q, s, reg, logger)

	// 11. Create API server.
	srv := api.NewServerWithConcreteRules(cfg.API.Bind, s, q, reg, rules, logger)

	// 12. Setup signal handling.
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)

	errCh := make(chan error, 3)

	// 13. Start components in goroutines.
	go func() {
		logger.Info("main: starting stream client", "component", "stream")
		if err := streamClient.Start(ctx); err != nil {
			errCh <- fmt.Errorf("stream: %w", err)
		}
	}()

	go func() {
		logger.Info("main: starting worker pool", "component", "worker", "count", workerCount)
		if err := pool.Start(ctx); err != nil {
			errCh <- fmt.Errorf("worker: %w", err)
		}
	}()

	go func() {
		logger.Info("main: starting api server", "component", "api", "bind", cfg.API.Bind)
		if err := srv.Start(ctx); err != nil {
			errCh <- fmt.Errorf("api: %w", err)
		}
	}()

	// 14. Wait for signal or component error.
	select {
	case sig := <-sigCh:
		logger.Info("main: received signal, shutting down", "signal", sig)
	case err := <-errCh:
		logger.Error("main: component error, shutting down", "error", err)
	}

	// 15. Graceful shutdown.
	cancel()

	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer shutdownCancel()

	if err := srv.Shutdown(shutdownCtx); err != nil && !errors.Is(err, context.DeadlineExceeded) {
		logger.Error("main: api server shutdown error", "error", err)
	}

	logger.Info("main: shutdown complete")
}

// validateConfig returns an error if cfg is missing required fields.
func validateConfig(cfg *config.Config) error {
	if cfg.X.BearerToken == "" {
		return fmt.Errorf("main: X bearer token is required; set via config or IRAN_WATCHER_X_BEARER_TOKEN env var")
	}
	return nil
}

// parseLogLevel maps a string to the corresponding slog.Level.
// Unknown values default to slog.LevelInfo.
func parseLogLevel(level string) slog.Level {
	switch level {
	case "debug":
		return slog.LevelDebug
	case "info":
		return slog.LevelInfo
	case "warn":
		return slog.LevelWarn
	case "error":
		return slog.LevelError
	default:
		return slog.LevelInfo
	}
}
