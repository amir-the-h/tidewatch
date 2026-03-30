package pipeline

import (
	"context"
	"fmt"
	"testing"

	"github.com/amir-the-h/iran-watcher/internal/inference"
	"github.com/amir-the-h/iran-watcher/internal/model"
)

// mockDiscoveryStore records calls and controls return values for tests.
type mockDiscoveryStore struct {
	candidates    map[string]*model.EntityCandidate // key: "word:type"
	promoted      []int64
	upsertErr     error
	saveEntityErr error
}

func (m *mockDiscoveryStore) UpsertEntityCandidate(_ context.Context, c *model.EntityCandidate) error {
	if m.upsertErr != nil {
		return m.upsertErr
	}
	if m.candidates == nil {
		m.candidates = make(map[string]*model.EntityCandidate)
	}
	key := c.Word + ":" + c.Type
	if existing, ok := m.candidates[key]; ok && existing.Status != "pending" {
		// Frozen — return existing values unchanged.
		c.ID = existing.ID
		c.Status = existing.Status
		c.OccurrenceCount = existing.OccurrenceCount
		c.AvgConfidence = existing.AvgConfidence
		return nil
	}
	if existing, ok := m.candidates[key]; ok {
		existing.OccurrenceCount++
		existing.AvgConfidence = (existing.AvgConfidence*float64(existing.OccurrenceCount-1) + c.AvgConfidence) / float64(existing.OccurrenceCount)
		c.ID = existing.ID
		c.Status = existing.Status
		c.OccurrenceCount = existing.OccurrenceCount
		c.AvgConfidence = existing.AvgConfidence
		return nil
	}
	c.ID = int64(len(m.candidates) + 1)
	c.Status = "pending"
	c.OccurrenceCount = 1
	clone := *c
	m.candidates[key] = &clone
	return nil
}

func (m *mockDiscoveryStore) UpdateEntityCandidateStatus(_ context.Context, id int64, status string, _ *int64, _ bool) error {
	for _, c := range m.candidates {
		if c.ID == id {
			c.Status = status
			break
		}
	}
	m.promoted = append(m.promoted, id)
	return nil
}

func (m *mockDiscoveryStore) SaveTrackedEntity(_ context.Context, e *model.TrackedEntity) error {
	if m.saveEntityErr != nil {
		return m.saveEntityErr
	}
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
	if len(st.candidates) != 0 {
		t.Errorf("expected no upserts, got %d", len(st.candidates))
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

func TestEntityDiscovery_UniqueConflict_LeavesCandidate_Pending(t *testing.T) {
	st := &mockDiscoveryStore{
		saveEntityErr: fmt.Errorf("UNIQUE constraint failed: tracked_entities.name"),
	}
	p := newEntityDiscoveryPipeline("disc", entityDiscoveryConfig{
		MinConfidence:    0.5,
		MinFrequency:     1,
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
		t.Fatalf("Run should not error on UNIQUE conflict: %v", err)
	}
	if out["auto_promoted"].(int) != 0 {
		t.Errorf("auto_promoted: got %v, want 0 (conflict = no promotion)", out["auto_promoted"])
	}
	if out["discovered"].(int) != 1 {
		t.Errorf("discovered: got %v, want 1", out["discovered"])
	}
}
