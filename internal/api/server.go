package api

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"

	"github.com/amir-the-h/iran-watcher/internal/auth"
	"github.com/amir-the-h/iran-watcher/internal/inference"
	"github.com/amir-the-h/iran-watcher/internal/model"
	"github.com/amir-the-h/iran-watcher/internal/notifier"
	"github.com/amir-the-h/iran-watcher/internal/pipeline"
	"github.com/amir-the-h/iran-watcher/internal/queue"
	"github.com/amir-the-h/iran-watcher/internal/search"
	"github.com/amir-the-h/iran-watcher/internal/store"
	"github.com/amir-the-h/iran-watcher/internal/stream"
	"github.com/amir-the-h/iran-watcher/internal/twitter"
	"github.com/amir-the-h/iran-watcher/internal/worker"
)

// queueSizer can report the number of items in a named queue.
type queueSizer interface {
	Len(ctx context.Context, queue string) (int64, error)
}

// workerPoolStatsProvider is satisfied by *worker.Pool and *worker.Manager.
type workerPoolStatsProvider interface {
	Stats() worker.PoolStats
}

// workerBufferFlusher is optionally implemented by the worker pool; satisfied
// by *worker.Manager. A type assertion is used so the interface does not need
// to be added to the existing workerPoolStatsProvider.
type workerBufferFlusher interface {
	Flush() int
	FlushPipeline(name string) int
}

// searchScheduler is satisfied by *search.Scheduler.
type searchScheduler interface {
	RunOnce(ctx context.Context) (int, error)
	RunWithParams(ctx context.Context, ruleIDs []int64, params search.SearchParams) (int, error)
	DefaultParams() search.SearchParams
}

// ruleManagerInterface allows mocking of stream.RuleManager in tests.
type ruleManagerInterface interface {
	AddRule(ctx context.Context, tag, value string) (string, error)
	DeleteRule(ctx context.Context, xRuleID string) error
	ListRules(ctx context.Context) ([]stream.XRule, error)
}

// streamClientInterface allows mocking of stream.Client in tests.
type streamClientInterface interface {
	Connect(ctx context.Context) error
	Disconnect()
	Connected() bool
}

// Server is the REST API server.
type Server struct {
	store                   store.Store
	producer                queue.Producer
	registry                *inference.Registry
	actionCache             *worker.ActionCache
	pipelineRegistry        *pipeline.Registry
	twitterClient           *twitter.Client // nil when Twitter not configured
	rules                   ruleManagerInterface
	stream                  streamClientInterface
	tier                    string
	backendPool             *inference.BackendPool
	ollamaFastWaitTimeout   time.Duration
	jwtSecret               string
	dispatcher              *notifier.Dispatcher
	searchScheduler         searchScheduler // nil if search not configured
	pool                    workerPoolStatsProvider
	sizer                   queueSizer
	failureLister           queue.FailureLister
	logger                  *slog.Logger
	modelsDir               string
	router                  chi.Router
	http                    *http.Server
	ctx                     context.Context
}

