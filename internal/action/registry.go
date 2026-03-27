package action

import (
	"sync"
)

// registryEntry pairs an Action with the rule IDs that gate it.
type registryEntry struct {
	action  Action
	ruleIDs []int64
}

// Registry manages the set of active Action instances.
// The worker manager calls AllForRules() to get the actions applicable to a tweet.
// Call Reload() whenever actions are created, updated, deleted, or a model reloads.
type Registry struct {
	mu       sync.RWMutex
	entries  []registryEntry
	reloadCh chan struct{}
}

// NewRegistry creates an empty Registry.
func NewRegistry() *Registry {
	return &Registry{reloadCh: make(chan struct{}, 1)}
}

// Load replaces the active action set.
func (r *Registry) Load(actions []Action) {
	entries := make([]registryEntry, len(actions))
	for i, a := range actions {
		entries[i] = registryEntry{action: a, ruleIDs: a.RuleIDs()}
	}

	r.mu.Lock()
	r.entries = entries
	r.mu.Unlock()
}

// Reload replaces the active action set and signals workers to resync channels.
func (r *Registry) Reload(actions []Action) {
	r.Load(actions)
	select {
	case r.reloadCh <- struct{}{}:
	default:
	}
}

// All returns all active actions.
func (r *Registry) All() []Action {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]Action, len(r.entries))
	for i, e := range r.entries {
		out[i] = e.action
	}
	return out
}

// AllForRules returns actions that should fire for a tweet matched by the given
// rule IDs. Global actions (empty rule_ids) are always included; restricted
// actions are included only if at least one rule ID overlaps.
func (r *Registry) AllForRules(matchedRuleIDs []int64) []Action {
	r.mu.RLock()
	defer r.mu.RUnlock()

	ruleSet := make(map[int64]struct{}, len(matchedRuleIDs))
	for _, id := range matchedRuleIDs {
		ruleSet[id] = struct{}{}
	}

	out := make([]Action, 0, len(r.entries))
	for _, e := range r.entries {
		if len(e.ruleIDs) == 0 {
			out = append(out, e.action)
			continue
		}
		for _, rid := range e.ruleIDs {
			if _, ok := ruleSet[rid]; ok {
				out = append(out, e.action)
				break
			}
		}
	}
	return out
}

// NotifyChan returns a channel that receives a struct{} after each Reload call.
func (r *Registry) NotifyChan() <-chan struct{} {
	return r.reloadCh
}
