# Depth-Priority Queue Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Replace per-action Redis lists with sorted sets so tasks are dequeued by depth (position from Start node), giving early-pipeline actions priority over deep sub-pipeline work.

**Architecture:** Add `Depth int` to `queue.Task`; swap `LPush`/`BRPop` for `ZAdd`/`BZPopMin` in `RedisQueue`; propagate depth through every task-creation site in `pipeline.go` (`depth` param on `dispatchInitialNode`, `+1` on `pushContinuationTask` and sub-pipeline tasks).

**Tech Stack:** Go 1.25.5 · `github.com/redis/go-redis/v9` (uses `ZAdd`, `BZPopMin`, `ZCard`) · existing `queue.Task`, `RedisQueue`, `worker/pipeline.go`

---

## File Map

| File | What changes |
|------|--------------|
| `internal/queue/queue.go` | Add `Depth int` to `Task` |
| `internal/queue/redis.go` | Extend `redisClient`; `PushTask`→`ZAdd`+`ZCard`; `PopTask`→`BZPopMin`+`ZCard`; `Len`/`TotalTaskLen`→`ZCard`; remove `LLen` |
| `internal/queue/queue_test.go` | Replace fakes for sorted-set API; update all impacted tests |
| `internal/worker/pipeline.go` | Add `depth` param to `dispatchInitialNode`; propagate `Depth` in all 6 task-construction sites |
| `internal/worker/pipeline_test.go` | Add 3 depth-propagation tests |

---

## Task 1: Add `Depth` field to `queue.Task`

**Files:**
- Modify: `internal/queue/queue.go`
- Modify: `internal/queue/queue_test.go`

- [ ] **Step 1: Write the failing test**

In `internal/queue/queue_test.go`, update `TestTask_JSONRoundTrip` to set and assert `Depth`:

```go
func TestTask_JSONRoundTrip(t *testing.T) {
	now := time.Date(2026, 3, 23, 12, 0, 0, 0, time.UTC)
	ev, _ := json.Marshal(TweetEvent{
		TweetID:      "789",
		Text:         "Breaking: Iran nuclear talks",
		AuthorHandle: "newsbot",
		RuleIDs:      []int64{1, 2},
	})
	original := &Task{
		TriggerType: "new_tweet",
		Event:       json.RawMessage(ev),
		ActionID:    7,
		Depth:       3,
		Context: map[string]any{
			"created_at": now.Format(time.RFC3339),
		},
	}

	data, err := json.Marshal(original)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	var got Task
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	if got.ActionID != original.ActionID {
		t.Errorf("ActionID: got %d, want %d", got.ActionID, original.ActionID)
	}
	if got.TriggerType != original.TriggerType {
		t.Errorf("TriggerType: got %q, want %q", got.TriggerType, original.TriggerType)
	}
	if got.Depth != 3 {
		t.Errorf("Depth: got %d, want 3", got.Depth)
	}

	var gotEv TweetEvent
	if err := json.Unmarshal(got.Event, &gotEv); err != nil {
		t.Fatalf("unmarshal event: %v", err)
	}
	if gotEv.TweetID != "789" {
		t.Errorf("TweetID: got %q, want %q", gotEv.TweetID, "789")
	}
	if len(gotEv.RuleIDs) != 2 {
		t.Fatalf("RuleIDs: got %d, want 2", len(gotEv.RuleIDs))
	}
}
```

- [ ] **Step 2: Run the test — expect compile error**

```bash
go test ./internal/queue/... -run TestTask_JSONRoundTrip -v
```

Expected: compile error — `unknown field 'Depth' in struct literal`

- [ ] **Step 3: Add `Depth` to `Task`**

In `internal/queue/queue.go`, add `Depth` after `Context`:

```go
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
	// Depth is the number of action-hops from the pipeline's Start node.
	// Lower depth = higher priority. Zero value = highest priority (backwards-compatible).
	Depth                  int            `json:"depth,omitempty"`
	Context                map[string]any `json:"context"`
}
```

- [ ] **Step 4: Run the test — expect PASS**

```bash
go test ./internal/queue/... -run TestTask_JSONRoundTrip -v
```

Expected: `PASS`

