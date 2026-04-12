# Per-Action Queue Revert + Ollama Concurrency Cap — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Revert the queue from two global sorted sets back to per-action Redis lists with BRPOP, and add an `ollamaActive` semaphore so workers never pop more Ollama tasks than there is backend capacity.

**Architecture:** Per-action `LPush`/`BRPOP` queues in Redis (one list per action ID). Workers call `buildReadyKeys()` to build a set of keys to BRPOP across — ONNX/internal/notification keys are always included; Ollama keys are included only when `IsAvailable()` AND `ollamaActive < pool.TotalCapacity()`. When no keys are ready, workers block in `WaitForAvailability` (sync.Cond, zero Redis traffic).

**Tech Stack:** Go 1.25.5, `github.com/redis/go-redis/v9`, `sync/atomic`, `sync.Cond` (already in BackendPool)

---

## File Map

| File | Change |
|------|--------|
| `internal/queue/queue.go` | Replace constants with function; remove Depth/IsOllama from Task; revert Consumer interface |
| `internal/queue/redis.go` | Revert to LPush/BRPop/LLen; SCAN-based TotalTaskLen/FlushQueue; fix broadcast payload |
| `internal/queue/queue_test.go` | Rewrite: remove sorted-set fakes/tests, add per-action key tests |
| `internal/inference/backend_pool.go` | Add TotalCapacity() method |
| `internal/inference/backend_pool_test.go` | Add TestBackendPool_TotalCapacity |
| `internal/worker/pool.go` | Remove Depth from WorkerInfo |
| `internal/worker/manager.go` | Add ollamaActive; add buildReadyKeys(); replace peek-then-pop with BRPop; remove Depth refs |
| `internal/worker/pipeline.go` | Remove depth param from dispatchInitialNode; remove IsOllama/Depth from all task constructions |
| `internal/api/handlers_tweets.go` | Remove IsOllama field from task construction |
| `internal/api/handlers_summaries.go` | Remove IsOllama field from task constructions (2 sites) |
| `internal/api/handlers_queue.go` | Recompute pending_tasks via TotalTaskLen; pending_ollama via actionCache iteration |
| `internal/api/handlers_queue_test.go` | Keep passing (no shape change needed, nil actionCache returns 0 ollama) |
| `CLAUDE.md` | Update Key Design Decisions queue bullet |

---

## Task 1: Revert `internal/queue/queue.go`

**Files:**
- Modify: `internal/queue/queue.go`

- [ ] **Step 1: Write the new queue.go**

Replace the entire file content:

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
	SubPipelineEntryNodeID *int64         `json:"sub_pipeline_entry_node_id,omitempty"`
	Context                map[string]any `json:"context"`
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

- [ ] **Step 2: Verify the file compiles (the rest of the package will break until Task 2)**

```bash
cd /mnt/storage/shared/projects/personal/iran-watcher
go build ./internal/queue/ 2>&1 | head -30
```

Expected: errors referencing `redis.go` using removed constants/types. That is fine — continue to Task 2.

---

## Task 2: Revert `internal/queue/redis.go`

**Files:**
- Modify: `internal/queue/redis.go`

- [ ] **Step 1: Write the new redis.go**

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
// Defining it as an interface allows injection of test doubles.
type redisClient interface {
	// List operations — task queues and dead-letter queue.
	LPush(ctx context.Context, key string, values ...interface{}) *redis.IntCmd
	BRPop(ctx context.Context, timeout time.Duration, keys ...string) *redis.StringSliceCmd
	LLen(ctx context.Context, key string) *redis.IntCmd
	LRange(ctx context.Context, key string, start, stop int64) *redis.StringSliceCmd
	Del(ctx context.Context, keys ...string) *redis.IntCmd
}

// scanner is implemented by redis clients that support SCAN (e.g. *redis.Client).
// Test fakes that omit this interface cause TotalTaskLen and FlushQueue to no-op.
type scanner interface {
	Scan(context.Context, uint64, string, int64) *redis.ScanCmd
}

// RedisQueue implements both Producer and Consumer using Redis lists.
// PushTask uses LPUSH; PopTask uses BRPOP (blocking right-pop from a left-push queue).
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

// PushTask serializes a Task to JSON and LPUSHes it onto the per-action tasks queue.
func (q *RedisQueue) PushTask(ctx context.Context, task *Task) error {
	b, err := json.Marshal(task)
	if err != nil {
		return fmt.Errorf("queue: marshal task: %w", err)
	}
	cmd := q.client.LPush(ctx, QueueTasksKey(task.ActionID), b)
	if err := cmd.Err(); err != nil {
		return fmt.Errorf("queue: push task: %w", err)
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
}

// PopTask blocks until a Task is available on one of the given keys or the
// timeout elapses. Returns (nil, nil) on timeout or when no keys are provided.
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
	if q.broadcaster != nil {
		pending, _ := q.client.LLen(ctx, res[0]).Result()
		q.broadcaster.Broadcast(hub.Event{
			Type: "queue_update",
			Payload: map[string]int64{
				"action_id": t.ActionID,
				"pending":   pending,
			},
		})
	}
	return &t, nil
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
	n, err := q.client.LLen(ctx, queue).Result()
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
			continue // skip malformed entries
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

// TotalTaskLen sums the lengths of all per-action task queues by scanning for
// keys matching iran-watcher:tasks:*. Returns 0 if the underlying client does
// not support SCAN (e.g. test fakes).
func (q *RedisQueue) TotalTaskLen(ctx context.Context) (int64, error) {
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
			if err != nil {
				continue
			}
			total += n
		}
		cursor = next
		if cursor == 0 {
			break
		}
	}
	return total, nil
}

