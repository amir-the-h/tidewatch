package worker

import (
	"context"
	"log/slog"
	"sync"
	"sync/atomic"

	"github.com/amir-the-h/iran-watcher/internal/inference"
	"github.com/amir-the-h/iran-watcher/internal/notifier"
	"github.com/amir-the-h/iran-watcher/internal/queue"
	"github.com/amir-the-h/iran-watcher/internal/store"
)

// PoolStats holds a snapshot of worker pool activity.
type PoolStats struct {
	Total          int                `json:"total"`
	Active         int                `json:"active"`
	Idle           int                `json:"idle"`
	Queued         int                `json:"queued,omitempty"`          // tasks buffered in in-memory pipeline channels
	Pipelines      map[string]int     `json:"pipelines,omitempty"`       // per-pipeline in-memory buffer depth
	PipelineActive map[string]int     `json:"pipeline_active,omitempty"` // workers currently executing each pipeline
	PipelineRate   map[string]float64 `json:"pipeline_rate,omitempty"`   // per-pipeline task throughput (tasks/sec)
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
