package worker

import (
	"sync"
	"sync/atomic"
	"time"

	"github.com/amir-the-h/iran-watcher/internal/inference"
	"github.com/amir-the-h/iran-watcher/internal/model"
	"github.com/amir-the-h/iran-watcher/internal/queue"
)

// PipelineTask is a unit of work: run one pipeline on one tweet.
type PipelineTask struct {
	Tweet    *model.StreamTweet
	Pipeline inference.Pipeline
	Tracker  *completionTracker
}

// completionTracker coordinates per-tweet state across concurrent pipeline runs.
// The last pipeline to finish triggers entity matching and dead-letter writes.
type completionTracker struct {
	tweet        *model.StreamTweet
	pending      atomic.Int32
	mu           sync.Mutex
	nerEntities  []model.NEREntity
	failedModels []queue.FailedModel
}

func newCompletionTracker(tweet *model.StreamTweet, n int) *completionTracker {
	t := &completionTracker{tweet: tweet}
	t.pending.Store(int32(n))
	return t
}

// record stores the result of one pipeline run and decrements the pending counter.
// Returns true when this was the last pending pipeline (caller should finalize).
func (t *completionTracker) record(nerEnts []model.NEREntity, failed *queue.FailedModel) bool {
	t.mu.Lock()
	t.nerEntities = append(t.nerEntities, nerEnts...)
	if failed != nil {
		t.failedModels = append(t.failedModels, *failed)
	}
	t.mu.Unlock()
	return t.pending.Add(-1) == 0
}

func unavailableFailedModel(name string) *queue.FailedModel {
	return &queue.FailedModel{
		Name:      name,
		Error:     "pipeline channel unavailable after registry reload",
		Timestamp: time.Now(),
	}
}
