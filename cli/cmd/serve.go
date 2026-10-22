package cmd

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/amir-the-h/tidewatch/internal/api"
	"github.com/amir-the-h/tidewatch/internal/auth"
	"github.com/amir-the-h/tidewatch/internal/config"
	"github.com/amir-the-h/tidewatch/internal/hub"
	"github.com/amir-the-h/tidewatch/internal/inference"
	"github.com/amir-the-h/tidewatch/internal/mcpserver"
	"github.com/amir-the-h/tidewatch/internal/model"
	"github.com/amir-the-h/tidewatch/internal/notifier"
	"github.com/amir-the-h/tidewatch/internal/pipeline"
	"github.com/amir-the-h/tidewatch/internal/queue"
	"github.com/amir-the-h/tidewatch/internal/source"
	"github.com/amir-the-h/tidewatch/internal/source/x"
	"github.com/amir-the-h/tidewatch/internal/store"
	"github.com/amir-the-h/tidewatch/internal/twitter"
	"github.com/amir-the-h/tidewatch/internal/worker"
	"github.com/amir-the-h/tidewatch/migrations"
	"github.com/spf13/cobra"
)

var configPath string

var serveCmd = &cobra.Command{
	Use:   "serve",
	Short: "Start the tidewatch server",
	RunE:  runServe,
}

