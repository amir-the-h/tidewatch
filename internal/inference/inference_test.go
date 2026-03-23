package inference

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/amir-the-h/iran-watcher/internal/model"
)

// stubPipeline is a test double implementing Pipeline with a configurable name.
type stubPipeline struct{ name string }

func (s *stubPipeline) Name() string { return s.name }
func (s *stubPipeline) Load(_ string) error { return nil }
func (s *stubPipeline) Run(_ []byte) (json.RawMessage, error) {
	return json.RawMessage(`{}`), nil
}
func (s *stubPipeline) RunBatch(texts [][]byte) ([]json.RawMessage, error) {
	out := make([]json.RawMessage, len(texts))
	for i := range texts {
		out[i] = json.RawMessage(`{}`)
	}
	return out, nil
}
func (s *stubPipeline) Close() error { return nil }

// stubFactory returns a factory function that creates a stubPipeline per model.
func stubFactory(m *model.Model) (Pipeline, error) {
	return &stubPipeline{name: m.Name}, nil
}

// TestRegistry_EmptyBeforeLoad verifies that a fresh registry has no active pipelines.
func TestRegistry_EmptyBeforeLoad(t *testing.T) {
	r := NewRegistry()
	got := r.All()
	if len(got) != 0 {
		t.Fatalf("expected empty slice, got %d pipelines", len(got))
	}
}

// TestRegistry_Load_TwoPipelines verifies that Load creates the correct pipelines.
func TestRegistry_Load_TwoPipelines(t *testing.T) {
	r := NewRegistry()
	models := []*model.Model{
		{Name: "model-a", Path: "/tmp/a.onnx"},
		{Name: "model-b", Path: "/tmp/b.onnx"},
	}
	if err := r.Load(context.Background(), models, stubFactory); err != nil {
		t.Fatalf("Load returned error: %v", err)
	}

	got := r.All()
	if len(got) != 2 {
		t.Fatalf("expected 2 pipelines, got %d", len(got))
	}

	names := map[string]bool{}
	for _, p := range got {
		names[p.Name()] = true
	}
	for _, m := range models {
		if !names[m.Name] {
			t.Errorf("expected pipeline %q not found", m.Name)
		}
	}
}

// TestRegistry_Reload_ReplacesPipelines verifies that Reload replaces all active pipelines.
func TestRegistry_Reload_ReplacesPipelines(t *testing.T) {
	r := NewRegistry()
	initial := []*model.Model{
		{Name: "model-a", Path: "/tmp/a.onnx"},
		{Name: "model-b", Path: "/tmp/b.onnx"},
	}
	if err := r.Load(context.Background(), initial, stubFactory); err != nil {
		t.Fatalf("Load returned error: %v", err)
	}

	updated := []*model.Model{
		{Name: "model-c", Path: "/tmp/c.onnx"},
	}
	if err := r.Reload(context.Background(), updated, stubFactory); err != nil {
		t.Fatalf("Reload returned error: %v", err)
	}

	got := r.All()
	if len(got) != 1 {
		t.Fatalf("expected 1 pipeline after reload, got %d", len(got))
	}
	if got[0].Name() != "model-c" {
		t.Errorf("expected pipeline %q, got %q", "model-c", got[0].Name())
	}
}

// TestRegistry_NotifyChan_SignalAfterReload verifies that Reload sends a signal.
func TestRegistry_NotifyChan_SignalAfterReload(t *testing.T) {
	r := NewRegistry()
	models := []*model.Model{
		{Name: "model-a", Path: "/tmp/a.onnx"},
	}
	if err := r.Reload(context.Background(), models, stubFactory); err != nil {
		t.Fatalf("Reload returned error: %v", err)
	}

	select {
	case <-r.NotifyChan():
		// signal received — expected
	default:
		t.Fatal("expected signal on NotifyChan after Reload, but channel was empty")
	}
}

// TestRegistry_NotifyChan_NoSignalAfterLoad verifies that Load does NOT send a signal.
func TestRegistry_NotifyChan_NoSignalAfterLoad(t *testing.T) {
	r := NewRegistry()
	models := []*model.Model{
		{Name: "model-a", Path: "/tmp/a.onnx"},
	}
	if err := r.Load(context.Background(), models, stubFactory); err != nil {
		t.Fatalf("Load returned error: %v", err)
	}

	select {
	case <-r.NotifyChan():
		t.Fatal("unexpected signal on NotifyChan after Load (not Reload)")
	default:
		// no signal — expected
	}
}
