# Single Priority Queue Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Replace N per-action sorted sets with a single global `iran-watcher:tasks` sorted set, add full worker details to the queue page, and remove the broken `buildReadyKeys()` / multi-key `BZPopMin` pattern.

**Architecture:** One Redis sorted set (scored by `Depth`) replaces all per-action keys. Workers always pop from that single key, then do a post-pop availability check for Ollama. The queue page becomes worker-centric: one row per worker (all N workers always shown), with Status, Action, Type, Tweet, Depth, Duration, and Backend columns.

**Tech Stack:** Go (Redis sorted sets via `redis/go-redis/v9`), Vue 3 + Pinia, Naive UI data table.

---

## File Map

| File | Change |
|------|--------|
| `internal/queue/queue.go` | `QueueTasksKey` → string constant; remove `QueueTasksPrefix` + key function; remove `keys ...string` from `Consumer.PopTask` |
| `internal/queue/redis.go` | `PushTask` → global key; `PopTask` → no keys param; `TotalTaskLen` → single ZCard; `FlushQueue` → single DEL; `queue_update` broadcast → `{pending}` only; remove `scanner` interface |
| `internal/queue/queue_test.go` | Update key tests → assert constant; update `PopTask` signature; update `TotalTaskLen`/`FlushQueue` tests |
| `internal/worker/pool.go` | New `WorkerInfo` fields; `PoolStats.Workers` replaces `ActiveWorkers`; remove `ActionThroughput` |
| `internal/worker/manager.go` | Remove `buildReadyKeys`, `completionTrackers`, `recordCompletion`, `throughputPerAction`; update `runWorker` with post-pop check + anti-spin; update `processTask` + `processDigestTask` to populate new fields; update `Stats()` + `broadcastWorkerStats` |
| `internal/worker/rate.go` | Delete entirely (no longer used) |
| `internal/worker/rate_test.go` | Delete entirely |
| `internal/api/handlers_queue.go` | New response shape: `{pending, workers: []WorkerInfo}` |
| `web/src/api/queue.ts` | New `WorkerInfo` type; `QueueResponse = {pending, workers}` |
| `web/src/stores/queues.ts` | New state: `pending` + `workers`; remove `queues`, `workerStats`, old methods; add `setQueuePending`, `setWorkers` |
| `web/src/composables/useWebSocket.ts` | Update `worker_stats` and `queue_update` handlers |
| `web/src/views/QueueView.vue` | Replace action table with worker table; `formatDuration` helper; remove ETA/throughput logic |
| `CLAUDE.md` | Update queue key description and `worker_stats` event shape |

---

### Task 1: Queue layer — single global key

**Files:**
- Modify: `internal/queue/queue.go`
- Modify: `internal/queue/redis.go`
- Modify: `internal/queue/queue_test.go`

- [ ] **Step 1: Write the failing tests**

In `internal/queue/queue_test.go`, replace `TestQueueTasksKey_Format` and `TestQueueTasksKey_Zero` with a single test asserting the constant value, and add a `TestPopTask_NoKeysParam` compile test:

```go
// TestQueueTasksKey_IsConstant verifies the global queue key constant.
func TestQueueTasksKey_IsConstant(t *testing.T) {
    if QueueTasksKey != "iran-watcher:tasks" {
        t.Errorf("QueueTasksKey = %q, want %q", QueueTasksKey, "iran-watcher:tasks")
    }
}

// TestTotalTaskLen_SingleKey verifies TotalTaskLen uses ZCard on the global key.
func TestTotalTaskLen_SingleKey(t *testing.T) {
    fake := &fakeRedisClient{
        zcard: map[string]int64{
            "iran-watcher:tasks": 17,
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

// TestFlushQueue_SingleDel verifies FlushQueue issues a single DEL on the global key.
func TestFlushQueue_SingleDel(t *testing.T) {
    fake := &fakeRedisClient{}
    q := &RedisQueue{client: fake}
    if err := q.FlushQueue(context.Background()); err != nil {
        t.Fatalf("FlushQueue: %v", err)
    }
    if len(fake.deleted) != 1 || fake.deleted[0] != "iran-watcher:tasks" {
        t.Errorf("deleted keys = %v, want [iran-watcher:tasks]", fake.deleted)
    }
}
```

Also add a `deleted []string` field to `fakeRedisClient` and capture `Del` calls:

```go
type fakeRedisClient struct {
    // existing fields ...
    zcard   map[string]int64
    deleted []string
}

func (f *fakeRedisClient) Del(ctx context.Context, keys ...string) *redis.IntCmd {
    f.deleted = append(f.deleted, keys...)
    return redis.NewIntCmd(ctx)
}

func (f *fakeRedisClient) ZCard(ctx context.Context, key string) *redis.IntCmd {
    cmd := redis.NewIntCmd(ctx)
    if f.zcard != nil {
        cmd.SetVal(f.zcard[key])
    }
    return cmd
}
```

- [ ] **Step 2: Run tests to verify they fail**

```bash
docker compose run --rm --no-deps iran-watcher go test ./internal/queue/... -run "TestQueueTasksKey_IsConstant|TestTotalTaskLen_SingleKey|TestFlushQueue_SingleDel" -v 2>&1 | tail -30
```

Expected: compile errors — `QueueTasksKey` is a function, not a constant.

- [ ] **Step 3: Update `internal/queue/queue.go`**

Replace the prefix constant, key function, and `Consumer` interface:

