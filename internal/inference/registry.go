package inference

import (
	"context"
	"fmt"
	"log/slog"
	"sync"

	"github.com/amir-the-h/iran-watcher/internal/model"
)

// registryEntry pairs a loaded Pipeline with the rule IDs that gate it.
// An empty ruleIDs slice means "run on all tweets" (global model).
type registryEntry struct {
	pipeline  Pipeline
	ruleIDs   []int64
	modelType model.ModelType
}

// Registry manages the set of active Pipeline instances.
// Workers call AllForRules() to get the pipelines applicable to a given tweet.
// When the API activates/deactivates a model, call Reload() to refresh.
type Registry struct {
	mu       sync.RWMutex
	entries  []registryEntry
	reloadCh chan struct{}
}

// NewRegistry creates a new empty Registry with a buffered notification channel.
func NewRegistry() *Registry {
	return &Registry{reloadCh: make(chan struct{}, 1)}
}

// load is the shared internal logic: build entries from models using factory.
func (r *Registry) load(ctx context.Context, models []*model.Model, factory func(*model.Model) (Pipeline, error)) ([]registryEntry, error) {
	entries := make([]registryEntry, 0, len(models))
	for _, m := range models {
		select {
		case <-ctx.Done():
			for _, e := range entries {
				if err := e.pipeline.Close(); err != nil {
					slog.Error("inference: registry: close pipeline on ctx cancel",
						"pipeline", e.pipeline.Name(), "error", err)
				}
			}
			return nil, fmt.Errorf("inference: registry: load cancelled: %w", ctx.Err())
		default:
		}

		p, err := factory(m)
		if err != nil {
			slog.Error("inference: registry: factory failed, skipping model",
				"model", m.Name, "error", err)
			continue
		}
		entries = append(entries, registryEntry{pipeline: p, ruleIDs: m.RuleIDs, modelType: m.Type})
	}
	return entries, nil
}

// Load replaces the active pipeline set using the provided models and factory.
func (r *Registry) Load(ctx context.Context, models []*model.Model, factory func(*model.Model) (Pipeline, error)) error {
	newEntries, err := r.load(ctx, models, factory)
	if err != nil {
		return err
	}

	r.mu.Lock()
	old := r.entries
	r.entries = newEntries
	r.mu.Unlock()

	for _, e := range old {
		if closeErr := e.pipeline.Close(); closeErr != nil {
			slog.Error("inference: registry: close old pipeline",
				"pipeline", e.pipeline.Name(), "error", closeErr)
		}
	}
	return nil
}

// Get returns the active pipeline with the given name, or nil if not loaded.
func (r *Registry) Get(name string) Pipeline {
	r.mu.RLock()
	defer r.mu.RUnlock()
	for _, e := range r.entries {
		if e.pipeline.Name() == name {
			return e.pipeline
		}
	}
	return nil
}

// All returns all active pipelines regardless of rule restrictions.
func (r *Registry) All() []Pipeline {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]Pipeline, len(r.entries))
	for i, e := range r.entries {
		out[i] = e.pipeline
	}
	return out
}

// AllForRules returns pipelines that should run for a tweet matched by the given
// rule IDs. Global pipelines (no rule restrictions) are always included.
// Restricted pipelines are included only if at least one of their rule IDs
// appears in matchedRuleIDs. If matchedRuleIDs is empty, only global pipelines
// are returned.
func (r *Registry) AllForRules(matchedRuleIDs []int64) []Pipeline {
	r.mu.RLock()
	defer r.mu.RUnlock()

	// Build a set for O(1) lookup.
	ruleSet := make(map[int64]struct{}, len(matchedRuleIDs))
	for _, id := range matchedRuleIDs {
		ruleSet[id] = struct{}{}
	}

	out := make([]Pipeline, 0, len(r.entries))
	for _, e := range r.entries {
		// Digest models are invoked only by the digest endpoint, never by the worker.
		if e.modelType == model.ModelTypeDigest {
			continue
		}
		if len(e.ruleIDs) == 0 {
			// Global model — always run.
			out = append(out, e.pipeline)
			continue
		}
		for _, rid := range e.ruleIDs {
			if _, ok := ruleSet[rid]; ok {
				out = append(out, e.pipeline)
				break
			}
		}
	}
	return out
}

// Reload replaces active pipelines and sends a signal on NotifyChan.
func (r *Registry) Reload(ctx context.Context, models []*model.Model, factory func(*model.Model) (Pipeline, error)) error {
	if err := r.Load(ctx, models, factory); err != nil {
		return err
	}
	select {
	case r.reloadCh <- struct{}{}:
	default:
	}
	return nil
}

// SetLabels updates the candidate labels for a zero-shot pipeline identified by
// name, without reloading or interrupting the model session.
func (r *Registry) SetLabels(name string, labels []string) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	for _, e := range r.entries {
		if lp, ok := e.pipeline.(LabeledPipeline); ok && e.pipeline.Name() == name {
			lp.SetLabels(labels)
			return
		}
	}
}

// NotifyChan returns a channel that receives a struct{} after each Reload call.
func (r *Registry) NotifyChan() <-chan struct{} {
	return r.reloadCh
}
