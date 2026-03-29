package notifier

import (
	"context"
	"log/slog"

	"github.com/amir-the-h/iran-watcher/internal/store"
)

const channelBufferSize = 256

// Dispatcher listens for NotificationEvents and logs them.
// Tweet-level notifications are delivered through the NotificationPipeline
// action system; the Dispatcher exists to satisfy the EventEmitter interface
// used by stream.Client, worker.Pool, and the API server.
type Dispatcher struct {
	ch     chan NotificationEvent
	store  store.Store
	logger *slog.Logger
}

// NewDispatcher creates a new Dispatcher.
func NewDispatcher(s store.Store, logger *slog.Logger) *Dispatcher {
	return &Dispatcher{
		ch:     make(chan NotificationEvent, channelBufferSize),
		store:  s,
		logger: logger,
	}
}

// Emit sends an event to the dispatcher channel. It is non-blocking: if the
// channel is full the event is dropped and a warning is logged. Safe for
// concurrent use.
func (d *Dispatcher) Emit(event NotificationEvent) {
	select {
	case d.ch <- event:
	default:
		d.logger.Warn("notifier: dispatcher: channel full, dropping event",
			"event_type", event.Type)
	}
}

// Start runs the dispatch loop until ctx is cancelled, then drains remaining
// events before returning.
func (d *Dispatcher) Start(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			d.drain()
			return
		case event := <-d.ch:
			d.handle(event)
		}
	}
}

// drain processes all events currently buffered in the channel.
func (d *Dispatcher) drain() {
	for {
		select {
		case event := <-d.ch:
			d.handle(event)
		default:
			return
		}
	}
}

// handle logs the event. Tweet-level notifications are delivered via the
// NotificationPipeline action system and do not need handling here.
func (d *Dispatcher) handle(event NotificationEvent) {
	d.logger.Debug("notifier: dispatcher: event received", "event_type", event.Type)
}
