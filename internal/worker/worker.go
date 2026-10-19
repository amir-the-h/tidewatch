package worker

import (
	"context"
	"log/slog"
	"sync/atomic"

	"github.com/amir-the-h/tidewatch/internal/model"
	"github.com/amir-the-h/tidewatch/internal/notifier"
	"github.com/amir-the-h/tidewatch/internal/queue"
	"github.com/amir-the-h/tidewatch/internal/store"
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
	// Legacy pool worker: processing is a no-op — use Manager instead.
	if w.logger != nil {
		w.logger.Warn("worker (legacy pool): started — use Manager for full processing")
	}
	<-ctx.Done()
}

// matchEntities is a no-op stub kept for compatibility. The Manager handles entity matching.
func (w *worker) matchEntities(_ context.Context, _ *model.Tweet, _ []model.NEREntity) {}
