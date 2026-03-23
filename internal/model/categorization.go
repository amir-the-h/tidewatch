package model

import (
	"encoding/json"
	"time"
)

type Categorization struct {
	ID           int64           `json:"id"`
	TweetID      string          `json:"tweet_id"`
	ModelName    string          `json:"model_name"`
	ModelVersion string          `json:"model_version"`
	Results      json.RawMessage `json:"results"`
	CreatedAt    time.Time       `json:"created_at"`
}
