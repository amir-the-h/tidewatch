package model

import "time"

// OllamaBackend is a stored Ollama server backend.
type OllamaBackend struct {
	ID        int64     `json:"id"`
	Name      string    `json:"name"`
	URL       string    `json:"url"`
	Enabled   bool      `json:"enabled"`
	CreatedAt time.Time `json:"created_at"`
}

// PipelineBackend is a priority-ordered assignment of a backend to a pipeline.
type PipelineBackend struct {
	PipelineID int64 `json:"pipeline_id"`
	BackendID  int64 `json:"backend_id"`
	Priority   int   `json:"priority"`
}
