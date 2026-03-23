package inference

import (
	"context"
	"fmt"
	"log/slog"
	"sync"

	"github.com/amir-the-h/iran-watcher/internal/model"
)

// Registry manages the set of active Pipeline instances.
// Workers call All() between tweet iterations.
// When the API activates/deactivates a model, call Reload() to refresh.
type Registry struct {
	mu       sync.RWMutex
	active   []Pipeline
	reloadCh chan struct{}
}

// NewRegistry creates a new empty Registry with a buffered notification channel.
func NewRegistry() *Registry {
	return &Registry{reloadCh: make(chan struct{}, 1)}
}

// load is the shared internal logic: replace active pipelines using factory,
// close old ones, and return errors only for fatal issues.
func (r *Registry) load(ctx context.Context, models []*model.Model, factory func(*model.Model) (Pipeline, error)) ([]Pipeline, error) {
	newPipelines := make([]Pipeline, 0, len(models))
	for _, m := range models {
		select {
		case <-ctx.Done():
			// Close any already-created pipelines before returning.
			for _, p := range newPipelines {
				if err := p.Close(); err != nil {
					slog.Error("inference: registry: close pipeline on ctx cancel",
						"pipeline", p.Name(), "error", err)
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
		newPipelines = append(newPipelines, p)
	}
	return newPipelines, nil
}

// Load replaces the active pipeline set using the provided models and factory.
// factory is called once per model to create a Pipeline.
// If factory returns an error for a model, that model is skipped (logged), others continue.
func (r *Registry) Load(ctx context.Context, models []*model.Model, factory func(*model.Model) (Pipeline, error)) error {
	newPipelines, err := r.load(ctx, models, factory)
	if err != nil {
		return err
	}

	r.mu.Lock()
	old := r.active
	r.active = newPipelines
	r.mu.Unlock()

	for _, p := range old {
		if closeErr := p.Close(); closeErr != nil {
			slog.Error("inference: registry: close old pipeline",
				"pipeline", p.Name(), "error", closeErr)
		}
	}
	return nil
}

// All returns a snapshot of currently active pipelines (concurrent reads safe).
func (r *Registry) All() []Pipeline {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]Pipeline, len(r.active))
	copy(out, r.active)
	return out
}

// Reload replaces active pipelines and sends a signal on NotifyChan.
// Workers select on this channel between tweet iterations to pick up model changes.
func (r *Registry) Reload(ctx context.Context, models []*model.Model, factory func(*model.Model) (Pipeline, error)) error {
	if err := r.Load(ctx, models, factory); err != nil {
		return err
	}
	// Non-blocking send: if no worker is listening the signal is dropped, not blocked.
	select {
	case r.reloadCh <- struct{}{}:
	default:
	}
	return nil
}

// NotifyChan returns a channel that receives a struct{} after each Reload call.
// Workers select on this channel between tweet iterations to pick up model changes.
// The channel is buffered(1) — if no worker is listening, the signal is not blocked.
func (r *Registry) NotifyChan() <-chan struct{} {
	return r.reloadCh
}