```go
package queue

import (
    "context"
    "encoding/json"
    "time"
)

const (
    // QueueTasksKey is the single global Redis sorted-set key for all pending tasks.
    // Tasks are scored by Depth (lower = higher priority).
    QueueTasksKey = "iran-watcher:tasks"

    // QueueFailed is the dead-letter queue key.
    QueueFailed = "iran-watcher:failed"
)

// Task is a single unit of work pushed to iran-watcher:tasks.
type Task struct {
    TriggerType    string          `json:"trigger_type"`
    Event          json.RawMessage `json:"event"`
    ActionID       int64           `json:"action_id"`
    PipelineNodeID int64           `json:"pipeline_node_id,omitempty"`
    PipelineID     int64           `json:"pipeline_id,omitempty"`
    RootPipelineID       int64  `json:"root_pipeline_id,omitempty"`
    ParentPipelineNodeID *int64 `json:"parent_pipeline_node_id,omitempty"`
    SubPipelineEntryNodeID *int64 `json:"sub_pipeline_entry_node_id,omitempty"`
    Depth   int            `json:"depth,omitempty"`
    Context map[string]any `json:"context"`
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
// PopTask blocks until a task is available or the timeout elapses.
// Returns (nil, nil) on timeout.
type Consumer interface {
    PopTask(ctx context.Context, timeout time.Duration) (*Task, error)
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

- [ ] **Step 4: Update `internal/queue/redis.go`**

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
    ZCard(ctx context.Context, key string) *redis.IntCmd
}

// RedisQueue implements both Producer and Consumer.
// All tasks go into the single global sorted set iran-watcher:tasks.
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

// PushTask serializes a Task to JSON and ZAdds it to the global sorted set.
// Score = task.Depth so lower-depth (higher-priority) tasks are popped first.
// Sorted sets deduplicate by member string, so re-queuing the same task is idempotent.
func (q *RedisQueue) PushTask(ctx context.Context, task *Task) error {
    b, err := json.Marshal(task)
    if err != nil {
        return fmt.Errorf("queue: marshal task: %w", err)
    }
    if err := q.client.ZAdd(ctx, QueueTasksKey, redis.Z{
        Score:  float64(task.Depth),
        Member: string(b),
    }).Err(); err != nil {
        return fmt.Errorf("queue: push task: %w", err)
    }
    if q.broadcaster != nil {
        pending, _ := q.client.ZCard(ctx, QueueTasksKey).Result()
        q.broadcaster.Broadcast(hub.Event{
            Type:    "queue_update",
            Payload: map[string]int64{"pending": pending},
        })
    }
    return nil
}

// PopTask blocks until a Task is available on the global sorted set
// or the timeout elapses. Returns the task with the lowest Depth (score) first.
// Returns (nil, nil) on timeout.
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
    if q.broadcaster != nil {
        pending, _ := q.client.ZCard(ctx, QueueTasksKey).Result()
        q.broadcaster.Broadcast(hub.Event{
            Type:    "queue_update",
            Payload: map[string]int64{"pending": pending},
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

// Len returns the number of items currently in the named queue (sorted set or list).
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

// TotalTaskLen returns the total number of pending tasks in the global queue.
func (q *RedisQueue) TotalTaskLen(ctx context.Context) (int64, error) {
    return q.Len(ctx, QueueTasksKey)
}

// FlushQueue deletes the global task queue.
func (q *RedisQueue) FlushQueue(ctx context.Context) error {
    if err := q.client.Del(ctx, QueueTasksKey).Err(); err != nil {
        return fmt.Errorf("queue: flush: %w", err)
    }
    return nil
}
```

- [ ] **Step 5: Run all queue tests**

```bash
docker compose run --rm --no-deps iran-watcher go test ./internal/queue/... -v 2>&1 | tail -40
```

Expected: all pass. Fix any compile errors from the signature change before proceeding (the worker package will have compile errors — those are expected and will be fixed in Task 2).

- [ ] **Step 6: Commit**

```bash
git add internal/queue/queue.go internal/queue/redis.go internal/queue/queue_test.go
git commit -m "feat(queue): replace per-action keys with single global sorted set"
```

---

### Task 2: Worker layer — remove buildReadyKeys, expand WorkerInfo

**Files:**
- Modify: `internal/worker/pool.go`
- Modify: `internal/worker/manager.go`
- Delete: `internal/worker/rate.go`
- Delete: `internal/worker/rate_test.go`

- [ ] **Step 1: Update `internal/worker/pool.go`**

Replace the entire file:

```go
package worker

import (
    "context"
    "log/slog"
    "sync"
    "sync/atomic"
    "time"

    "github.com/amir-the-h/iran-watcher/internal/inference"
    "github.com/amir-the-h/iran-watcher/internal/notifier"
    "github.com/amir-the-h/iran-watcher/internal/queue"
    "github.com/amir-the-h/iran-watcher/internal/store"
)

// WorkerInfo describes a single worker slot.
// Idle workers have Status="idle" and zero values for all other fields.
type WorkerInfo struct {
    ID          int       `json:"id"`
    Status      string    `json:"status"`       // "active" | "idle"
    ActionID    int64     `json:"action_id"`
    Action      string    `json:"action"`
    Pipeline    string    `json:"pipeline"`
    TweetID     string    `json:"tweet_id"`
    Depth       int       `json:"depth"`
    StartedAt   time.Time `json:"started_at"`
    DurationMS  int64     `json:"duration_ms"`  // computed at snapshot time
    BackendName string    `json:"backend_name"` // display name; empty for non-Ollama
}

// PoolStats holds a snapshot of worker pool activity.
type PoolStats struct {
    Total   int          `json:"total"`
    Active  int          `json:"active"`
    Idle    int          `json:"idle"`
    Workers []WorkerInfo `json:"workers"` // all workers: active first, then idle
}

// Pool manages N worker goroutines that process tweets from the queue.
type Pool struct {
    count    int
    consumer queue.Consumer
    producer queue.Producer
    store    store.Store
    registry *inference.Registry
    logger   *slog.Logger
    emitter  notifier.EventEmitter
    active   atomic.Int32
}

// Stats returns a snapshot of current worker pool activity.
// Note: Workers slice is always empty for Pool. Use Manager for per-worker tracking.
func (p *Pool) Stats() PoolStats {
    active := int(p.active.Load())
    return PoolStats{Total: p.count, Active: active, Idle: p.count - active}
}

// NewPool creates a new Pool with the given number of workers and dependencies.
func NewPool(count int, consumer queue.Consumer, producer queue.Producer, s store.Store, reg *inference.Registry, emitter notifier.EventEmitter, logger *slog.Logger) *Pool {
    return &Pool{
        count:    count,
        consumer: consumer,
        producer: producer,
        store:    s,
        registry: reg,
        logger:   logger,
        emitter:  emitter,
    }
}

// Start launches count workers and blocks until ctx is cancelled.
// All workers are joined before Start returns.
func (p *Pool) Start(ctx context.Context) error {
    var wg sync.WaitGroup
    wg.Add(p.count)

    for i := range p.count {
        w := &worker{
            id:       i + 1,
            consumer: p.consumer,
            producer: p.producer,
            store:    p.store,
            logger:   p.logger,
            emitter:  p.emitter,
            active:   &p.active,
        }
        go func() {
            defer wg.Done()
            w.run(ctx)
        }()
    }

    wg.Wait()
    return nil
}
```

- [ ] **Step 2: Update `internal/worker/manager.go`**

Replace the entire file. Key changes: remove `buildReadyKeys`, `completionTrackers`, `recordCompletion`, `throughputPerAction`; update `runWorker` with post-pop check; update `processTask` and `processDigestTask` to store full `WorkerInfo`; update `Stats()` and `broadcastWorkerStats`:

```go
package worker

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
    workerActions sync.Map // key: int (worker ID), value: WorkerInfo
    workerCancels sync.Map // key: int (worker ID), value: context.CancelFunc
}

// WithDispatcher sets an optional event emitter used to fire digest_done/digest_failed events.
func (m *Manager) WithDispatcher(d notifier.EventEmitter) *Manager {
    m.dispatcher = d
    return m
}

// WithBroadcaster sets an optional WebSocket broadcaster for live UI updates.
func (m *Manager) WithBroadcaster(b hub.Broadcaster) *Manager {
    m.broadcaster = b
    return m
}

// broadcastWorkerStats pushes the current pool stats to all connected WS clients.
func (m *Manager) broadcastWorkerStats() {
    if m.broadcaster == nil {
        return
    }
    s := m.Stats()
    m.broadcaster.Broadcast(hub.Event{
        Type: "worker_stats",
        Payload: map[string]any{
            "total":   s.Total,
            "active":  s.Active,
            "idle":    s.Idle,
            "workers": s.Workers,
        },
    })
}

func NewManager(
    total int,
    consumer queue.Consumer,
    producer queue.Producer,
    st store.Store,
    cache *ActionCache,
    logger *slog.Logger,
) *Manager {
    return &Manager{
        totalWorkers: total,
        consumer:     consumer,
        producer:     producer,
        store:        st,
        cache:        cache,
        logger:       logger,
    }
}

// Stats returns a snapshot of worker pool activity, including all N worker slots.
// Active workers appear first (ordered by ID), followed by idle workers.
// DurationMS is computed at snapshot time from StartedAt.
func (m *Manager) Stats() PoolStats {
    active := int(m.active.Load())
    idle := m.totalWorkers - active
    if idle < 0 {
        idle = 0
    }

    // Collect active worker info from the sync.Map.
    activeMap := make(map[int]WorkerInfo, active)
    m.workerActions.Range(func(k, v any) bool {
        wi := v.(WorkerInfo)
        wi.DurationMS = time.Since(wi.StartedAt).Milliseconds()
        activeMap[k.(int)] = wi
        return true
    })

    workers := make([]WorkerInfo, 0, m.totalWorkers)
    // Active workers first (ascending ID order).
    for i := 1; i <= m.totalWorkers; i++ {
        if wi, ok := activeMap[i]; ok {
            workers = append(workers, wi)
        }
    }
    // Idle workers after (ascending ID order).
    for i := 1; i <= m.totalWorkers; i++ {
        if _, ok := activeMap[i]; !ok {
            workers = append(workers, WorkerInfo{ID: i, Status: "idle"})
        }
    }

    return PoolStats{
        Total:   m.totalWorkers,
        Active:  active,
        Idle:    idle,
        Workers: workers,
    }
}

// Start launches N worker goroutines. Blocks until ctx is cancelled.
func (m *Manager) Start(ctx context.Context) error {
    var wg sync.WaitGroup
    for i := range m.totalWorkers {
        wg.Add(1)
        go func(id int) {
            defer wg.Done()
            m.runWorker(ctx, id+1)
        }(i)
    }
    wg.Wait()
    return nil
}

func (m *Manager) runWorker(ctx context.Context, id int) {
    for {
        select {
        case <-ctx.Done():
            return
        default:
        }

        task, err := m.consumer.PopTask(ctx, 1*time.Second)
        if err != nil {
            if m.logger != nil {
                m.logger.Error("worker: pop task", "worker_id", id, "error", err)
            }
            continue
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

        // Post-pop: if Ollama and no backend available, re-queue and sleep to avoid spin.
        if av, ok := entry.Pipeline.(inference.AvailabilityChecker); ok && !av.IsAvailable() {
            if pushErr := m.producer.PushTask(ctx, task); pushErr != nil && m.logger != nil {
                m.logger.Warn("worker: re-queue Ollama task — no backend available", "error", pushErr)
            }
            time.Sleep(100 * time.Millisecond)
            continue
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

func (m *Manager) processTask(ctx context.Context, workerID int, task *queue.Task) {
    m.active.Add(1)
    m.broadcastWorkerStats()
    defer func() {
        m.active.Add(-1)
        m.broadcastWorkerStats()
    }()

    entry := m.cache.Get(task.ActionID)
    if entry == nil {
        if m.logger != nil {
            m.logger.Warn("worker: unknown action_id in task — action not in cache (inactive or missing?)",
                "action_id", task.ActionID,
                "trigger", task.TriggerType,
                "pipeline_id", task.PipelineID,
                "pipeline_node_id", task.PipelineNodeID,
            )
        }
        return
    }

    if task.TriggerType == "digest_requested" {
        m.processDigestTask(ctx, workerID, task, entry)
        return
    }

    // Unmarshal the tweet event before storing WorkerInfo so TweetID is available.
    var ev queue.TweetEvent
    _ = json.Unmarshal(task.Event, &ev)

    startedAt := time.Now()
    info := WorkerInfo{
        ID:        workerID,
        Status:    "active",
        ActionID:  task.ActionID,
        Action:    entry.Action.Name,
        Pipeline:  entry.Pipeline.Name(),
        TweetID:   ev.TweetID,
        Depth:     task.Depth,
        StartedAt: startedAt,
    }
    m.workerActions.Store(workerID, info)
    defer m.workerActions.Delete(workerID)

    // Decrement pipeline run tracking when this task is done.
    if task.RootPipelineID != 0 {
        defer func() {
            done, err := m.store.DecrementPipelineRunTracking(ctx, task.RootPipelineID, ev.TweetID)
            if err != nil && m.logger != nil {
                m.logger.Warn("worker: decrement pipeline run tracking",
                    "root_pipeline_id", task.RootPipelineID, "tweet_id", ev.TweetID, "error", err)
            }
            if done {
                m.firePipelineFinished(ctx, task.RootPipelineID, ev.TweetID)
            }
        }()
    }

    allMeta, _ := m.store.MergeAllTweetExecutionMetadata(ctx, ev.TweetID)
    prevMeta, _ := m.store.MergeActionExecutionMetadata(ctx, task.ActionID, ev.TweetID)

    mergedCtx := make(map[string]any, len(task.Context)+len(allMeta)+len(prevMeta)+6)
    for k, v := range allMeta {
        mergedCtx[k] = decodeMetaValue(v)
    }
    delete(mergedCtx, "telegram_message_id")
    for k, v := range prevMeta {
        mergedCtx[k] = decodeMetaValue(v)
    }
    if _, hasOwn := mergedCtx["telegram_message_id"]; !hasOwn {
        if lastID, ok := allMeta["telegram_message_id"]; ok {
            mergedCtx["last_telegram_message_id"] = decodeMetaValue(lastID)
        }
    }
    for k, v := range task.Context {
        mergedCtx[k] = v
    }
    mergedCtx["tweet_id"] = ev.TweetID
    mergedCtx["tweet_author"] = ev.AuthorHandle
    mergedCtx["author"] = ev.AuthorHandle
    mergedCtx["tweet"] = ev.Text
    mergedCtx["tweet_short"] = truncateText(ev.Text, 280)
    mergedCtx["tweet_url"] = "https://x.com/" + ev.AuthorHandle + "/status/" + ev.TweetID
    mergedCtx["rule_ids"] = ev.RuleIDs

    enrichContext(mergedCtx)

    input := inference.PipelineInput{
        Text:    ev.Text,
        TweetID: ev.TweetID,
        Context: mergedCtx,
    }

    // Inject backend callback so Ollama pipelines can report which backend they acquire.
    inferenceCtx := inference.WithBackendNotify(ctx, func(backend string) {
        m.workerActions.Store(workerID, WorkerInfo{
            ID:          workerID,
            Status:      "active",
            ActionID:    task.ActionID,
            Action:      entry.Action.Name,
            Pipeline:    entry.Pipeline.Name(),
            TweetID:     ev.TweetID,
            Depth:       task.Depth,
            StartedAt:   startedAt,
            BackendName: backend,
        })
    })

    if m.logger != nil {
        m.logger.Info("worker: running pipeline",
            "worker_id", workerID,
            "action_id", task.ActionID,
            "action", entry.Action.Name,
            "pipeline", entry.Pipeline.Name(),
            "tweet_id", ev.TweetID,
            "pipeline_id", task.PipelineID,
            "pipeline_node_id", task.PipelineNodeID,
        )
    }

    startTime := time.Now()
    outputs, runErr := entry.Pipeline.Run(inferenceCtx, input)

    exec := &model.ActionExecution{
        ActionID:   task.ActionID,
        TweetID:    ev.TweetID,
        Status:     model.ExecutionStatusSuccess,
        ExecutedAt: startTime,
    }
    if runErr != nil {
        exec.Status = model.ExecutionStatusError
        exec.Error = runErr.Error()
        if m.logger != nil {
            m.logger.Warn("worker: pipeline run failed",
                "worker_id", workerID, "action_id", task.ActionID,
                "pipeline", entry.Pipeline.Name(), "error", runErr)
        }
    }
    if saveErr := m.store.SaveActionExecution(ctx, exec); saveErr != nil {
        if m.logger != nil {
            m.logger.Warn("worker: save action execution", "error", saveErr)
        }
    } else if m.broadcaster != nil {
        m.broadcaster.Broadcast(hub.Event{
            Type:    "action_execution",
            Payload: exec,
        })
    }

    if runErr != nil {
        return
    }
    if outputs == nil {
        if m.logger != nil {
            m.logger.Warn("worker: pipeline returned nil outputs — pipeline continuation skipped",
                "worker_id", workerID,
                "action_id", task.ActionID,
                "action", entry.Action.Name,
                "pipeline", entry.Pipeline.Name(),
                "tweet_id", ev.TweetID,
                "pipeline_node_id", task.PipelineNodeID,
            )
        }
        return
    }

    for _, t := range entry.Transformers {
        transformed, tErr := t.Apply(outputs)
        if tErr != nil {
            if m.logger != nil {
                m.logger.Warn("worker: transformer failed, using untransformed output",
                    "worker_id", workerID, "action_id", task.ActionID, "error", tErr)
            }
            break
        }
        outputs = transformed
    }

    for k, v := range outputs {
        val := fmt.Sprintf("%v", v)
        if b, err := json.Marshal(v); err == nil {
            val = string(b)
        }
        _ = m.store.SaveActionExecutionMetadata(ctx, exec.ID, k, val)
    }

    outputKeys := make([]string, 0, len(outputs))
    for k, v := range outputs {
        mergedCtx[k] = v
        outputKeys = append(outputKeys, fmt.Sprintf("%s=%v", k, v))
    }

    if m.logger != nil {
        m.logger.Info("worker: pipeline completed",
            "worker_id", workerID,
            "action_id", task.ActionID,
            "action", entry.Action.Name,
            "tweet_id", ev.TweetID,
            "output_keys", outputKeys,
            "pipeline_node_id", task.PipelineNodeID,
        )
    }

    PushContinuationTasks(ctx, m.store, m.producer, m.cache, task, mergedCtx, m.logger)
}

// firePipelineFinished dispatches tasks for all pipelines linked to the
// pipeline_finished trigger for the given source pipeline.
func (m *Manager) firePipelineFinished(ctx context.Context, pipelineID int64, tweetID string) {
    eventPayload, err := m.store.GetPipelineRunEventPayload(ctx, pipelineID, tweetID)
    if err != nil || len(eventPayload) == 0 {
        if m.logger != nil {
            m.logger.Warn("worker: pipeline_finished: missing event payload",
                "pipeline_id", pipelineID, "tweet_id", tweetID)
        }
        return
    }
    var ev queue.TweetEvent
    if err := json.Unmarshal(eventPayload, &ev); err != nil {
        if m.logger != nil {
            m.logger.Warn("worker: pipeline_finished: unmarshal event", "error", err)
        }
        return
    }
    if m.logger != nil {
        m.logger.Info("worker: pipeline finished — firing pipeline_finished trigger",
            "pipeline_id", pipelineID, "tweet_id", tweetID)
    }
    if err := PushInitialPipelineTasks(ctx, m.store, m.producer, m.cache,
        "pipeline_finished", ev, m.logger, pipelineID); err != nil && m.logger != nil {
        m.logger.Warn("worker: push pipeline_finished tasks failed",
            "pipeline_id", pipelineID, "error", err)
    }
}

func (m *Manager) processDigestTask(ctx context.Context, workerID int, task *queue.Task, entry *CacheEntry) {
    startedAt := time.Now()
    info := WorkerInfo{
        ID:        workerID,
        Status:    "active",
        ActionID:  task.ActionID,
        Action:    entry.Action.Name,
        Pipeline:  entry.Pipeline.Name(),
        Depth:     task.Depth,
        StartedAt: startedAt,
    }
    m.workerActions.Store(workerID, info)
    defer m.workerActions.Delete(workerID)

    dp, ok := entry.Pipeline.(inference.DigestPipeline)
    if !ok {
        if m.logger != nil {
            m.logger.Warn("worker: digest action pipeline does not implement DigestPipeline",
                "worker_id", workerID, "action_id", task.ActionID, "pipeline", entry.Pipeline.Name())
        }
        return
    }

    jobID, _ := task.Context["digest_job_id"].(string)
    if jobID == "" {
        var ev queue.DigestEvent
        _ = json.Unmarshal(task.Event, &ev)
        jobID = ev.DigestJobID
    }
    if jobID == "" {
        if m.logger != nil {
            m.logger.Warn("worker: digest task missing digest_job_id", "worker_id", workerID, "action_id", task.ActionID)
        }
        return
    }

    job, err := m.store.GetDigestJob(ctx, jobID)
    if err != nil {
        if m.logger != nil {
            m.logger.Warn("worker: digest job not found", "job_id", jobID, "error", err)
        }
        return
    }

    job.Status = model.DigestStatusRunning
    if err := m.store.UpdateDigestJob(ctx, job); err != nil && m.logger != nil {
        m.logger.Warn("worker: digest job mark running", "job_id", jobID, "error", err)
    }

    inferenceCtx := inference.WithBackendNotify(ctx, func(backend string) {
        m.workerActions.Store(workerID, WorkerInfo{
            ID:          workerID,
            Status:      "active",
            ActionID:    task.ActionID,
            Action:      entry.Action.Name,
            Pipeline:    entry.Pipeline.Name(),
            Depth:       task.Depth,
            StartedAt:   startedAt,
            BackendName: backend,
        })
    })

    p := job.Params
    tweets, err := m.store.ListTweets(ctx, store.TweetFilter{
        RuleID:   p.RuleID,
        SourceID: p.SourceID,
        EntityID: p.EntityID,
        From:     p.From,
        To:       p.To,
        Page:     1,
        PerPage:  p.Limit,
    })

    now := time.Now().UTC()
    job.CompletedAt = &now

    if err != nil {
        if m.logger != nil {
            m.logger.Error("worker: digest job: list tweets", "job_id", jobID, "error", err)
        }
        job.Status = model.DigestStatusError
        job.Error = "failed to fetch tweets: " + err.Error()
        _ = m.store.UpdateDigestJob(ctx, job)
        m.emitDigestEvent(job, entry.Pipeline.Name())
        return
    }

    job.TweetCount = len(tweets)

    if len(tweets) == 0 {
        job.Status = model.DigestStatusDone
        job.Result = "No tweets found matching the selected filters."
        _ = m.store.UpdateDigestJob(ctx, job)
        m.emitDigestEvent(job, entry.Pipeline.Name())
        return
    }

    formatted := make([]string, len(tweets))
    for i, t := range tweets {
        if t.AuthorHandle != "" {
            formatted[i] = fmt.Sprintf("@%s: %s", t.AuthorHandle, t.Text)
        } else {
            formatted[i] = t.Text
        }
    }

    digest, err := dp.Digest(inferenceCtx, formatted)
    if err != nil {
        if m.logger != nil {
            m.logger.Error("worker: digest job: run", "job_id", jobID, "action_id", task.ActionID, "error", err)
        }
        job.Status = model.DigestStatusError
        job.Error = "digest generation failed: " + err.Error()
        _ = m.store.UpdateDigestJob(ctx, job)
        m.emitDigestEvent(job, entry.Pipeline.Name())
        return
    }

    job.Status = model.DigestStatusDone
    job.Result = digest
    if err := m.store.UpdateDigestJob(ctx, job); err != nil && m.logger != nil {
        m.logger.Error("worker: digest job: save result", "job_id", jobID, "error", err)
    }
    m.emitDigestEvent(job, entry.Pipeline.Name())
}

func (m *Manager) emitDigestEvent(job *model.DigestJob, pipelineName string) {
    if m.dispatcher == nil {
        return
    }
    eventType := notifier.EventDigestDone
    if job.Status == model.DigestStatusError {
        eventType = notifier.EventDigestFailed
    }
    m.dispatcher.Emit(notifier.NotificationEvent{
        Type:      eventType,
        DigestJob: job,
        ModelName: pipelineName,
        Error:     job.Error,
        Timestamp: time.Now().UTC(),
    })
}

// Flush cancels the context of every worker that is currently executing a task.
// Workers blocked in BZPopMin (idle) are unaffected. Returns the count of interrupted tasks.
func (m *Manager) Flush() int {
    n := 0
    m.workerCancels.Range(func(_, v any) bool {
        if cancel, ok := v.(context.CancelFunc); ok {
            cancel()
        }
        n++
        return true
    })
    return n
}

func (m *Manager) FlushPipeline(_ string) int { return 0 }

func truncateText(s string, max int) string {
    runes := []rune(s)
    if len(runes) <= max {
        return s
    }
    return string(runes[:max]) + "…"
}

func decodeMetaValue(s string) any {
    var v any
    if err := json.Unmarshal([]byte(s), &v); err != nil {
        return s
    }
    return v
}

func enrichContext(ctx map[string]any) {
    if _, ok := ctx["top_label"]; !ok {
        if labels, ok := ctx["labels"].([]any); ok && len(labels) > 0 {
            ctx["top_label"] = labels[0]
            if scores, ok := ctx["scores"].([]any); ok && len(scores) > 0 {
                ctx["top_score"] = scores[0]
            }
        }
    }

    if _, ok := ctx["entity_list"]; !ok {
        if entities, ok := ctx["entities"].([]any); ok && len(entities) > 0 {
            var parts []string
            for _, e := range entities {
                em, ok := e.(map[string]any)
                if !ok {
                    continue
                }
                word, _ := em["word"].(string)
                label, _ := em["label"].(string)
                if word == "" {
                    continue
                }
                if label != "" {
                    parts = append(parts, word+" ("+label+")")
                } else {
                    parts = append(parts, word)
                }
            }
            if len(parts) > 0 {
                ctx["entity_list"] = strings.Join(parts, ", ")
            }
        }
    }
}
```

