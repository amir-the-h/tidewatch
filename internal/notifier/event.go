package notifier

import (
	"encoding/json"
	"time"

	"github.com/amir-the-h/tidewatch/internal/model"
)

const (
	EventSignalIngested     = "signal_ingested"
	EventStreamConnected    = "stream_connected"
	EventStreamDisconnected = "stream_disconnected"
	EventStreamError        = "stream_error"
	EventDigestDone         = "digest_done"
	EventDigestFailed       = "digest_failed"
)

// ValidEventTypes is the set of all valid event type values.
var ValidEventTypes = map[string]bool{
	EventSignalIngested:     true,
	EventStreamConnected:    true,
	EventStreamDisconnected: true,
	EventStreamError:        true,
	EventDigestDone:         true,
	EventDigestFailed:       true,
}

// NotificationEvent is the data emitted by components for the dispatcher.
type NotificationEvent struct {
	Type      string
	Signal    *model.Signal
	DigestJob *model.DigestJob
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
