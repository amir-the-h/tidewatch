# Per-Action Queue Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Replace the single `iran-watcher:tasks` Redis list with per-action keys (`iran-watcher:tasks:{action_id}`) so workers only pop from queues whose pipeline can be immediately acquired.

**Architecture:** Workers call `buildReadyKeys()` before each BRPOP — it iterates the ActionCache and includes an action's key only if its pipeline is available (ONNX/internal/notification: always; Ollama: only when a backend is healthy+idle). `PopTask` accepts variadic keys and passes them straight to `BRPOP`. An optional `AvailabilityChecker` interface on pipelines drives the per-key availability decision without exposing `BackendPool` outside the inference package.

**Tech Stack:** Go 1.25.5, `go-redis/v9`, `sync.Cond` (existing in `BackendPool`), Vue 3 + Naive UI (frontend).

---

## File Map

| File | Change |
|------|--------|
| `internal/queue/queue.go` | Remove `QueueTasks`/`QueueRaw`; add `QueueTasksPrefix`, `QueueTasksKey`; update `Consumer` interface |
| `internal/queue/redis.go` | `PushTask` → route by `task.ActionID`; `PopTask` → variadic keys; `FlushQueue` → SCAN pattern; add `TotalTaskLen` |
| `internal/queue/queue_test.go` | Update `fakeRedisClient`, update existing `PopTask` calls to pass keys, add new tests |
| `internal/inference/pipeline.go` | Add `AvailabilityChecker` interface |
| `internal/inference/backend_pool.go` | Add private `isAnyAvailable`, public `IsAnyAvailable`, `WaitForAvailability` |
| `internal/inference/ollama.go` | Add `IsAvailable()` on `OllamaPipeline` |
| `internal/inference/backend_pool_test.go` | Add tests for `IsAnyAvailable` and `WaitForAvailability` |
| `internal/worker/action_cache.go` | Add `Pool()` accessor |
| `internal/worker/manager.go` | Add `buildReadyKeys`; replace `runWorker` loop |
| `internal/worker/worker.go` | Update legacy `run` to not call `PopTask` (no-op) |
| `internal/worker/worker_test.go` | Update `mockConsumer.PopTask` signature; fix affected tests |
| `internal/api/server.go` | Add `TotalTaskLen` to `queueSizer`; register `GET /api/queue` route |
| `internal/api/handlers_queue.go` | New file: `handleGetQueue` |
| `internal/api/handlers_health.go` | Switch `queue.pending` to `TotalTaskLen` |
| `web/src/App.vue` | Change nav key from `/failures` to `/queue` |
| `web/src/router/index.ts` | Rename route; add `/failures` redirect |
| `web/src/api/queue.ts` | New file: `getQueue`, `flushQueue` |
| `web/src/api/failures.ts` | Remove `flushQueue` (moved to `queue.ts`) |
| `web/src/views/QueueView.vue` | New file (replaces `FailuresView.vue`): Queues table + dead-letter |
| `web/src/views/FailuresView.vue` | Delete |
| `CLAUDE.md` | Update Key Design Decisions |

---

## Task 1: Queue constants & Consumer interface

**Files:**
- Modify: `internal/queue/queue.go`

- [ ] **Step 1: Write the failing test** (compile-time contract check — verifies new constants and signature exist)

Add to `internal/queue/queue_test.go` after the existing tests:

```go
// TestQueueTasksKey_Format verifies the per-action key format.
func TestQueueTasksKey_Format(t *testing.T) {
	key := QueueTasksKey(42)
	want := "iran-watcher:tasks:42"
	if key != want {
		t.Errorf("QueueTasksKey(42) = %q, want %q", key, want)
	}
}

// TestQueueTasksKey_Zero verifies key for action ID 0.
func TestQueueTasksKey_Zero(t *testing.T) {
	key := QueueTasksKey(0)
	want := "iran-watcher:tasks:0"
	if key != want {
		t.Errorf("QueueTasksKey(0) = %q, want %q", key, want)
	}
}
```

- [ ] **Step 2: Run tests to confirm they fail**

```bash
go test ./internal/queue/... -run TestQueueTasksKey -v
```
Expected: compile error — `QueueTasksKey undefined`

- [ ] **Step 3: Replace `internal/queue/queue.go`** with the following:

```go
package queue

import (
	"context"
	"encoding/json"
	"strconv"
	"time"
)

const (
	// QueueTasksPrefix is the key prefix for per-action task queues.
	// Full key: QueueTasksPrefix + strconv.FormatInt(actionID, 10)
	QueueTasksPrefix = "iran-watcher:tasks:"

	// QueueFailed is the dead-letter queue key.
	QueueFailed = "iran-watcher:failed"
)

// QueueTasksKey returns the Redis key for the task queue of the given action.
func QueueTasksKey(actionID int64) string {
	return QueueTasksPrefix + strconv.FormatInt(actionID, 10)
}

// Task is a single unit of work pushed to iran-watcher:tasks:{action_id}.
type Task struct {
	TriggerType string          `json:"trigger_type"`
	Event       json.RawMessage `json:"event"`
	ActionID    int64           `json:"action_id"`
	ChainNodeID int64           `json:"chain_node_id,omitempty"`
	ChainID     int64           `json:"chain_id,omitempty"`
	Context     map[string]any  `json:"context"`
}

// TweetEvent is the Event payload for trigger_type=new_tweet.
type TweetEvent struct {
	TweetID      string  `json:"tweet_id"`
	Text         string  `json:"text"`
	AuthorHandle string  `json:"author_handle"`
	RuleIDs      []int64 `json:"rule_ids"`
}

// DigestEvent is the Event payload for trigger_type=digest_requested.
type DigestEvent struct {
	DigestJobID string `json:"digest_job_id"`
}

// FailedTweet wraps a Task with error annotations for dead-letter storage.
type FailedTweet struct {
	Task         *Task         `json:"task"`
	FailedModels []FailedModel `json:"failed_models"`
}

// FailedModel records which pipeline failed and why.
type FailedModel struct {
	Name      string    `json:"name"`
	Error     string    `json:"error"`
	Timestamp time.Time `json:"timestamp"`
}

// Producer pushes tasks onto a queue.
type Producer interface {
	PushTask(ctx context.Context, task *Task) error
	PushFailed(ctx context.Context, ft *FailedTweet) error
}

// Consumer pops tasks from a queue.
// PopTask accepts one or more queue keys and performs a BRPOP across all of
// them. Returns (nil, nil) on timeout or when keys is empty.
type Consumer interface {
	PopTask(ctx context.Context, timeout time.Duration, keys ...string) (*Task, error)
	PopFailed(ctx context.Context, timeout time.Duration) (*FailedTweet, error)
}

// Sizer reports the number of items in a named queue.
type Sizer interface {
	Len(ctx context.Context, queue string) (int64, error)
}

// FailureLister enumerates and clears dead-letter queue entries.
type FailureLister interface {
	ListFailed(ctx context.Context, offset, limit int64) ([]*FailedTweet, error)
	ClearFailed(ctx context.Context) error
	FlushQueue(ctx context.Context) error
}
```

