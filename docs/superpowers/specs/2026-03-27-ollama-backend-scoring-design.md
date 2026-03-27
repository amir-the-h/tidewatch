# Ollama Backend Performance Scoring & Smart Routing Design

**Date:** 2026-03-27
**Status:** Approved

## Problem

The Ollama backend pool currently selects the first healthy+idle backend for all task types. When one machine has a significantly faster GPU than another (e.g. a desktop vs a MacBook), expensive long-running tasks like digest generation and chat streaming are just as likely to land on the slower machine, making them unnecessarily slow.

## Goals

- Automatically discover relative backend performance through real request outcomes
- Route digest and chat tasks to the fastest available backend
- Keep extraction using fair round-robin (throughput matters more than latency there)
- Prefer the fastest backend for heavy tasks but fall back quickly if it's busy

## Approach: EMA Score Per Backend + `acquireFastest`

Each backend tracks a `score float64` (tokens/sec) maintained as an exponential moving average. Scores are seeded at startup via a one-time benchmark and updated continuously from real request completions. A new `acquireFastest` acquire path waits briefly for the best backend and falls back to the fastest currently-idle one.

---

## State Changes

### `ollamaBackend`

```go
type ollamaBackend struct {
    baseURL string
    client  *http.Client
    healthy bool
    inUse   bool
    score   float64  // tokens/sec, EMA; 0 = not yet measured
}
```

### `OllamaPipeline`

Add one tunable:

```go
fastWaitTimeout time.Duration  // how long acquireFastest waits for best backend; default 5s
```

---

## Benchmarking & Score Updates

### Initial benchmark at `Load()`

After the health-ping pass, `Load()` benchmarks every healthy backend by sending a minimal fixed prompt to `/api/generate` (stream: false) and reading `eval_count` / `eval_duration` from the response:

```go
func (p *OllamaPipeline) benchmark(b *ollamaBackend) (float64, error) {
    // POST /api/generate: {"model": p.model, "prompt": "Hi", "stream": false}
    // returns: float64(eval_count) / (float64(eval_duration) / 1e9)  // tokens/sec
}
```

This seeds each backend's score before any real traffic arrives. Benchmark failure is non-fatal — the backend stays healthy with `score = 0` and will be scored once real requests complete.

### EMA update (α = 0.3)

```go
func (b *ollamaBackend) updateScore(tokensPerSec float64) {
    if b.score == 0 {
        b.score = tokensPerSec  // first measurement — seed directly
    } else {
        b.score = 0.3*tokensPerSec + 0.7*b.score
    }
}
```

α = 0.3 weights recent observations meaningfully while smoothing noise. A backend that throttles will show a declining score within a few requests.

### No periodic re-benchmarking

Scores are updated only from real request completions — no synthetic traffic after startup. This avoids forcing model loads on idle backends.

### Score extraction from responses

Ollama includes `eval_count` and `eval_duration` (nanoseconds) in every completed response. A shared struct reads them:

```go
type ollamaEval struct {
    EvalCount    int   `json:"eval_count"`
    EvalDuration int64 `json:"eval_duration"`
}
```

After each `Extract`, `Digest`, and `ChatStream` response, compute:

```go
tokensPerSec := float64(eval.EvalCount) / (float64(eval.EvalDuration) / 1e9)
p.mu.Lock()
b.updateScore(tokensPerSec)
p.mu.Unlock()
```

**`ChatStream` note:** `eval_count`/`eval_duration` appear in the final streaming chunk (when `done: true`). Parse that chunk and update the score before releasing the backend.

---

## `acquireFastest`

Used by `Digest` and `ChatStream`. Prefers the highest-scoring healthy backend; waits up to `fastWaitTimeout` for it to free up before falling back to the fastest currently-idle backend.

```go
func (p *OllamaPipeline) acquireFastest(ctx context.Context) (*ollamaBackend, error) {
    stop := context.AfterFunc(ctx, func() { p.cond.Broadcast() })
    defer stop()

    deadline := time.Now().Add(p.fastWaitTimeout)
    timer := time.AfterFunc(p.fastWaitTimeout, func() { p.cond.Broadcast() })
    defer timer.Stop()

    p.mu.Lock()
    defer p.mu.Unlock()
    for {
        best := p.bestHealthy()   // highest score among healthy backends
        if best != nil && !best.inUse {
            best.inUse = true
            return best, nil
        }
        if best != nil && time.Now().After(deadline) {
            // best is still busy after timeout — take fastest idle instead
            if b := p.fastestIdle(); b != nil {
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

**`bestHealthy()`** — returns the healthy backend with the highest score; if all scores are 0, returns the first healthy backend.

**`fastestIdle()`** — returns the healthy+idle backend with the highest score; nil if none available.

---

## Routing Changes

| Method | Acquire path | Reason |
|--------|-------------|--------|
| `Extract` | `acquire(context.Background())` | High throughput; fairness over latency |
| `Digest` | `acquireFastest(context.Background())` | Long-running; benefits from fastest backend |
| `ChatStream` | `acquireFastest(ctx)` | Latency-sensitive; user is waiting for stream |
| `EstimateTokens` | `acquire(ctx)` | Lightweight; no preference needed |

---

## Configuration

New key under the `ollama` config section:

```yaml
ollama:
  fast_wait_timeout: 5s  # default; how long to wait for best backend before falling back
```

Env override: `IRAN_WATCHER_OLLAMA_FAST_WAIT_TIMEOUT`.

---

## Files Changed

- `internal/inference/ollama.go` — all implementation changes
- `internal/config/config.go` — add `FastWaitTimeout time.Duration` field
- `configs/iran-watcher.yaml` — document new config key
