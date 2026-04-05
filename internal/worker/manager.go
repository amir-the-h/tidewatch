package worker

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/amir-the-h/iran-watcher/internal/hub"
	"github.com/amir-the-h/iran-watcher/internal/inference"
	"github.com/amir-the-h/iran-watcher/internal/model"
	"github.com/amir-the-h/iran-watcher/internal/notifier"
	"github.com/amir-the-h/iran-watcher/internal/queue"
	"github.com/amir-the-h/iran-watcher/internal/store"
)

// Manager launches N worker goroutines that pop Tasks from Redis.
type Manager struct {
	totalWorkers       int
	consumer           queue.Consumer
	producer           queue.Producer
	store              store.Store
	cache              *ActionCache
	dispatcher         notifier.EventEmitter
	broadcaster        hub.Broadcaster
	logger             *slog.Logger
	active             atomic.Int32
	workerActions      sync.Map // key: int (worker ID), value: WorkerInfo
	workerCancels      sync.Map // key: int (worker ID), value: context.CancelFunc
	completionTrackers sync.Map // key: int64 (action_id), value: *rateTracker
}

// WithDispatcher sets an optional event emitter used to fire digest_done/digest_failed events.
func (m *Manager) WithDispatcher(d notifier.EventEmitter) *Manager {
	m.dispatcher = d
	return m
}

// WithBroadcaster sets an optional WebSocket broadcaster for live UI updates.
func (m *Manager) WithBroadcaster(b hub.Broadcaster) *Manager {
	m.broadcaster = b
	return m
}

// broadcastWorkerStats pushes the current pool stats to all connected WS clients.
func (m *Manager) broadcastWorkerStats() {
	if m.broadcaster == nil {
		return
	}
	s := m.Stats()
	m.broadcaster.Broadcast(hub.Event{
		Type: "worker_stats",
		Payload: map[string]any{
			"total":             s.Total,
			"active":            s.Active,
			"idle":              s.Idle,
			"active_workers":    s.ActiveWorkers,
			"action_throughput": s.ActionThroughput,
		},
	})
}

func NewManager(
	total int,
	consumer queue.Consumer,
	producer queue.Producer,
	st store.Store,
	cache *ActionCache,
	logger *slog.Logger,
) *Manager {
	return &Manager{
		totalWorkers: total,
		consumer:     consumer,
		producer:     producer,
		store:        st,
		cache:        cache,
		logger:       logger,
	}
}

// Stats returns a snapshot of worker pool activity.
func (m *Manager) Stats() PoolStats {
	active := int(m.active.Load())
	idle := m.totalWorkers - active
	if idle < 0 {
		idle = 0
	}

	var activeWorkers []WorkerInfo
	m.workerActions.Range(func(_, v any) bool {
		activeWorkers = append(activeWorkers, v.(WorkerInfo))
		return true
	})

	return PoolStats{
		Total:            m.totalWorkers,
		Active:           active,
		Idle:             idle,
		ActiveWorkers:    activeWorkers,
		ActionThroughput: m.throughputPerAction(),
	}
}

// recordCompletion appends a completion timestamp for actionID.
// Called after every task completes (success or error).
func (m *Manager) recordCompletion(actionID int64) {
	v, _ := m.completionTrackers.LoadOrStore(actionID, &rateTracker{})
	v.(*rateTracker).record()
}

// throughputPerAction returns current estimated throughput (tasks/sec) per action ID.
// Actions with fewer than 2 completions or a stale window return 0.
func (m *Manager) throughputPerAction() map[int64]float64 {
	result := make(map[int64]float64)
	m.completionTrackers.Range(func(k, v any) bool {
		result[k.(int64)] = v.(*rateTracker).rate()
		return true
	})
	return result
}

// Start launches N worker goroutines. Blocks until ctx is cancelled.
func (m *Manager) Start(ctx context.Context) error {
	var wg sync.WaitGroup
	for i := range m.totalWorkers {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			m.runWorker(ctx, id+1)
		}(i)
	}
	wg.Wait()
	return nil
}

