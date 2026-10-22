package source

import (
	"fmt"
	"sort"
	"sync"
)

// Registry maps connector type → connector. It is populated at startup in
// cli/cmd/serve.go, like internal/pipeline.Registry.
type Registry struct {
	mu         sync.RWMutex
	connectors map[string]Connector
}

// NewRegistry returns an empty registry.
func NewRegistry() *Registry {
	return &Registry{connectors: map[string]Connector{}}
}

// Register adds a connector under its descriptor's type. It panics on a
// duplicate type or a connector that implements neither mode, since both are
// programming errors.
func (r *Registry) Register(c Connector) {
	d := c.Descriptor()
	switch d.Mode {
	case ModeStream:
		if _, ok := c.(StreamConnector); !ok {
			panic(fmt.Sprintf("source: connector %q declares stream mode but has no Run", d.Type))
		}
	case ModePoll:
		if _, ok := c.(PollConnector); !ok {
			panic(fmt.Sprintf("source: connector %q declares poll mode but has no Poll", d.Type))
		}
	default:
		panic(fmt.Sprintf("source: connector %q has unknown mode %q", d.Type, d.Mode))
	}
	if d.Backfill {
		if _, ok := c.(Backfiller); !ok {
			panic(fmt.Sprintf("source: connector %q declares backfill but has no Backfill", d.Type))
		}
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, dup := r.connectors[d.Type]; dup {
		panic(fmt.Sprintf("source: connector %q registered twice", d.Type))
	}
	r.connectors[d.Type] = c
}

// Get returns the connector for a type.
func (r *Registry) Get(typ string) (Connector, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	c, ok := r.connectors[typ]
	return c, ok
}

// Descriptors returns every registered connector's descriptor, sorted by type.
func (r *Registry) Descriptors() []Descriptor {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]Descriptor, 0, len(r.connectors))
	for _, c := range r.connectors {
		out = append(out, c.Descriptor())
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Type < out[j].Type })
	return out
}