// FlushQueue deletes all per-action task queues by scanning for
// keys matching iran-watcher:tasks:* and deleting them in batches.
// Returns nil if the client does not support SCAN.
func (q *RedisQueue) FlushQueue(ctx context.Context) error {
	s, ok := q.client.(scanner)
	if !ok {
		return nil
	}
	var cursor uint64
	for {
		keys, next, err := s.Scan(ctx, cursor, QueueTasksPrefix+"*", 100).Result()
		if err != nil {
			return fmt.Errorf("queue: flush: scan: %w", err)
		}
		if len(keys) > 0 {
			if err := q.client.Del(ctx, keys...).Err(); err != nil {
				return fmt.Errorf("queue: flush: del: %w", err)
			}
		}
		cursor = next
		if cursor == 0 {
			break
		}
	}
	return nil
}
```

- [ ] **Step 2: Verify queue package compiles**

```bash
go build ./internal/queue/
```

Expected: success (or errors from other packages referencing removed constants — fix those in later tasks).

- [ ] **Step 3: Commit**

```bash
git add internal/queue/queue.go internal/queue/redis.go
git commit -m "feat(queue): revert to per-action LPush/BRPop lists"
```

---

## Task 3: Rewrite `internal/queue/queue_test.go`

**Files:**
- Modify: `internal/queue/queue_test.go`

- [ ] **Step 1: Write the new test file**

Replace the entire file:

```go
package queue

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/amir-the-h/iran-watcher/internal/hub"
	"github.com/redis/go-redis/v9"
)

// ---------------------------------------------------------------------------
// fakeRedisClient — implements redisClient for unit tests.
// BRPop returns redis.Nil (timeout) by default.
// ---------------------------------------------------------------------------

type fakeRedisClient struct {
	brpopErr    error
	brpopResult []string
	llenResult  map[string]int64
	deleted     []string
}

func (f *fakeRedisClient) LPush(_ context.Context, _ string, _ ...interface{}) *redis.IntCmd {
	return redis.NewIntResult(1, nil)
}

func (f *fakeRedisClient) BRPop(_ context.Context, _ time.Duration, _ ...string) *redis.StringSliceCmd {
	err := f.brpopErr
	if err == nil && len(f.brpopResult) == 0 {
		err = redis.Nil
	}
	return redis.NewStringSliceResult(f.brpopResult, err)
}

func (f *fakeRedisClient) LLen(_ context.Context, key string) *redis.IntCmd {
	if f.llenResult != nil {
		return redis.NewIntResult(f.llenResult[key], nil)
	}
	return redis.NewIntResult(0, nil)
}

func (f *fakeRedisClient) LRange(_ context.Context, _ string, _, _ int64) *redis.StringSliceCmd {
	return redis.NewStringSliceResult(nil, nil)
}

func (f *fakeRedisClient) Del(_ context.Context, keys ...string) *redis.IntCmd {
	f.deleted = append(f.deleted, keys...)
	return redis.NewIntResult(int64(len(keys)), nil)
}

// captureBroadcaster records all Broadcast calls for test assertions.
type captureBroadcaster struct {
	events []hub.Event
}

func (b *captureBroadcaster) Broadcast(e hub.Event) {
	b.events = append(b.events, e)
}

// ---------------------------------------------------------------------------
// Test 1: QueueTasksKey formats correctly
// ---------------------------------------------------------------------------

func TestQueueTasksKey_Format(t *testing.T) {
	if got := QueueTasksKey(42); got != "iran-watcher:tasks:42" {
		t.Errorf("QueueTasksKey(42) = %q, want %q", got, "iran-watcher:tasks:42")
	}
	if got := QueueTasksKey(1); got != "iran-watcher:tasks:1" {
		t.Errorf("QueueTasksKey(1) = %q, want %q", got, "iran-watcher:tasks:1")
	}
}

// ---------------------------------------------------------------------------
// Test 2: FailedTweet JSON round-trip
// ---------------------------------------------------------------------------

func TestFailedTweet_JSONRoundTrip(t *testing.T) {
	ev, _ := json.Marshal(TweetEvent{TweetID: "123", Text: "Hello Iran", AuthorHandle: "testuser", RuleIDs: []int64{1}})
	original := &FailedTweet{
		Task: &Task{TriggerType: "new_tweet", Event: json.RawMessage(ev), ActionID: 42},
		FailedModels: []FailedModel{
			{Name: "sentiment", Error: "model not loaded", Timestamp: time.Date(2026, 3, 23, 0, 0, 0, 0, time.UTC)},
		},
	}
	data, err := json.Marshal(original)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var got FailedTweet
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if got.Task == nil || got.Task.ActionID != 42 {
		t.Fatalf("task round-trip failed: got %+v", got.Task)
	}
	if len(got.FailedModels) != 1 || got.FailedModels[0].Name != "sentiment" {
		t.Errorf("failed models round-trip failed: got %+v", got.FailedModels)
	}
}