- [ ] **Step 5: Commit**

```bash
git add internal/queue/queue.go internal/queue/queue_test.go
git commit -m "feat(queue): add Depth field to Task for priority ordering"
```

---

## Task 2: Update `redisClient` interface and all test fakes

**Files:**
- Modify: `internal/queue/redis.go`
- Modify: `internal/queue/queue_test.go`

This task prepares the interface and fakes so everything compiles before we implement new behaviour.

- [ ] **Step 1: Update `redisClient` interface in `redis.go`**

Replace the existing interface with (note: `LLen` is removed; `ZAdd`, `BZPopMin`, `ZCard` added):

```go
// redisClient is the narrow subset of redis.Client methods used by RedisQueue.
type redisClient interface {
	// List operations — used only for the dead-letter queue.
	LPush(ctx context.Context, key string, values ...interface{}) *redis.IntCmd
	BRPop(ctx context.Context, timeout time.Duration, keys ...string) *redis.StringSliceCmd
	LRange(ctx context.Context, key string, start, stop int64) *redis.StringSliceCmd
	Del(ctx context.Context, keys ...string) *redis.IntCmd

	// Sorted-set operations — used for per-action task queues.
	ZAdd(ctx context.Context, key string, members ...redis.Z) *redis.IntCmd
	BZPopMin(ctx context.Context, timeout float64, keys ...string) *redis.ZSliceWithKeyCmd
	ZCard(ctx context.Context, key string) *redis.IntCmd
}
```

- [ ] **Step 2: Replace `fakeRedisClient` in `queue_test.go`**

Replace the existing `fakeRedisClient` struct and its methods with:

```go
// fakeRedisClient implements redisClient for unit tests.
// BZPopMin returns redis.Nil (timeout) by default — override in derived fakes for success paths.
type fakeRedisClient struct {
	brpopErr    error
	brpopResult []string
}

func (f *fakeRedisClient) LPush(_ context.Context, _ string, _ ...interface{}) *redis.IntCmd {
	return redis.NewIntResult(0, nil)
}

func (f *fakeRedisClient) BRPop(_ context.Context, _ time.Duration, _ ...string) *redis.StringSliceCmd {
	return redis.NewStringSliceResult(f.brpopResult, f.brpopErr)
}

func (f *fakeRedisClient) LRange(_ context.Context, _ string, _, _ int64) *redis.StringSliceCmd {
	return redis.NewStringSliceResult(nil, nil)
}

func (f *fakeRedisClient) Del(_ context.Context, _ ...string) *redis.IntCmd {
	return redis.NewIntResult(0, nil)
}

func (f *fakeRedisClient) ZAdd(_ context.Context, _ string, _ ...redis.Z) *redis.IntCmd {
	return redis.NewIntResult(1, nil)
}

func (f *fakeRedisClient) BZPopMin(ctx context.Context, _ float64, _ ...string) *redis.ZSliceWithKeyCmd {
	cmd := redis.NewZSliceWithKeyCmd(ctx, "bzpopmin")
	cmd.SetErr(redis.Nil) // default: no item available (timeout)
	return cmd
}

func (f *fakeRedisClient) ZCard(_ context.Context, _ string) *redis.IntCmd {
	return redis.NewIntResult(0, nil)
}
```

- [ ] **Step 3: Replace `pushCaptureFakeClient` in `queue_test.go`**

Replace the existing struct (it overrode `LPush`) with one that overrides `ZAdd` and provides `ZCard`:

```go
type pushCaptureFakeClient struct {
	fakeRedisClient
	pushedKey    string
	pushedScore  float64
	pushedMember string
}

func (f *pushCaptureFakeClient) ZAdd(_ context.Context, key string, members ...redis.Z) *redis.IntCmd {
	f.pushedKey = key
	if len(members) > 0 {
		f.pushedScore = members[0].Score
		if s, ok := members[0].Member.(string); ok {
			f.pushedMember = s
		}
	}
	return redis.NewIntResult(1, nil)
}

func (f *pushCaptureFakeClient) ZCard(_ context.Context, _ string) *redis.IntCmd {
	return redis.NewIntResult(1, nil) // returns 1 for broadcast assertions
}
```