- [ ] **Step 3: Delete `internal/worker/rate.go` and `internal/worker/rate_test.go`**

```bash
rm internal/worker/rate.go internal/worker/rate_test.go
```

- [ ] **Step 4: Run worker tests**

```bash
docker compose run --rm --no-deps iran-watcher go test ./internal/worker/... -v 2>&1 | tail -50
```

Expected: all pass. The `rateTracker` tests are gone (files deleted); all other worker tests pass.

- [ ] **Step 5: Run the full test suite to check for compile errors across packages**

```bash
docker compose run --rm --no-deps iran-watcher go build ./... 2>&1
```

Expected: compile errors only in `internal/api/` (handlers_queue.go references old `ActiveWorkers`/`ActionThroughput`). Fix those in Task 3.

- [ ] **Step 6: Commit**

```bash
git add internal/worker/pool.go internal/worker/manager.go
git rm internal/worker/rate.go internal/worker/rate_test.go
git commit -m "feat(worker): remove buildReadyKeys, expand WorkerInfo, all-workers Stats()"
```

---

### Task 3: API layer — new queue response shape

**Files:**
- Modify: `internal/api/handlers_queue.go`
- Modify: `internal/api/handlers_health.go` (minor: already works via `TotalTaskLen`)

- [ ] **Step 1: Write failing test**

