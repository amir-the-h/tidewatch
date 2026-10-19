// Package pipeline provides the internal pipeline registry and built-in
// pipeline implementations (entity matcher, twitter, notification).
package pipeline

import (
	"fmt"

	"github.com/amir-the-h/tidewatch/internal/inference"
)

// Factory creates a Pipeline instance from DB config.
type Factory func(name string, config map[string]any) (inference.Pipeline, error)

// Registry maps handler names to factories for internal pipelines.
type Registry struct {
	factories map[string]Factory
}

// NewRegistry creates an empty Registry.
func NewRegistry() *Registry {
	return &Registry{factories: make(map[string]Factory)}
}

// Register associates a handler name with a factory function.
func (r *Registry) Register(handler string, f Factory) {
	r.factories[handler] = f
}

// Handlers returns all registered handler names.
func (r *Registry) Handlers() []string {
	out := make([]string, 0, len(r.factories))
	for h := range r.factories {
		out = append(out, h)
	}
	return out
}

// Build instantiates a pipeline using the factory for the given handler.
// config must contain "handler" key matching a registered name.
func (r *Registry) Build(name string, config map[string]any) (inference.Pipeline, error) {
	handler, _ := config["handler"].(string)
	if handler == "" {
		return nil, fmt.Errorf("pipeline: registry: config missing 'handler'")
	}
	f, ok := r.factories[handler]
	if !ok {
		return nil, fmt.Errorf("pipeline: registry: unknown handler %q", handler)
	}
	return f(name, config)
}
