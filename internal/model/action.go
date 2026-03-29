package model

import "time"

// Action subscribes a pipeline to a chain node.
type Action struct {
	ID         int64     `json:"id"`
	Name       string    `json:"name"`
	PipelineID int64     `json:"pipeline_id"`
	Active     bool      `json:"active"`
	CreatedAt  time.Time `json:"created_at"`
	UpdatedAt  time.Time `json:"updated_at"`
}

// ExecutionStatus values for ActionExecution.
const (
	ExecutionStatusSuccess = "success"
	ExecutionStatusError   = "error"
)

// ActionExecution is one log entry per action fired.
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
