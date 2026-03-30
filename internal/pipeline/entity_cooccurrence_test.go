package pipeline

import (
	"context"
	"testing"

	"github.com/amir-the-h/iran-watcher/internal/inference"
)

type mockCooccurrenceStore struct {
	upserted [][2]int64
}

func (m *mockCooccurrenceStore) UpsertEntityConnection(_ context.Context, fromID, toID int64) error {
	if fromID > toID {
		fromID, toID = toID, fromID
	}
	m.upserted = append(m.upserted, [2]int64{fromID, toID})
	return nil
}

func newCooccurrencePipeline(st *mockCooccurrenceStore) *EntityCooccurrencePipeline {
	return &EntityCooccurrencePipeline{name: "test-cooccurrence", store: st}
}

func TestEntityCooccurrence_NoPairsWhenFewerThanTwo(t *testing.T) {
	st := &mockCooccurrenceStore{}
	p := newCooccurrencePipeline(st)

	// Zero IDs
	out, err := p.Run(context.Background(), inference.PipelineInput{
		Context: map[string]any{"matched_ids": []int64{}},
	})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if out["pairs_updated"].(int) != 0 {
		t.Errorf("want pairs_updated=0, got %v", out["pairs_updated"])
	}
	if len(st.upserted) != 0 {
		t.Errorf("want 0 upserts, got %d", len(st.upserted))
	}

	// One ID
	out, err = p.Run(context.Background(), inference.PipelineInput{
		Context: map[string]any{"matched_ids": []int64{1}},
	})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if out["pairs_updated"].(int) != 0 {
		t.Errorf("want pairs_updated=0 for single id, got %v", out["pairs_updated"])
	}
}

func TestEntityCooccurrence_PairsThreeEntities(t *testing.T) {
	st := &mockCooccurrenceStore{}
	p := newCooccurrencePipeline(st)

	out, err := p.Run(context.Background(), inference.PipelineInput{
		Context: map[string]any{"matched_ids": []int64{1, 2, 3}},
	})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	// C(3,2) = 3 pairs
	if out["pairs_updated"].(int) != 3 {
		t.Errorf("want pairs_updated=3, got %v", out["pairs_updated"])
	}
	if len(st.upserted) != 3 {
		t.Errorf("want 3 upserts, got %d", len(st.upserted))
	}
}

func TestEntityCooccurrence_DeduplicatesPairs(t *testing.T) {
	st := &mockCooccurrenceStore{}
	p := newCooccurrencePipeline(st)

	// Two IDs produce exactly one pair.
	out, _ := p.Run(context.Background(), inference.PipelineInput{
		Context: map[string]any{"matched_ids": []int64{5, 3}},
	})
	if out["pairs_updated"].(int) != 1 {
		t.Errorf("want 1 pair, got %v", out["pairs_updated"])
	}
	// Verify normalized order: from < to
	if st.upserted[0] != [2]int64{3, 5} {
		t.Errorf("want normalized pair [3,5], got %v", st.upserted[0])
	}
}

func TestEntityCooccurrence_MissingContextReturnsZero(t *testing.T) {
	st := &mockCooccurrenceStore{}
	p := newCooccurrencePipeline(st)

	out, err := p.Run(context.Background(), inference.PipelineInput{
		Context: map[string]any{},
	})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if out["pairs_updated"].(int) != 0 {
		t.Errorf("want 0, got %v", out["pairs_updated"])
	}
}

func TestEntityCooccurrence_FloatIDsFromRedis(t *testing.T) {
	st := &mockCooccurrenceStore{}
	p := newCooccurrencePipeline(st)

	// Simulate matched_ids after JSON round-trip through Redis (numbers become float64).
	out, err := p.Run(context.Background(), inference.PipelineInput{
		Context: map[string]any{"matched_ids": []any{float64(1), float64(2), float64(3)}},
	})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if out["pairs_updated"].(int) != 3 {
		t.Errorf("want pairs_updated=3, got %v", out["pairs_updated"])
	}
	if len(st.upserted) != 3 {
		t.Errorf("want 3 upserts, got %d", len(st.upserted))
	}
}
