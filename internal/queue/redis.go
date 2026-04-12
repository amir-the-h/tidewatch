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
