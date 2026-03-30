# WebSocket Live Updates Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Replace all UI polling with event-driven WebSocket push so every entity type (operational stats and CRUD entities) stays live without intervals.

**Architecture:** A `Hub` goroutine fans out typed JSON events to all connected browser clients. Sources (Producer, Worker Manager, BackendPool, Stream Client, API mutation handlers) call `hub.Broadcast()` when their state changes. One persistent WS connection per browser session; Pinia stores update reactively on incoming events. No polling remains anywhere.

**Tech Stack:** `gorilla/websocket` (Go), native browser `WebSocket` API, Pinia (Vue 3), chi router (existing)

---

## File Map

| File | Action |
|---|---|
| `internal/hub/hub.go` | **Create** — Hub, Broadcaster interface, Event, New(), Run(), Broadcast(), RegisterTestSink() |
| `internal/hub/hub_test.go` | **Create** — fan-out, drop-slow-client, unregister tests |
| `internal/hub/client.go` | **Create** — Client, ServeWS(), readPump(), writePump() |
| `internal/api/handlers_ws.go` | **Create** — WS upgrade handler with JWT auth from query param |
| `internal/queue/redis.go` | **Modify** — add broadcaster field, WithBroadcaster(), broadcast in PushTask and PushFailed/ClearFailed |
| `internal/worker/manager.go` | **Modify** — add broadcaster field, WithBroadcaster(), broadcast worker_stats, failed_count, action_execution |
| `internal/inference/backend_pool.go` | **Modify** — add broadcaster field, WithBroadcaster(), broadcast ollama_backends after health probes |
| `internal/stream/client.go` | **Modify** — add broadcaster field, WithBroadcaster(), broadcast stream_status |
| `internal/api/server.go` | **Modify** — add hub field, WithHub(), /api/ws route (public), broadcast after every CRUD mutation |
| `cli/cmd/serve.go` | **Modify** — construct Hub, wire WithBroadcaster/WithHub, start Hub.Run goroutine |
| `web/nginx.conf` | **Modify** — add /api/ws location with WebSocket proxy headers |
| `web/src/stores/ws.ts` | **Create** — useWsStore: connection status |
| `web/src/stores/queues.ts` | **Create** — useQueuesStore: full queue entries (from HTTP), pending updates (from WS), worker stats |
| `web/src/stores/executions.ts` | **Create** — useExecutionsStore: recent action executions, prepend on WS push |
| `web/src/stores/ollama.ts` | **Create** — useOllamaStore: backends list |
| `web/src/stores/actions.ts` | **Modify** — add setAll(items) |
| `web/src/stores/rules.ts` | **Modify** — add setAll(items) |
| `web/src/stores/chains.ts` | **Modify** — add setAll(items) |
| `web/src/stores/sources.ts` | **Modify** — add setAll(items) |
| `web/src/stores/entities.ts` | **Modify** — add setAll(items) for tracked_entities |
| `web/src/stores/notifications.ts` | **Modify** — add setProviders(items) and setChannels(items) |
| `web/src/stores/models.ts` | **Modify** — add setAll(items) for pipelines |
| `web/src/stores/stream.ts` | **Modify** — add setConnected(v: boolean) |
| `web/src/stores/failures.ts` | **Modify** — add setTotal(n: number) |
| `web/src/composables/useWebSocket.ts` | **Create** — connect, disconnect, reconnect-with-backoff, ping, stale-detection, event routing |
| `web/src/views/QueueView.vue` | **Modify** — remove setInterval, use useQueuesStore for state |
| `web/src/views/OllamaView.vue` | **Modify** — remove setInterval, use useOllamaStore for state |
| `web/src/App.vue` | **Modify** — remove setInterval, connect/disconnect WS on auth state, use stores for header stats |

---

## Task 1: Hub Package — Hub Struct and Fan-out Logic

**Files:**
- Create: `internal/hub/hub.go`
- Create: `internal/hub/hub_test.go`

- [ ] **Step 1: Add gorilla/websocket dependency**

```bash
cd /mnt/storage/shared/projects/personal/iran-watcher
go get github.com/gorilla/websocket
```

Expected: `go.mod` and `go.sum` updated with `github.com/gorilla/websocket`.

- [ ] **Step 2: Write the failing test**

```go
// internal/hub/hub_test.go
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

	time.Sleep(10 * time.Millisecond) // let register complete

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
	time.Sleep(10 * time.Millisecond)
	unreg()
	time.Sleep(10 * time.Millisecond)

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
	time.Sleep(10 * time.Millisecond)

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
```

- [ ] **Step 3: Run test to verify it fails**

```bash
go test ./internal/hub/... -v
```

Expected: compile error — package `hub` does not exist.

- [ ] **Step 4: Implement hub.go**

```go
// internal/hub/hub.go
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
				close(c.send)
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
					delete(h.clients, c)
					close(c.send)
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
```

- [ ] **Step 5: Run tests to verify they pass**

```bash
go test ./internal/hub/... -v
```

Expected: all three tests PASS.

- [ ] **Step 6: Commit**

```bash
git add internal/hub/hub.go internal/hub/hub_test.go go.mod go.sum
git commit -m "feat: add hub package with broadcaster interface and fan-out logic"
```

---

## Task 2: Hub Client — WebSocket Read/Write Pumps

**Files:**
- Create: `internal/hub/client.go`

- [ ] **Step 1: Implement client.go**

```go
// internal/hub/client.go
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

// Client wraps a single WebSocket connection managed by the Hub.
type Client struct {
	hub  *Hub
	conn *websocket.Conn
	send chan []byte
}

// ServeWS upgrades the HTTP connection and starts the client's read/write pumps.
// The caller must have already authenticated the request before calling ServeWS.
func ServeWS(h *Hub, w http.ResponseWriter, r *http.Request) {
	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		// Upgrade writes its own error response on failure.
		return
	}
	c := &Client{hub: h, conn: conn, send: make(chan []byte, sendBufferSize)}
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
// When the hub closes the send channel (slow-client eviction), writePump exits.
func (c *Client) writePump() {
	defer c.conn.Close()
	for msg := range c.send {
		c.conn.SetWriteDeadline(time.Now().Add(writeWait))
		if err := c.conn.WriteMessage(websocket.TextMessage, msg); err != nil {
			return
		}
	}
}
```