// NewServer creates a new Server and registers all routes.
func NewServer(
	bind string,
	s store.Store,
	q queue.Producer,
	reg *inference.Registry,
	cache *worker.ActionCache,
	pipeReg *pipeline.Registry,
	tc *twitter.Client,
	rules ruleManagerInterface,
	sc streamClientInterface,
	tier string,
	backendPool *inference.BackendPool,
	ollamaFastWaitTimeout time.Duration,
	jwtSecret string,
	logger *slog.Logger,
	dispatcher *notifier.Dispatcher,
	ss searchScheduler,
	pool workerPoolStatsProvider,
	sizer queueSizer,
	lister queue.FailureLister,
	modelsDir string,
) *Server {
	srv := &Server{
		store:                 s,
		producer:              q,
		registry:              reg,
		actionCache:           cache,
		pipelineRegistry:      pipeReg,
		twitterClient:         tc,
		rules:                 rules,
		stream:                sc,
		tier:                  tier,
		backendPool:           backendPool,
		ollamaFastWaitTimeout: ollamaFastWaitTimeout,
		jwtSecret:             jwtSecret,
		dispatcher:            dispatcher,
		searchScheduler:       ss,
		pool:                  pool,
		sizer:                 sizer,
		failureLister:         lister,
		logger:                logger,
		modelsDir:             modelsDir,
	}

	authMw := auth.Middleware(jwtSecret)
	adminMw := auth.AdminOnly

	r := chi.NewRouter()
	r.Use(middleware.Logger)
	r.Use(middleware.Recoverer)
	r.Use(corsMiddleware)

	r.Route("/api", func(r chi.Router) {
		// Public — no auth required.
		r.Post("/auth/login", srv.handleLogin)
		r.Get("/twitter/auth/callback", srv.handleTwitterAuthCallback)

		// All routes below require a valid JWT.
		r.Group(func(r chi.Router) {
			r.Use(authMw)

			r.Get("/auth/me", srv.handleMe)

			// Health + stream status — viewers can see these for the status bar.
			r.Get("/health", srv.handleGetHealth)
			r.Get("/stream/status", srv.handleStreamStatus)

			// Read-only endpoints accessible to all authenticated users.
			r.Get("/rules", srv.handleListRules)
			r.Get("/rules/{id}", srv.handleGetRule)
			r.Get("/sources", srv.handleListSources)
			r.Get("/tweets", srv.handleListTweets)
			r.Get("/tweets/{id}", srv.handleGetTweet)
			r.Get("/map", srv.handleListMapPins)
			r.Get("/tracked-entities", srv.handleListTrackedEntities)
			r.Get("/tracked-entities/{id}", srv.handleGetTrackedEntity)
			r.Get("/tracked-entities/{id}/tracks", srv.handleListEntityTracks)
			r.Get("/tracked-entities/{id}/alias-suggestions", srv.handleListEntityAliasSuggestions)
			r.Get("/alias-suggestions", srv.handleSearchAliasSuggestions)

			// Chains (read) and triggers — all authenticated users.
			r.Get("/chains", srv.handleListChains)
			r.Get("/chains/{id}", srv.handleGetChain)
			r.Get("/chains/{id}/graph", srv.handleGetChainGraph)
			r.Get("/triggers", srv.handleListTriggers)
			r.Get("/triggers/links", srv.handleListTriggerChainLinks)

			// Admin-only routes.
			r.Group(func(r chi.Router) {
				r.Use(adminMw)

				// Sources
				r.Post("/sources", srv.handleCreateSource)
				r.Put("/sources/{id}", srv.handleUpdateSource)
				r.Delete("/sources/{id}", srv.handleDeleteSource)
				r.Post("/sources/{id}/deactivate", srv.handleDeactivateSource)

				// Rules
				r.Post("/rules", srv.handleCreateRule)
				r.Put("/rules/{id}", srv.handleUpdateRule)
				r.Delete("/rules/{id}", srv.handleDeleteRule)
				r.Post("/rules/{id}/activate", srv.handleActivateRule)
				r.Post("/rules/{id}/deactivate", srv.handleDeactivateRule)

				// Tweets mutations
				r.Delete("/tweets", srv.handleDeleteTweets)
				r.Post("/tweets/reprocess", srv.handleReprocessTweets)
				r.Delete("/tweets/noise", srv.handleDeleteNoiseTweets)

				// Pipelines (replaces /models)
				r.Get("/pipelines", srv.handleListPipelines)
				r.Post("/pipelines", srv.handleCreatePipeline)
				r.Get("/pipelines/handlers", srv.handleListPipelineHandlers)
				r.Get("/models", srv.handleListModels)
				r.Put("/pipelines/{id}", srv.handleUpdatePipeline)
				r.Delete("/pipelines/{id}", srv.handleDeletePipeline)
				r.Post("/pipelines/{id}/activate", srv.handleActivatePipeline)
				r.Post("/pipelines/{id}/deactivate", srv.handleDeactivatePipeline)

				// Chains (mutations)
				r.Post("/chains", srv.handleCreateChain)
				r.Put("/chains/{id}", srv.handleUpdateChain)
				r.Delete("/chains/{id}", srv.handleDeleteChain)
				r.Post("/chains/{id}/activate", srv.handleActivateChain)
				r.Post("/chains/{id}/deactivate", srv.handleDeactivateChain)
				r.Put("/chains/{id}/graph", srv.handleSaveChainGraph)

				// Trigger chain links
				r.Post("/triggers/links", srv.handleCreateTriggerChainLink)
				r.Delete("/triggers/links/{id}", srv.handleDeleteTriggerChainLink)
				r.Post("/triggers/links/{id}/activate", srv.handleActivateTriggerChainLink)
				r.Post("/triggers/links/{id}/deactivate", srv.handleDeactivateTriggerChainLink)

				// Stream control
				r.Post("/stream/connect", srv.handleStreamConnect)
				r.Post("/stream/disconnect", srv.handleStreamDisconnect)

				// Tracked Entities mutations
				r.Post("/tracked-entities", srv.handleCreateTrackedEntity)
				r.Put("/tracked-entities/{id}", srv.handleUpdateTrackedEntity)
				r.Delete("/tracked-entities/{id}", srv.handleDeleteTrackedEntity)
				r.Post("/tracked-entities/{id}/activate", srv.handleActivateTrackedEntity)
				r.Post("/tracked-entities/{id}/deactivate", srv.handleDeactivateTrackedEntity)

				// Summaries
				r.Get("/summaries", srv.handleListSummaries)
				r.Post("/summaries/run", srv.handleRunSummaries)

				// Digest jobs
				r.Get("/digest", srv.handleListDigests)
				r.Post("/digest", srv.handleDigest)
				r.Get("/digest/{id}", srv.handleGetDigest)
				r.Delete("/digest/{id}", srv.handleDeleteDigest)

				// Chat
				r.Post("/chat/estimate", srv.handleChatEstimate)
				r.Post("/chat/stream", srv.handleChatStream)

				// Failures (dead-letter queue)
				r.Get("/failures", srv.handleListFailures)
				r.Delete("/failures", srv.handleClearFailures)

				// Ollama backend management
				r.Get("/ollama/backends", srv.handleListOllamaBackends)
				r.Post("/ollama/backends", srv.handleCreateOllamaBackend)
				r.Put("/ollama/backends/{id}", srv.handleUpdateOllamaBackend)
				r.Delete("/ollama/backends/{id}", srv.handleDeleteOllamaBackend)
				r.Post("/ollama/backends/{id}/enable", srv.handleEnableOllamaBackend)
				r.Post("/ollama/backends/{id}/disable", srv.handleDisableOllamaBackend)
				// Pipeline-backend assignments
				r.Get("/pipelines/{id}/backends", srv.handleListPipelineBackends)
				r.Put("/pipelines/{id}/backends", srv.handleSetPipelineBackends)
				r.Delete("/pipelines/{id}/backends/{backend_id}", srv.handleRemovePipelineBackend)

				// Queue management
				r.Delete("/queue", srv.handleFlushQueue)
				r.Delete("/workers/buffer", srv.handleFlushWorkerBuffer)
				r.Delete("/workers/buffer/{pipeline}", srv.handleFlushPipelineBuffer)

				// Search jobs
				r.Get("/search", srv.handleListSearchJobs)
				r.Post("/search/run", srv.handleSearchRun)
				r.Get("/search/{id}", srv.handleGetSearchJob)
				r.Delete("/search/{id}", srv.handleDeleteSearchJob)

				// Notification providers
				r.Get("/notification-providers", srv.handleListNotificationProviders)
				r.Post("/notification-providers", srv.handleCreateNotificationProvider)
				r.Get("/notification-providers/{id}", srv.handleGetNotificationProvider)
				r.Put("/notification-providers/{id}", srv.handleUpdateNotificationProvider)
				r.Delete("/notification-providers/{id}", srv.handleDeleteNotificationProvider)
				r.Post("/notification-providers/{id}/activate", srv.handleActivateNotificationProvider)
				r.Post("/notification-providers/{id}/deactivate", srv.handleDeactivateNotificationProvider)

				// Notification channels
				r.Get("/notification-channels", srv.handleListNotificationChannels)
				r.Post("/notification-channels", srv.handleCreateNotificationChannel)
				r.Get("/notification-channels/{id}", srv.handleGetNotificationChannel)
				r.Put("/notification-channels/{id}", srv.handleUpdateNotificationChannel)
				r.Delete("/notification-channels/{id}", srv.handleDeleteNotificationChannel)
				r.Post("/notification-channels/{id}/activate", srv.handleActivateNotificationChannel)
				r.Post("/notification-channels/{id}/deactivate", srv.handleDeactivateNotificationChannel)

				// User management
				r.Get("/users", srv.handleListUsers)
				r.Post("/users", srv.handleCreateUser)
				r.Put("/users/{id}", srv.handleUpdateUser)
				r.Delete("/users/{id}", srv.handleDeleteUser)

				// Actions
				r.Get("/actions", srv.handleListActions)
				r.Post("/actions", srv.handleCreateAction)
				r.Put("/actions/{id}", srv.handleUpdateAction)
				r.Delete("/actions/{id}", srv.handleDeleteAction)
				r.Post("/actions/{id}/activate", srv.handleActivateAction)
				r.Post("/actions/{id}/deactivate", srv.handleDeactivateAction)
				r.Post("/actions/{id}/run", srv.handleRunAction)

				// Action execution log
				r.Get("/actions/{id}/executions", srv.handleListActionExecutions)
				r.Get("/executions", srv.handleListAllExecutions)
				r.Get("/executions/{id}/metadata", srv.handleGetExecutionMetadata)
				r.Delete("/executions", srv.handleDeleteExecutions)

				// Twitter OAuth (callback is public — registered above)
				r.Get("/twitter/auth/start", srv.handleTwitterAuthStart)
				r.Get("/twitter/auth/status", srv.handleTwitterAuthStatus)
				r.Delete("/twitter/auth", srv.handleTwitterDisconnect)
			})
		})
	})

	srv.router = r
	srv.http = &http.Server{
		Addr:    bind,
		Handler: r,
	}

	return srv
}

