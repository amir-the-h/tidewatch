# Pipeline Finished Trigger Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add a `pipeline_finished` trigger that fires when all branches of a pipeline complete for a tweet, enabling cross-pipeline orchestration (e.g. run analytics in parallel, then notify when all done).

**Architecture:** Reference counting via a new `pipeline_run_tracking` SQLite table. A `RootPipelineID` field on `queue.Task` propagates through all sub-tasks. `PushTask` increments the counter; a deferred decrement in `processTask` fires after `PushContinuationTasks` (ensuring K increments happen before the -1 decrement). When the counter hits 0, `firePipelineFinished` dispatches to any pipelines linked via `pipeline_finished` + `source_pipeline_id`.

**Tech Stack:** Go (net/http, database/sql, modernc.org/sqlite), Vue 3 + Naive UI, TypeScript

---

## File Map

| File | Change |
|---|---|
| `migrations/026_pipeline_finished_trigger.sql` | NEW — `pipeline_run_tracking` table + `source_pipeline_id` column on `trigger_pipeline_links` |
| `internal/queue/queue.go` | Add `RootPipelineID int64` to `Task` |
| `internal/model/pipeline.go` | Add `SourcePipelineID *int64` to `TriggerPipelineLink` |
| `internal/store/store.go` | Add 4 new Store methods; update `ListActiveTriggerPipelineLinks` signature |
| `internal/store/sqlite_actions.go` | Implement `InitPipelineRunTracking`, `IncrementPipelineRunTracking`, `DecrementPipelineRunTracking`, `GetPipelineRunEventPayload` |
| `internal/store/sqlite_pipelines.go` | Update `SaveTriggerPipelineLink`, `ListTriggerPipelineLinks`, `ListActiveTriggerPipelineLinks`, `scanTriggerPipelineLink` |
| `internal/store/sqlite_actions_test.go` | Tests for `DecrementPipelineRunTracking` |
| `internal/worker/pipeline.go` | Add `pushTrackedTask` helper; update `PushInitialTasksForGraph` to seed counter + set `RootPipelineID`; update `PushInitialPipelineTasks` to accept `sourcePipelineID`; replace all `producer.PushTask` calls with `pushTrackedTask` |
| `internal/worker/manager.go` | Defer decrement in `processTask`; add `firePipelineFinished` method |
| `internal/stream/client.go` | Update `PushInitialPipelineTasks` call (add `0` arg) |
| `internal/search/client.go` | Update `PushInitialPipelineTasks` call (add `0` arg) |
| `internal/api/handlers_tweets.go` | Update `PushInitialPipelineTasks` call (add `0` arg) |
| `internal/api/handlers_triggers.go` | Add `pipeline_finished` to trigger list; accept `source_pipeline_id` in create |
| `web/src/api/trigger_links.ts` | Add `source_pipeline_id` to `TriggerPipelineLink` type and create body |
| `web/src/views/PipelinesView.vue` | Show "Source Pipeline" picker when trigger type is `pipeline_finished` |
| `web/src/views/PipelineBuilderView.vue` | Add `exists` / `not_exists` to condition operator options; hide value input for these ops |
| `CLAUDE.md` | Document `pipeline_finished` trigger and `pipeline_run_tracking` table |

---

### Task 1: Database Migration

**Files:**
- Create: `migrations/026_pipeline_finished_trigger.sql`

- [ ] **Write the migration**

```sql
-- migrations/026_pipeline_finished_trigger.sql

-- Track active task count per (pipeline, tweet) for pipeline_finished detection.
CREATE TABLE pipeline_run_tracking (
    pipeline_id   INTEGER NOT NULL,
    tweet_id      TEXT    NOT NULL,
    active_tasks  INTEGER NOT NULL DEFAULT 0,
    event_payload BLOB,
    created_at    DATETIME NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%SZ','now')),
    completed_at  DATETIME,
    PRIMARY KEY (pipeline_id, tweet_id)
);

-- Allow trigger_pipeline_links for pipeline_finished to scope to a source pipeline.
ALTER TABLE trigger_pipeline_links
    ADD COLUMN source_pipeline_id INTEGER REFERENCES pipelines(id) ON DELETE CASCADE;
```

- [ ] **Verify the migration file exists and has correct SQL**

```bash
cat /mnt/shared/projects/personal/iran-watcher/migrations/026_pipeline_finished_trigger.sql
```

Expected: the two SQL statements above, no syntax errors.

- [ ] **Commit**

```bash
git add migrations/026_pipeline_finished_trigger.sql
git commit -m "feat(db): add pipeline_run_tracking table and source_pipeline_id to trigger links"
```

---

### Task 2: Extend queue.Task and TriggerPipelineLink models

**Files:**
- Modify: `internal/queue/queue.go`
- Modify: `internal/model/pipeline.go`

- [ ] **Add `RootPipelineID` to `queue.Task`**

In `internal/queue/queue.go`, add the field after `PipelineID`:

```go
// Task is a single unit of work pushed to iran-watcher:tasks:{action_id}.
type Task struct {
	TriggerType          string          `json:"trigger_type"`
	Event                json.RawMessage `json:"event"`
	ActionID             int64           `json:"action_id"`
	PipelineNodeID       int64           `json:"pipeline_node_id,omitempty"`
	PipelineID           int64           `json:"pipeline_id,omitempty"`
	// RootPipelineID is the top-level pipeline that originally dispatched this
	// task (set at PushInitialTasksForGraph time, propagated through all sub-tasks).
	// Non-zero tasks contribute to pipeline_run_tracking for pipeline_finished detection.
	RootPipelineID         int64          `json:"root_pipeline_id,omitempty"`
	ParentPipelineNodeID   *int64         `json:"parent_pipeline_node_id,omitempty"`
	SubPipelineEntryNodeID *int64         `json:"sub_pipeline_entry_node_id,omitempty"`
	Context              map[string]any  `json:"context"`
}
```

- [ ] **Add `SourcePipelineID` to `TriggerPipelineLink`**

In `internal/model/pipeline.go`, update `TriggerPipelineLink`:

```go
// TriggerPipelineLink connects a system trigger type to a pipeline.
type TriggerPipelineLink struct {
	ID               int64     `json:"id"`
	TriggerType      string    `json:"trigger_type"`
	PipelineID       int64     `json:"pipeline_id"`
	// SourcePipelineID scopes pipeline_finished links to a specific source pipeline.
	// Nil for all other trigger types.
	SourcePipelineID *int64    `json:"source_pipeline_id,omitempty"`
	Active           bool      `json:"active"`
	CreatedAt        time.Time `json:"created_at"`
}
```

- [ ] **Verify compilation**

```bash
cd /mnt/shared/projects/personal/iran-watcher && go build ./internal/queue/... ./internal/model/...
```

Expected: no errors.

- [ ] **Commit**

```bash
git add internal/queue/queue.go internal/model/pipeline.go
git commit -m "feat: add RootPipelineID to Task and SourcePipelineID to TriggerPipelineLink"
```

---

### Task 3: Store interface — add pipeline run tracking methods

**Files:**
- Modify: `internal/store/store.go`

- [ ] **Update `Store` interface**

In `internal/store/store.go`, update the `ListActiveTriggerPipelineLinks` signature and add four new methods after `MarkPipelineNodeLeafComplete`:

```go
// TriggerPipelineLinks (replaces TriggerChainLinks)
SaveTriggerPipelineLink(ctx context.Context, l *model.TriggerPipelineLink) error
ListTriggerPipelineLinks(ctx context.Context) ([]*model.TriggerPipelineLink, error)
// ListActiveTriggerPipelineLinks returns active links for triggerType.
// sourcePipelineID=0 matches links with no source_pipeline_id (all non-pipeline_finished triggers).
// sourcePipelineID>0 matches links whose source_pipeline_id equals that value (pipeline_finished).
ListActiveTriggerPipelineLinks(ctx context.Context, triggerType string, sourcePipelineID int64) ([]*model.TriggerPipelineLink, error)
DeleteTriggerPipelineLink(ctx context.Context, id int64) error
BulkDeleteTriggerPipelineLinks(ctx context.Context, f BulkDeleteTriggerPipelineLinksFilter) (int64, error)
SetTriggerPipelineLinkActive(ctx context.Context, id int64, active bool) error

// Pipeline node execution tracking (for sub-pipeline fan-in)
InsertPipelineNodeExecution(ctx context.Context, parentNodeID int64, tweetID string, leafNodeID int64) error
MarkPipelineNodeLeafComplete(ctx context.Context, parentNodeID int64, tweetID string, leafNodeID int64) (allDone bool, err error)

// Pipeline run tracking (for pipeline_finished trigger)
// InitPipelineRunTracking seeds a tracking row for (pipelineID, tweetID) with initialCount
// active tasks. Upserts — resets an existing row if the pipeline is re-run.
InitPipelineRunTracking(ctx context.Context, pipelineID int64, tweetID string, initialCount int, eventPayload []byte) error
// IncrementPipelineRunTracking atomically adds 1 to active_tasks for (pipelineID, tweetID).
// No-op if no tracking row exists (untracked pipelines, e.g. backfill start-from-node runs).
IncrementPipelineRunTracking(ctx context.Context, pipelineID int64, tweetID string) error
// DecrementPipelineRunTracking atomically subtracts 1 from active_tasks.
// Returns done=true when active_tasks reaches 0. Returns done=false with no error
// if no tracking row exists (untracked pipelines). Sets completed_at when done.
DecrementPipelineRunTracking(ctx context.Context, pipelineID int64, tweetID string) (done bool, err error)
// GetPipelineRunEventPayload returns the original TweetEvent JSON stored at tracking time.
GetPipelineRunEventPayload(ctx context.Context, pipelineID int64, tweetID string) ([]byte, error)
```

- [ ] **Verify compilation (will fail until implementations added — that's expected)**

```bash
cd /mnt/shared/projects/personal/iran-watcher && go build ./internal/store/... 2>&1 | head -20
```

Expected: errors about missing methods on `sqliteStore` — this is correct, they get added in the next task.

- [ ] **Commit**

```bash
git add internal/store/store.go
git commit -m "feat(store): add pipeline run tracking methods to Store interface"
```

---

### Task 4: SQLite — implement pipeline run tracking

**Files:**
- Modify: `internal/store/sqlite_actions.go`

- [ ] **Add the four new methods at the bottom of `sqlite_actions.go`, after `MarkPipelineNodeLeafComplete`**

```go
// ── Pipeline Run Tracking (pipeline_finished trigger) ─────────────────────────

func (s *sqliteStore) InitPipelineRunTracking(ctx context.Context, pipelineID int64, tweetID string, initialCount int, eventPayload []byte) error {
	now := time.Now().UTC().Format(time.RFC3339)
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO pipeline_run_tracking (pipeline_id, tweet_id, active_tasks, event_payload, created_at, completed_at)
		VALUES (?, ?, ?, ?, ?, NULL)
		ON CONFLICT (pipeline_id, tweet_id) DO UPDATE SET
		    active_tasks  = excluded.active_tasks,
		    event_payload = excluded.event_payload,
		    completed_at  = NULL,
		    created_at    = excluded.created_at`,
		pipelineID, tweetID, initialCount, eventPayload, now,
	)
	if err != nil {
		return fmt.Errorf("store: init pipeline run tracking: %w", err)
	}
	return nil
}

func (s *sqliteStore) IncrementPipelineRunTracking(ctx context.Context, pipelineID int64, tweetID string) error {
	_, err := s.db.ExecContext(ctx,
		`UPDATE pipeline_run_tracking SET active_tasks = active_tasks + 1
		 WHERE pipeline_id = ? AND tweet_id = ?`,
		pipelineID, tweetID,
	)
	if err != nil {
		return fmt.Errorf("store: increment pipeline run tracking: %w", err)
	}
	return nil
}