func init() {
	serveCmd.Flags().StringVar(&configPath, "config", "configs/tidewatch.yaml", "path to YAML config file")
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

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	h := hub.New(logger)
	go h.Run(ctx)

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
	q.WithBroadcaster(h)

	activeTrue := true
	onnxType := model.ActionTypeONNX
	onnxActions, err := s.ListActions(ctx, store.ActionFilter{Type: &onnxType, Active: &activeTrue})
	if err != nil {
		return fmt.Errorf("failed to check active onnx actions: %w", err)
	}
	if len(onnxActions) > 0 {
		if err := inference.InitRuntime(cfg.Inference.ONNXLibPath); err != nil {
			return fmt.Errorf("failed to initialize ONNX runtime: %w", err)
		}
		defer func() {
			if err := inference.DestroyRuntime(); err != nil {
				logger.Error("serve: onnx runtime destroy error", "error", err)
			}
		}()
		logger.Info("serve: onnx runtime initialized", "component", "inference", "active_onnx_actions", len(onnxActions))
	} else {
		logger.Info("serve: skipping onnx runtime init (no active onnx actions)", "component", "inference")
	}

	infReg := inference.NewRegistry()

	// Load Ollama backends from DB.
	rawBackends, err := s.ListOllamaBackends(ctx)
	if err != nil {
		return fmt.Errorf("failed to load ollama backends: %w", err)
	}
	pool := inference.NewBackendPool()
	pool.WithBroadcaster(h)
	pool.Load(toBackendRecords(rawBackends))
	defer pool.Close()

	// Optional Twitter client — only created when credentials are configured.
	var twitterClient *twitter.Client
	if cfg.Twitter.ClientID != "" {
		twitterClient = twitter.NewClient(cfg.Twitter.ClientID, cfg.Twitter.ClientSecret, cfg.Twitter.CallbackURL, s)
		logger.Info("serve: twitter client enabled", "component", "twitter")
	} else {
		logger.Info("serve: twitter client disabled (no client_id configured)", "component", "twitter")
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

	// Build internal pipeline registry.
	pipeReg := pipeline.NewRegistry()
	pipeReg.Register("entity_matcher", pipeline.NewEntityMatcherFactory(s))
	pipeReg.Register("entity_discovery", pipeline.NewEntityDiscoveryFactory(s))
	pipeReg.Register("entity_cooccurrence", pipeline.NewEntityCooccurrenceFactory(s))
	nominatimURL := cfg.Nominatim.URL
	if nominatimURL == "" {
		nominatimURL = "http://nominatim:8080"
	}
	pipeReg.Register("reverse_geocoder", pipeline.NewReverseGeocoderFactory(s, nominatimURL))
	if twitterClient != nil {
		pipeReg.Register("twitter_retweet", pipeline.NewRetweetFactory(twitterClient))
		pipeReg.Register("twitter_like", pipeline.NewLikeFactory(twitterClient))
		pipeReg.Register("twitter_quote", pipeline.NewQuoteFactory(twitterClient))
	}
	if pushClient != nil {
		pipeReg.Register("notification_push", pipeline.NewPushoverFactory(pushClient))
	}
	if telegramClient != nil {
		pipeReg.Register("notification_tg", pipeline.NewTelegramFactory(telegramClient))
	}
	pipeReg.Register("discord_tag_thread", pipeline.NewDiscordTagThreadFactory())

	// Build ActionCache.
	cache := worker.NewActionCache(s, infReg, pipeReg, pool, logger)
	if err := cache.Load(ctx); err != nil {
		return fmt.Errorf("failed to load action cache: %w", err)
	}
	logger.Info("serve: action cache loaded", "component", "actions")

	dispatcher := notifier.NewDispatcher(s, logger)
	var emitter notifier.EventEmitter = dispatcher

	workerCount := cfg.Worker.Count
	if workerCount < 1 {
		workerCount = 1
	}

	mgr := worker.NewManager(workerCount, q, q, s, cache, logger).WithDispatcher(dispatcher).WithBroadcaster(h)

	// Sources: connector types are registered here (rss arrives in phase 1
	// step 4); the manager runs the active sources.
	xStream := x.NewStream(s)
	sourceReg := source.NewRegistry()
	sourceReg.Register(xStream)
	sourceReg.Register(x.NewSearch(s))
	ingester := &source.Ingester{Store: s, Producer: q, Cache: cache, Broadcaster: h, Emitter: emitter, Logger: logger}
	sourceMgr := source.NewManager(s, sourceReg, ingester, h, logger, source.Options{
		MaxConcurrentPolls: cfg.Sources.MaxConcurrentPolls,
	})

	jwtSecret := cfg.Auth.JWTSecret
	if jwtSecret == "" {
		// Refuse to start — a missing or well-known secret makes all tokens trivially forgeable.
		return fmt.Errorf("serve: auth.jwt_secret is not set; set TIDEWATCH_AUTH_JWT_SECRET (e.g. `openssl rand -hex 32`)")
	}

	if err := seedAdminUser(ctx, s, cfg.Auth.AdminUsername, cfg.Auth.AdminPassword, logger); err != nil {
		return fmt.Errorf("serve: seed admin: %w", err)
	}

	// Build the tool executor: a Dispatcher that calls back into this API server.
	_, apiPort, _ := net.SplitHostPort(cfg.API.Bind)
	if apiPort == "" {
		apiPort = "8080"
	}
	toolExec := mcpserver.NewDispatcherWithCredentials(
		"http://127.0.0.1:"+apiPort,
		cfg.Auth.AdminUsername,
		cfg.Auth.AdminPassword,
	)

	srv := api.NewServer(cfg.API.Bind, s, q, infReg, cache, pipeReg, twitterClient, pool, cfg.Ollama.FastWaitTimeout, jwtSecret, logger, dispatcher, mgr, q, q, cfg.Inference.ModelsDir, toolExec)
	srv.WithHub(h)
	srv.WithSources(sourceMgr)
	srv.WithXRules(xStream)

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)

	errCh := make(chan error, 2)

	go func() {
		logger.Info("serve: starting worker manager", "component", "worker", "count", workerCount)
		if err := mgr.Start(ctx); err != nil {
			errCh <- fmt.Errorf("worker: %w", err)
		}
	}()

	if err := sourceMgr.Start(ctx); err != nil {
		return fmt.Errorf("serve: start sources: %w", err)
	}

	go func() {
		logger.Info("serve: starting notification dispatcher", "component", "notifier")
		dispatcher.Start(ctx)
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

	sourceMgr.Stop()

	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer shutdownCancel()

	if err := srv.Shutdown(shutdownCtx); err != nil && !errors.Is(err, context.DeadlineExceeded) {
		logger.Error("serve: api server shutdown error", "error", err)
	}

	logger.Info("serve: shutdown complete")
	return nil
}

func toBackendRecords(backends []model.OllamaBackend) []inference.BackendRecord {
	out := make([]inference.BackendRecord, len(backends))
	for i, b := range backends {
		out[i] = inference.BackendRecord{ID: b.ID, Name: b.Name, URL: b.URL, Enabled: b.Enabled, MaxConcurrent: b.MaxConcurrent}
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
	if password == "changeme" {
		logger.Warn("serve: seeding admin with the default password — set TIDEWATCH_AUTH_ADMIN_PASSWORD or change it after first login", "component", "auth")
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