- [ ] **Step 4: Replace `brpopSuccessFakeClient` with `bzpopSuccessFakeClient` in `queue_test.go`**

Delete the old struct and add:

```go
// bzpopSuccessFakeClient simulates a successful BZPopMin result for PopTask tests.
type bzpopSuccessFakeClient struct {
	fakeRedisClient
	popKey    string
	popMember string
	zcardN    int64
}

func (f *bzpopSuccessFakeClient) BZPopMin(ctx context.Context, _ float64, _ ...string) *redis.ZSliceWithKeyCmd {
	cmd := redis.NewZSliceWithKeyCmd(ctx, "bzpopmin")
	cmd.SetVal(f.popKey, []redis.Z{{Score: 0, Member: f.popMember}})
	return cmd
}

func (f *bzpopSuccessFakeClient) ZCard(_ context.Context, _ string) *redis.IntCmd {
	return redis.NewIntResult(f.zcardN, nil)
}
```

- [ ] **Step 5: Replace `scanFakeClient` in `queue_test.go`**

Replace the existing struct (it had `lenByKey map[string]int64` and an `LLen` override) with:

```go
// scanFakeClient extends fakeRedisClient with Scan and ZCard for TotalTaskLen/FlushQueue tests.
type scanFakeClient struct {
	fakeRedisClient
	scanKeys   []string
	zcardByKey map[string]int64
	deleted    []string
}

func (f *scanFakeClient) Scan(_ context.Context, cursor uint64, _ string, _ int64) *redis.ScanCmd {
	if cursor == 0 {
		return redis.NewScanCmdResult(f.scanKeys, 0, nil)
	}
	return redis.NewScanCmdResult(nil, 0, nil)
}

func (f *scanFakeClient) ZCard(_ context.Context, key string) *redis.IntCmd {
	n := f.zcardByKey[key]
	return redis.NewIntResult(n, nil)
}

func (f *scanFakeClient) Del(_ context.Context, keys ...string) *redis.IntCmd {
	f.deleted = append(f.deleted, keys...)
	return redis.NewIntResult(int64(len(keys)), nil)
}
```

- [ ] **Step 6: Verify the package still compiles**

```bash
go build ./internal/queue/...
```

Expected: no errors (implementation still uses old methods; fakes now satisfy new interface)

- [ ] **Step 7: Commit**

```bash
git add internal/queue/redis.go internal/queue/queue_test.go
git commit -m "refactor(queue): update redisClient interface and test fakes for sorted-set ops"
```

---

## Task 3: Implement `PushTask` with `ZAdd`

**Files:**
- Modify: `internal/queue/redis.go`
- Modify: `internal/queue/queue_test.go`

- [ ] **Step 1: Write the failing tests**

Replace `TestPushTask_RoutesToActionKey` with two tests:

```go
func TestPushTask_RoutesToCorrectKey(t *testing.T) {
	fake := &pushCaptureFakeClient{}
	q := &RedisQueue{client: fake}

	task := &Task{TriggerType: "new_tweet", ActionID: 7, Depth: 2, Context: map[string]any{}}
	if err := q.PushTask(context.Background(), task); err != nil {
		t.Fatalf("PushTask: %v", err)
	}
	want := "iran-watcher:tasks:7"
	if fake.pushedKey != want {
		t.Errorf("pushed to key %q, want %q", fake.pushedKey, want)
	}
}

func TestPushTask_UsesDepthAsScore(t *testing.T) {
	fake := &pushCaptureFakeClient{}
	q := &RedisQueue{client: fake}

	task := &Task{TriggerType: "new_tweet", ActionID: 7, Depth: 3, Context: map[string]any{}}
	if err := q.PushTask(context.Background(), task); err != nil {
		t.Fatalf("PushTask: %v", err)
	}
	if fake.pushedScore != 3 {
		t.Errorf("ZAdd score = %v, want 3", fake.pushedScore)
	}
}

func TestPushTask_BroadcastsQueueUpdate(t *testing.T) {
	fake := &pushCaptureFakeClient{}
	bc := &captureBroadcaster{}
	q := &RedisQueue{client: fake, broadcaster: bc}

	task := &Task{TriggerType: "new_tweet", ActionID: 7, Depth: 0, Context: map[string]any{}}
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
	if payload["action_id"] != 7 {
		t.Errorf("action_id: got %d, want 7", payload["action_id"])
	}
	if payload["pending"] != 1 { // pushCaptureFakeClient.ZCard returns 1
		t.Errorf("pending: got %d, want 1", payload["pending"])
	}
}
```