func (s *sqliteStore) DecrementPipelineRunTracking(ctx context.Context, pipelineID int64, tweetID string) (bool, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return false, fmt.Errorf("store: decrement pipeline run tracking: begin tx: %w", err)
	}
	defer tx.Rollback() //nolint:errcheck

	res, err := tx.ExecContext(ctx,
		`UPDATE pipeline_run_tracking SET active_tasks = active_tasks - 1
		 WHERE pipeline_id = ? AND tweet_id = ? AND active_tasks > 0`,
		pipelineID, tweetID,
	)
	if err != nil {
		return false, fmt.Errorf("store: decrement pipeline run tracking: update: %w", err)
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		// No tracking row, or already at 0 — untracked pipeline or re-entry guard.
		_ = tx.Commit()
		return false, nil
	}

	var remaining int
	if err := tx.QueryRowContext(ctx,
		`SELECT active_tasks FROM pipeline_run_tracking WHERE pipeline_id = ? AND tweet_id = ?`,
		pipelineID, tweetID,
	).Scan(&remaining); err != nil {
		return false, fmt.Errorf("store: decrement pipeline run tracking: query: %w", err)
	}

	if remaining == 0 {
		now := time.Now().UTC().Format(time.RFC3339)
		_, _ = tx.ExecContext(ctx,
			`UPDATE pipeline_run_tracking SET completed_at = ? WHERE pipeline_id = ? AND tweet_id = ?`,
			now, pipelineID, tweetID,
		)
	}

	if err := tx.Commit(); err != nil {
		return false, fmt.Errorf("store: decrement pipeline run tracking: commit: %w", err)
	}
	return remaining == 0, nil
}