Add to `internal/api/` (create `handlers_queue_test.go` if it doesn't exist — check first):

```bash
ls internal/api/*queue*test* 2>/dev/null || echo "no queue test file"
```

If no test file exists, create `internal/api/handlers_queue_test.go`:

```go
package api

import (
    "encoding/json"
    "net/http"
    "net/http/httptest"
    "testing"

    "github.com/amir-the-h/iran-watcher/internal/worker"
)

type stubPoolStats struct {
    stats worker.PoolStats
}

func (s *stubPoolStats) Stats() worker.PoolStats { return s.stats }

type stubSizer struct{ pending int64 }

func (s *stubSizer) Len(_ interface{}, _ string) (int64, error)  { return s.pending, nil }
func (s *stubSizer) TotalTaskLen(_ interface{}) (int64, error) { return s.pending, nil }

func TestHandleGetQueue_Shape(t *testing.T) {
    srv := &Server{
        pool: &stubPoolStats{
            stats: worker.PoolStats{
                Total:  3,
                Active: 1,
                Idle:   2,
                Workers: []worker.WorkerInfo{
                    {ID: 1, Status: "active", Action: "NER", Pipeline: "onnx", TweetID: "123", Depth: 0},
                    {ID: 2, Status: "idle"},
                    {ID: 3, Status: "idle"},
                },
            },
        },
        sizer: &stubSizer{pending: 42},
    }

    req := httptest.NewRequest(http.MethodGet, "/api/queue", nil)
    rr := httptest.NewRecorder()
    srv.handleGetQueue(rr, req)

    if rr.Code != http.StatusOK {
        t.Fatalf("status = %d, want 200", rr.Code)
    }

    var resp map[string]any
    if err := json.NewDecoder(rr.Body).Decode(&resp); err != nil {
        t.Fatalf("decode: %v", err)
    }

    if _, ok := resp["pending"]; !ok {
        t.Error("response missing 'pending' field")
    }
    if _, ok := resp["workers"]; !ok {
        t.Error("response missing 'workers' field")
    }
    if _, ok := resp["queues"]; ok {
        t.Error("response must NOT contain 'queues' field")
    }
}
```

- [ ] **Step 2: Run test to verify it fails**

```bash
docker compose run --rm --no-deps iran-watcher go test ./internal/api/... -run TestHandleGetQueue_Shape -v 2>&1 | tail -20
```

Expected: FAIL — response has `queues` not `pending`/`workers`.

- [ ] **Step 3: Replace `internal/api/handlers_queue.go`**

```go
package api

import (
    "net/http"

    "github.com/amir-the-h/iran-watcher/internal/queue"
)

func (s *Server) handleGetQueue(w http.ResponseWriter, r *http.Request) {
    var pending int64
    if s.sizer != nil {
        pending, _ = s.sizer.Len(r.Context(), queue.QueueTasksKey)
    }

    var workers any = []any{}
    if s.pool != nil {
        workers = s.pool.Stats().Workers
    }

    writeJSON(w, http.StatusOK, map[string]any{
        "pending": pending,
        "workers": workers,
    })
}
```

- [ ] **Step 4: Run the test**

```bash
docker compose run --rm --no-deps iran-watcher go test ./internal/api/... -run TestHandleGetQueue_Shape -v 2>&1 | tail -20
```

Expected: PASS.

- [ ] **Step 5: Check `handlers_health.go` — no changes needed**

`handleGetHealth` uses `s.sizer.TotalTaskLen(r.Context())` which now just calls `Len(ctx, QueueTasksKey)` — behaviour unchanged. `s.pool.Stats()` returns the new `PoolStats` shape. `resp["workers"] = stats` serializes the full `PoolStats` struct including the new `Workers []WorkerInfo` field. No code change needed.

- [ ] **Step 6: Build to confirm clean compile**

```bash
docker compose run --rm --no-deps iran-watcher go build ./... 2>&1
```

Expected: no errors.

- [ ] **Step 7: Commit**

```bash
git add internal/api/handlers_queue.go internal/api/handlers_queue_test.go
git commit -m "feat(api): GET /api/queue returns {pending, workers} instead of per-action queues"
```

---

### Task 4: Frontend — worker table

**Files:**
- Modify: `web/src/api/queue.ts`
- Modify: `web/src/stores/queues.ts`
- Modify: `web/src/composables/useWebSocket.ts`
- Modify: `web/src/views/QueueView.vue`

- [ ] **Step 1: Update `web/src/api/queue.ts`**

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
  pending: number
  workers: WorkerInfo[]
}

