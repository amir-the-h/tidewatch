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
	QueueTasksPrefix = "tidewatch:tasks:"

	// QueueFailed is the dead-letter queue key.
	QueueFailed = "tidewatch:failed"
)

// QueueTasksKey returns the Redis key for the task queue of the given action.
func QueueTasksKey(actionID int64) string {
	return QueueTasksPrefix + strconv.FormatInt(actionID, 10)
}

// Task is a single unit of work pushed to tidewatch:tasks:{action_id}.
type Task struct {
	TriggerType    string          `json:"trigger_type"`
	Event          json.RawMessage `json:"event"`
	ActionID       int64           `json:"action_id"`
	PipelineNodeID int64           `json:"pipeline_node_id,omitempty"`
	PipelineID     int64           `json:"pipeline_id,omitempty"`
	// RootPipelineID is the top-level pipeline that originally dispatched this
	// task (set at PushInitialTasksForGraph time, propagated through all sub-tasks).
	// Non-zero tasks contribute to pipeline_run_tracking for pipeline_finished detection.
	RootPipelineID       int64  `json:"root_pipeline_id,omitempty"`
	ParentPipelineNodeID *int64 `json:"parent_pipeline_node_id,omitempty"`
	// SubPipelineEntryNodeID is the node ID of the INITIAL action dispatched
	// into a sub-pipeline. It is used as the leaf-tracking key in
	// pipeline_node_executions so that any termination of that branch
	// (structural or via condition-fail) can signal the parent pipeline.
	SubPipelineEntryNodeID *int64         `json:"sub_pipeline_entry_node_id,omitempty"`
	Context                map[string]any `json:"context"`
}

// SignalEvent is the Event payload for trigger_type=new_signal. It carries no
// text: the worker loads the signal by ID when it builds the task context, so
// Redis payloads stay small regardless of article length.
type SignalEvent struct {
	SignalID   int64  `json:"signal_id"`
	Kind       string `json:"kind"`
	SourceID   int64  `json:"source_id,omitempty"`   // the source whose ingest dispatched it (phase 1 step 2)
	SourceType string `json:"source_type,omitempty"` // e.g. x_stream, x_search
}

// DigestEvent is the Event payload for trigger_type=digest_requested.
type DigestEvent struct {
	DigestJobID string `json:"digest_job_id"`
}

// FailedTask wraps a Task with error annotations for dead-letter storage.
type FailedTask struct {
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
	PushFailed(ctx context.Context, ft *FailedTask) error
}

// Consumer pops tasks from a queue.
// PopTask accepts one or more queue keys and performs a BRPOP across all of
// them. Returns (nil, nil) on timeout or when keys is empty.
type Consumer interface {
	PopTask(ctx context.Context, timeout time.Duration, keys ...string) (*Task, error)
	PopFailed(ctx context.Context, timeout time.Duration) (*FailedTask, error)
}

// Sizer reports the number of items in a named queue.
type Sizer interface {
	Len(ctx context.Context, queue string) (int64, error)
}

// FailureLister enumerates and clears dead-letter queue entries.
type FailureLister interface {
	ListFailed(ctx context.Context, offset, limit int64) ([]*FailedTask, error)
	ClearFailed(ctx context.Context) error
	FlushQueue(ctx context.Context) error
}