func (m *Manager) runWorker(ctx context.Context, id int) {
	for {
		select {
		case <-ctx.Done():
			return
		default:
		}

		keys := m.buildReadyKeys()
		if len(keys) == 0 {
			// No active actions, or all Ollama backends are busy.
			// Wait until a backend becomes available or the timeout fires,
			// then re-evaluate. Nil pool guard handles test environments.
			if pool := m.cache.Pool(); pool != nil {
				pool.WaitForAvailability(ctx, 1*time.Second, nil)
			} else {
				select {
				case <-ctx.Done():
					return
				case <-time.After(100 * time.Millisecond):
				}
			}
			continue
		}

		task, err := m.consumer.PopTask(ctx, 1*time.Second, keys...)
		if err != nil {
			if m.logger != nil {
				m.logger.Error("worker: pop task", "worker_id", id, "error", err)
			}
			continue
		}
		if task == nil {
			continue
		}
		func() {
			taskCtx, taskCancel := context.WithCancel(ctx)
			m.workerCancels.Store(id, taskCancel)
			defer func() {
				taskCancel()
				m.workerCancels.Delete(id)
			}()
			m.processTask(taskCtx, id, task)
		}()
	}
}

// buildReadyKeys returns the Redis queue keys for all active actions whose
// pipeline is currently acquirable. ONNX, internal, and notification
// pipelines are always included. Ollama pipelines are included only when
// at least one assigned (or pool-wide) backend is healthy, enabled, and idle.
func (m *Manager) buildReadyKeys() []string {
	entries := m.cache.All()
	keys := make([]string, 0, len(entries))
	for _, e := range entries {
		if av, ok := e.Pipeline.(inference.AvailabilityChecker); ok {
			if av.IsAvailable() {
				keys = append(keys, queue.QueueTasksKey(e.Action.ID))
			}
		} else {
			// No AvailabilityChecker → always available (ONNX, internal, notification).
			keys = append(keys, queue.QueueTasksKey(e.Action.ID))
		}
	}
	return keys
}