// ---------------------------------------------------------------------------
// Test 3: Task JSON round-trip (no Depth or IsOllama fields)
// ---------------------------------------------------------------------------

func TestTask_JSONRoundTrip(t *testing.T) {
	ev, _ := json.Marshal(TweetEvent{TweetID: "789", Text: "Breaking news", AuthorHandle: "newsbot", RuleIDs: []int64{1, 2}})
	original := &Task{
		TriggerType: "new_tweet",
		Event:       json.RawMessage(ev),
		ActionID:    7,
		Context:     map[string]any{"foo": "bar"},
	}
	data, err := json.Marshal(original)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var got Task
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if got.ActionID != 7 {
		t.Errorf("ActionID: got %d, want 7", got.ActionID)
	}
	if got.TriggerType != "new_tweet" {
		t.Errorf("TriggerType: got %q, want new_tweet", got.TriggerType)
	}
}

// ---------------------------------------------------------------------------
// Test 4: PushTask routes to per-action key (LPush)
// ---------------------------------------------------------------------------

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

func TestPushTask_RoutesToPerActionKey(t *testing.T) {
	fake := &pushCaptureFakeClient{}
	q := &RedisQueue{client: fake}
	task := &Task{TriggerType: "new_tweet", ActionID: 42, Context: map[string]any{}}
	if err := q.PushTask(context.Background(), task); err != nil {
		t.Fatalf("PushTask: %v", err)
	}
	want := "iran-watcher:tasks:42"
	if fake.pushedKey != want {
		t.Errorf("pushed to key %q, want %q", fake.pushedKey, want)
	}
}

func TestPushTask_BroadcastsActionPending(t *testing.T) {
	fake := &pushCaptureFakeClient{}
	bc := &captureBroadcaster{}
	q := &RedisQueue{client: fake, broadcaster: bc}
	task := &Task{TriggerType: "new_tweet", ActionID: 7, Context: map[string]any{}}
	if err := q.PushTask(context.Background(), task); err != nil {
		t.Fatalf("PushTask: %v", err)
	}
	if len(bc.events) != 1 {
		t.Fatalf("expected 1 broadcast, got %d", len(bc.events))
	}
	if bc.events[0].Type != "queue_update" {
		t.Errorf("event type: got %q, want queue_update", bc.events[0].Type)
	}
	payload, ok := bc.events[0].Payload.(map[string]int64)
	if !ok {
		t.Fatalf("unexpected payload type %T", bc.events[0].Payload)
	}
	if _, ok := payload["action_id"]; !ok {
		t.Error("broadcast payload missing 'action_id'")
	}
	if _, ok := payload["pending"]; !ok {
		t.Error("broadcast payload missing 'pending'")
	}
}

func TestPushTask_NoBroadcastWithoutBroadcaster(t *testing.T) {
	fake := &pushCaptureFakeClient{}
	q := &RedisQueue{client: fake} // no broadcaster
	task := &Task{TriggerType: "new_tweet", ActionID: 3, Context: map[string]any{}}
	if err := q.PushTask(context.Background(), task); err != nil {
		t.Fatalf("PushTask: %v", err)
	}
	// No panic = pass
}

// ---------------------------------------------------------------------------
// Test 5: PopTask — timeout returns (nil, nil)
// ---------------------------------------------------------------------------

func TestPopTask_ReturnsNilOnTimeout(t *testing.T) {
	q := &RedisQueue{client: &fakeRedisClient{brpopErr: redis.Nil}}
	task, err := q.PopTask(context.Background(), 1*time.Second, "iran-watcher:tasks:1")
	if err != nil {
		t.Fatalf("expected nil error on timeout, got: %v", err)
	}
	if task != nil {
		t.Fatalf("expected nil task on timeout, got: %+v", task)
	}
}

func TestPopTask_NoKeysReturnsNil(t *testing.T) {
	q := &RedisQueue{client: &fakeRedisClient{}}
	task, err := q.PopTask(context.Background(), 1*time.Second) // no keys
	if err != nil {
		t.Fatalf("expected nil error, got: %v", err)
	}
	if task != nil {
		t.Fatalf("expected nil task when no keys, got: %+v", task)
	}
}

// ---------------------------------------------------------------------------
// Test 6: PopTask — success path with broadcast
// ---------------------------------------------------------------------------

type brpopSuccessFakeClient struct {
	fakeRedisClient
	result []string // [key, json]
}

func (f *brpopSuccessFakeClient) BRPop(_ context.Context, _ time.Duration, _ ...string) *redis.StringSliceCmd {
	return redis.NewStringSliceResult(f.result, nil)
}