- [ ] **Step 2: Verify it compiles**

```bash
go build ./internal/hub/...
```

Expected: no errors.

- [ ] **Step 3: Commit**

```bash
git add internal/hub/client.go
git commit -m "feat: add hub client with read/write pumps"
```

---

## Task 3: Wire Broadcaster into Producer (Queue)

**Files:**
- Modify: `internal/queue/redis.go`

- [ ] **Step 1: Read the current file**

Read `internal/queue/redis.go`. Note the exact field name of `q.client` and the key used in `PushTask` and `PushFailed`. Verify `PushTask` currently calls `.Err()` on the LPush result (not `.Result()`).

- [ ] **Step 2: Add the broadcaster field and WithBroadcaster method**

Find the `RedisQueue` struct definition. Add the `broadcaster` field:

```go
type RedisQueue struct {
    client     redisClient  // existing field name — verify in file
    broadcaster hub.Broadcaster
}
```

Add the import for `internal/hub` at the top:
```go
import (
    // existing imports ...
    "github.com/amir-the-h/iran-watcher/internal/hub"
)
```

Add the setter after the struct:
```go
// WithBroadcaster wires a broadcaster for live queue-depth updates.
// Must be called before the queue is used.
func (q *RedisQueue) WithBroadcaster(b hub.Broadcaster) {
    q.broadcaster = b
}
```

- [ ] **Step 3: Update PushTask to emit queue_update**

Find the `PushTask` method. Change the LPUSH call from `.Err()` to `.Result()` so we capture the new list length, then broadcast:

```go
// Before (find the exact line and replace):
//   return q.client.LPush(ctx, QueueTasksKey(task.ActionID), b).Err()

// After:
cmd := q.client.LPush(ctx, QueueTasksKey(task.ActionID), b)
if err := cmd.Err(); err != nil {
    return err
}
if q.broadcaster != nil {
    q.broadcaster.Broadcast(hub.Event{
        Type: "queue_update",
        Payload: map[string]int64{
            "action_id": task.ActionID,
            "pending":   cmd.Val(),
        },
    })
}
return nil
```

- [ ] **Step 4: Update PushFailed to emit failed_count**

Find the `PushFailed` method. Apply the same pattern:

```go
// Replace the LPush call in PushFailed:
cmd := q.client.LPush(ctx, failedKey, b)  // use whatever constant/variable the file uses for the failed queue key
if err := cmd.Err(); err != nil {
    return err
}
if q.broadcaster != nil {
    q.broadcaster.Broadcast(hub.Event{
        Type:    "failed_count",
        Payload: map[string]int64{"count": cmd.Val()},
    })
}
return nil
```

- [ ] **Step 5: Update ClearFailed to broadcast zero count**

Find the `ClearFailed` method. After the DEL succeeds, broadcast:

```go
if q.broadcaster != nil {
    q.broadcaster.Broadcast(hub.Event{
        Type:    "failed_count",
        Payload: map[string]int64{"count": 0},
    })
}
```

- [ ] **Step 6: Verify it compiles**

```bash
go build ./internal/queue/...
```

Expected: no errors.

- [ ] **Step 7: Commit**

```bash
git add internal/queue/redis.go
git commit -m "feat: broadcast queue_update and failed_count events from producer"
```

---

## Task 4: Wire Broadcaster into Worker Manager

**Files:**
- Modify: `internal/worker/manager.go`

- [ ] **Step 1: Read the current file**

Read `internal/worker/manager.go`. Find:
- Where a worker calls `m.store.SaveActionExecution(ctx, exec)` — this is where `action_execution` gets broadcast
- Where a worker calls `m.producer.PushFailed(...)` — this is where `failed_count` changes (but failed_count is now handled by the producer; Manager does not need to broadcast it separately)
- Where `m.active` is incremented and decremented — these are the points for `worker_stats` broadcast

- [ ] **Step 2: Add broadcaster field and WithBroadcaster method**

Add import for `internal/hub`. Add field to Manager struct and setter:

```go
// In Manager struct, add:
broadcaster hub.Broadcaster

// Add method:
func (m *Manager) WithBroadcaster(b hub.Broadcaster) *Manager {
    m.broadcaster = b
    return m
}
```

- [ ] **Step 3: Add broadcast helper**

Add a private helper that reads current stats and broadcasts — call this whenever worker count changes:

```go
func (m *Manager) broadcastWorkerStats() {
    if m.broadcaster == nil {
        return
    }
    s := m.Stats()
    m.broadcaster.Broadcast(hub.Event{
        Type:    "worker_stats",
        Payload: map[string]int{
            "total":  s.Total,
            "active": s.Active,
            "idle":   s.Idle,
        },
    })
}
```

- [ ] **Step 4: Broadcast worker_stats on worker state change**

Find the goroutine body where `m.active.Add(1)` is called (worker picks up task) and where `m.active.Add(-1)` is called (worker finishes). Add `m.broadcastWorkerStats()` immediately after each:

```go
// After m.active.Add(1):
m.broadcastWorkerStats()

// After m.active.Add(-1):
m.broadcastWorkerStats()
```

- [ ] **Step 5: Broadcast action_execution after save**

Find the `m.store.SaveActionExecution(ctx, exec)` call. After it succeeds, broadcast the execution:

```go
if err := m.store.SaveActionExecution(ctx, exec); err != nil {
    m.logger.Error("worker: save execution", "err", err)
    // existing error handling
} else if m.broadcaster != nil {
    m.broadcaster.Broadcast(hub.Event{
        Type:    "action_execution",
        Payload: exec,
    })
}
```

- [ ] **Step 6: Verify it compiles**

```bash
go build ./internal/worker/...
```

Expected: no errors.