- [ ] **Step 2: Run — expect FAIL**

```bash
go test ./internal/queue/... -run "TestPushTask" -v
```

Expected: tests fail or panic (PushTask still uses LPush)

- [ ] **Step 3: Implement `PushTask` with `ZAdd`**

In `internal/queue/redis.go`, replace `PushTask`:

```go
// PushTask serializes a Task to JSON and ZAdds it to the per-action sorted set.
// Score = task.Depth so lower-depth (higher-priority) tasks are popped first.
func (q *RedisQueue) PushTask(ctx context.Context, task *Task) error {
	b, err := json.Marshal(task)
	if err != nil {
		return fmt.Errorf("queue: marshal task: %w", err)
	}
	key := QueueTasksKey(task.ActionID)
	if err := q.client.ZAdd(ctx, key, redis.Z{
		Score:  float64(task.Depth),
		Member: string(b),
	}).Err(); err != nil {
		return fmt.Errorf("queue: push task: %w", err)
	}
	if q.broadcaster != nil {
		pending, _ := q.client.ZCard(ctx, key).Result()
		q.broadcaster.Broadcast(hub.Event{
			Type: "queue_update",
			Payload: map[string]int64{
				"action_id": task.ActionID,
				"pending":   pending,
			},
		})
	}
	return nil
}
```

- [ ] **Step 4: Run — expect PASS**

```bash
go test ./internal/queue/... -run "TestPushTask" -v
```

Expected: all `TestPushTask_*` pass

- [ ] **Step 5: Commit**

```bash
git add internal/queue/redis.go internal/queue/queue_test.go
git commit -m "feat(queue): PushTask uses ZAdd with Depth as score"
```

---

## Task 4: Implement `PopTask` with `BZPopMin`

**Files:**
- Modify: `internal/queue/redis.go`
- Modify: `internal/queue/queue_test.go`

- [ ] **Step 1: Update existing `PopTask` tests**

Replace all `TestPopTask_*` tests (keep `TestPopFailed_ReturnsNilOnTimeout` — it tests `PopFailed` which still uses `BRPop`):

```go
func TestPopTask_ReturnsNilOnTimeout(t *testing.T) {
	// fakeRedisClient.BZPopMin returns redis.Nil by default.
	q := &RedisQueue{client: &fakeRedisClient{}}
	task, err := q.PopTask(context.Background(), 1*time.Second, "iran-watcher:tasks:1")
	if err != nil {
		t.Fatalf("expected nil error on timeout, got: %v", err)
	}
	if task != nil {
		t.Fatalf("expected nil task on timeout, got: %+v", task)
	}
}

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

func TestPopTask_BroadcastsQueueUpdateOnSuccess(t *testing.T) {
	ev, _ := json.Marshal(TweetEvent{TweetID: "1", Text: "test", AuthorHandle: "user"})
	taskJSON, _ := json.Marshal(&Task{
		TriggerType: "new_tweet",
		ActionID:    7,
		Depth:       1,
		Event:       json.RawMessage(ev),
		Context:     map[string]any{},
	})
	fake := &bzpopSuccessFakeClient{
		popKey:    "iran-watcher:tasks:7",
		popMember: string(taskJSON),
		zcardN:    5,
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
	if task.Depth != 1 {
		t.Errorf("Depth: got %d, want 1", task.Depth)
	}
	if len(bc.events) != 1 {
		t.Fatalf("expected 1 broadcast event, got %d", len(bc.events))
	}
	if bc.events[0].Type != "queue_update" {
		t.Errorf("event type: got %q, want queue_update", bc.events[0].Type)
	}
	payload, ok := bc.events[0].Payload.(map[string]int64)
	if !ok {
		t.Fatalf("unexpected payload type %T", bc.events[0].Payload)
	}
	if payload["action_id"] != 7 {
		t.Errorf("action_id: got %d, want 7", payload["action_id"])
	}
	if payload["pending"] != 5 {
		t.Errorf("pending: got %d, want 5", payload["pending"])
	}
}

func TestPopTask_NoBroadcastWhenNoBroadcaster(t *testing.T) {
	ev, _ := json.Marshal(TweetEvent{TweetID: "2", Text: "x", AuthorHandle: "u"})
	taskJSON, _ := json.Marshal(&Task{TriggerType: "new_tweet", ActionID: 3, Event: json.RawMessage(ev), Context: map[string]any{}})
	fake := &bzpopSuccessFakeClient{popKey: "iran-watcher:tasks:3", popMember: string(taskJSON)}
	q := &RedisQueue{client: fake} // no broadcaster

	task, err := q.PopTask(context.Background(), 1*time.Second, "iran-watcher:tasks:3")
	if err != nil {
		t.Fatalf("PopTask: %v", err)
	}
	if task == nil {
		t.Fatal("expected non-nil task")
	}
	// No panic = pass
}
```

