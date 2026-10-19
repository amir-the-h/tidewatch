package inference

import (
	"context"
	"testing"
)

// stubPipeline is a test double implementing Pipeline with a configurable name.
type stubPipeline struct{ name string }

func (s *stubPipeline) Name() string       { return s.name }
func (s *stubPipeline) Type() PipelineType { return PipelineTypeInternal }
func (s *stubPipeline) Run(_ context.Context, _ PipelineInput) (map[string]any, error) {
	return map[string]any{}, nil
}
func (s *stubPipeline) Close() error { return nil }

// TestRegistry_EmptyBeforeLoad verifies that a fresh registry has no active pipelines.
func TestRegistry_EmptyBeforeLoad(t *testing.T) {
	r := NewRegistry()
	got := r.All()
	if len(got) != 0 {
		t.Fatalf("expected empty slice, got %d pipelines", len(got))
	}
}

// TestRegistry_Register_TwoPipelines verifies that Register adds pipelines correctly.
func TestRegistry_Register_TwoPipelines(t *testing.T) {
	r := NewRegistry()
	r.Register(&stubPipeline{name: "model-a"})
	r.Register(&stubPipeline{name: "model-b"})

	got := r.All()
	if len(got) != 2 {
		t.Fatalf("expected 2 pipelines, got %d", len(got))
	}

	names := map[string]bool{}
	for _, p := range got {
		names[p.Name()] = true
	}
	for _, n := range []string{"model-a", "model-b"} {
		if !names[n] {
			t.Errorf("expected pipeline %q not found", n)
		}
	}
}

// TestRegistry_Register_ReplacesByName verifies that registering the same name replaces the pipeline.
func TestRegistry_Register_ReplacesByName(t *testing.T) {
	r := NewRegistry()
	r.Register(&stubPipeline{name: "model-a"})
	r.Register(&stubPipeline{name: "model-b"})
	// Replace model-a with a new instance.
	r.Register(&stubPipeline{name: "model-a"})

	got := r.All()
	if len(got) != 2 {
		t.Fatalf("expected 2 pipelines after replace, got %d", len(got))
	}
}

// TestRegistry_Get verifies that Get returns the correct pipeline by name.
func TestRegistry_Get(t *testing.T) {
	r := NewRegistry()
	r.Register(&stubPipeline{name: "model-a"})
	r.Register(&stubPipeline{name: "model-b"})

	p := r.Get("model-a")
	if p == nil {
		t.Fatal("expected pipeline, got nil")
	}
	if p.Name() != "model-a" {
		t.Errorf("expected 'model-a', got %q", p.Name())
	}

	missing := r.Get("does-not-exist")
	if missing != nil {
		t.Errorf("expected nil for missing pipeline, got %v", missing)
	}
}