- [ ] **Step 7: Commit**

```bash
git add internal/worker/manager.go
git commit -m "feat: broadcast worker_stats and action_execution events from worker manager"
```

---

## Task 5: Wire Broadcaster into BackendPool

**Files:**
- Modify: `internal/inference/backend_pool.go`

- [ ] **Step 1: Read the current file**

Read `internal/inference/backend_pool.go`. Find:
- `BackendPool` struct definition
- `NewBackendPool()` constructor
- `Load()` method (starts the health checker goroutine)
- `probeUnhealthy()` and `probeHealthy()` methods
- `runHealthChecker()` method (calls the two probe methods)

- [ ] **Step 2: Add broadcaster field and WithBroadcaster method**

Add import for `internal/hub`. Add field and setter:

```go
// In BackendPool struct, add:
broadcaster hub.Broadcaster

// Add method (after NewBackendPool):
func (p *BackendPool) WithBroadcaster(b hub.Broadcaster) {
    p.broadcaster = b
}
```

- [ ] **Step 3: Broadcast ollama_backends after each health probe**

In `runHealthChecker`, after each call to `probeUnhealthy()` and `probeHealthy()`, broadcast the current stats. Use `reflect.DeepEqual` to only send when state actually changed:

```go
import "reflect"

// In runHealthChecker, replace:
//   case <-unhealthyTick.C:
//       p.probeUnhealthy()
// With:
case <-unhealthyTick.C:
    before := p.Stats()
    p.probeUnhealthy()
    after := p.Stats()
    if !reflect.DeepEqual(before, after) && p.broadcaster != nil {
        p.broadcaster.Broadcast(hub.Event{Type: "ollama_backends", Payload: after})
    }

// Similarly for healthyTick:
case <-healthyTick.C:
    before := p.Stats()
    p.probeHealthy()
    after := p.Stats()
    if !reflect.DeepEqual(before, after) && p.broadcaster != nil {
        p.broadcaster.Broadcast(hub.Event{Type: "ollama_backends", Payload: after})
    }
```

- [ ] **Step 4: Also broadcast when a backend is enabled/disabled or added/removed**

Find the methods that mutate backends (e.g., `Load`, `AddBackend`, `RemoveBackend`, or equivalent — check the file). After any mutation that changes the set of backends, broadcast:

```go
if p.broadcaster != nil {
    p.broadcaster.Broadcast(hub.Event{Type: "ollama_backends", Payload: p.Stats()})
}
```

Add this after each mutation inside `p.mu.Lock()` / `p.mu.Unlock()` blocks where appropriate.

- [ ] **Step 5: Verify it compiles**

```bash
go build ./internal/inference/...
```

Expected: no errors.

- [ ] **Step 6: Commit**

```bash
git add internal/inference/backend_pool.go
git commit -m "feat: broadcast ollama_backends events from backend pool health checker"
```

---

## Task 6: Wire Broadcaster into Stream Client

**Files:**
- Modify: `internal/stream/client.go`

- [ ] **Step 1: Read the current file**

Read `internal/stream/client.go`. Find:
- `Client` struct definition
- `Connect()` method — where `notifier.EventStreamConnected` is emitted
- `Disconnect()` method — where `notifier.EventStreamDisconnected` is emitted

- [ ] **Step 2: Add broadcaster field and WithBroadcaster method**

Add import for `internal/hub`. Add field to Client struct and setter:

```go
// In Client struct, add:
broadcaster hub.Broadcaster

// Add method:
func (c *Client) WithBroadcaster(b hub.Broadcaster) {
    c.broadcaster = b
}
```

- [ ] **Step 3: Emit stream_status on connect**

Find the line in `Connect()` (or the internal connect goroutine) where the successful connection is established and `EventStreamConnected` is emitted. Right after it, broadcast:

```go
if c.broadcaster != nil {
    c.broadcaster.Broadcast(hub.Event{
        Type:    "stream_status",
        Payload: map[string]bool{"connected": true},
    })
}
```

- [ ] **Step 4: Emit stream_status on disconnect**

Find the line in `Disconnect()` (or wherever `EventStreamDisconnected` is emitted). Right after it, broadcast:

```go
if c.broadcaster != nil {
    c.broadcaster.Broadcast(hub.Event{
        Type:    "stream_status",
        Payload: map[string]bool{"connected": false},
    })
}
```

- [ ] **Step 5: Verify it compiles**

```bash
go build ./internal/stream/...
```

Expected: no errors.

- [ ] **Step 6: Commit**

```bash
git add internal/stream/client.go
git commit -m "feat: broadcast stream_status events from stream client"
```

---

## Task 7: WS Endpoint and Hub Field in API Server

**Files:**
- Create: `internal/api/handlers_ws.go`
- Modify: `internal/api/server.go`

- [ ] **Step 1: Read the server and auth package**

Read `internal/api/server.go` to find the `Server` struct fields and the route registration section. Read `internal/auth/` to find the token validation function — look for a function like `ValidateToken`, `VerifyToken`, or `ParseToken` that takes a token string and secret and returns claims or an error.

- [ ] **Step 2: Add hub field and WithHub to Server**

In `internal/api/server.go`, add import for `internal/hub`. Add the field to the Server struct and a setter:

```go
// In Server struct, add:
hub *hub.Hub

// Add method:
func (s *Server) WithHub(h *hub.Hub) *Server {
    s.hub = h
    return s
}
```

- [ ] **Step 3: Register the /api/ws route**

In the route registration section of `server.go`, add the WS endpoint in the **public** route group (before authentication middleware) so the handler can do its own token check:

```go
// In the public route group (alongside /api/auth/login etc.):
r.Get("/api/ws", s.handleWS)
```

- [ ] **Step 4: Create handlers_ws.go**

Check `internal/auth/` for the exact token verification function signature, then write the handler:

