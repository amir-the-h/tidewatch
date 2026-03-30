package hub

import (
	"context"
	"encoding/json"
	"log/slog"

	"github.com/gorilla/websocket"
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

// Client represents a connected WebSocket client. For test sinks conn is nil —
// fan-out only uses the send channel.
type Client struct {
	hub      *Hub
	conn     *websocket.Conn
	send     chan []byte
	done     chan struct{} // closed by hub on slow-client eviction; nil for test sinks
	testSink bool
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
				// Close done to signal writePump to exit. writePump is blocked
				// on c.send / c.done and won't exit otherwise. testSink clients
				// have no writePump, so don't close their done channel.
				if !c.testSink {
					close(c.done)
				}
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
					// Slow or dead client: evict from hub immediately.
					// Close done so writePump exits; it will close conn, causing readPump to exit.
					// testSink clients have no writePump, so don't close their done channel.
					delete(h.clients, c)
					if !c.testSink {
						close(c.done)
					}
				}
			}
		}
	}
}

// RegisterTestSink wires a raw byte channel as a client for unit testing.
// Returns an unregister function. The connection field is nil — do not use for
// real WS writes. Safe for hub fan-out and drop tests only.
func (h *Hub) RegisterTestSink(ch chan []byte) func() {
	c := &Client{hub: h, send: ch, testSink: true}
	h.register <- c
	return func() { h.unregister <- c }
}
