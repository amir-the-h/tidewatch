package queue

import (
	"context"
	"encoding/json"
	"testing"
	"time"

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

func TestPopTask_ReturnsNilOnEmptyResult(t *testing.T) {
	// BRPop can also return an empty slice with no error in some edge cases.
	fake := &fakeRedisClient{
		brpopErr:    nil,
		brpopResult: []string{}, // empty — fewer than 2 elements
	}
	q := &RedisQueue{client: fake}

	task, err := q.PopTask(context.Background(), 1*time.Second)
	if err != nil {
		t.Fatalf("expected nil error on empty result, got: %v", err)
	}
	if task != nil {
		t.Fatalf("expected nil task on empty result, got: %+v", task)
	}
}
