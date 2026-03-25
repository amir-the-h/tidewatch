package queue

import (
	"context"
	"time"

	"github.com/amir-the-h/iran-watcher/internal/model"
)

const (
	QueueRaw    = "iran-watcher:raw"
	QueueFailed = "iran-watcher:failed"
)

// FailedTweet wraps a StreamTweet with error annotations for dead-letter storage.
type FailedTweet struct {
	Tweet        *model.StreamTweet `json:"tweet"`
	FailedModels []FailedModel      `json:"failed_models"`
}

// FailedModel records which model failed and why.
type FailedModel struct {
	Name      string    `json:"name"`
	Error     string    `json:"error"`
	Timestamp time.Time `json:"timestamp"`
}

// Producer pushes tweets onto a queue.
type Producer interface {
	// Push serializes a StreamTweet to JSON and LPUSHes it onto the named queue.
	Push(ctx context.Context, queue string, tweet *model.StreamTweet) error
	// PushFailed pushes a FailedTweet to the dead-letter queue (QueueFailed).
	PushFailed(ctx context.Context, ft *FailedTweet) error
}

// Consumer pops tweets from a queue.
type Consumer interface {
	// Pop blocks until a StreamTweet is available or timeout elapses.
	// Returns (nil, nil) on timeout (no message available, not an error).
	Pop(ctx context.Context, queue string, timeout time.Duration) (*model.StreamTweet, error)
	// PopFailed pops from the dead-letter queue.
	PopFailed(ctx context.Context, timeout time.Duration) (*FailedTweet, error)
}

// Sizer can report the number of items currently in a named queue.
type Sizer interface {
	Len(ctx context.Context, queue string) (int64, error)
}