func (s *sqliteStore) GetPipelineRunEventPayload(ctx context.Context, pipelineID int64, tweetID string) ([]byte, error) {
	var payload []byte
	err := s.db.QueryRowContext(ctx,
		`SELECT event_payload FROM pipeline_run_tracking WHERE pipeline_id = ? AND tweet_id = ?`,
		pipelineID, tweetID,
	).Scan(&payload)
	if err != nil {
		return nil, fmt.Errorf("store: get pipeline run event payload: %w", err)
	}
	return payload, nil
}
```

- [ ] **Verify compilation**

```bash
cd /mnt/shared/projects/personal/iran-watcher && go build ./internal/store/...
```

Expected: no errors. (The interface is now satisfied.)

- [ ] **Commit**

```bash
git add internal/store/sqlite_actions.go
git commit -m "feat(store): implement pipeline run tracking methods in SQLite"
```

---

### Task 5: Store tests — pipeline run tracking

**Files:**
- Modify: `internal/store/sqlite_actions_test.go`

- [ ] **Write tests for `DecrementPipelineRunTracking`** (append after existing tests)

```go
func TestPipelineRunTracking(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)

	pipelineID := int64(1)
	tweetID := "tweet-track-1"
	payload := []byte(`{"tweet_id":"tweet-track-1","text":"hello"}`)

	// Decrement on non-existent row returns done=false, no error
	done, err := s.DecrementPipelineRunTracking(ctx, pipelineID, tweetID)
	if err != nil {
		t.Fatalf("DecrementPipelineRunTracking (no row): %v", err)
	}
	if done {
		t.Error("expected done=false for non-existent row")
	}

	// Init with 2 tasks
	if err := s.InitPipelineRunTracking(ctx, pipelineID, tweetID, 2, payload); err != nil {
		t.Fatalf("InitPipelineRunTracking: %v", err)
	}

	// Increment adds 1 (simulates a downstream push): 2→3
	if err := s.IncrementPipelineRunTracking(ctx, pipelineID, tweetID); err != nil {
		t.Fatalf("IncrementPipelineRunTracking: %v", err)
	}

	// Decrement 1: 3→2, not done
	done, err = s.DecrementPipelineRunTracking(ctx, pipelineID, tweetID)
	if err != nil {
		t.Fatalf("DecrementPipelineRunTracking (3→2): %v", err)
	}
	if done {
		t.Error("expected done=false at 2")
	}

	// Decrement 2: 2→1, not done
	done, err = s.DecrementPipelineRunTracking(ctx, pipelineID, tweetID)
	if err != nil {
		t.Fatalf("DecrementPipelineRunTracking (2→1): %v", err)
	}
	if done {
		t.Error("expected done=false at 1")
	}

	// Decrement 3: 1→0, done
	done, err = s.DecrementPipelineRunTracking(ctx, pipelineID, tweetID)
	if err != nil {
		t.Fatalf("DecrementPipelineRunTracking (1→0): %v", err)
	}
	if !done {
		t.Error("expected done=true at 0")
	}

	// GetPipelineRunEventPayload returns the stored payload
	got, err := s.GetPipelineRunEventPayload(ctx, pipelineID, tweetID)
	if err != nil {
		t.Fatalf("GetPipelineRunEventPayload: %v", err)
	}
	if string(got) != string(payload) {
		t.Errorf("payload mismatch: got %q, want %q", got, payload)
	}

	// Decrement again at 0 — no-op, no error, done=false (re-entry guard)
	done, err = s.DecrementPipelineRunTracking(ctx, pipelineID, tweetID)
	if err != nil {
		t.Fatalf("DecrementPipelineRunTracking (re-entry): %v", err)
	}
	if done {
		t.Error("expected done=false on re-entry decrement at 0")
	}
}
```

- [ ] **Run the tests**

```bash
cd /mnt/shared/projects/personal/iran-watcher && go test ./internal/store/... -run TestPipelineRunTracking -v
```

Expected: `PASS`.

- [ ] **Commit**

```bash
git add internal/store/sqlite_actions_test.go
git commit -m "test(store): pipeline run tracking decrement and re-entry guard"
```

---

### Task 6: SQLite — update TriggerPipelineLink CRUD for source_pipeline_id

**Files:**
- Modify: `internal/store/sqlite_pipelines.go`

- [ ] **Update `SaveTriggerPipelineLink`** — include `source_pipeline_id` in the INSERT

Replace the existing `SaveTriggerPipelineLink` implementation:

```go
func (s *sqliteStore) SaveTriggerPipelineLink(ctx context.Context, l *model.TriggerPipelineLink) error {
	now := time.Now().UTC().Format(time.RFC3339)
	var sourcePipelineID *int64
	if l.SourcePipelineID != nil && *l.SourcePipelineID != 0 {
		sourcePipelineID = l.SourcePipelineID
	}
	res, err := s.db.ExecContext(ctx,
		`INSERT INTO trigger_pipeline_links (trigger_type, pipeline_id, source_pipeline_id, active, created_at) VALUES (?,?,?,?,?)`,
		l.TriggerType, l.PipelineID, sourcePipelineID, boolToInt(l.Active), now,
	)
	if err != nil {
		return fmt.Errorf("store: save trigger pipeline link: %w", err)
	}
	id, _ := res.LastInsertId()
	l.ID = id
	return nil
}
```

- [ ] **Update `ListTriggerPipelineLinks`** — SELECT includes `source_pipeline_id`

Replace the existing `ListTriggerPipelineLinks` implementation:

```go
func (s *sqliteStore) ListTriggerPipelineLinks(ctx context.Context) ([]*model.TriggerPipelineLink, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, trigger_type, pipeline_id, active, created_at, source_pipeline_id
		 FROM trigger_pipeline_links ORDER BY id ASC`)
	if err != nil {
		return nil, fmt.Errorf("store: list trigger pipeline links: %w", err)
	}
	defer rows.Close()
	var out []*model.TriggerPipelineLink
	for rows.Next() {
		l, err := scanTriggerPipelineLink(rows)
		if err != nil {
			return nil, fmt.Errorf("store: list trigger pipeline links: scan: %w", err)
		}
		out = append(out, l)
	}
	return out, rows.Err()
}
```

- [ ] **Update `ListActiveTriggerPipelineLinks`** — new signature + source_pipeline_id filter

Replace the existing `ListActiveTriggerPipelineLinks` implementation:

```go
func (s *sqliteStore) ListActiveTriggerPipelineLinks(ctx context.Context, triggerType string, sourcePipelineID int64) ([]*model.TriggerPipelineLink, error) {
	var (
		rows *sql.Rows
		err  error
	)
	if sourcePipelineID == 0 {
		// Normal triggers: only links without a source_pipeline_id.
		rows, err = s.db.QueryContext(ctx,
			`SELECT id, trigger_type, pipeline_id, active, created_at, source_pipeline_id
			 FROM trigger_pipeline_links
			 WHERE trigger_type = ? AND active = 1
			   AND (source_pipeline_id IS NULL OR source_pipeline_id = 0)
			 ORDER BY id ASC`, triggerType)
	} else {
		// pipeline_finished: only links scoped to this specific source pipeline.
		rows, err = s.db.QueryContext(ctx,
			`SELECT id, trigger_type, pipeline_id, active, created_at, source_pipeline_id
			 FROM trigger_pipeline_links
			 WHERE trigger_type = ? AND active = 1 AND source_pipeline_id = ?
			 ORDER BY id ASC`, triggerType, sourcePipelineID)
	}
	if err != nil {
		return nil, fmt.Errorf("store: list active trigger pipeline links: %w", err)
	}
	defer rows.Close()
	var out []*model.TriggerPipelineLink
	for rows.Next() {
		l, err := scanTriggerPipelineLink(rows)
		if err != nil {
			return nil, fmt.Errorf("store: list active trigger pipeline links: scan: %w", err)
		}
		out = append(out, l)
	}
	return out, rows.Err()
}
```

- [ ] **Update `scanTriggerPipelineLink`** — scan `source_pipeline_id`

Replace the existing `scanTriggerPipelineLink` function:

```go
func scanTriggerPipelineLink(r pipelineScanner) (*model.TriggerPipelineLink, error) {
	l := &model.TriggerPipelineLink{}
	var activeInt int
	var createdAt string
	var sourcePipelineID sql.NullInt64
	if err := r.Scan(&l.ID, &l.TriggerType, &l.PipelineID, &activeInt, &createdAt, &sourcePipelineID); err != nil {
		return nil, err
	}
	l.Active = activeInt != 0
	if sourcePipelineID.Valid && sourcePipelineID.Int64 != 0 {
		l.SourcePipelineID = &sourcePipelineID.Int64
	}
	if t, err := time.Parse(time.RFC3339, createdAt); err == nil {
		l.CreatedAt = t
	}
	return l, nil
}
```

- [ ] **Run existing store tests to confirm nothing is broken**

```bash
cd /mnt/shared/projects/personal/iran-watcher && go test ./internal/store/... -v 2>&1 | tail -20
```

Expected: all tests PASS.

- [ ] **Commit**

```bash
git add internal/store/sqlite_pipelines.go
git commit -m "feat(store): update trigger_pipeline_links CRUD to support source_pipeline_id"
```

---

### Task 7: pipeline.go — pushTrackedTask, RootPipelineID propagation, counter seeding

**Files:**
- Modify: `internal/worker/pipeline.go`

- [ ] **Add `pushTrackedTask` helper** — add this function near the bottom of `pipeline.go`, before the graph helpers section:

```go
// pushTrackedTask pushes a task to the queue and, if the task has a RootPipelineID,
// increments the pipeline run tracking counter. Returns true on success.
func pushTrackedTask(
	ctx context.Context,
	st store.Store,
	producer queue.Producer,
	task *queue.Task,
	logger *slog.Logger,
) bool {
	if err := producer.PushTask(ctx, task); err != nil {
		if logger != nil {
			logger.Warn("pipeline: push task failed",
				"action_id", task.ActionID, "node_id", task.PipelineNodeID, "error", err)
		}
		return false
	}
	if task.RootPipelineID != 0 {
		tweetID := extractTweetID(task)
		if err := st.IncrementPipelineRunTracking(ctx, task.RootPipelineID, tweetID); err != nil && logger != nil {
			logger.Warn("pipeline: increment run tracking failed",
				"root_pipeline_id", task.RootPipelineID, "tweet_id", tweetID, "error", err)
		}
	}
	return true
}
```

- [ ] **Update `PushInitialPipelineTasks` signature** — add `sourcePipelineID int64` parameter and pass it to `ListActiveTriggerPipelineLinks`:

```go
func PushInitialPipelineTasks(
	ctx context.Context,
	st store.Store,
	producer queue.Producer,
	cache *ActionCache,
	triggerType string,
	ev queue.TweetEvent,
	logger *slog.Logger,
	sourcePipelineID int64,
) error {
	links, err := st.ListActiveTriggerPipelineLinks(ctx, triggerType, sourcePipelineID)
	if err != nil {
		return fmt.Errorf("pipeline: list active trigger links for %q: %w", triggerType, err)
	}
	// ... rest of function unchanged ...
```

- [ ] **Update `PushInitialTasksForGraph`** — seed the run tracking counter and set `RootPipelineID` on initial tasks

The key change: after counting pushed tasks, call `InitPipelineRunTracking` if `N > 0`.

In `PushInitialTasksForGraph`, the `pushed` variable already counts the initial tasks. Add after the loop:

```go
// Seed the pipeline run tracking counter so pipeline_finished fires when done.
if pushed > 0 {
	eventPayload, _ := json.Marshal(ev) // ev already parsed earlier in the function
	if err := st.InitPipelineRunTracking(ctx, pipelineID, tweetID, pushed, eventPayload); err != nil && logger != nil {
		logger.Warn("pipeline: init run tracking failed",
			"pipeline_id", pipelineID, "tweet_id", tweetID, "error", err)
	}
}
```

Note: `PushInitialTasksForGraph` already has `tweetID` as a parameter. The `ev` is reconstructed from `eventPayload` inside the function. Store the parsed `ev.TweetID` — it's already in the parameter as `tweetID`.

So the full change to `PushInitialTasksForGraph`: pass `rootPipelineID = pipelineID` into all task constructors AND add the tracking seed. The `pipelineID` parameter is the root pipeline.

- [ ] **Set `RootPipelineID` in `dispatchInitialNode`** — for the action case, add `RootPipelineID: rootPipelineID` to the task:

In `dispatchInitialNode`, the `pipelineID` parameter IS the root pipeline ID when called from `PushInitialTasksForGraph`. Update the task construction for the action case:

```go
task := &queue.Task{
    TriggerType:    triggerType,
    Event:          eventPayload,
    ActionID:       *node.ActionID,
    PipelineNodeID: node.ID,
    PipelineID:     pipelineID,
    RootPipelineID: pipelineID, // NEW
    Context:        map[string]any{},
}
if pushErr := producer.PushTask(ctx, task); pushErr != nil {
```

Wait — `dispatchInitialNode` is called both from `PushInitialTasksForGraph` (where pipelineID == rootPipelineID) AND from `PushTasksFromNode` (same). Sub-pipeline initial dispatch inside `dispatchInitialNode` needs to propagate the root pipeline ID, not the sub-pipeline's ID.

The cleanest fix: add a `rootPipelineID int64` parameter to `dispatchInitialNode`.

Update `dispatchInitialNode` signature to:
```go
func dispatchInitialNode(
	ctx context.Context,
	st store.Store,
	producer queue.Producer,
	cache *ActionCache,
	graph *model.PipelineGraph,
	pipelineID int64,
	rootPipelineID int64,   // NEW
	triggerType string,
	eventPayload []byte,
	tweetID string,
	node *model.PipelineNode,
	initCtx map[string]any,
	logger *slog.Logger,
) int {
```

Then in the action case, use `pushTrackedTask` instead of `producer.PushTask`:
```go
task := &queue.Task{
    TriggerType:    triggerType,
    Event:          eventPayload,
    ActionID:       *node.ActionID,
    PipelineNodeID: node.ID,
    PipelineID:     pipelineID,
    RootPipelineID: rootPipelineID,
    Context:        map[string]any{},
}
if pushTrackedTask(ctx, st, producer, task, logger) {
    pushed++
    // log...
}
```

In the pipeline (sub-pipeline) case, propagate `rootPipelineID`:
```go
subTask := &queue.Task{
    TriggerType:            triggerType,
    Event:                  eventPayload,
    ActionID:               *subActionNode.ActionID,
    PipelineNodeID:         subActionNode.ID,
    PipelineID:             *node.SubPipelineID,
    RootPipelineID:         rootPipelineID,  // propagate root, not sub-pipeline ID
    ParentPipelineNodeID:   &parentNodeID,
    SubPipelineEntryNodeID: &entryNodeID,
    Context:                map[string]any{},
}
if pushTrackedTask(ctx, st, producer, subTask, logger) {
    pushed++
}
```

In the condition case (recursive call to `dispatchInitialNode`), pass `rootPipelineID` through:
```go
pushed += dispatchInitialNode(ctx, st, producer, cache, graph, pipelineID,
    rootPipelineID, triggerType, eventPayload, tweetID, nextNode, initCtx, logger)
```

- [ ] **Update callers of `dispatchInitialNode`** in `PushInitialTasksForGraph` and `PushTasksFromNode`:

In `PushInitialTasksForGraph`:
```go
pushed += dispatchInitialNode(ctx, st, producer, cache, graph, pipelineID,
    pipelineID, triggerType, eventPayload, tweetID, node, initCtx, logger)
```

In `PushTasksFromNode`:
```go
return dispatchInitialNode(ctx, st, producer, cache, graph, pipelineID,
    pipelineID, triggerType, eventPayload, tweetID, startNode, initCtx, logger)
```

- [ ] **Add tracking seed in `PushInitialTasksForGraph` after the push loop**

The full end of `PushInitialTasksForGraph` (after the `for` loop) becomes:

```go
pushed := 0
for _, edge := range outgoingEdges(graph, startNode.ID) {
    node := findNode(graph, edge.TargetNodeID)
    if node == nil {
        continue
    }
    pushed += dispatchInitialNode(ctx, st, producer, cache, graph, pipelineID,
        pipelineID, triggerType, eventPayload, tweetID, node, initCtx, logger)
}

// Seed pipeline run tracking so pipeline_finished fires when all tasks complete.
// Skip if no tasks were pushed (empty pipeline) or no tweet ID (non-tweet triggers).
if pushed > 0 && tweetID != "" {
    if err := st.InitPipelineRunTracking(ctx, pipelineID, tweetID, pushed, eventPayload); err != nil && logger != nil {
        logger.Warn("pipeline: init run tracking failed",
            "pipeline_id", pipelineID, "tweet_id", tweetID, "error", err)
    }
}

return pushed
```

- [ ] **Update `PushContinuationTasks` — use `pushTrackedTask` and propagate `RootPipelineID`**

Replace every `producer.PushTask` call in `PushContinuationTasks` with `pushTrackedTask`. All downstream tasks must carry `RootPipelineID` from the parent task.

Update `pushContinuationTask` helper to propagate `RootPipelineID`:
```go
func pushContinuationTask(
	ctx context.Context,
	st store.Store,
	producer queue.Producer,
	parent *queue.Task,
	node *model.PipelineNode,
	mergedCtx map[string]any,
	logger *slog.Logger,
) bool {
	task := &queue.Task{
		TriggerType:            parent.TriggerType,
		Event:                  parent.Event,
		ActionID:               *node.ActionID,
		PipelineNodeID:         node.ID,
		PipelineID:             parent.PipelineID,
		RootPipelineID:         parent.RootPipelineID,
		ParentPipelineNodeID:   parent.ParentPipelineNodeID,
		SubPipelineEntryNodeID: parent.SubPipelineEntryNodeID,
		Context:                copyContext(mergedCtx),
	}
	return pushTrackedTask(ctx, st, producer, task, logger)
}
```

Update `pushContinuationTask` call sites in `PushContinuationTasks` to pass `st`:
```go
if pushContinuationTask(ctx, st, producer, task, targetNode, mergedCtx, logger) {
    pushed++
}
```

For sub-pipeline task pushes inside `PushContinuationTasks` (the Pipeline case and post-condition Pipeline case), update all direct `producer.PushTask` calls to use `pushTrackedTask` and add `RootPipelineID: task.RootPipelineID` to each `subTask`.

For the `signalSubPipelineLeafDone` internal `parentTask`, propagate `RootPipelineID`:
```go
parentTask := &queue.Task{
    TriggerType:    task.TriggerType,
    Event:          task.Event,
    PipelineNodeID: *task.ParentPipelineNodeID,
    PipelineID:     parentPipelineID,
    RootPipelineID: task.RootPipelineID,
}
PushContinuationTasks(ctx, st, producer, cache, parentTask, freshCtx, logger)
```

Note: `pushContinuationTask` now takes `st store.Store` as second parameter. Also update `PushContinuationTasks`'s own signature to include `st store.Store`:

```go
func PushContinuationTasks(
	ctx context.Context,
	st store.Store,
	producer queue.Producer,
	cache *ActionCache,
	task *queue.Task,
	mergedCtx map[string]any,
	logger *slog.Logger,
) {
```

(This is already there — `st` is already the second parameter. Just verify it's passed through.)

- [ ] **Verify compilation**

```bash
cd /mnt/shared/projects/personal/iran-watcher && go build ./internal/worker/...
```

Expected: compilation errors about callers of `PushInitialPipelineTasks` (they need the new `sourcePipelineID` arg). Note the call sites in `internal/stream/client.go`, `internal/search/client.go`, and `internal/api/handlers_tweets.go` — fix them in the next step.

- [ ] **Fix the three external callers** of `PushInitialPipelineTasks` to pass `0` for `sourcePipelineID`

In `internal/stream/client.go:345`:
```go
if err := worker.PushInitialPipelineTasks(ctx, c.store, c.producer, c.cache, "new_tweet", ev, c.logger, 0); err != nil {
```

In `internal/api/handlers_tweets.go:225`:
```go
if err := worker.PushInitialPipelineTasks(r.Context(), s.store, s.producer, s.actionCache, "new_tweet", ev, s.logger, 0); err != nil {
```

In `internal/search/client.go:263`:
```go
if pushErr := worker.PushInitialPipelineTasks(ctx, c.store, c.producer, c.cache, "new_tweet", ev, c.logger, 0); pushErr != nil && c.logger != nil {
```

- [ ] **Verify compilation again**

```bash
cd /mnt/shared/projects/personal/iran-watcher && go build ./...
```

Expected: no errors.

- [ ] **Run worker pipeline tests**

```bash
cd /mnt/shared/projects/personal/iran-watcher && go test ./internal/worker/... -v 2>&1 | tail -20
```

Expected: all existing tests PASS.

- [ ] **Commit**

```bash
git add internal/worker/pipeline.go internal/stream/client.go internal/search/client.go internal/api/handlers_tweets.go
git commit -m "feat(worker): add RootPipelineID propagation, pushTrackedTask, and run tracking seed"
```

---

### Task 8: manager.go — deferred decrement and firePipelineFinished

**Files:**
- Modify: `internal/worker/manager.go`

- [ ] **Add deferred decrement in `processTask`**

In `internal/worker/manager.go`, in the `processTask` function, after the line:

```go
var ev queue.TweetEvent
_ = json.Unmarshal(task.Event, &ev)
```

Add the deferred decrement (before the rest of the function):

```go
// Decrement the pipeline run tracking counter when this task is done.
// The defer fires AFTER PushContinuationTasks (at end of function or early return),
// so all downstream task increments happen before this -1.
if task.RootPipelineID != 0 {
    defer func() {
        done, err := m.store.DecrementPipelineRunTracking(ctx, task.RootPipelineID, ev.TweetID)
        if err != nil && m.logger != nil {
            m.logger.Warn("worker: decrement pipeline run tracking",
                "root_pipeline_id", task.RootPipelineID, "tweet_id", ev.TweetID, "error", err)
        }
        if done {
            m.firePipelineFinished(ctx, task.RootPipelineID, ev.TweetID)
        }
    }()
}
```

- [ ] **Add `firePipelineFinished` method** on `Manager` (add after `processTask`):

```go
// firePipelineFinished dispatches tasks for all pipelines linked to the
// pipeline_finished trigger for the given source pipeline.
func (m *Manager) firePipelineFinished(ctx context.Context, pipelineID int64, tweetID string) {
	eventPayload, err := m.store.GetPipelineRunEventPayload(ctx, pipelineID, tweetID)
	if err != nil || len(eventPayload) == 0 {
		if m.logger != nil {
			m.logger.Warn("worker: pipeline_finished: missing event payload",
				"pipeline_id", pipelineID, "tweet_id", tweetID)
		}
		return
	}
	var ev queue.TweetEvent
	if err := json.Unmarshal(eventPayload, &ev); err != nil {
		if m.logger != nil {
			m.logger.Warn("worker: pipeline_finished: unmarshal event", "error", err)
		}
		return
	}
	if m.logger != nil {
		m.logger.Info("worker: pipeline finished — firing pipeline_finished trigger",
			"pipeline_id", pipelineID, "tweet_id", tweetID)
	}
	if err := PushInitialPipelineTasks(ctx, m.store, m.producer, m.cache,
		"pipeline_finished", ev, m.logger, pipelineID); err != nil && m.logger != nil {
		m.logger.Warn("worker: push pipeline_finished tasks failed",
			"pipeline_id", pipelineID, "error", err)
	}
}
```

- [ ] **Verify compilation**

```bash
cd /mnt/shared/projects/personal/iran-watcher && go build ./internal/worker/...
```

Expected: no errors.

- [ ] **Run all worker tests**

```bash
cd /mnt/shared/projects/personal/iran-watcher && go test ./internal/worker/... -v 2>&1 | tail -20
```

Expected: all PASS.

- [ ] **Commit**

```bash
git add internal/worker/manager.go
git commit -m "feat(worker): fire pipeline_finished trigger when all pipeline tasks complete"
```

---

### Task 9: API — expose pipeline_finished trigger and source_pipeline_id

**Files:**
- Modify: `internal/api/handlers_triggers.go`

- [ ] **Add `pipeline_finished` to the trigger list and accept `source_pipeline_id` in create**

Replace `handleListTriggers` to add the new entry:

```go
func (s *Server) handleListTriggers(w http.ResponseWriter, r *http.Request) {
	triggers := []map[string]string{
		{"type": "new_tweet",          "description": "Fires when a tweet is received from the stream"},
		{"type": "new_categorization", "description": "Fires when an ONNX/Ollama action completes"},
		{"type": "entity_matched",     "description": "Fires when an entity matcher action finds a match"},
		{"type": "digest_requested",   "description": "Fires when a digest job is created via POST /api/digest"},
		{"type": "server_started",     "description": "Fires once when the server process starts"},
		{"type": "stream_connected",   "description": "Fires when the X stream connects"},
		{"type": "digest_completed",   "description": "Fires when a digest job finishes"},
		{"type": "pipeline_finished",  "description": "Fires when all branches of a pipeline complete for a tweet"},
	}
	writeJSON(w, http.StatusOK, triggers)
}
```

Replace `handleCreateTriggerPipelineLink` to accept the optional `source_pipeline_id`:

```go
func (s *Server) handleCreateTriggerPipelineLink(w http.ResponseWriter, r *http.Request) {
	var req struct {
		TriggerType      string `json:"trigger_type"`
		PipelineID       int64  `json:"pipeline_id"`
		SourcePipelineID *int64 `json:"source_pipeline_id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid body")
		return
	}
	if req.TriggerType == "" || req.PipelineID == 0 {
		writeError(w, http.StatusBadRequest, "trigger_type and pipeline_id are required")
		return
	}
	if req.TriggerType == "pipeline_finished" && (req.SourcePipelineID == nil || *req.SourcePipelineID == 0) {
		writeError(w, http.StatusBadRequest, "source_pipeline_id is required for pipeline_finished trigger")
		return
	}
	l := &model.TriggerPipelineLink{
		TriggerType:      req.TriggerType,
		PipelineID:       req.PipelineID,
		SourcePipelineID: req.SourcePipelineID,
		Active:           true,
	}
	if err := s.store.SaveTriggerPipelineLink(r.Context(), l); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if list, err := s.store.ListTriggerPipelineLinks(r.Context()); err == nil {
		s.broadcastEvent("trigger_links", list)
	}
	writeJSON(w, http.StatusCreated, l)
}
```

- [ ] **Verify compilation**

```bash
cd /mnt/shared/projects/personal/iran-watcher && go build ./internal/api/...
```

Expected: no errors.

- [ ] **Commit**

```bash
git add internal/api/handlers_triggers.go
git commit -m "feat(api): add pipeline_finished trigger type and source_pipeline_id to trigger link create"
```

---

### Task 10: Frontend — trigger_links.ts + PipelinesView.vue

**Files:**
- Modify: `web/src/api/trigger_links.ts`
- Modify: `web/src/views/PipelinesView.vue`

- [ ] **Update `trigger_links.ts`** — add `source_pipeline_id` to the type and create function

Replace the contents of `web/src/api/trigger_links.ts`:

```typescript
import { apiFetch } from './client'

