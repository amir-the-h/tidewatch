package api

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"

	"github.com/amir-the-h/iran-watcher/internal/inference"
	"github.com/amir-the-h/iran-watcher/internal/model"
	"github.com/amir-the-h/iran-watcher/internal/notifier"
	"github.com/amir-the-h/iran-watcher/internal/queue"
	"github.com/amir-the-h/iran-watcher/internal/store"
	"github.com/amir-the-h/iran-watcher/internal/stream"
)

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
	store      store.Store
	producer   queue.Producer
	registry   *inference.Registry
	rules      ruleManagerInterface
	stream     streamClientInterface
	tier       string
	dispatcher *notifier.Dispatcher
	logger     *slog.Logger
	router     chi.Router
	http       *http.Server
	ctx        context.Context
}

// NewServer creates a new Server and registers all routes.
func NewServer(
	bind string,
	s store.Store,
	q queue.Producer,
	reg *inference.Registry,
	rules ruleManagerInterface,
	sc streamClientInterface,
	tier string,
	logger *slog.Logger,
	dispatcher *notifier.Dispatcher,
) *Server {
	srv := &Server{
		store:      s,
		producer:   q,
		registry:   reg,
		rules:      rules,
		stream:     sc,
		tier:       tier,
		dispatcher: dispatcher,
		logger:     logger,
	}

	r := chi.NewRouter()
	r.Use(middleware.Logger)
	r.Use(middleware.Recoverer)
	r.Use(corsMiddleware)

	r.Route("/api", func(r chi.Router) {
		// Health
		r.Get("/health", srv.handleGetHealth)

		// Sources
		r.Get("/sources", srv.handleListSources)
		r.Post("/sources", srv.handleCreateSource)
		r.Put("/sources/{id}", srv.handleUpdateSource)
		r.Delete("/sources/{id}", srv.handleDeleteSource)
		r.Post("/sources/{id}/deactivate", srv.handleDeactivateSource)

		// Rules
		r.Get("/rules", srv.handleListRules)
		r.Post("/rules", srv.handleCreateRule)
		r.Get("/rules/{id}", srv.handleGetRule)
		r.Put("/rules/{id}", srv.handleUpdateRule)
		r.Delete("/rules/{id}", srv.handleDeleteRule)
		r.Post("/rules/{id}/activate", srv.handleActivateRule)
		r.Post("/rules/{id}/deactivate", srv.handleDeactivateRule)

		// Tweets — static routes MUST be registered before /{id}
		r.Get("/tweets", srv.handleListTweets)
		r.Delete("/tweets", srv.handleDeleteTweets)
		r.Post("/tweets/reprocess", srv.handleReprocessTweets)
		r.Get("/tweets/{id}", srv.handleGetTweet)

		// Models — static routes before parameterized
		r.Get("/models", srv.handleListModels)
		r.Post("/models/download", srv.handleDownloadModel)
		r.Post("/models", srv.handleCreateModel)
		r.Delete("/models/{id}", srv.handleDeleteModel)
		r.Post("/models/{id}/activate", srv.handleActivateModel)
		r.Post("/models/{id}/deactivate", srv.handleDeactivateModel)
		r.Get("/models/{id}/labels", srv.handleListModelLabels)
		r.Post("/models/{id}/labels", srv.handleAddModelLabel)
		r.Delete("/models/{id}/labels/{label}", srv.handleRemoveModelLabel)

		// Stream
		r.Get("/stream/status", srv.handleStreamStatus)
		r.Post("/stream/connect", srv.handleStreamConnect)
		r.Post("/stream/disconnect", srv.handleStreamDisconnect)

		// Categorizations
		r.Get("/categorizations", srv.handleListCategorizations)

		// Tracked Entities — static routes before parameterized
		r.Get("/tracked-entities", srv.handleListTrackedEntities)
		r.Post("/tracked-entities", srv.handleCreateTrackedEntity)
		r.Get("/tracked-entities/{id}", srv.handleGetTrackedEntity)
		r.Put("/tracked-entities/{id}", srv.handleUpdateTrackedEntity)
		r.Delete("/tracked-entities/{id}", srv.handleDeleteTrackedEntity)
		r.Post("/tracked-entities/{id}/activate", srv.handleActivateTrackedEntity)
		r.Post("/tracked-entities/{id}/deactivate", srv.handleDeactivateTrackedEntity)
		r.Get("/tracked-entities/{id}/tracks", srv.handleListEntityTracks)

		// Notifications — static routes before parameterized
		r.Get("/notifications", srv.handleListNotifications)
		r.Post("/notifications", srv.handleCreateNotification)
		r.Post("/notifications/test", srv.handleTestNotification)
		r.Get("/notifications/{id}", srv.handleGetNotification)
		r.Put("/notifications/{id}", srv.handleUpdateNotification)
		r.Delete("/notifications/{id}", srv.handleDeleteNotification)
		r.Post("/notifications/{id}/activate", srv.handleActivateNotification)
		r.Post("/notifications/{id}/deactivate", srv.handleDeactivateNotification)
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

// corsMiddleware sets CORS headers and handles preflight OPTIONS requests.
func corsMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, DELETE, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type")
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
	return func(id int64) (*model.Source, error) {
		src, err := s.store.GetSource(r.Context(), id)
		if err != nil {
			return nil, err
		}
		if !src.Active {
			return nil, fmt.Errorf("source %d is inactive", id)
		}
		return src, nil
	}
}

// makeRuleResolver returns a RuleResolver backed by the store,
// returning only active local rules.
func (s *Server) makeRuleResolver(r *http.Request) stream.RuleResolver {
	return func(id int64) (*model.Rule, error) {
		rule, err := s.store.GetRule(r.Context(), id)
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

// NewServerWithConcreteRules is a convenience constructor for production use
// where a *stream.RuleManager and *stream.Client are available.
func NewServerWithConcreteRules(
	bind string,
	s store.Store,
	q queue.Producer,
	reg *inference.Registry,
	rules *stream.RuleManager,
	sc *stream.Client,
	tier string,
	logger *slog.Logger,
	dispatcher *notifier.Dispatcher,
) *Server {
	return NewServer(bind, s, q, reg, rules, sc, tier, logger, dispatcher)
}
