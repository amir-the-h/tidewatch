package notifier

import (
	"context"
	"log/slog"
	"testing"
	"time"
)

func newTestDispatcher() *Dispatcher {
	return NewDispatcher(nil, slog.Default())
}

func TestDispatcher_EmitAndReceive(t *testing.T) {
	d := newTestDispatcher()
	ctx, cancel := context.WithCancel(context.Background())

	go d.Start(ctx)

	d.Emit(NotificationEvent{Type: EventSignalIngested, Timestamp: time.Now()})
	d.Emit(NotificationEvent{Type: EventDigestDone, Timestamp: time.Now()})

	// Give the dispatcher a moment to process, then stop.
	time.Sleep(20 * time.Millisecond)
	cancel()
}

func TestDispatcher_DropWhenFull(t *testing.T) {
	d := newTestDispatcher()
	// Fill beyond capacity without starting the loop.
	for i := 0; i < channelBufferSize+10; i++ {
		d.Emit(NotificationEvent{Type: EventSignalIngested})
	}
	if len(d.ch) != channelBufferSize {
		t.Errorf("expected channel at capacity %d, got %d", channelBufferSize, len(d.ch))
	}
}

func TestDispatcher_DrainOnCancel(t *testing.T) {
	d := newTestDispatcher()
	ctx, cancel := context.WithCancel(context.Background())

	// Queue some events before starting.
	for i := 0; i < 5; i++ {
		d.Emit(NotificationEvent{Type: EventDigestDone})
	}

	cancel()
	// Start should drain buffered events and return promptly.
	done := make(chan struct{})
	go func() {
		d.Start(ctx)
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("dispatcher did not drain within timeout")
	}
}

func TestDispatcher_SatisfiesEventEmitter(t *testing.T) {
	var _ EventEmitter = (*Dispatcher)(nil)
}
