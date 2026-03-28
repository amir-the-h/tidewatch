package queue

import (
	"context"
	"encoding/json"
	"time"
)

const (
	QueueTasks  = "iran-watcher:tasks"
	QueueFailed = "iran-watcher:failed"
	// QueueRaw is kept for backward-compat during transition but deprecated.
	QueueRaw = "iran-watcher:raw"
)

// Task is a single unit of work pushed to iran-watcher:tasks.
type Task struct {
	TriggerType string          `json:"trigger_type"`
	Event       json.RawMessage `json:"event"`    // serialised trigger payload
	ActionID    int64           `json:"action_id"`
	Context     map[string]any  `json:"context"` // accumulated outputs from upstream actions
}

// TweetEvent is the Event payload for trigger_type=new_tweet.
type TweetEvent struct {
	TweetID      string  `json:"tweet_id"`
	Text         string  `json:"text"`
	AuthorHandle string  `json:"author_handle"`
	RuleIDs      []int64 `json:"rule_ids"`
}

// FailedTweet wraps a Task with error annotations for dead-letter storage.
type FailedTweet struct {
	Task         *Task         `json:"task"`
	FailedModels []FailedModel `json:"failed_models"`
}

// FailedModel records which pipeline failed and why.
type FailedModel struct {
	Name      string    `json:"name"`
	Error     string    `json:"error"`
	Timestamp time.Time `json:"timestamp"`
}

// Producer pushes tasks onto a queue.
type Producer interface {
	PushTask(ctx context.Context, task *Task) error
	PushFailed(ctx context.Context, ft *FailedTweet) error
}

// Consumer pops tasks from a queue.
type Consumer interface {
	PopTask(ctx context.Context, timeout time.Duration) (*Task, error)
	PopFailed(ctx context.Context, timeout time.Duration) (*FailedTweet, error)
}

// Sizer reports the number of items in a named queue.
type Sizer interface {
	Len(ctx context.Context, queue string) (int64, error)
}

// FailureLister enumerates and clears dead-letter queue entries.
type FailureLister interface {
	ListFailed(ctx context.Context, offset, limit int64) ([]*FailedTweet, error)
	ClearFailed(ctx context.Context) error
	FlushQueue(ctx context.Context) error
}