// Start begins listening. Blocks until ctx is cancelled or an error occurs.
func (s *Server) Start(ctx context.Context) error {
	s.ctx = ctx
	errCh := make(chan error, 1)
	go func() {
		s.logger.Info("api: server starting", "addr", s.http.Addr)
		if err := s.http.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			errCh <- err
		}
		close(errCh)
	}()

	select {
	case <-ctx.Done():
		return s.Shutdown(context.Background())
	case err := <-errCh:
		return err
	}
}

// Shutdown gracefully stops the HTTP server.
func (s *Server) Shutdown(ctx context.Context) error {
	s.logger.Info("api: server shutting down")
	return s.http.Shutdown(ctx)
}

// reloadCache reloads the action cache from the store.
// Called after any change that affects pipeline or action configuration.
func (s *Server) reloadCache(ctx context.Context) {
	if s.actionCache == nil {
		return
	}
	if err := s.actionCache.Load(ctx); err != nil && s.logger != nil {
		s.logger.Warn("api: reload action cache", "error", err)
	}
}

// corsMiddleware sets CORS headers and handles preflight OPTIONS requests.
func corsMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, DELETE, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization")
		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		next.ServeHTTP(w, r)
	})
}

// writeJSON encodes v as JSON and writes it with the given HTTP status.
func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

