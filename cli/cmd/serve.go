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
	"github.com/amir-the-h/iran-watcher/internal/auth"
	"github.com/amir-the-h/iran-watcher/internal/config"
	"github.com/amir-the-h/iran-watcher/internal/inference"
	"github.com/amir-the-h/iran-watcher/internal/model"
	"github.com/amir-the-h/iran-watcher/internal/notifier"
	"github.com/amir-the-h/iran-watcher/internal/queue"
	"github.com/amir-the-h/iran-watcher/internal/search"
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

	if err := inference.InitRuntime(cfg.Inference.ONNXLibPath); err != nil {
		return fmt.Errorf("failed to initialize ONNX runtime: %w", err)
	}
	defer func() {
		if err := inference.DestroyRuntime(); err != nil {
			logger.Error("serve: onnx runtime destroy error", "error", err)
		}
	}()

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

	ollamaBackends := ollamaBackendConfigs(cfg.OllamaBackends())

	factory := func(m *model.Model) (inference.Pipeline, error) {
		switch m.Type {
		case model.ModelTypeSummarizer:
			p := inference.NewSeq2SeqPipeline(m.Name)
			return p, p.Load(m.Path)
		case model.ModelTypeExtraction:
			prompt := inference.OllamaGenericSystem
			if m.Name == "geoconfirmed-extractor" {
				prompt = inference.OllamaGeoConfirmedSystem
			}
			p := inference.NewOllamaPipeline(m.Name, ollamaBackends, prompt, cfg.Ollama.FastWaitTimeout)
			return p, p.Load(m.Path)
		case model.ModelTypeDigest:
			p := inference.NewOllamaPipeline(m.Name, ollamaBackends, inference.OllamaDigestSystem, cfg.Ollama.FastWaitTimeout)
			return p, p.Load(m.Path)
		default:
			p := inference.NewONNXPipeline(m.Name)
			return p, p.Load(m.Path)
		}
	}

	if err := reg.Load(ctx, activeModels, factory); err != nil {
		return fmt.Errorf("failed to load inference registry: %w", err)
	}

	// Initialize notifier clients (each optional — skipped if credentials not configured).
	var pushClient *notifier.PushoverClient
	if cfg.Pushover.AppToken != "" && cfg.Pushover.UserKey != "" {
		pushClient = notifier.NewPushoverClient(cfg.Pushover.AppToken, cfg.Pushover.UserKey, logger)
		logger.Info("serve: pushover notifier enabled", "component", "notifier")
	} else {
		logger.Info("serve: pushover notifier disabled (no credentials)", "component", "notifier")
	}

	var telegramClient *notifier.TelegramClient
	if cfg.Telegram.BotToken != "" {
		telegramClient = notifier.NewTelegramClient(cfg.Telegram.BotToken, logger)
		logger.Info("serve: telegram notifier enabled", "component", "notifier")
	} else {
		logger.Info("serve: telegram notifier disabled (no bot token)", "component", "notifier")
	}

	var dispatcher *notifier.Dispatcher
	var emitter notifier.EventEmitter
	if pushClient != nil || telegramClient != nil {
		dispatcher = notifier.NewDispatcher(s, pushClient, telegramClient, logger)
		emitter = dispatcher
	}

	rules := stream.NewRuleManager(cfg.X.BearerToken, nil)
	streamClient := stream.NewClient(cfg.X.BearerToken, s, q, rules, emitter, logger)

	workerCount := cfg.Worker.Count
	if workerCount == 0 {
		workerCount = len(activeModels)
	}
	if workerCount < 1 {
		workerCount = 1
	}

	// Build search scheduler (nil when search is disabled).
	searchParams := search.SearchParams{
		MaxResults:  cfg.Search.MaxResults,
		Limit:       cfg.Search.Limit,
		TweetFields: cfg.Search.TweetFields,
		UserFields:  cfg.Search.UserFields,
		Expansions:  cfg.Search.Expansions,
	}
	if cfg.Search.StartTime != "" {
		if t, err := time.Parse(time.RFC3339, cfg.Search.StartTime); err == nil {
			searchParams.StartTime = t
		}
	}
	searchClient := search.NewClient(cfg.X.BearerToken, cfg.X.Tier, s, q, logger)
	searchScheduler := search.NewScheduler(searchClient, s, cfg.Search.Interval, searchParams, logger)

	mgr := worker.NewManager(workerCount, q, q, s, reg, emitter, logger)

	jwtSecret := cfg.Auth.JWTSecret
	if jwtSecret == "" {
		// Warn loudly — an empty secret makes all tokens trivially forgeable.
		logger.Warn("serve: auth.jwt_secret is not set; using an insecure default — set IRAN_WATCHER_AUTH_JWT_SECRET in production", "component", "auth")
		jwtSecret = "iran-watcher-insecure-default-secret"
	}

	if err := seedAdminUser(ctx, s, cfg.Auth.AdminUsername, cfg.Auth.AdminPassword, logger); err != nil {
		return fmt.Errorf("serve: seed admin: %w", err)
	}

	srv := api.NewServerWithConcreteRules(cfg.API.Bind, s, q, reg, rules, streamClient, cfg.X.Tier, ollamaBackends, cfg.Ollama.FastWaitTimeout, jwtSecret, logger, dispatcher, searchScheduler, mgr, q, q)

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)

	errCh := make(chan error, 2)

	if cfg.Stream.AutoConnect {
		logger.Info("serve: auto-connecting stream", "component", "stream")
		if err := streamClient.Connect(ctx); err != nil {
			logger.Error("serve: stream auto-connect failed", "component", "stream", "error", err)
		}
	} else {
		logger.Info("serve: stream is disconnected; use API or CLI to connect", "component", "stream")
	}

	go func() {
		logger.Info("serve: starting worker manager", "component", "worker", "count", workerCount)
		if err := mgr.Start(ctx); err != nil {
			errCh <- fmt.Errorf("worker: %w", err)
		}
	}()

	if dispatcher != nil {
		go func() {
			logger.Info("serve: starting notification dispatcher", "component", "notifier")
			dispatcher.Start(ctx)
		}()
	}

	if cfg.Search.Enabled {
		go func() {
			logger.Info("serve: starting search scheduler", "component", "search", "interval", cfg.Search.Interval)
			if err := searchScheduler.Start(ctx); err != nil && !errors.Is(err, context.Canceled) {
				logger.Error("serve: search scheduler stopped", "error", err)
			}
		}()
	}

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