func (m *Manager) processTask(ctx context.Context, workerID int, task *queue.Task) {
	m.active.Add(1)
	m.broadcastWorkerStats()
	defer func() {
		m.active.Add(-1)
		m.broadcastWorkerStats()
	}()

	entry := m.cache.Get(task.ActionID)
	if entry == nil {
		if m.logger != nil {
			m.logger.Warn("worker: unknown action_id in task — action not in cache (inactive or missing?)",
				"action_id", task.ActionID,
				"trigger", task.TriggerType,
				"pipeline_id", task.PipelineID,
				"pipeline_node_id", task.PipelineNodeID,
			)
		}
		return
	}

	if task.TriggerType == "digest_requested" {
		// Digest tasks are excluded from the throughput tracker — they are long-running
		// one-offs with a different completion profile and would skew per-action rate estimates.
		m.processDigestTask(ctx, workerID, task, entry)
		return
	}

	// Track which action this worker is executing.
	info := WorkerInfo{
		ID:       workerID,
		Action:   entry.Action.Name,
		Pipeline: entry.Pipeline.Name(),
	}
	m.workerActions.Store(workerID, info)
	defer m.workerActions.Delete(workerID)

	var ev queue.TweetEvent
	_ = json.Unmarshal(task.Event, &ev)

	// Load execution metadata for this tweet across all prior actions so that
	// notification pipelines can reference ONNX/Ollama outputs even when not
	// triggered via a pipeline (e.g. ${label}, ${score}, ${summary}, ${entity_list}).
	allMeta, _ := m.store.MergeAllTweetExecutionMetadata(ctx, ev.TweetID)

	// Load metadata from prior executions of THIS action on the same tweet
	// (e.g. telegram_message_id for edit-vs-send logic). Overrides allMeta on conflict.
	prevMeta, _ := m.store.MergeActionExecutionMetadata(ctx, task.ActionID, ev.TweetID)

	mergedCtx := make(map[string]any, len(task.Context)+len(allMeta)+len(prevMeta)+6)
	// Inject in ascending priority order: all-tweet meta → this-action meta → pipeline context → well-known fields.
	for k, v := range allMeta {
		mergedCtx[k] = decodeMetaValue(v)
	}
	// telegram_message_id must be scoped to the same action to prevent different
	// notification actions from editing each other's messages. Remove it from
	// allMeta's contribution; prevMeta (action-scoped) will supply the correct value.
	delete(mergedCtx, "telegram_message_id")
	for k, v := range prevMeta {
		mergedCtx[k] = decodeMetaValue(v)
	}
	// If this action has no prior telegram_message_id of its own (first send),
	// expose the last known notification message ID as last_telegram_message_id
	// so the notification pipeline can reply to it and form a Telegram reply thread.
	if _, hasOwn := mergedCtx["telegram_message_id"]; !hasOwn {
		if lastID, ok := allMeta["telegram_message_id"]; ok {
			mergedCtx["last_telegram_message_id"] = decodeMetaValue(lastID)
		}
	}
	for k, v := range task.Context {
		mergedCtx[k] = v
	}
	mergedCtx["tweet_id"] = ev.TweetID
	mergedCtx["tweet_author"] = ev.AuthorHandle
	mergedCtx["author"] = ev.AuthorHandle
	mergedCtx["tweet"] = ev.Text
	mergedCtx["tweet_short"] = truncateText(ev.Text, 280)
	mergedCtx["tweet_url"] = "https://x.com/" + ev.AuthorHandle + "/status/" + ev.TweetID
	mergedCtx["rule_ids"] = ev.RuleIDs // []int64 — used by rule_matches condition operator

	// Derive convenience vars from common ONNX/Ollama output shapes.
	enrichContext(mergedCtx)

	input := inference.PipelineInput{
		Text:    ev.Text,
		TweetID: ev.TweetID,
		Context: mergedCtx,
	}

	// Inject backend callback so Ollama pipelines can report which backend they acquire.
	inferenceCtx := inference.WithBackendNotify(ctx, func(backend string) {
		m.workerActions.Store(workerID, WorkerInfo{
			ID:       workerID,
			Action:   entry.Action.Name,
			Pipeline: entry.Pipeline.Name(),
			Backend:  backend,
		})
	})

	if m.logger != nil {
		m.logger.Info("worker: running pipeline",
			"worker_id", workerID,
			"action_id", task.ActionID,
			"action", entry.Action.Name,
			"pipeline", entry.Pipeline.Name(),
			"tweet_id", ev.TweetID,
			"pipeline_id", task.PipelineID,
			"pipeline_node_id", task.PipelineNodeID,
		)
	}

	startTime := time.Now()
	outputs, runErr := entry.Pipeline.Run(inferenceCtx, input)

	exec := &model.ActionExecution{
		ActionID:   task.ActionID,
		TweetID:    ev.TweetID,
		Status:     model.ExecutionStatusSuccess,
		ExecutedAt: startTime,
	}
	if runErr != nil {
		exec.Status = model.ExecutionStatusError
		exec.Error = runErr.Error()
		if m.logger != nil {
			m.logger.Warn("worker: pipeline run failed",
				"worker_id", workerID, "action_id", task.ActionID,
				"pipeline", entry.Pipeline.Name(), "error", runErr)
		}
	}
	if saveErr := m.store.SaveActionExecution(ctx, exec); saveErr != nil {
		if m.logger != nil {
			m.logger.Warn("worker: save action execution", "error", saveErr)
		}
	} else if m.broadcaster != nil {
		m.broadcaster.Broadcast(hub.Event{
			Type:    "action_execution",
			Payload: exec,
		})
	}

	m.recordCompletion(task.ActionID)

	if runErr != nil {
		return
	}
	if outputs == nil {
		if m.logger != nil {
			m.logger.Warn("worker: pipeline returned nil outputs — pipeline continuation skipped",
				"worker_id", workerID,
				"action_id", task.ActionID,
				"action", entry.Action.Name,
				"pipeline", entry.Pipeline.Name(),
				"tweet_id", ev.TweetID,
				"pipeline_node_id", task.PipelineNodeID,
			)
		}
		return
	}

	for _, t := range entry.Transformers {
		transformed, tErr := t.Apply(outputs)
		if tErr != nil {
			if m.logger != nil {
				m.logger.Warn("worker: transformer failed, using untransformed output",
					"worker_id", workerID, "action_id", task.ActionID, "error", tErr)
			}
			break
		}
		outputs = transformed
	}

	for k, v := range outputs {
		val := fmt.Sprintf("%v", v)
		if b, err := json.Marshal(v); err == nil {
			val = string(b)
		}
		_ = m.store.SaveActionExecutionMetadata(ctx, exec.ID, k, val)
	}

	outputKeys := make([]string, 0, len(outputs))
	for k, v := range outputs {
		mergedCtx[k] = v
		outputKeys = append(outputKeys, fmt.Sprintf("%s=%v", k, v))
	}

	if m.logger != nil {
		m.logger.Info("worker: pipeline completed",
			"worker_id", workerID,
			"action_id", task.ActionID,
			"action", entry.Action.Name,
			"tweet_id", ev.TweetID,
			"output_keys", outputKeys,
			"pipeline_node_id", task.PipelineNodeID,
		)
	}

	// Continue pipeline: push tasks for downstream action nodes whose conditions pass.
	PushContinuationTasks(ctx, m.store, m.producer, m.cache, task, mergedCtx, m.logger)
}

