package queue

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/amir-the-h/tidewatch/internal/hub"
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
	if got := QueueTasksKey(42); got != "tidewatch:tasks:42" {
		t.Errorf("QueueTasksKey(42) = %q, want %q", got, "tidewatch:tasks:42")
	}
	if got := QueueTasksKey(1); got != "tidewatch:tasks:1" {
		t.Errorf("QueueTasksKey(1) = %q, want %q", got, "tidewatch:tasks:1")
	}
}

// ---------------------------------------------------------------------------
// Test 2: FailedTask JSON round-trip
// ---------------------------------------------------------------------------

func TestFailedTask_JSONRoundTrip(t *testing.T) {
	ev, _ := json.Marshal(SignalEvent{SignalID: 123, Kind: "post"})
	original := &FailedTask{
		Task: &Task{TriggerType: "new_signal", Event: json.RawMessage(ev), ActionID: 42},
		FailedModels: []FailedModel{
			{Name: "sentiment", Error: "model not loaded", Timestamp: time.Date(2026, 3, 23, 0, 0, 0, 0, time.UTC)},
		},
	}
	data, err := json.Marshal(original)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var got FailedTask
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
// Test 3: Task JSON round-trip (Depth and IsOllama are intentionally absent — per-action routing supersedes them)
// ---------------------------------------------------------------------------

func TestTask_JSONRoundTrip(t *testing.T) {
	ev, _ := json.Marshal(SignalEvent{SignalID: 789, Kind: "post", SourceType: "x_stream"})
	original := &Task{
		TriggerType: "new_signal",
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
	if got.TriggerType != "new_signal" {
		t.Errorf("TriggerType: got %q, want new_signal", got.TriggerType)
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
	task := &Task{TriggerType: "new_signal", ActionID: 42, Context: map[string]any{}}
	if err := q.PushTask(context.Background(), task); err != nil {
		t.Fatalf("PushTask: %v", err)
	}
	want := "tidewatch:tasks:42"
	if fake.pushedKey != want {
		t.Errorf("pushed to key %q, want %q", fake.pushedKey, want)
	}
}

func TestPushTask_BroadcastsActionPending(t *testing.T) {
	fake := &pushCaptureFakeClient{}
	bc := &captureBroadcaster{}
	q := &RedisQueue{client: fake, broadcaster: bc}
	task := &Task{TriggerType: "new_signal", ActionID: 7, Context: map[string]any{}}
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
	task := &Task{TriggerType: "new_signal", ActionID: 3, Context: map[string]any{}}
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
	task, err := q.PopTask(context.Background(), 1*time.Second, "tidewatch:tasks:1")
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
	ev, _ := json.Marshal(SignalEvent{SignalID: 1, Kind: "post"})
	taskJSON, _ := json.Marshal(&Task{TriggerType: "new_signal", ActionID: 7, Event: json.RawMessage(ev), Context: map[string]any{}})
	fake := &brpopSuccessFakeClient{
		fakeRedisClient: fakeRedisClient{llenResult: map[string]int64{"tidewatch:tasks:7": 5}},
		result:          []string{"tidewatch:tasks:7", string(taskJSON)},
	}
	bc := &captureBroadcaster{}
	q := &RedisQueue{client: fake, broadcaster: bc}

	task, err := q.PopTask(context.Background(), 1*time.Second, "tidewatch:tasks:7")
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
		t.Fatalf("expected nil FailedTask on timeout, got: %+v", ft)
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