- [ ] **Step 2: Run — expect FAIL**

```bash
go test ./internal/queue/... -run "TestPopTask" -v
```

Expected: failures (PopTask still uses BRPop)

- [ ] **Step 3: Implement `PopTask` with `BZPopMin`**

In `internal/queue/redis.go`, replace `PopTask`:

```go
// PopTask blocks until a Task is available on one of the given sorted-set keys or
// the timeout elapses. Returns the task with the lowest Depth score first.
// Returns (nil, nil) on timeout or when no keys are provided.
func (q *RedisQueue) PopTask(ctx context.Context, timeout time.Duration, keys ...string) (*Task, error) {
	if len(keys) == 0 {
		return nil, nil
	}
	popKey, zs, err := q.client.BZPopMin(ctx, timeout.Seconds(), keys...).Result()
	if err != nil {
		if errors.Is(err, redis.Nil) {
			return nil, nil
		}
		return nil, fmt.Errorf("queue: pop task: %w", err)
	}
	if len(zs) == 0 || popKey == "" {
		return nil, nil
	}
	member, ok := zs[0].Member.(string)
	if !ok {
		return nil, fmt.Errorf("queue: pop task: member is not a string")
	}
	var t Task
	if err := json.Unmarshal([]byte(member), &t); err != nil {
		return nil, fmt.Errorf("queue: pop task: unmarshal: %w", err)
	}
	if q.broadcaster != nil {
		pending, _ := q.client.ZCard(ctx, popKey).Result()
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
```

- [ ] **Step 4: Run — expect PASS**

```bash
go test ./internal/queue/... -run "TestPopTask|TestPopFailed" -v
```

Expected: all pass

- [ ] **Step 5: Commit**

```bash
git add internal/queue/redis.go internal/queue/queue_test.go
git commit -m "feat(queue): PopTask uses BZPopMin to dequeue by depth priority"
```

---

## Task 5: Update `Len` and `TotalTaskLen` to use `ZCard`

**Files:**
- Modify: `internal/queue/redis.go`
- Modify: `internal/queue/queue_test.go`

- [ ] **Step 1: Update `TestTotalTaskLen_SumsAllActionQueues`**

The test currently uses `lenByKey` on `scanFakeClient`. Replace with `zcardByKey`:

```go
func TestTotalTaskLen_SumsAllActionQueues(t *testing.T) {
	fake := &scanFakeClient{
		scanKeys: []string{"iran-watcher:tasks:1", "iran-watcher:tasks:2"},
		zcardByKey: map[string]int64{
			"iran-watcher:tasks:1": 3,
			"iran-watcher:tasks:2": 5,
		},
	}
	q := &RedisQueue{client: fake}
	total, err := q.TotalTaskLen(context.Background())
	if err != nil {
		t.Fatalf("TotalTaskLen: %v", err)
	}
	if total != 8 {
		t.Errorf("TotalTaskLen = %d, want 8", total)
	}
}
```