// writeError writes a JSON error response with the given HTTP status.
func writeError(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]string{"error": msg})
}

// makeResolver returns a SourceResolver backed by the store,
// returning only active sources.
func (s *Server) makeResolver(r *http.Request) stream.SourceResolver {
	return s.resolverWithCtx(r.Context())
}

// makeRuleResolver returns a RuleResolver backed by the store,
// returning only active local rules.
func (s *Server) makeRuleResolver(r *http.Request) stream.RuleResolver {
	return s.ruleResolverWithCtx(r.Context())
}

func (s *Server) resolverWithCtx(ctx context.Context) stream.SourceResolver {
	return func(id int64) (*model.Source, error) {
		src, err := s.store.GetSource(ctx, id)
		if err != nil {
			return nil, err
		}
		if !src.Active {
			return nil, fmt.Errorf("source %d is inactive", id)
		}
		return src, nil
	}
}

func (s *Server) ruleResolverWithCtx(ctx context.Context) stream.RuleResolver {
	return func(id int64) (*model.Rule, error) {
		rule, err := s.store.GetRule(ctx, id)
		if err != nil {
			return nil, err
		}
		if !rule.Local {
			return nil, fmt.Errorf("rule %d is not a local rule", id)
		}
		if !rule.Active {
			return nil, fmt.Errorf("rule %d is inactive", id)
		}
		return rule, nil
	}
}

