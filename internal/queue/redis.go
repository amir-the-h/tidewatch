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
	// List operations — dead-letter queue only.
	LPush(ctx context.Context, key string, values ...interface{}) *redis.IntCmd
	BRPop(ctx context.Context, timeout time.Duration, keys ...string) *redis.StringSliceCmd
	LRange(ctx context.Context, key string, start, stop int64) *redis.StringSliceCmd
	Del(ctx context.Context, keys ...string) *redis.IntCmd

	// Sorted-set operations — per-action task queues.
	ZAdd(ctx context.Context, key string, members ...redis.Z) *redis.IntCmd
	BZPopMin(ctx context.Context, timeout time.Duration, keys ...string) *redis.ZWithKeyCmd
	ZCard(ctx context.Context, key string) *redis.IntCmd
}

// scanner is implemented by redis clients that support SCAN (e.g. *redis.Client).
// Test fakes that omit this interface cause TotalTaskLen and FlushQueue to no-op.
type scanner interface {
	Scan(context.Context, uint64, string, int64) *redis.ScanCmd
}

// RedisQueue implements both Producer and Consumer.
// Per-action task queues use Redis sorted sets (ZAdd/BZPopMin) for depth-priority ordering.
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

// PopTask blocks until a Task is available on one of the given sorted-set keys
// or the timeout elapses. Returns the task with the lowest Depth (score) first.
// Returns (nil, nil) on timeout or when no keys are provided.
func (q *RedisQueue) PopTask(ctx context.Context, timeout time.Duration, keys ...string) (*Task, error) {
	if len(keys) == 0 {
		return nil, nil
	}
	zwk, err := q.client.BZPopMin(ctx, timeout, keys...).Result()
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
		pending, _ := q.client.ZCard(ctx, zwk.Key).Result()
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

// Len returns the number of items currently in the named task queue (sorted set).
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

// FlushQueue deletes all per-action task queues by scanning for keys matching
// iran-watcher:tasks:*. No-op if the underlying client does not support SCAN.
func (q *RedisQueue) FlushQueue(ctx context.Context) error {
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
