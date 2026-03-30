# Live Updates via WebSocket — Design Spec

**Date:** 2026-03-30
**Status:** Approved

## Problem

The UI keeps state current through polling: `QueueView` calls `/api/health` and `/api/queue` every 3 seconds; `OllamaView` calls `/api/ollama/backends` every 5 seconds; the app header polls alongside these. Polling adds latency, wastes resources, and doesn't scale to full entity sync (CRUD changes on one client are invisible to another until the next poll cycle).

## Goal

Replace all polling with a single persistent WebSocket connection per client. The backend pushes typed events when state changes. The frontend updates Pinia stores reactively. All entity types are covered — operational stats and CRUD entities alike.

---

## Architecture

```
Backend sources                Hub                  Browser
──────────────                 ───                  ───────
Producer         ─ Broadcast ─▶ Hub.Run()  ──────▶  useWebSocket
Worker Manager   ─ Broadcast ─▶  (fan-out) ──────▶  composable
BackendPool      ─ Broadcast ─▶            ──────▶  Pinia stores
Stream client    ─ Broadcast ─▶
API handlers     ─ Broadcast ─▶
```

The Hub is a pure fan-out: it never fetches state. Sources own their state and push to the Hub when it changes.

---

## Backend

### New package: `internal/hub`

```
internal/hub/
  hub.go     — Hub struct, Run() goroutine, Broadcast()
  client.go  — Client struct, read pump, write pump
```

#### Hub

```go
type Hub struct {
    clients    map[*Client]bool
    broadcast  chan Event
    register   chan *Client
    unregister chan *Client
}

type Event struct {
    Type    string `json:"type"`
    Payload any    `json:"payload"`
}
```

`Hub.Run()` is a `select` loop:
- `register` — add client to map
- `unregister` — remove client, close its `send` channel
- `broadcast` — marshal event to JSON, write to all `client.send` channels; if a channel is full, drop that client

The `broadcast` channel is buffered so emitters never block on a busy hub.

#### Client

```go
type Client struct {
    hub  *Hub
    conn *websocket.Conn  // gorilla/websocket
    send chan []byte
}
```

Each client runs two goroutines:

**Read pump** — reads frames from the connection. Handles `{"type":"ping"}` by writing `{"type":"pong"}` directly. Detects close frames and network errors → unregisters the client.

**Write pump** — drains the `send` channel. Before each write: `conn.SetWriteDeadline(time.Now().Add(10s))`. If the write deadline is exceeded (client TCP connection is dead without a clean close), the write returns an error and the client is unregistered. This prevents goroutine leaks from zombie connections.

#### Broadcaster interface

```go
type Broadcaster interface {
    Broadcast(Event)
}
```

Injected into sources via constructor. Keeps source packages decoupled from the hub implementation.

### WS Endpoint

`GET /api/ws` — authenticated, all roles (viewers get the same live feed consistent with their read access).

Auth: token passed as `?token=<jwt>` query param. The upgrade handler validates the JWT before accepting the connection.

Library: `gorilla/websocket`.

### Event Emission

Sources emit when their state changes — no polling in the hub.

| Source | Trigger | Event type | Payload |
|---|---|---|---|
| `queue.Producer` | `LPUSH` returns new list length | `queue_update` | `{action_id, pending}` |
| `worker.Manager` | Worker picks up or finishes a task | `worker_stats` | `{total, active, idle}` |
| `worker.Manager` | Task dead-lettered | `failed_count` | `{count}` |
| `worker.Manager` | Execution saved to DB | `action_execution` | execution row |
| `inference.BackendPool` | Health check completes and state changed | `ollama_backends` | full backend list |
| `stream.Client` | Connect / disconnect | `stream_status` | `{connected}` |
| API mutation handlers | POST/PUT/DELETE succeeds | entity type (see below) | updated list or entity |

**CRUD entity events** — emitted from API handlers after successful DB writes:

| Entity | Event type |
|---|---|
| Actions | `actions` |
| Pipelines | `pipelines` |
| Chains | `chains` |
| Rules | `rules` |
| Sources | `sources` |
| Tracked entities | `tracked_entities` |
| Notification providers | `notification_providers` |
| Notification channels | `notification_channels` |
| Ollama backends (CRUD) | `ollama_backends` |
| Users | `users` |

Payload for CRUD events is the full updated list (same shape as the corresponding GET endpoint), so the frontend can replace its store state without diffing.

### Wire-up

`Hub` is constructed in `cmd/iran-watcher/main.go` alongside existing dependencies, then:
- Passed to `queue.NewProducer`
- Passed to `worker.NewManager`
- Passed to `inference.NewBackendPool`
- Passed to `stream.NewClient`
- Passed to `api.NewServer` (for CRUD broadcast calls in handlers)

---

## Frontend

### `useWsStore` (Pinia)

Holds connection state: `status: 'connecting' | 'connected' | 'disconnected'`. Any component can read this to show a reconnecting indicator.

### `useWebSocket` composable (`web/src/composables/useWebSocket.ts`)

Manages the single WS connection for the app lifetime.