- [ ] **Step 4: Run tests**

```bash
go test ./internal/queue/... -run TestQueueTasksKey -v
```
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/queue/queue.go internal/queue/queue_test.go
git commit -m "feat: per-action queue keys and updated Consumer interface"
```

---

## Task 2: RedisQueue implementation

**Files:**
- Modify: `internal/queue/redis.go`
- Modify: `internal/queue/queue_test.go`

- [ ] **Step 1: Update `fakeRedisClient` and existing tests**

In `internal/queue/queue_test.go`, update the `fakeRedisClient.BRPop` signature (variadic keys — it already uses `_ ...string` so just verify it compiles). Update the two `PopTask` calls that pass no keys:

```go
// Replace:
task, err := q.PopTask(context.Background(), 1*time.Second)
// With:
task, err := q.PopTask(context.Background(), 1*time.Second, "iran-watcher:tasks:1")
```

Do this for both `TestPopTask_ReturnsNilOnTimeout` and `TestPopTask_ReturnsNilOnEmptyResult`.

Also add a test for the empty-keys early-return:

```go
func TestPopTask_EmptyKeys_ReturnsNil(t *testing.T) {
	q := &RedisQueue{client: &fakeRedisClient{}}
	task, err := q.PopTask(context.Background(), 1*time.Second)
	if err != nil {
		t.Fatalf("expected nil error with no keys, got: %v", err)
	}
	if task != nil {
		t.Fatalf("expected nil task with no keys, got: %+v", task)
	}
}
```

Add a test for routing by ActionID:

```go
type pushCaptureFakeClient struct {
	fakeRedisClient
	pushedKey string
	pushedVal string
}

func (f *pushCaptureFakeClient) LPush(_ context.Context, key string, values ...interface{}) *redis.IntCmd {
	f.pushedKey = key
	if len(values) > 0 {
		if b, ok := values[0].([]byte); ok {
			f.pushedVal = string(b)
		}
	}
	return redis.NewIntResult(1, nil)
}

func TestPushTask_RoutesToActionKey(t *testing.T) {
	fake := &pushCaptureFakeClient{}
	q := &RedisQueue{client: fake}

	task := &Task{
		TriggerType: "new_tweet",
		ActionID:    7,
		Context:     map[string]any{},
	}
	if err := q.PushTask(context.Background(), task); err != nil {
		t.Fatalf("PushTask: %v", err)
	}
	want := "iran-watcher:tasks:7"
	if fake.pushedKey != want {
		t.Errorf("pushed to key %q, want %q", fake.pushedKey, want)
	}
}
```

- [ ] **Step 2: Run tests to confirm they fail**

```bash
go test ./internal/queue/... -v
```
Expected: compile errors — `PopTask` signature mismatch, `pushCaptureFakeClient` won't satisfy `redisClient` without the right methods.

- [ ] **Step 3: Rewrite `internal/queue/redis.go`**

```go
package queue

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/redis/go-redis/v9"
)

// redisClient is the narrow subset of redis.Client methods used by RedisQueue.
type redisClient interface {
	LPush(ctx context.Context, key string, values ...interface{}) *redis.IntCmd
	BRPop(ctx context.Context, timeout time.Duration, keys ...string) *redis.StringSliceCmd
	LLen(ctx context.Context, key string) *redis.IntCmd
	LRange(ctx context.Context, key string, start, stop int64) *redis.StringSliceCmd
	Del(ctx context.Context, keys ...string) *redis.IntCmd
}

// RedisQueue implements Producer, Consumer, Sizer, and FailureLister using Redis lists.
type RedisQueue struct {
	client redisClient
}

// NewRedisQueue constructs a RedisQueue connected to the given Redis address.
func NewRedisQueue(addr, password string, db int) *RedisQueue {
	client := redis.NewClient(&redis.Options{
		Addr:     addr,
		Password: password,
		DB:       db,
	})
	return &RedisQueue{client: client}
}

// PushTask serializes a Task to JSON and LPUSHes it onto the per-action queue
// iran-watcher:tasks:{task.ActionID}.
func (q *RedisQueue) PushTask(ctx context.Context, task *Task) error {
	b, err := json.Marshal(task)
	if err != nil {
		return fmt.Errorf("queue: marshal task: %w", err)
	}
	key := QueueTasksKey(task.ActionID)
	if err := q.client.LPush(ctx, key, b).Err(); err != nil {
		return fmt.Errorf("queue: push task: %w", err)
	}
	return nil
}

// PopTask blocks until a Task is available on one of the given keys or the
// timeout elapses. Returns (nil, nil) on timeout or when keys is empty.
func (q *RedisQueue) PopTask(ctx context.Context, timeout time.Duration, keys ...string) (*Task, error) {
	if len(keys) == 0 {
		return nil, nil
	}
	res, err := q.client.BRPop(ctx, timeout, keys...).Result()
	if err != nil {
		if errors.Is(err, redis.Nil) {
			return nil, nil
		}
		return nil, fmt.Errorf("queue: pop task: %w", err)
	}
	if len(res) < 2 {
		return nil, nil
	}
	var t Task
	if err := json.Unmarshal([]byte(res[1]), &t); err != nil {
		return nil, fmt.Errorf("queue: pop task: unmarshal: %w", err)
	}
	return &t, nil
}

// PushFailed pushes a FailedTweet to the dead-letter queue.
func (q *RedisQueue) PushFailed(ctx context.Context, ft *FailedTweet) error {
	data, err := json.Marshal(ft)
	if err != nil {
		return fmt.Errorf("queue: push failed: marshal: %w", err)
	}
	if err := q.client.LPush(ctx, QueueFailed, data).Err(); err != nil {
		return fmt.Errorf("queue: push failed: %w", err)
	}
	return nil
}

// PopFailed pops a FailedTweet from the dead-letter queue.
// Returns (nil, nil) on timeout.
func (q *RedisQueue) PopFailed(ctx context.Context, timeout time.Duration) (*FailedTweet, error) {
	result, err := q.client.BRPop(ctx, timeout, QueueFailed).Result()
	if err != nil {
		if errors.Is(err, redis.Nil) {
			return nil, nil
		}
		return nil, fmt.Errorf("queue: pop failed: %w", err)
	}
	if len(result) < 2 {
		return nil, nil
	}
	var ft FailedTweet
	if err := json.Unmarshal([]byte(result[1]), &ft); err != nil {
		return nil, fmt.Errorf("queue: pop failed: unmarshal: %w", err)
	}
	return &ft, nil
}