```go
// internal/api/handlers_ws.go
package api

import (
	"net/http"

	"github.com/amir-the-h/iran-watcher/internal/auth"
	"github.com/amir-the-h/iran-watcher/internal/hub"
)

func (s *Server) handleWS(w http.ResponseWriter, r *http.Request) {
	token := r.URL.Query().Get("token")
	if token == "" {
		http.Error(w, "missing token", http.StatusUnauthorized)
		return
	}
	// Use the same JWT verification the auth middleware uses.
	// Check internal/auth/ for the exact function name — common patterns:
	//   auth.ValidateToken(token, s.jwtSecret)
	//   auth.VerifyToken(token, s.jwtSecret)
	//   auth.ParseClaims(token, s.jwtSecret)
	if _, err := auth.ValidateToken(token, s.jwtSecret); err != nil {
		http.Error(w, "invalid token", http.StatusUnauthorized)
		return
	}
	if s.hub == nil {
		http.Error(w, "websocket not configured", http.StatusServiceUnavailable)
		return
	}
	hub.ServeWS(s.hub, w, r)
}
```

**Important:** Replace `auth.ValidateToken` with the actual function name found in `internal/auth/`.

- [ ] **Step 5: Verify it compiles**

```bash
go build ./internal/api/...
```

Expected: no errors.

- [ ] **Step 6: Commit**

```bash
git add internal/api/handlers_ws.go internal/api/server.go
git commit -m "feat: add /api/ws endpoint with JWT auth from query param"
```

---

## Task 8: Broadcast CRUD Entity Events from API Handlers

**Files:**
- Modify: `internal/api/server.go` and all mutation handler files

- [ ] **Step 1: Add a broadcast helper to Server**

In `internal/api/server.go`, add a helper that broadcasts an entity list event. This avoids repeating the nil check everywhere:

```go
func (s *Server) broadcastEvent(eventType string, payload any) {
    if s.hub == nil {
        return
    }
    s.hub.Broadcast(hub.Event{Type: eventType, Payload: payload})
}
```

- [ ] **Step 2: Add broadcast to action handlers**

Read `internal/api/handlers_actions.go` (or wherever action CRUD lives). After each successful POST, PUT, DELETE, activate, deactivate handler, re-fetch the full list and broadcast:

Pattern to apply after every mutation that succeeds:
```go
// After the mutation and 200/201 response is written:
if actions, err := s.store.ListActions(r.Context()); err == nil {
    s.broadcastEvent("actions", actions)
}
```

Apply this pattern to all of these entities in their respective handler files:

| Handler file(s) | Entity | Event type | Store method to call |
|---|---|---|---|
| handlers_actions.go | actions | `"actions"` | `s.store.ListActions(ctx)` |
| handlers_pipelines.go | pipelines | `"pipelines"` | `s.store.ListPipelines(ctx)` |
| handlers_chains.go | chains | `"chains"` | `s.store.ListChains(ctx)` (or similar) |
| handlers_rules.go | rules | `"rules"` | `s.store.ListRules(ctx)` |
| handlers_sources.go | sources | `"sources"` | `s.store.ListSources(ctx)` |
| handlers_entities.go | tracked_entities | `"tracked_entities"` | `s.store.ListTrackedEntities(ctx)` |
| handlers_notifications.go | notification_providers | `"notification_providers"` | `s.store.ListNotificationProviders(ctx)` |
| handlers_notifications.go | notification_channels | `"notification_channels"` | `s.store.ListNotificationChannels(ctx)` |
| handlers_ollama.go | ollama_backends (CRUD ops) | `"ollama_backends"` | `s.backendPool.Stats()` |
| handlers_users.go | users | `"users"` | `s.store.ListUsers(ctx)` |

**Note:** For each file, read it first to find the exact store method names (they follow the pattern `s.store.List<Entity>(ctx)`). Only broadcast after the DB write succeeds and before returning the HTTP response. Do not broadcast on GET or read-only endpoints.

- [ ] **Step 3: Verify it compiles**

```bash
go build ./internal/api/...
```

Expected: no errors.

- [ ] **Step 4: Commit**

```bash
git add internal/api/
git commit -m "feat: broadcast entity list events from all CRUD mutation handlers"
```

---

## Task 9: Wire Hub in Serve Command and Update nginx

**Files:**
- Modify: `cli/cmd/serve.go`
- Modify: `web/nginx.conf`

- [ ] **Step 1: Read serve.go**

Read `cli/cmd/serve.go` in full. Identify:
- Where the logger is created (Hub needs the logger)
- Where the Redis queue `q` is created
- Where the Worker Manager `mgr` is created
- Where the BackendPool `pool` is created and `pool.Load()` is called
- Where the Stream Client `streamClient` is created
- Where `api.NewServerWithConcreteRules(...)` is called
- Where the context `ctx` is created that governs shutdown

- [ ] **Step 2: Construct Hub and start Run goroutine**

Add import for `internal/hub`. After the logger is created and before any other component, construct the Hub and start its event loop:

```go
h := hub.New(logger)
go h.Run(ctx)
```

- [ ] **Step 3: Wire Hub into all sources**

Add these calls after each respective component is constructed (before it is used):

```go
// After q (RedisQueue) is created:
q.WithBroadcaster(h)

// After mgr (worker.Manager) is created:
mgr.WithBroadcaster(h)

// After pool (BackendPool) is created but BEFORE pool.Load() is called:
pool.WithBroadcaster(h)

// After streamClient is created:
streamClient.WithBroadcaster(h)

// After srv (API server) is created:
srv.WithHub(h)
```

- [ ] **Step 4: Update nginx.conf for WebSocket proxying**

Read `web/nginx.conf`. Find the `/api/` proxy location block. Add a new location block **before** the generic `/api/` block (nginx matches from most specific to least specific):

```nginx
location /api/ws {
    proxy_pass http://${API_UPSTREAM};
    proxy_http_version 1.1;
    proxy_set_header Upgrade $http_upgrade;
    proxy_set_header Connection "upgrade";
    proxy_set_header Host $host;
    proxy_read_timeout 3600s;
    proxy_send_timeout 3600s;
}
```

