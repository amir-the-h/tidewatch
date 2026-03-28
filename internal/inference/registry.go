package inference

import "sync"

// Registry manages the set of active Pipeline instances.
type Registry struct {
	mu      sync.RWMutex
	entries []Pipeline
}

// NewRegistry creates a new empty Registry.
func NewRegistry() *Registry { return &Registry{} }

// Register adds or replaces a pipeline by name.
func (r *Registry) Register(p Pipeline) {
	r.mu.Lock()
	defer r.mu.Unlock()
	for i, e := range r.entries {
		if e.Name() == p.Name() {
			r.entries[i] = p
			return
		}
	}
	r.entries = append(r.entries, p)
}

// Get returns the pipeline with the given name, or nil.
func (r *Registry) Get(name string) Pipeline {
	r.mu.RLock()
	defer r.mu.RUnlock()
	for _, e := range r.entries {
		if e.Name() == name {
			return e
		}
	}
	return nil
}

// All returns all registered pipelines.
func (r *Registry) All() []Pipeline {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]Pipeline, len(r.entries))
	copy(out, r.entries)
	return out
}

// SetLabels updates candidate labels on a zero-shot pipeline by name.
func (r *Registry) SetLabels(name string, labels []string) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	for _, e := range r.entries {
		if lp, ok := e.(LabeledPipeline); ok && e.Name() == name {
			lp.SetLabels(labels)
			return
		}
	}
}
