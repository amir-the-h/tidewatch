package pipeline

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/amir-the-h/tidewatch/internal/inference"
)

// cooccurrenceStore is the minimal store interface required by EntityCooccurrencePipeline.
type cooccurrenceStore interface {
	UpsertEntityConnection(ctx context.Context, fromID, toID int64) error
}

// EntityCooccurrencePipeline reads matched_ids from the pipeline context and
// upserts a connection row for every unique pair of co-occurring entities.
// Outputs: {"pairs_updated": N}
type EntityCooccurrencePipeline struct {
	name  string
	store cooccurrenceStore
}

// NewEntityCooccurrenceFactory returns a Factory for EntityCooccurrencePipeline.
func NewEntityCooccurrenceFactory(st cooccurrenceStore) Factory {
	return func(name string, _ map[string]any) (inference.Pipeline, error) {
		return &EntityCooccurrencePipeline{name: name, store: st}, nil
	}
}

func (p *EntityCooccurrencePipeline) Name() string                 { return p.name }
func (p *EntityCooccurrencePipeline) Type() inference.PipelineType { return inference.PipelineTypeInternal }
func (p *EntityCooccurrencePipeline) Close() error                 { return nil }

func (p *EntityCooccurrencePipeline) Run(ctx context.Context, input inference.PipelineInput) (map[string]any, error) {
	ids := extractMatchedIDs(input.Context)
	if len(ids) < 2 {
		return map[string]any{"pairs_updated": 0}, nil
	}

	count := 0
	for i := 0; i < len(ids); i++ {
		for j := i + 1; j < len(ids); j++ {
			a, b := ids[i], ids[j]
			if err := p.store.UpsertEntityConnection(ctx, a, b); err != nil {
				return nil, fmt.Errorf("entity_cooccurrence: upsert (%d,%d): %w", a, b, err)
			}
			count++
		}
	}

	return map[string]any{"pairs_updated": count}, nil
}

// extractMatchedIDs pulls []int64 from context["matched_ids"].
// It handles both []int64 (direct) and []any (JSON-decoded from Redis context).
func extractMatchedIDs(ctx map[string]any) []int64 {
	raw, ok := ctx["matched_ids"]
	if !ok {
		return nil
	}
	// Direct type assertion for in-process chain calls.
	if ids, ok := raw.([]int64); ok {
		return ids
	}
	// JSON round-trip via Redis stores numbers as float64 inside []any.
	if anySlice, ok := raw.([]any); ok {
		ids := make([]int64, 0, len(anySlice))
		for _, v := range anySlice {
			switch n := v.(type) {
			case float64:
				ids = append(ids, int64(n))
			case json.Number:
				if i, err := n.Int64(); err == nil {
					ids = append(ids, i)
				}
			}
		}
		return ids
	}
	return nil
}
