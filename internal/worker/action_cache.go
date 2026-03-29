package worker

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"sync"

	"github.com/amir-the-h/iran-watcher/internal/inference"
	"github.com/amir-the-h/iran-watcher/internal/model"
	"github.com/amir-the-h/iran-watcher/internal/notifier"
	"github.com/amir-the-h/iran-watcher/internal/pipeline"
	"github.com/amir-the-h/iran-watcher/internal/store"
	"github.com/amir-the-h/iran-watcher/internal/transform"
)

// CacheEntry pairs an Action DB row with its resolved Pipeline instance and
// any output transformers parsed from the pipeline config.
type CacheEntry struct {
	Action       *model.Action
	Pipeline     inference.Pipeline
	Transformers []transform.Transformer
}

// ActionCache is a hot-reloadable in-memory map of action_id → CacheEntry.
// Shared (read-mostly) between the stream listener and all workers.
type ActionCache struct {
	mu      sync.RWMutex
	entries map[int64]*CacheEntry

	store    store.Store
	infReg   *inference.Registry
	pipeReg  *pipeline.Registry
	pool    *inference.BackendPool
	logger  *slog.Logger
}

// NewActionCache creates a new ActionCache with the given dependencies.
func NewActionCache(
	st store.Store,
	infReg *inference.Registry,
	pipeReg *pipeline.Registry,
	pool *inference.BackendPool,
	logger *slog.Logger,
) *ActionCache {
	return &ActionCache{
		entries: make(map[int64]*CacheEntry),
		store:   st,
		infReg:  infReg,
		pipeReg: pipeReg,
		pool:    pool,
		logger:  logger,
	}
}