export interface TriggerInfo {
  type: string
  description: string
}

export interface TriggerPipelineLink {
  id: number
  trigger_type: string
  pipeline_id: number
  source_pipeline_id?: number
  active: boolean
  created_at: string
}

export const listTriggers = () => apiFetch<TriggerInfo[]>('/api/triggers')
export const listTriggerPipelineLinks = () => apiFetch<TriggerPipelineLink[]>('/api/triggers/links')
export const createTriggerPipelineLink = (body: {
  trigger_type: string
  pipeline_id: number
  source_pipeline_id?: number
}) => apiFetch<TriggerPipelineLink>('/api/triggers/links', { method: 'POST', body: JSON.stringify(body) })
export const deleteTriggerPipelineLink = (id: number) =>
  apiFetch<void>(`/api/triggers/links/${id}`, { method: 'DELETE' })
export const activateTriggerPipelineLink = (id: number) =>
  apiFetch<void>(`/api/triggers/links/${id}/activate`, { method: 'POST' })
export const deactivateTriggerPipelineLink = (id: number) =>
  apiFetch<void>(`/api/triggers/links/${id}/deactivate`, { method: 'POST' })
```

- [ ] **Update `PipelinesView.vue`** — add `linkFormSourcePipelineId` ref and the source pipeline picker

Add the reactive ref after `linkFormPipelineId`:

```typescript
const linkFormPipelineId = ref<number | null>(null)
const linkFormSourcePipelineId = ref<number | null>(null)  // NEW
```

Update `openAddLink` to reset the new field:

```typescript
function openAddLink(triggerType: string) {
  linkFormTrigger.value = triggerType
  linkFormPipelineId.value = null
  linkFormSourcePipelineId.value = null  // NEW
  showLinkForm.value = true
}
```

Update `onSaveLink` to pass `source_pipeline_id` when trigger is `pipeline_finished`:

```typescript
async function onSaveLink() {
  if (!linkFormPipelineId.value) { msg.error('Pipeline is required'); return }
  if (linkFormTrigger.value === 'pipeline_finished' && !linkFormSourcePipelineId.value) {
    msg.error('Source Pipeline is required for pipeline_finished trigger')
    return
  }
  savingLink.value = true
  try {
    const body: { trigger_type: string; pipeline_id: number; source_pipeline_id?: number } = {
      trigger_type: linkFormTrigger.value,
      pipeline_id: linkFormPipelineId.value,
    }
    if (linkFormTrigger.value === 'pipeline_finished' && linkFormSourcePipelineId.value) {
      body.source_pipeline_id = linkFormSourcePipelineId.value
    }
    const l = await createTriggerPipelineLink(body)
    links.value.push(l)
    msg.success('Link created')
    showLinkForm.value = false
  } catch (e: any) { msg.error(e.message) }
  finally { savingLink.value = false }
}
```

Update the FormDrawer in the template — add the "Source Pipeline" picker after the existing Pipeline picker. Find the `<FormDrawer>` block (around line 1311) and update:

```html
<!-- Connect Pipeline to Trigger Drawer -->
<FormDrawer
  v-model:show="showLinkForm"
  :title="`Connect pipeline to ${linkFormTrigger}`"
  save-label="Connect"
  :loading="savingLink"
  @save="onSaveLink"