func (m *Manager) processDigestTask(ctx context.Context, workerID int, task *queue.Task, entry *CacheEntry) {
	info := WorkerInfo{
		ID:       workerID,
		Action:   entry.Action.Name,
		Pipeline: entry.Pipeline.Name(),
	}
	m.workerActions.Store(workerID, info)
	defer m.workerActions.Delete(workerID)

	dp, ok := entry.Pipeline.(inference.DigestPipeline)
	if !ok {
		if m.logger != nil {
			m.logger.Warn("worker: digest action pipeline does not implement DigestPipeline",
				"worker_id", workerID, "action_id", task.ActionID, "pipeline", entry.Pipeline.Name())
		}
		return
	}

	jobID, _ := task.Context["digest_job_id"].(string)
	if jobID == "" {
		var ev queue.DigestEvent
		_ = json.Unmarshal(task.Event, &ev)
		jobID = ev.DigestJobID
	}
	if jobID == "" {
		if m.logger != nil {
			m.logger.Warn("worker: digest task missing digest_job_id", "worker_id", workerID, "action_id", task.ActionID)
		}
		return
	}

	job, err := m.store.GetDigestJob(ctx, jobID)
	if err != nil {
		if m.logger != nil {
			m.logger.Warn("worker: digest job not found", "job_id", jobID, "error", err)
		}
		return
	}

	job.Status = model.DigestStatusRunning
	if err := m.store.UpdateDigestJob(ctx, job); err != nil && m.logger != nil {
		m.logger.Warn("worker: digest job mark running", "job_id", jobID, "error", err)
	}

	inferenceCtx := inference.WithBackendNotify(ctx, func(backend string) {
		m.workerActions.Store(workerID, WorkerInfo{
			ID:       workerID,
			Action:   entry.Action.Name,
			Pipeline: entry.Pipeline.Name(),
			Backend:  backend,
		})
	})

	p := job.Params
	tweets, err := m.store.ListTweets(ctx, store.TweetFilter{
		RuleID:   p.RuleID,
		SourceID: p.SourceID,
		EntityID: p.EntityID,
		From:     p.From,
		To:       p.To,
		Page:     1,
		PerPage:  p.Limit,
	})

	now := time.Now().UTC()
	job.CompletedAt = &now

	if err != nil {
		if m.logger != nil {
			m.logger.Error("worker: digest job: list tweets", "job_id", jobID, "error", err)
		}
		job.Status = model.DigestStatusError
		job.Error = "failed to fetch tweets: " + err.Error()
		_ = m.store.UpdateDigestJob(ctx, job)
		m.emitDigestEvent(job, entry.Pipeline.Name())
		return
	}

	job.TweetCount = len(tweets)

	if len(tweets) == 0 {
		job.Status = model.DigestStatusDone
		job.Result = "No tweets found matching the selected filters."
		_ = m.store.UpdateDigestJob(ctx, job)
		m.emitDigestEvent(job, entry.Pipeline.Name())
		return
	}

	formatted := make([]string, len(tweets))
	for i, t := range tweets {
		if t.AuthorHandle != "" {
			formatted[i] = fmt.Sprintf("@%s: %s", t.AuthorHandle, t.Text)
		} else {
			formatted[i] = t.Text
		}
	}

	digest, err := dp.Digest(inferenceCtx, formatted)
	if err != nil {
		if m.logger != nil {
			m.logger.Error("worker: digest job: run", "job_id", jobID, "action_id", task.ActionID, "error", err)
		}
		job.Status = model.DigestStatusError
		job.Error = "digest generation failed: " + err.Error()
		_ = m.store.UpdateDigestJob(ctx, job)
		m.emitDigestEvent(job, entry.Pipeline.Name())
		return
	}

	job.Status = model.DigestStatusDone
	job.Result = digest
	if err := m.store.UpdateDigestJob(ctx, job); err != nil && m.logger != nil {
		m.logger.Error("worker: digest job: save result", "job_id", jobID, "error", err)
	}
	m.emitDigestEvent(job, entry.Pipeline.Name())
}