// Len returns the number of items currently in the named queue.
func (q *RedisQueue) Len(ctx context.Context, queue string) (int64, error) {
	n, err := q.client.LLen(ctx, queue).Result()
	if err != nil {
		return 0, fmt.Errorf("queue: len: %w", err)
	}
	return n, nil
}

// TotalTaskLen sums the lengths of all per-action task queues by scanning for
// keys matching iran-watcher:tasks:*. Returns 0 if the underlying client does
// not support SCAN (e.g. test fakes).
func (q *RedisQueue) TotalTaskLen(ctx context.Context) (int64, error) {
	type scanner interface {
		Scan(context.Context, uint64, string, int64) *redis.ScanCmd
	}
	s, ok := q.client.(scanner)
	if !ok {
		return 0, nil
	}
	var total int64
	var cursor uint64
	for {
		keys, next, err := s.Scan(ctx, cursor, QueueTasksPrefix+"*", 100).Result()
		if err != nil {
			return 0, fmt.Errorf("queue: total task len: scan: %w", err)
		}
		for _, key := range keys {
			n, err := q.client.LLen(ctx, key).Result()
			if err == nil {
				total += n
			}
		}
		if next == 0 {
			break
		}
		cursor = next
	}
	return total, nil
}

// ListFailed returns up to limit FailedTweet entries starting at offset
// without consuming them from the queue.
func (q *RedisQueue) ListFailed(ctx context.Context, offset, limit int64) ([]*FailedTweet, error) {
	stop := offset + limit - 1
	items, err := q.client.LRange(ctx, QueueFailed, offset, stop).Result()
	if err != nil {
		return nil, fmt.Errorf("queue: list failed: %w", err)
	}
	result := make([]*FailedTweet, 0, len(items))
	for _, item := range items {
		var ft FailedTweet
		if err := json.Unmarshal([]byte(item), &ft); err != nil {
			continue
		}
		result = append(result, &ft)
	}
	return result, nil
}

// ClearFailed deletes all entries from the dead-letter queue.
func (q *RedisQueue) ClearFailed(ctx context.Context) error {
	if err := q.client.Del(ctx, QueueFailed).Err(); err != nil {
		return fmt.Errorf("queue: clear failed: %w", err)
	}
	return nil
}

// FlushQueue deletes all per-action task queues by scanning for keys matching
// iran-watcher:tasks:*. No-op if the underlying client does not support SCAN.
func (q *RedisQueue) FlushQueue(ctx context.Context) error {
	type scanner interface {
		Scan(context.Context, uint64, string, int64) *redis.ScanCmd
	}
	s, ok := q.client.(scanner)
	if !ok {
		return nil
	}
	var cursor uint64
	for {
		keys, next, err := s.Scan(ctx, cursor, QueueTasksPrefix+"*", 100).Result()
		if err != nil {
			return fmt.Errorf("queue: flush scan: %w", err)
		}
		if len(keys) > 0 {
			if err := q.client.Del(ctx, keys...).Err(); err != nil {
				return fmt.Errorf("queue: flush delete: %w", err)
			}
		}
		if next == 0 {
			break
		}
		cursor = next
	}
	return nil
}
```

- [ ] **Step 4: Run tests**

```bash
go test ./internal/queue/... -v
```
Expected: all PASS

- [ ] **Step 5: Commit**

```bash
git add internal/queue/redis.go internal/queue/queue_test.go
git commit -m "feat: per-action queue routing, variadic PopTask, TotalTaskLen, SCAN-based FlushQueue"
```

---

## Task 3: AvailabilityChecker interface + BackendPool + OllamaPipeline

**Files:**
- Modify: `internal/inference/pipeline.go`
- Modify: `internal/inference/backend_pool.go`
- Modify: `internal/inference/ollama.go`
- Modify: `internal/inference/backend_pool_test.go`

- [ ] **Step 1: Write the failing tests**

Add to `internal/inference/backend_pool_test.go`:

```go
func TestBackendPool_IsAnyAvailable_TrueWhenIdleBackend(t *testing.T) {
	p := NewBackendPool()
	defer p.Close()
	p.Load([]BackendRecord{{ID: 1, Name: "b1", URL: "http://localhost:11434", Enabled: true}})

	if !p.IsAnyAvailable(nil) {
		t.Fatal("expected backend to be available after Load")
	}
}

func TestBackendPool_IsAnyAvailable_FalseWhenAllInUse(t *testing.T) {
	p := NewBackendPool()
	defer p.Close()
	p.Load([]BackendRecord{{ID: 1, Name: "b1", URL: "http://localhost:11434", Enabled: true}})

	p.mu.Lock()
	p.backends[1].inUse = true
	p.mu.Unlock()

	if p.IsAnyAvailable(nil) {
		t.Fatal("expected no backend when all in use")
	}
}

func TestBackendPool_IsAnyAvailable_RespectAssignedIDs(t *testing.T) {
	p := NewBackendPool()
	defer p.Close()
	p.Load([]BackendRecord{
		{ID: 1, Name: "b1", URL: "http://b1:11434", Enabled: true},
		{ID: 2, Name: "b2", URL: "http://b2:11434", Enabled: true},
	})

	p.mu.Lock()
	p.backends[1].inUse = true
	p.mu.Unlock()

	// Full pool: backend 2 is still available.
	if !p.IsAnyAvailable(nil) {
		t.Fatal("expected full pool to have an available backend")
	}
	// Assigned to backend 1 only: unavailable.
	if p.IsAnyAvailable([]int64{1}) {
		t.Fatal("expected assigned backend 1 to be unavailable")
	}
	// Assigned to backend 2 only: available.
	if !p.IsAnyAvailable([]int64{2}) {
		t.Fatal("expected assigned backend 2 to be available")
	}
}

func TestBackendPool_WaitForAvailability_ReturnsWhenBackendReleased(t *testing.T) {
	p := NewBackendPool()
	defer p.Close()
	p.Load([]BackendRecord{{ID: 1, Name: "b1", URL: "http://localhost:11434", Enabled: true}})

	b := p.Get(1)
	p.mu.Lock()
	b.inUse = true
	p.mu.Unlock()

	// Release the backend after 50ms.
	go func() {
		time.Sleep(50 * time.Millisecond)
		p.Release(b)
	}()

	ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer cancel()

	start := time.Now()
	p.WaitForAvailability(ctx, 500*time.Millisecond, nil)
	elapsed := time.Since(start)

	if elapsed < 40*time.Millisecond {
		t.Errorf("returned too fast: %v (want ≥40ms)", elapsed)
	}
	if elapsed > 300*time.Millisecond {
		t.Errorf("returned too slow: %v (want ≤300ms)", elapsed)
	}
}

