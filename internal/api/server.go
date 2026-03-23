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

// Server is the REST API server.
type Server struct {
	store    store.Store
	producer queue.Producer
	registry *inference.Registry
	rules    ruleManagerInterface
	tier     string
	logger   *slog.Logger
	router   chi.Router
	http     *http.Server
}

// NewServer creates a new Server and registers all routes.
func NewServer(
	bind string,
	s store.Store,
	q queue.Producer,
	reg *inference.Registry,
	rules ruleManagerInterface,
	tier string,
	logger *slog.Logger,
) *Server {
	srv := &Server{
		store:    s,
		producer: q,
		registry: reg,
		rules:    rules,
		tier:     tier,
		logger:   logger,
	}

	r := chi.NewRouter()
	r.Use(middleware.Logger)
	r.Use(middleware.Recoverer)

	r.Route("/api", func(r chi.Router) {
		// Health
		r.Get("/health", srv.handleGetHealth)

		// Sources
		r.Get("/sources", srv.handleListSources)
		r.Post("/sources", srv.handleCreateSource)
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

		// Tweets — static route /reprocess MUST be registered before /{id}
		r.Get("/tweets", srv.handleListTweets)
		r.Post("/tweets/reprocess", srv.handleReprocessTweets)
		r.Get("/tweets/{id}", srv.handleGetTweet)

		// Models — static routes before parameterized
		r.Get("/models", srv.handleListModels)
		r.Post("/models/download", srv.handleDownloadModel)
		r.Post("/models", srv.handleCreateModel)
		r.Delete("/models/{id}", srv.handleDeleteModel)
		r.Post("/models/{id}/activate", srv.handleActivateModel)
		r.Post("/models/{id}/deactivate", srv.handleDeactivateModel)

		// Categorizations
		r.Get("/categorizations", srv.handleListCategorizations)
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

// NewServerWithConcreteRules is a convenience constructor for production use
// where a *stream.RuleManager is available.
func NewServerWithConcreteRules(
	bind string,
	s store.Store,
	q queue.Producer,
	reg *inference.Registry,
	rules *stream.RuleManager,
	tier string,
	logger *slog.Logger,
) *Server {
	return NewServer(bind, s, q, reg, rules, tier, logger)
}