export const getQueue = () => apiFetch<QueueResponse>('/api/queue')

export const flushQueue = () =>
  apiFetch<{ flushed: boolean }>('/api/queue', { method: 'DELETE' })

export const flushWorkerBuffer = () =>
  apiFetch<{ flushed: number }>('/api/workers/buffer', { method: 'DELETE' })
```

- [ ] **Step 2: Replace `web/src/stores/queues.ts`**

```typescript
// web/src/stores/queues.ts
import { defineStore } from 'pinia'
import { ref } from 'vue'
import type { WorkerInfo } from '../api/queue'

export type { WorkerInfo }

export const useQueuesStore = defineStore('queues', () => {
  const pending = ref(0)
  const workers = ref<WorkerInfo[]>([])

  // Called on initial HTTP fetch (onMounted in QueueView).
  function setFromResponse(p: number, w: WorkerInfo[]) {
    pending.value = p
    workers.value = w
  }

  // Called on queue_update WebSocket event.
  function setQueuePending(p: number) {
    pending.value = p
  }

  // Called on worker_stats WebSocket event.
  function setWorkers(w: WorkerInfo[]) {
    workers.value = w
  }

  return { pending, workers, setFromResponse, setQueuePending, setWorkers }
})
```

- [ ] **Step 3: Update `web/src/composables/useWebSocket.ts`**

Find and replace the two handler lines in `buildHandlers()`:

```typescript
// Before:
worker_stats:           (p: any) => queuesStore.setWorkerStats(p),
queue_update:           (p: any) => queuesStore.setQueueEntry(p),

