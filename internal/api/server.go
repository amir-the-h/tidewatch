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

	"github.com/amir-the-h/tidewatch/internal/auth"
	"github.com/amir-the-h/tidewatch/internal/hub"
	"github.com/amir-the-h/tidewatch/internal/inference"
	"github.com/amir-the-h/tidewatch/internal/model"
	"github.com/amir-the-h/tidewatch/internal/notifier"
	"github.com/amir-the-h/tidewatch/internal/pipeline"
	"github.com/amir-the-h/tidewatch/internal/queue"
	"github.com/amir-the-h/tidewatch/internal/source"
	"github.com/amir-the-h/tidewatch/internal/source/x"
	"github.com/amir-the-h/tidewatch/internal/store"
	"github.com/amir-the-h/tidewatch/internal/twitter"
	"github.com/amir-the-h/tidewatch/internal/worker"
)

// queueSizer can report queue sizes.
type queueSizer interface {
	Len(ctx context.Context, queue string) (int64, error)
	// TotalTaskLen returns the total number of pending tasks in the global sorted-set queue.
	TotalTaskLen(ctx context.Context) (int64, error)
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

// xRuleSyncer reaches the active x_stream source's rules API; satisfied by
// *x.Stream. api is nil when no x_stream source is active.
type xRuleSyncer interface {
	WithRules(ctx context.Context, fn func(api x.RuleAPI, tier string) error) error
}

// Server is the REST API server.
type Server struct {
	store                 store.Store
	producer              queue.Producer
	registry              *inference.Registry
	actionCache           *worker.ActionCache
	pipelineRegistry      *pipeline.Registry
	twitterClient         *twitter.Client // nil when Twitter not configured
	xRules                xRuleSyncer     // nil until WithXRules: rules are only stored
	backendPool           *inference.BackendPool
	ollamaFastWaitTimeout time.Duration
	jwtSecret             string
	dispatcher            *notifier.Dispatcher
	toolExec              inference.ToolExecutor
	sources               *source.Manager // nil until WithSources
	pool                  workerPoolStatsProvider
	sizer                 queueSizer
	failureLister         queue.FailureLister
	logger                *slog.Logger
	modelsDir             string
	hub                   *hub.Hub
	router                chi.Router
	http                  *http.Server
	ctx                   context.Context
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
	backendPool *inference.BackendPool,
	ollamaFastWaitTimeout time.Duration,
	jwtSecret string,
	logger *slog.Logger,
	dispatcher *notifier.Dispatcher,
	pool workerPoolStatsProvider,
	sizer queueSizer,
	lister queue.FailureLister,
	modelsDir string,
	toolExec inference.ToolExecutor,
) *Server {
	srv := &Server{
		store:                 s,
		producer:              q,
		registry:              reg,
		actionCache:           cache,
		pipelineRegistry:      pipeReg,
		twitterClient:         tc,
		backendPool:           backendPool,
		ollamaFastWaitTimeout: ollamaFastWaitTimeout,
		jwtSecret:             jwtSecret,
		dispatcher:            dispatcher,
		toolExec:              toolExec,
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
		r.Get("/ws", srv.handleWS)

		// All routes below require a valid JWT.
		r.Group(func(r chi.Router) {
			r.Use(authMw)

			r.Get("/auth/me", srv.handleMe)

			// Health — viewers can see it for the status bar.
			r.Get("/health", srv.handleGetHealth)

			// Read-only endpoints accessible to all authenticated users.
			r.Get("/x/rules", srv.handleListXRules)
			r.Get("/x/rules/{id}", srv.handleGetXRule)
			r.Get("/x/terms", srv.handleListXTerms)
			r.Get("/signals", srv.handleListSignals)
			r.Get("/signals/{id}", srv.handleGetSignal)
			r.Get("/map", srv.handleListMapPins)
			r.Get("/tracked-entities", srv.handleListTrackedEntities)
			r.Get("/tracked-entities/{id}", srv.handleGetTrackedEntity)
			r.Get("/tracked-entities/{id}/tracks", srv.handleListEntityTracks)
			r.Get("/tracked-entities/{id}/alias-suggestions", srv.handleListEntityAliasSuggestions)
			r.Get("/alias-suggestions", srv.handleSearchAliasSuggestions)
			r.Get("/entity-connections/subgraph", srv.handleGetEntityConnectionSubgraph)

			// Pipelines (DAG workflows, read) and triggers — all authenticated users.
			r.Get("/pipelines", srv.handleListPipelines)
			r.Get("/pipelines/{id}", srv.handleGetPipeline)
			r.Get("/pipelines/{id}/graph", srv.handleGetPipelineGraph)
			r.Get("/triggers", srv.handleListTriggers)
			r.Get("/triggers/links", srv.handleListTriggerPipelineLinks)

			// Admin-only routes.
			r.Group(func(r chi.Router) {
				r.Use(adminMw)

				// X rule builder: terms (rule atoms) and rules
				r.Post("/x/terms", srv.handleCreateXTerm)
				r.Put("/x/terms/{id}", srv.handleUpdateXTerm)
				r.Delete("/x/terms", srv.handleBulkDeleteXTerms)
				r.Delete("/x/terms/{id}", srv.handleDeleteXTerm)
				r.Post("/x/terms/{id}/deactivate", srv.handleDeactivateXTerm)
				r.Post("/x/rules", srv.handleCreateXRule)
				r.Put("/x/rules/{id}", srv.handleUpdateXRule)
				r.Delete("/x/rules", srv.handleBulkDeleteXRules)
				r.Delete("/x/rules/{id}", srv.handleDeleteXRule)
				r.Post("/x/rules/{id}/activate", srv.handleActivateXRule)
				r.Post("/x/rules/{id}/deactivate", srv.handleDeactivateXRule)

				// Sources (configured connector instances)
				r.Get("/sources", srv.handleListSources)
				r.Post("/sources", srv.handleCreateSource)
				r.Delete("/sources", srv.handleBulkDeleteSources)
				r.Get("/sources/types", srv.handleListSourceTypes)
				r.Get("/sources/status", srv.handleListSourceStatus)
				r.Get("/sources/{id}", srv.handleGetSource)
				r.Put("/sources/{id}", srv.handleUpdateSource)
				r.Delete("/sources/{id}", srv.handleDeleteSource)
				r.Post("/sources/{id}/activate", srv.handleActivateSource)
				r.Post("/sources/{id}/deactivate", srv.handleDeactivateSource)
				r.Post("/sources/{id}/fetch", srv.handleFetchSource)
				r.Post("/sources/{id}/backfill", srv.handleBackfillSource)
				r.Get("/sources/{id}/runs", srv.handleListSourceRuns)
				r.Get("/source-runs/{id}", srv.handleGetSourceRun)

				// Signals mutations
				r.Delete("/signals", srv.handleDeleteSignals)
				r.Post("/signals/reprocess", srv.handleReprocessSignals)
				r.Delete("/signals/noise", srv.handleDeleteNoiseSignals)

				// Pipelines (DAG workflows — mutations)
				r.Post("/pipelines", srv.handleCreatePipeline)
				r.Put("/pipelines/{id}", srv.handleUpdatePipeline)
				r.Delete("/pipelines", srv.handleBulkDeletePipelines)
				r.Delete("/pipelines/{id}", srv.handleDeletePipeline)
				r.Post("/pipelines/{id}/activate", srv.handleActivatePipeline)
				r.Post("/pipelines/{id}/deactivate", srv.handleDeactivatePipeline)
				r.Put("/pipelines/{id}/graph", srv.handleSavePipelineGraph)
				r.Post("/pipelines/{id}/run", srv.handleRunPipeline)

				// Trigger pipeline links
				r.Post("/triggers/links", srv.handleCreateTriggerPipelineLink)
				r.Delete("/triggers/links", srv.handleBulkDeleteTriggerPipelineLinks)
				r.Delete("/triggers/links/{id}", srv.handleDeleteTriggerPipelineLink)
				r.Post("/triggers/links/{id}/activate", srv.handleActivateTriggerPipelineLink)
				r.Post("/triggers/links/{id}/deactivate", srv.handleDeactivateTriggerPipelineLink)

				// Tracked Entities mutations
				r.Post("/tracked-entities", srv.handleCreateTrackedEntity)
				r.Put("/tracked-entities/{id}", srv.handleUpdateTrackedEntity)
				r.Delete("/tracked-entities", srv.handleBulkDeleteTrackedEntities)
				r.Delete("/tracked-entities/{id}", srv.handleDeleteTrackedEntity)
				r.Post("/tracked-entities/{id}/activate", srv.handleActivateTrackedEntity)
				r.Post("/tracked-entities/{id}/deactivate", srv.handleDeactivateTrackedEntity)

				// Entity Candidates
				r.Get("/entity-candidates", srv.handleListEntityCandidates)
				r.Get("/entity-candidates/{id}", srv.handleGetEntityCandidate)
				r.Post("/entity-candidates/{id}/promote", srv.handlePromoteEntityCandidate)
				r.Post("/entity-candidates/{id}/reject", srv.handleRejectEntityCandidate)
				r.Post("/entity-candidates/{id}/merge", srv.handleMergeEntityCandidate)
				r.Delete("/entity-candidates", srv.handleBulkDeleteEntityCandidates)
				r.Delete("/entity-candidates/{id}", srv.handleDeleteEntityCandidate)

				// Entity Connections
				r.Get("/entity-connections", srv.handleListEntityConnections)
				r.Delete("/entity-connections", srv.handleDeleteEntityConnections)

				// Summaries
				r.Get("/summaries", srv.handleListSummaries)
				r.Post("/summaries/run", srv.handleRunSummaries)

				// Digest jobs
				r.Get("/digest", srv.handleListDigests)
				r.Post("/digest", srv.handleDigest)
				r.Get("/digest/{id}", srv.handleGetDigest)
				r.Delete("/digest", srv.handleBulkDeleteDigestJobs)
				r.Delete("/digest/{id}", srv.handleDeleteDigest)

				// Chat
				r.Get("/chat/tools", srv.handleChatTools)
				r.Post("/chat/stream", srv.handleChatStream)

				// Failures (dead-letter queue)
				r.Get("/failures", srv.handleListFailures)
				r.Delete("/failures", srv.handleClearFailures)

				// Ollama backend management
				r.Get("/ollama/backends", srv.handleListOllamaBackends)
				r.Post("/ollama/backends", srv.handleCreateOllamaBackend)
				r.Put("/ollama/backends/{id}", srv.handleUpdateOllamaBackend)
				r.Delete("/ollama/backends", srv.handleBulkDeleteOllamaBackends)
				r.Delete("/ollama/backends/{id}", srv.handleDeleteOllamaBackend)
				r.Post("/ollama/backends/{id}/enable", srv.handleEnableOllamaBackend)
				r.Post("/ollama/backends/{id}/disable", srv.handleDisableOllamaBackend)
				// Action-backend assignments (Ollama backend priority for each action)
				r.Get("/actions/{id}/backends", srv.handleListActionBackends)
				r.Put("/actions/{id}/backends", srv.handleSetActionBackends)
				r.Delete("/actions/{id}/backends/{backend_id}", srv.handleRemoveActionBackend)

				// Queue management
				r.Get("/queue", srv.handleGetQueue)
				r.Delete("/queue", srv.handleFlushQueue)
				r.Delete("/workers/buffer", srv.handleFlushWorkerBuffer)
				r.Delete("/workers/buffer/{pipeline}", srv.handleFlushPipelineBuffer)

				// Notification providers
				r.Get("/notification-providers", srv.handleListNotificationProviders)
				r.Post("/notification-providers", srv.handleCreateNotificationProvider)
				r.Get("/notification-providers/{id}", srv.handleGetNotificationProvider)
				r.Put("/notification-providers/{id}", srv.handleUpdateNotificationProvider)
				r.Delete("/notification-providers", srv.handleBulkDeleteNotificationProviders)
				r.Delete("/notification-providers/{id}", srv.handleDeleteNotificationProvider)
				r.Post("/notification-providers/{id}/activate", srv.handleActivateNotificationProvider)
				r.Post("/notification-providers/{id}/deactivate", srv.handleDeactivateNotificationProvider)

				// Notification channels
				r.Get("/notification-channels", srv.handleListNotificationChannels)
				r.Post("/notification-channels", srv.handleCreateNotificationChannel)
				r.Get("/notification-channels/{id}", srv.handleGetNotificationChannel)
				r.Put("/notification-channels/{id}", srv.handleUpdateNotificationChannel)
				r.Delete("/notification-channels", srv.handleBulkDeleteNotificationChannels)
				r.Delete("/notification-channels/{id}", srv.handleDeleteNotificationChannel)
				r.Post("/notification-channels/{id}/activate", srv.handleActivateNotificationChannel)
				r.Post("/notification-channels/{id}/deactivate", srv.handleDeactivateNotificationChannel)

				// User management
				r.Get("/users", srv.handleListUsers)
				r.Post("/users", srv.handleCreateUser)
				r.Put("/users/{id}", srv.handleUpdateUser)
				r.Delete("/users/{id}", srv.handleDeleteUser)

				// Actions (execution backends: onnx, ollama, internal, notification)
				r.Get("/actions", srv.handleListActions)
				r.Post("/actions", srv.handleCreateAction)
				r.Get("/actions/handlers", srv.handleListActionHandlers)
				r.Get("/models", srv.handleListModels)
				r.Get("/actions/{id}", srv.handleGetAction)
				r.Put("/actions/{id}", srv.handleUpdateAction)
				r.Delete("/actions", srv.handleBulkDeleteActions)
				r.Delete("/actions/{id}", srv.handleDeleteAction)
				r.Post("/actions/{id}/activate", srv.handleActivateAction)
				r.Post("/actions/{id}/deactivate", srv.handleDeactivateAction)

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

// WithHub wires the WebSocket hub for live event push.
func (s *Server) WithHub(h *hub.Hub) *Server {
	s.hub = h
	return s
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

// broadcastEvent fans out a typed event to all connected WebSocket clients.
// No-op if no hub is configured.
func (s *Server) broadcastEvent(eventType string, payload any) {
	if s.hub == nil {
		return
	}
	s.hub.Broadcast(hub.Event{Type: eventType, Payload: payload})
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
func (s *Server) makeResolver(r *http.Request) x.SourceResolver {
	return s.resolverWithCtx(r.Context())
}

// makeRuleResolver returns a RuleResolver backed by the store,
// returning only active local rules.
func (s *Server) makeRuleResolver(r *http.Request) x.RuleResolver {
	return s.ruleResolverWithCtx(r.Context())
}

func (s *Server) resolverWithCtx(ctx context.Context) x.SourceResolver {
	return func(id int64) (*model.XTerm, error) {
		src, err := s.store.GetXTerm(ctx, id)
		if err != nil {
			return nil, err
		}
		if !src.Active {
			return nil, fmt.Errorf("source %d is inactive", id)
		}
		return src, nil
	}
}

func (s *Server) ruleResolverWithCtx(ctx context.Context) x.RuleResolver {
	return func(id int64) (*model.XRule, error) {
		rule, err := s.store.GetXRule(ctx, id)
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
	err := s.withXRules(ctx, func(api x.RuleAPI, _ string) error {
		s.recompileDependents(ctx, api, changedID)
		return nil
	})
	if err != nil {
		s.logger.Error("cascade: x rules", "rule_id", changedID, "error", err)
	}
}

// recompileDependents is cascadeRecompile's walk; api is nil when no x_stream
// source is active (the next stream start syncs the new values).
func (s *Server) recompileDependents(ctx context.Context, api x.RuleAPI, changedID int64) {
	resolver := s.resolverWithCtx(ctx)
	ruleResolver := s.ruleResolverWithCtx(ctx)
	compiler := x.NewRuleCompiler()

	// BFS over the dependency graph.
	visited := map[int64]bool{changedID: true}
	queue := []int64{changedID}

	for len(queue) > 0 {
		current := queue[0]
		queue = queue[1:]

		deps, err := s.store.ListXRulesDependingOn(ctx, current)
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

			if api != nil && !dep.Local && dep.Active && dep.XRuleID != "" {
				if err := api.DeleteRule(ctx, dep.XRuleID); err != nil {
					s.logger.Error("cascade: delete old x rule", "rule_id", dep.ID, "error", err)
					continue
				}
				newXRuleID, err := api.AddRule(ctx, dep.Name, compiled)
				if err != nil {
					s.logger.Error("cascade: add new x rule", "rule_id", dep.ID, "error", err)
					continue
				}
				dep.XRuleID = newXRuleID
				if err := s.store.SetXRuleActive(ctx, dep.ID, true, newXRuleID); err != nil {
					s.logger.Error("cascade: set rule active", "rule_id", dep.ID, "error", err)
					continue
				}
			}

			if err := s.store.UpdateXRule(ctx, dep); err != nil {
				s.logger.Error("cascade: update rule", "rule_id", dep.ID, "error", err)
				continue
			}

			s.logger.Info("cascade: recompiled rule", "rule_id", dep.ID, "triggered_by", changedID)
			queue = append(queue, dep.ID)
		}
	}
}

// WithXRules wires the x_stream connector, through which the x_rules routes
// reach the active x_stream source to sync rules with X.
func (s *Server) WithXRules(r xRuleSyncer) *Server {
	s.xRules = r
	return s
}

// withXRules runs fn holding the X rule-sync lock, with the active x_stream
// source's rules API and the tier rules are validated for. With no active
// x_stream source api is nil and tier is "basic": rule changes are only
// stored, and the stream syncs them with X when it next starts.
func (s *Server) withXRules(ctx context.Context, fn func(api x.RuleAPI, tier string) error) error {
	if s.xRules == nil {
		return fn(nil, "basic")
	}
	return s.xRules.WithRules(ctx, fn)
}
