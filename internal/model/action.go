package model

import "time"

// TriggerType is the event that initiates an action.
type TriggerType string

const (
	TriggerTypeNewTweet          TriggerType = "new_tweet"
	TriggerTypeNewCategorization TriggerType = "new_categorization"
	TriggerTypeEntityMatched     TriggerType = "entity_matched"
	TriggerTypeChainOnly         TriggerType = "chain_only"
	TriggerTypeDigestRequested   TriggerType = "digest_requested"
)

// Action subscribes a pipeline to a trigger.
type Action struct {
	ID          int64       `json:"id"`
	Name        string      `json:"name"`
	TriggerType TriggerType `json:"trigger_type"`
	PipelineID  int64       `json:"pipeline_id"`
	Active      bool        `json:"active"`
	RuleIDs     []int64     `json:"rule_ids"`
	CreatedAt   time.Time   `json:"created_at"`
	UpdatedAt   time.Time   `json:"updated_at"`
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