func TestBackendPool_WaitForAvailability_RespectsTimeout(t *testing.T) {
	p := NewBackendPool()
	defer p.Close()
	p.Load([]BackendRecord{{ID: 1, Name: "b1", URL: "http://localhost:11434", Enabled: true}})

	b := p.Get(1)
	p.mu.Lock()
	b.inUse = true
	p.mu.Unlock()

	start := time.Now()
	p.WaitForAvailability(context.Background(), 100*time.Millisecond, nil)
	elapsed := time.Since(start)

	if elapsed < 80*time.Millisecond {
		t.Errorf("returned too fast: %v (want ≥80ms)", elapsed)
	}
	if elapsed > 400*time.Millisecond {
		t.Errorf("returned too slow: %v (want ≤400ms)", elapsed)
	}
}
```

- [ ] **Step 2: Run tests to confirm they fail**

```bash
go test ./internal/inference/... -run "TestBackendPool_IsAnyAvailable|TestBackendPool_WaitForAvailability" -v
```
Expected: compile error — `IsAnyAvailable` and `WaitForAvailability` undefined

- [ ] **Step 3: Add `AvailabilityChecker` to `internal/inference/pipeline.go`**

Add after the `ConcurrencyLimiter` interface block:

```go
// AvailabilityChecker is implemented by pipelines whose execution requires
// a scarce resource (e.g. an Ollama backend). Pipelines that do not implement
// this interface are treated as always available by the worker.
type AvailabilityChecker interface {
	// IsAvailable reports whether the pipeline can be acquired right now
	// without blocking (non-blocking check).
	IsAvailable() bool
}
```

- [ ] **Step 4: Add `isAnyAvailable`, `IsAnyAvailable`, `WaitForAvailability` to `internal/inference/backend_pool.go`**

Add before the `runHealthChecker` function:

```go
// isAnyAvailable reports whether any backend matching assignedIDs is healthy,
// enabled, and not in use. Empty/nil assignedIDs checks the full pool.
// Caller must hold p.mu.
func (p *BackendPool) isAnyAvailable(assignedIDs []int64) bool {
	if len(assignedIDs) == 0 {
		return p.bestEnabledIdle() != nil
	}
	for _, id := range assignedIDs {
		b, ok := p.backends[id]
		if ok && b.enabled && b.healthy && !b.inUse {
			return true
		}
	}
	return false
}

// IsAnyAvailable reports whether any backend matching assignedIDs is
// immediately acquirable (non-blocking). Empty/nil assignedIDs checks
// the full pool. Uses the same priority logic as AcquireAny / acquirePrioritized.
func (p *BackendPool) IsAnyAvailable(assignedIDs []int64) bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.isAnyAvailable(assignedIDs)
}

// WaitForAvailability blocks until a backend matching assignedIDs is
// available, the timeout elapses, or ctx is cancelled.
// Empty/nil assignedIDs waits for any backend in the full pool.
func (p *BackendPool) WaitForAvailability(ctx context.Context, timeout time.Duration, assignedIDs []int64) {
	deadline := time.Now().Add(timeout)
	timer := time.AfterFunc(timeout, func() { p.cond.Broadcast() })
	defer timer.Stop()
	watchCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	go func() { <-watchCtx.Done(); p.cond.Broadcast() }()

	p.mu.Lock()
	defer p.mu.Unlock()
	for {
		if ctx.Err() != nil || time.Now().After(deadline) {
			return
		}
		if p.isAnyAvailable(assignedIDs) {
			return
		}
		p.cond.Wait()
	}
}
```

- [ ] **Step 5: Add `IsAvailable` to `OllamaPipeline` in `internal/inference/ollama.go`**

Add after the `MaxConcurrency` method:

```go
// IsAvailable reports whether a backend is immediately acquirable for this
// pipeline. Respects priority-ordered assignedIDs; empty slice falls back to
// the full pool sorted by score. Satisfies inference.AvailabilityChecker.
func (p *OllamaPipeline) IsAvailable() bool {
	return p.pool.IsAnyAvailable(p.assignedIDs)
}
```

- [ ] **Step 6: Run tests**

```bash
go test ./internal/inference/... -v
```
Expected: all PASS

- [ ] **Step 7: Commit**

```bash
git add internal/inference/pipeline.go internal/inference/backend_pool.go \
        internal/inference/ollama.go internal/inference/backend_pool_test.go
