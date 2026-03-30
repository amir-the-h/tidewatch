package hub

import (
	"encoding/json"
	"net/http"
	"time"

	"github.com/gorilla/websocket"
)

const (
	writeWait      = 10 * time.Second
	sendBufferSize = 64
)

var upgrader = websocket.Upgrader{
	ReadBufferSize:  1024,
	WriteBufferSize: 1024,
	// Origin check is delegated to the auth layer (JWT in query param).
	CheckOrigin: func(r *http.Request) bool { return true },
}

// ServeWS upgrades the HTTP connection and starts the client's read/write pumps.
// The caller must have already authenticated the request before calling ServeWS.
func ServeWS(h *Hub, w http.ResponseWriter, r *http.Request) {
	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		// Upgrade writes its own error response on failure.
		return
	}
	c := &Client{
		hub:  h,
		conn: conn,
		send: make(chan []byte, sendBufferSize),
		done: make(chan struct{}),
	}
	h.register <- c
	go c.writePump()
	go c.readPump()
}

// readPump reads frames from the browser. It handles application-level ping
// messages by enqueuing a pong into the send channel (so writePump sends it).
// On any read error or close, it unregisters the client.
func (c *Client) readPump() {
	defer func() {
		c.hub.unregister <- c
		c.conn.Close()
	}()
	for {
		_, msg, err := c.conn.ReadMessage()
		if err != nil {
			return
		}
		var env struct {
			Type string `json:"type"`
		}
		if json.Unmarshal(msg, &env) == nil && env.Type == "ping" {
			pong, _ := json.Marshal(Event{Type: "pong"})
			select {
			case c.send <- pong:
			default:
				// send buffer full: skip pong, client will detect stale connection
			}
		}
	}
}

// writePump drains the send channel and writes frames to the browser.
// A write deadline of writeWait is set before each frame to detect dead TCP
// connections that never sent a clean close (e.g. mobile radio dropped).
// When the hub evicts this client (slow-client case), it closes Client.done;
// writePump selects on done to exit and trigger readPump cleanup via conn.Close.
// When the send channel is closed (normal unregister), range exits naturally.
func (c *Client) writePump() {
	defer c.conn.Close()
	for {
		select {
		case msg, ok := <-c.send:
			if !ok {
				// Hub closed the channel (normal unregister path).
				return
			}
			c.conn.SetWriteDeadline(time.Now().Add(writeWait))
			if err := c.conn.WriteMessage(websocket.TextMessage, msg); err != nil {
				return
			}
		case <-c.done:
			// Hub evicted this client (slow-client path). Exit cleanly.
			return
		}
	}
}
