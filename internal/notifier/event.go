package notifier

import (
	"encoding/json"
	"time"

	"github.com/amir-the-h/iran-watcher/internal/model"
)

const (
	EventTweetMatched         = "tweet_matched"
	EventTweetCategorized     = "tweet_categorized"
	EventCategorizationFailed = "categorization_failed"
	EventStreamConnected      = "stream_connected"
	EventStreamDisconnected   = "stream_disconnected"
	EventStreamError          = "stream_error"
)

// ValidEventTypes is the set of all valid event type values.
var ValidEventTypes = map[string]bool{
	EventTweetMatched:         true,
	EventTweetCategorized:     true,
	EventCategorizationFailed: true,
	EventStreamConnected:      true,
	EventStreamDisconnected:   true,
	EventStreamError:          true,
}

// NotificationEvent is the data emitted by components for the dispatcher.
type NotificationEvent struct {
	Type      string
	Tweet     *model.Tweet
	RuleIDs   []int64
	ModelName string
	Results   json.RawMessage
	Error     string
	Timestamp time.Time
}

// EventEmitter is the interface accepted by stream.Client and worker.Pool.
// Implementations must be safe for concurrent use. A nil EventEmitter is
// valid — callers should check for nil before calling Emit.
type EventEmitter interface {
	Emit(event NotificationEvent)
}