func TestPopTask_BroadcastsActionPending(t *testing.T) {
	ev, _ := json.Marshal(TweetEvent{TweetID: "1", Text: "test", AuthorHandle: "user"})
	taskJSON, _ := json.Marshal(&Task{TriggerType: "new_tweet", ActionID: 7, Event: json.RawMessage(ev), Context: map[string]any{}})
	fake := &brpopSuccessFakeClient{
		fakeRedisClient: fakeRedisClient{llenResult: map[string]int64{"iran-watcher:tasks:7": 5}},
		result:          []string{"iran-watcher:tasks:7", string(taskJSON)},
	}
	bc := &captureBroadcaster{}
	q := &RedisQueue{client: fake, broadcaster: bc}

	task, err := q.PopTask(context.Background(), 1*time.Second, "iran-watcher:tasks:7")
	if err != nil {
		t.Fatalf("PopTask: %v", err)
	}
	if task == nil {
		t.Fatal("expected non-nil task")
	}
	if task.ActionID != 7 {
		t.Errorf("ActionID: got %d, want 7", task.ActionID)
	}
	if len(bc.events) != 1 {
		t.Fatalf("expected 1 broadcast event, got %d", len(bc.events))
	}
	payload, ok := bc.events[0].Payload.(map[string]int64)
	if !ok {
		t.Fatalf("expected map[string]int64 payload, got %T", bc.events[0].Payload)
	}
	if payload["action_id"] != 7 {
		t.Errorf("broadcast action_id = %d, want 7", payload["action_id"])
	}
	if payload["pending"] != 5 {
		t.Errorf("broadcast pending = %d, want 5", payload["pending"])
	}
}

// ---------------------------------------------------------------------------
// Test 7: PopFailed — timeout
// ---------------------------------------------------------------------------

func TestPopFailed_ReturnsNilOnTimeout(t *testing.T) {
	fake := &fakeRedisClient{brpopErr: redis.Nil}
	q := &RedisQueue{client: fake}
	ft, err := q.PopFailed(context.Background(), 1*time.Second)
	if err != nil {
		t.Fatalf("expected nil error on timeout, got: %v", err)
	}
	if ft != nil {
		t.Fatalf("expected nil FailedTweet on timeout, got: %+v", ft)
	}
}

// ---------------------------------------------------------------------------
// Test 8: TotalTaskLen — returns 0 when client has no SCAN support
// ---------------------------------------------------------------------------

func TestTotalTaskLen_ZeroWithoutScanner(t *testing.T) {
	// fakeRedisClient does not implement scanner — TotalTaskLen returns 0.
	q := &RedisQueue{client: &fakeRedisClient{}}
	total, err := q.TotalTaskLen(context.Background())
	if err != nil {
		t.Fatalf("TotalTaskLen: %v", err)
	}
	if total != 0 {
		t.Errorf("TotalTaskLen = %d, want 0 (no scanner)", total)
	}
}

// ---------------------------------------------------------------------------
// Test 9: FlushQueue — no-op when client has no SCAN support
// ---------------------------------------------------------------------------

func TestFlushQueue_NoopWithoutScanner(t *testing.T) {
	fake := &fakeRedisClient{}
	q := &RedisQueue{client: fake}
	if err := q.FlushQueue(context.Background()); err != nil {
		t.Fatalf("FlushQueue: %v", err)
	}
	if len(fake.deleted) != 0 {
		t.Errorf("expected no deletions without scanner, got %v", fake.deleted)
	}
}
```

- [ ] **Step 2: Run tests**

```bash
go test ./internal/queue/ -v 2>&1 | tail -30
```

Expected: all tests PASS.

- [ ] **Step 3: Commit**

```bash
git add internal/queue/queue_test.go
git commit -m "test(queue): rewrite tests for per-action BRPop queue"
```

---

## Task 4: Add `BackendPool.TotalCapacity()` + test

**Files:**
- Modify: `internal/inference/backend_pool.go`
- Modify: `internal/inference/backend_pool_test.go`

- [ ] **Step 1: Write failing test first**

Append to `internal/inference/backend_pool_test.go`:

```go
func TestBackendPool_TotalCapacity(t *testing.T) {
	p := NewBackendPool()
	defer p.Close()

	// Empty pool: capacity is 0.
	if got := p.TotalCapacity(); got != 0 {
		t.Errorf("empty pool TotalCapacity = %d, want 0", got)
	}

	p.Load([]BackendRecord{
		{ID: 1, Name: "a", URL: "http://a:11434", Enabled: true, MaxConcurrent: 2},
		{ID: 2, Name: "b", URL: "http://b:11434", Enabled: true, MaxConcurrent: 3},
		{ID: 3, Name: "c", URL: "http://c:11434", Enabled: false, MaxConcurrent: 5}, // disabled: excluded
	})

	if got := p.TotalCapacity(); got != 5 {
		t.Errorf("TotalCapacity = %d, want 5 (2+3, disabled excluded)", got)
	}
}

