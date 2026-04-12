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
// Test 1: FailedTweet JSON round-trip
// ---------------------------------------------------------------------------

func TestFailedTweet_JSONRoundTrip(t *testing.T) {
	ev, _ := json.Marshal(TweetEvent{
		TweetID:      "123",
		Text:         "Hello Iran",
		AuthorHandle: "testuser",
		RuleIDs:      []int64{1},
	})
	original := &FailedTweet{
		Task: &Task{
			TriggerType: "new_tweet",
			Event:       json.RawMessage(ev),
			ActionID:    42,
		},
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

	if got.Task == nil {
		t.Fatal("task is nil after round-trip")
	}
	if got.Task.ActionID != original.Task.ActionID {
		t.Errorf("action ID: got %d, want %d", got.Task.ActionID, original.Task.ActionID)
	}
	if got.Task.TriggerType != original.Task.TriggerType {
		t.Errorf("trigger type: got %q, want %q", got.Task.TriggerType, original.Task.TriggerType)
	}
	if len(got.FailedModels) != 1 {
		t.Fatalf("failed models: got %d, want 1", len(got.FailedModels))
	}
	if got.FailedModels[0].Name != "sentiment" {
		t.Errorf("failed model name: got %q, want %q", got.FailedModels[0].Name, "sentiment")
	}
	if got.FailedModels[0].Error != "model not loaded" {
		t.Errorf("failed model error: got %q, want %q", got.FailedModels[0].Error, "model not loaded")
	}
}

// ---------------------------------------------------------------------------
// Test 2: Task JSON round-trip
// ---------------------------------------------------------------------------

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

// ---------------------------------------------------------------------------
// Test 3: PopTask returns (nil, nil) on timeout (redis.Nil error from BRPop)
// ---------------------------------------------------------------------------

// fakeRedisClient is a minimal redisClient implementation for testing.
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

func (f *fakeRedisClient) LLen(_ context.Context, _ string) *redis.IntCmd {
	return redis.NewIntResult(0, nil)
}

func (f *fakeRedisClient) LRange(_ context.Context, _ string, _, _ int64) *redis.StringSliceCmd {
	return redis.NewStringSliceResult(nil, nil)
}

func (f *fakeRedisClient) Del(_ context.Context, _ ...string) *redis.IntCmd {
	return redis.NewIntResult(0, nil)
}

func TestPopTask_ReturnsNilOnTimeout(t *testing.T) {
	fake := &fakeRedisClient{
		brpopErr:    redis.Nil, // BRPop returns redis.Nil on timeout
		brpopResult: nil,
	}
	q := &RedisQueue{client: fake}

	task, err := q.PopTask(context.Background(), 1*time.Second, "iran-watcher:tasks:1")
	if err != nil {
		t.Fatalf("expected nil error on timeout, got: %v", err)
	}
	if task != nil {
		t.Fatalf("expected nil task on timeout, got: %+v", task)
	}
}

func TestPopFailed_ReturnsNilOnTimeout(t *testing.T) {
	fake := &fakeRedisClient{
		brpopErr:    redis.Nil,
		brpopResult: nil,
	}
	q := &RedisQueue{client: fake}

	ft, err := q.PopFailed(context.Background(), 1*time.Second)
	if err != nil {
		t.Fatalf("expected nil error on timeout, got: %v", err)
	}
	if ft != nil {
		t.Fatalf("expected nil FailedTweet on timeout, got: %+v", ft)
	}
}

func TestPopTask_ReturnsNilOnEmptyResult(t *testing.T) {
	// BRPop can also return an empty slice with no error in some edge cases.
	fake := &fakeRedisClient{
		brpopErr:    nil,
		brpopResult: []string{}, // empty — fewer than 2 elements
	}
	q := &RedisQueue{client: fake}

	task, err := q.PopTask(context.Background(), 1*time.Second, "iran-watcher:tasks:1")
	if err != nil {
		t.Fatalf("expected nil error on empty result, got: %v", err)
	}
	if task != nil {
		t.Fatalf("expected nil task on empty result, got: %+v", task)
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

// ---------------------------------------------------------------------------
// scanFakeClient — extends fakeRedisClient with Scan for TotalTaskLen/FlushQueue
// ---------------------------------------------------------------------------

// scanFakeClient extends fakeRedisClient with a Scan implementation for testing
// TotalTaskLen and FlushQueue.
type scanFakeClient struct {
	fakeRedisClient
	scanKeys []string         // keys returned by Scan
	lenByKey map[string]int64 // key → LLEN result
	deleted  []string         // keys passed to Del
}

func (f *scanFakeClient) Scan(_ context.Context, cursor uint64, _ string, _ int64) *redis.ScanCmd {
	// Return all keys on the first call (cursor 0), then signal done (next=0).
	if cursor == 0 {
		return redis.NewScanCmdResult(f.scanKeys, 0, nil)
	}
	return redis.NewScanCmdResult(nil, 0, nil)
}

func (f *scanFakeClient) LLen(_ context.Context, key string) *redis.IntCmd {
	n := f.lenByKey[key]
	return redis.NewIntResult(n, nil)
}

func (f *scanFakeClient) Del(_ context.Context, keys ...string) *redis.IntCmd {
	f.deleted = append(f.deleted, keys...)
	return redis.NewIntResult(int64(len(keys)), nil)
}

func TestTotalTaskLen_SumsAllActionQueues(t *testing.T) {
	fake := &scanFakeClient{
		scanKeys: []string{"iran-watcher:tasks:1", "iran-watcher:tasks:2"},
		lenByKey: map[string]int64{
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

func TestFlushQueue_DeletesAllActionKeys(t *testing.T) {
	fake := &scanFakeClient{
		scanKeys: []string{"iran-watcher:tasks:3", "iran-watcher:tasks:4"},
	}
	q := &RedisQueue{client: fake}
	if err := q.FlushQueue(context.Background()); err != nil {
		t.Fatalf("FlushQueue: %v", err)
	}
	if len(fake.deleted) != 2 {
		t.Errorf("deleted %d keys, want 2", len(fake.deleted))
	}
}

// ---------------------------------------------------------------------------
// Key format tests
// ---------------------------------------------------------------------------

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

// captureBroadcaster records all Broadcast calls for test assertions.
type captureBroadcaster struct {
	events []hub.Event
}

func (b *captureBroadcaster) Broadcast(e hub.Event) {
	b.events = append(b.events, e)
}

// brpopSuccessFakeClient returns a successful BRPOP result with a configurable LLEN.
type brpopSuccessFakeClient struct {
	fakeRedisClient
	brpopKey   string
	brpopVal   string
	llenResult int64
}

func (f *brpopSuccessFakeClient) BRPop(_ context.Context, _ time.Duration, _ ...string) *redis.StringSliceCmd {
	return redis.NewStringSliceResult([]string{f.brpopKey, f.brpopVal}, nil)
}

func (f *brpopSuccessFakeClient) LLen(_ context.Context, _ string) *redis.IntCmd {
	return redis.NewIntResult(f.llenResult, nil)
}

func TestPopTask_BroadcastsQueueUpdateOnSuccess(t *testing.T) {
	ev, _ := json.Marshal(TweetEvent{TweetID: "1", Text: "test", AuthorHandle: "user"})
	taskJSON, _ := json.Marshal(&Task{
		TriggerType: "new_tweet",
		ActionID:    7,
		Event:       json.RawMessage(ev),
		Context:     map[string]any{},
	})
	fake := &brpopSuccessFakeClient{
		brpopKey:   "iran-watcher:tasks:7",
		brpopVal:   string(taskJSON),
		llenResult: 5,
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
	if len(bc.events) != 1 {
		t.Fatalf("expected 1 broadcast event, got %d", len(bc.events))
	}
	if bc.events[0].Type != "queue_update" {
		t.Errorf("event type: got %q, want %q", bc.events[0].Type, "queue_update")
	}
	payload, ok := bc.events[0].Payload.(map[string]int64)
	if !ok {
		t.Fatalf("expected map[string]int64 payload, got %T", bc.events[0].Payload)
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
	fake := &brpopSuccessFakeClient{brpopKey: "iran-watcher:tasks:3", brpopVal: string(taskJSON), llenResult: 0}
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