Without `proxy_http_version 1.1` and the Upgrade/Connection headers, nginx will not forward the WebSocket handshake and the browser will get a 400.

- [ ] **Step 5: Build Docker image to verify**

```bash
docker compose build iran-watcher
```

Expected: build succeeds with no Go compilation errors.

- [ ] **Step 6: Commit**

```bash
git add cli/cmd/serve.go web/nginx.conf
git commit -m "feat: wire hub into all event sources and configure nginx ws proxy"
```

---

## Task 10: Frontend Operational Stores

**Files:**
- Create: `web/src/stores/ws.ts`
- Create: `web/src/stores/queues.ts`
- Create: `web/src/stores/executions.ts`
- Create: `web/src/stores/ollama.ts`
- Modify: `web/src/stores/stream.ts`
- Modify: `web/src/stores/failures.ts`

- [ ] **Step 1: Read the existing store files**

Read `web/src/stores/stream.ts` and `web/src/stores/failures.ts` to understand their existing structure before adding methods.

Also read `web/src/stores/actions.ts` to understand the TypeScript type patterns used in this project (how models are imported/typed).

- [ ] **Step 2: Create web/src/stores/ws.ts**

```typescript
// web/src/stores/ws.ts
import { defineStore } from 'pinia'
import { ref } from 'vue'

export type WsStatus = 'connecting' | 'connected' | 'disconnected'

export const useWsStore = defineStore('ws', () => {
  const status = ref<WsStatus>('disconnected')

  function setStatus(s: WsStatus) {
    status.value = s
  }

  return { status, setStatus }
})
```

- [ ] **Step 3: Create web/src/stores/queues.ts**

```typescript
// web/src/stores/queues.ts
import { defineStore } from 'pinia'
import { ref, computed } from 'vue'

export interface QueueEntry {
  action_id: number
  action_name: string
  pipeline_name: string
  pipeline_type: string
  pending: number
  worker: { id: number; backend: string } | null
}

export interface WorkerStats {
  Total: number
  Active: number
  Idle: number
  ActiveWorkers: Array<{ ID: number; Action: string; Pipeline: string; Backend: string }>
}

export const useQueuesStore = defineStore('queues', () => {
  const queues = ref<QueueEntry[]>([])
  const workerStats = ref<WorkerStats | null>(null)

  // Set the full list from the initial HTTP fetch (onMounted in QueueView).
  function setQueues(entries: QueueEntry[]) {
    queues.value = entries
  }

  // Update a single action's pending count from a WS queue_update event.
  function setQueueEntry(payload: { action_id: number; pending: number }) {
    const entry = queues.value.find(q => q.action_id === payload.action_id)
    if (entry) {
      entry.pending = payload.pending
    }
  }

  function setWorkerStats(stats: WorkerStats) {
    workerStats.value = stats
  }

  const totalPending = computed(() =>
    queues.value.reduce((sum, q) => sum + q.pending, 0)
  )

  return { queues, workerStats, totalPending, setQueues, setQueueEntry, setWorkerStats }
})
```

- [ ] **Step 4: Create web/src/stores/executions.ts**

Read `web/src/stores/actions.ts` to find how `ActionExecution` is typed in this project, then write:

```typescript
// web/src/stores/executions.ts
import { defineStore } from 'pinia'
import { ref } from 'vue'

// Mirror the Go model.ActionExecution struct
export interface ActionExecution {
  id: number
  action_id: number
  tweet_id: string
  status: string
  error?: string
  executed_at: string
  metadata?: Record<string, string>
}

export const useExecutionsStore = defineStore('executions', () => {
  const executions = ref<ActionExecution[]>([])

  // Called from WS event: prepend new execution to the list, cap at 200.
  function prepend(exec: ActionExecution) {
    executions.value.unshift(exec)
    if (executions.value.length > 200) {
      executions.value.splice(200)
    }
  }

  function setAll(items: ActionExecution[]) {
    executions.value = items
  }

  return { executions, prepend, setAll }
})
```

- [ ] **Step 5: Create web/src/stores/ollama.ts**

Read `web/src/views/OllamaView.vue` to understand the type shape of Ollama backends used in the view, then write:

```typescript
// web/src/stores/ollama.ts
import { defineStore } from 'pinia'
import { ref } from 'vue'

export interface OllamaBackend {
  id: number
  name: string
  url: string
  enabled: boolean
  healthy: boolean
  in_use: boolean
  score: number
  request_count: number
  last_active_at: string | null
}

export const useOllamaStore = defineStore('ollama', () => {
  const backends = ref<OllamaBackend[]>([])

  function setBackends(items: OllamaBackend[]) {
    backends.value = items
  }

  return { backends, setBackends }
})
```

- [ ] **Step 6: Add setConnected to stream.ts**

In `web/src/stores/stream.ts`, add this function to the store's return value:

```typescript
function setConnected(v: boolean) {
  connected.value = v
}
// Add setConnected to the return statement
```

- [ ] **Step 7: Add setTotal to failures.ts**

In `web/src/stores/failures.ts`, add this function inside the store:

```typescript
function setTotal(n: number) {
  total.value = n
}
// Add setTotal to the return statement
```

- [ ] **Step 8: Verify frontend builds**

```bash
docker compose build iran-watcher-web 2>/dev/null || docker compose build web 2>/dev/null
```

Or verify TypeScript:
```bash
cd web && npx tsc --noEmit 2>&1 | head -30
```

Expected: no type errors.

- [ ] **Step 9: Commit**

```bash
git add web/src/stores/ws.ts web/src/stores/queues.ts web/src/stores/executions.ts web/src/stores/ollama.ts web/src/stores/stream.ts web/src/stores/failures.ts
git commit -m "feat: add operational Pinia stores for WS-driven live updates"
```

---

## Task 11: Add setAll to Existing Entity Stores

**Files:**
- Modify: `web/src/stores/actions.ts`, `rules.ts`, `chains.ts`, `sources.ts`, `entities.ts`, `notifications.ts`, `models.ts`

- [ ] **Step 1: Read all target store files**

