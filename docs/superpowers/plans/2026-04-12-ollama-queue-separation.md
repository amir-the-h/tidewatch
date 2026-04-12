# Ollama Queue Separation Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Separate Ollama tasks into their own Redis sorted set so workers never pop Ollama tasks they cannot immediately run, and depth priority remains globally respected across both queues.

**Architecture:** Two Redis sorted sets — `iran-watcher:tasks` (ONNX/internal/notification, always immediately runnable) and `iran-watcher:tasks:ollama` (Ollama only, gated by backend capacity). Every `Task` carries `IsOllama bool` which `PushTask` reads to route to the correct key. Workers use a peek-then-pop loop: read the minimum depth from both queues, pop from whichever has the globally lowest depth (subject to Ollama capacity), block on the non-Ollama queue when both are empty or no Ollama capacity exists.

**Tech Stack:** Go 1.25.5, Redis sorted sets (`ZAdd`/`ZPopMin`/`ZRangeWithScores`/`BZPopMin`), Vue 3 + Pinia, Naive UI.

---

## File Map

| File | Change |
|------|--------|
| `internal/queue/queue.go` | Add `QueueOllamaTasksKey` const; add `IsOllama bool` to `Task`; add `TryPopTask`, `TryPopOllamaTask`, `PeekMinDepth` to `Consumer` interface |
| `internal/queue/redis.go` | Add `ZPopMin`, `ZRangeWithScores` to `redisClient`; implement new Consumer methods; route `PushTask` on `IsOllama`; update `TotalTaskLen`, `FlushQueue`; update broadcast shape to `{pending_tasks, pending_ollama}` |
| `internal/queue/queue_test.go` | Tests for Ollama routing, new Consumer methods, updated broadcast shape, multi-key `FlushQueue` and `TotalTaskLen` |
| `internal/worker/pipeline.go` | Add `isOllamaAction` helper; set `IsOllama` on every `queue.Task{}` literal; add `cache *ActionCache` param to `pushContinuationTask` |
| `internal/worker/manager.go` | Replace `runWorker` body with peek-then-pop loop; remove 100 ms sleep; add `IsOllama` safety-net re-queue |
| `internal/api/handlers_queue.go` | Return `pending_tasks` and `pending_ollama` instead of `pending` |
| `web/src/api/queue.ts` | Update `QueueResponse` type |
| `web/src/stores/queues.ts` | Replace `pending` with `pendingTasks` + `pendingOllama` |
| `web/src/composables/useWebSocket.ts` | Update `queue_update` handler |
| `web/src/views/QueueView.vue` | Replace single Pending card with two cards |
| `CLAUDE.md` | Update queue key descriptions and event shapes |

---

## Task 1: Queue layer — two keys, `IsOllama` routing, Consumer extensions

**Files:**
- Modify: `internal/queue/queue.go`
- Modify: `internal/queue/redis.go`
- Modify: `internal/queue/queue_test.go`

### Context

`internal/queue/queue.go` currently defines `QueueTasksKey = "iran-watcher:tasks"` and the `Consumer` interface with only `PopTask` (blocking) and `PopFailed`. `internal/queue/redis.go` implements `PushTask`, `PopTask`, `TotalTaskLen`, `FlushQueue`, and a `redisClient` interface. Tests live in `internal/queue/queue_test.go`.

---

- [ ] **Step 1: Write failing tests**

Add to `internal/queue/queue_test.go`:

```go
// ---------------------------------------------------------------------------
// Routing tests
// ---------------------------------------------------------------------------

func TestPushTask_OllamaRoutesToOllamaKey(t *testing.T) {
	fake := &pushCaptureFakeClient{}
	q := &RedisQueue{client: fake}
	task := &Task{TriggerType: "new_tweet", ActionID: 7, Depth: 1, IsOllama: true, Context: map[string]any{}}
	if err := q.PushTask(context.Background(), task); err != nil {
		t.Fatalf("PushTask: %v", err)
	}
	if fake.pushedKey != QueueOllamaTasksKey {
		t.Errorf("pushed to key %q, want %q", fake.pushedKey, QueueOllamaTasksKey)
	}
}

func TestPushTask_NonOllamaRoutesToTasksKey(t *testing.T) {
	fake := &pushCaptureFakeClient{}
	q := &RedisQueue{client: fake}
	task := &Task{TriggerType: "new_tweet", ActionID: 7, Depth: 1, IsOllama: false, Context: map[string]any{}}
	if err := q.PushTask(context.Background(), task); err != nil {
		t.Fatalf("PushTask: %v", err)
	}
	if fake.pushedKey != QueueTasksKey {
		t.Errorf("pushed to key %q, want %q", fake.pushedKey, QueueTasksKey)
	}
}

// ---------------------------------------------------------------------------
// Broadcast shape test
// ---------------------------------------------------------------------------

func TestPushTask_BroadcastsQueueUpdateShape(t *testing.T) {
	fake := &pushCaptureFakeClient{}
	bc := &captureBroadcaster{}
	q := &RedisQueue{client: fake, broadcaster: bc}

	task := &Task{TriggerType: "new_tweet", ActionID: 7, Depth: 0, IsOllama: false, Context: map[string]any{}}
	if err := q.PushTask(context.Background(), task); err != nil {
		t.Fatalf("PushTask: %v", err)
	}
	if len(bc.events) != 1 {
		t.Fatalf("expected 1 broadcast, got %d", len(bc.events))
	}
	payload, ok := bc.events[0].Payload.(map[string]int64)
	if !ok {
		t.Fatalf("unexpected payload type %T", bc.events[0].Payload)
	}
	if _, hasPendingTasks := payload["pending_tasks"]; !hasPendingTasks {
		t.Error("broadcast payload missing 'pending_tasks'")
	}
	if _, hasPendingOllama := payload["pending_ollama"]; !hasPendingOllama {
		t.Error("broadcast payload missing 'pending_ollama'")
	}
	if _, hasOld := payload["pending"]; hasOld {
		t.Error("broadcast payload should not contain legacy 'pending' key")
	}
}

// ---------------------------------------------------------------------------
// FlushQueue deletes both keys
// ---------------------------------------------------------------------------

func TestFlushQueue_DeletesBothKeys(t *testing.T) {
	fake := &fakeRedisClient{}
	q := &RedisQueue{client: fake}
	if err := q.FlushQueue(context.Background()); err != nil {
		t.Fatalf("FlushQueue: %v", err)
	}
	wantKeys := map[string]bool{QueueTasksKey: true, QueueOllamaTasksKey: true}
	for _, k := range fake.deleted {
		delete(wantKeys, k)
	}
	if len(wantKeys) != 0 {
		t.Errorf("missing deleted keys: %v", wantKeys)
	}
}

// ---------------------------------------------------------------------------
// TotalTaskLen sums both queues
// ---------------------------------------------------------------------------

func TestTotalTaskLen_SumsBothQueues(t *testing.T) {
	fake := &fakeRedisClient{
		zcard: map[string]int64{
			QueueTasksKey:       10,
			QueueOllamaTasksKey: 7,
		},
	}
	q := &RedisQueue{client: fake}
	total, err := q.TotalTaskLen(context.Background())
	if err != nil {
		t.Fatalf("TotalTaskLen: %v", err)
	}
	if total != 17 {
		t.Errorf("TotalTaskLen = %d, want 17", total)
	}
}

// ---------------------------------------------------------------------------
// TryPopTask / TryPopOllamaTask return nil when queue is empty
// ---------------------------------------------------------------------------

func TestTryPopTask_ReturnsNilWhenEmpty(t *testing.T) {
	q := &RedisQueue{client: &fakeRedisClient{}}
	task, err := q.TryPopTask(context.Background())
	if err != nil {
		t.Fatalf("expected nil error, got: %v", err)
	}
	if task != nil {
		t.Fatalf("expected nil task, got: %+v", task)
	}
}

func TestTryPopOllamaTask_ReturnsNilWhenEmpty(t *testing.T) {
	q := &RedisQueue{client: &fakeRedisClient{}}
	task, err := q.TryPopOllamaTask(context.Background())
	if err != nil {
		t.Fatalf("expected nil error, got: %v", err)
	}
	if task != nil {
		t.Fatalf("expected nil task, got: %+v", task)
	}
}

// ---------------------------------------------------------------------------
// PeekMinDepth returns false when queue is empty, score when present
// ---------------------------------------------------------------------------

func TestPeekMinDepth_ReturnsFalseWhenEmpty(t *testing.T) {
	q := &RedisQueue{client: &fakeRedisClient{}}
	_, ok := q.PeekMinDepth(context.Background(), QueueTasksKey)
	if ok {
		t.Error("expected ok=false for empty queue")
	}
}

func TestPeekMinDepth_ReturnsScoreWhenPresent(t *testing.T) {
	fake := &peekFakeClient{score: 3.0}
	q := &RedisQueue{client: fake}
	depth, ok := q.PeekMinDepth(context.Background(), QueueTasksKey)
	if !ok {
		t.Fatal("expected ok=true")
	}
	if depth != 3.0 {
		t.Errorf("depth = %v, want 3.0", depth)
	}
}

// peekFakeClient returns a fixed score for ZRangeWithScores.
type peekFakeClient struct {
	fakeRedisClient
	score float64
}

func (f *peekFakeClient) ZRangeWithScores(_ context.Context, _ string, _, _ int64) *redis.ZSliceCmd {
	cmd := redis.NewZSliceCmd(context.Background())
	cmd.SetVal([]redis.Z{{Score: f.score, Member: "x"}})
	return cmd
}
```

- [ ] **Step 2: Run tests to verify they fail**

```bash
go test ./internal/queue/... -run "TestPushTask_OllamaRoutesToOllamaKey|TestPushTask_NonOllamaRoutesToTasksKey|TestPushTask_BroadcastsQueueUpdateShape|TestFlushQueue_DeletesBothKeys|TestTotalTaskLen_SumsBothQueues|TestTryPopTask|TestTryPopOllamaTask|TestPeekMinDepth" -v 2>&1 | tail -30
```

Expected: compile errors or FAIL (symbols not yet defined).

- [ ] **Step 3: Update `queue.go`**

Replace the entire file:

```go
package queue

import (
	"context"
	"encoding/json"
	"time"
)

const (
	// QueueTasksKey is the global Redis sorted-set key for non-Ollama pending tasks.
	// Tasks are scored by Depth (lower = higher priority).
	QueueTasksKey = "iran-watcher:tasks"

	// QueueOllamaTasksKey is the global Redis sorted-set key for Ollama pending tasks.
	// Workers only pop from this key when backend capacity is available.
	QueueOllamaTasksKey = "iran-watcher:tasks:ollama"

	// QueueFailed is the dead-letter queue key.
	QueueFailed = "iran-watcher:failed"
)

// Task is a single unit of work pushed to a task queue.
type Task struct {
	TriggerType    string          `json:"trigger_type"`
	Event          json.RawMessage `json:"event"`
	ActionID       int64           `json:"action_id"`
	PipelineNodeID int64           `json:"pipeline_node_id,omitempty"`
	PipelineID     int64           `json:"pipeline_id,omitempty"`
	// RootPipelineID is the top-level pipeline that originally dispatched this
	// task (set at PushInitialTasksForGraph time, propagated through all sub-tasks).
	// Non-zero tasks contribute to pipeline_run_tracking for pipeline_finished detection.
	RootPipelineID       int64  `json:"root_pipeline_id,omitempty"`
	ParentPipelineNodeID *int64 `json:"parent_pipeline_node_id,omitempty"`
	// SubPipelineEntryNodeID is the node ID of the INITIAL action dispatched
	// into a sub-pipeline. It is used as the leaf-tracking key in
	// pipeline_node_executions so that any termination of that branch
	// (structural or via condition-fail) can signal the parent pipeline.
	SubPipelineEntryNodeID *int64 `json:"sub_pipeline_entry_node_id,omitempty"`
	// Depth is the number of action-hops from the pipeline's Start node.
	// Lower depth = higher priority. Zero value = highest priority (backwards-compatible).
	Depth int `json:"depth,omitempty"`
	// IsOllama marks tasks whose action uses an Ollama pipeline.
	// PushTask routes IsOllama=true tasks to QueueOllamaTasksKey.
	// The field is preserved across re-queue round-trips.
	IsOllama bool           `json:"is_ollama,omitempty"`
	Context  map[string]any `json:"context"`
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

// FailedModel records which action failed and why.
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
type Consumer interface {
	// PopTask blocks until a task is available on iran-watcher:tasks or the
	// timeout elapses. Returns (nil, nil) on timeout.
	PopTask(ctx context.Context, timeout time.Duration) (*Task, error)
	// TryPopTask is a non-blocking pop from iran-watcher:tasks.
	// Returns (nil, nil) if the queue is empty.
	TryPopTask(ctx context.Context) (*Task, error)
	// TryPopOllamaTask is a non-blocking pop from iran-watcher:tasks:ollama.
	// Returns (nil, nil) if the queue is empty.
	TryPopOllamaTask(ctx context.Context) (*Task, error)
	// PeekMinDepth returns the score (Depth) of the lowest-scored member in
	// the given key without consuming it. Returns (0, false) if the key is empty.
	PeekMinDepth(ctx context.Context, key string) (depth float64, ok bool)
	// PopFailed pops a FailedTweet from the dead-letter queue.
	// Returns (nil, nil) on timeout.
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

- [ ] **Step 4: Update `redis.go`**

Replace the entire file:

```go
package queue

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/amir-the-h/iran-watcher/internal/hub"
	"github.com/redis/go-redis/v9"
)

