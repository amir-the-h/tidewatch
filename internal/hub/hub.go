package hub

import (
	"context"
	"encoding/json"
	"log/slog"
)

const broadcastBuffer = 256

// Broadcaster is the narrow interface source packages use to push events.
// The concrete implementation is Hub.
type Broadcaster interface {
	Broadcast(Event)
}

// NopBroadcaster is a no-op Broadcaster for use in tests or when WS is disabled.
var NopBroadcaster Broadcaster = nopBroadcaster{}

type nopBroadcaster struct{}

func (nopBroadcaster) Broadcast(Event) {}

// Event is a typed JSON payload sent to all connected browser clients.
type Event struct {
	Type    string `json:"type"`
	Payload any    `json:"payload"`
}

// Client represents a connected WebSocket client. The conn field is populated
// by the WebSocket handler (client.go, Task 2). For test sinks conn is nil —
// fan-out only uses the send channel.
type Client struct {
	hub  *Hub
	send chan []byte
}

// Hub maintains connected clients and fans out broadcast events.
// All mutations go through channels — no external locking required.
type Hub struct {
	clients    map[*Client]bool
	broadcast  chan Event
	register   chan *Client
	unregister chan *Client
	logger     *slog.Logger
}

// New returns a Hub ready to Run.
func New(logger *slog.Logger) *Hub {
	return &Hub{
		clients:    make(map[*Client]bool),
		broadcast:  make(chan Event, broadcastBuffer),
		register:   make(chan *Client),
		unregister: make(chan *Client),
		logger:     logger,
	}
}

// Broadcast enqueues an event for fan-out. Non-blocking: drops if buffer full.
func (h *Hub) Broadcast(e Event) {
	select {
	case h.broadcast <- e:
	default:
		h.logger.Warn("hub: broadcast buffer full, dropping event", "type", e.Type)
	}
}

// Run is the hub event loop. Blocks until ctx is cancelled.
func (h *Hub) Run(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			return
		case c := <-h.register:
			h.clients[c] = true
		case c := <-h.unregister:
			if _, ok := h.clients[c]; ok {
				delete(h.clients, c)
				// send channel is closed by the write pump (client.go) when the
				// WebSocket connection is done. For test sinks the caller owns the
				// channel — do not close it here.
			}
		case event := <-h.broadcast:
			data, err := json.Marshal(event)
			if err != nil {
				h.logger.Error("hub: marshal event", "err", err)
				continue
			}
			for c := range h.clients {
				select {
				case c.send <- data:
				default:
					// Slow or dead client: evict immediately.
					// The write pump (client.go) is responsible for closing c.send;
					// we only remove the client from the active set here.
					delete(h.clients, c)
				}
			}
		}
	}
}

// RegisterTestSink wires a raw byte channel as a client for unit testing.
// Returns an unregister function. The connection field is nil — do not use for
// real WS writes. Safe for hub fan-out and drop tests only.
func (h *Hub) RegisterTestSink(ch chan []byte) func() {
	c := &Client{hub: h, send: ch}
	h.register <- c
	return func() { h.unregister <- c }
}
