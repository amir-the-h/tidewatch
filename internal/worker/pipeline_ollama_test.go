package worker

import (
	"context"
	"testing"

	"github.com/amir-the-h/iran-watcher/internal/inference"
	"github.com/amir-the-h/iran-watcher/internal/model"
)

// mockOllamaPipeline satisfies inference.Pipeline with Type()=PipelineTypeOllama.
type mockOllamaPipeline struct{}

func (m *mockOllamaPipeline) Type() inference.PipelineType { return inference.PipelineTypeOllama }
func (m *mockOllamaPipeline) Name() string                 { return "ollama" }
func (m *mockOllamaPipeline) Run(_ context.Context, _ inference.PipelineInput) (map[string]any, error) {
	return nil, nil
}
func (m *mockOllamaPipeline) Close() error { return nil }

// mockOnnxPipeline satisfies inference.Pipeline with Type()=PipelineTypeONNX.
type mockOnnxPipeline struct{}

func (m *mockOnnxPipeline) Type() inference.PipelineType { return inference.PipelineTypeONNX }
func (m *mockOnnxPipeline) Name() string                 { return "onnx" }
func (m *mockOnnxPipeline) Run(_ context.Context, _ inference.PipelineInput) (map[string]any, error) {
	return nil, nil
}
func (m *mockOnnxPipeline) Close() error { return nil }

func buildCacheWithPipeline(actionID int64, p inference.Pipeline) *ActionCache {
	c := &ActionCache{entries: make(map[int64]*CacheEntry)}
	c.entries[actionID] = &CacheEntry{
		Action:   &model.Action{ID: actionID},
		Pipeline: p,
	}
	return c
}

func TestIsOllamaAction_ReturnsTrueForOllama(t *testing.T) {
	cache := buildCacheWithPipeline(1, &mockOllamaPipeline{})
	if !IsOllamaAction(cache, 1) {
		t.Error("expected IsOllamaAction=true for Ollama pipeline")
	}
}

func TestIsOllamaAction_ReturnsFalseForONNX(t *testing.T) {
	cache := buildCacheWithPipeline(2, &mockOnnxPipeline{})
	if IsOllamaAction(cache, 2) {
		t.Error("expected IsOllamaAction=false for ONNX pipeline")
	}
}

func TestIsOllamaAction_ReturnsFalseForNilCache(t *testing.T) {
	if IsOllamaAction(nil, 1) {
		t.Error("expected IsOllamaAction=false for nil cache")
	}
}

func TestIsOllamaAction_ReturnsFalseForUnknownAction(t *testing.T) {
	cache := buildCacheWithPipeline(1, &mockOllamaPipeline{})
	if IsOllamaAction(cache, 999) {
		t.Error("expected IsOllamaAction=false for unknown action_id")
	}
}
