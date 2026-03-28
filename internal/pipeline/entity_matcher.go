package pipeline

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/amir-the-h/iran-watcher/internal/inference"
	"github.com/amir-the-h/iran-watcher/internal/model"
	"github.com/amir-the-h/iran-watcher/internal/store"
)

// EntityMatcherPipeline matches active tracked entities in tweet text.
// Outputs: {"matched_ids": [1,2], "matched": true}
type EntityMatcherPipeline struct {
	name  string
	store store.Store
}

// NewEntityMatcherFactory returns a Factory that creates EntityMatcherPipeline.
func NewEntityMatcherFactory(st store.Store) Factory {
	return func(name string, _ map[string]any) (inference.Pipeline, error) {
		return &EntityMatcherPipeline{name: name, store: st}, nil
	}
}

func (p *EntityMatcherPipeline) Name() string                  { return p.name }
func (p *EntityMatcherPipeline) Type() inference.PipelineType  { return inference.PipelineTypeInternal }
func (p *EntityMatcherPipeline) Close() error                  { return nil }

func (p *EntityMatcherPipeline) Run(ctx context.Context, input inference.PipelineInput) (map[string]any, error) {
	entities, err := p.store.ListActiveTrackedEntities(ctx)
	if err != nil {
		return nil, fmt.Errorf("entity_matcher: list entities: %w", err)
	}

	// Decode NER entities from context if present (populated by an upstream NER pipeline).
	var nerEntities []model.NEREntity
	if raw, ok := input.Context["entities"]; ok {
		if b, err := json.Marshal(raw); err == nil {
			_ = json.Unmarshal(b, &nerEntities)
		}
	}

	textLower := strings.ToLower(input.Text)
	var matchedIDs []int64
	for _, entity := range entities {
		candidates := append([]string{entity.Name}, entity.Aliases...)
		for _, c := range candidates {
			cLower := strings.ToLower(c)
			matched := false
			for _, ne := range nerEntities {
				if strings.ToLower(ne.Word) == cLower {
					matched = true
					break
				}
			}
			if !matched {
				matched = strings.Contains(textLower, cLower)
			}
			if matched {
				matchedIDs = append(matchedIDs, entity.ID)
				break
			}
		}
	}

	return map[string]any{
		"matched":     len(matchedIDs) > 0,
		"matched_ids": matchedIDs,
	}, nil
}
