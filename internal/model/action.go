package model

import (
	"encoding/json"
	"time"
)

// ActionType identifies the kind of action to perform.
type ActionType string

const (
	ActionTypePipeline ActionType = "pipeline"
	ActionTypeRetweet  ActionType = "retweet"
	ActionTypeLike     ActionType = "like"
	ActionTypeQuote    ActionType = "quote"
)

// Action is a persistent record of a configured dispatch unit.
type Action struct {
	ID        int64           `json:"id"`
	Name      string          `json:"name"`
	Type      ActionType      `json:"type"`
	Active    bool            `json:"active"`
	RuleIDs   []int64         `json:"rule_ids"`
	Config    json.RawMessage `json:"config"`
	CreatedAt time.Time       `json:"created_at"`
}

// ExecutionStatus values for ActionExecution.
const (
	ExecutionStatusSuccess = "success"
	ExecutionStatusError   = "error"
)

// ActionExecution is one log entry per action fired on one tweet.
type ActionExecution struct {
	ID         int64             `json:"id"`
	ActionID   int64             `json:"action_id"`
	TweetID    string            `json:"tweet_id"`
	Status     string            `json:"status"` // success | error
	Error      string            `json:"error,omitempty"`
	ExecutedAt time.Time         `json:"executed_at"`
	Metadata   map[string]string `json:"metadata,omitempty"`
}

// TwitterCredentials stores the connected Twitter account's OAuth 2.0 tokens.
// Only one row is ever stored (enforced by PRIMARY KEY DEFAULT 1).
type TwitterCredentials struct {
	ID            int64     `json:"id"`
	AccessToken   string    `json:"-"` // never serialised to API responses
	RefreshToken  string    `json:"-"`
	ExpiresAt     time.Time `json:"expires_at"`
	TwitterUserID string    `json:"twitter_user_id"`
	Username      string    `json:"username"`
	UpdatedAt     time.Time `json:"updated_at"`
}
