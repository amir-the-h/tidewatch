package worker

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"sync/atomic"
	"time"

	"github.com/amir-the-h/iran-watcher/internal/inference"
	"github.com/amir-the-h/iran-watcher/internal/model"
	"github.com/amir-the-h/iran-watcher/internal/notifier"
	"github.com/amir-the-h/iran-watcher/internal/queue"
	"github.com/amir-the-h/iran-watcher/internal/store"
)

type worker struct {
	id       int
	consumer queue.Consumer
	producer queue.Producer
	store    store.Store
	registry *inference.Registry
	logger   *slog.Logger
	emitter  notifier.EventEmitter
	active   *atomic.Int32 // shared with Pool; incremented while processing a tweet
}

func (w *worker) run(ctx context.Context) {
	for {
		// Step 1: check for context cancellation or registry reload signal.
		select {
		case <-ctx.Done():
			return
		case <-w.registry.NotifyChan():
			// registry reloaded — pipelines updated, continue to next iteration
			continue
		default:
		}

		// Step 2: Pop from the raw queue with a 3-second timeout.
		streamTweet, err := w.consumer.Pop(ctx, queue.QueueRaw, 3*time.Second)
		if err != nil {
			if w.logger != nil {
				w.logger.Error("worker: run: pop failed",
					"worker_id", w.id,
					"error", fmt.Errorf("worker: run: %w", err))
			}
			continue
		}

		// Step 3: nil result means timeout — loop back.
		if streamTweet == nil {
			continue
		}

		if w.active != nil {
			w.active.Add(1)
		}

		// Step 4: resolve which rules this tweet matched, then select applicable pipelines.
		tweetRuleIDs, ruleErr := w.store.ListTweetRuleIDs(ctx, streamTweet.Tweet.ID)
		if ruleErr != nil && w.logger != nil {
			w.logger.Warn("worker: run: list tweet rule ids",
				"worker_id", w.id,
				"tweet_id", streamTweet.Tweet.ID,
				"error", ruleErr)
		}
		pipelines := w.registry.AllForRules(tweetRuleIDs)
		// NOTE: legacy worker does not support TargetActions filtering.
		// The Manager-based dispatcher handles action targeting.
		var failedModels []queue.FailedModel

		// nerEntities collects NER entities from any pipeline that returns them.
		var nerEntities []model.NEREntity

		for _, pipeline := range pipelines {
			result, runErr := pipeline.Run([]byte(streamTweet.Tweet.Text))
			if runErr != nil {
				if w.logger != nil {
					w.logger.Warn("worker: run: pipeline failed",
						"worker_id", w.id,
						"tweet_id", streamTweet.Tweet.ID,
						"pipeline", pipeline.Name(),
						"error", fmt.Errorf("worker: run: %w", runErr))
				}
				failedModels = append(failedModels, queue.FailedModel{
					Name:      pipeline.Name(),
					Error:     runErr.Error(),
					Timestamp: time.Now(),
				})
				if w.emitter != nil {
					w.emitter.Emit(notifier.NotificationEvent{
						Type:      notifier.EventCategorizationFailed,
						Tweet:     streamTweet.Tweet,
						ModelName: pipeline.Name(),
						Error:     runErr.Error(),
						Timestamp: time.Now(),
					})
				}
				continue
			}

			// Collect NER entities from any pipeline that produces them.
			var nerOut struct {
				Entities []model.NEREntity `json:"entities"`
			}
			if json.Unmarshal(result, &nerOut) == nil {
				nerEntities = append(nerEntities, nerOut.Entities...)
			}

			c := &model.Categorization{
				TweetID:   streamTweet.Tweet.ID,
				ModelName: pipeline.Name(),
				Results:   result,
				CreatedAt: time.Now(),
			}
			if saveErr := w.store.SaveCategorization(ctx, c); saveErr != nil {
				if w.logger != nil {
					w.logger.Warn("worker: run: save categorization failed",
						"worker_id", w.id,
						"tweet_id", streamTweet.Tweet.ID,
						"pipeline", pipeline.Name(),
						"error", fmt.Errorf("worker: run: %w", saveErr))
				}
				failedModels = append(failedModels, queue.FailedModel{
					Name:      pipeline.Name(),
					Error:     saveErr.Error(),
					Timestamp: time.Now(),
				})
			}
			if w.emitter != nil {
				w.emitter.Emit(notifier.NotificationEvent{
					Type:      notifier.EventTweetCategorized,
					Tweet:     streamTweet.Tweet,
					RuleIDs:   nil,
					ModelName: pipeline.Name(),
					Results:   result,
					Timestamp: time.Now(),
				})
			}
		}

		// Step 4.5: match tweet against active tracked entities.
		w.matchEntities(ctx, streamTweet.Tweet, nerEntities)

		// Step 5: push to dead-letter queue if any models failed.
		if len(failedModels) > 0 {
			ft := &queue.FailedTweet{
				Tweet:        streamTweet,
				FailedModels: failedModels,
			}
			if pushErr := w.producer.PushFailed(ctx, ft); pushErr != nil {
				if w.logger != nil {
					w.logger.Error("worker: run: push failed tweet",
						"worker_id", w.id,
						"tweet_id", streamTweet.Tweet.ID,
						"error", fmt.Errorf("worker: run: %w", pushErr))
				}
			}
		}

		if w.active != nil {
			w.active.Add(-1)
		}
	}
}
