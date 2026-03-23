package worker

import (
	"context"
	"log/slog"
	"sync"

	"github.com/amir-the-h/iran-watcher/internal/inference"
	"github.com/amir-the-h/iran-watcher/internal/queue"
	"github.com/amir-the-h/iran-watcher/internal/store"
)

// Pool manages N worker goroutines that process tweets from the queue.
type Pool struct {
	count    int
	consumer queue.Consumer
	producer queue.Producer
	store    store.Store
	registry *inference.Registry
	logger   *slog.Logger
}

// NewPool creates a new Pool with the given number of workers and dependencies.
func NewPool(count int, consumer queue.Consumer, producer queue.Producer, s store.Store, reg *inference.Registry, logger *slog.Logger) *Pool {
	return &Pool{
		count:    count,
		consumer: consumer,
		producer: producer,
		store:    s,
		registry: reg,
		logger:   logger,
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
			registry: p.registry,
			logger:   p.logger,
		}
		go func() {
			defer wg.Done()
			w.run(ctx)
		}()
	}

	wg.Wait()
	return nil
}
