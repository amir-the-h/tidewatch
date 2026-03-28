package worker

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"sync/atomic"
	"time"

	"github.com/amir-the-h/iran-watcher/internal/model"
	"github.com/amir-the-h/iran-watcher/internal/notifier"
	"github.com/amir-the-h/iran-watcher/internal/queue"
	"github.com/amir-the-h/iran-watcher/internal/store"
)

// worker is the legacy per-goroutine worker used by Pool.
// Deprecated: use Manager instead. Kept for backward compatibility.
type worker struct {
	id       int
	consumer queue.Consumer
	producer queue.Producer
	store    store.Store
	logger   *slog.Logger
	emitter  notifier.EventEmitter
	active   *atomic.Int32 // shared with Pool; incremented while processing a tweet
}

func (w *worker) run(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			return
		default:
		}

		// Pop a Task from the tasks queue.
		task, err := w.consumer.PopTask(ctx, 3*time.Second)
		if err != nil {
			if w.logger != nil {
				w.logger.Error("worker: run: pop failed",
					"worker_id", w.id,
					"error", fmt.Errorf("worker: run: %w", err))
			}
			continue
		}
		if task == nil {
			continue
		}

		if w.active != nil {
			w.active.Add(1)
		}

		var ev queue.TweetEvent
		_ = json.Unmarshal(task.Event, &ev)

		// Legacy pool does not resolve pipelines — log and skip.
		if w.logger != nil {
			w.logger.Warn("worker (legacy pool): received task — use Manager for full processing",
				"worker_id", w.id, "action_id", task.ActionID, "tweet_id", ev.TweetID)
		}

		if w.active != nil {
			w.active.Add(-1)
		}
	}
}

// matchEntities is a no-op stub kept for compatibility. The Manager handles entity matching.
func (w *worker) matchEntities(_ context.Context, _ *model.Tweet, _ []model.NEREntity) {}