**Lifecycle:**
- `connect()` — called after successful login. Opens `ws[s]://<host>/api/ws?token=<jwt>`.
- `disconnect()` — called on logout. Sends clean close frame.
- Auto-reconnect on `onerror` / `onclose` with exponential backoff: 1s → 2s → 4s → 8s → … capped at 30s.

**Ping/pong:**
- `setInterval` every 30s sends `{"type":"ping"}`.
- `lastMessageAt` is updated on every received message (pong or any event).
- A parallel `setInterval` every 10s checks: if `Date.now() - lastMessageAt > 60_000` → force close → reconnect backoff triggers.

**Event routing:**

```ts
const handlers: Record<string, (payload: any) => void> = {
  worker_stats:           (p) => useQueueStore().setWorkerStats(p),
  queue_update:           (p) => useQueueStore().setQueueEntry(p),
  failed_count:           (p) => useQueueStore().setFailedCount(p),
  stream_status:          (p) => useQueueStore().setStreamStatus(p),
  ollama_backends:        (p) => useOllamaStore().setBackends(p),
  action_execution:       (p) => useExecutionsStore().prepend(p),
  actions:                (p) => useActionsStore().setAll(p),
  pipelines:              (p) => usePipelinesStore().setAll(p),
  chains:                 (p) => useChainsStore().setAll(p),
  rules:                  (p) => useRulesStore().setAll(p),
  sources:                (p) => useSourcesStore().setAll(p),
  tracked_entities:       (p) => useTrackedEntitiesStore().setAll(p),
  notification_providers: (p) => useNotificationProvidersStore().setAll(p),
  notification_channels:  (p) => useNotificationChannelsStore().setAll(p),
  users:                  (p) => useUsersStore().setAll(p),
}
```

Unknown event types are silently ignored (forward compatibility).

### Store changes

Each Pinia store gains setter methods (`setAll`, `setWorkerStats`, `setQueueEntry`, etc.) that update reactive state. These are the same shape as the existing `onMounted` fetch handlers — stores that already exist just get the new setters; stores that don't exist yet (for entities currently fetched inline in components) are created.

### Polling removal

- `QueueView.vue` — remove `setInterval` for `/api/health` and `/api/queue`
- `OllamaView.vue` — remove `setInterval` for `/api/ollama/backends`
- App header — remove polling for health/stream status

All views keep their `onMounted` fetch for initial page load. On reconnect after a disconnect, each view re-fetches initial data to fill any gap.

---

## Error Handling

| Scenario | Handling |
|---|---|
| Client TCP connection dies without clean close | Write deadline (10s) returns error → client unregistered, goroutine exits |
| Slow client (send channel full) | Client dropped from hub immediately, connection closed |
| Backend goes down | Browser `onclose` fires → reconnect backoff loop; `useWsStore.status = 'disconnected'`; UI shows reconnecting indicator |
| JWT expired at upgrade time | Upgrade handler rejects with 401; frontend redirects to login |
| Network blip (no message for 60s) | Client-side stale check fires → force close → reconnect |

---

## Testing

**Backend:**
- Hub unit test: register N mock clients, broadcast an event, assert all received it; unregister one, broadcast again, assert dropped client is not written to.
- Write deadline test: real WS connection to test server, pause the reader, assert server drops client after deadline.
- Broadcaster integration: mock `Broadcaster`, trigger a mutation (e.g. LPUSH), assert `Broadcast()` called with correct event type and payload.

**Frontend:**
- Store unit tests: call setter methods directly, assert reactive state updates correctly. No WS infrastructure needed.
- Composable test: mock `WebSocket`, assert ping interval fires, assert events route to the correct store handler.

---

## New Dependencies

- `github.com/gorilla/websocket` — Go WS library

## Files Changed / Added

| Path | Change |
|---|---|
| `internal/hub/hub.go` | New — Hub struct and Run() |
| `internal/hub/client.go` | New — Client read/write pumps |
| `internal/queue/redis.go` | Inject Broadcaster, emit `queue_update` on LPUSH |
| `internal/worker/manager.go` | Inject Broadcaster, emit `worker_stats`, `failed_count`, `action_execution` |
| `internal/inference/backend_pool.go` | Inject Broadcaster, emit `ollama_backends` on health change |
| `internal/stream/client.go` | Inject Broadcaster, emit `stream_status` on connect/disconnect |
| `internal/api/server.go` | Add `/api/ws` route; inject Hub into Server |
| `internal/api/handlers_ws.go` | New — WS upgrade handler |
| `internal/api/handlers_*.go` | Broadcast entity events after mutations |
| `cmd/iran-watcher/main.go` | Construct Hub, wire into all components |
| `web/src/composables/useWebSocket.ts` | New — WS composable |
| `web/src/stores/ws.ts` | New — connection state store |
| `web/src/stores/*.ts` | Add setter methods for WS-driven updates |
| `web/src/views/QueueView.vue` | Remove setInterval polling |
| `web/src/views/OllamaView.vue` | Remove setInterval polling |
| `web/src/App.vue` | Remove header polling; call connect/disconnect on auth state |