// ollamaBackendConfigs converts config.OllamaBackendConfig slice to
// inference.BackendConfig slice for use with NewOllamaPipeline.
func ollamaBackendConfigs(cfgBackends []config.OllamaBackendConfig) []inference.BackendConfig {
	out := make([]inference.BackendConfig, len(cfgBackends))
	for i, b := range cfgBackends {
		out[i] = inference.BackendConfig{URL: b.URL, Name: b.Name}
	}
	return out
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

// seedAdminUser ensures an admin account exists. If no admin is present in the
// database, one is created from the config values. Subsequent starts are no-ops.
func seedAdminUser(ctx context.Context, s store.Store, username, password string, logger *slog.Logger) error {
	count, err := s.CountAdmins(ctx)
	if err != nil {
		return fmt.Errorf("count admins: %w", err)
	}
	if count > 0 {
		return nil // admin already exists
	}

	hash, err := auth.HashPassword(password)
	if err != nil {
		return fmt.Errorf("hash password: %w", err)
	}

	now := time.Now()
	u := &model.User{
		Username:     username,
		PasswordHash: hash,
		Role:         model.RoleAdmin,
		CreatedAt:    now,
		UpdatedAt:    now,
	}
	if err := s.SaveUser(ctx, u); err != nil {
		return fmt.Errorf("save admin user: %w", err)
	}
	logger.Info("serve: created initial admin user", "username", username, "component", "auth")
	return nil
}
