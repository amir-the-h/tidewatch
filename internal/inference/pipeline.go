package inference

import "encoding/json"

// Pipeline is the interface every ONNX model integration must implement.
// Each Pipeline handles its own tokenization and pre/post-processing internally.
type Pipeline interface {
	// Name returns the model name as registered in the models table.
	Name() string
	// Load initializes the model from the given local .onnx file path.
	Load(modelPath string) error
	// Run takes raw tweet text (UTF-8 bytes) and returns model-specific JSON output.
	Run(text []byte) (json.RawMessage, error)
	// RunBatch runs inference on multiple texts. Default implementations loop over Run().
	RunBatch(texts [][]byte) ([]json.RawMessage, error)
	// Close releases ONNX Runtime resources.
	Close() error
}