>
  <n-form-item
    v-if="linkFormTrigger === 'pipeline_finished'"
    label="Source Pipeline"
  >
    <n-select
      v-model:value="linkFormSourcePipelineId"
      :options="pipelineSelectOptions"
      placeholder="Pipeline that must finish first"
      clearable
    />
  </n-form-item>
  <n-form-item label="Pipeline">
    <n-select
      v-model:value="linkFormPipelineId"
      :options="pipelineSelectOptions"
      placeholder="Select a pipeline"
      clearable
    />
  </n-form-item>
</FormDrawer>
```

Also update the link display badge to show `source_pipeline_id` when present. Find the badge rendering block (around line 928-939) and add a source pipeline label:

```html
<n-flex v-if="linksByTrigger[trigger.type]?.length" :size="6" wrap>
  <n-tag
    v-for="link in linksByTrigger[trigger.type]"
    :key="link.id"
    size="small"
    :bordered="false"
    :style="{ background: '#313244', cursor: 'pointer' }"
    closable
    @close="removeLink(link.id)"
    @click="toggleLink(link)"
  >
    <span :style="{ color: link.active ? '#cdd6f4' : '#6c7086' }">
      {{ link.source_pipeline_id ? `[${pipelineName(link.source_pipeline_id)}] → ` : '' }}{{ pipelineName(link.pipeline_id) }}
    </span>
  </n-tag>