(`TestFlushQueue_DeletesAllActionKeys` requires no change — it doesn't inspect counts.)

- [ ] **Step 2: Run — expect FAIL**

```bash
go test ./internal/queue/... -run "TestTotalTaskLen|TestFlushQueue" -v
```

Expected: `TestTotalTaskLen_SumsAllActionQueues` fails (still calls LLen which doesn't exist on scanFakeClient anymore)

- [ ] **Step 3: Replace `Len` and `TotalTaskLen` in `redis.go`**

Replace `Len`:

```go
// Len returns the number of items in the named queue.
// Task queues (iran-watcher:tasks:*) are sorted sets — uses ZCard.
// All other keys (e.g. dead-letter) are plain lists — uses LLen via the Sizer interface
// but currently this method is only called for task keys, so ZCard is always correct.
func (q *RedisQueue) Len(ctx context.Context, queue string) (int64, error) {
	n, err := q.client.ZCard(ctx, queue).Result()
	if err != nil {
		return 0, fmt.Errorf("queue: len: %w", err)
	}
	return n, nil
}
```

Replace the inner loop in `TotalTaskLen` (change `LLen` → `ZCard`):

```go
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
			n, err := q.client.ZCard(ctx, key).Result()
			if err != nil {
				if errors.Is(err, redis.Nil) {
					continue
				}
				return 0, fmt.Errorf("queue: total task len: zcard %s: %w", key, err)
			}
			total += n
		}
		if next == 0 {
			break
		}
		cursor = next
	}
	return total, nil
}
```

- [ ] **Step 4: Run all queue tests — expect PASS**

```bash
go test ./internal/queue/... -v
```

Expected: all tests pass

- [ ] **Step 5: Commit**

```bash
git add internal/queue/redis.go internal/queue/queue_test.go
git commit -m "feat(queue): Len and TotalTaskLen use ZCard for sorted-set task queues"
```

---

## Task 6: Depth propagation — initial dispatch

**Files:**
- Modify: `internal/worker/pipeline.go`
- Modify: `internal/worker/pipeline_test.go`

- [ ] **Step 1: Write failing test for initial task depth**

Add to `internal/worker/pipeline_test.go`:

```go
func TestPushInitialTasksForGraph_InitialTasksHaveDepthZero(t *testing.T) {
	actionID := int64(10)
	graph := &model.PipelineGraph{
		Nodes: []*model.PipelineNode{
			{ID: 1, PipelineID: 5, Type: model.PipelineNodeTypeStart},
			{ID: 2, PipelineID: 5, Type: model.PipelineNodeTypeAction, ActionID: &actionID},
		},
		Edges: []*model.PipelineEdge{
			{ID: 1, PipelineID: 5, SourceNodeID: 1, TargetNodeID: 2},
		},
	}
	mp := &mockProducer{}
	cache := &ActionCache{entries: make(map[int64]*CacheEntry)}
	cache.entries[actionID] = &CacheEntry{
		Action:   &model.Action{ID: actionID, Name: "test", Active: true},
		Pipeline: &mockPipeline{name: "mock"},
	}
	st := &mockStore{}

	pushed := PushInitialTasksForGraph(
		context.Background(), st, mp, cache,
		graph, 5, "new_tweet",
		[]byte(`{"tweet_id":"t1","text":"hello","author_handle":"u1"}`),
		"t1", nil,
	)
	if pushed != 1 {
		t.Fatalf("expected 1 pushed, got %d", pushed)
	}
	if mp.pushed[0].Depth != 0 {
		t.Errorf("initial task Depth = %d, want 0", mp.pushed[0].Depth)
	}
}
```

- [ ] **Step 2: Run — expect FAIL**

```bash
go test ./internal/worker/... -run TestPushInitialTasksForGraph_InitialTasksHaveDepthZero -v
```

Expected: FAIL — `Depth = 0` passes vacuously (field exists, zero value), unless the test is catching a missing set. Actually the field exists and defaults to 0, so the test may PASS trivially. If so, note it and proceed — the real depth logic will be verified in Task 7 (continuation).

- [ ] **Step 3: Add `depth int` parameter to `dispatchInitialNode`**

In `internal/worker/pipeline.go`, change the signature:

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

In the action node case, set `Depth`:

```go
task := &queue.Task{
	TriggerType:    triggerType,
	Event:          eventPayload,
	ActionID:       *node.ActionID,
	PipelineNodeID: node.ID,
	PipelineID:     pipelineID,
	RootPipelineID: rootPipelineID,
	Depth:          depth,
	Context:        map[string]any{},
}
```

In the condition node case, pass `depth` unchanged in the recursive call:

```go
pushed += dispatchInitialNode(ctx, st, producer, cache, graph, pipelineID,
	rootPipelineID, triggerType, eventPayload, tweetID, nextNode, initCtx, depth, logger)
```

In the sub-pipeline node case, set `Depth: depth + 1` on sub-tasks:

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
	Context:                map[string]any{},
}
```

Update the two call sites of `dispatchInitialNode` to pass `0` for depth:

In `PushInitialTasksForGraph`:
```go
pushed += dispatchInitialNode(ctx, st, producer, cache, graph, pipelineID,
	pipelineID, triggerType, eventPayload, tweetID, node, initCtx, 0, logger)
```

In `PushTasksFromNode`:
```go
return dispatchInitialNode(ctx, st, producer, cache, graph, pipelineID,
	pipelineID, triggerType, eventPayload, tweetID, startNode, initCtx, 0, logger)
```

- [ ] **Step 4: Verify it compiles and the test passes**

```bash
go test ./internal/worker/... -run TestPushInitialTasksForGraph_InitialTasksHaveDepthZero -v
```

Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/worker/pipeline.go internal/worker/pipeline_test.go
git commit -m "feat(pipeline): add depth param to dispatchInitialNode; initial tasks get Depth=0"
```

---

## Task 7: Depth propagation — continuation and sub-pipeline tasks

**Files:**
- Modify: `internal/worker/pipeline.go`
- Modify: `internal/worker/pipeline_test.go`

- [ ] **Step 1: Write failing tests**

Add to `internal/worker/pipeline_test.go`:

```go
func TestPushContinuationTask_IncrementsDepth(t *testing.T) {
	actionID := int64(20)
	graph := &model.PipelineGraph{
		Nodes: []*model.PipelineNode{
			{ID: 10, PipelineID: 5, Type: model.PipelineNodeTypeAction, ActionID: &actionID},
			{ID: 11, PipelineID: 5, Type: model.PipelineNodeTypeAction, ActionID: &actionID},
		},
		Edges: []*model.PipelineEdge{
			{ID: 1, PipelineID: 5, SourceNodeID: 10, TargetNodeID: 11},
		},
	}
	mp := &mockProducer{}
	cache := &ActionCache{entries: make(map[int64]*CacheEntry)}
	cache.entries[actionID] = &CacheEntry{
		Action:   &model.Action{ID: actionID, Name: "test", Active: true},
		Pipeline: &mockPipeline{name: "mock"},
	}

	parentTask := &queue.Task{
		TriggerType:    "new_tweet",
		ActionID:       actionID,
		PipelineNodeID: 10,
		PipelineID:     5,
		RootPipelineID: 0, // 0 skips run-tracking store calls
		Depth:          2,
		Event:          []byte(`{"tweet_id":"t1"}`),
		Context:        map[string]any{},
	}

	// Pre-load the graph so GetPipelineGraph on mockStore returns our graph.
	st := &subPipelineTestStore{subGraph: graph}
	PushContinuationTasks(context.Background(), st, mp, cache, parentTask, map[string]any{}, nil)

	if len(mp.pushed) != 1 {
		t.Fatalf("expected 1 continuation task, got %d", len(mp.pushed))
	}
	if mp.pushed[0].Depth != 3 {
		t.Errorf("continuation task Depth = %d, want 3 (parent.Depth+1)", mp.pushed[0].Depth)
	}
}
```

Also add a helper store for the test (in `pipeline_test.go`):

```go
// subPipelineTestStore wraps mockStore and overrides GetPipelineGraph to return a fixed graph.
type subPipelineTestStore struct {
	mockStore
	subGraph *model.PipelineGraph
}

func (s *subPipelineTestStore) GetPipelineGraph(_ context.Context, _ int64) (*model.PipelineGraph, error) {
	return s.subGraph, nil
}

func (s *subPipelineTestStore) GetPipelineIDForNode(_ context.Context, nodeID int64) (int64, error) {
	if s.subGraph != nil {
		for _, n := range s.subGraph.Nodes {
			if n.ID == nodeID {
				return n.PipelineID, nil
			}
		}
	}
	return 0, nil
}
```

- [ ] **Step 2: Run — expect FAIL**

```bash
go test ./internal/worker/... -run TestPushContinuationTask_IncrementsDepth -v
```

Expected: FAIL — `Depth = 0` (not yet set in pushContinuationTask)

- [ ] **Step 3: Update `pushContinuationTask`**

In `internal/worker/pipeline.go`, add `Depth` to the task built in `pushContinuationTask`:

```go
func pushContinuationTask(
	ctx context.Context,
	st store.Store,
	producer queue.Producer,
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
		Context:                copyContext(mergedCtx),
	}
	return pushTrackedTask(ctx, st, producer, task, logger)
}
```

- [ ] **Step 4: Update sub-pipeline task creation in `PushContinuationTasks` — regular case**

In `PushContinuationTasks`, find the `case model.PipelineNodeTypePipeline:` block (the one outside the condition handler). Add `Depth: task.Depth + 1`:

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
	Context:                copyContext(mergedCtx),
}
```

- [ ] **Step 5: Update sub-pipeline task creation in `PushContinuationTasks` — post-condition case**

Find the second sub-pipeline task construction (inside the `case model.PipelineNodeTypeCondition:` → `case model.PipelineNodeTypePipeline:` block). Add the same `Depth: task.Depth + 1`:

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
	Context:                copyContext(mergedCtx),
}
```