func (m *Manager) emitDigestEvent(job *model.DigestJob, pipelineName string) {
	if m.dispatcher == nil {
		return
	}
	eventType := notifier.EventDigestDone
	if job.Status == model.DigestStatusError {
		eventType = notifier.EventDigestFailed
	}
	m.dispatcher.Emit(notifier.NotificationEvent{
		Type:      eventType,
		DigestJob: job,
		ModelName: pipelineName,
		Error:     job.Error,
		Timestamp: time.Now().UTC(),
	})
}

// Flush cancels the context of every worker that is currently executing a
// task. Workers blocked in BRPOP (idle) are unaffected. Returns the number
// of in-flight tasks that were cancelled.
func (m *Manager) Flush() int {
	n := 0
	m.workerCancels.Range(func(_, v any) bool {
		if cancel, ok := v.(context.CancelFunc); ok {
			cancel()
		}
		n++
		return true
	})
	return n
}

func (m *Manager) FlushPipeline(_ string) int { return 0 }

// decodeMetaValue JSON-decodes a DB-stored metadata value so it can be used
// cleanly in template substitution. String values lose their JSON quotes;
// numbers become float64; arrays and objects become []interface{}/map[string]any.
// Falls back to the raw string if the value is not valid JSON.
func truncateText(s string, max int) string {
	runes := []rune(s)
	if len(runes) <= max {
		return s
	}
	return string(runes[:max]) + "…"
}

func decodeMetaValue(s string) any {
	var v any
	if err := json.Unmarshal([]byte(s), &v); err != nil {
		return s
	}
	return v
}

// enrichContext derives convenience template vars from common ONNX/Ollama output
// shapes. It never overwrites a key that is already present in ctx.
func enrichContext(ctx map[string]any) {
	// Zero-shot classification: {"labels": [...], "scores": [...]}
	// → top_label (string), top_score (float64)
	if _, ok := ctx["top_label"]; !ok {
		if labels, ok := ctx["labels"].([]any); ok && len(labels) > 0 {
			ctx["top_label"] = labels[0]
			if scores, ok := ctx["scores"].([]any); ok && len(scores) > 0 {
				ctx["top_score"] = scores[0]
			}
		}
	}

	// NER: {"entities": [{"word": "...", "label": "...", "score": ...}, ...]}
	// → entity_list: "word (LABEL), ..." human-readable summary
	if _, ok := ctx["entity_list"]; !ok {
		if entities, ok := ctx["entities"].([]any); ok && len(entities) > 0 {
			var parts []string
			for _, e := range entities {
				em, ok := e.(map[string]any)
				if !ok {
					continue
				}
				word, _ := em["word"].(string)
				label, _ := em["label"].(string)
				if word == "" {
					continue
				}
				if label != "" {
					parts = append(parts, word+" ("+label+")")
				} else {
					parts = append(parts, word)
				}
			}
			if len(parts) > 0 {
				ctx["entity_list"] = strings.Join(parts, ", ")
			}
		}
	}
}
