// Package action defines the generic dispatch unit used by the worker manager.
// Every unit of work triggered by an incoming tweet is an Action — running an
// ONNX/Ollama pipeline, retweeting, liking, or quoting are all action types.
package action

import (
	"context"

	"github.com/amir-the-h/iran-watcher/internal/model"
)

// Action is the interface every dispatchable unit must satisfy.
type Action interface {
	// ID returns the persistent database ID of this action.
	ID() int64
	// Name returns the human-readable name used as the channel key.
	Name() string
	// Type returns the action's type.
	Type() model.ActionType
	// RuleIDs returns the rule gate; empty means "fire on all rules".
	RuleIDs() []int64
	// Execute runs the action for one tweet.
	Execute(ctx context.Context, tweet model.Tweet) error
}

// ConcurrencyLimiter is an optional interface implemented by actions that need
// to cap the number of concurrent executions (e.g. Ollama-backed pipelines).
type ConcurrencyLimiter interface {
	MaxConcurrency() int
}
