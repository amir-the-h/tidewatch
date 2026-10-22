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

	"github.com/amir-the-h/tidewatch/internal/hub"
	"github.com/amir-the-h/tidewatch/internal/inference"
	"github.com/amir-the-h/tidewatch/internal/model"
	"github.com/amir-the-h/tidewatch/internal/notifier"
	"github.com/amir-the-h/tidewatch/internal/queue"
	"github.com/amir-the-h/tidewatch/internal/store"
)

// Manager launches N worker goroutines that pop Tasks from Redis.
type Manager struct {
	totalWorkers  int
	consumer      queue.Consumer
	producer      queue.Producer
	store         store.Store
	cache         *ActionCache
	dispatcher    notifier.EventEmitter
	broadcaster   hub.Broadcaster
	logger        *slog.Logger
	active        atomic.Int32
	ollamaActive  atomic.Int32
	workerActions sync.Map // key: int (worker ID), value: WorkerInfo
	workerCancels sync.Map // key: int (worker ID), value: context.CancelFunc
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
			"total":   s.Total,
			"active":  s.Active,
			"idle":    s.Idle,
			"workers": s.Workers,
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

// Stats returns a snapshot of worker pool activity, including all N worker slots.
// Active workers appear first (ordered by ID), followed by idle workers.
// DurationMS is computed at snapshot time from StartedAt.
func (m *Manager) Stats() PoolStats {
	active := int(m.active.Load())
	idle := m.totalWorkers - active
	if idle < 0 {
		idle = 0
	}

	// Collect active worker info from the sync.Map.
	activeMap := make(map[int]WorkerInfo, active)
	m.workerActions.Range(func(k, v any) bool {
		wi := v.(WorkerInfo)
		wi.DurationMS = time.Since(wi.StartedAt).Milliseconds()
		activeMap[k.(int)] = wi
		return true
	})

	// Fixed ascending ID order regardless of status.
	workers := make([]WorkerInfo, 0, m.totalWorkers)
	for i := 1; i <= m.totalWorkers; i++ {
		if wi, ok := activeMap[i]; ok {
			workers = append(workers, wi)
		} else {
			workers = append(workers, WorkerInfo{ID: i, Status: "idle"})
		}
	}

	return PoolStats{
		Total:   m.totalWorkers,
		Active:  active,
		Idle:    idle,
		Workers: workers,
	}
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

// buildReadyKeys returns the Redis queue keys for all active actions whose
// pipeline is currently acquirable. ONNX, internal, and notification
// pipelines are always included. Ollama pipelines are included only when
// at least one assigned (or pool-wide) backend is healthy and enabled,
// AND the global ollamaActive count is below the pool's total capacity.
func (m *Manager) buildReadyKeys() []string {
	entries := m.cache.All()
	pool := m.cache.Pool()
	var cap int32
	if pool != nil {
		cap = pool.TotalCapacity()
	}
	keys := make([]string, 0, len(entries))
	for _, e := range entries {
		if av, ok := e.Pipeline.(inference.AvailabilityChecker); ok {
			// Ollama pipeline: gated by both backend-level availability and a soft
			// in-flight count check. ollamaActive is a best-effort pre-filter —
			// multiple workers can pass this check simultaneously before any increments,
			// causing transient over-subscription. AcquireForPipeline provides the
			// authoritative serialization point; workers that exceed backend capacity
			// will block there until a slot is released.
			if av.IsAvailable() && m.ollamaActive.Load() < cap {
				keys = append(keys, queue.QueueTasksKey(e.Action.ID))
			}
		} else {
			// No AvailabilityChecker → always available (ONNX, internal, notification).
			keys = append(keys, queue.QueueTasksKey(e.Action.ID))
		}
	}
	return keys
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
			// Block until a backend becomes available or timeout fires, then re-evaluate.
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

		entry := m.cache.Get(task.ActionID)
		if entry == nil {
			if m.logger != nil {
				m.logger.Warn("worker: discarding task for unknown action_id — action inactive or deleted",
					"worker_id", id, "action_id", task.ActionID)
			}
			continue
		}

		// Track Ollama concurrency: increment before execution, decrement after.
		// BackendPool.Release() already calls cond.Broadcast() when the pipeline
		// returns its backend slot, waking workers in WaitForAvailability.
		isOllama := false
		if _, ok := entry.Pipeline.(inference.AvailabilityChecker); ok {
			isOllama = true
			m.ollamaActive.Add(1)
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

		if isOllama {
			m.ollamaActive.Add(-1)
		}
	}
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
		m.processDigestTask(ctx, workerID, task, entry)
		return
	}

	// Unmarshal the signal event before storing WorkerInfo so SignalID is available.
	var ev queue.SignalEvent
	_ = json.Unmarshal(task.Event, &ev)

	startedAt := time.Now()
	info := WorkerInfo{
		ID:        workerID,
		Status:    "active",
		ActionID:  task.ActionID,
		Action:    entry.Action.Name,
		Pipeline:  entry.Pipeline.Name(),
		SignalID:  ev.SignalID,
		StartedAt: startedAt,
	}
	m.workerActions.Store(workerID, info)
	defer m.workerActions.Delete(workerID)

	// Decrement pipeline run tracking when this task is done.
	if task.RootPipelineID != 0 {
		defer func() {
			done, err := m.store.DecrementPipelineRunTracking(ctx, task.RootPipelineID, ev.SignalID)
			if err != nil && m.logger != nil {
				m.logger.Warn("worker: decrement pipeline run tracking",
					"root_pipeline_id", task.RootPipelineID, "signal_id", ev.SignalID, "error", err)
			}
			if done {
				m.firePipelineFinished(ctx, task.RootPipelineID, ev.SignalID)
			}
		}()
	}

	sig, err := m.store.GetSignal(ctx, ev.SignalID)
	if err != nil {
		if m.logger != nil {
			m.logger.Warn("worker: load signal failed — task dropped",
				"worker_id", workerID, "action_id", task.ActionID, "signal_id", ev.SignalID, "error", err)
		}
		return
	}

	allMeta, _ := m.store.MergeAllSignalExecutionMetadata(ctx, ev.SignalID)
	prevMeta, _ := m.store.MergeActionExecutionMetadata(ctx, task.ActionID, ev.SignalID)

	mergedCtx := make(map[string]any, len(task.Context)+len(allMeta)+len(prevMeta)+6)
	for k, v := range allMeta {
		mergedCtx[k] = decodeMetaValue(v)
	}
	delete(mergedCtx, "telegram_message_id")
	for k, v := range prevMeta {
		mergedCtx[k] = decodeMetaValue(v)
	}
	if _, hasOwn := mergedCtx["telegram_message_id"]; !hasOwn {
		if lastID, ok := allMeta["telegram_message_id"]; ok {
			mergedCtx["last_telegram_message_id"] = decodeMetaValue(lastID)
		}
	}
	for k, v := range task.Context {
		mergedCtx[k] = v
	}
	// Signal keys go last so an action's output can't overwrite them.
	for k, v := range signalContext(sig, ev) {
		mergedCtx[k] = v
	}

	enrichContext(mergedCtx)

	input := inference.PipelineInput{
		Text:     signalInputText(sig),
		SignalID: sig.ID,
		Context:  mergedCtx,
	}

	// Inject backend callback so Ollama pipelines can report which backend they acquire.
	inferenceCtx := inference.WithBackendNotify(ctx, func(backend string) {
		m.workerActions.Store(workerID, WorkerInfo{
			ID:          workerID,
			Status:      "active",
			ActionID:    task.ActionID,
			Action:      entry.Action.Name,
			Pipeline:    entry.Pipeline.Name(),
			SignalID:    ev.SignalID,
			StartedAt:   startedAt,
			BackendName: backend,
		})
	})

	if m.logger != nil {
		m.logger.Info("worker: running pipeline",
			"worker_id", workerID,
			"action_id", task.ActionID,
			"action", entry.Action.Name,
			"pipeline", entry.Pipeline.Name(),
			"signal_id", ev.SignalID,
			"pipeline_id", task.PipelineID,
			"pipeline_node_id", task.PipelineNodeID,
		)
	}

	startTime := time.Now()
	outputs, runErr := entry.Pipeline.Run(inferenceCtx, input)

	exec := &model.ActionExecution{
		ActionID:   task.ActionID,
		SignalID:   ev.SignalID,
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
				"signal_id", ev.SignalID,
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
			"signal_id", ev.SignalID,
			"output_keys", outputKeys,
			"pipeline_node_id", task.PipelineNodeID,
		)
	}

	PushContinuationTasks(ctx, m.store, m.producer, m.cache, task, mergedCtx, m.logger)
}

