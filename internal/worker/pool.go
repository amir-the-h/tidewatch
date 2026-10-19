package worker

import (
	"context"
	"log/slog"
	"sync"
	"sync/atomic"
	"time"

	"github.com/amir-the-h/tidewatch/internal/inference"
	"github.com/amir-the-h/tidewatch/internal/notifier"
	"github.com/amir-the-h/tidewatch/internal/queue"
	"github.com/amir-the-h/tidewatch/internal/store"
)

// WorkerInfo describes a single worker slot.
// Idle workers have Status="idle" and zero values for all other fields.
type WorkerInfo struct {
	ID          int       `json:"id"`
	Status      string    `json:"status"` // "active" | "idle"
	ActionID    int64     `json:"action_id"`
	Action      string    `json:"action"`
	Pipeline    string    `json:"pipeline"`
	TweetID     string    `json:"tweet_id"`
	StartedAt   time.Time `json:"started_at"`
	DurationMS  int64     `json:"duration_ms"`  // computed at snapshot time
	BackendName string    `json:"backend_name"` // display name; empty for non-Ollama
}

// PoolStats holds a snapshot of worker pool activity.
type PoolStats struct {
	Total   int          `json:"total"`
	Active  int          `json:"active"`
	Idle    int          `json:"idle"`
	Workers []WorkerInfo `json:"workers"` // all workers in fixed ascending ID order
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
