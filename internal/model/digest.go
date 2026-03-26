package model

import "time"

// DigestStatus represents the lifecycle state of a digest job.
type DigestStatus string

const (
	DigestStatusPending   DigestStatus = "pending"
	DigestStatusRunning   DigestStatus = "running"
	DigestStatusDone      DigestStatus = "done"
	DigestStatusError     DigestStatus = "error"
)

// DigestJob represents a background digest generation task.
type DigestJob struct {
	ID          string       `json:"id"`
	Status      DigestStatus `json:"status"`
	Params      DigestParams `json:"params"`
	Result      string       `json:"result,omitempty"`
	Error       string       `json:"error,omitempty"`
	TweetCount  int          `json:"tweet_count"`
	CreatedAt   time.Time    `json:"created_at"`
	CompletedAt *time.Time   `json:"completed_at,omitempty"`
}

// DigestParams holds the input parameters for a digest job.
type DigestParams struct {
	From     *time.Time `json:"from,omitempty"`
	To       *time.Time `json:"to,omitempty"`
	RuleID   *int64     `json:"rule_id,omitempty"`
	SourceID *int64     `json:"source_id,omitempty"`
	EntityID *int64     `json:"entity_id,omitempty"`
	Limit    int        `json:"limit"`
}