- [ ] **Step 6: Update `parentTask` in `signalSubPipelineLeafDone`**

In `PushContinuationTasks`, find the `signalSubPipelineLeafDone` closure and add `Depth` to `parentTask`:

```go
parentTask := &queue.Task{
	TriggerType:    task.TriggerType,
	Event:          task.Event,
	PipelineNodeID: *task.ParentPipelineNodeID,
	PipelineID:     parentPipelineID,
	RootPipelineID: task.RootPipelineID,
	Depth:          task.Depth, // parent continuation inherits last sub-task depth
}
```

- [ ] **Step 7: Run all worker tests — expect PASS**

```bash
go test ./internal/worker/... -v
```

Expected: all pass

- [ ] **Step 8: Commit**

```bash
git add internal/worker/pipeline.go internal/worker/pipeline_test.go
git commit -m "feat(pipeline): propagate Depth through continuation and sub-pipeline tasks"
```

---

## Task 8: Full test suite + Docker build + CLAUDE.md

**Files:**
- Modify: `CLAUDE.md`

- [ ] **Step 1: Run the full test suite**

```bash
go test ./...
```

Expected: all packages pass

- [ ] **Step 2: Build the Docker image**

```bash
docker compose build --build-arg BUILDKIT_INLINE_CACHE=1 iran-watcher
```