Read each of these files to find: the existing reactive list ref name and the TypeScript type used for items.

- [ ] **Step 2: Add setAll to actions.ts**

Find the `actions` ref (type `Action[]` or similar). Add inside the store:

```typescript
function setAll(items: typeof actions.value) {
  actions.value = items
}
// Add setAll to return statement
```

- [ ] **Step 3: Add setAll to rules.ts**

```typescript
function setAll(items: typeof rules.value) {
  rules.value = items
}
// Add to return statement
```

- [ ] **Step 4: Add setAll to chains.ts**

Read chains.ts to find the chains ref name. Add the same pattern.

- [ ] **Step 5: Add setAll to sources.ts**

```typescript
function setAll(items: typeof sources.value) {
  sources.value = items
}
// Add to return statement
```

- [ ] **Step 6: Add setAll to entities.ts (tracked entities)**

Read entities.ts to find the entities ref name. Add the same pattern.

- [ ] **Step 7: Add setProviders and setChannels to notifications.ts**

Read notifications.ts to find the providers and channels ref names. Add:

```typescript
function setProviders(items: typeof providers.value) {
  providers.value = items
}
function setChannels(items: typeof channels.value) {
  channels.value = items
}
// Add both to return statement
```

- [ ] **Step 8: Add setAll to models.ts (pipelines)**

Read models.ts to find the models/pipelines ref name. Add:

```typescript
function setAll(items: typeof models.value) {  // use the actual ref name
  models.value = items
}
// Add to return statement
```

- [ ] **Step 9: Verify TypeScript compiles**

```bash
cd web && npx tsc --noEmit 2>&1 | head -30
```

Expected: no errors.

- [ ] **Step 10: Commit**

```bash
git add web/src/stores/actions.ts web/src/stores/rules.ts web/src/stores/chains.ts web/src/stores/sources.ts web/src/stores/entities.ts web/src/stores/notifications.ts web/src/stores/models.ts
git commit -m "feat: add setAll methods to entity stores for WS-driven updates"
```

---

## Task 12: useWebSocket Composable

**Files:**
- Create: `web/src/composables/useWebSocket.ts`

- [ ] **Step 1: Check how the auth store is used elsewhere**

Read `web/src/stores/auth.ts` to find the exact field name for the JWT token (e.g., `auth.token` or `auth.token.value`).

- [ ] **Step 2: Create the composable**

```typescript
// web/src/composables/useWebSocket.ts
import { useWsStore } from '@/stores/ws'
import { useQueuesStore } from '@/stores/queues'
import { useOllamaStore } from '@/stores/ollama'
import { useExecutionsStore } from '@/stores/executions'
import { useActionsStore } from '@/stores/actions'
import { useRulesStore } from '@/stores/rules'
import { useChainsStore } from '@/stores/chains'
import { useSourcesStore } from '@/stores/sources'
import { useEntitiesStore } from '@/stores/entities'
import { useNotificationsStore } from '@/stores/notifications'
import { useModelsStore } from '@/stores/models'
import { useStreamStore } from '@/stores/stream'
import { useFailuresStore } from '@/stores/failures'
import { useAuthStore } from '@/stores/auth'

// Adjust the import paths above to match the actual store names in your codebase.
// The store names used here may differ — check web/src/stores/ for the exact
// export names (e.g., useActionsStore vs useActionStore).

let ws: WebSocket | null = null
let pingInterval: ReturnType<typeof setInterval> | null = null
let staleInterval: ReturnType<typeof setInterval> | null = null
let lastMessageAt = 0
let reconnectDelay = 1000
let stopped = false

const handlers: Record<string, (payload: any) => void> = {}

function buildHandlers() {
  const wsStore = useWsStore()
  const queuesStore = useQueuesStore()
  const ollamaStore = useOllamaStore()
  const executionsStore = useExecutionsStore()
  const actionsStore = useActionsStore()
  const rulesStore = useRulesStore()
  const chainsStore = useChainsStore()
  const sourcesStore = useSourcesStore()
  const entitiesStore = useEntitiesStore()
  const notificationsStore = useNotificationsStore()
  const modelsStore = useModelsStore()
  const streamStore = useStreamStore()
  const failuresStore = useFailuresStore()

  return {
    pong:                   (_: any) => { /* timestamp updated in onmessage */ },
    worker_stats:           (p: any) => queuesStore.setWorkerStats(p),
    queue_update:           (p: any) => queuesStore.setQueueEntry(p),
    failed_count:           (p: any) => failuresStore.setTotal(p.count),
    stream_status:          (p: any) => streamStore.setConnected(p.connected),
    ollama_backends:        (p: any) => ollamaStore.setBackends(p),
    action_execution:       (p: any) => executionsStore.prepend(p),
    actions:                (p: any) => actionsStore.setAll(p),
    pipelines:              (p: any) => modelsStore.setAll(p),
    chains:                 (p: any) => chainsStore.setAll(p),
    rules:                  (p: any) => rulesStore.setAll(p),
    sources:                (p: any) => sourcesStore.setAll(p),
    tracked_entities:       (p: any) => entitiesStore.setAll(p),
    notification_providers: (p: any) => notificationsStore.setProviders(p),
    notification_channels:  (p: any) => notificationsStore.setChannels(p),
  }
}

function clearTimers() {
  if (pingInterval !== null) { clearInterval(pingInterval); pingInterval = null }
  if (staleInterval !== null) { clearInterval(staleInterval); staleInterval = null }
}

function scheduleReconnect() {
  if (stopped) return
  setTimeout(connect, reconnectDelay)
  reconnectDelay = Math.min(reconnectDelay * 2, 30_000)
}

export function connect() {
  if (stopped) return
  const auth = useAuthStore()
  const wsStore = useWsStore()

  if (!auth.isAuthenticated || !auth.token) return

  wsStore.setStatus('connecting')

  const proto = location.protocol === 'https:' ? 'wss' : 'ws'
  const url = `${proto}://${location.host}/api/ws?token=${auth.token}`

  ws = new WebSocket(url)
  const eventHandlers = buildHandlers()

  ws.onopen = () => {
    wsStore.setStatus('connected')
    reconnectDelay = 1000
    lastMessageAt = Date.now()

    pingInterval = setInterval(() => {
      if (ws?.readyState === WebSocket.OPEN) {
        ws.send(JSON.stringify({ type: 'ping' }))
      }
    }, 30_000)

    staleInterval = setInterval(() => {
      if (Date.now() - lastMessageAt > 60_000) {
        ws?.close()
      }
    }, 10_000)
  }

  ws.onmessage = (e) => {
    lastMessageAt = Date.now()
    try {
      const event = JSON.parse(e.data) as { type: string; payload: any }
      const handler = eventHandlers[event.type]
      if (handler) handler(event.payload)
    } catch {
      // malformed message: ignore
    }
  }

  ws.onerror = () => {
    // onclose will fire after onerror — let it handle reconnect
  }

  ws.onclose = () => {
    clearTimers()
    wsStore.setStatus('disconnected')
    if (!stopped) scheduleReconnect()
  }
}