</n-flex>
```

- [ ] **Commit**

```bash
git add web/src/api/trigger_links.ts web/src/views/PipelinesView.vue
git commit -m "feat(ui): support source_pipeline_id in trigger link form for pipeline_finished"
```

---

### Task 11: Frontend — add exists/not_exists to condition node

**Files:**
- Modify: `web/src/views/PipelineBuilderView.vue`

- [ ] **Add `exists` and `not_exists` to `conditionOpOptions`**

Find the `conditionOpOptions` array (around line 116) and update it:

```typescript
const conditionOpOptions = [
  { label: '==', value: '==' }, { label: '!=', value: '!=' },
  { label: '>', value: '>' }, { label: '>=', value: '>=' },
  { label: '<', value: '<' }, { label: '<=', value: '<=' },
  { label: 'contains', value: 'contains' }, { label: 'not_contains', value: 'not_contains' },
  { label: 'exists', value: 'exists' }, { label: 'not_exists', value: 'not_exists' },
  { label: 'is_empty', value: 'is_empty' }, { label: 'not_empty', value: 'not_empty' },
  { label: 'rule_matches', value: 'rule_matches' },
  { label: 'rule_not_matches', value: 'rule_not_matches' },
]
```

- [ ] **Hide the value input for `exists` and `not_exists`**

Find the `v-else-if` condition that hides the value input for `is_empty`, `not_empty`, etc. (around line 564):

```html
<n-input
  v-else-if="!['is_empty','not_empty','rule_matches','rule_not_matches'].includes(cond.op)"
  v-model:value="cond.value"
  size="small" placeholder="value" style="margin-top: 3px;"