func TestBackendPool_TotalCapacity_UnhealthyExcluded(t *testing.T) {
	p := NewBackendPool()
	defer p.Close()

	p.Load([]BackendRecord{
		{ID: 1, Name: "healthy", URL: "http://a:11434", Enabled: true, MaxConcurrent: 4},
		{ID: 2, Name: "unhealthy", URL: "http://b:11434", Enabled: true, MaxConcurrent: 2},
	})

	// Mark backend 2 unhealthy.
	p.mu.Lock()
	p.backends[2].healthy = false
	p.mu.Unlock()

	if got := p.TotalCapacity(); got != 4 {
		t.Errorf("TotalCapacity = %d, want 4 (unhealthy backend excluded)", got)
	}
}
```

- [ ] **Step 2: Run test to confirm it fails**

```bash
go test ./internal/inference/ -run "TestBackendPool_TotalCapacity" -v 2>&1
```

Expected: FAIL — `p.TotalCapacity undefined`.

- [ ] **Step 3: Implement TotalCapacity in backend_pool.go**

Add after the `IsAnyAvailable` method (around line 258):

```go
// TotalCapacity returns the sum of max_concurrent for all healthy, enabled backends.
// Returns 0 if the pool is empty or all backends are disabled/unhealthy.
func (p *BackendPool) TotalCapacity() int32 {
	p.mu.Lock()
	defer p.mu.Unlock()
	var total int32
	for _, b := range p.backends {
		if b.enabled && b.healthy {
			total += int32(b.concurrencyLimit())
		}
	}
	return total
}
```

- [ ] **Step 4: Run tests to confirm they pass**

```bash
go test ./internal/inference/ -run "TestBackendPool_TotalCapacity" -v 2>&1
```

Expected: PASS.

- [ ] **Step 5: Run full inference test suite**

```bash
go test ./internal/inference/ -v 2>&1 | tail -20
```

Expected: all tests PASS.

- [ ] **Step 6: Commit**

```bash
git add internal/inference/backend_pool.go internal/inference/backend_pool_test.go
git commit -m "feat(inference): add BackendPool.TotalCapacity() for Ollama concurrency cap"
```

---

## Task 5: Remove `Depth` from `WorkerInfo` in `internal/worker/pool.go`

**Files:**
- Modify: `internal/worker/pool.go`

- [ ] **Step 1: Remove the Depth field**

In `pool.go`, find the `WorkerInfo` struct and remove the `Depth` line:

```go
// WorkerInfo describes a single worker slot.
// Idle workers have Status="idle" and zero values for all other fields.
type WorkerInfo struct {
	ID          int       `json:"id"`
	Status      string    `json:"status"`       // "active" | "idle"
	ActionID    int64     `json:"action_id"`
	Action      string    `json:"action"`
	Pipeline    string    `json:"pipeline"`
	TweetID     string    `json:"tweet_id"`
	StartedAt   time.Time `json:"started_at"`
	DurationMS  int64     `json:"duration_ms"`  // computed at snapshot time
	BackendName string    `json:"backend_name"` // display name; empty for non-Ollama
}
```

(Remove the `Depth int \`json:"depth"\`` line.)

- [ ] **Step 2: Verify compile (will fail in manager.go until Task 6)**

```bash
go build ./internal/worker/ 2>&1 | grep "Depth" | head -10
```

Expected: errors in `manager.go` referencing `Depth` — those are fixed in Task 6.

---

## Task 6: Revert `internal/worker/manager.go`

**Files:**
- Modify: `internal/worker/manager.go`

- [ ] **Step 1: Update Manager struct and imports**

Replace the import block and Manager struct. Remove `"math"` from imports:

```go
import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/amir-the-h/iran-watcher/internal/hub"
	"github.com/amir-the-h/iran-watcher/internal/inference"
	"github.com/amir-the-h/iran-watcher/internal/model"
	"github.com/amir-the-h/iran-watcher/internal/notifier"
	"github.com/amir-the-h/iran-watcher/internal/queue"
	"github.com/amir-the-h/iran-watcher/internal/store"
)

// Manager launches N worker goroutines that pop Tasks from Redis.
type Manager struct {
	totalWorkers  int
	consumer      queue.Consumer
	producer      queue.Producer
	store         store.Store
	cache         *ActionCache
	dispatcher    notifier.EventEmitter
	broadcaster   hub.Broadcaster
	logger        *slog.Logger
	active        atomic.Int32
	ollamaActive  atomic.Int32 // tracks in-flight Ollama tasks for the concurrency cap
	workerActions sync.Map // key: int (worker ID), value: WorkerInfo
	workerCancels sync.Map // key: int (worker ID), value: context.CancelFunc
}
```

- [ ] **Step 2: Replace runWorker with buildReadyKeys + BRPop approach**

Replace the entire `runWorker` method:

```go
// buildReadyKeys returns the Redis queue keys for all active actions whose
// pipeline is currently acquirable. ONNX, internal, and notification
// pipelines are always included. Ollama pipelines are included only when
// at least one assigned (or pool-wide) backend is healthy and enabled,
// AND the global ollamaActive count is below the pool's total capacity.
func (m *Manager) buildReadyKeys() []string {
	entries := m.cache.All()
	pool := m.cache.Pool()
	var cap int32
	if pool != nil {
		cap = pool.TotalCapacity()
	}
	keys := make([]string, 0, len(entries))
	for _, e := range entries {
		if av, ok := e.Pipeline.(inference.AvailabilityChecker); ok {
			// Ollama pipeline: gated by both backend-level availability and the
			// global in-flight count not exceeding total backend capacity.
			if av.IsAvailable() && m.ollamaActive.Load() < cap {
				keys = append(keys, queue.QueueTasksKey(e.Action.ID))
			}
		} else {
			// No AvailabilityChecker → always available (ONNX, internal, notification).
			keys = append(keys, queue.QueueTasksKey(e.Action.ID))
		}
	}
	return keys
}

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
			// Block until a backend becomes available or timeout fires, then re-evaluate.
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

		entry := m.cache.Get(task.ActionID)
		if entry == nil {
			if m.logger != nil {
				m.logger.Warn("worker: discarding task for unknown action_id — action inactive or deleted",
					"worker_id", id, "action_id", task.ActionID)
			}
			continue
		}

		// Track Ollama concurrency: increment before execution, decrement after.
		// BackendPool.Release() already calls cond.Broadcast() when the pipeline
		// returns its backend slot, waking workers in WaitForAvailability.
		isOllama := false
		if _, ok := entry.Pipeline.(inference.AvailabilityChecker); ok {
			isOllama = true
			m.ollamaActive.Add(1)
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

		if isOllama {
			m.ollamaActive.Add(-1)
		}
	}
}
```

- [ ] **Step 3: Remove Depth references from processTask and processDigestTask**

In `processTask`, find the `WorkerInfo` construction (~line 270) and remove `Depth: task.Depth`:

```go
info := WorkerInfo{
	ID:        workerID,
	Status:    "active",
	ActionID:  task.ActionID,
	Action:    entry.Action.Name,
	Pipeline:  entry.Pipeline.Name(),
	TweetID:   ev.TweetID,
	StartedAt: startedAt,
}
```

Also find the backend-notify callback's `WorkerInfo` construction (~line 333) and remove `Depth: task.Depth`:

```go
m.workerActions.Store(workerID, WorkerInfo{
	ID:          workerID,
	Status:      "active",
	ActionID:    task.ActionID,
	Action:      entry.Action.Name,
	Pipeline:    entry.Pipeline.Name(),
	TweetID:     ev.TweetID,
	StartedAt:   startedAt,
	BackendName: backend,
})
```

In `processDigestTask`, find its `WorkerInfo` construction (~line 477) and remove `Depth: task.Depth`:

```go
info := WorkerInfo{
	ID:        workerID,
	Status:    "active",
	ActionID:  task.ActionID,
	Action:    entry.Action.Name,
	Pipeline:  entry.Pipeline.Name(),
	StartedAt: startedAt,
}
```

Also find the backend-notify callback inside `processDigestTask` (~line 523) and remove `Depth: task.Depth`:

```go
m.workerActions.Store(workerID, WorkerInfo{
	ID:          workerID,
	Status:      "active",
	ActionID:    task.ActionID,
	Action:      entry.Action.Name,
	Pipeline:    entry.Pipeline.Name(),
	StartedAt:   startedAt,
	BackendName: backend,
})
```

Also update the `Flush` docstring — change "BZPopMin" to "BRPOP":

```go
// Flush cancels the context of every worker that is currently executing a
// task. Workers blocked in BRPOP (idle) are unaffected. Returns the count of interrupted tasks.
```

- [ ] **Step 4: Build worker package**

```bash
go build ./internal/worker/ 2>&1 | head -20
```

Expected: only errors from `pipeline.go` referencing `IsOllama`/`Depth` fields (fixed in Task 7), and errors from `api` package (fixed in Task 8). Worker manager itself should compile.

- [ ] **Step 5: Commit**

```bash
git add internal/worker/pool.go internal/worker/manager.go
git commit -m "feat(worker): restore buildReadyKeys+BRPop; add ollamaActive concurrency cap"
```

---

## Task 7: Remove `IsOllama` and `Depth` from `internal/worker/pipeline.go`

**Files:**
- Modify: `internal/worker/pipeline.go`

This file has 6 task-construction sites. Remove `IsOllama` and `Depth` / `depth` fields from all of them, and remove the `depth int` parameter from `dispatchInitialNode`.

- [ ] **Step 1: Remove `depth int` parameter from `dispatchInitialNode` signature**

Change the signature from:
```go
func dispatchInitialNode(
    ctx context.Context,
    st store.Store,
    producer queue.Producer,
    cache *ActionCache,
    graph *model.PipelineGraph,
    pipelineID int64,
    rootPipelineID int64,
    triggerType string,
    eventPayload []byte,
    tweetID string,
    node *model.PipelineNode,
    initCtx map[string]any,
    depth int,
    logger *slog.Logger,
) int {
```

To:
```go
func dispatchInitialNode(
    ctx context.Context,
    st store.Store,
    producer queue.Producer,
    cache *ActionCache,
    graph *model.PipelineGraph,
    pipelineID int64,
    rootPipelineID int64,
    triggerType string,
    eventPayload []byte,
    tweetID string,
    node *model.PipelineNode,
    initCtx map[string]any,
    logger *slog.Logger,
) int {
```

- [ ] **Step 2: Update the action-node task construction inside `dispatchInitialNode`**

