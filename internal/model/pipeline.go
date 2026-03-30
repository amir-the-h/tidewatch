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

// CoordinateType identifies how coordinate data is encoded in action_execution_metadata.
type CoordinateType string

const (
	CoordinateTypeArrayOfPairs   CoordinateType = "array_of_pairs"
	CoordinateTypeSinglePair     CoordinateType = "single_pair"
	CoordinateTypeObject         CoordinateType = "object"
	CoordinateTypeArrayOfObjects CoordinateType = "array_of_objects"
	CoordinateTypeGeoJSONPoint   CoordinateType = "geojson_point"
)

// MapConfig is embedded in a pipeline's Config JSON under the key "map_config".
// If CoordinateKey is empty the pipeline produces no map pins.
type MapConfig struct {
	CoordinateKey  string         `json:"coordinate_key"`
	CoordinateType CoordinateType `json:"coordinate_type"`
}

// pipelineConfigWithMap is used only for extracting map_config from a pipeline config blob.
type pipelineConfigWithMap struct {
	MapConfig *MapConfig `json:"map_config"`
}

// ParseMapConfig extracts the MapConfig from a pipeline config blob.
// Returns nil if absent, invalid JSON, or CoordinateKey is empty.
func ParseMapConfig(config json.RawMessage) *MapConfig {
	if len(config) == 0 {
		return nil
	}
	var c pipelineConfigWithMap
	if err := json.Unmarshal(config, &c); err != nil || c.MapConfig == nil {
		return nil
	}
	if c.MapConfig.CoordinateKey == "" {
		return nil
	}
	return c.MapConfig
}