export function disconnect() {
  stopped = true
  clearTimers()
  ws?.close()
  ws = null
  useWsStore().setStatus('disconnected')
}

export function resetAndConnect() {
  stopped = false
  reconnectDelay = 1000
  connect()
}
```

**Important:** The store import names (`useActionsStore`, `useChainsStore`, etc.) and the method names on those stores (`setAll`, `setProviders`, etc.) must match what was implemented in Task 11. Check each store file's export name before saving.

- [ ] **Step 3: Verify TypeScript compiles**

```bash
cd web && npx tsc --noEmit 2>&1 | head -30
```

Expected: no errors. Fix any import path mismatches.

- [ ] **Step 4: Commit**

```bash
git add web/src/composables/useWebSocket.ts
git commit -m "feat: add useWebSocket composable with reconnect backoff and event routing"
```

---

## Task 13: Remove Polling from QueueView

**Files:**
- Modify: `web/src/views/QueueView.vue`

- [ ] **Step 1: Read QueueView.vue in full**

Understand:
- Where `pollTimer` and the `setInterval` are
- What `fetchStats()` and `fetchQueues()` return and how they populate local state
- How the template uses `queueStats`, `workerStats`, `queues`

- [ ] **Step 2: Import and use useQueuesStore**

Add the import and store usage at the top of `<script setup>`:

```typescript
import { useQueuesStore } from '@/stores/queues'
const queuesStore = useQueuesStore()
```

- [ ] **Step 3: Replace local queueStats and workerStats refs**

Remove the local `queueStats` and `workerStats` refs. Replace their usages in the template with `queuesStore.workerStats` and `queuesStore.totalPending` / `queuesStore.queues`.

- [ ] **Step 4: Keep onMounted fetch but push to store**

Keep the initial HTTP fetch in `onMounted` to populate state immediately. Change `fetchQueues()` to push results to the store instead of local refs:

```typescript
async function fetchQueues() {
  try {
    const result = await apiFetch<{ queues: any[] }>('/api/queue')
    queuesStore.setQueues(result.queues)
  } catch { /* ignore */ }
}
```

Similarly update `fetchStats()` to push to the store:
```typescript
async function fetchStats() {
  try {
    const health = await apiFetch<any>('/api/health')
    if (health.workers) queuesStore.setWorkerStats(health.workers)
  } catch { /* ignore */ }
}
```

- [ ] **Step 5: Remove the polling setInterval**

Remove the `pollTimer` variable, the `setInterval` call in `onMounted`, and the `clearInterval` in `onUnmounted`. The `onMounted` block should call `fetchQueues()` and `fetchStats()` once only:

```typescript
onMounted(() => {
  fetchQueues()
  fetchStats()
})
// Remove onUnmounted entirely (no timer to clean up)
```

- [ ] **Step 6: Update the template**

Replace all template references to local `queueStats` / `workerStats` / `queues` with the store equivalents:
- `queueStats?.pending` → `queuesStore.totalPending`
- `workerStats?.total` → `queuesStore.workerStats?.Total`
- `workerStats?.active` → `queuesStore.workerStats?.Active`
- `workerStats?.idle` → `queuesStore.workerStats?.Idle`
- `queues` → `queuesStore.queues`

- [ ] **Step 7: Verify the view works**

```bash
docker compose build iran-watcher-web 2>/dev/null || docker compose build web 2>/dev/null
```

Expected: build succeeds.

- [ ] **Step 8: Commit**

```bash
git add web/src/views/QueueView.vue
git commit -m "feat: replace QueueView polling with WS-driven store updates"
```

---

## Task 14: Remove Polling from OllamaView

**Files:**
- Modify: `web/src/views/OllamaView.vue`

- [ ] **Step 1: Read OllamaView.vue in full**

Find `pollTimer`, the `refresh()` function, and how `backends` is used in the template.

- [ ] **Step 2: Import and use useOllamaStore**

```typescript
import { useOllamaStore } from '@/stores/ollama'
const ollamaStore = useOllamaStore()
```

- [ ] **Step 3: Replace local backends ref with store**

Remove the local `backends` ref. Keep the `refresh()` function but have it push results into the store:

```typescript
async function refresh() {
  try {
    const items = await listOllamaBackends()  // keep existing API call
    ollamaStore.setBackends(items)
  } catch { /* server unreachable */ }
}
```

- [ ] **Step 4: Remove setInterval**

Remove `pollTimer`, the `setInterval(refresh, 5000)` call, and the `clearInterval` in `onUnmounted`. The `onMounted` block should call `refresh()` once:

```typescript
onMounted(async () => {
  loading.value = true
  await refresh()
  loading.value = false
})
```

- [ ] **Step 5: Update template to use store**

Replace `backends` with `ollamaStore.backends` in all template bindings.

- [ ] **Step 6: Commit**

```bash
git add web/src/views/OllamaView.vue
git commit -m "feat: replace OllamaView polling with WS-driven store updates"
```

---

## Task 15: Remove Polling from App.vue and Connect WS on Auth

**Files:**
- Modify: `web/src/App.vue`

- [ ] **Step 1: Read App.vue in full**

Find:
- The `fetchStats()` function and the `setInterval` / `clearInterval` pattern
- The `queueStats` and `workerStats` local refs used in the header template
- Where auth state is watched (e.g., `watch(auth.isAuthenticated, ...)` or `onMounted`)
- How `stream.fetchStatus()` is called

- [ ] **Step 2: Import stores and composable**

```typescript
import { useQueuesStore } from '@/stores/queues'
import { useStreamStore } from '@/stores/stream'
import { useFailuresStore } from '@/stores/failures'
import { connect as wsConnect, disconnect as wsDisconnect, resetAndConnect } from '@/composables/useWebSocket'
import { useWsStore } from '@/stores/ws'
import { useAuthStore } from '@/stores/auth'