// firePipelineFinished dispatches tasks for all pipelines linked to the
// pipeline_finished trigger for the given source pipeline.
func (m *Manager) firePipelineFinished(ctx context.Context, pipelineID int64, signalID int64) {
	eventPayload, err := m.store.GetPipelineRunEventPayload(ctx, pipelineID, signalID)
	if err != nil || len(eventPayload) == 0 {
		if m.logger != nil {
			m.logger.Warn("worker: pipeline_finished: missing event payload",
				"pipeline_id", pipelineID, "signal_id", signalID)
		}
		return
	}
	var ev queue.SignalEvent
	if err := json.Unmarshal(eventPayload, &ev); err != nil {
		if m.logger != nil {
			m.logger.Warn("worker: pipeline_finished: unmarshal event", "error", err)
		}
		return
	}
	if m.logger != nil {
		m.logger.Info("worker: pipeline finished — firing pipeline_finished trigger",
			"pipeline_id", pipelineID, "signal_id", signalID)
	}
	if err := PushInitialPipelineTasks(ctx, m.store, m.producer, m.cache,
		"pipeline_finished", ev, m.logger, pipelineID); err != nil && m.logger != nil {
		m.logger.Warn("worker: push pipeline_finished tasks failed",
			"pipeline_id", pipelineID, "error", err)
	}
}

