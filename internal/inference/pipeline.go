package inference

import (
	"context"
	"encoding/json"
	"io"
	"time"
)

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

// DigestPipeline is implemented by pipelines that can produce a single
// briefing/summary from a collection of pre-formatted tweet strings.
type DigestPipeline interface {
	Pipeline
	Digest(tweets []string) (string, error)
}

// ConcurrencyLimiter is optionally implemented by pipelines that want to cap
// how many workers process them simultaneously. The manager skips a pipeline's
// channel when activePipelines already equals MaxConcurrency(), preventing
// idle workers from piling up behind a serialised backend (e.g. Ollama).
type ConcurrencyLimiter interface {
	MaxConcurrency() int
}

// ChatMessage is a single turn in a multi-turn conversation.
type ChatMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

// ChatPipeline is implemented by pipelines that support streaming interactive
// chat over a fixed tweet context window. OllamaPipeline satisfies this interface.
type ChatPipeline interface {
	Pipeline
	// ChatStream sends a multi-turn conversation with tweet context to the model
	// and writes SSE-formatted chunks ("data: <json>\n\n") to w as they arrive,
	// ending with "data: [DONE]\n\n". The caller must set SSE response headers
	// before invoking this method.
	ChatStream(ctx context.Context, tweetContext []string, messages []ChatMessage, w io.Writer) error
	// EstimateTokens returns the approximate token count for the given texts and
	// the model's context-window limit.
	EstimateTokens(ctx context.Context, texts []string) (estimated int, contextLimit int, err error)
}

// BackendStat holds a snapshot of a single Ollama backend's runtime state.
type BackendStat struct {
	URL          string     `json:"url"`
	Healthy      bool       `json:"healthy"`
	InUse        bool       `json:"in_use"`
	Score        float64    `json:"score"`
	RequestCount int64      `json:"request_count"`
	LastActiveAt *time.Time `json:"last_active_at,omitempty"` // nil if never used
}

// BackendStatter is implemented by pipelines that manage a backend pool.
type BackendStatter interface {
	BackendStats() []BackendStat
}
