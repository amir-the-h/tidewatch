package pipeline

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"

	"github.com/amir-the-h/iran-watcher/internal/inference"
	"github.com/amir-the-h/iran-watcher/internal/model"
)

// discoveryStore is the minimal store interface required by EntityDiscoveryPipeline.
type discoveryStore interface {
	UpsertEntityCandidate(ctx context.Context, c *model.EntityCandidate) error
	UpdateEntityCandidateStatus(ctx context.Context, id int64, status string, promotedEntityID *int64, autoPromoted bool) error
	SaveTrackedEntity(ctx context.Context, e *model.TrackedEntity) error
}

// entityDiscoveryConfig holds the user-configured thresholds and settings.
type entityDiscoveryConfig struct {
	MinConfidence    float64  `json:"min_confidence"`
	MinFrequency     int      `json:"min_frequency"`
	AutoPromoteTypes []string `json:"auto_promote_types"`
	EntityKey        string   `json:"entity_key"`
}

// EntityDiscoveryPipeline extracts entities from the execution context,
// upserts them as candidates, and auto-promotes those that cross thresholds.
// Outputs: {"discovered": N, "auto_promoted": M, "candidates": [...]}
type EntityDiscoveryPipeline struct {
	name   string
	config entityDiscoveryConfig
	store  discoveryStore
	logger *slog.Logger
}

// NewEntityDiscoveryFactory returns a Factory for EntityDiscoveryPipeline.
func NewEntityDiscoveryFactory(st discoveryStore) Factory {
	return func(name string, cfg map[string]any) (inference.Pipeline, error) {
		b, err := json.Marshal(cfg)
		if err != nil {
			return nil, fmt.Errorf("entity_discovery: marshal config: %w", err)
		}
		var c entityDiscoveryConfig
		if err := json.Unmarshal(b, &c); err != nil {
			return nil, fmt.Errorf("entity_discovery: parse config: %w", err)
		}
		if c.EntityKey == "" {
			c.EntityKey = "entities"
		}
		if c.MinConfidence == 0 {
			c.MinConfidence = 0.75
		}
		if c.MinFrequency == 0 {
			c.MinFrequency = 3
		}
		return newEntityDiscoveryPipeline(name, c, st), nil
	}
}

// newEntityDiscoveryPipeline creates a pipeline instance — used by factory and tests.
func newEntityDiscoveryPipeline(name string, cfg entityDiscoveryConfig, st discoveryStore) *EntityDiscoveryPipeline {
	return &EntityDiscoveryPipeline{name: name, config: cfg, store: st, logger: slog.Default()}
}

func (p *EntityDiscoveryPipeline) Name() string                 { return p.name }
func (p *EntityDiscoveryPipeline) Type() inference.PipelineType { return inference.PipelineTypeInternal }
func (p *EntityDiscoveryPipeline) Close() error                 { return nil }

func (p *EntityDiscoveryPipeline) Run(ctx context.Context, input inference.PipelineInput) (map[string]any, error) {
	nerEntities := p.extractEntities(input)
	if len(nerEntities) == 0 {
		return map[string]any{
			"discovered":    0,
			"auto_promoted": 0,
			"candidates":    []any{},
		}, nil
	}

	type result struct {
		Word   string `json:"word"`
		Type   string `json:"type"`
		Status string `json:"status"`
	}
	var results []result
	autoPromotedCount := 0

	for _, ne := range nerEntities {
		word := normalizeNERWord(ne.Word)
		if len([]rune(word)) < 2 {
			continue // skip tokenizer artifacts and single chars
		}
		c := &model.EntityCandidate{
			Word:          word,
			Type:          ne.Label,
			AvgConfidence: float64(ne.Score),
			Sources:       []string{p.name},
		}

		if err := p.store.UpsertEntityCandidate(ctx, c); err != nil {
			p.logger.Warn("entity_discovery: upsert candidate", "word", ne.Word, "error", err)
			continue
		}

		if c.Status == "pending" && p.shouldAutoPromote(c) {
			if err := p.autoPromote(ctx, c); err != nil {
				p.logger.Warn("entity_discovery: auto-promote", "word", c.Word, "error", err)
			} else {
				autoPromotedCount++
			}
		}

		results = append(results, result{Word: c.Word, Type: c.Type, Status: c.Status})
	}

	return map[string]any{
		"discovered":    len(results),
		"auto_promoted": autoPromotedCount,
		"candidates":    results,
	}, nil
}

// extractEntities reads and parses the entities array from the pipeline context.
// Accepts both []model.NEREntity and []map[string]any shapes.
func (p *EntityDiscoveryPipeline) extractEntities(input inference.PipelineInput) []model.NEREntity {
	raw, ok := input.Context[p.config.EntityKey]
	if !ok {
		return nil
	}
	b, err := json.Marshal(raw)
	if err != nil {
		return nil
	}
	var entities []model.NEREntity
	if err := json.Unmarshal(b, &entities); err != nil {
		return nil
	}
	return entities
}

// shouldAutoPromote returns true when the candidate meets all configured thresholds.
// An empty AutoPromoteTypes list means nothing auto-promotes.
func (p *EntityDiscoveryPipeline) shouldAutoPromote(c *model.EntityCandidate) bool {
	if len(p.config.AutoPromoteTypes) == 0 {
		return false
	}
	if c.AvgConfidence < p.config.MinConfidence {
		return false
	}
	if c.OccurrenceCount < p.config.MinFrequency {
		return false
	}
	for _, t := range p.config.AutoPromoteTypes {
		if t == c.Type {
			return true
		}
	}
	return false
}

// autoPromote creates a TrackedEntity and marks the candidate as promoted.
// On UNIQUE name conflict, the candidate is left as pending (user resolves via UI).
func (p *EntityDiscoveryPipeline) autoPromote(ctx context.Context, c *model.EntityCandidate) error {
	e := &model.TrackedEntity{
		Name:    c.Word,
		Type:    c.Type,
		Aliases: []string{},
		Active:  true,
	}
	if err := p.store.SaveTrackedEntity(ctx, e); err != nil {
		if strings.Contains(err.Error(), "UNIQUE constraint failed") {
			p.logger.Debug("entity_discovery: candidate already tracked, leaving pending", "word", c.Word)
			return nil
		}
		return fmt.Errorf("entity_discovery: save tracked entity: %w", err)
	}
	if err := p.store.UpdateEntityCandidateStatus(ctx, c.ID, "promoted", &e.ID, true); err != nil {
		return fmt.Errorf("entity_discovery: update candidate status: %w", err)
	}
	c.Status = "promoted"
	c.PromotedEntityID = &e.ID
	return nil
}

// normalizeNERWord strips tokenizer artifacts produced by HuggingFace BPE/WordPiece models:
// leading Ċ/Ġ/▁ (sentencepiece/BPE continuation markers), ## (BERT wordpiece prefix), and #.
func normalizeNERWord(word string) string {
	// Strip leading Unicode continuation markers (Ġ U+0120, Ċ U+010A, ▁ U+2581)
	word = strings.TrimLeftFunc(word, func(r rune) bool {
		return r == '\u0120' || r == '\u010A' || r == '\u2581'
	})
	// Strip BERT wordpiece prefix and stray hash characters
	word = strings.TrimPrefix(word, "##")
	word = strings.TrimLeft(word, "#")
	return strings.TrimSpace(word)
}
