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
// Test 3: PopTask returns (nil, nil) on timeout (redis.Nil error from BZPopMin)
// ---------------------------------------------------------------------------

// fakeRedisClient implements redisClient for unit tests.
// BZPopMin returns redis.Nil (timeout) by default — override in derived fakes for success paths.
type fakeRedisClient struct {
	brpopErr    error
	brpopResult []string
	zcard       map[string]int64
	deleted     []string
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

func (f *fakeRedisClient) Del(ctx context.Context, keys ...string) *redis.IntCmd {
	f.deleted = append(f.deleted, keys...)
	return redis.NewIntCmd(ctx)
}

func (f *fakeRedisClient) ZAdd(_ context.Context, _ string, _ ...redis.Z) *redis.IntCmd {
	return redis.NewIntResult(1, nil)
}

func (f *fakeRedisClient) BZPopMin(ctx context.Context, _ time.Duration, _ ...string) *redis.ZWithKeyCmd {
	cmd := redis.NewZWithKeyCmd(ctx, "bzpopmin")
	cmd.SetErr(redis.Nil) // default: no item available (timeout)
	return cmd
}

func (f *fakeRedisClient) ZCard(_ context.Context, key string) *redis.IntCmd {
	cmd := redis.NewIntCmd(context.Background())
	if f.zcard != nil {
		cmd.SetVal(f.zcard[key])
	}
	return cmd
}

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

func TestPopTask_ReturnsNilOnTimeout(t *testing.T) {
	// fakeRedisClient.BZPopMin returns redis.Nil (timeout) by default.
	q := &RedisQueue{client: &fakeRedisClient{}}

	task, err := q.PopTask(context.Background(), 1*time.Second)
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

func TestPushTask_RoutesToGlobalKey(t *testing.T) {
	fake := &pushCaptureFakeClient{}
	q := &RedisQueue{client: fake}

	task := &Task{TriggerType: "new_tweet", ActionID: 7, Depth: 2, Context: map[string]any{}}
	if err := q.PushTask(context.Background(), task); err != nil {
		t.Fatalf("PushTask: %v", err)
	}
	want := "iran-watcher:tasks"
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
	if _, ok := payload["pending_tasks"]; !ok {
		t.Error("broadcast payload missing 'pending_tasks'")
	}
	if _, ok := payload["pending_ollama"]; !ok {
		t.Error("broadcast payload missing 'pending_ollama'")
	}
}

// ---------------------------------------------------------------------------
// Key constant tests
// ---------------------------------------------------------------------------

// TestQueueKeys_AreConstants verifies the queue key constants.
func TestQueueKeys_AreConstants(t *testing.T) {
	if QueueTasksKey != "iran-watcher:tasks" {
		t.Errorf("QueueTasksKey = %q, want %q", QueueTasksKey, "iran-watcher:tasks")
	}
	if QueueOllamaTasksKey != "iran-watcher:tasks:ollama" {
		t.Errorf("QueueOllamaTasksKey = %q, want %q", QueueOllamaTasksKey, "iran-watcher:tasks:ollama")
	}
}

// captureBroadcaster records all Broadcast calls for test assertions.
type captureBroadcaster struct {
	events []hub.Event
}

func (b *captureBroadcaster) Broadcast(e hub.Event) {
	b.events = append(b.events, e)
}

// bzpopSuccessFakeClient simulates a successful BZPopMin result for PopTask tests.
type bzpopSuccessFakeClient struct {
	fakeRedisClient
	popKey    string
	popMember string
	zcardN    int64
}

func (f *bzpopSuccessFakeClient) BZPopMin(ctx context.Context, _ time.Duration, _ ...string) *redis.ZWithKeyCmd {
	cmd := redis.NewZWithKeyCmd(ctx, "bzpopmin")
	cmd.SetVal(&redis.ZWithKey{Key: f.popKey, Z: redis.Z{Score: 0, Member: f.popMember}})
	return cmd
}

func (f *bzpopSuccessFakeClient) ZCard(_ context.Context, _ string) *redis.IntCmd {
	return redis.NewIntResult(f.zcardN, nil)
}

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

func TestPopTask_BroadcastsQueueUpdateOnSuccess(t *testing.T) {
	ev, _ := json.Marshal(TweetEvent{TweetID: "1", Text: "test", AuthorHandle: "user"})
	taskJSON, _ := json.Marshal(&Task{
		TriggerType: "new_tweet",
		ActionID:    7,
		Event:       json.RawMessage(ev),
		Context:     map[string]any{},
	})
	fake := &bzpopSuccessFakeClient{
		popKey:    "iran-watcher:tasks",
		popMember: string(taskJSON),
		zcardN:    5,
	}
	bc := &captureBroadcaster{}
	q := &RedisQueue{client: fake, broadcaster: bc}

	task, err := q.PopTask(context.Background(), 1*time.Second)
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
	if _, ok := payload["pending_tasks"]; !ok {
		t.Error("broadcast payload missing 'pending_tasks'")
	}
	if _, ok := payload["pending_ollama"]; !ok {
		t.Error("broadcast payload missing 'pending_ollama'")
	}
	if _, hasOld := payload["pending"]; hasOld {
		t.Error("broadcast payload must not contain legacy 'pending' key")
	}
}

func TestPopTask_NoBroadcastWhenNoBroadcaster(t *testing.T) {
	ev, _ := json.Marshal(TweetEvent{TweetID: "2", Text: "x", AuthorHandle: "u"})
	taskJSON, _ := json.Marshal(&Task{TriggerType: "new_tweet", ActionID: 3, Event: json.RawMessage(ev), Context: map[string]any{}})
	fake := &bzpopSuccessFakeClient{popKey: "iran-watcher:tasks", popMember: string(taskJSON), zcardN: 0}
	q := &RedisQueue{client: fake} // no broadcaster

	task, err := q.PopTask(context.Background(), 1*time.Second)
	if err != nil {
		t.Fatalf("PopTask: %v", err)
	}
	if task == nil {
		t.Fatal("expected non-nil task")
	}
	// No panic = pass
}

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
		t.Error("broadcast payload must not contain legacy 'pending' key")
	}
}

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