// cascadeRecompile recompiles all rules that directly or transitively
// reference changedID. Active X stream rules are updated on X as well.
// Intended to run in a background goroutine using the server's lifecycle
// context (s.ctx) so it outlives the originating HTTP request.
func (s *Server) cascadeRecompile(ctx context.Context, changedID int64) {
	resolver := s.resolverWithCtx(ctx)
	ruleResolver := s.ruleResolverWithCtx(ctx)
	compiler := stream.NewRuleCompiler()

	// BFS over the dependency graph.
	visited := map[int64]bool{changedID: true}
	queue := []int64{changedID}

	for len(queue) > 0 {
		current := queue[0]
		queue = queue[1:]

		deps, err := s.store.ListRulesDependingOn(ctx, current)
		if err != nil {
			s.logger.Error("cascade: list dependents", "rule_id", current, "error", err)
			return
		}

		for _, dep := range deps {
			if visited[dep.ID] {
				continue
			}
			visited[dep.ID] = true

			var node model.ExprNode
			if err := json.Unmarshal(dep.Expression, &node); err != nil {
				s.logger.Error("cascade: parse expression", "rule_id", dep.ID, "error", err)
				continue
			}

			compiled, err := compiler.CompileWithRules(node, resolver, ruleResolver)
			if err != nil {
				s.logger.Warn("cascade: recompile failed", "rule_id", dep.ID, "error", err)
				continue
			}

			dep.CompiledValue = compiled

			if !dep.Local && dep.Active && dep.XRuleID != "" {
				if err := s.rules.DeleteRule(ctx, dep.XRuleID); err != nil {
					s.logger.Error("cascade: delete old x rule", "rule_id", dep.ID, "error", err)
					continue
				}
				newXRuleID, err := s.rules.AddRule(ctx, dep.Name, compiled)
				if err != nil {
					s.logger.Error("cascade: add new x rule", "rule_id", dep.ID, "error", err)
					continue
				}
				dep.XRuleID = newXRuleID
				if err := s.store.SetRuleActive(ctx, dep.ID, true, newXRuleID); err != nil {
					s.logger.Error("cascade: set rule active", "rule_id", dep.ID, "error", err)
					continue
				}
			}

			if err := s.store.UpdateRule(ctx, dep); err != nil {
				s.logger.Error("cascade: update rule", "rule_id", dep.ID, "error", err)
				continue
			}

			s.logger.Info("cascade: recompiled rule", "rule_id", dep.ID, "triggered_by", changedID)
			queue = append(queue, dep.ID)
		}
	}
}

// NewServerWithConcreteRules is a convenience constructor for production use
// where a *stream.RuleManager and *stream.Client are available.
func NewServerWithConcreteRules(
	bind string,
	s store.Store,
	q queue.Producer,
	reg *inference.Registry,
	cache *worker.ActionCache,
	pipeReg *pipeline.Registry,
	tc *twitter.Client,
	rules *stream.RuleManager,
	sc *stream.Client,
	tier string,
	backendPool *inference.BackendPool,
	ollamaFastWaitTimeout time.Duration,
	jwtSecret string,
	logger *slog.Logger,
	dispatcher *notifier.Dispatcher,
	ss searchScheduler,
	pool workerPoolStatsProvider,
	sizer queueSizer,
	lister queue.FailureLister,
	modelsDir string,
) *Server {
	return NewServer(bind, s, q, reg, cache, pipeReg, tc, rules, sc, tier, backendPool, ollamaFastWaitTimeout, jwtSecret, logger, dispatcher, ss, pool, sizer, lister, modelsDir)
}