// Load (re)builds the entire cache from active actions in the DB.
func (c *ActionCache) Load(ctx context.Context) error {
	// Fetch priority-ordered backend IDs for each pipeline.
	pipelineBackendIDs, err := c.store.ListAllPipelineBackendIDs(ctx)
	if err != nil {
		return fmt.Errorf("action_cache: list pipeline backend ids: %w", err)
	}

	active := true
	actions, err := c.store.ListActions(ctx, store.ActionFilter{Active: &active})
	if err != nil {
		return fmt.Errorf("action_cache: list actions: %w", err)
	}

	entries := make(map[int64]*CacheEntry, len(actions))
	for _, a := range actions {
		p, transformers, pErr := c.resolvePipeline(ctx, a.PipelineID, pipelineBackendIDs)
		if pErr != nil {
			if c.logger != nil {
				c.logger.Warn("action_cache: skip action (pipeline resolution failed)",
					"action_id", a.ID, "pipeline_id", a.PipelineID, "error", pErr)
			}
			continue
		}
		entries[a.ID] = &CacheEntry{Action: a, Pipeline: p, Transformers: transformers}
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

// All returns all active CacheEntries in the cache.
func (c *ActionCache) All() []*CacheEntry {
	c.mu.RLock()
	defer c.mu.RUnlock()
	out := make([]*CacheEntry, 0, len(c.entries))
	for _, e := range c.entries {
		if e.Action.Active {
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

func (c *ActionCache) resolvePipeline(ctx context.Context, pipelineID int64, pipelineBackendIDs map[int64][]int64) (inference.Pipeline, []transform.Transformer, error) {
	row, err := c.store.GetPipeline(ctx, pipelineID)
	if err != nil {
		return nil, nil, fmt.Errorf("get pipeline row: %w", err)
	}

	var cfg map[string]any
	if len(row.Config) > 0 {
		if err := json.Unmarshal(row.Config, &cfg); err != nil {
			cfg = make(map[string]any)
		}
	} else {
		cfg = make(map[string]any)
	}

	transformers, err := transform.Parse(cfg)
	if err != nil && c.logger != nil {
		c.logger.Warn("action_cache: invalid transformer config, ignoring",
			"pipeline", row.Name, "error", err)
	}

	var p inference.Pipeline
	switch row.Type {
	case model.PipelineTypeONNX:
		if p = c.infReg.Get(row.Name); p != nil {
			return p, transformers, nil
		}
		onnx, err := inference.NewONNXPipelineFromConfig(row.Name, cfg)
		if err != nil {
			return nil, nil, fmt.Errorf("onnx: %w", err)
		}
		p = onnx
		labels, _ := c.store.ListPipelineLabels(ctx, pipelineID)
		if len(labels) > 0 {
			if lp, ok := p.(inference.LabeledPipeline); ok {
				lp.SetLabels(labels)
			}
		}
		c.infReg.Register(p)

	case model.PipelineTypeOllama:
		// Always build fresh from DB config so that updates to model name,
		// system_prompt, or backend assignments take effect without a restart.
		assignedIDs := pipelineBackendIDs[pipelineID] // nil → use full pool
		p = inference.NewOllamaPipelineFromConfig(row.Name, cfg, c.pool, assignedIDs, 0)
		c.infReg.Register(p)

	case model.PipelineTypeInternal:
		p, err = c.pipeReg.Build(row.Name, cfg)
		if err != nil {
			return nil, nil, err
		}

	case model.PipelineTypeNotification:
		p, err = buildNotificationPipeline(ctx, c.store, row, c.logger)
		if err != nil {
			return nil, nil, err
		}

	default:
		return nil, nil, fmt.Errorf("unknown pipeline type: %s", row.Type)
	}

	return p, transformers, nil
}

func buildNotificationPipeline(ctx context.Context, st store.Store, p *model.Pipeline, logger *slog.Logger) (inference.Pipeline, error) {
	// Parse pipeline config for channel_id, message_template, and delivery options.
	var pipelineCfg map[string]any
	if len(p.Config) > 0 {
		if err := json.Unmarshal(p.Config, &pipelineCfg); err != nil {
			return nil, fmt.Errorf("action cache: parse notification pipeline config: %w", err)
		}
	}
	if pipelineCfg == nil {
		pipelineCfg = map[string]any{}
	}

	channelID, _ := pipelineCfg["channel_id"].(float64)
	if channelID == 0 {
		return nil, fmt.Errorf("action cache: notification pipeline missing channel_id")
	}
	messageTemplate, _ := pipelineCfg["message_template"].(string)

	channel, err := st.GetNotificationChannel(ctx, int64(channelID))
	if err != nil {
		return nil, fmt.Errorf("action cache: load notification channel %d: %w", int64(channelID), err)
	}

	provider, err := st.GetNotificationProvider(ctx, channel.ProviderID)
	if err != nil {
		return nil, fmt.Errorf("action cache: load notification provider %d: %w", channel.ProviderID, err)
	}

	// Build delivery config: pipeline options first, then overlay channel routing (chat_id, device).
	deliveryConfig := make(map[string]any, len(pipelineCfg))
	for k, v := range pipelineCfg {
		if k == "channel_id" || k == "message_template" {
			continue
		}
		deliveryConfig[k] = v
	}
	if len(channel.Config) > 0 && string(channel.Config) != "null" {
		var routingCfg map[string]any
		if err := json.Unmarshal(channel.Config, &routingCfg); err == nil {
			for k, v := range routingCfg {
				deliveryConfig[k] = v
			}
		}
	}

	var notifProvider notifier.Provider
	switch provider.Type {
	case "pushover":
		var creds struct {
			AppToken string `json:"app_token"`
			UserKey  string `json:"user_key"`
		}
		if err := json.Unmarshal(provider.Config, &creds); err != nil {
			return nil, fmt.Errorf("action cache: parse pushover config: %w", err)
		}
		notifProvider = notifier.NewPushoverClient(creds.AppToken, creds.UserKey, logger)
	case "telegram":
		var creds struct {
			BotToken string `json:"bot_token"`
		}
		if err := json.Unmarshal(provider.Config, &creds); err != nil {
			return nil, fmt.Errorf("action cache: parse telegram config: %w", err)
		}
		notifProvider = notifier.NewTelegramClient(creds.BotToken, logger)
	default:
		return nil, fmt.Errorf("action cache: unknown notification provider type %q", provider.Type)
	}

	// Provide a loader so the pipeline can expand ${actions} into per-action
	// expandable blocks without needing direct store access.
	loader := func(ctx context.Context, tweetID string) ([]pipeline.ActionSummary, error) {
		summaries, err := st.ListTweetActionSummaries(ctx, tweetID)
		if err != nil {
			return nil, err
		}
		out := make([]pipeline.ActionSummary, len(summaries))
		for i, s := range summaries {
			out[i] = pipeline.ActionSummary{
				ActionName: s.ActionName,
				Metadata:   s.Metadata,
			}
		}
		return out, nil
	}

	return pipeline.NewNotificationPipeline(p.Name, messageTemplate, deliveryConfig, notifProvider, loader), nil
}
