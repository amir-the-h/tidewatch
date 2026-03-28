package worker

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"sync"

	"github.com/amir-the-h/iran-watcher/internal/inference"
	"github.com/amir-the-h/iran-watcher/internal/model"
	"github.com/amir-the-h/iran-watcher/internal/pipeline"
	"github.com/amir-the-h/iran-watcher/internal/store"
)

// CacheEntry pairs an Action DB row with its resolved Pipeline instance.
type CacheEntry struct {
	Action   *model.Action
	Pipeline inference.Pipeline
}

// ActionCache is a hot-reloadable in-memory map of action_id → CacheEntry.
// Shared (read-mostly) between the stream listener and all workers.
type ActionCache struct {
	mu      sync.RWMutex
	entries map[int64]*CacheEntry

	store    store.Store
	infReg   *inference.Registry
	pipeReg  *pipeline.Registry
	backends []inference.BackendConfig
	logger   *slog.Logger
}

// NewActionCache creates a new ActionCache with the given dependencies.
func NewActionCache(
	st store.Store,
	infReg *inference.Registry,
	pipeReg *pipeline.Registry,
	backends []inference.BackendConfig,
	logger *slog.Logger,
) *ActionCache {
	return &ActionCache{
		entries:  make(map[int64]*CacheEntry),
		store:    st,
		infReg:   infReg,
		pipeReg:  pipeReg,
		backends: backends,
		logger:   logger,
	}
}

// Load (re)builds the entire cache from active actions in the DB.
func (c *ActionCache) Load(ctx context.Context) error {
	active := true
	actions, err := c.store.ListActions(ctx, store.ActionFilter{Active: &active})
	if err != nil {
		return fmt.Errorf("action_cache: list actions: %w", err)
	}

	entries := make(map[int64]*CacheEntry, len(actions))
	for _, a := range actions {
		p, pErr := c.resolvePipeline(ctx, a.PipelineID)
		if pErr != nil {
			if c.logger != nil {
				c.logger.Warn("action_cache: skip action (pipeline resolution failed)",
					"action_id", a.ID, "pipeline_id", a.PipelineID, "error", pErr)
			}
			continue
		}
		entries[a.ID] = &CacheEntry{Action: a, Pipeline: p}
	}

	c.mu.Lock()
	c.entries = entries
	c.mu.Unlock()
	return nil
}

// Get returns the CacheEntry for the given action ID, or nil if not found.
func (c *ActionCache) Get(actionID int64) *CacheEntry {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.entries[actionID]
}

// AllForRules returns CacheEntries whose actions should fire for the given rule IDs.
// Global actions (empty rule_ids) are always included.
func (c *ActionCache) AllForRules(matchedRuleIDs []int64) []*CacheEntry {
	ruleSet := make(map[int64]struct{}, len(matchedRuleIDs))
	for _, id := range matchedRuleIDs {
		ruleSet[id] = struct{}{}
	}

	c.mu.RLock()
	defer c.mu.RUnlock()

	var out []*CacheEntry
	for _, e := range c.entries {
		if !e.Action.Active {
			continue
		}
		if len(e.Action.RuleIDs) == 0 {
			out = append(out, e)
			continue
		}
		for _, rid := range e.Action.RuleIDs {
			if _, ok := ruleSet[rid]; ok {
				out = append(out, e)
				break
			}
		}
	}
	return out
}

// AllForTrigger returns CacheEntries for the given trigger type, filtered by rule IDs.
func (c *ActionCache) AllForTrigger(trigger model.TriggerType, matchedRuleIDs []int64) []*CacheEntry {
	all := c.AllForRules(matchedRuleIDs)
	var out []*CacheEntry
	for _, e := range all {
		if e.Action.TriggerType == trigger {
			out = append(out, e)
		}
	}
	return out
}

// FindByType returns the first active pipeline matching the given inference type.
// Used by chat/digest endpoints to locate an Ollama pipeline.
func (c *ActionCache) FindByType(pipelineType inference.PipelineType) inference.Pipeline {
	c.mu.RLock()
	defer c.mu.RUnlock()
	for _, e := range c.entries {
		if e.Pipeline.Type() == pipelineType {
			return e.Pipeline
		}
	}
	return nil
}

func (c *ActionCache) resolvePipeline(ctx context.Context, pipelineID int64) (inference.Pipeline, error) {
	row, err := c.store.GetPipeline(ctx, pipelineID)
	if err != nil {
		return nil, fmt.Errorf("get pipeline row: %w", err)
	}

	var cfg map[string]any
	if len(row.Config) > 0 {
		if err := json.Unmarshal(row.Config, &cfg); err != nil {
			cfg = make(map[string]any)
		}
	} else {
		cfg = make(map[string]any)
	}

	switch row.Type {
	case model.PipelineTypeONNX:
		if p := c.infReg.Get(row.Name); p != nil {
			return p, nil
		}
		onnx, err := inference.NewONNXPipelineFromConfig(row.Name, cfg)
		if err != nil {
			return nil, fmt.Errorf("onnx: %w", err)
		}
		var p inference.Pipeline = onnx
		labels, _ := c.store.ListPipelineLabels(ctx, pipelineID)
		if len(labels) > 0 {
			if lp, ok := p.(inference.LabeledPipeline); ok {
				lp.SetLabels(labels)
			}
		}
		c.infReg.Register(p)
		return p, nil

	case model.PipelineTypeOllama:
		if p := c.infReg.Get(row.Name); p != nil {
			return p, nil
		}
		p := inference.NewOllamaPipelineFromConfig(row.Name, cfg, c.backends, 0)
		c.infReg.Register(p)
		return p, nil

	case model.PipelineTypeInternal:
		return c.pipeReg.Build(row.Name, cfg)

	default:
		return nil, fmt.Errorf("unknown pipeline type: %s", row.Type)
	}
}