Find the task construction for `model.PipelineNodeTypeAction` inside `dispatchInitialNode` and remove `Depth` and `IsOllama`:

```go
task := &queue.Task{
    TriggerType:    triggerType,
    Event:          eventPayload,
    ActionID:       *node.ActionID,
    PipelineNodeID: node.ID,
    PipelineID:     pipelineID,
    RootPipelineID: rootPipelineID,
    Context:        map[string]any{},
}
```

- [ ] **Step 3: Update the sub-pipeline task construction inside `dispatchInitialNode`**

Find the `subTask` construction for `model.PipelineNodeTypePipeline` inside `dispatchInitialNode` and remove `Depth` and `IsOllama`:

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
    Context:                map[string]any{},
}
```

- [ ] **Step 4: Update the recursive call inside `dispatchInitialNode`**

Find the recursive `dispatchInitialNode` call (inside the condition-node branch) and remove the `depth` argument. It currently passes `depth` as the second-to-last argument. Change to:

```go
dispatchInitialNode(ctx, st, producer, cache, graph, pipelineID,
    rootPipelineID, triggerType, eventPayload, tweetID, nextNode, initCtx, logger)
```

- [ ] **Step 5: Update callers in `PushInitialTasksForGraph` and `PushTasksFromNode`**

In `PushInitialTasksForGraph` (the loop over outgoing edges from startNode):
```go
pushed += dispatchInitialNode(ctx, st, producer, cache, graph, pipelineID,
    pipelineID, triggerType, eventPayload, tweetID, node, initCtx, logger)
```

In `PushTasksFromNode`:
```go
return dispatchInitialNode(ctx, st, producer, cache, graph, pipelineID,
    pipelineID, triggerType, eventPayload, tweetID, startNode, initCtx, logger)
```

- [ ] **Step 6: Update `parentTask` construction inside `signalSubPipelineLeafDone` in `PushContinuationTasks`**

Remove `Depth: task.Depth`:

```go
parentTask := &queue.Task{
    TriggerType:    task.TriggerType,
    Event:          task.Event,
    PipelineNodeID: *task.ParentPipelineNodeID,
    PipelineID:     parentPipelineID,
    RootPipelineID: task.RootPipelineID,
}
```

- [ ] **Step 7: Update sub-pipeline task constructions inside `PushContinuationTasks` continuation loop**

Find all `subTask` constructions in the continuation path (there are two — one after condition nodes, one after pipeline nodes) and remove `Depth` and `IsOllama` from both:

```go
subTask := &queue.Task{
    TriggerType:            task.TriggerType,
    Event:                  task.Event,
    ActionID:               *subActionNode.ActionID,
    PipelineNodeID:         subActionNode.ID,
    PipelineID:             *nextNode.SubPipelineID,   // or *targetNode.SubPipelineID
    RootPipelineID:         task.RootPipelineID,
    ParentPipelineNodeID:   &parentNodeID,
    SubPipelineEntryNodeID: &entryNodeID,
    Context:                copyContext(mergedCtx),
}
```

- [ ] **Step 8: Update `pushContinuationTask` helper**

Remove `Depth` and `IsOllama` from the task construction in `pushContinuationTask`:

```go
task := &queue.Task{
    TriggerType:            parent.TriggerType,
    Event:                  parent.Event,
    ActionID:               *node.ActionID,
    PipelineNodeID:         node.ID,
    PipelineID:             parent.PipelineID,
    RootPipelineID:         parent.RootPipelineID,
    ParentPipelineNodeID:   parent.ParentPipelineNodeID,
    SubPipelineEntryNodeID: parent.SubPipelineEntryNodeID,
    Context:                copyContext(mergedCtx),
}
```

- [ ] **Step 9: Build worker package**

```bash
go build ./internal/worker/ 2>&1 | head -20
```

Expected: success (or only errors from `api` package not yet fixed).

- [ ] **Step 10: Commit**

```bash
git add internal/worker/pipeline.go
git commit -m "feat(worker): remove IsOllama and Depth from all task construction sites"
```

---

## Task 8: Fix API handlers

**Files:**
- Modify: `internal/api/handlers_queue.go`
- Modify: `internal/api/handlers_tweets.go`
- Modify: `internal/api/handlers_summaries.go`

- [ ] **Step 1: Rewrite `handlers_queue.go`**

Replace the entire file:

```go
package api

import (
	"net/http"

	"github.com/amir-the-h/iran-watcher/internal/queue"
	"github.com/amir-the-h/iran-watcher/internal/worker"
)

