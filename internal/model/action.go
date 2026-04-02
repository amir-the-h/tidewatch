package model

import (
	"encoding/json"
	"time"
)

// ActionType identifies the execution backend for an action.
type ActionType string

const (
	ActionTypeONNX         ActionType = "onnx"
	ActionTypeOllama       ActionType = "ollama"
	ActionTypeInternal     ActionType = "internal"
	ActionTypeNotification ActionType = "notification"
)

// Action is a named, reusable execution unit.
type Action struct {
	ID        int64           `json:"id"`
	Name      string          `json:"name"`
	Type      ActionType      `json:"type"`
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

// MapConfig is embedded in an action's Config JSON under the key "map_config".
// If CoordinateKey is empty the action produces no map pins.
type MapConfig struct {
	CoordinateKey  string         `json:"coordinate_key"`
	CoordinateType CoordinateType `json:"coordinate_type"`
}

type actionConfigWithMap struct {
	MapConfig *MapConfig `json:"map_config"`
}

// ParseMapConfig extracts the MapConfig from an action config blob.
// Returns nil if absent, invalid JSON, or CoordinateKey is empty.
func ParseMapConfig(config json.RawMessage) *MapConfig {
	if len(config) == 0 {
		return nil
	}
	var c actionConfigWithMap
	if err := json.Unmarshal(config, &c); err != nil || c.MapConfig == nil {
		return nil
	}
	if c.MapConfig.CoordinateKey == "" {
		return nil
	}
	return c.MapConfig
}

// ExecutionStatus values for ActionExecution.
const (
	ExecutionStatusSuccess = "success"
	ExecutionStatusError   = "error"
)

// ActionExecution is one log entry per action fired on a tweet.
type ActionExecution struct {
	ID         int64             `json:"id"`
	ActionID   int64             `json:"action_id"`
	TweetID    string            `json:"tweet_id"`
	Status     string            `json:"status"`
	Error      string            `json:"error,omitempty"`
	ExecutedAt time.Time         `json:"executed_at"`
	Metadata   map[string]string `json:"metadata,omitempty"`
}

// TwitterCredentials stores the connected Twitter account's OAuth 2.0 tokens.
type TwitterCredentials struct {
	ID            int64     `json:"id"`
	AccessToken   string    `json:"-"`
	RefreshToken  string    `json:"-"`
	ExpiresAt     time.Time `json:"expires_at"`
	TwitterUserID string    `json:"twitter_user_id"`
	Username      string    `json:"username"`
	UpdatedAt     time.Time `json:"updated_at"`
}