func (m *Manager) processDigestTask(ctx context.Context, workerID int, task *queue.Task, entry *CacheEntry) {
	startedAt := time.Now()
	info := WorkerInfo{
		ID:        workerID,
		Status:    "active",
		ActionID:  task.ActionID,
		Action:    entry.Action.Name,
		Pipeline:  entry.Pipeline.Name(),
		StartedAt: startedAt,
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
			ID:          workerID,
			Status:      "active",
			ActionID:    task.ActionID,
			Action:      entry.Action.Name,
			Pipeline:    entry.Pipeline.Name(),
			StartedAt:   startedAt,
			BackendName: backend,
		})
	})

	p := job.Params
	signals, err := m.store.ListSignals(ctx, store.SignalFilter{
		RuleID:   p.RuleID,
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
			m.logger.Error("worker: digest job: list signals", "job_id", jobID, "error", err)
		}
		job.Status = model.DigestStatusError
		job.Error = "failed to fetch signals: " + err.Error()
		_ = m.store.UpdateDigestJob(ctx, job)
		m.emitDigestEvent(job, entry.Pipeline.Name())
		return
	}

	job.SignalCount = len(signals)

	if len(signals) == 0 {
		job.Status = model.DigestStatusDone
		job.Result = "No signals found matching the selected filters."
		_ = m.store.UpdateDigestJob(ctx, job)
		m.emitDigestEvent(job, entry.Pipeline.Name())
		return
	}

	formatted := make([]string, len(signals))
	for i, sg := range signals {
		text := signalInputText(sg)
		if sg.Author != "" {
			formatted[i] = fmt.Sprintf("%s: %s", sg.Author, text)
		} else {
			formatted[i] = text
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

// Flush cancels the context of every worker that is currently executing a task.
// Workers blocked in BRPOP (idle) are unaffected. Returns the count of interrupted tasks.
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

func enrichContext(ctx map[string]any) {
	if _, ok := ctx["top_label"]; !ok {
		if labels, ok := ctx["labels"].([]any); ok && len(labels) > 0 {
			ctx["top_label"] = labels[0]
			if scores, ok := ctx["scores"].([]any); ok && len(scores) > 0 {
				ctx["top_score"] = scores[0]
			}
		}
	}

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