/>
```

Update it to also hide for `exists` and `not_exists`:

```html
<n-input
  v-else-if="!['is_empty','not_empty','exists','not_exists','rule_matches','rule_not_matches'].includes(cond.op)"
  v-model:value="cond.value"
  size="small" placeholder="value" style="margin-top: 3px;"
/>
```

- [ ] **Commit**

```bash
git add web/src/views/PipelineBuilderView.vue
git commit -m "feat(ui): add exists/not_exists operators to condition node"
```

---

### Task 12: Update CLAUDE.md documentation

**Files:**
- Modify: `CLAUDE.md`

- [ ] **Add `pipeline_finished` trigger and `pipeline_run_tracking` to Key Design Decisions**

In the "Event-driven actions system" bullet in CLAUDE.md, find the list of trigger types and add:

```
`pipeline_finished` (fires when all branches of a top-level pipeline complete for a tweet — triggers downstream notification or join pipelines)
```

Find the `pipeline_node_executions` mention and add a note about `pipeline_run_tracking`:

Add a new Key Design Decision entry:

```
- **Pipeline finished trigger** — `pipeline_finished` fires when every task in a top-level pipeline completes for a tweet, regardless of branch outcome; detected via reference counting in `pipeline_run_tracking (pipeline_id, tweet_id, active_tasks, event_payload)` — counter seeded at N (initial tasks pushed by `PushInitialTasksForGraph`), incremented by `pushTrackedTask` on each `producer.PushTask` call, decremented (via defer) in `processTask` after `PushContinuationTasks`; when `active_tasks` reaches 0, `firePipelineFinished` dispatches to pipelines linked via `trigger_type=pipeline_finished` scoped to the source pipeline ID; `RootPipelineID` on `queue.Task` propagates through all sub-pipeline tasks so every level of nesting counts against the same root counter; `trigger_pipeline_links.source_pipeline_id` scopes a `pipeline_finished` link to a specific pipeline — required for this trigger type; condition node operators `exists`/`not_exists` check for key presence in the merged metadata context (backend already supported; now exposed in UI)
```

- [ ] **Commit**

```bash
git add CLAUDE.md
git commit -m "docs(claude.md): document pipeline_finished trigger and pipeline_run_tracking"
```

---

### Task 13: Build verification

- [ ] **Build the Docker image**

```bash
cd /mnt/shared/projects/personal/iran-watcher && docker compose build --build-arg BUILDKIT_INLINE_CACHE=1 iran-watcher
```

Expected: build succeeds with no errors.

- [ ] **Run all unit tests**

```bash
cd /mnt/shared/projects/personal/iran-watcher && go test ./...
```

Expected: all tests pass.

- [ ] **Final commit if any loose files remain**

```bash
git status
```

If clean: no action needed.
