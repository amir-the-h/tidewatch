package model

import (
	"encoding/json"
	"time"
)

// PipelineType identifies the execution backend.
type PipelineType string

const (
	PipelineTypeONNX          PipelineType = "onnx"
	PipelineTypeOllama        PipelineType = "ollama"
	PipelineTypeInternal      PipelineType = "internal"
	PipelineTypeNotification  PipelineType = "notification"
)

// Pipeline is a persistent record of a configured execution unit.
type Pipeline struct {
	ID        int64           `json:"id"`
	Name      string          `json:"name"`
	Type      PipelineType    `json:"type"`
	Config    json.RawMessage `json:"config"`
	Active    bool            `json:"active"`
	CreatedAt time.Time       `json:"created_at"`
	UpdatedAt time.Time       `json:"updated_at"`
}