git commit -m "feat: AvailabilityChecker interface, BackendPool.IsAnyAvailable/WaitForAvailability, OllamaPipeline.IsAvailable"
```

---

## Task 4: Worker layer — ActionCache, Manager, legacy worker

**Files:**
- Modify: `internal/worker/action_cache.go`
- Modify: `internal/worker/manager.go`
- Modify: `internal/worker/worker.go`
- Modify: `internal/worker/worker_test.go`

- [ ] **Step 1: Update `mockConsumer.PopTask` signature in `internal/worker/worker_test.go`**

```go
// Replace:
func (m *mockConsumer) PopTask(_ context.Context, _ time.Duration) (*queue.Task, error) {
// With:
func (m *mockConsumer) PopTask(_ context.Context, _ time.Duration, _ ...string) (*queue.Task, error) {
```

- [ ] **Step 2: Run tests to confirm existing manager tests still compile**

```bash
go test ./internal/worker/... -v 2>&1 | head -30
```
Expected: compile error — `Manager.runWorker` calls old `PopTask` with no keys until we update it.

- [ ] **Step 3: Add `Pool()` accessor to `internal/worker/action_cache.go`**

Add after the `FindByType` method:

```go
// Pool returns the BackendPool used by this cache.
// Used by Manager to wait for Ollama backend availability when no ready
// queue keys are available.
func (c *ActionCache) Pool() *inference.BackendPool {
	return c.pool
}
```

- [ ] **Step 4: Replace `runWorker` and add `buildReadyKeys` in `internal/worker/manager.go`**

Replace the `runWorker` function entirely:

```go
func (m *Manager) runWorker(ctx context.Context, id int) {
	for {
		select {
		case <-ctx.Done():
			return
		default:
		}

		keys := m.buildReadyKeys()
		if len(keys) == 0 {
			// No active actions, or all Ollama backends are busy.
			// Wait until a backend becomes available or the timeout fires,
			// then re-evaluate. Nil pool guard handles test environments.
			if pool := m.cache.Pool(); pool != nil {
				pool.WaitForAvailability(ctx, 1*time.Second, nil)
			} else {
				select {
				case <-ctx.Done():
					return
				case <-time.After(100 * time.Millisecond):
				}
			}
			continue
		}

		task, err := m.consumer.PopTask(ctx, 1*time.Second, keys...)
		if err != nil {
			if m.logger != nil {
				m.logger.Error("worker: pop task", "worker_id", id, "error", err)
			}
			continue
		}
		if task == nil {
			continue
		}
		m.processTask(ctx, id, task)
	}
}

// buildReadyKeys returns the Redis queue keys for all active actions whose
// pipeline is currently acquirable. ONNX, internal, and notification
// pipelines are always included. Ollama pipelines are included only when
// at least one assigned (or pool-wide) backend is healthy, enabled, and idle.
func (m *Manager) buildReadyKeys() []string {
	entries := m.cache.All()
	keys := make([]string, 0, len(entries))
	for _, e := range entries {
		if av, ok := e.Pipeline.(inference.AvailabilityChecker); ok {
			if av.IsAvailable() {
				keys = append(keys, queue.QueueTasksKey(e.Action.ID))
			}
		} else {
			// No AvailabilityChecker → always available (ONNX, internal, notification).
			keys = append(keys, queue.QueueTasksKey(e.Action.ID))
		}
	}
	return keys
}
```

Also add `"github.com/amir-the-h/iran-watcher/internal/inference"` to the import block of `manager.go` if it's not already there (it already is).

- [ ] **Step 5: Update legacy `worker.run` in `internal/worker/worker.go`**

Replace the entire `run` method:

```go
func (w *worker) run(ctx context.Context) {
	// Legacy pool worker: processing is a no-op — use Manager instead.
	if w.logger != nil {
		w.logger.Warn("worker (legacy pool): started — use Manager for full processing")
	}
	<-ctx.Done()
}
```

- [ ] **Step 6: Run tests**

```bash
go test ./internal/worker/... -v
```
Expected: all PASS. Note: `TestManager_UnknownActionID_Skipped` and `TestManager_Start_RespectsCtxCancel` run with empty caches — they will complete within their 2-second deadlines without finding any tasks to process, then cancel.

- [ ] **Step 7: Commit**

```bash
git add internal/worker/action_cache.go internal/worker/manager.go \
        internal/worker/worker.go internal/worker/worker_test.go
git commit -m "feat: per-action queue worker — buildReadyKeys, AvailabilityChecker-gated BRPOP, Pool accessor"
```

---

## Task 5: API — GET /api/queue, health update, route registration

**Files:**
- Modify: `internal/api/server.go`
- Create: `internal/api/handlers_queue.go`
- Modify: `internal/api/handlers_health.go`

- [ ] **Step 1: Extend `queueSizer` and register route in `internal/api/server.go`**

Change the `queueSizer` interface:

```go
// queueSizer can report queue sizes.
type queueSizer interface {
	Len(ctx context.Context, queue string) (int64, error)
	// TotalTaskLen returns the sum of all per-action task queue lengths.
	TotalTaskLen(ctx context.Context) (int64, error)
}
```

Add the route inside the admin-only group, after the existing `r.Delete("/queue", ...)` line:

```go
r.Get("/queue", srv.handleGetQueue)
```

- [ ] **Step 2: Create `internal/api/handlers_queue.go`**

```go
package api

import (
	"net/http"
	"sort"

	"github.com/amir-the-h/iran-watcher/internal/queue"
)

type queueEntry struct {
	ActionID     int64      `json:"action_id"`
	ActionName   string     `json:"action_name"`
	PipelineName string     `json:"pipeline_name"`
	PipelineType string     `json:"pipeline_type"`
	Pending      int64      `json:"pending"`
	Worker       *workerRef `json:"worker"`
}

type workerRef struct {
	ID      int    `json:"id"`
	Backend string `json:"backend,omitempty"`
}

func (s *Server) handleGetQueue(w http.ResponseWriter, r *http.Request) {
	if s.actionCache == nil {
		writeJSON(w, http.StatusOK, map[string]any{"queues": []queueEntry{}})
		return
	}

	entries := s.actionCache.All()

	// Build worker lookup: action name → WorkerInfo.
	workerByAction := make(map[string]workerRef)
	if s.pool != nil {
		for _, wi := range s.pool.Stats().ActiveWorkers {
			workerByAction[wi.Action] = workerRef{ID: wi.ID, Backend: wi.Backend}
		}
	}

	result := make([]queueEntry, 0, len(entries))
	for _, e := range entries {
		var pending int64
		if s.sizer != nil {
			pending, _ = s.sizer.Len(r.Context(), queue.QueueTasksKey(e.Action.ID))
		}
		qe := queueEntry{
			ActionID:     e.Action.ID,
			ActionName:   e.Action.Name,
			PipelineName: e.Pipeline.Name(),
			PipelineType: string(e.Pipeline.Type()),
			Pending:      pending,
		}
		if ref, ok := workerByAction[e.Action.Name]; ok {
			qe.Worker = &ref
		}
		result = append(result, qe)
	}

	// Sort: rows with activity (worker assigned or pending > 0) first,
	// then by pending desc, then alphabetically by action name.
	sort.Slice(result, func(i, j int) bool {
		ri, rj := result[i], result[j]
		iActive := ri.Worker != nil || ri.Pending > 0
		jActive := rj.Worker != nil || rj.Pending > 0
		if iActive != jActive {
			return iActive
		}
		if ri.Pending != rj.Pending {
			return ri.Pending > rj.Pending
		}
		return ri.ActionName < rj.ActionName
	})

	writeJSON(w, http.StatusOK, map[string]any{"queues": result})
}
```

- [ ] **Step 3: Update health handler in `internal/api/handlers_health.go`**

Replace the `s.sizer` block:

```go
if s.sizer != nil {
	pending, _ := s.sizer.TotalTaskLen(r.Context())
	failed, _ := s.sizer.Len(r.Context(), queue.QueueFailed)
	resp["queue"] = map[string]any{
		"pending": pending,
		"failed":  failed,
	}
}
```

Also remove the `"github.com/amir-the-h/iran-watcher/internal/queue"` import reference to `queue.QueueTasks` since it no longer exists. The import is still needed for `queue.QueueFailed`.

- [ ] **Step 4: Build to verify compilation**

```bash
go build ./...
```
Expected: no errors. If any callers reference the removed `QueueTasks` constant, fix them now.

- [ ] **Step 5: Check for remaining references to removed constants**

```bash
grep -r "QueueTasks\b\|QueueRaw\b" --include="*.go" .
```
Expected: no matches. If any are found, replace `QueueTasks` with `QueueTasksKey(actionID)` or `QueueTasksPrefix+"*"` as appropriate, and remove `QueueRaw` references.

- [ ] **Step 5b: Fix any mock `queueSizer` in `internal/api/api_test.go`**

The `queueSizer` interface now requires `TotalTaskLen`. If `api_test.go` defines a mock that implements the old interface, it will fail to compile. Find any such struct:

```bash
grep -n "queueSizer\|func.*Len.*context\|TotalTaskLen" internal/api/api_test.go
```

If a mock exists without `TotalTaskLen`, add the method:

```go
func (m *mockQueueSizer) TotalTaskLen(_ context.Context) (int64, error) {
    return 0, nil
}
```

(Replace `mockQueueSizer` with whatever the struct is actually named in the test file.)

- [ ] **Step 6: Check for remaining references to the old `PopTask` signature (no keys)**

```bash
grep -rn "\.PopTask(ctx" --include="*.go" . | grep -v "_test.go"
```
Review each result. Any call that doesn't pass keys (e.g. from the integration test) needs updating. Pass the appropriate key(s).

- [ ] **Step 7: Commit**

```bash
git add internal/api/server.go internal/api/handlers_queue.go internal/api/handlers_health.go
git commit -m "feat: GET /api/queue per-action breakdown, health uses TotalTaskLen"
```

---

## Task 6: App.vue nav + router + api/queue.ts

**Files:**
- Modify: `web/src/App.vue`
- Modify: `web/src/router/index.ts`
- Create: `web/src/api/queue.ts`
- Modify: `web/src/api/failures.ts`

- [ ] **Step 1: Update the nav item in `web/src/App.vue`**

Find the line:
```javascript
{ label: 'Queue', key: '/failures' },
```
Change `key` to `'/queue'`:
```javascript
{ label: 'Queue', key: '/queue' },
```

- [ ] **Step 2: Update `web/src/router/index.ts`**

Replace:
```typescript
{ path: '/failures', component: () => import('../views/FailuresView.vue') },
```
With:
```typescript
{ path: '/queue', component: () => import('../views/QueueView.vue') },
{ path: '/failures', redirect: '/queue' },
```

- [ ] **Step 3: Create `web/src/api/queue.ts`**

```typescript
import { apiFetch } from './client'

export interface WorkerRef {
  id: number
  backend?: string
}

export interface QueueEntry {
  action_id: number
  action_name: string
  pipeline_name: string
  pipeline_type: 'onnx' | 'ollama' | 'internal' | 'notification'
  pending: number
  worker: WorkerRef | null
}

export interface QueueResponse {
  queues: QueueEntry[]
}

export const getQueue = () => apiFetch<QueueResponse>('/api/queue')

export const flushQueue = () =>
  apiFetch<{ flushed: boolean }>('/api/queue', { method: 'DELETE' })
```

- [ ] **Step 4: Update `web/src/api/failures.ts`**

Remove the `flushQueue` export (it now lives in `queue.ts`). The file should only export failure-related functions:

```typescript
// web/src/api/failures.ts
import { apiFetch } from './client'

export interface FailedModel {
  name: string
  error: string
  timestamp: string
}

export interface MatchingRule {
  id: string
  tag: string
}

export interface FailedTweetInner {
  id: string
  author_id: string
  author_handle: string
  text: string
  created_at: string
  fetched_at: string
}

export interface StreamTweet {
  Tweet: FailedTweetInner
  MatchingRules: MatchingRule[] | null
}

export interface FailedTweet {
  tweet: StreamTweet
  failed_models: FailedModel[]
}

export interface FailuresResponse {
  failures: FailedTweet[]
  total: number
}

export const listFailures = (offset = 0, limit = 50) =>
  apiFetch<FailuresResponse>(`/api/failures?offset=${offset}&limit=${limit}`)

export const clearFailures = () =>
  apiFetch<{ cleared: boolean }>('/api/failures', { method: 'DELETE' })
```

- [ ] **Step 5: Commit**

```bash
git add web/src/App.vue web/src/router/index.ts web/src/api/queue.ts web/src/api/failures.ts
git commit -m "feat: rename /failures route to /queue, add queue.ts API client"
```

---

## Task 7: QueueView.vue

**Files:**
- Create: `web/src/views/QueueView.vue`
- Delete: `web/src/views/FailuresView.vue`

- [ ] **Step 1: Create `web/src/views/QueueView.vue`**

```vue
<!-- web/src/views/QueueView.vue -->
<script setup lang="ts">
import { h, ref, computed, onMounted, onUnmounted } from 'vue'
import { useFailuresStore } from '../stores/failures'
import { useMessage, NButton, NSpace, NTag, NIcon } from 'naive-ui'
import type { DataTableColumns } from 'naive-ui'
import type { FailedTweet } from '../api/failures'
import { getQueue, flushQueue } from '../api/queue'
import type { QueueEntry } from '../api/queue'
import { apiFetch } from '../api/client'
import { formatDateTime } from '../utils/format'
import { InformationCircleOutline, RefreshOutline, TrashOutline, BanOutline } from '@vicons/ionicons5'
import { iconBtn } from '../utils/renderActions'

const store = useFailuresStore()
const msg   = useMessage()

// ── Stats ─────────────────────────────────────────────────────────────────────

interface QueueStats { pending: number; failed: number }
interface WorkerStats { total: number; active: number; idle: number }

const queueStats  = ref<QueueStats | null>(null)
const workerStats = ref<WorkerStats | null>(null)
const queues      = ref<QueueEntry[]>([])

interface PendingSample { t: number; v: number }
const pendingHistory = ref<PendingSample[]>([])

async function fetchStats() {
  try {
    const h = await apiFetch<any>('/api/health')
    if (h.queue) {
      queueStats.value = h.queue
      pendingHistory.value.push({ t: Date.now(), v: h.queue.pending as number })
      if (pendingHistory.value.length > 10) pendingHistory.value.shift()
    }
    if (h.workers) workerStats.value = h.workers
  } catch { /* ignore */ }
}

async function fetchQueues() {
  try {
    const data = await getQueue()
    queues.value = data.queues
  } catch { /* ignore */ }
}

const drainEta = computed<string | null>(() => {
  const hist = pendingHistory.value
  if (hist.length < 2) return null
  const first = hist[0], last = hist[hist.length - 1]
  const dt = (last.t - first.t) / 1000
  const dv = first.v - last.v
  if (dt <= 0 || dv <= 0) return null
  const rate = dv / dt
  const secs = Math.ceil(last.v / rate)
  if (secs < 60) return `~${secs}s`
  const m = Math.floor(secs / 60), s = secs % 60
  if (m < 60) return s > 0 ? `~${m}m ${s}s` : `~${m}m`
  const hr = Math.floor(m / 60), rm = m % 60
  return rm > 0 ? `~${hr}h ${rm}m` : `~${hr}h`
})

// ── Actions ───────────────────────────────────────────────────────────────────

const showDetail  = ref(false)
const selected    = ref<FailedTweet | null>(null)
const clearing    = ref(false)
const flushing    = ref(false)
const drawerWidth = computed(() => Math.min(540, window.innerWidth - 16))

function openDetail(row: FailedTweet) {
  selected.value   = row
  showDetail.value = true
}

async function onClear() {
  clearing.value = true
  try {
    await store.clear()
    msg.success('Dead-letter queue cleared')
    await fetchStats()
  } catch (e: any) { msg.error(e.message) }
  finally { clearing.value = false }
}

async function onFlush() {
  flushing.value = true
  try {
    await flushQueue()
    msg.success('Pending queue flushed')
    await Promise.all([store.fetch(), fetchStats(), fetchQueues()])
  } catch (e: any) { msg.error(e.message) }
  finally { flushing.value = false }
}

// ── Queue table ───────────────────────────────────────────────────────────────

const pipelineTagType: Record<string, 'info' | 'success' | 'default' | 'warning'> = {
  onnx: 'info',
  ollama: 'success',
  internal: 'default',
  notification: 'warning',
}

const queueColumns = [
  {
    title: 'Action',
    key: 'action_name',
    render: (row: QueueEntry) =>
      h('span', { style: { color: '#cdd6f4' } }, row.action_name),
  },
  {
    title: 'Pipeline',
    key: 'pipeline_name',
    render: (row: QueueEntry) =>
      h(NSpace, { size: 6, align: 'center' }, {
        default: () => [
          h('span', { style: { color: '#a6adc8' } }, row.pipeline_name),
          h(NTag, {
            type: pipelineTagType[row.pipeline_type] ?? 'default',
            size: 'small',
            round: true,
          }, { default: () => row.pipeline_type }),
        ],
      }),
  },
  {
    title: 'Pending',
    key: 'pending',
    width: 100,
    render: (row: QueueEntry) =>
      h('span', {
        style: {
          color: row.pending > 0 ? '#f9e2af' : '#585b70',
          fontVariantNumeric: 'tabular-nums',
        },
      }, row.pending > 0 ? String(row.pending) : '—'),
  },
  {
    title: 'Worker',
    key: 'worker',
    width: 220,
    render: (row: QueueEntry) => {
      if (!row.worker) return h('span', { style: { color: '#585b70' } }, '—')
      const { id, backend } = row.worker
      let suffix = ''
      if (backend === 'waiting') suffix = ' · waiting…'
      else if (backend) suffix = ` · ${backend.replace(/^https?:\/\//, '')}`
      return h('span', {
        style: { color: '#a6e3a1', fontVariantNumeric: 'tabular-nums' },
      }, `#${id}${suffix}`)
    },
  },
]

// ── Failures table ────────────────────────────────────────────────────────────

const failureColumns: DataTableColumns<FailedTweet> = [
  {
    title: 'Tweet',
    key: 'text',
    ellipsis: { tooltip: true },
    render: (row) => row.tweet?.Tweet?.text ?? '—',
  },
  {
    title: 'Author',
    key: 'author',
    width: 140,
    render: (row) => {
      const handle = row.tweet?.Tweet?.author_handle
      return handle ? `@${handle}` : row.tweet?.Tweet?.author_id ?? '—'
    },
  },
  {
    title: 'Failed Models',
    key: 'failed_models',
    width: 220,
    render: (row) => {
      const models = row.failed_models ?? []
      return h(NSpace, { size: 4, wrap: true }, {
        default: () => models.map(m =>
          h(NTag, { type: 'error', size: 'small', round: true }, { default: () => m.name })
        ),
      })
    },
  },
  {
    title: 'Failed At',
    key: 'timestamp',
    width: 180,
    render: (row) => {
      const ts = row.failed_models?.[0]?.timestamp
      return ts ? formatDateTime(ts) : '—'
    },
  },
  {
    title: '',
    key: 'actions',
    width: 50,
    render: (row) => iconBtn(InformationCircleOutline, 'Details', () => openDetail(row)),
  },
]

const pagination = computed(() => ({
  page:      store.page,
  pageSize:  store.perPage,
  itemCount: store.total,
  showSizePicker: true,
  pageSizes: [20, 50, 100],
  onChange:          (p: number)  => store.fetch(p),
  onUpdatePageSize:  (ps: number) => store.fetch(1, ps),
}))

// ── Lifecycle ─────────────────────────────────────────────────────────────────

let pollTimer: ReturnType<typeof setInterval> | null = null

onMounted(() => {
  store.fetch()
  fetchStats()
  fetchQueues()
  pollTimer = setInterval(() => { fetchStats(); fetchQueues() }, 3000)
})
onUnmounted(() => { if (pollTimer !== null) clearInterval(pollTimer) })
</script>

<template>
  <n-space vertical size="large">

    <!-- Header -->
    <n-space align="center" justify="space-between" style="flex-wrap: wrap; gap: 8px;">
      <n-h2 style="margin: 0;">Queue</n-h2>
      <n-space size="small">
        <n-tooltip trigger="hover">
          <template #trigger>
            <n-button @click="() => { store.fetch(); fetchStats(); fetchQueues() }" :loading="store.loading">
              <template #icon><n-icon><RefreshOutline /></n-icon></template>
            </n-button>
          </template>
          Refresh
        </n-tooltip>
        <n-popconfirm @positive-click="onFlush">
          <template #trigger>
            <n-tooltip trigger="hover">
              <template #trigger>
                <n-button type="warning" :loading="flushing">
                  <template #icon><n-icon><BanOutline /></n-icon></template>
                </n-button>
              </template>
              Flush Pending
            </n-tooltip>
          </template>
          Drop all pending tasks from all action queues?
        </n-popconfirm>
        <n-popconfirm @positive-click="onClear">
          <template #trigger>
            <n-tooltip trigger="hover">
              <template #trigger>
                <n-button type="error" :loading="clearing" :disabled="store.total === 0">
                  <template #icon><n-icon><TrashOutline /></n-icon></template>
                </n-button>
              </template>
              Clear Failures ({{ store.total }})
            </n-tooltip>
          </template>
          Remove all {{ store.total }} entries from the dead-letter queue?
        </n-popconfirm>
      </n-space>
    </n-space>

    <!-- Stats cards -->
    <n-grid :cols="3" :x-gap="12" :y-gap="12" responsive="screen" :item-responsive="true">
      <n-gi span="3 s:1">
        <n-card size="small" embedded>
          <n-statistic label="Redis Pending">
            <template #default>
              <span :style="{ color: (queueStats?.pending ?? 0) > 0 ? '#f9e2af' : '#cdd6f4' }">
                {{ queueStats?.pending ?? '—' }}
              </span>
            </template>
            <template #suffix>
              <span v-if="drainEta" style="color: #585b70; font-size: 12px; margin-left: 8px;">
                {{ drainEta }}
              </span>
            </template>
          </n-statistic>
        </n-card>
      </n-gi>
      <n-gi span="3 s:1">
        <n-card size="small" embedded>
          <n-statistic label="Dead-Letter">
            <template #default>
              <span :style="{ color: (queueStats?.failed ?? 0) > 0 ? '#f38ba8' : '#cdd6f4' }">
                {{ queueStats?.failed ?? '—' }}
              </span>
            </template>
          </n-statistic>
        </n-card>
      </n-gi>
      <n-gi span="3 s:1">
        <n-card size="small" embedded>
          <n-statistic label="Workers">
            <template #default>
              <span :style="{ color: (workerStats?.active ?? 0) > 0 ? '#a6e3a1' : '#cdd6f4' }">
                {{ workerStats?.active ?? '—' }}
              </span>
              <span style="color: #585b70; font-size: 14px;"> / {{ workerStats?.total ?? '—' }}</span>
            </template>
            <template #suffix>
              <span v-if="workerStats" style="color: #585b70; font-size: 12px; margin-left: 8px;">
                {{ workerStats.idle }} idle
              </span>
            </template>
          </n-statistic>
          <div v-if="workerStats" style="margin-top: 8px; height: 4px; border-radius: 2px; background: rgba(255,255,255,0.08); overflow: hidden;">
            <div :style="{
              height: '100%',
              width: workerStats.total > 0 ? (workerStats.active / workerStats.total * 100) + '%' : '0%',
              background: workerStats.active > 0 ? '#a6e3a1' : 'rgba(255,255,255,0.15)',
              transition: 'width 0.5s ease',
            }" />
          </div>
        </n-card>
      </n-gi>
    </n-grid>

    <!-- Per-action queues table -->
    <n-card size="small" title="Action Queues">
      <n-empty
        v-if="queues.length === 0"
        description="No active actions"
        style="padding: 16px 0;"
      />
      <n-data-table
        v-else
        :columns="queueColumns"
        :data="queues"
        :pagination="false"
        size="small"
      />
    </n-card>

    <!-- Dead-letter failures -->
    <n-card size="small" title="Dead-Letter Queue">
      <n-alert v-if="store.error" type="error" style="margin-bottom: 12px;">{{ store.error }}</n-alert>

      <n-empty
        v-if="!store.loading && store.failures.length === 0"
        description="No failures — dead-letter queue is empty"
        style="padding: 32px 0;"
      />

      <n-data-table
        v-else
        :columns="failureColumns"
        :data="store.failures"
        :pagination="pagination"
        :loading="store.loading"
        :scroll-x="640"
        :row-key="(row: FailedTweet) => row.tweet?.Tweet?.id ?? ''"
        :row-props="(row: FailedTweet) => ({ style: 'cursor: pointer;', onClick: () => openDetail(row) })"
        size="small"
      />
    </n-card>

    <!-- Detail drawer -->
    <n-drawer v-model:show="showDetail" :width="drawerWidth" placement="right">
      <n-drawer-content :native-scrollbar="false" closable>
        <template #header>Failure Details</template>

        <template v-if="selected">
          <n-space vertical size="medium">

            <n-card size="small" title="Tweet">
              <n-descriptions :column="1" size="small" label-placement="left">
                <n-descriptions-item label="ID">
                  <n-text code>{{ selected.tweet?.Tweet?.id }}</n-text>
                </n-descriptions-item>
                <n-descriptions-item label="Author">
                  @{{ selected.tweet?.Tweet?.author_handle || selected.tweet?.Tweet?.author_id }}
                </n-descriptions-item>
                <n-descriptions-item label="Created">
                  {{ selected.tweet?.Tweet?.created_at ? formatDateTime(selected.tweet.Tweet.created_at) : '—' }}
                </n-descriptions-item>
              </n-descriptions>
              <div style="margin-top: 12px; white-space: pre-wrap; color: #cdd6f4; font-size: 13px;">
                {{ selected.tweet?.Tweet?.text }}
              </div>
            </n-card>

            <n-card v-if="selected.tweet?.MatchingRules?.length" size="small" title="Matching Rules">
              <n-space size="small" wrap>
                <n-tag
                  v-for="rule in selected.tweet.MatchingRules"
                  :key="rule.id"
                  type="info" size="small" round
                >
                  {{ rule.tag || rule.id }}
                </n-tag>
              </n-space>
            </n-card>

            <n-card size="small" title="Failed Models">
              <n-space vertical size="small">
                <n-card v-for="(fm, i) in selected.failed_models" :key="i" size="small" embedded>
                  <n-space align="center" justify="space-between">
                    <n-tag type="error" size="small" round>{{ fm.name }}</n-tag>
                    <n-text depth="3" style="font-size: 11px;">
                      {{ fm.timestamp ? formatDateTime(fm.timestamp) : '' }}
                    </n-text>
                  </n-space>
                  <div style="
                    margin-top: 8px; padding: 8px;
                    background: rgba(243,139,168,0.08); border-radius: 4px;
                    font-family: monospace; font-size: 12px;
                    color: #f38ba8; white-space: pre-wrap; word-break: break-all;
                  ">{{ fm.error }}</div>
                </n-card>
              </n-space>
            </n-card>

          </n-space>
        </template>
      </n-drawer-content>
    </n-drawer>

  </n-space>
</template>
```

- [ ] **Step 2: Delete `web/src/views/FailuresView.vue`**

```bash
rm web/src/views/FailuresView.vue
```

- [ ] **Step 3: Commit**

```bash
git add web/src/views/QueueView.vue
git rm web/src/views/FailuresView.vue
git commit -m "feat: QueueView with per-action queues table, replace FailuresView"
```

---

## Task 8: Docker build & CLAUDE.md update

**Files:**
- Modify: `CLAUDE.md`

- [ ] **Step 1: Build the container**

```bash
docker compose build --build-arg BUILDKIT_INLINE_CACHE=1 iran-watcher
```
Expected: build succeeds with no errors. Fix any compilation errors before proceeding.

- [ ] **Step 2: Also build the web container**

```bash
docker compose build web
```
Expected: build succeeds.

- [ ] **Step 3: Update CLAUDE.md**

In the **Key Design Decisions** section, update the bullet for the task queue:

- Change "Dead-letter queue (`iran-watcher:failed`) — pipeline execution errors dead-lettered here; task queue is `iran-watcher:tasks` (replaces `iran-watcher:raw`)" to:

  > **Per-action task queues** (`iran-watcher:tasks:{action_id}`) — one Redis list per active action; producers route by `task.ActionID`; workers call `buildReadyKeys()` before each BRPOP and include an action's key only if its pipeline is immediately acquirable (`AvailabilityChecker.IsAvailable()`); ONNX/internal/notification pipelines are always included; Ollama pipelines are included only when a healthy idle backend is available; dead-letter queue remains `iran-watcher:failed`; `FlushQueue` uses `SCAN iran-watcher:tasks:*` to find and delete all per-action keys.

- Also update the `Consumer` interface description (in the **Simplified event-driven worker** bullet or inline):

  > `Consumer.PopTask(ctx, timeout, keys ...string)` — variadic keys passed straight to `BRPOP`; returns `(nil, nil)` immediately when keys is empty.

- [ ] **Step 4: Commit**

```bash
git add CLAUDE.md
git commit -m "docs: update CLAUDE.md for per-action queue architecture"
```

---

## Migration reminder

Before deploying to production, delete the old shared queue key from Redis (the new code never pushes to it, but old tasks sitting there won't be processed):

```bash
redis-cli DEL iran-watcher:tasks iran-watcher:raw
```
