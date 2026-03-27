# Ollama Backend Health & Failover Design

**Date:** 2026-03-27
**Status:** Approved

## Problem

The Ollama backend pool supports multiple URLs (e.g. a desktop PC and a MacBook). Either machine can go offline at any time. The current implementation has no ongoing health monitoring — a failed backend stays in the pool and requests that land on it simply error out. There is no recovery path until the process restarts.

## Goals

- Automatically disable backends that stop responding
- Periodically probe disabled backends and re-enable them when they recover
- Transparently retry requests on another healthy backend when the current one fails mid-request
- Block (rather than error) when all backends are temporarily offline, resuming when one recovers

## Approach: Mutex + Condition Variable Pool

Replace the existing `pool chan *ollamaBackend` semaphore with a mutex-protected slice and a `sync.Cond`. Each backend tracks its own `healthy` and `inUse` state. A background health-checker goroutine drives transitions between healthy and unhealthy. Request-time failures trigger immediate marking and transparent retry.

---

## State Model

### `ollamaBackend`

```go
type ollamaBackend struct {
    baseURL string
    client  *http.Client
    healthy bool   // managed by health checker and request-time failures
    inUse   bool   // true while a request holds this backend
}
```

All backends start `healthy: true, inUse: false`.

### `OllamaPipeline` changes

Remove `pool chan *ollamaBackend`. Add:

```go
mu           sync.Mutex
cond         *sync.Cond        // broadcast whenever any backend state changes
cancelHealth context.CancelFunc
```

`MaxConcurrency()` returns the count of currently healthy backends (checked under lock) so the worker manager's concurrency limit stays accurate as backends come and go.

---

## Acquire / Release / MarkUnhealthy

### Acquire

Finds the first `healthy && !inUse` backend and marks it `inUse`. If none is available, waits on the condition variable. `context.AfterFunc` broadcasts on the cond when the context is cancelled so waiters unblock cleanly on shutdown.

```go
func (p *OllamaPipeline) acquire(ctx context.Context) (*ollamaBackend, error) {
    stop := context.AfterFunc(ctx, func() { p.cond.Broadcast() })
    defer stop()
    p.mu.Lock()
    defer p.mu.Unlock()
    for {
        for _, b := range p.backends {
            if b.healthy && !b.inUse {
                b.inUse = true
                return b, nil
            }
        }
        if ctx.Err() != nil {
            return nil, ctx.Err()
        }
        p.cond.Wait()
    }
}
```

### Release

```go
func (p *OllamaPipeline) release(b *ollamaBackend) {
    p.mu.Lock()
    b.inUse = false
    p.mu.Unlock()
    p.cond.Broadcast()
}
```

### MarkUnhealthy

Called on request-time backend failures and by the health checker. Clears both flags and wakes waiters.

```go
func (p *OllamaPipeline) markUnhealthy(b *ollamaBackend) {
    p.mu.Lock()
    b.healthy = false
    b.inUse = false
    p.mu.Unlock()
    p.cond.Broadcast()
    slog.Warn("ollama: backend marked unhealthy", "url", b.baseURL)
}
```

---

## Background Health Checker

Started in `Load()`, stopped in `Close()` via a cancel context stored as `cancelHealth`.

```
unhealthy probe: every 30 seconds
healthy probe:   every 60 seconds
ping timeout:    5 seconds (GET /api/tags)
```

```go
func (p *OllamaPipeline) runHealthChecker(ctx context.Context) {
    unhealthyTicker := time.NewTicker(30 * time.Second)
    healthyTicker   := time.NewTicker(60 * time.Second)
    defer unhealthyTicker.Stop()
    defer healthyTicker.Stop()

    for {
        select {
        case <-ctx.Done():
            return
        case <-unhealthyTicker.C:
            p.probeUnhealthy()
        case <-healthyTicker.C:
            p.probeHealthy()
        }
    }
}
```

**probeUnhealthy** — iterates `!healthy` backends, pings each with a 5s timeout. On success: sets `healthy = true`, broadcasts, logs `slog.Info("ollama: backend recovered", "url", ...)`.

**probeHealthy** — iterates `healthy` backends (regardless of `inUse`), pings each. On failure: calls `markUnhealthy(b)` (already logs and broadcasts).

Probes do not hold the mutex during the HTTP request — they snapshot the target backends first (under lock), then probe outside the lock, then update state (under lock).

---

## Request-Time Failover

`Extract`, `Digest`, and `ChatStream` wrap their core logic in a retry loop. On a backend error classified as a backend availability issue, the backend is marked unhealthy and the loop retries by re-acquiring.

```go
for {
    b, err := p.acquire(ctx)
    if err != nil {
        return nil, err  // context cancelled / shutdown
    }
    result, err := p.doWork(ctx, b, ...)
    if err != nil {
        if isBackendError(err) {
            p.markUnhealthy(b)
            continue  // retry on next healthy backend (blocks if none available)
        }
        p.release(b)
        return nil, err  // model/request error — no retry
    }
    p.release(b)
    return result, nil
}
```

### `isBackendError`

Returns `true` for:
- `net.Error` (connection refused, dial timeout, read timeout)
- HTTP 5xx responses

Returns `false` for:
- HTTP 4xx (bad request, model not found — retrying won't help)
- JSON parse errors
- Model-level errors (empty response, failed coordinate parsing)

---

## Startup Behaviour Change

`Load()` currently fails if any backend is unreachable. Under this design, `Load()` marks unreachable backends as `healthy: false` and continues — allowing startup even when some machines are offline. The health checker will bring them online when they recover. If all backends are unreachable at startup, the first request blocks until one recovers.

---

## Concurrency Limit

`MaxConcurrency()` previously returned `cap(pool)` (total backend count). It now returns the number of currently `healthy` backends (under lock), so the worker manager does not over-schedule when backends are offline.

---

## Files Changed

- `internal/inference/ollama.go` — all changes live here; no new files needed

## Removed

- `acquireBlocking()` — replaced by `acquire(context.Background())` at the two call sites that previously used it