// After:
worker_stats:           (p: any) => queuesStore.setWorkers(p.workers ?? []),
queue_update:           (p: any) => queuesStore.setQueuePending(p.pending ?? 0),
```

Also update the import in `useWebSocket.ts` if the store's exported types changed — `useQueuesStore` import is unchanged; just the method calls above change.

- [ ] **Step 4: Replace `web/src/views/QueueView.vue`**

Replace the entire file with the worker-centric table. Key changes: remove ETA/throughput logic, replace `queueColumns` with `workerColumns`, update store calls, update stats card for `pending`:

```vue
<!-- web/src/views/QueueView.vue -->
<script setup lang="ts">
import { h, ref, computed, onMounted } from 'vue'
import { useFailuresStore } from '../stores/failures'
import { useQueuesStore } from '../stores/queues'
import { useMessage, NButton, NSpace, NTag, NIcon, NTooltip } from 'naive-ui'
import type { DataTableColumns } from 'naive-ui'
import type { FailedTweet } from '../api/failures'
import type { WorkerInfo } from '../api/queue'
import { getQueue, flushQueue, flushWorkerBuffer } from '../api/queue'
import { apiFetch } from '../api/client'
import { formatDateTime } from '../utils/format'
import { InformationCircleOutline, RefreshOutline, TrashOutline, BanOutline, StopCircleOutline } from '@vicons/ionicons5'
import { iconBtn } from '../utils/renderActions'

function formatDuration(ms: number): string {
  if (ms < 1000) return '0.0s'
  const secs = ms / 1000
  if (secs < 60) return `${secs.toFixed(1)}s`
  const m = Math.floor(secs / 60)
  const s = Math.round(secs % 60)
  return s > 0 ? `${m}m ${s}s` : `${m}m`
}

const store       = useFailuresStore()
const queuesStore = useQueuesStore()
const msg         = useMessage()

// ── Data fetching ──────────────────────────────────────────────────────────────

async function fetchQueue() {
  try {
    const data = await getQueue()
    queuesStore.setFromResponse(data.pending, data.workers)
  } catch { /* ignore */ }
}

// ── Computed stats ─────────────────────────────────────────────────────────────

const totalWorkers  = computed(() => queuesStore.workers.length)
const activeWorkers = computed(() => queuesStore.workers.filter(w => w.status === 'active').length)
const idleWorkers   = computed(() => queuesStore.workers.filter(w => w.status === 'idle').length)

// ── Actions ───────────────────────────────────────────────────────────────────

const showDetail  = ref(false)
const selected    = ref<FailedTweet | null>(null)
const clearing    = ref(false)
const flushing    = ref(false)
const stopping    = ref(false)
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
  } catch (e: any) { msg.error(e.message) }
  finally { clearing.value = false }
}

async function onFlush() {
  flushing.value = true
  try {
    await flushQueue()
    msg.success('Pending queue flushed')
    await Promise.all([store.fetch(), fetchQueue()])
  } catch (e: any) { msg.error(e.message) }
  finally { flushing.value = false }
}

async function onStopWorkers() {
  stopping.value = true
  try {
    await flushWorkerBuffer()
    await flushQueue()
    msg.success('Workers stopped and queue flushed')
    await Promise.all([store.fetch(), fetchQueue()])
  } catch (e: any) { msg.error(e.message) }
  finally { stopping.value = false }
}

// ── Worker table ──────────────────────────────────────────────────────────────

const pipelineTagType: Record<string, 'info' | 'success' | 'default' | 'warning'> = {
  onnx: 'info',
  ollama: 'success',
  internal: 'default',
  notification: 'warning',
}