Expected: successful build with no errors

- [ ] **Step 3: Update CLAUDE.md**

In the "Key Design Decisions" section, find the bullet that describes the queue system (`**At-most-once delivery**` or the worker/queue bullet) and add the priority note.

Find the line:
```
- **At-most-once delivery** for the Redis queue — acceptable because tweets are persisted to SQLite before queueing
```

Append after it:
```
- **Depth-priority queue** — per-action task queues are Redis sorted sets (score = `Task.Depth`); `BZPopMin` dequeues the lowest-depth task first; `Depth` is the number of action-hops from the pipeline's Start node (0 for initial tasks, +1 per continuation hop, +1 when entering a sub-pipeline); lower depth = higher priority so early-stage actions are never starved behind deep sub-pipeline work; the failed queue (`iran-watcher:failed`) remains a plain list
```

- [ ] **Step 4: Flush the queue after deploy**

After deploying, call the flush endpoint once to clear any tasks left in the old list-format queues:

```bash
curl -X DELETE http://localhost:8080/api/queue \
  -H "Authorization: Bearer $TOKEN"
```

Expected: `{"flushed": true}` (or similar success response)

- [ ] **Step 5: Final commit**

```bash
git add CLAUDE.md
git commit -m "docs(claude): document depth-priority sorted-set queue"
```