const queuesStore = useQueuesStore()
const streamStore = useStreamStore()
const failuresStore = useFailuresStore()
const wsStore = useWsStore()
```

- [ ] **Step 3: Remove local queueStats and workerStats refs**

Delete the local `queueStats` and `workerStats` refs and the `fetchStats()` function.

- [ ] **Step 4: Remove the polling setInterval**

Remove `pollTimer`, the `setInterval(fetchStats, 3000)` call in `onMounted`, and the `clearInterval` in `onUnmounted`.

- [ ] **Step 5: Connect/disconnect WS based on auth state**

Replace the `onMounted` polling setup with WS connection management:

```typescript
import { watch, onMounted, onUnmounted } from 'vue'
const auth = useAuthStore()

onMounted(() => {
  if (auth.isAuthenticated) {
    resetAndConnect()
  }
})

onUnmounted(() => {
  wsDisconnect()
})

watch(() => auth.isAuthenticated, (authenticated) => {
  if (authenticated) {
    resetAndConnect()
  } else {
    wsDisconnect()
  }
})
```

- [ ] **Step 6: Update header template to use stores**

Replace all template references to local `queueStats` / `workerStats` with store equivalents:
- `queueStats?.pending` → `queuesStore.totalPending`
- `queueStats?.failed` → `failuresStore.total`
- `workerStats?.total` → `queuesStore.workerStats?.Total`
- `workerStats?.active` → `queuesStore.workerStats?.Active`
- `workerStats?.idle` → `queuesStore.workerStats?.Idle`
- `stream.connected` → `streamStore.connected`

Optionally, show the WS connection state from `wsStore.status` so users can see "reconnecting…" in the header when the WS is disconnected.

- [ ] **Step 7: Keep the initial health fetch on mount**

Add a one-time fetch so the header shows current state immediately before the first WS event arrives:

```typescript
async function fetchInitialStats() {
  if (!auth.isAuthenticated) return
  try {
    const h = await apiFetch<any>('/api/health')
    if (h.workers) queuesStore.setWorkerStats(h.workers)
  } catch { /* ignore */ }
  if (auth.isAdmin) {
    try { await streamStore.fetchStatus() } catch { /* ignore */ }
  }
}

onMounted(() => {
  fetchInitialStats()
  if (auth.isAuthenticated) resetAndConnect()
})
```

- [ ] **Step 8: Verify build**

```bash
docker compose build iran-watcher-web 2>/dev/null || docker compose build web 2>/dev/null
```

Expected: build succeeds.

- [ ] **Step 9: Commit**

```bash
git add web/src/App.vue
git commit -m "feat: remove App.vue polling, connect WS on login, use stores for header stats"
```

---

## Task 16: Final Build Verification and Docker Test

**Files:** None new.

- [ ] **Step 1: Build the full Docker image**

```bash
docker compose build iran-watcher
docker compose build
```

Expected: both images build successfully.

- [ ] **Step 2: Run unit tests**

```bash
go test ./internal/hub/... -v
go test ./... 2>&1 | grep -E "FAIL|ok"
```

Expected: `internal/hub` tests pass. No other tests regress.

- [ ] **Step 3: Manual smoke test (if running)**

```bash
docker compose up -d
# Open browser, log in, open DevTools → Network → WS tab
# Navigate to /queue — verify WebSocket connection at /api/ws
# Check that messages arrive periodically (worker_stats, queue_update)
# Let a tweet come in (or manually trigger) — verify action_execution event appears
# Disconnect network briefly — verify UI shows reconnecting indicator
# Reconnect — verify WS reconnects automatically
docker compose down
```

- [ ] **Step 4: Commit any fixes found during smoke test**

```bash
git add -A
git commit -m "fix: address issues found during WS smoke test"
```

- [ ] **Step 5: Update CLAUDE.md**

Open `CLAUDE.md`. In **Key Design Decisions**, add an entry for the WebSocket live update system:

```
- **WebSocket live updates** — a `Hub` goroutine in `internal/hub` fans out typed JSON events to all authenticated browser clients; sources (Producer, Worker Manager, BackendPool, Stream Client, API mutation handlers) call `hub.Broadcast()` when state changes; the frontend maintains one persistent WS connection per session via `useWebSocket` composable (`web/src/composables/useWebSocket.ts`); Pinia stores update reactively; no polling remains; `Broadcaster` interface defined in `internal/hub/hub.go` is injected via `WithBroadcaster()` setters; event types: `queue_update`, `worker_stats`, `failed_count`, `stream_status`, `ollama_backends`, `action_execution`, and all CRUD entity types (`actions`, `pipelines`, `chains`, `rules`, `sources`, `tracked_entities`, `notification_providers`, `notification_channels`, `users`); ping/pong is application-level (30s interval); client detects stale connection (no message for 60s) and reconnects with exponential backoff capped at 30s; endpoint: `GET /api/ws?token=<jwt>` (public route, auth via query param)
```

- [ ] **Step 6: Final commit**

```bash
git add CLAUDE.md
git commit -m "docs: update CLAUDE.md with websocket live updates design decisions"
```