const workerColumns: DataTableColumns<WorkerInfo> = [
  {
    title: '#',
    key: 'id',
    width: 50,
    render: (row) => h('span', { style: { color: '#585b70', fontVariantNumeric: 'tabular-nums' } }, String(row.id)),
  },
  {
    title: 'Status',
    key: 'status',
    width: 90,
    render: (row) => {
      const isActive = row.status === 'active'
      return h('span', {
        style: { color: isActive ? '#a6e3a1' : '#585b70' },
      }, isActive ? '● active' : '○ idle')
    },
  },
  {
    title: 'Action',
    key: 'action',
    render: (row) =>
      row.status === 'active'
        ? h('span', { style: { color: '#cdd6f4' } }, row.action)
        : h('span', { style: { color: '#313244' } }, '—'),
  },
  {
    title: 'Type',
    key: 'pipeline',
    width: 130,
    render: (row) => {
      if (row.status !== 'active' || !row.pipeline) {
        return h('span', { style: { color: '#313244' } }, '—')
      }
      return h(NTag, {
        type: pipelineTagType[row.pipeline] ?? 'default',
        size: 'small',
        round: true,
      }, { default: () => row.pipeline })
    },
  },
  {
    title: 'Tweet',
    key: 'tweet_id',
    width: 130,
    render: (row) =>
      row.tweet_id
        ? h('span', { style: { color: '#a6adc8', fontVariantNumeric: 'tabular-nums', fontFamily: 'monospace' } },
            row.tweet_id.slice(0, 12) + (row.tweet_id.length > 12 ? '…' : ''))
        : h('span', { style: { color: '#313244' } }, '—'),
  },
  {
    title: 'Depth',
    key: 'depth',
    width: 70,
    render: (row) =>
      row.status === 'active'
        ? h('span', { style: { color: '#cba6f7', fontVariantNumeric: 'tabular-nums' } }, String(row.depth))
        : h('span', { style: { color: '#313244' } }, '—'),
  },
  {
    title: 'Duration',
    key: 'duration_ms',
    width: 100,
    render: (row) =>
      row.status === 'active'
        ? h('span', { style: { color: '#f9e2af', fontVariantNumeric: 'tabular-nums' } }, formatDuration(row.duration_ms))
        : h('span', { style: { color: '#313244' } }, '—'),
  },
  {
    title: 'Backend',
    key: 'backend_name',
    width: 130,
    render: (row) =>
      row.backend_name
        ? h('span', { style: { color: '#89dceb' } }, row.backend_name)
        : h('span', { style: { color: '#313244' } }, '—'),
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

onMounted(() => {
  store.fetch()
  fetchQueue()
})
</script>

<template>
  <n-space vertical size="large">

    <!-- Header -->
    <n-space align="center" justify="space-between" style="flex-wrap: wrap; gap: 8px;">
      <n-h2 style="margin: 0;">Queue</n-h2>
      <n-space size="small">
        <n-tooltip trigger="hover">
          <template #trigger>
            <n-button @click="() => { store.fetch(); fetchQueue() }" :loading="store.loading">
              <template #icon><n-icon><RefreshOutline /></n-icon></template>
            </n-button>
          </template>
          Refresh
        </n-tooltip>
        <n-popconfirm @positive-click="onStopWorkers">
          <template #trigger>
            <n-tooltip trigger="hover">
              <template #trigger>
                <n-button type="error" :loading="stopping">
                  <template #icon><n-icon><StopCircleOutline /></n-icon></template>
                </n-button>
              </template>
              Stop Workers &amp; Flush Queue
            </n-tooltip>
          </template>
          Cancel all active worker tasks and drop all pending queue items?
        </n-popconfirm>
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
          Drop all pending tasks from the queue?
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
          <n-statistic label="Pending">
            <template #default>
              <span :style="{ color: queuesStore.pending > 0 ? '#f9e2af' : '#cdd6f4' }">
                {{ queuesStore.pending }}
              </span>
            </template>
          </n-statistic>
        </n-card>
      </n-gi>
      <n-gi span="3 s:1">
        <n-card size="small" embedded>
          <n-statistic label="Dead-Letter">
            <template #default>
              <span :style="{ color: store.total > 0 ? '#f38ba8' : '#cdd6f4' }">
                {{ store.total > 0 ? store.total : '—' }}
              </span>
            </template>
          </n-statistic>
        </n-card>
      </n-gi>
      <n-gi span="3 s:1">
        <n-card size="small" embedded>
          <n-statistic label="Workers">
            <template #default>
              <span :style="{ color: activeWorkers > 0 ? '#a6e3a1' : '#cdd6f4' }">
                {{ totalWorkers > 0 ? activeWorkers : '—' }}
              </span>
              <span v-if="totalWorkers > 0" style="color: #585b70; font-size: 14px;"> / {{ totalWorkers }}</span>
            </template>
            <template #suffix>
              <span v-if="totalWorkers > 0" style="color: #585b70; font-size: 12px; margin-left: 8px;">
                {{ idleWorkers }} idle
              </span>
            </template>
          </n-statistic>
          <div v-if="totalWorkers > 0" style="margin-top: 8px; height: 4px; border-radius: 2px; background: rgba(255,255,255,0.08); overflow: hidden;">
            <div :style="{
              height: '100%',
              width: totalWorkers > 0 ? (activeWorkers / totalWorkers * 100) + '%' : '0%',
              background: activeWorkers > 0 ? '#a6e3a1' : 'rgba(255,255,255,0.15)',
              transition: 'width 0.5s ease',
            }" />
          </div>
        </n-card>
      </n-gi>
    </n-grid>

    <!-- Workers table -->
    <n-card size="small" title="Workers">
      <n-empty
        v-if="queuesStore.workers.length === 0"
        description="No worker data"
        style="padding: 16px 0;"
      />
      <n-data-table
        v-else
        :columns="workerColumns"
        :data="queuesStore.workers"
        :pagination="false"
        size="small"
        :row-props="(row: WorkerInfo) => ({ style: row.status === 'idle' ? 'opacity: 0.4;' : '' })"
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

- [ ] **Step 5: Check for other files referencing old store methods**

```bash
grep -r "setQueues\|setQueueEntry\|setWorkerStats\|activeQueues\|inactiveQueues\|workerStats\|totalPending\|ActionThroughput\|active_workers\|action_throughput" web/src/ --include="*.ts" --include="*.vue" -l
```

Fix any remaining references found (the only expected hit is `useWebSocket.ts` which was already updated in Step 3).

- [ ] **Step 6: Commit**

```bash
git add web/src/api/queue.ts web/src/stores/queues.ts web/src/composables/useWebSocket.ts web/src/views/QueueView.vue
git commit -m "feat(ui): replace action queue table with worker table on Queue page"
```

---

### Task 5: Build verification + CLAUDE.md update

**Files:**
- Modify: `CLAUDE.md`

- [ ] **Step 1: Docker build**

```bash
docker compose build --build-arg BUILDKIT_INLINE_CACHE=1 iran-watcher 2>&1 | tail -20
```

Expected: build succeeds with no errors. Fix any remaining compile errors before proceeding.

- [ ] **Step 2: Update CLAUDE.md**

In the **Key Design Decisions** section, find the bullet that describes the queue key (`iran-watcher:tasks:{action_id}`) and update it:

Find text:
```
tasks are queued per-action at `iran-watcher:tasks:{action_id}` (replaces the single `iran-watcher:tasks` key); `FlushQueue` scans `iran-watcher:tasks:*` to drain all per-action keys atomically
```

Replace with:
```
all tasks go into a single global sorted set `iran-watcher:tasks`, scored by task `Depth` (lower = higher priority); `FlushQueue` issues a single `DEL iran-watcher:tasks`; workers pop via `BZPopMin` on the single key and do a post-pop availability check — Ollama tasks with no free backend are re-queued (idempotent sorted-set add) and the worker sleeps 100 ms to avoid spin
```

Find the `worker_stats` event payload description in the WebSocket section:
```
`worker_stats` (payload includes `active_workers: WorkerInfo[]`, `action_throughput: map[action_id]float64` items/sec)
```

Replace with:
```
`worker_stats` (payload: `{total, active, idle, workers: WorkerInfo[]}` — all N worker slots; active workers first; `WorkerInfo` includes `id`, `status` ("active"|"idle"), `action_id`, `action`, `pipeline`, `tweet_id`, `depth`, `started_at`, `duration_ms` (computed server-side), `backend_name` (display name for Ollama, empty otherwise))
```

Find the `queue_update` event description:
```
`queue_update`
```
and update its payload description from `{action_id, pending}` to `{pending}` (global count only, no action_id).

- [ ] **Step 3: Run the full test suite**

```bash
docker compose run --rm --no-deps iran-watcher go test ./... 2>&1 | tail -30
```

Expected: all tests pass.

- [ ] **Step 4: Commit**

```bash
git add CLAUDE.md
git commit -m "docs: update CLAUDE.md for single-priority-queue and worker_stats shape"
```

---

## Migration Note

On deploy: call `DELETE /api/queue` (flush) to drop any remaining per-action sorted-set keys from the old format (`iran-watcher:tasks:*`). The old per-action keys are not automatically cleaned up by the new `FlushQueue` (which only DELs `iran-watcher:tasks`). Run:

```bash
# From a container with Redis access:
redis-cli --scan --pattern 'iran-watcher:tasks:*' | xargs redis-cli del
```

Or call the flush endpoint after deploy: `DELETE /api/queue`.
