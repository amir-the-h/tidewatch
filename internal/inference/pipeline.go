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

// LabeledPipeline is implemented by pipelines that support dynamic candidate labels
// (e.g. zero-shot classification). Labels can be updated without reloading the model.
type LabeledPipeline interface {
	Pipeline
	SetLabels(labels []string)
}

// SummarizationPipeline is implemented by seq2seq pipelines that generate text summaries.
// Workers use this interface to populate the summary field on entity tracks.
type SummarizationPipeline interface {
	Pipeline
	Summarize(text string) (string, error)
}

// ExtractionPipeline is implemented by pipelines that extract structured fields
// from tweet text (e.g. coordinates, URLs, attribution) into an ExtractionResult.
type ExtractionPipeline interface {
	Pipeline
	Extract(text string) (*ExtractionResult, error)
}
