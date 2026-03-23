package inference

import (
	"encoding/json"
	"fmt"
)

// ONNXPipeline implements Pipeline using onnxruntime_go and daulet/tokenizers.
// Requires: ONNX Runtime shared library installed, CGO enabled, Rust toolchain.
type ONNXPipeline struct {
	name      string
	modelPath string
	// TODO: add onnxruntime_go session and daulet tokenizer fields when
	// system dependencies are available
}

// NewONNXPipeline creates a new ONNXPipeline with the given model name.
func NewONNXPipeline(name string) *ONNXPipeline {
	return &ONNXPipeline{name: name}
}

// Name returns the model name as registered in the models table.
func (p *ONNXPipeline) Name() string { return p.name }

// Load initializes the model from the given local .onnx file path.
func (p *ONNXPipeline) Load(modelPath string) error {
	p.modelPath = modelPath
	// TODO: initialize onnxruntime_go session from modelPath
	// TODO: load daulet/tokenizers tokenizer from modelPath directory
	return nil
}

// Run takes raw tweet text (UTF-8 bytes) and returns model-specific JSON output.
func (p *ONNXPipeline) Run(text []byte) (json.RawMessage, error) {
	if p.modelPath == "" {
		return nil, fmt.Errorf("inference: onnx: model not loaded")
	}
	// TODO: tokenize text using daulet/tokenizers
	// TODO: run onnxruntime_go session
	// TODO: post-process logits into label/score JSON
	// Stub: return a placeholder result
	return json.RawMessage(`{"label":"stub","score":1.0}`), nil
}

// RunBatch runs inference on multiple texts, looping over Run().
func (p *ONNXPipeline) RunBatch(texts [][]byte) ([]json.RawMessage, error) {
	results := make([]json.RawMessage, len(texts))
	for i, t := range texts {
		r, err := p.Run(t)
		if err != nil {
			return nil, err
		}
		results[i] = r
	}
	return results, nil
}

// Close releases ONNX Runtime resources.
func (p *ONNXPipeline) Close() error {
	// TODO: release onnxruntime_go session
	return nil
}
