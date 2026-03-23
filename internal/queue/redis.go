package queue

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/amir-the-h/iran-watcher/internal/model"
	"github.com/redis/go-redis/v9"
)

// redisClient is the narrow subset of redis.Client methods used by RedisQueue.
// Defining it as an interface allows injection of test doubles.
type redisClient interface {
	LPush(ctx context.Context, key string, values ...interface{}) *redis.IntCmd
	BRPop(ctx context.Context, timeout time.Duration, keys ...string) *redis.StringSliceCmd
}

// RedisQueue implements both Producer and Consumer using Redis lists.
// Push uses LPUSH; Pop uses BRPOP (blocking right-pop from a left-push queue).
type RedisQueue struct {
	client redisClient
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

// Push serializes a StreamTweet to JSON and LPUSHes it onto the named queue.
func (q *RedisQueue) Push(ctx context.Context, queue string, tweet *model.StreamTweet) error {
	data, err := json.Marshal(tweet)
	if err != nil {
		return fmt.Errorf("queue: push: marshal: %w", err)
	}
	if err := q.client.LPush(ctx, queue, data).Err(); err != nil {
		return fmt.Errorf("queue: push: %w", err)
	}
	return nil
}

// PushFailed pushes a FailedTweet to the dead-letter queue.
func (q *RedisQueue) PushFailed(ctx context.Context, ft *FailedTweet) error {
	data, err := json.Marshal(ft)
	if err != nil {
		return fmt.Errorf("queue: push failed: marshal: %w", err)
	}
	if err := q.client.LPush(ctx, QueueFailed, data).Err(); err != nil {
		return fmt.Errorf("queue: push failed: %w", err)
	}
	return nil
}

// Pop blocks until a StreamTweet is available on the given queue or the timeout
// elapses. Returns (nil, nil) on timeout.
func (q *RedisQueue) Pop(ctx context.Context, queue string, timeout time.Duration) (*model.StreamTweet, error) {
	result, err := q.client.BRPop(ctx, timeout, queue).Result()
	if err != nil {
		if err == redis.Nil {
			// Timeout — no message available, not an error.
			return nil, nil
		}
		return nil, fmt.Errorf("queue: pop: %w", err)
	}
	if len(result) < 2 {
		// Empty result from BRPop — treat as timeout.
		return nil, nil
	}
	var tweet model.StreamTweet
	if err := json.Unmarshal([]byte(result[1]), &tweet); err != nil {
		return nil, fmt.Errorf("queue: pop: unmarshal: %w", err)
	}
	return &tweet, nil
}

// PopFailed pops a FailedTweet from the dead-letter queue.
// Returns (nil, nil) on timeout.
func (q *RedisQueue) PopFailed(ctx context.Context, timeout time.Duration) (*FailedTweet, error) {
	result, err := q.client.BRPop(ctx, timeout, QueueFailed).Result()
	if err != nil {
		if err == redis.Nil {
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
