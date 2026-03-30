package hub_test

import (
	"context"
	"encoding/json"
	"log/slog"
	"testing"
	"time"

	"github.com/amir-the-h/iran-watcher/internal/hub"
)

func TestHub_BroadcastReachesAllClients(t *testing.T) {
	h := hub.New(slog.Default())
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go h.Run(ctx)

	ch1 := make(chan []byte, 8)
	ch2 := make(chan []byte, 8)
	unreg1 := h.RegisterTestSink(ch1)
	unreg2 := h.RegisterTestSink(ch2)
	defer unreg1()
	defer unreg2()

	h.Broadcast(hub.Event{Type: "test", Payload: "hello"})

	for _, ch := range []chan []byte{ch1, ch2} {
		select {
		case msg := <-ch:
			var got hub.Event
			if err := json.Unmarshal(msg, &got); err != nil {
				t.Fatalf("unmarshal: %v", err)
			}
			if got.Type != "test" {
				t.Errorf("want type=test, got %q", got.Type)
			}
		case <-time.After(time.Second):
			t.Fatal("timed out waiting for broadcast")
		}
	}
}

func TestHub_UnregisteredClientReceivesNothing(t *testing.T) {
	h := hub.New(slog.Default())
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go h.Run(ctx)

	ch := make(chan []byte, 8)
	unreg := h.RegisterTestSink(ch)
	unreg()

	h.Broadcast(hub.Event{Type: "test", Payload: nil})

	select {
	case <-ch:
		t.Fatal("unregistered client should not receive broadcast")
	case <-time.After(100 * time.Millisecond):
		// expected: no message
	}
}

func TestHub_SlowClientIsDropped(t *testing.T) {
	h := hub.New(slog.Default())
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go h.Run(ctx)

	// Full channel: no room to receive — client should be dropped
	slowCh := make(chan []byte) // unbuffered, will always block
	_ = h.RegisterTestSink(slowCh)

	// Should not block even though slowCh can't receive
	done := make(chan struct{})
	go func() {
		h.Broadcast(hub.Event{Type: "test", Payload: nil})
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("Broadcast blocked on slow client")
	}
}