func (s *Server) handleGetQueue(w http.ResponseWriter, r *http.Request) {
	var pendingTasks, pendingOllama int64
	if s.sizer != nil {
		pendingTasks, _ = s.sizer.TotalTaskLen(r.Context())
		// Compute pending_ollama by summing LLen for all Ollama action queues.
		if s.actionCache != nil {
			for _, entry := range s.actionCache.All() {
				if worker.IsOllamaAction(s.actionCache, entry.Action.ID) {
					n, _ := s.sizer.Len(r.Context(), queue.QueueTasksKey(entry.Action.ID))
					pendingOllama += n
				}
			}
		}
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

- [ ] **Step 2: Remove `IsOllama` from `handlers_tweets.go`**

Find the task construction in `handlers_tweets.go` (~line 240) that sets `IsOllama: worker.IsOllamaAction(s.actionCache, actionID)`. Remove that field from the struct literal.

Search for it:
```bash
grep -n "IsOllama" internal/api/handlers_tweets.go
```

Remove the `IsOllama: worker.IsOllamaAction(s.actionCache, actionID),` line from the task construction.

- [ ] **Step 3: Remove `IsOllama` from `handlers_summaries.go`** (2 sites)

```bash
grep -n "IsOllama" internal/api/handlers_summaries.go
```

Remove both `IsOllama: worker.IsOllamaAction(s.actionCache, actionID),` lines from the two task constructions in `handlers_summaries.go`.

- [ ] **Step 4: Build the full project**

```bash
go build ./... 2>&1 | head -30
```

Expected: success (zero errors).

- [ ] **Step 5: Run all tests**

```bash
go test ./... 2>&1 | tail -30
```

Expected: all packages PASS.

- [ ] **Step 6: Commit**

```bash
git add internal/api/handlers_queue.go internal/api/handlers_tweets.go internal/api/handlers_summaries.go
git commit -m "feat(api): revert queue handler to use per-action keys + TotalTaskLen"
```

---

## Task 9: Docker build verification + CLAUDE.md update

**Files:**
- Modify: `CLAUDE.md`

- [ ] **Step 1: Build the Docker image**

```bash
docker compose build --build-arg BUILDKIT_INLINE_CACHE=1 iran-watcher 2>&1 | tail -20
```

Expected: `Successfully built` or `exporting layers` — no compile errors.

- [ ] **Step 2: Update CLAUDE.md — Key Design Decisions queue bullet**

Find the `**At-most-once delivery**` / **Simplified event-driven worker** bullet in CLAUDE.md and update the queue description. Replace the current sorted-set / peek-then-pop description with:

> **Simplified event-driven worker** (`internal/worker.Manager`) — N worker goroutines each independently build a ready-keys list (`buildReadyKeys`): ONNX/internal/notification action keys are always included; Ollama action keys are included only when `IsAvailable()` AND `ollamaActive < pool.TotalCapacity()`; workers then block on `BRPOP` across those keys with a 1-second timeout; when no keys are ready (all Ollama, all slots taken), workers block in `WaitForAvailability` (sync.Cond, zero Redis traffic); `ollamaActive atomic.Int32` on Manager increments before executing an Ollama task and decrements after — capped at `BackendPool.TotalCapacity()` (sum of `max_concurrent` for healthy+enabled backends) to prevent workers from popping more Ollama tasks than there is backend capacity; `ActionCache` (`internal/worker.ActionCache`) is a hot-reloadable in-memory map of `action_id → (Action row + resolved Pipeline)` shared by the stream listener and all workers; on activate/deactivate the cache is fully rebuilt from DB

Also update the **At-most-once delivery** bullet to reference per-action keys:

> **At-most-once delivery** — acceptable because tweets are persisted to SQLite before queueing; per-action Redis lists (`iran-watcher:tasks:{action_id}`) use `LPUSH` / `BRPOP`; dead-letter list (`iran-watcher:failed`) uses the same; `FlushQueue` SCans and DELetes all `iran-watcher:tasks:*` keys; `DELETE /api/queue` flushes the pending task queue

- [ ] **Step 3: Commit CLAUDE.md**

```bash
git add CLAUDE.md
git commit -m "docs: update CLAUDE.md for per-action queue revert + ollamaActive cap"
```

---

## Self-Review

**Spec coverage check:**

| Spec requirement | Task |
|---|---|
| Queue reverts to per-action LPush/BRPop | Tasks 1–3 |
| Remove Depth, IsOllama from Task | Tasks 1, 7 |
| Consumer interface reverts (variadic keys) | Task 1 |
| TotalTaskLen → SCAN + LLen | Task 2 |
| FlushQueue → SCAN + DEL | Task 2 |
| Broadcast: action_id + pending | Task 2 |
| BackendPool.TotalCapacity() | Task 4 |
| Remove Depth from WorkerInfo | Task 5 |
| buildReadyKeys() restored + ollamaActive gate | Task 6 |
| runWorker uses BRPop (not peek-then-pop) | Task 6 |
| Remove Depth refs from processTask/processDigestTask | Task 6 |
| dispatchInitialNode: remove depth param | Task 7 |
| All pipeline.go task constructions: no IsOllama/Depth | Task 7 |
| handlers_queue.go: TotalTaskLen + actionCache iteration | Task 8 |
| Remove IsOllama from handlers_tweets/summaries | Task 8 |
| Docker build verified | Task 9 |
| CLAUDE.md updated | Task 9 |

**Placeholder scan:** No TBDs or "implement later" language found.

**Type consistency:** `QueueTasksKey(actionID int64)` used consistently in manager.go, pipeline.go, handlers_queue.go. `Consumer.PopTask(ctx, timeout, keys...)` used in manager.go. `WorkerInfo` without `Depth` used throughout manager.go.
