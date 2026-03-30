package pipeline

import (
	"context"
	"testing"

	"github.com/amir-the-h/iran-watcher/internal/inference"
	"github.com/amir-the-h/iran-watcher/internal/model"
)

// mockDiscoveryStore records calls and controls return values for tests.
type mockDiscoveryStore struct {
	upserted  []*model.EntityCandidate
	promoted  []int64
	upsertErr error
}

func (m *mockDiscoveryStore) UpsertEntityCandidate(_ context.Context, c *model.EntityCandidate) error {
	if m.upsertErr != nil {
		return m.upsertErr
	}
	c.ID = int64(len(m.upserted) + 1)
	c.Status = "pending"
	c.OccurrenceCount = 1
	m.upserted = append(m.upserted, c)
	return nil
}

func (m *mockDiscoveryStore) UpdateEntityCandidateStatus(_ context.Context, id int64, status string, _ *int64, _ bool) error {
	m.promoted = append(m.promoted, id)
	return nil
}

func (m *mockDiscoveryStore) SaveTrackedEntity(_ context.Context, e *model.TrackedEntity) error {
	e.ID = 99
	return nil
}

func TestEntityDiscovery_NoEntitiesInContext(t *testing.T) {
	st := &mockDiscoveryStore{}
	p := newEntityDiscoveryPipeline("disc", entityDiscoveryConfig{
		MinConfidence: 0.7,
		MinFrequency:  2,
		EntityKey:     "entities",
	}, st)

	out, err := p.Run(context.Background(), inference.PipelineInput{
		Context: map[string]any{},
	})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if out["discovered"].(int) != 0 {
		t.Errorf("discovered: got %v, want 0", out["discovered"])
	}
	if len(st.upserted) != 0 {
		t.Errorf("expected no upserts, got %d", len(st.upserted))
	}
}

func TestEntityDiscovery_BelowThreshold_StaysPending(t *testing.T) {
	st := &mockDiscoveryStore{}
	p := newEntityDiscoveryPipeline("disc", entityDiscoveryConfig{
		MinConfidence:    0.7,
		MinFrequency:     3,
		AutoPromoteTypes: []string{"LOC"},
		EntityKey:        "entities",
	}, st)

	out, err := p.Run(context.Background(), inference.PipelineInput{
		Context: map[string]any{
			"entities": []map[string]any{
				{"word": "Tehran", "label": "LOC", "score": 0.8},
			},
		},
	})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if out["discovered"].(int) != 1 {
		t.Errorf("discovered: got %v, want 1", out["discovered"])
	}
	if out["auto_promoted"].(int) != 0 {
		t.Errorf("auto_promoted: got %v, want 0", out["auto_promoted"])
	}
	// OccurrenceCount is 1 after first upsert, frequency threshold is 3 → not promoted
	if len(st.promoted) != 0 {
		t.Errorf("expected no promotions, got %d", len(st.promoted))
	}
}

func TestEntityDiscovery_AboveThreshold_AutoPromotes(t *testing.T) {
	st := &mockDiscoveryStore{}
	p := newEntityDiscoveryPipeline("disc", entityDiscoveryConfig{
		MinConfidence:    0.7,
		MinFrequency:     1, // frequency = 1 so first upsert qualifies
		AutoPromoteTypes: []string{"LOC"},
		EntityKey:        "entities",
	}, st)

	out, err := p.Run(context.Background(), inference.PipelineInput{
		Context: map[string]any{
			"entities": []map[string]any{
				{"word": "Tehran", "label": "LOC", "score": 0.9},
			},
		},
	})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if out["auto_promoted"].(int) != 1 {
		t.Errorf("auto_promoted: got %v, want 1", out["auto_promoted"])
	}
	if len(st.promoted) != 1 {
		t.Errorf("expected 1 promotion call, got %d", len(st.promoted))
	}
}

func TestEntityDiscovery_TypeNotInAutoPromoteList_StaysPending(t *testing.T) {
	st := &mockDiscoveryStore{}
	p := newEntityDiscoveryPipeline("disc", entityDiscoveryConfig{
		MinConfidence:    0.5,
		MinFrequency:     1,
		AutoPromoteTypes: []string{"LOC"}, // PER not in list
		EntityKey:        "entities",
	}, st)

	out, err := p.Run(context.Background(), inference.PipelineInput{
		Context: map[string]any{
			"entities": []map[string]any{
				{"word": "Khamenei", "label": "PER", "score": 0.95},
			},
		},
	})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if out["auto_promoted"].(int) != 0 {
		t.Errorf("auto_promoted: got %v, want 0", out["auto_promoted"])
	}
}

func TestEntityDiscovery_EmptyAutoPromoteTypes_NeverAutoPromotes(t *testing.T) {
	st := &mockDiscoveryStore{}
	p := newEntityDiscoveryPipeline("disc", entityDiscoveryConfig{
		MinConfidence:    0.5,
		MinFrequency:     1,
		AutoPromoteTypes: []string{}, // empty = nothing auto-promotes
		EntityKey:        "entities",
	}, st)

	out, err := p.Run(context.Background(), inference.PipelineInput{
		Context: map[string]any{
			"entities": []map[string]any{
				{"word": "Tehran", "label": "LOC", "score": 0.99},
			},
		},
	})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if out["auto_promoted"].(int) != 0 {
		t.Errorf("auto_promoted: got %v, want 0", out["auto_promoted"])
	}
}

func TestEntityDiscovery_CustomEntityKey(t *testing.T) {
	st := &mockDiscoveryStore{}
	p := newEntityDiscoveryPipeline("disc", entityDiscoveryConfig{
		MinConfidence: 0.5,
		MinFrequency:  1,
		EntityKey:     "my_ner_output",
	}, st)

	out, err := p.Run(context.Background(), inference.PipelineInput{
		Context: map[string]any{
			"my_ner_output": []map[string]any{
				{"word": "Tehran", "label": "LOC", "score": 0.8},
			},
		},
	})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if out["discovered"].(int) != 1 {
		t.Errorf("discovered: got %v, want 1", out["discovered"])
	}
}