// redisClient is the narrow subset of redis.Client methods used by RedisQueue.
type redisClient interface {
	LPush(ctx context.Context, key string, values ...interface{}) *redis.IntCmd
	BRPop(ctx context.Context, timeout time.Duration, keys ...string) *redis.StringSliceCmd
	LRange(ctx context.Context, key string, start, stop int64) *redis.StringSliceCmd
	Del(ctx context.Context, keys ...string) *redis.IntCmd
	ZAdd(ctx context.Context, key string, members ...redis.Z) *redis.IntCmd
	BZPopMin(ctx context.Context, timeout time.Duration, keys ...string) *redis.ZWithKeyCmd
	ZPopMin(ctx context.Context, key string, count ...int64) *redis.ZSliceCmd
	ZRangeWithScores(ctx context.Context, key string, start, stop int64) *redis.ZSliceCmd
	ZCard(ctx context.Context, key string) *redis.IntCmd
}

// RedisQueue implements both Producer and Consumer.
// Non-Ollama tasks go into iran-watcher:tasks; Ollama tasks into iran-watcher:tasks:ollama.
// The dead-letter queue uses a plain list (LPush/BRPop).
type RedisQueue struct {
	client      redisClient
	broadcaster hub.Broadcaster
}

// WithBroadcaster wires a broadcaster for live queue-depth updates.
func (q *RedisQueue) WithBroadcaster(b hub.Broadcaster) {
	q.broadcaster = b
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

// broadcastQueueUpdate emits current pending counts for both queues.
func (q *RedisQueue) broadcastQueueUpdate(ctx context.Context) {
	if q.broadcaster == nil {
		return
	}
	pendingTasks, _ := q.client.ZCard(ctx, QueueTasksKey).Result()
	pendingOllama, _ := q.client.ZCard(ctx, QueueOllamaTasksKey).Result()
	q.broadcaster.Broadcast(hub.Event{
		Type: "queue_update",
		Payload: map[string]int64{
			"pending_tasks":  pendingTasks,
			"pending_ollama": pendingOllama,
		},
	})
}

// PushTask serializes a Task to JSON and ZAdds it to the appropriate sorted set.
// IsOllama=true tasks go to QueueOllamaTasksKey; others go to QueueTasksKey.
// Score = task.Depth so lower-depth (higher-priority) tasks are popped first.
// Sorted sets deduplicate by member string, so re-queuing the same task is idempotent.
func (q *RedisQueue) PushTask(ctx context.Context, task *Task) error {
	b, err := json.Marshal(task)
	if err != nil {
		return fmt.Errorf("queue: marshal task: %w", err)
	}
	key := QueueTasksKey
	if task.IsOllama {
		key = QueueOllamaTasksKey
	}
	if err := q.client.ZAdd(ctx, key, redis.Z{
		Score:  float64(task.Depth),
		Member: string(b),
	}).Err(); err != nil {
		return fmt.Errorf("queue: push task: %w", err)
	}
	q.broadcastQueueUpdate(ctx)
	return nil
}

// PopTask blocks until a Task is available on iran-watcher:tasks or the timeout elapses.
// Returns the task with the lowest Depth (score) first. Returns (nil, nil) on timeout.
func (q *RedisQueue) PopTask(ctx context.Context, timeout time.Duration) (*Task, error) {
	zwk, err := q.client.BZPopMin(ctx, timeout, QueueTasksKey).Result()
	if err != nil {
		if errors.Is(err, redis.Nil) {
			return nil, nil
		}
		return nil, fmt.Errorf("queue: pop task: %w", err)
	}
	if zwk == nil || zwk.Key == "" {
		return nil, nil
	}
	member, ok := zwk.Z.Member.(string)
	if !ok {
		return nil, fmt.Errorf("queue: pop task: member is not a string")
	}
	var t Task
	if err := json.Unmarshal([]byte(member), &t); err != nil {
		return nil, fmt.Errorf("queue: pop task: unmarshal: %w", err)
	}
	q.broadcastQueueUpdate(ctx)
	return &t, nil
}

// TryPopTask is a non-blocking pop from iran-watcher:tasks.
// Returns (nil, nil) if the queue is empty.
func (q *RedisQueue) TryPopTask(ctx context.Context) (*Task, error) {
	return q.zpopmin(ctx, QueueTasksKey)
}

// TryPopOllamaTask is a non-blocking pop from iran-watcher:tasks:ollama.
// Returns (nil, nil) if the queue is empty.
func (q *RedisQueue) TryPopOllamaTask(ctx context.Context) (*Task, error) {
	return q.zpopmin(ctx, QueueOllamaTasksKey)
}

// zpopmin performs a non-blocking ZPopMin on the given key.
func (q *RedisQueue) zpopmin(ctx context.Context, key string) (*Task, error) {
	results, err := q.client.ZPopMin(ctx, key, 1).Result()
	if err != nil {
		if errors.Is(err, redis.Nil) {
			return nil, nil
		}
		return nil, fmt.Errorf("queue: zpopmin %s: %w", key, err)
	}
	if len(results) == 0 {
		return nil, nil
	}
	member, ok := results[0].Member.(string)
	if !ok {
		return nil, fmt.Errorf("queue: zpopmin: member is not a string")
	}
	var t Task
	if err := json.Unmarshal([]byte(member), &t); err != nil {
		return nil, fmt.Errorf("queue: zpopmin: unmarshal: %w", err)
	}
	q.broadcastQueueUpdate(ctx)
	return &t, nil
}

// PeekMinDepth returns the score (Depth) of the lowest-scored member in the
// given key without consuming it. Returns (0, false) if the key is empty.
func (q *RedisQueue) PeekMinDepth(ctx context.Context, key string) (float64, bool) {
	results, err := q.client.ZRangeWithScores(ctx, key, 0, 0).Result()
	if err != nil || len(results) == 0 {
		return 0, false
	}
	return results[0].Score, true
}

// PushFailed pushes a FailedTweet to the dead-letter queue.
func (q *RedisQueue) PushFailed(ctx context.Context, ft *FailedTweet) error {
	data, err := json.Marshal(ft)
	if err != nil {
		return fmt.Errorf("queue: push failed: marshal: %w", err)
	}
	cmd := q.client.LPush(ctx, QueueFailed, data)
	if err := cmd.Err(); err != nil {
		return fmt.Errorf("queue: push failed: %w", err)
	}
	if q.broadcaster != nil {
		q.broadcaster.Broadcast(hub.Event{
			Type:    "failed_count",
			Payload: map[string]int64{"count": cmd.Val()},
		})
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
	n, err := q.client.ZCard(ctx, queue).Result()
	if err != nil {
		return 0, fmt.Errorf("queue: len: %w", err)
	}
	return n, nil
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
	if q.broadcaster != nil {
		q.broadcaster.Broadcast(hub.Event{
			Type:    "failed_count",
			Payload: map[string]int64{"count": 0},
		})
	}
	return nil
}

// TotalTaskLen returns the total number of pending tasks across both task queues.
func (q *RedisQueue) TotalTaskLen(ctx context.Context) (int64, error) {
	tasks, err := q.Len(ctx, QueueTasksKey)
	if err != nil {
		return 0, err
	}
	ollama, err := q.Len(ctx, QueueOllamaTasksKey)
	if err != nil {
		return 0, err
	}
	return tasks + ollama, nil
}

// FlushQueue deletes both task queues.
func (q *RedisQueue) FlushQueue(ctx context.Context) error {
	if err := q.client.Del(ctx, QueueTasksKey, QueueOllamaTasksKey).Err(); err != nil {
		return fmt.Errorf("queue: flush: %w", err)
	}
	return nil
}
```

- [ ] **Step 5: Update fake client in `queue_test.go`**

Add the two new methods to `fakeRedisClient` (after the existing `ZCard` method):

```go
func (f *fakeRedisClient) ZPopMin(_ context.Context, _ string, _ ...int64) *redis.ZSliceCmd {
	cmd := redis.NewZSliceCmd(context.Background())
	cmd.SetVal(nil)
	return cmd
}

func (f *fakeRedisClient) ZRangeWithScores(_ context.Context, _ string, _, _ int64) *redis.ZSliceCmd {
	cmd := redis.NewZSliceCmd(context.Background())
	cmd.SetVal(nil)
	return cmd
}
```

Also add `ZPopMin` and `ZRangeWithScores` to `pushCaptureFakeClient` (delegates to `fakeRedisClient`):

```go
func (f *pushCaptureFakeClient) ZPopMin(_ context.Context, _ string, _ ...int64) *redis.ZSliceCmd {
	cmd := redis.NewZSliceCmd(context.Background())
	cmd.SetVal(nil)
	return cmd
}

func (f *pushCaptureFakeClient) ZRangeWithScores(_ context.Context, _ string, _, _ int64) *redis.ZSliceCmd {
	cmd := redis.NewZSliceCmd(context.Background())
	cmd.SetVal(nil)
	return cmd
}
```

Also add to `bzpopSuccessFakeClient`:

```go
func (f *bzpopSuccessFakeClient) ZPopMin(_ context.Context, _ string, _ ...int64) *redis.ZSliceCmd {
	cmd := redis.NewZSliceCmd(context.Background())
	cmd.SetVal(nil)
	return cmd
}

func (f *bzpopSuccessFakeClient) ZRangeWithScores(_ context.Context, _ string, _, _ int64) *redis.ZSliceCmd {
	cmd := redis.NewZSliceCmd(context.Background())
	cmd.SetVal(nil)
	return cmd
}
```

Also update the existing `TestPushTask_BroadcastsQueueUpdate` test — the payload now has `pending_tasks` and `pending_ollama` instead of `pending`. Replace:

```go
// OLD — remove this:
payload, ok := bc.events[0].Payload.(map[string]int64)
if !ok {
	t.Fatalf("unexpected payload type %T", bc.events[0].Payload)
}
if payload["pending"] != 1 { // pushCaptureFakeClient.ZCard returns 1
	t.Errorf("pending: got %d, want 1", payload["pending"])
}
```

With:

```go
// NEW:
payload, ok := bc.events[0].Payload.(map[string]int64)
if !ok {
	t.Fatalf("unexpected payload type %T", bc.events[0].Payload)
}
if _, ok := payload["pending_tasks"]; !ok {
	t.Error("broadcast payload missing 'pending_tasks'")
}
if _, ok := payload["pending_ollama"]; !ok {
	t.Error("broadcast payload missing 'pending_ollama'")
}
```

Do the same fix to `TestPopTask_BroadcastsQueueUpdateOnSuccess` — replace the `payload["pending"] != 5` assertion with:

```go
if _, ok := payload["pending_tasks"]; !ok {
	t.Error("broadcast payload missing 'pending_tasks'")
}
```

Also replace `TestQueueTasksKey_IsConstant` with a test that checks both constants:

```go
func TestQueueKeys_AreConstants(t *testing.T) {
	if QueueTasksKey != "iran-watcher:tasks" {
		t.Errorf("QueueTasksKey = %q, want %q", QueueTasksKey, "iran-watcher:tasks")
	}
	if QueueOllamaTasksKey != "iran-watcher:tasks:ollama" {
		t.Errorf("QueueOllamaTasksKey = %q, want %q", QueueOllamaTasksKey, "iran-watcher:tasks:ollama")
	}
}
```

Replace `TestTotalTaskLen_SingleKey` with `TestTotalTaskLen_SumsBothQueues` (already written in Step 1).

Replace `TestFlushQueue_SingleDel` with `TestFlushQueue_DeletesBothKeys` (already written in Step 1).

- [ ] **Step 6: Run tests to verify they pass**

```bash
go test ./internal/queue/... -v 2>&1 | tail -40
```

Expected: all tests PASS.

- [ ] **Step 7: Commit**

```bash
git add internal/queue/queue.go internal/queue/redis.go internal/queue/queue_test.go
git commit -m "feat(queue): two-queue routing — Ollama tasks to separate sorted set"
```

---

## Task 2: Worker push layer — set `IsOllama` on tasks in `pipeline.go`

**Files:**
- Modify: `internal/worker/pipeline.go`

### Context

`internal/worker/pipeline.go` constructs `queue.Task{}` literals in these functions:
- `dispatchInitialNode` — for action nodes (line ~246) and sub-pipeline entry nodes (lines ~301, ~607, ~668 in the file as of last read)
- `pushContinuationTask` — for downstream action nodes (line ~736)
- `PushContinuationTasks` — creates a synthetic `parentTask` used only as a parameter to recurse into `PushContinuationTasks`, NOT pushed to Redis (no `IsOllama` needed there)

The `isOllamaAction` helper takes `*ActionCache` and `actionID int64`, returns `bool`. `ActionCache.Get(id)` returns `*CacheEntry` with a `Pipeline inference.Pipeline`; `Pipeline.Type()` returns `inference.PipelineType`; `inference.PipelineTypeOllama` is the constant to compare against.

---

- [ ] **Step 1: Write failing test**

Add a new test file `internal/worker/pipeline_ollama_test.go`:

```go
package worker

import (
	"testing"

	"github.com/amir-the-h/iran-watcher/internal/inference"
	"github.com/amir-the-h/iran-watcher/internal/model"
)

// mockOllamaPipeline satisfies inference.Pipeline with Type()=PipelineTypeOllama.
type mockOllamaPipeline struct{}

func (m *mockOllamaPipeline) Type() inference.PipelineType { return inference.PipelineTypeOllama }
func (m *mockOllamaPipeline) Name() string                 { return "ollama" }
func (m *mockOllamaPipeline) Run(_ interface{}, _ inference.PipelineInput) (map[string]any, error) {
	return nil, nil
}

// mockOnnxPipeline satisfies inference.Pipeline with Type()=PipelineTypeONNX.
type mockOnnxPipeline struct{}

func (m *mockOnnxPipeline) Type() inference.PipelineType { return inference.PipelineTypeONNX }
func (m *mockOnnxPipeline) Name() string                 { return "onnx" }
func (m *mockOnnxPipeline) Run(_ interface{}, _ inference.PipelineInput) (map[string]any, error) {
	return nil, nil
}

func buildCacheWithPipeline(actionID int64, p inference.Pipeline) *ActionCache {
	c := &ActionCache{entries: make(map[int64]*CacheEntry)}
	c.entries[actionID] = &CacheEntry{
		Action:   &model.Action{ID: actionID},
		Pipeline: p,
	}
	return c
}

func TestIsOllamaAction_ReturnsTrueForOllama(t *testing.T) {
	cache := buildCacheWithPipeline(1, &mockOllamaPipeline{})
	if !isOllamaAction(cache, 1) {
		t.Error("expected isOllamaAction=true for Ollama pipeline")
	}
}

func TestIsOllamaAction_ReturnsFalseForONNX(t *testing.T) {
	cache := buildCacheWithPipeline(2, &mockOnnxPipeline{})
	if isOllamaAction(cache, 2) {
		t.Error("expected isOllamaAction=false for ONNX pipeline")
	}
}

func TestIsOllamaAction_ReturnsFalseForNilCache(t *testing.T) {
	if isOllamaAction(nil, 1) {
		t.Error("expected isOllamaAction=false for nil cache")
	}
}

func TestIsOllamaAction_ReturnsFalseForUnknownAction(t *testing.T) {
	cache := buildCacheWithPipeline(1, &mockOllamaPipeline{})
	if isOllamaAction(cache, 999) {
		t.Error("expected isOllamaAction=false for unknown action_id")
	}
}
```

Note: `Run` signature must match `inference.Pipeline`. Read `internal/inference/pipeline.go` to verify the exact `Run` signature and adjust the mock if needed.

- [ ] **Step 2: Run test to verify it fails**

```bash
go test ./internal/worker/... -run "TestIsOllamaAction" -v 2>&1 | tail -20
```

Expected: compile error — `isOllamaAction` undefined.

- [ ] **Step 3: Add `isOllamaAction` helper and update `pipeline.go`**

At the top of `internal/worker/pipeline.go`, after the import block, add:

```go
// isOllamaAction reports whether the action identified by actionID uses an
// Ollama pipeline. Returns false when cache is nil or the action is not found.
func isOllamaAction(cache *ActionCache, actionID int64) bool {
	if cache == nil {
		return false
	}
	entry := cache.Get(actionID)
	return entry != nil && entry.Pipeline.Type() == inference.PipelineTypeOllama
}
```

This requires adding `"github.com/amir-the-h/iran-watcher/internal/inference"` to the import block if not already present.

Then update every `queue.Task{}` literal that will be pushed to Redis to include `IsOllama`:

**In `dispatchInitialNode`, action-node case (~line 246):**
```go
task := &queue.Task{
    TriggerType:    triggerType,
    Event:          eventPayload,
    ActionID:       *node.ActionID,
    PipelineNodeID: node.ID,
    PipelineID:     pipelineID,
    RootPipelineID: rootPipelineID,
    Depth:          depth,
    IsOllama:       isOllamaAction(cache, *node.ActionID),
    Context:        map[string]any{},
}
```

**In `dispatchInitialNode`, sub-pipeline action-node case (~line 301):**
```go
subTask := &queue.Task{
    TriggerType:            triggerType,
    Event:                  eventPayload,
    ActionID:               *subActionNode.ActionID,
    PipelineNodeID:         subActionNode.ID,
    PipelineID:             *node.SubPipelineID,
    RootPipelineID:         rootPipelineID,
    ParentPipelineNodeID:   &parentNodeID,
    SubPipelineEntryNodeID: &entryNodeID,
    Depth:                  depth + 1,
    IsOllama:               isOllamaAction(cache, *subActionNode.ActionID),
    Context:                map[string]any{},
}
```

**In `PushContinuationTasks`, post-condition sub-pipeline action-node case (~line 607):**
```go
subTask := &queue.Task{
    TriggerType:            task.TriggerType,
    Event:                  task.Event,
    ActionID:               *subActionNode.ActionID,
    PipelineNodeID:         subActionNode.ID,
    PipelineID:             *nextNode.SubPipelineID,
    RootPipelineID:         task.RootPipelineID,
    ParentPipelineNodeID:   &parentNodeID,
    SubPipelineEntryNodeID: &entryNodeID,
    Depth:                  task.Depth + 1,
    IsOllama:               isOllamaAction(cache, *subActionNode.ActionID),
    Context:                copyContext(mergedCtx),
}
```

**In `PushContinuationTasks`, inline sub-pipeline action-node case (~line 668):**
```go
subTask := &queue.Task{
    TriggerType:            task.TriggerType,
    Event:                  task.Event,
    ActionID:               *subActionNode.ActionID,
    PipelineNodeID:         subActionNode.ID,
    PipelineID:             *targetNode.SubPipelineID,
    RootPipelineID:         task.RootPipelineID,
    ParentPipelineNodeID:   &parentNodeID,
    SubPipelineEntryNodeID: &entryNodeID,
    Depth:                  task.Depth + 1,
    IsOllama:               isOllamaAction(cache, *subActionNode.ActionID),
    Context:                copyContext(mergedCtx),
}
```

**Update `pushContinuationTask` signature and body (~line 727):**

Add `cache *ActionCache` parameter and set `IsOllama`:

```go
func pushContinuationTask(
    ctx context.Context,
    st store.Store,
    producer queue.Producer,
    cache *ActionCache,
    parent *queue.Task,
    node *model.PipelineNode,
    mergedCtx map[string]any,
    logger *slog.Logger,
) bool {
    task := &queue.Task{
        TriggerType:            parent.TriggerType,
        Event:                  parent.Event,
        ActionID:               *node.ActionID,
        PipelineNodeID:         node.ID,
        PipelineID:             parent.PipelineID,
        RootPipelineID:         parent.RootPipelineID,
        ParentPipelineNodeID:   parent.ParentPipelineNodeID,
        SubPipelineEntryNodeID: parent.SubPipelineEntryNodeID,
        Depth:                  parent.Depth + 1,
        IsOllama:               isOllamaAction(cache, *node.ActionID),
        Context:                copyContext(mergedCtx),
    }
    return pushTrackedTask(ctx, st, producer, task, logger)
}
```

Update the two call sites of `pushContinuationTask` in `PushContinuationTasks` to pass `cache`:

```go
// Both calls change from:
if pushContinuationTask(ctx, st, producer, task, targetNode, mergedCtx, logger) {
// To:
if pushContinuationTask(ctx, st, producer, cache, task, targetNode, mergedCtx, logger) {
```

- [ ] **Step 4: Run tests to verify they pass**

```bash
go test ./internal/worker/... -run "TestIsOllamaAction" -v 2>&1 | tail -20
```

Expected: all 4 `TestIsOllamaAction_*` tests PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/worker/pipeline.go internal/worker/pipeline_ollama_test.go
git commit -m "feat(worker): set IsOllama on tasks at push time"
```

---

## Task 3: Worker run layer — peek-then-pop loop in `manager.go`

**Files:**
- Modify: `internal/worker/manager.go`

### Context

`internal/worker/manager.go`'s `runWorker` currently: pops with 1s timeout, checks `AvailabilityChecker.IsAvailable()`, re-queues if unavailable + sleeps 100ms. Replace the entire `runWorker` body with the peek-then-pop loop. The `m.cache.Pool()` call returns `*inference.BackendPool`; `BackendPool.IsAvailable()` is already used in the current code. Add `"math"` to the import block.

---

- [ ] **Step 1: Write failing test**

Add to `internal/worker/worker_test.go` (find the existing test file):

```go
func TestRunWorker_UsesIsOllamaForRequeue(t *testing.T) {
	// This test verifies that when a task has IsOllama=true and the re-queue
	// safety net fires, PushTask is called with a task that still has IsOllama=true.
	pushedTasks := make([]*queue.Task, 0)
	prod := &mockProducer{
		pushFn: func(ctx context.Context, task *queue.Task) error {
			pushedTasks = append(pushedTasks, task)
			return nil
		},
	}
	// Build a task with IsOllama=true.
	task := &queue.Task{ActionID: 1, IsOllama: true}
	// Verify IsOllama is preserved across a PushTask call (the router reads it).
	if err := prod.PushTask(context.Background(), task); err != nil {
		t.Fatalf("PushTask: %v", err)
	}
	if len(pushedTasks) != 1 || !pushedTasks[0].IsOllama {
		t.Error("expected re-queued task to have IsOllama=true")
	}
}
```

Note: `mockProducer` likely already exists in `worker_test.go`. If it doesn't have a `pushFn` field, read the file and adapt the test to use whatever mock is available.

- [ ] **Step 2: Run test to verify it passes (it should — just validating the mock setup)**

```bash
go test ./internal/worker/... -run "TestRunWorker_UsesIsOllamaForRequeue" -v 2>&1 | tail -10
```

- [ ] **Step 3: Replace `runWorker` in `manager.go`**

Add `"math"` to the import block. Then replace the `runWorker` method:

```go
func (m *Manager) runWorker(ctx context.Context, id int) {
	for {
		select {
		case <-ctx.Done():
			return
		default:
		}

		// Peek minimum depths across both queues to find the globally lowest-depth
		// runnable task. Non-Ollama tasks are always immediately runnable;
		// Ollama tasks are only considered when backend capacity exists.
		d1, hasD1 := m.consumer.PeekMinDepth(ctx, queue.QueueTasksKey)
		if !hasD1 {
			d1 = math.MaxFloat64
		}

		d2 := math.MaxFloat64
		ollamaPool := m.cache.Pool()
		ollamaAvailable := ollamaPool != nil && ollamaPool.IsAvailable()
		if ollamaAvailable {
			if depth, ok := m.consumer.PeekMinDepth(ctx, queue.QueueOllamaTasksKey); ok {
				d2 = depth
			}
		}

		var task *queue.Task
		var popErr error

		switch {
		case d1 == math.MaxFloat64 && d2 == math.MaxFloat64:
			// Both queues empty (or no Ollama capacity): block on non-Ollama queue
			// with a short timeout so we periodically re-check Ollama capacity.
			task, popErr = m.consumer.PopTask(ctx, 250*time.Millisecond)
			if popErr != nil && m.logger != nil {
				m.logger.Error("worker: pop task", "worker_id", id, "error", popErr)
			}
		case d1 <= d2:
			// Non-Ollama queue has equal or lower depth — pop from it.
			task, popErr = m.consumer.TryPopTask(ctx)
			if popErr != nil && m.logger != nil {
				m.logger.Error("worker: try pop task", "worker_id", id, "error", popErr)
			}
		default:
			// Ollama queue has strictly lower depth and capacity is available — pop from it.
			task, popErr = m.consumer.TryPopOllamaTask(ctx)
			if popErr != nil && m.logger != nil {
				m.logger.Error("worker: try pop ollama task", "worker_id", id, "error", popErr)
			}
		}

		if task == nil {
			continue
		}

		// Post-pop: discard if action no longer exists in cache.
		entry := m.cache.Get(task.ActionID)
		if entry == nil {
			if m.logger != nil {
				m.logger.Warn("worker: discarding task for unknown action_id — action inactive or deleted",
					"worker_id", id, "action_id", task.ActionID)
			}
			continue
		}

		// Safety net for Ollama tasks: re-check availability after pop to close the
		// race between PeekMinDepth and AcquireForPipeline inside the pipeline.
		// If the race occurred, re-queue the task (IsOllama is preserved so it routes
		// back to iran-watcher:tasks:ollama) and continue without sleeping —
		// the loop will block naturally in the empty-queues branch above.
		if task.IsOllama {
			if av, ok := entry.Pipeline.(inference.AvailabilityChecker); ok && !av.IsAvailable() {
				if pushErr := m.producer.PushTask(ctx, task); pushErr != nil && m.logger != nil {
					m.logger.Warn("worker: re-queue Ollama task — no backend after pop (race)",
						"worker_id", id, "action_id", task.ActionID, "error", pushErr)
				}
				continue
			}
		}

		func() {
			taskCtx, taskCancel := context.WithCancel(ctx)
			m.workerCancels.Store(id, taskCancel)
			defer func() {
				taskCancel()
				m.workerCancels.Delete(id)
			}()
			m.processTask(taskCtx, id, task)
		}()
	}
}
```

- [ ] **Step 4: Update `mockConsumer` in `worker_test.go` to satisfy the new `Consumer` interface**

The `Consumer` interface now has `TryPopTask`, `TryPopOllamaTask`, and `PeekMinDepth`. Add stub implementations to the mock (return `nil, nil` and `0, false` respectively):

```go
func (m *mockConsumer) TryPopTask(_ context.Context) (*queue.Task, error) {
    return nil, nil
}

func (m *mockConsumer) TryPopOllamaTask(_ context.Context) (*queue.Task, error) {
    return nil, nil
}

func (m *mockConsumer) PeekMinDepth(_ context.Context, _ string) (float64, bool) {
    return 0, false
}
```

- [ ] **Step 5: Run tests to verify they pass**

```bash
go test ./internal/worker/... -v 2>&1 | tail -30
```

Expected: all existing worker tests PASS (the mock stubs satisfy the interface).

- [ ] **Step 6: Commit**

```bash
git add internal/worker/manager.go internal/worker/worker_test.go
git commit -m "feat(worker): peek-then-pop loop with Ollama capacity gating"
```

---

## Task 4: API + UI — updated queue response shape

**Files:**
- Modify: `internal/api/handlers_queue.go`
- Modify: `web/src/api/queue.ts`
- Modify: `web/src/stores/queues.ts`
- Modify: `web/src/composables/useWebSocket.ts`
- Modify: `web/src/views/QueueView.vue`

### Context

`handlers_queue.go` currently returns `{"pending": N, "workers": [...]}`. It reads pending with `s.sizer.Len(ctx, queue.QueueTasksKey)`.

`web/src/stores/queues.ts` has `pending: ref(0)` and `setQueuePending(p)`.

`useWebSocket.ts` line 51: `queue_update: (p: any) => queuesStore.setQueuePending(p.pending ?? 0)`.

`QueueView.vue` shows one "Pending" card reading `queuesStore.pending`.

---

- [ ] **Step 1: Update `handlers_queue.go`**

Replace the entire file:

```go
package api

import (
	"net/http"

	"github.com/amir-the-h/iran-watcher/internal/queue"
)

func (s *Server) handleGetQueue(w http.ResponseWriter, r *http.Request) {
	var pendingTasks, pendingOllama int64
	if s.sizer != nil {
		pendingTasks, _ = s.sizer.Len(r.Context(), queue.QueueTasksKey)
		pendingOllama, _ = s.sizer.Len(r.Context(), queue.QueueOllamaTasksKey)
	}

	var workers any = []any{}
	if s.pool != nil {
		workers = s.pool.Stats().Workers
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"pending_tasks":  pendingTasks,
		"pending_ollama": pendingOllama,
		"workers":        workers,
	})
}
```

- [ ] **Step 2: Update the queue handler test**

In `internal/api/handlers_queue_test.go`, update `TestHandleGetQueue_Shape` to check for `pending_tasks` and `pending_ollama` instead of `pending`:

```go
func TestHandleGetQueue_Shape(t *testing.T) {
	srv := &Server{sizer: &stubSizerQueue{pending: 5}, pool: &stubPoolStats{}}
	req := httptest.NewRequest(http.MethodGet, "/api/queue", nil)
	rr := httptest.NewRecorder()
	srv.handleGetQueue(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rr.Code)
	}
	var body map[string]any
	if err := json.NewDecoder(rr.Body).Decode(&body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if _, ok := body["pending_tasks"]; !ok {
		t.Error("response missing 'pending_tasks'")
	}
	if _, ok := body["pending_ollama"]; !ok {
		t.Error("response missing 'pending_ollama'")
	}
	if _, ok := body["workers"]; !ok {
		t.Error("response missing 'workers'")
	}
	if _, ok := body["pending"]; ok {
		t.Error("response should not contain legacy 'pending' key")
	}
	if _, ok := body["queues"]; ok {
		t.Error("response should not contain 'queues'")
	}
}
```

- [ ] **Step 3: Update `web/src/api/queue.ts`**

Replace the entire file:

```typescript
import { apiFetch } from './client'

export interface WorkerInfo {
  id: number
  status: 'active' | 'idle'
  action_id: number
  action: string
  pipeline: string
  tweet_id: string
  depth: number
  started_at: string
  duration_ms: number
  backend_name: string
}

export interface QueueResponse {
  pending_tasks: number
  pending_ollama: number
  workers: WorkerInfo[]
}

export const getQueue = () => apiFetch<QueueResponse>('/api/queue')

export const flushQueue = () =>
  apiFetch<{ flushed: boolean }>('/api/queue', { method: 'DELETE' })

export const flushWorkerBuffer = () =>
  apiFetch<{ flushed: number }>('/api/workers/buffer', { method: 'DELETE' })
```

- [ ] **Step 4: Update `web/src/stores/queues.ts`**

Replace the entire file:

```typescript
// web/src/stores/queues.ts
import { defineStore } from 'pinia'
import { ref } from 'vue'
import type { WorkerInfo } from '../api/queue'

export type { WorkerInfo }

export const useQueuesStore = defineStore('queues', () => {
  const pendingTasks  = ref(0)
  const pendingOllama = ref(0)
  const workers       = ref<WorkerInfo[]>([])

  // Called on initial HTTP fetch (onMounted in QueueView).
  function setFromResponse(pt: number, po: number, w: WorkerInfo[]) {
    pendingTasks.value  = pt
    pendingOllama.value = po
    workers.value       = w
  }

  // Called on queue_update WebSocket event.
  function setQueuePending(pt: number, po: number) {
    pendingTasks.value  = pt
    pendingOllama.value = po
  }

  // Called on worker_stats WebSocket event.
  function setWorkers(w: WorkerInfo[]) {
    workers.value = w
  }

  return { pendingTasks, pendingOllama, workers, setFromResponse, setQueuePending, setWorkers }
})
```

- [ ] **Step 5: Update `web/src/composables/useWebSocket.ts`**

Replace line 51 (`queue_update` handler):

```typescript
// OLD:
queue_update: (p: any) => queuesStore.setQueuePending(p.pending ?? 0),

// NEW:
queue_update: (p: any) => queuesStore.setQueuePending(p.pending_tasks ?? 0, p.pending_ollama ?? 0),
```

- [ ] **Step 6: Update `web/src/views/QueueView.vue`**

Replace the `fetchQueue` call and the Pending stat card. Find the `fetchQueue` function and update it:

```typescript
async function fetchQueue() {
  try {
    const data = await getQueue()
    queuesStore.setFromResponse(data.pending_tasks, data.pending_ollama, data.workers)
  } catch { /* ignore */ }
}
```

Find the single "Pending" `<n-statistic>` card in the template and replace it with two cards. The exact surrounding markup will vary — look for the card that displays `queuesStore.pending` and replace with:

```html
<n-statistic label="Pending (tasks)" :value="queuesStore.pendingTasks" />
<n-statistic label="Pending (Ollama)" :value="queuesStore.pendingOllama" />
```

- [ ] **Step 7: Check for any other references to `queuesStore.pending` or `p.pending`**

```bash
grep -rn "queuesStore\.pending\b\|\.pending\b" web/src/ --include="*.ts" --include="*.vue" | grep -v "pendingTasks\|pendingOllama\|node_modules"
```

Fix any remaining references to use `pendingTasks` or `pendingOllama` as appropriate.

- [ ] **Step 8: Commit**

```bash
git add internal/api/handlers_queue.go internal/api/handlers_queue_test.go \
        web/src/api/queue.ts web/src/stores/queues.ts \
        web/src/composables/useWebSocket.ts web/src/views/QueueView.vue
git commit -m "feat(api+ui): pending_tasks/pending_ollama split in queue response and store"
```

---

## Task 5: Build verification + CLAUDE.md update

**Files:**
- Modify: `CLAUDE.md`

---

- [ ] **Step 1: Build Docker image**

```bash
docker compose build iran-watcher 2>&1 | tail -20
```

Expected: `Image iran-watcher:local Built` with no error.

- [ ] **Step 2: Update `CLAUDE.md`**

Find and update these sections in `CLAUDE.md`:

**Architecture summary** — change `iran-watcher:tasks` to mention both keys:

```
# OLD:
Task push to single global Redis sorted set `iran-watcher:tasks` (score=Depth)

# NEW:
Task push to Redis sorted sets: `iran-watcher:tasks` (ONNX/internal/notification, score=Depth)
and `iran-watcher:tasks:ollama` (Ollama only, score=Depth)
```

**Simplified event-driven worker bullet** — update the worker description to mention two-queue routing:

```
# OLD (worker description):
workers pop from the single global queue

# NEW:
workers use a peek-then-pop loop: read the minimum depth from both queues
(`iran-watcher:tasks` and `iran-watcher:tasks:ollama`), pop from whichever
has the globally lowest depth; non-Ollama tasks are always immediately runnable;
Ollama tasks are only popped when `BackendPool.IsAvailable()` returns true;
workers block on `iran-watcher:tasks` with a 250ms timeout when both queues
are empty or no Ollama capacity exists; `task.IsOllama bool` set at every
push call site routes tasks to the correct key
```

**Dead-letter bullet** — update `FlushQueue` description:

```
# OLD:
`FlushQueue` issues a single `DEL iran-watcher:tasks`

# NEW:
`FlushQueue` issues `DEL iran-watcher:tasks iran-watcher:tasks:ollama`
```

**`queue_update` WebSocket event shape** — update payload:

```
# OLD:
`queue_update` (payload: `{pending}` — global pending count only)

# NEW:
`queue_update` (payload: `{pending_tasks, pending_ollama}` — separate counts for
non-Ollama and Ollama queues)
```

**`GET /api/queue` response** — update:

```
# OLD:
`GET /api/queue` (global queue status: `{pending: int64, workers: []WorkerInfo}`)

# NEW:
`GET /api/queue` (global queue status: `{pending_tasks: int64, pending_ollama: int64, workers: []WorkerInfo}`)
```

- [ ] **Step 3: Commit**

```bash
git add CLAUDE.md
git commit -m "docs: update CLAUDE.md for two-queue Ollama separation"
```

---

## Self-Review Checklist

After writing this plan, checked against the spec:

- ✅ `QueueOllamaTasksKey = "iran-watcher:tasks:ollama"` — Task 1
- ✅ `IsOllama bool` on `Task` — Task 1
- ✅ `PushTask` routes on `IsOllama` — Task 1
- ✅ `TryPopTask`, `TryPopOllamaTask`, `PeekMinDepth` on `Consumer` — Task 1
- ✅ `TotalTaskLen` sums both queues — Task 1
- ✅ `FlushQueue` deletes both keys — Task 1
- ✅ `queue_update` broadcast shape: `{pending_tasks, pending_ollama}` — Task 1
- ✅ `isOllamaAction` helper — Task 2
- ✅ `IsOllama` set on all pushed tasks in `pipeline.go` — Task 2
- ✅ `pushContinuationTask` gains `cache` param — Task 2
- ✅ Peek-then-pop worker loop — Task 3
- ✅ 250ms block timeout — Task 3
- ✅ Safety-net re-queue with no sleep — Task 3
- ✅ `100ms sleep removed` — Task 3
- ✅ API returns `pending_tasks` and `pending_ollama` — Task 4
- ✅ Store split into `pendingTasks` + `pendingOllama` — Task 4
- ✅ WebSocket handler updated — Task 4
- ✅ Two pending cards in QueueView — Task 4
- ✅ CLAUDE.md updated — Task 5
