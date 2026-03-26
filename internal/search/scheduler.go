package search

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/amir-the-h/iran-watcher/internal/model"
	"github.com/amir-the-h/iran-watcher/internal/store"
)

// Scheduler periodically invokes the search Client for all active rules.
type Scheduler struct {
	client   *Client
	store    store.Store
	interval time.Duration
	params   SearchParams
	logger   *slog.Logger
}

// NewScheduler creates a new Scheduler that runs RunOnce every interval.
func NewScheduler(client *Client, st store.Store, interval time.Duration, params SearchParams, logger *slog.Logger) *Scheduler {
	return &Scheduler{
		client:   client,
		store:    st,
		interval: interval,
		params:   params,
		logger:   logger.With("component", "search.scheduler"),
	}
}

// Start runs the scheduler loop until ctx is cancelled.
// It waits interval before the first run.
func (s *Scheduler) Start(ctx context.Context) error {
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(s.interval):
			n, err := s.RunOnce(ctx)
			if err != nil {
				s.logger.ErrorContext(ctx, "search run failed", "error", err)
				continue
			}
			s.logger.InfoContext(ctx, "search run completed", "tweets_fetched", n)
		}
	}
}

// DefaultParams returns the scheduler's configured default SearchParams.
func (s *Scheduler) DefaultParams() SearchParams {
	return s.params
}

// RunOnce loads all active rules from the store and calls client.RunForRules
// using the scheduler's default params. Used by the periodic background loop.
func (s *Scheduler) RunOnce(ctx context.Context) (int, error) {
	return s.RunWithParams(ctx, nil, s.params)
}

// RunWithParams runs a search for the given rule IDs (all active rules if empty)
// using the provided params. Called by the API handler for on-demand runs.
func (s *Scheduler) RunWithParams(ctx context.Context, ruleIDs []int64, params SearchParams) (int, error) {
	var rules []*model.Rule

	if len(ruleIDs) > 0 {
		for _, id := range ruleIDs {
			r, err := s.store.GetRule(ctx, id)
			if err != nil {
				return 0, fmt.Errorf("search: scheduler: get rule %d: %w", id, err)
			}
			if r.Active && r.CompiledValue != "" && !r.Local {
				rules = append(rules, r)
			}
		}
	} else {
		active := true
		local := false
		all, err := s.store.ListRules(ctx, store.RuleFilter{Active: &active, Local: &local})
		if err != nil {
			return 0, fmt.Errorf("search: scheduler: list rules: %w", err)
		}
		for _, r := range all {
			if r.CompiledValue != "" {
				rules = append(rules, r)
			}
		}
	}

	n, err := s.client.RunForRules(ctx, rules, params)
	if err != nil {
		return n, fmt.Errorf("search: scheduler: run for rules: %w", err)
	}
	return n, nil
}
