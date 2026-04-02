package queue

import (
	"context"
	"encoding/json"
	"strconv"
	"time"
)

const (
	// QueueTasksPrefix is the key prefix for per-action task queues.
	// Full key: QueueTasksPrefix + strconv.FormatInt(actionID, 10)
	QueueTasksPrefix = "iran-watcher:tasks:"

	// QueueFailed is the dead-letter queue key.
	QueueFailed = "iran-watcher:failed"

)

// QueueTasksKey returns the Redis key for the task queue of the given action.
func QueueTasksKey(actionID int64) string {
	return QueueTasksPrefix + strconv.FormatInt(actionID, 10)
}

// Task is a single unit of work pushed to iran-watcher:tasks:{action_id}.
type Task struct {
	TriggerType          string          `json:"trigger_type"`
	Event                json.RawMessage `json:"event"`
	ActionID             int64           `json:"action_id"`
	PipelineNodeID       int64           `json:"pipeline_node_id,omitempty"`
	PipelineID           int64           `json:"pipeline_id,omitempty"`
	ParentPipelineNodeID *int64          `json:"parent_pipeline_node_id,omitempty"`
	Context              map[string]any  `json:"context"`
}

// TweetEvent is the Event payload for trigger_type=new_tweet.
type TweetEvent struct {
	TweetID      string  `json:"tweet_id"`
	Text         string  `json:"text"`
	AuthorHandle string  `json:"author_handle"`
	RuleIDs      []int64 `json:"rule_ids"`
}

// DigestEvent is the Event payload for trigger_type=digest_requested.
type DigestEvent struct {
	DigestJobID string `json:"digest_job_id"`
}

// FailedTweet wraps a Task with error annotations for dead-letter storage.
type FailedTweet struct {
	Task         *Task         `json:"task"`
	FailedModels []FailedModel `json:"failed_models"`
}

// FailedModel records which action failed and why.
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
// PopTask accepts one or more queue keys and performs a BRPOP across all of
// them. Returns (nil, nil) on timeout or when keys is empty.
type Consumer interface {
	PopTask(ctx context.Context, timeout time.Duration, keys ...string) (*Task, error)
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
