package search

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
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

	mu       sync.Mutex
	sinceIDs map[int64]string // rule.ID -> newest tweet ID seen on the last run
}

// NewScheduler creates a new Scheduler that runs RunOnce every interval.
func NewScheduler(client *Client, st store.Store, interval time.Duration, params SearchParams, logger *slog.Logger) *Scheduler {
	return &Scheduler{
		client:   client,
		store:    st,
		interval: interval,
		params:   params,
		logger:   logger.With("component", "search.scheduler"),
		sinceIDs: make(map[int64]string),
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

// RunOnce loads all active rules from the store and runs a search for each
// using the scheduler's default params. Used by the periodic background loop.
func (s *Scheduler) RunOnce(ctx context.Context) (int, error) {
	return s.RunWithParams(ctx, nil, s.params)
}

// RunWithParams runs a search for the given rule IDs (all active rules if empty)
// using the provided params. Called by the API handler for on-demand runs.
//
// Per-rule since_id values are injected from the scheduler's in-memory table so
// that each run only fetches tweets that are genuinely new since the last run.
// After each successful rule fetch, the newest tweet ID is recorded and used as
// the since_id on the next call for that rule.
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

	total := 0
	for _, rule := range rules {
		// Inject the per-rule since_id so we only fetch tweets newer than the
		// last run's newest ID. If no prior run exists for this rule, the
		// caller-provided SinceID (or StartTime) acts as the initial bound.
		ruleParams := params
		s.mu.Lock()
		if id, ok := s.sinceIDs[rule.ID]; ok {
			ruleParams.SinceID = id
		}
		s.mu.Unlock()

		n, newestID, err := s.client.RunForRule(ctx, rule, ruleParams)
		total += n
		if err != nil {
			s.logger.ErrorContext(ctx, "failed to fetch tweets for rule",
				"rule_id", rule.ID,
				"rule_name", rule.Name,
				"error", err,
			)
			continue
		}
		s.logger.InfoContext(ctx, "fetched tweets for rule",
			"rule_id", rule.ID,
			"rule_name", rule.Name,
			"tweets_fetched", n,
		)

		// Record the newest tweet ID so the next run uses it as since_id.
		if newestID != "" {
			s.mu.Lock()
			s.sinceIDs[rule.ID] = newestID
			s.mu.Unlock()
		}
	}
	return total, nil
}
