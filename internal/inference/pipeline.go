package inference

import (
	"context"
	"io"
	"time"
)

// PipelineType mirrors model.PipelineType but lives in inference to avoid
// circular imports.
type PipelineType string

const (
	PipelineTypeONNX          PipelineType = "onnx"
	PipelineTypeOllama        PipelineType = "ollama"
	PipelineTypeInternal      PipelineType = "internal"
	PipelineTypeNotification  PipelineType = "notification"
)

// PipelineInput carries all data a pipeline needs to run.
type PipelineInput struct {
	// Text is the primary text to process (tweet body).
	Text string
	// Context is the accumulated flat map of outputs from upstream actions in a chain.
	Context map[string]any
	// Config is the pipeline-specific configuration from the DB pipeline row.
	Config map[string]any
}

// Pipeline is the interface every execution unit must implement.
type Pipeline interface {
	// Name returns the pipeline name as stored in the pipelines table.
	Name() string
	// Type returns the pipeline type (onnx, ollama, internal).
	Type() PipelineType
	// Run executes the pipeline and returns structured outputs.
	Run(ctx context.Context, input PipelineInput) (map[string]any, error)
	// Close releases any held resources.
	Close() error
}

// LabeledPipeline is implemented by zero-shot pipelines with dynamic labels.
type LabeledPipeline interface {
	Pipeline
	SetLabels(labels []string)
}

// SummarizationPipeline is implemented by seq2seq pipelines.
type SummarizationPipeline interface {
	Pipeline
	Summarize(ctx context.Context, text string) (string, error)
}

// ExtractionPipeline is implemented by geo-extraction pipelines.
type ExtractionPipeline interface {
	Pipeline
	Extract(ctx context.Context, text string) (*ExtractionResult, error)
}

// DigestPipeline is implemented by digest-capable pipelines.
type DigestPipeline interface {
	Pipeline
	Digest(ctx context.Context, tweets []string) (string, error)
}

// ConcurrencyLimiter is optionally implemented by pipelines that cap concurrency.
type ConcurrencyLimiter interface {
	MaxConcurrency() int
}

// ChatMessage is a single turn in a multi-turn conversation.
type ChatMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

// ChatPipeline is implemented by pipelines that support streaming chat.
type ChatPipeline interface {
	Pipeline
	ChatStream(ctx context.Context, tweetContext []string, messages []ChatMessage, w io.Writer) error
	EstimateTokens(ctx context.Context, texts []string) (estimated int, contextLimit int, err error)
}

// BackendStat is a runtime snapshot of one Ollama backend.
type BackendStat struct {
	ID           int64      `json:"id"`
	Name         string     `json:"name"`
	URL          string     `json:"url"`
	Enabled      bool       `json:"enabled"`
	Healthy      bool       `json:"healthy"`
	InUse        bool       `json:"in_use"`
	Score        float64    `json:"score"`
	RequestCount int64      `json:"request_count"`
	LastActiveAt *time.Time `json:"last_active_at,omitempty"`
}

// ExtractionResult is the structured output of the geo-event extraction pipeline.
type ExtractionResult struct {
	Description     string               `json:"description"`
	Locations       []ExtractionLocation `json:"locations"`
	GeolocatedBy    []string             `json:"geolocated_by"`
	GeolocationURLs []string             `json:"geolocation_urls"`
	ProofURLs       []string             `json:"proof_urls"`
	LocationURL     string               `json:"location_url"`
	SourceURLs      []string             `json:"source_urls"`
	Truncated       bool                 `json:"truncated,omitempty"`
}

// ExtractionLocation holds a single geographic location.
type ExtractionLocation struct {
	Label       string     `json:"label,omitempty"`
	Coordinates [2]float64 `json:"coordinates"`
	Place       string     `json:"place,omitempty"`
}
