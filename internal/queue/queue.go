package queue

import (
	"context"
	"encoding/json"
	"time"
)

const (
	// QueueTasksKey is the global Redis sorted-set key for non-Ollama pending tasks.
	// Tasks are scored by Depth (lower = higher priority).
	QueueTasksKey = "iran-watcher:tasks"

	// QueueOllamaTasksKey is the global Redis sorted-set key for Ollama pending tasks.
	// Workers only pop from this key when backend capacity is available.
	QueueOllamaTasksKey = "iran-watcher:tasks:ollama"

	// QueueFailed is the dead-letter queue key.
	QueueFailed = "iran-watcher:failed"
)

// Task is a single unit of work pushed to a task queue.
type Task struct {
	TriggerType    string          `json:"trigger_type"`
	Event          json.RawMessage `json:"event"`
	ActionID       int64           `json:"action_id"`
	PipelineNodeID int64           `json:"pipeline_node_id,omitempty"`
	PipelineID     int64           `json:"pipeline_id,omitempty"`
	// RootPipelineID is the top-level pipeline that originally dispatched this
	// task (set at PushInitialTasksForGraph time, propagated through all sub-tasks).
	RootPipelineID       int64  `json:"root_pipeline_id,omitempty"`
	ParentPipelineNodeID *int64 `json:"parent_pipeline_node_id,omitempty"`
	// SubPipelineEntryNodeID is the node ID of the INITIAL action dispatched
	// into a sub-pipeline.
	SubPipelineEntryNodeID *int64 `json:"sub_pipeline_entry_node_id,omitempty"`
	// Depth is the number of action-hops from the pipeline's Start node.
	// Lower depth = higher priority.
	Depth int `json:"depth,omitempty"`
	// IsOllama marks tasks whose action uses an Ollama pipeline.
	// PushTask routes IsOllama=true tasks to QueueOllamaTasksKey.
	IsOllama bool           `json:"is_ollama,omitempty"`
	Context  map[string]any `json:"context"`
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
type Consumer interface {
	// PopTask blocks until a task is available on iran-watcher:tasks or the
	// timeout elapses. Returns (nil, nil) on timeout.
	PopTask(ctx context.Context, timeout time.Duration) (*Task, error)
	// TryPopTask is a non-blocking pop from iran-watcher:tasks.
	// Returns (nil, nil) if the queue is empty.
	TryPopTask(ctx context.Context) (*Task, error)
	// TryPopOllamaTask is a non-blocking pop from iran-watcher:tasks:ollama.
	// Returns (nil, nil) if the queue is empty.
	TryPopOllamaTask(ctx context.Context) (*Task, error)
	// PeekMinDepth returns the score (Depth) of the lowest-scored member in
	// the given key without consuming it. Returns (0, false) if the key is empty.
	PeekMinDepth(ctx context.Context, key string) (depth float64, ok bool)
	// PopFailed pops a FailedTweet from the dead-letter queue.
	// Returns (nil, nil) on timeout.
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
