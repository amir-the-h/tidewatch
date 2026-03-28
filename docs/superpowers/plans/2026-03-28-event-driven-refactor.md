# Event-Driven Pipeline Refactor Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Replace the pipeline-centric, in-memory fan-out dispatch model with a fully event-driven action system where every unit of work is a typed `Task` in Redis, pipelines produce structured `map[string]any` outputs, and actions chain conditionally on those outputs.

**Architecture:** Triggers (e.g., `new_tweet`) push per-action `Task` items to `iran-watcher:tasks`. Workers BRPOP tasks, resolve the action's pipeline from `ActionCache`, call `Pipeline.Run(ctx, PipelineInput)`, save outputs to `action_execution_metadata`, evaluate chain conditions, and push downstream tasks. The `ActionCache` is shared between the stream listener and workers, eliminating the dispatcher goroutine and per-action channels.

**Tech Stack:** Go 1.25.5, SQLite (modernc.org/sqlite), Redis (BRPOP/LPUSH), onnxruntime-go, Ollama HTTP API, daulet/tokenizers, Vue 3 + TypeScript.

---

## File Map

### New files
- `migrations/016_event_driven.sql` — pipelines, action_chains, restructured actions, pipeline_labels
- `internal/model/pipeline.go` — Pipeline domain type (DB row)
- `internal/model/action_chain.go` — ActionChain domain type
- `internal/store/sqlite_pipelines.go` — Pipeline CRUD
- `internal/store/sqlite_action_chains.go` — ActionChain CRUD
- `internal/pipeline/registry.go` — internal pipeline handler registry + PipelineFactory type
- `internal/pipeline/entity_matcher.go` — entity matching as an internal Pipeline
- `internal/pipeline/twitter.go` — retweet/like/quote as internal Pipelines
- `internal/pipeline/notification.go` — push/telegram notification as internal Pipelines
- `internal/worker/action_cache.go` — ActionCache: action_id → (Action row + resolved Pipeline)
- `internal/api/handlers_pipelines.go` — /api/pipelines CRUD + activate/deactivate + download + handlers
- `internal/api/handlers_chains.go` — /api/actions/{id}/chains CRUD
- `internal/api/handlers_triggers.go` — GET /api/triggers
- `web/src/views/PipelinesView.vue` — pipeline management UI

### Modified files
- `internal/inference/pipeline.go` — updated Pipeline interface (PipelineInput, map output, drop Load/RunBatch)
- `internal/inference/onnx.go` — updated Run signature; new NewONNXPipelineFromConfig factory
- `internal/inference/ollama.go` — updated Run signature; new NewOllamaPipelineFromConfig factory
- `internal/inference/registry.go` — updated for new Pipeline interface
- `internal/model/action.go` — add TriggerType, PipelineID; remove Type, Config
- `internal/store/store.go` — add PipelineFilter, Pipeline CRUD, ActionChain CRUD; remove model_labels/rule_models methods; update ActionFilter
- `internal/store/sqlite_actions.go` — updated for new schema (trigger_type, pipeline_id, updated_at; drop type/config)
- `internal/store/sqlite_models.go` — remove (Models replaced by Pipelines)
- `internal/queue/queue.go` — new Task/TaskEvent types, QueueTasks const; keep FailedTweet
- `internal/queue/redis.go` — PushTask/PopTask replacing Push/Pop for raw queue
- `internal/worker/manager.go` — simplified: BRPOP tasks, no dispatcher/scheduler/per-action channels
- `internal/stream/client.go` — push Tasks via ActionCache lookup
- `internal/api/server.go` — add pipeline/chain/trigger routes; remove model routes
- `internal/api/handlers_actions.go` — updated for new schema (trigger_type, pipeline_id)
- `internal/api/handlers_models.go` — remove (replaced by handlers_pipelines.go)
- `cli/cmd/serve.go` — register internal pipelines, build ActionCache, remove model/action loading
- `web/src/router/index.ts` — add /pipelines route
- `web/src/views/ActionsView.vue` — trigger_type selector, pipeline selector, chain builder

### Removed
- `internal/action/` — entire package (PipelineAction, TwitterAction, Registry, pipeline_task.go)

---

## Task 1: Migration 016 — New Database Schema

**Files:**
- Create: `migrations/016_event_driven.sql`

- [ ] **Step 1: Write the migration**

```sql
-- migrations/016_event_driven.sql

-- 1. Create pipelines table (replaces models)
CREATE TABLE IF NOT EXISTS pipelines (
    id          INTEGER PRIMARY KEY AUTOINCREMENT,
    name        TEXT    NOT NULL UNIQUE,
    type        TEXT    NOT NULL,    -- onnx | ollama | internal
    config      TEXT    NOT NULL DEFAULT '{}',
    active      INTEGER NOT NULL DEFAULT 1,
    created_at  DATETIME NOT NULL DEFAULT (datetime('now')),
    updated_at  DATETIME NOT NULL DEFAULT (datetime('now'))
);

-- 2. Migrate models → pipelines (preserve IDs so action config model_id references remain valid)
INSERT OR IGNORE INTO pipelines (id, name, type, config, active, created_at, updated_at)
SELECT
    m.id,
    m.name,
    CASE
        WHEN m.type IN ('summarizer','extraction','digest') THEN 'ollama'
        ELSE 'onnx'
    END,
    json_object('path', m.path, 'model_type', m.type),
    m.active,
    m.created_at,
    m.created_at
FROM models m;

-- 3. Create internal pipeline rows for twitter action types that exist
INSERT OR IGNORE INTO pipelines (name, type, config, active, created_at, updated_at)
SELECT DISTINCT
    'twitter-' || type,
    'internal',
    json_object('handler', 'twitter_' || type),
    1,
    datetime('now'),
    datetime('now')
FROM actions WHERE type IN ('retweet','like','quote');

-- 4. Rename pipeline_labels (keep labels but reference pipelines not models)
CREATE TABLE IF NOT EXISTS pipeline_labels (
    pipeline_id  INTEGER NOT NULL REFERENCES pipelines(id) ON DELETE CASCADE,
    label        TEXT    NOT NULL,
    PRIMARY KEY (pipeline_id, label)
);

INSERT OR IGNORE INTO pipeline_labels (pipeline_id, label)
SELECT model_id, label FROM model_labels;

-- 5. Create action_chains table
CREATE TABLE IF NOT EXISTS action_chains (
    id               INTEGER PRIMARY KEY AUTOINCREMENT,
    source_action_id INTEGER NOT NULL REFERENCES actions(id) ON DELETE CASCADE,
    target_action_id INTEGER NOT NULL REFERENCES actions(id) ON DELETE CASCADE,
    conditions       TEXT    NOT NULL DEFAULT '[]',
    created_at       DATETIME NOT NULL DEFAULT (datetime('now'))
);
CREATE INDEX IF NOT EXISTS idx_action_chains_source ON action_chains(source_action_id);
CREATE INDEX IF NOT EXISTS idx_action_chains_target ON action_chains(target_action_id);

-- 6. Restructure actions table (rename + recreate)
ALTER TABLE actions RENAME TO actions_old;

CREATE TABLE actions (
    id           INTEGER PRIMARY KEY AUTOINCREMENT,
    name         TEXT    NOT NULL,
    trigger_type TEXT    NOT NULL DEFAULT 'new_tweet',
    pipeline_id  INTEGER NOT NULL REFERENCES pipelines(id),
    rule_ids     TEXT    NOT NULL DEFAULT '[]',
    active       INTEGER NOT NULL DEFAULT 1,
    created_at   DATETIME NOT NULL DEFAULT (datetime('now')),
    updated_at   DATETIME NOT NULL DEFAULT (datetime('now'))
);
CREATE INDEX IF NOT EXISTS idx_actions_pipeline_id ON actions(pipeline_id);
CREATE INDEX IF NOT EXISTS idx_actions_active ON actions(active);

-- 7. Migrate actions: pipeline type → pipeline_id = model_id from config
INSERT INTO actions (id, name, trigger_type, pipeline_id, rule_ids, active, created_at, updated_at)
SELECT
    a.id,
    a.name,
    'new_tweet',
    CAST(json_extract(a.config, '$.model_id') AS INTEGER),
    a.rule_ids,
    a.active,
    a.created_at,
    a.created_at
FROM actions_old a
WHERE a.type = 'pipeline'
  AND json_extract(a.config, '$.model_id') IS NOT NULL;

-- 8. Migrate twitter actions
INSERT INTO actions (id, name, trigger_type, pipeline_id, rule_ids, active, created_at, updated_at)
SELECT
    a.id,
    a.name,
    'new_tweet',
    (SELECT p.id FROM pipelines p WHERE p.name = 'twitter-' || a.type AND p.type = 'internal' LIMIT 1),
    a.rule_ids,
    a.active,
    a.created_at,
    a.created_at
FROM actions_old a
WHERE a.type IN ('retweet','like','quote');

DROP TABLE actions_old;

-- 9. Drop old tables
DROP TABLE IF EXISTS rule_models;
DROP TABLE IF EXISTS model_labels;
DROP TABLE IF EXISTS models;
```

- [ ] **Step 2: Verify migration runs against in-memory SQLite**

```bash
go test ./migrations/ -v -run TestMigrations
```

Expected: PASS. If the migrations_test.go runs all migrations, it will catch SQL syntax errors.

- [ ] **Step 3: Commit**

```bash
git add migrations/016_event_driven.sql
git commit -m "feat: migration 016 — pipelines table, action_chains, restructure actions"
```

---

## Task 2: Domain Models

**Files:**
- Create: `internal/model/pipeline.go`
- Create: `internal/model/action_chain.go`
- Modify: `internal/model/action.go`
- Modify: `internal/model/model.go` (keep temporarily, mark deprecated)

- [ ] **Step 1: Create `internal/model/pipeline.go`**

```go
package model

import (
	"encoding/json"
	"time"
)

// PipelineType identifies the execution backend.
type PipelineType string

const (
	PipelineTypeONNX     PipelineType = "onnx"
	PipelineTypeOllama   PipelineType = "ollama"
	PipelineTypeInternal PipelineType = "internal"
)

// Pipeline is a persistent record of a configured execution unit.
type Pipeline struct {
	ID        int64           `json:"id"`
	Name      string          `json:"name"`
	Type      PipelineType    `json:"type"`
	Config    json.RawMessage `json:"config"`
	Active    bool            `json:"active"`
	CreatedAt time.Time       `json:"created_at"`
	UpdatedAt time.Time       `json:"updated_at"`
}
```

- [ ] **Step 2: Create `internal/model/action_chain.go`**

```go
package model

import "time"

// ActionChain is a conditional link from one action to another.
// After source_action completes, conditions are evaluated against the
// merged execution context. If all pass, a new Task is pushed for target_action.
type ActionChain struct {
	ID             int64            `json:"id"`
	SourceActionID int64            `json:"source_action_id"`
	TargetActionID int64            `json:"target_action_id"`
	Conditions     []ChainCondition `json:"conditions"`
	CreatedAt      time.Time        `json:"created_at"`
}

// ChainCondition is a single predicate evaluated against a string context value.
// Supported operators: ==, !=, >, >=, <, <=, contains, not_contains,
// is_empty, not_empty, is_array, is_object, array_empty, array_not_empty.
type ChainCondition struct {
	Key      string `json:"key"`
	Operator string `json:"op"`
	Value    string `json:"value,omitempty"`
}
```

- [ ] **Step 3: Update `internal/model/action.go`** — replace `Type ActionType` and `Config` with `TriggerType` and `PipelineID`; add `UpdatedAt`

```go
package model

import "time"

// TriggerType is the event that initiates an action.
type TriggerType string

const (
	TriggerTypeNewTweet          TriggerType = "new_tweet"
	TriggerTypeNewCategorization TriggerType = "new_categorization"
	TriggerTypeEntityMatched     TriggerType = "entity_matched"
)

// Action subscribes a pipeline to a trigger.
type Action struct {
	ID          int64       `json:"id"`
	Name        string      `json:"name"`
	TriggerType TriggerType `json:"trigger_type"`
	PipelineID  int64       `json:"pipeline_id"`
	Active      bool        `json:"active"`
	RuleIDs     []int64     `json:"rule_ids"`
	CreatedAt   time.Time   `json:"created_at"`
	UpdatedAt   time.Time   `json:"updated_at"`
}

// ExecutionStatus values for ActionExecution.
const (
	ExecutionStatusSuccess = "success"
	ExecutionStatusError   = "error"
)

// ActionExecution is one log entry per action fired on one tweet.
type ActionExecution struct {
	ID         int64             `json:"id"`
	ActionID   int64             `json:"action_id"`
	TweetID    string            `json:"tweet_id"`
	Status     string            `json:"status"`
	Error      string            `json:"error,omitempty"`
	ExecutedAt time.Time         `json:"executed_at"`
	Metadata   map[string]string `json:"metadata,omitempty"`
}

// TwitterCredentials stores the connected Twitter account's OAuth 2.0 tokens.
type TwitterCredentials struct {
	ID            int64     `json:"id"`
	AccessToken   string    `json:"-"`
	RefreshToken  string    `json:"-"`
	ExpiresAt     time.Time `json:"expires_at"`
	TwitterUserID string    `json:"twitter_user_id"`
	Username      string    `json:"username"`
	UpdatedAt     time.Time `json:"updated_at"`
}
```

- [ ] **Step 4: Verify it compiles (ignoring downstream breakage for now)**

```bash
go build ./internal/model/...
```

Expected: PASS (model package has no external dependencies)

- [ ] **Step 5: Commit**

```bash
git add internal/model/pipeline.go internal/model/action_chain.go internal/model/action.go
git commit -m "feat: add Pipeline and ActionChain domain models; update Action for event-driven schema"
```

---

## Task 3: Store Interface Updates

**Files:**
- Modify: `internal/store/store.go`

- [ ] **Step 1: Replace model-related methods and add pipeline + chain methods in `store.go`**

Add `PipelineFilter` and `ActionChainFilter`, add Pipeline CRUD and ActionChain CRUD, update `ActionFilter` to use `TriggerType` instead of `Type`. Remove `SaveModel`, `GetModel`, `ListModels`, `DeleteModel`, `SetModelActive`, `ListModelLabels`, `AddModelLabel`, `RemoveModelLabel`, `AddModelRule`, `RemoveModelRule`, `ListTweetRuleIDs` (rule_models gone).

Replace the existing Store interface with:

```go
// PipelineFilter defines optional filters for listing pipelines.
type PipelineFilter struct {
	Type   *model.PipelineType
	Active *bool
}

// ActionFilter defines optional filters for listing actions.
type ActionFilter struct {
	TriggerType *model.TriggerType
	PipelineID  *int64
	Active      *bool
}

// ActionChainFilter defines optional filters for listing action chains.
type ActionChainFilter struct {
	SourceActionID *int64
	TargetActionID *int64
}
```

And in the Store interface, replace the Models section with:

```go
// Pipelines
SavePipeline(ctx context.Context, p *model.Pipeline) error
GetPipeline(ctx context.Context, id int64) (*model.Pipeline, error)
ListPipelines(ctx context.Context, filter PipelineFilter) ([]*model.Pipeline, error)
UpdatePipeline(ctx context.Context, p *model.Pipeline) error
DeletePipeline(ctx context.Context, id int64) error
SetPipelineActive(ctx context.Context, id int64, active bool) error
ListPipelineLabels(ctx context.Context, pipelineID int64) ([]string, error)
AddPipelineLabel(ctx context.Context, pipelineID int64, label string) error
RemovePipelineLabel(ctx context.Context, pipelineID int64, label string) error

// ActionChains
SaveActionChain(ctx context.Context, c *model.ActionChain) error
GetActionChain(ctx context.Context, id int64) (*model.ActionChain, error)
ListActionChains(ctx context.Context, filter ActionChainFilter) ([]*model.ActionChain, error)
UpdateActionChain(ctx context.Context, c *model.ActionChain) error
DeleteActionChain(ctx context.Context, id int64) error

// TweetRuleIDs (kept — used by stream listener rule matching)
ListTweetRuleIDs(ctx context.Context, tweetID string) ([]int64, error)
```

Also update the Actions section — `ListActions` filter is now using the updated `ActionFilter`.

- [ ] **Step 2: Compile check (will fail due to missing implementations — that's ok)**

```bash
go build ./internal/store/ 2>&1 | head -30
```

Expected: errors about missing methods on sqliteStore — expected, continue.

- [ ] **Step 3: Commit interface**

```bash
git add internal/store/store.go
git commit -m "feat: update Store interface for event-driven schema (pipelines, chains, updated action filter)"
```

---

## Task 4: Store Implementations

**Files:**
- Create: `internal/store/sqlite_pipelines.go`
- Create: `internal/store/sqlite_action_chains.go`
- Modify: `internal/store/sqlite_actions.go`
- Delete: `internal/store/sqlite_models.go` (find the models file first)

- [ ] **Step 1: Find models store file**

```bash
grep -rl "func.*sqliteStore.*SaveModel\|func.*sqliteStore.*ListModels" internal/store/
```

Note the filename. It will be deleted after pipeline implementation is written.

- [ ] **Step 2: Create `internal/store/sqlite_pipelines.go`**

```go
package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/amir-the-h/iran-watcher/internal/model"
)

func (s *sqliteStore) SavePipeline(ctx context.Context, p *model.Pipeline) error {
	cfg := p.Config
	if len(cfg) == 0 {
		cfg = []byte("{}")
	}
	now := time.Now().UTC().Format(time.RFC3339)
	res, err := s.db.ExecContext(ctx,
		`INSERT INTO pipelines (name, type, config, active, created_at, updated_at) VALUES (?,?,?,?,?,?)`,
		p.Name, string(p.Type), string(cfg), boolToInt(p.Active), now, now,
	)
	if err != nil {
		return fmt.Errorf("store: save pipeline: %w", err)
	}
	id, err := res.LastInsertId()
	if err != nil {
		return fmt.Errorf("store: save pipeline: last insert id: %w", err)
	}
	p.ID = id
	return nil
}

func (s *sqliteStore) GetPipeline(ctx context.Context, id int64) (*model.Pipeline, error) {
	row := s.db.QueryRowContext(ctx,
		`SELECT id, name, type, config, active, created_at, updated_at FROM pipelines WHERE id = ?`, id)
	p, err := scanPipeline(row)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, fmt.Errorf("store: get pipeline: not found: %w", err)
		}
		return nil, fmt.Errorf("store: get pipeline: %w", err)
	}
	return p, nil
}

func (s *sqliteStore) ListPipelines(ctx context.Context, filter PipelineFilter) ([]*model.Pipeline, error) {
	var where []string
	var args []any
	if filter.Type != nil {
		where = append(where, "type = ?")
		args = append(args, string(*filter.Type))
	}
	if filter.Active != nil {
		where = append(where, "active = ?")
		args = append(args, boolToInt(*filter.Active))
	}
	q := "SELECT id, name, type, config, active, created_at, updated_at FROM pipelines"
	if len(where) > 0 {
		q += " WHERE " + strings.Join(where, " AND ")
	}
	q += " ORDER BY id ASC"

	rows, err := s.db.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, fmt.Errorf("store: list pipelines: %w", err)
	}
	defer rows.Close()

	var out []*model.Pipeline
	for rows.Next() {
		p, err := scanPipeline(rows)
		if err != nil {
			return nil, fmt.Errorf("store: list pipelines: scan: %w", err)
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

func (s *sqliteStore) UpdatePipeline(ctx context.Context, p *model.Pipeline) error {
	cfg := p.Config
	if len(cfg) == 0 {
		cfg = []byte("{}")
	}
	_, err := s.db.ExecContext(ctx,
		`UPDATE pipelines SET name=?, config=?, updated_at=? WHERE id=?`,
		p.Name, string(cfg), time.Now().UTC().Format(time.RFC3339), p.ID,
	)
	if err != nil {
		return fmt.Errorf("store: update pipeline: %w", err)
	}
	return nil
}

func (s *sqliteStore) DeletePipeline(ctx context.Context, id int64) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM pipelines WHERE id = ?`, id)
	if err != nil {
		return fmt.Errorf("store: delete pipeline: %w", err)
	}
	return nil
}

func (s *sqliteStore) SetPipelineActive(ctx context.Context, id int64, active bool) error {
	_, err := s.db.ExecContext(ctx,
		`UPDATE pipelines SET active=?, updated_at=? WHERE id=?`,
		boolToInt(active), time.Now().UTC().Format(time.RFC3339), id,
	)
	if err != nil {
		return fmt.Errorf("store: set pipeline active: %w", err)
	}
	return nil
}

func (s *sqliteStore) ListPipelineLabels(ctx context.Context, pipelineID int64) ([]string, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT label FROM pipeline_labels WHERE pipeline_id = ? ORDER BY label ASC`, pipelineID)
	if err != nil {
		return nil, fmt.Errorf("store: list pipeline labels: %w", err)
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var l string
		if err := rows.Scan(&l); err != nil {
			return nil, fmt.Errorf("store: list pipeline labels: scan: %w", err)
		}
		out = append(out, l)
	}
	return out, rows.Err()
}

func (s *sqliteStore) AddPipelineLabel(ctx context.Context, pipelineID int64, label string) error {
	_, err := s.db.ExecContext(ctx,
		`INSERT OR IGNORE INTO pipeline_labels (pipeline_id, label) VALUES (?,?)`, pipelineID, label)
	if err != nil {
		return fmt.Errorf("store: add pipeline label: %w", err)
	}
	return nil
}

func (s *sqliteStore) RemovePipelineLabel(ctx context.Context, pipelineID int64, label string) error {
	_, err := s.db.ExecContext(ctx,
		`DELETE FROM pipeline_labels WHERE pipeline_id=? AND label=?`, pipelineID, label)
	if err != nil {
		return fmt.Errorf("store: remove pipeline label: %w", err)
	}
	return nil
}

type pipelineScanner interface{ Scan(dest ...any) error }

func scanPipeline(r pipelineScanner) (*model.Pipeline, error) {
	p := &model.Pipeline{}
	var activeInt int
	var configStr, createdAt, updatedAt string
	if err := r.Scan(&p.ID, &p.Name, &p.Type, &configStr, &activeInt, &createdAt, &updatedAt); err != nil {
		return nil, err
	}
	p.Active = activeInt != 0
	p.Config = json.RawMessage(configStr)
	if t, err := time.Parse(time.RFC3339, createdAt); err == nil {
		p.CreatedAt = t
	}
	if t, err := time.Parse(time.RFC3339, updatedAt); err == nil {
		p.UpdatedAt = t
	}
	return p, nil
}
```

- [ ] **Step 3: Create `internal/store/sqlite_action_chains.go`**

```go
package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/amir-the-h/iran-watcher/internal/model"
)

func (s *sqliteStore) SaveActionChain(ctx context.Context, c *model.ActionChain) error {
	conds, err := json.Marshal(c.Conditions)
	if err != nil {
		return fmt.Errorf("store: save action chain: marshal conditions: %w", err)
	}
	now := time.Now().UTC().Format(time.RFC3339)
	res, err := s.db.ExecContext(ctx,
		`INSERT INTO action_chains (source_action_id, target_action_id, conditions, created_at)
		 VALUES (?,?,?,?)`,
		c.SourceActionID, c.TargetActionID, string(conds), now,
	)
	if err != nil {
		return fmt.Errorf("store: save action chain: %w", err)
	}
	id, err := res.LastInsertId()
	if err != nil {
		return fmt.Errorf("store: save action chain: last insert id: %w", err)
	}
	c.ID = id
	return nil
}

func (s *sqliteStore) GetActionChain(ctx context.Context, id int64) (*model.ActionChain, error) {
	row := s.db.QueryRowContext(ctx,
		`SELECT id, source_action_id, target_action_id, conditions, created_at FROM action_chains WHERE id=?`, id)
	c, err := scanActionChain(row)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, fmt.Errorf("store: get action chain: not found: %w", err)
		}
		return nil, fmt.Errorf("store: get action chain: %w", err)
	}
	return c, nil
}

func (s *sqliteStore) ListActionChains(ctx context.Context, filter ActionChainFilter) ([]*model.ActionChain, error) {
	q := `SELECT id, source_action_id, target_action_id, conditions, created_at FROM action_chains`
	var where []string
	var args []any
	if filter.SourceActionID != nil {
		where = append(where, "source_action_id = ?")
		args = append(args, *filter.SourceActionID)
	}
	if filter.TargetActionID != nil {
		where = append(where, "target_action_id = ?")
		args = append(args, *filter.TargetActionID)
	}
	if len(where) > 0 {
		q += " WHERE " + join(where, " AND ")
	}
	q += " ORDER BY id ASC"

	rows, err := s.db.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, fmt.Errorf("store: list action chains: %w", err)
	}
	defer rows.Close()

	var out []*model.ActionChain
	for rows.Next() {
		c, err := scanActionChain(rows)
		if err != nil {
			return nil, fmt.Errorf("store: list action chains: scan: %w", err)
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

func (s *sqliteStore) UpdateActionChain(ctx context.Context, c *model.ActionChain) error {
	conds, err := json.Marshal(c.Conditions)
	if err != nil {
		return fmt.Errorf("store: update action chain: marshal conditions: %w", err)
	}
	_, err = s.db.ExecContext(ctx,
		`UPDATE action_chains SET conditions=? WHERE id=?`, string(conds), c.ID)
	if err != nil {
		return fmt.Errorf("store: update action chain: %w", err)
	}
	return nil
}

func (s *sqliteStore) DeleteActionChain(ctx context.Context, id int64) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM action_chains WHERE id=?`, id)
	if err != nil {
		return fmt.Errorf("store: delete action chain: %w", err)
	}
	return nil
}

type chainScanner interface{ Scan(dest ...any) error }

func scanActionChain(r chainScanner) (*model.ActionChain, error) {
	c := &model.ActionChain{}
	var condsStr, createdAt string
	if err := r.Scan(&c.ID, &c.SourceActionID, &c.TargetActionID, &condsStr, &createdAt); err != nil {
		return nil, err
	}
	if err := json.Unmarshal([]byte(condsStr), &c.Conditions); err != nil {
		c.Conditions = []model.ChainCondition{}
	}
	if t, err := time.Parse(time.RFC3339, createdAt); err == nil {
		c.CreatedAt = t
	}
	return c, nil
}

// join is a local alias to avoid importing strings in this file.
func join(parts []string, sep string) string {
	result := ""
	for i, p := range parts {
		if i > 0 {
			result += sep
		}
		result += p
	}
	return result
}
```

- [ ] **Step 4: Update `internal/store/sqlite_actions.go`** to use new schema

Replace `scanAction` and all CRUD to use `trigger_type`, `pipeline_id`, `updated_at` instead of `type`, `config`:

```go
func (s *sqliteStore) SaveAction(ctx context.Context, a *model.Action) error {
	ruleIDsJSON, err := json.Marshal(a.RuleIDs)
	if err != nil {
		return fmt.Errorf("store: save action: marshal rule_ids: %w", err)
	}
	now := time.Now().UTC().Format(time.RFC3339)
	res, err := s.db.ExecContext(ctx,
		`INSERT INTO actions (name, trigger_type, pipeline_id, active, rule_ids, created_at, updated_at)
		 VALUES (?,?,?,?,?,?,?)`,
		a.Name, string(a.TriggerType), a.PipelineID, boolToInt(a.Active),
		string(ruleIDsJSON), now, now,
	)
	if err != nil {
		return fmt.Errorf("store: save action: %w", err)
	}
	id, err := res.LastInsertId()
	if err != nil {
		return fmt.Errorf("store: save action: last insert id: %w", err)
	}
	a.ID = id
	return nil
}

func (s *sqliteStore) GetAction(ctx context.Context, id int64) (*model.Action, error) {
	row := s.db.QueryRowContext(ctx,
		`SELECT id, name, trigger_type, pipeline_id, active, rule_ids, created_at, updated_at
		 FROM actions WHERE id = ?`, id)
	a, err := scanAction(row)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, fmt.Errorf("store: get action: not found: %w", err)
		}
		return nil, fmt.Errorf("store: get action: %w", err)
	}
	return a, nil
}

func (s *sqliteStore) ListActions(ctx context.Context, filter ActionFilter) ([]*model.Action, error) {
	var where []string
	var args []any
	if filter.TriggerType != nil {
		where = append(where, "trigger_type = ?")
		args = append(args, string(*filter.TriggerType))
	}
	if filter.PipelineID != nil {
		where = append(where, "pipeline_id = ?")
		args = append(args, *filter.PipelineID)
	}
	if filter.Active != nil {
		where = append(where, "active = ?")
		args = append(args, boolToInt(*filter.Active))
	}
	q := `SELECT id, name, trigger_type, pipeline_id, active, rule_ids, created_at, updated_at FROM actions`
	if len(where) > 0 {
		q += " WHERE " + strings.Join(where, " AND ")
	}
	q += " ORDER BY id ASC"

	rows, err := s.db.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, fmt.Errorf("store: list actions: %w", err)
	}
	defer rows.Close()

	var out []*model.Action
	for rows.Next() {
		a, err := scanAction(rows)
		if err != nil {
			return nil, fmt.Errorf("store: list actions: scan: %w", err)
		}
		out = append(out, a)
	}
	return out, rows.Err()
}

func (s *sqliteStore) UpdateAction(ctx context.Context, a *model.Action) error {
	ruleIDsJSON, err := json.Marshal(a.RuleIDs)
	if err != nil {
		return fmt.Errorf("store: update action: marshal rule_ids: %w", err)
	}
	_, err = s.db.ExecContext(ctx,
		`UPDATE actions SET name=?, trigger_type=?, pipeline_id=?, rule_ids=?, updated_at=? WHERE id=?`,
		a.Name, string(a.TriggerType), a.PipelineID,
		string(ruleIDsJSON), time.Now().UTC().Format(time.RFC3339), a.ID,
	)
	if err != nil {
		return fmt.Errorf("store: update action: %w", err)
	}
	return nil
}

func (s *sqliteStore) SetActionActive(ctx context.Context, id int64, active bool) error {
	_, err := s.db.ExecContext(ctx,
		`UPDATE actions SET active=?, updated_at=? WHERE id=?`,
		boolToInt(active), time.Now().UTC().Format(time.RFC3339), id,
	)
	if err != nil {
		return fmt.Errorf("store: set action active: %w", err)
	}
	return nil
}

func scanAction(r actionScanner) (*model.Action, error) {
	a := &model.Action{}
	var activeInt int
	var ruleIDsStr, createdAt, updatedAt string
	if err := r.Scan(&a.ID, &a.Name, &a.TriggerType, &a.PipelineID, &activeInt,
		&ruleIDsStr, &createdAt, &updatedAt); err != nil {
		return nil, err
	}
	a.Active = activeInt != 0
	if err := json.Unmarshal([]byte(ruleIDsStr), &a.RuleIDs); err != nil || a.RuleIDs == nil {
		a.RuleIDs = []int64{}
	}
	if t, err := time.Parse(time.RFC3339, createdAt); err == nil {
		a.CreatedAt = t
	}
	if t, err := time.Parse(time.RFC3339, updatedAt); err == nil {
		a.UpdatedAt = t
	}
	return a, nil
}
```

Keep `DeleteAction`, `SaveActionExecution`, `ListActionExecutions`, `SaveActionExecutionMetadata`, `ListActionExecutionMetadata`, TwitterCredentials methods, and `ListTweetRuleIDs` unchanged.

- [ ] **Step 5: Delete old models store file**

```bash
# The models store file — verify the filename first with the grep from Step 1
rm internal/store/sqlite_models.go
```

- [ ] **Step 6: Add `ListTweetRuleIDs` to store if it's currently in the models file**

Check if `ListTweetRuleIDs` is in sqlite_models.go. If so, move it to `sqlite_actions.go` before deleting:

```go
func (s *sqliteStore) ListTweetRuleIDs(ctx context.Context, tweetID string) ([]int64, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT rule_id FROM tweet_rules WHERE tweet_id = ?`, tweetID)
	if err != nil {
		return nil, fmt.Errorf("store: list tweet rule ids: %w", err)
	}
	defer rows.Close()
	var ids []int64
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			return nil, fmt.Errorf("store: list tweet rule ids: scan: %w", err)
		}
		ids = append(ids, id)
	}
	return ids, rows.Err()
}
```

- [ ] **Step 7: Update mock store** (find mock file and add stub implementations for all new Store interface methods, stub-remove deleted methods)

```bash
find . -name "*.go" | xargs grep -l "mock\|Mock" | grep store
```

Add stub methods for: `SavePipeline`, `GetPipeline`, `ListPipelines`, `UpdatePipeline`, `DeletePipeline`, `SetPipelineActive`, `ListPipelineLabels`, `AddPipelineLabel`, `RemovePipelineLabel`, `SaveActionChain`, `GetActionChain`, `ListActionChains`, `UpdateActionChain`, `DeleteActionChain`. Remove stubs for deleted model methods.

- [ ] **Step 8: Run store tests**

```bash
go test ./internal/store/... -v 2>&1 | tail -20
```

Expected: PASS (migration tests and store unit tests pass)

- [ ] **Step 9: Commit**

```bash
git add internal/store/ internal/model/
git commit -m "feat: implement Pipeline and ActionChain store CRUD; update Action store for new schema"
```

---

## Task 5: Updated Pipeline Interface

**Files:**
- Modify: `internal/inference/pipeline.go`

- [ ] **Step 1: Replace Pipeline interface in `internal/inference/pipeline.go`**

Keep all existing sub-interfaces but update their embedded `Pipeline`. Drop `Load` and `RunBatch` from the base interface:

```go
package inference

import (
	"context"
	"io"
	"time"
)

// PipelineType mirrors model.PipelineType but lives in inference to avoid
// circular imports.
type PipelineType string

const (
	PipelineTypeONNX     PipelineType = "onnx"
	PipelineTypeOllama   PipelineType = "ollama"
	PipelineTypeInternal PipelineType = "internal"
)

// PipelineInput carries all data a pipeline needs to run.
type PipelineInput struct {
	// Text is the primary text to process (tweet body).
	Text string
	// Context is the accumulated flat map of outputs from upstream actions in a chain.
	Context map[string]any
	// Config is the pipeline-specific configuration from the DB pipeline row.
	Config map[string]any
}

// Pipeline is the interface every execution unit must implement.
type Pipeline interface {
	// Name returns the pipeline name as stored in the pipelines table.
	Name() string
	// Type returns the pipeline type (onnx, ollama, internal).
	Type() PipelineType
	// Run executes the pipeline and returns structured outputs.
	Run(ctx context.Context, input PipelineInput) (map[string]any, error)
	// Close releases any held resources.
	Close() error
}

// LabeledPipeline is implemented by zero-shot pipelines with dynamic labels.
type LabeledPipeline interface {
	Pipeline
	SetLabels(labels []string)
}

// SummarizationPipeline is implemented by seq2seq pipelines.
type SummarizationPipeline interface {
	Pipeline
	Summarize(ctx context.Context, text string) (string, error)
}

// ExtractionPipeline is implemented by geo-extraction pipelines.
type ExtractionPipeline interface {
	Pipeline
	Extract(ctx context.Context, text string) (*ExtractionResult, error)
}

// DigestPipeline is implemented by digest-capable pipelines.
type DigestPipeline interface {
	Pipeline
	Digest(ctx context.Context, tweets []string) (string, error)
}

// ConcurrencyLimiter is optionally implemented by pipelines that cap concurrency.
type ConcurrencyLimiter interface {
	MaxConcurrency() int
}

// ChatMessage is a single turn in a multi-turn conversation.
type ChatMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

// ChatPipeline is implemented by pipelines that support streaming chat.
type ChatPipeline interface {
	Pipeline
	ChatStream(ctx context.Context, tweetContext []string, messages []ChatMessage, w io.Writer) error
	EstimateTokens(ctx context.Context, texts []string) (estimated int, contextLimit int, err error)
}

// BackendConfig holds URL and optional display name for an Ollama backend.
type BackendConfig struct {
	URL  string
	Name string
}

// BackendStat is a runtime snapshot of one Ollama backend.
type BackendStat struct {
	Name         string     `json:"name"`
	URL          string     `json:"url"`
	Healthy      bool       `json:"healthy"`
	InUse        bool       `json:"in_use"`
	Score        float64    `json:"score"`
	RequestCount int64      `json:"request_count"`
	LastActiveAt *time.Time `json:"last_active_at,omitempty"`
}

// BackendStatter is implemented by pipelines managing a backend pool.
type BackendStatter interface {
	BackendStats() []BackendStat
}

// ExtractionResult is the structured output of the geo-event extraction pipeline.
// (Kept here so ExtractionPipeline can reference it without circular imports.)
type ExtractionResult struct {
	Description     string               `json:"description"`
	Locations       []ExtractionLocation `json:"locations"`
	GeolocatedBy    []string             `json:"geolocated_by"`
	GeolocationURLs []string             `json:"geolocation_urls"`
	ProofURLs       []string             `json:"proof_urls"`
}

// ExtractionLocation holds a single geographic location.
type ExtractionLocation struct {
	Label       string     `json:"label,omitempty"`
	Coordinates [2]float64 `json:"coordinates"`
	Place       string     `json:"place,omitempty"`
}
```

Note: `ExtractionResult` and `ExtractionLocation` are currently defined in `ollama.go`. Move them here, then remove from `ollama.go`.

- [ ] **Step 2: Compile to see all breakage**

```bash
go build ./... 2>&1 | head -50
```

This will show many errors across onnx.go, ollama.go, summarizer.go, registry.go, action/, worker/, api/, etc. These are fixed in subsequent tasks.

- [ ] **Step 3: Commit (with build errors — this is the interface declaration commit)**

```bash
git add internal/inference/pipeline.go
git commit -m "feat: update Pipeline interface — PipelineInput, map[string]any output, drop Load/RunBatch"
```

---

## Task 6: Update ONNX and Summarizer Pipeline Implementations

**Files:**
- Modify: `internal/inference/onnx.go`
- Modify: `internal/inference/summarizer.go`

- [ ] **Step 1: Update `ONNXPipeline` in `onnx.go`**

Add `Type()` method. Change `Run` signature. Add factory `NewONNXPipelineFromConfig`. Keep internal `Load` logic (call it from factory):

```go
// NewONNXPipelineFromConfig creates and loads an ONNX pipeline from DB pipeline config.
// config must contain "path" key with the model file path.
func NewONNXPipelineFromConfig(name string, config map[string]any) (*ONNXPipeline, error) {
	path, _ := config["path"].(string)
	if path == "" {
		return nil, fmt.Errorf("inference: onnx: config missing 'path'")
	}
	p := &ONNXPipeline{name: name}
	if err := p.load(path); err != nil {
		return nil, err
	}
	return p, nil
}

// load acquires the shared ONNX session (was previously the public Load method).
func (p *ONNXPipeline) load(modelPath string) error {
	sess, err := acquireSession(modelPath)
	if err != nil {
		return err
	}
	p.shared = sess
	return nil
}

func (p *ONNXPipeline) Type() PipelineType { return PipelineTypeONNX }

// Run runs the ONNX model on input.Text and returns structured outputs.
// For classifiers: {"label": "...", "confidence": 0.95, "scores": {...}}
// For NER: {"entities": [...], "entity_count": N}
func (p *ONNXPipeline) Run(ctx context.Context, input PipelineInput) (map[string]any, error) {
	if p.shared == nil {
		return nil, fmt.Errorf("inference: onnx: model not loaded")
	}
	// Use labels from input.Config if provided (zero-shot), else use pipeline labels.
	if labels, ok := input.Config["labels"].([]string); ok && len(labels) > 0 {
		p.SetLabels(labels)
	}
	raw, err := p.runRaw([]byte(input.Text))
	if err != nil {
		return nil, err
	}
	// Convert json.RawMessage → map[string]any
	var out map[string]any
	if err := json.Unmarshal(raw, &out); err != nil {
		return map[string]any{"raw": string(raw)}, nil
	}
	return out, nil
}
```

Rename the existing `Run(text []byte)` to `runRaw(text []byte)` (keep the body unchanged). This is the internal method.

Remove the public `Load` method (now replaced by `load`). Remove `RunBatch`.

- [ ] **Step 2: Update `summarizer.go`** (Seq2SeqPipeline) similarly

Add `Type()`, update `Run` to new signature. The Seq2SeqPipeline's `Summarize` method can call `runRaw` internally:

```go
func (p *Seq2SeqPipeline) Type() PipelineType { return PipelineTypeONNX }

func (p *Seq2SeqPipeline) Run(ctx context.Context, input PipelineInput) (map[string]any, error) {
	s, err := p.Summarize(ctx, input.Text)
	if err != nil {
		return nil, err
	}
	return map[string]any{"summary": s}, nil
}

// Summarize is the specialized method for seq2seq text generation.
// ctx is now threaded through.
func (p *Seq2SeqPipeline) Summarize(ctx context.Context, text string) (string, error) {
	// existing body unchanged
}
```

Note: `Summarize` in the current interface doesn't take `ctx`. The new `SummarizationPipeline` interface has `Summarize(ctx context.Context, text string)`. Update accordingly.

Add `NewSeq2SeqPipelineFromConfig`:
```go
func NewSeq2SeqPipelineFromConfig(name string, config map[string]any) (*Seq2SeqPipeline, error) {
	path, _ := config["path"].(string)
	if path == "" {
		return nil, fmt.Errorf("inference: seq2seq: config missing 'path'")
	}
	p := NewSeq2SeqPipeline(name)
	if err := p.load(path); err != nil {
		return nil, err
	}
	return p, nil
}
```

- [ ] **Step 3: Update `inference.Registry`** to remove model-specific loading logic

The Registry no longer calls `factory(m *model.Model)` — it just holds Pipeline instances. Simplify to:

```go
type Registry struct {
	mu       sync.RWMutex
	entries  []Pipeline
}

func NewRegistry() *Registry { return &Registry{} }

func (r *Registry) Register(p Pipeline) {
	r.mu.Lock()
	defer r.mu.Unlock()
	// replace existing by name or append
	for i, e := range r.entries {
		if e.Name() == p.Name() {
			r.entries[i] = p
			return
		}
	}
	r.entries = append(r.entries, p)
}

func (r *Registry) Get(name string) Pipeline {
	r.mu.RLock()
	defer r.mu.RUnlock()
	for _, e := range r.entries {
		if e.Name() == name { return e }
	}
	return nil
}

func (r *Registry) All() []Pipeline {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]Pipeline, len(r.entries))
	copy(out, r.entries)
	return out
}

func (r *Registry) SetLabels(name string, labels []string) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	for _, e := range r.entries {
		if lp, ok := e.(LabeledPipeline); ok && e.Name() == name {
			lp.SetLabels(labels)
			return
		}
	}
}
```

The old `Load`, `Reload`, `AllForRules`, `NotifyChan` methods are removed. Rule filtering is no longer done at the pipeline level — it's in the ActionCache. The `reloadCh` is removed.

- [ ] **Step 4: Compile inference package**

```bash
go build ./internal/inference/... 2>&1
```

Fix any remaining compile errors.

- [ ] **Step 5: Run inference tests**

```bash
go test ./internal/inference/... 2>&1
```

Expected: PASS (core logic unchanged, only signatures updated)

- [ ] **Step 6: Commit**

```bash
git add internal/inference/
git commit -m "feat: update ONNX and Seq2Seq pipeline implementations for new Pipeline interface"
```

---

## Task 7: Update Ollama Pipeline Implementation

**Files:**
- Modify: `internal/inference/ollama.go`

- [ ] **Step 1: Add `Type()` and update `Run` signature**

```go
func (p *OllamaPipeline) Type() PipelineType { return PipelineTypeOllama }

func (p *OllamaPipeline) Run(ctx context.Context, input PipelineInput) (map[string]any, error) {
	// For extraction pipeline: use Extract internally
	// For digest/summarizer: not called via Run (called via Digest/ChatStream)
	// For generic: run generate and parse JSON response
	raw, err := p.runRaw(ctx, []byte(input.Text))
	if err != nil {
		return nil, err
	}
	var out map[string]any
	if err := json.Unmarshal(raw, &out); err != nil {
		return map[string]any{"raw": string(raw)}, nil
	}
	return out, nil
}
```

Rename existing `Run(text []byte) json.RawMessage` to `runRaw(ctx context.Context, text []byte) (json.RawMessage, error)`. Update its signature to accept context and thread `ctx` through the HTTP call.

Update `Extract`, `Summarize`, `Digest`, `ChatStream`, `EstimateTokens` to accept `ctx context.Context` as first argument (to match updated sub-interfaces).

Remove `RunBatch`. Remove `Load` (path is in config; the Ollama pipeline doesn't load a file — it uses model name from config).

Add factory:
```go
// NewOllamaPipelineFromConfig creates an Ollama pipeline from DB pipeline config.
// config must contain "model" key (Ollama model name) and optionally "system_prompt".
func NewOllamaPipelineFromConfig(name string, config map[string]any, backends []BackendConfig, fastWaitTimeout time.Duration) *OllamaPipeline {
	modelName, _ := config["model"].(string)
	if modelName == "" {
		modelName = name
	}
	systemPrompt, _ := config["system_prompt"].(string)
	if systemPrompt == "" {
		systemPrompt = OllamaGenericSystem
	}
	return NewOllamaPipeline(name, backends, systemPrompt, fastWaitTimeout)
	// Note: the existing NewOllamaPipeline already sets the model name from the name param
}
```

- [ ] **Step 2: Update `ExtractionResult` and `ExtractionLocation`** — remove from ollama.go since they're now in pipeline.go

Search for where these types are defined in ollama.go and delete them (they were moved to pipeline.go in Task 5).

- [ ] **Step 3: Compile**

```bash
go build ./internal/inference/... 2>&1
```

- [ ] **Step 4: Run tests**

```bash
go test ./internal/inference/... -v 2>&1 | tail -20
```

- [ ] **Step 5: Commit**

```bash
git add internal/inference/
git commit -m "feat: update Ollama pipeline for new Pipeline interface; add NewOllamaPipelineFromConfig"
```

---

## Task 8: Internal Pipeline Package

**Files:**
- Create: `internal/pipeline/registry.go`
- Create: `internal/pipeline/entity_matcher.go`
- Create: `internal/pipeline/twitter.go`
- Create: `internal/pipeline/notification.go`

- [ ] **Step 1: Create `internal/pipeline/registry.go`**

```go
// Package pipeline provides the internal pipeline registry and built-in
// pipeline implementations (entity matcher, twitter, notification).
package pipeline

import (
	"fmt"

	"github.com/amir-the-h/iran-watcher/internal/inference"
)

// Factory creates a Pipeline instance from DB config.
type Factory func(name string, config map[string]any) (inference.Pipeline, error)

// Registry maps handler names to factories for internal pipelines.
type Registry struct {
	factories map[string]Factory
}

// NewRegistry creates an empty Registry.
func NewRegistry() *Registry {
	return &Registry{factories: make(map[string]Factory)}
}

// Register associates a handler name with a factory function.
func (r *Registry) Register(handler string, f Factory) {
	r.factories[handler] = f
}

// Handlers returns all registered handler names.
func (r *Registry) Handlers() []string {
	out := make([]string, 0, len(r.factories))
	for h := range r.factories {
		out = append(out, h)
	}
	return out
}

// Build instantiates a pipeline using the factory for the given handler.
// config must contain "handler" key matching a registered name.
func (r *Registry) Build(name string, config map[string]any) (inference.Pipeline, error) {
	handler, _ := config["handler"].(string)
	if handler == "" {
		return nil, fmt.Errorf("pipeline: registry: config missing 'handler'")
	}
	f, ok := r.factories[handler]
	if !ok {
		return nil, fmt.Errorf("pipeline: registry: unknown handler %q", handler)
	}
	return f(name, config)
}
```

- [ ] **Step 2: Create `internal/pipeline/entity_matcher.go`**

```go
package pipeline

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/amir-the-h/iran-watcher/internal/inference"
	"github.com/amir-the-h/iran-watcher/internal/model"
	"github.com/amir-the-h/iran-watcher/internal/store"
)

// EntityMatcherPipeline matches active tracked entities in tweet text.
// Outputs: {"matched_ids": [1,2], "matched": true}
type EntityMatcherPipeline struct {
	name  string
	store store.Store
}

// NewEntityMatcherFactory returns a Factory that creates EntityMatcherPipeline.
// The store must be injected at registration time.
func NewEntityMatcherFactory(st store.Store) Factory {
	return func(name string, _ map[string]any) (inference.Pipeline, error) {
		return &EntityMatcherPipeline{name: name, store: st}, nil
	}
}

func (p *EntityMatcherPipeline) Name() string                     { return p.name }
func (p *EntityMatcherPipeline) Type() inference.PipelineType     { return inference.PipelineTypeInternal }
func (p *EntityMatcherPipeline) Close() error                     { return nil }

func (p *EntityMatcherPipeline) Run(ctx context.Context, input inference.PipelineInput) (map[string]any, error) {
	entities, err := p.store.ListActiveTrackedEntities(ctx)
	if err != nil {
		return nil, fmt.Errorf("entity_matcher: list entities: %w", err)
	}

	// Decode NER entities from context if present (populated by an upstream NER pipeline).
	var nerEntities []model.NEREntity
	if raw, ok := input.Context["entities"]; ok {
		if b, err := json.Marshal(raw); err == nil {
			_ = json.Unmarshal(b, &nerEntities)
		}
	}

	textLower := strings.ToLower(input.Text)
	var matchedIDs []int64
	for _, entity := range entities {
		candidates := append([]string{entity.Name}, entity.Aliases...)
		for _, c := range candidates {
			cLower := strings.ToLower(c)
			matched := false
			for _, ne := range nerEntities {
				if strings.ToLower(ne.Word) == cLower {
					matched = true
					break
				}
			}
			if !matched {
				matched = strings.Contains(textLower, cLower)
			}
			if matched {
				matchedIDs = append(matchedIDs, entity.ID)
				break
			}
		}
	}

	return map[string]any{
		"matched":     len(matchedIDs) > 0,
		"matched_ids": matchedIDs,
	}, nil
}
```

- [ ] **Step 3: Create `internal/pipeline/twitter.go`**

```go
package pipeline

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/amir-the-h/iran-watcher/internal/inference"
	"github.com/amir-the-h/iran-watcher/internal/twitter"
)

type twitterClient interface {
	Retweet(ctx context.Context, tweetID string) (string, error)
	Like(ctx context.Context, tweetID string) error
	QuoteTweet(ctx context.Context, tweetID, text string) (string, error)
}

// RetweetPipeline retweeets the tweet identified in context["tweet_id"].
// Outputs: {"retweet_id": "..."}
type RetweetPipeline struct {
	name   string
	client twitterClient
}

func NewRetweetFactory(client *twitter.Client) Factory {
	return func(name string, _ map[string]any) (inference.Pipeline, error) {
		return &RetweetPipeline{name: name, client: client}, nil
	}
}

func (p *RetweetPipeline) Name() string                     { return p.name }
func (p *RetweetPipeline) Type() inference.PipelineType     { return inference.PipelineTypeInternal }
func (p *RetweetPipeline) Close() error                     { return nil }

func (p *RetweetPipeline) Run(ctx context.Context, input inference.PipelineInput) (map[string]any, error) {
	tweetID, _ := input.Context["tweet_id"].(string)
	if tweetID == "" {
		return nil, fmt.Errorf("retweet: missing tweet_id in context")
	}
	if p.client == nil {
		return nil, fmt.Errorf("retweet: twitter client not configured")
	}
	retweetID, err := p.client.Retweet(ctx, tweetID)
	if err != nil {
		var rl *twitter.RateLimitError
		if errors.As(err, &rl) {
			return map[string]any{"rate_limited": true}, nil
		}
		return nil, fmt.Errorf("retweet: %w", err)
	}
	return map[string]any{"retweet_id": retweetID}, nil
}

// LikePipeline likes the tweet identified in context["tweet_id"].
// Outputs: {"liked": true}
type LikePipeline struct {
	name   string
	client twitterClient
}

func NewLikeFactory(client *twitter.Client) Factory {
	return func(name string, _ map[string]any) (inference.Pipeline, error) {
		return &LikePipeline{name: name, client: client}, nil
	}
}

func (p *LikePipeline) Name() string                     { return p.name }
func (p *LikePipeline) Type() inference.PipelineType     { return inference.PipelineTypeInternal }
func (p *LikePipeline) Close() error                     { return nil }

func (p *LikePipeline) Run(ctx context.Context, input inference.PipelineInput) (map[string]any, error) {
	tweetID, _ := input.Context["tweet_id"].(string)
	if tweetID == "" {
		return nil, fmt.Errorf("like: missing tweet_id in context")
	}
	if p.client == nil {
		return nil, fmt.Errorf("like: twitter client not configured")
	}
	if err := p.client.Like(ctx, tweetID); err != nil {
		var rl *twitter.RateLimitError
		if errors.As(err, &rl) {
			return map[string]any{"rate_limited": true}, nil
		}
		return nil, fmt.Errorf("like: %w", err)
	}
	return map[string]any{"liked": true}, nil
}

// QuotePipeline quotes the tweet using a template from pipeline config.
// config["template"] supports {text} and {author} placeholders.
// Outputs: {"quote_tweet_id": "..."}
type QuotePipeline struct {
	name     string
	client   twitterClient
	template string
}

func NewQuoteFactory(client *twitter.Client) Factory {
	return func(name string, config map[string]any) (inference.Pipeline, error) {
		tmpl, _ := config["template"].(string)
		return &QuotePipeline{name: name, client: client, template: tmpl}, nil
	}
}

func (p *QuotePipeline) Name() string                     { return p.name }
func (p *QuotePipeline) Type() inference.PipelineType     { return inference.PipelineTypeInternal }
func (p *QuotePipeline) Close() error                     { return nil }

func (p *QuotePipeline) Run(ctx context.Context, input inference.PipelineInput) (map[string]any, error) {
	tweetID, _ := input.Context["tweet_id"].(string)
	author, _ := input.Context["tweet_author"].(string)
	if tweetID == "" {
		return nil, fmt.Errorf("quote: missing tweet_id in context")
	}
	if p.client == nil {
		return nil, fmt.Errorf("quote: twitter client not configured")
	}
	text := p.template
	text = strings.ReplaceAll(text, "{text}", input.Text)
	text = strings.ReplaceAll(text, "{author}", author)

	quoteID, err := p.client.QuoteTweet(ctx, tweetID, text)
	if err != nil {
		var rl *twitter.RateLimitError
		if errors.As(err, &rl) {
			return map[string]any{"rate_limited": true}, nil
		}
		return nil, fmt.Errorf("quote: %w", err)
	}
	return map[string]any{"quote_tweet_id": quoteID}, nil
}
```

- [ ] **Step 4: Create `internal/pipeline/notification.go`**

```go
package pipeline

import (
	"context"
	"fmt"

	"github.com/amir-the-h/iran-watcher/internal/inference"
	"github.com/amir-the-h/iran-watcher/internal/notifier"
)

// PushoverPipeline sends a Pushover notification.
// Outputs: {"sent": true, "provider": "pushover"}
type PushoverPipeline struct {
	name   string
	client *notifier.PushoverClient
}

func NewPushoverFactory(client *notifier.PushoverClient) Factory {
	return func(name string, _ map[string]any) (inference.Pipeline, error) {
		return &PushoverPipeline{name: name, client: client}, nil
	}
}

func (p *PushoverPipeline) Name() string                     { return p.name }
func (p *PushoverPipeline) Type() inference.PipelineType     { return inference.PipelineTypeInternal }
func (p *PushoverPipeline) Close() error                     { return nil }

func (p *PushoverPipeline) Run(ctx context.Context, input inference.PipelineInput) (map[string]any, error) {
	if p.client == nil {
		return nil, fmt.Errorf("pushover: client not configured")
	}
	title, _ := input.Config["title"].(string)
	if title == "" {
		title = "Iran Watcher Alert"
	}
	if err := p.client.Send(ctx, title, input.Text); err != nil {
		return nil, fmt.Errorf("pushover: send: %w", err)
	}
	return map[string]any{"sent": true, "provider": "pushover"}, nil
}

// TelegramPipeline sends a Telegram notification.
// Outputs: {"sent": true, "provider": "telegram"}
type TelegramPipeline struct {
	name   string
	client *notifier.TelegramClient
}

func NewTelegramFactory(client *notifier.TelegramClient) Factory {
	return func(name string, _ map[string]any) (inference.Pipeline, error) {
		return &TelegramPipeline{name: name, client: client}, nil
	}
}

func (p *TelegramPipeline) Name() string                     { return p.name }
func (p *TelegramPipeline) Type() inference.PipelineType     { return inference.PipelineTypeInternal }
func (p *TelegramPipeline) Close() error                     { return nil }

func (p *TelegramPipeline) Run(ctx context.Context, input inference.PipelineInput) (map[string]any, error) {
	if p.client == nil {
		return nil, fmt.Errorf("telegram: client not configured")
	}
	chatID, _ := input.Config["chat_id"].(string)
	if err := p.client.Send(ctx, chatID, input.Text); err != nil {
		return nil, fmt.Errorf("telegram: send: %w", err)
	}
	return map[string]any{"sent": true, "provider": "telegram"}, nil
}
```

Note: `PushoverClient.Send` and `TelegramClient.Send` may not exist by this exact signature — check `internal/notifier/` and adapt. The key pattern is: take ctx + message text, send the notification.

- [ ] **Step 5: Compile**

```bash
go build ./internal/pipeline/... 2>&1
```

- [ ] **Step 6: Commit**

```bash
git add internal/pipeline/
git commit -m "feat: internal pipeline package — registry, entity_matcher, twitter, notification pipelines"
```

---

## Task 9: ActionCache

**Files:**
- Create: `internal/worker/action_cache.go`

- [ ] **Step 1: Create `internal/worker/action_cache.go`**

```go
package worker

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"sync"

	"github.com/amir-the-h/iran-watcher/internal/inference"
	"github.com/amir-the-h/iran-watcher/internal/model"
	"github.com/amir-the-h/iran-watcher/internal/pipeline"
	"github.com/amir-the-h/iran-watcher/internal/store"
)

// CacheEntry pairs an Action DB row with its resolved Pipeline instance.
type CacheEntry struct {
	Action   *model.Action
	Pipeline inference.Pipeline
}

// ActionCache is a hot-reloadable in-memory map of action_id → CacheEntry.
// Shared (read-mostly) between the stream listener and all workers.
type ActionCache struct {
	mu       sync.RWMutex
	entries  map[int64]*CacheEntry // keyed by action ID

	store       store.Store
	infReg      *inference.Registry  // for onnx/ollama pipelines
	pipeReg     *pipeline.Registry   // for internal pipelines
	backends    []inference.BackendConfig
	fastTimeout int // seconds for Ollama fast-wait
	logger      *slog.Logger
}

// NewActionCache creates a new ActionCache with the given dependencies.
func NewActionCache(
	st store.Store,
	infReg *inference.Registry,
	pipeReg *pipeline.Registry,
	backends []inference.BackendConfig,
	logger *slog.Logger,
) *ActionCache {
	return &ActionCache{
		entries: make(map[int64]*CacheEntry),
		store:   st,
		infReg:  infReg,
		pipeReg: pipeReg,
		backends: backends,
		logger:  logger,
	}
}

// Load (re)builds the entire cache from active actions in the DB.
func (c *ActionCache) Load(ctx context.Context) error {
	active := true
	actions, err := c.store.ListActions(ctx, store.ActionFilter{Active: &active})
	if err != nil {
		return fmt.Errorf("action_cache: list actions: %w", err)
	}

	entries := make(map[int64]*CacheEntry, len(actions))
	for _, a := range actions {
		p, pErr := c.resolvePipeline(ctx, a.PipelineID)
		if pErr != nil {
			if c.logger != nil {
				c.logger.Warn("action_cache: skip action (pipeline resolution failed)",
					"action_id", a.ID, "pipeline_id", a.PipelineID, "error", pErr)
			}
			continue
		}
		entries[a.ID] = &CacheEntry{Action: a, Pipeline: p}
	}

	c.mu.Lock()
	c.entries = entries
	c.mu.Unlock()
	return nil
}

// Get returns the CacheEntry for the given action ID, or nil if not found.
func (c *ActionCache) Get(actionID int64) *CacheEntry {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.entries[actionID]
}

// AllForRules returns CacheEntries whose actions should fire for the given rule IDs.
// Global actions (empty rule_ids) are always included.
func (c *ActionCache) AllForRules(matchedRuleIDs []int64) []*CacheEntry {
	ruleSet := make(map[int64]struct{}, len(matchedRuleIDs))
	for _, id := range matchedRuleIDs {
		ruleSet[id] = struct{}{}
	}

	c.mu.RLock()
	defer c.mu.RUnlock()

	var out []*CacheEntry
	for _, e := range c.entries {
		if !e.Action.Active {
			continue
		}
		if len(e.Action.RuleIDs) == 0 {
			out = append(out, e)
			continue
		}
		for _, rid := range e.Action.RuleIDs {
			if _, ok := ruleSet[rid]; ok {
				out = append(out, e)
				break
			}
		}
	}
	return out
}

// AllForTrigger returns CacheEntries for the given trigger type, filtered by rule IDs.
func (c *ActionCache) AllForTrigger(trigger model.TriggerType, matchedRuleIDs []int64) []*CacheEntry {
	all := c.AllForRules(matchedRuleIDs)
	var out []*CacheEntry
	for _, e := range all {
		if e.Action.TriggerType == trigger {
			out = append(out, e)
		}
	}
	return out
}

// FindByType returns the first active pipeline matching the given inference type.
// Used by chat/digest endpoints to locate an Ollama pipeline.
func (c *ActionCache) FindByType(pipelineType inference.PipelineType) inference.Pipeline {
	c.mu.RLock()
	defer c.mu.RUnlock()
	for _, e := range c.entries {
		if e.Pipeline.Type() == pipelineType {
			return e.Pipeline
		}
	}
	return nil
}

func (c *ActionCache) resolvePipeline(ctx context.Context, pipelineID int64) (inference.Pipeline, error) {
	row, err := c.store.GetPipeline(ctx, pipelineID)
	if err != nil {
		return nil, fmt.Errorf("get pipeline row: %w", err)
	}

	// Parse config JSON.
	var cfg map[string]any
	if err := json.Unmarshal(row.Config, &cfg); err != nil {
		cfg = make(map[string]any)
	}

	switch row.Type {
	case model.PipelineTypeONNX:
		// Check if already loaded in inference registry (shared session cache).
		if p := c.infReg.Get(row.Name); p != nil {
			return p, nil
		}
		p, err := inference.NewONNXPipelineFromConfig(row.Name, cfg)
		if err != nil {
			return nil, fmt.Errorf("onnx: %w", err)
		}
		// Load labels if any.
		labels, _ := c.store.ListPipelineLabels(ctx, pipelineID)
		if len(labels) > 0 {
			if lp, ok := p.(inference.LabeledPipeline); ok {
				lp.SetLabels(labels)
			}
		}
		c.infReg.Register(p)
		return p, nil

	case model.PipelineTypeOllama:
		if p := c.infReg.Get(row.Name); p != nil {
			return p, nil
		}
		// Determine system prompt from model_type or explicit config.
		p := inference.NewOllamaPipelineFromConfig(row.Name, cfg, c.backends, 0)
		c.infReg.Register(p)
		return p, nil

	case model.PipelineTypeInternal:
		return c.pipeReg.Build(row.Name, cfg)

	default:
		return nil, fmt.Errorf("unknown pipeline type: %s", row.Type)
	}
}
```

- [ ] **Step 2: Compile**

```bash
go build ./internal/worker/... 2>&1
```

Fix any import errors.

- [ ] **Step 3: Commit**

```bash
git add internal/worker/action_cache.go
git commit -m "feat: ActionCache — hot-reloadable action_id to pipeline mapping"
```

---

## Task 10: Queue Task Type and Worker Simplification

**Files:**
- Modify: `internal/queue/queue.go`
- Modify: `internal/queue/redis.go`
- Modify: `internal/worker/manager.go`

- [ ] **Step 1: Update `internal/queue/queue.go`** — add Task type, QueueTasks const

```go
package queue

import (
	"context"
	"encoding/json"
	"time"
)

const (
	QueueTasks  = "iran-watcher:tasks"
	QueueFailed = "iran-watcher:failed"
	// QueueRaw is kept for backward-compat during transition but deprecated.
	QueueRaw = "iran-watcher:raw"
)

// Task is a single unit of work pushed to iran-watcher:tasks.
type Task struct {
	TriggerType string             `json:"trigger_type"`
	Event       json.RawMessage    `json:"event"`      // serialised trigger payload
	ActionID    int64              `json:"action_id"`
	Context     map[string]any     `json:"context"`    // accumulated outputs from upstream actions
}

// TweetEvent is the Event payload for trigger_type=new_tweet.
type TweetEvent struct {
	TweetID      string  `json:"tweet_id"`
	Text         string  `json:"text"`
	AuthorHandle string  `json:"author_handle"`
	RuleIDs      []int64 `json:"rule_ids"`
}

// FailedTweet wraps a Task with error annotations for dead-letter storage.
type FailedTweet struct {
	Task          *Task         `json:"task"`
	FailedModels  []FailedModel `json:"failed_models"`
}

// FailedModel records which pipeline failed and why.
type FailedModel struct {
	Name      string    `json:"name"`
	Error     string    `json:"error"`
	Timestamp time.Time `json:"timestamp"`
}

// Producer pushes tasks onto a queue.
type Producer interface {
	PushTask(ctx context.Context, task *Task) error
	PushFailed(ctx context.Context, ft *FailedTweet) error
}

// Consumer pops tasks from a queue.
type Consumer interface {
	PopTask(ctx context.Context, timeout time.Duration) (*Task, error)
	PopFailed(ctx context.Context, timeout time.Duration) (*FailedTweet, error)
}

// Sizer reports the number of items in a named queue.
type Sizer interface {
	Len(ctx context.Context, queue string) (int64, error)
}

// FailureLister enumerates and clears dead-letter queue entries.
type FailureLister interface {
	ListFailed(ctx context.Context, offset, limit int64) ([]*FailedTweet, error)
	ClearFailed(ctx context.Context) error
	FlushQueue(ctx context.Context) error
}
```

- [ ] **Step 2: Update `internal/queue/redis.go`** — implement PushTask/PopTask

Replace the existing `Push`/`Pop` methods with:

```go
func (r *RedisQueue) PushTask(ctx context.Context, task *Task) error {
	b, err := json.Marshal(task)
	if err != nil {
		return fmt.Errorf("queue: marshal task: %w", err)
	}
	if err := r.client.LPush(ctx, QueueTasks, b).Err(); err != nil {
		return fmt.Errorf("queue: push task: %w", err)
	}
	return nil
}

func (r *RedisQueue) PopTask(ctx context.Context, timeout time.Duration) (*Task, error) {
	res, err := r.client.BRPop(ctx, timeout, QueueTasks).Result()
	if err != nil {
		if errors.Is(err, redis.Nil) {
			return nil, nil // timeout
		}
		return nil, fmt.Errorf("queue: pop task: %w", err)
	}
	if len(res) < 2 {
		return nil, nil
	}
	var t Task
	if err := json.Unmarshal([]byte(res[1]), &t); err != nil {
		return nil, fmt.Errorf("queue: pop task: unmarshal: %w", err)
	}
	return &t, nil
}
```

Update `PushFailed`/`PopFailed` to use the new `FailedTweet` struct (which now wraps a `Task` instead of `StreamTweet`).

- [ ] **Step 3: Rewrite `internal/worker/manager.go`** — simplified worker loop

Remove: `dispatcher()`, `syncActionChans()`, `runWorker()`, `schedOrder`, `schedNames`, `actionChs`, `actionLimits`, `actionTrackers`. Keep: `Stats()`, `Flush()`, `FlushPipeline()`.

```go
package worker

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"sync"
	"sync/atomic"
	"time"

	"github.com/amir-the-h/iran-watcher/internal/model"
	"github.com/amir-the-h/iran-watcher/internal/queue"
	"github.com/amir-the-h/iran-watcher/internal/store"
)

// Manager launches N worker goroutines that pop Tasks from Redis.
type Manager struct {
	totalWorkers int
	consumer     queue.Consumer
	producer     queue.Producer
	store        store.Store
	cache        *ActionCache
	logger       *slog.Logger
	active       atomic.Int32
}

func NewManager(total int, consumer queue.Consumer, producer queue.Producer, st store.Store, cache *ActionCache, logger *slog.Logger) *Manager {
	return &Manager{
		totalWorkers: total,
		consumer:     consumer,
		producer:     producer,
		store:        st,
		cache:        cache,
		logger:       logger,
	}
}

func (m *Manager) Stats() PoolStats {
	active := int(m.active.Load())
	return PoolStats{
		Total:  m.totalWorkers,
		Active: active,
		Idle:   m.totalWorkers - active,
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

func (m *Manager) runWorker(ctx context.Context, id int) {
	for {
		select {
		case <-ctx.Done():
			return
		default:
		}

		task, err := m.consumer.PopTask(ctx, 3*time.Second)
		if err != nil {
			if m.logger != nil {
				m.logger.Error("worker: pop task", "worker_id", id, "error", err)
			}
			continue
		}
		if task == nil {
			continue // timeout
		}
		m.processTask(ctx, id, task)
	}
}

func (m *Manager) processTask(ctx context.Context, workerID int, task *queue.Task) {
	m.active.Add(1)
	defer m.active.Add(-1)

	entry := m.cache.Get(task.ActionID)
	if entry == nil {
		if m.logger != nil {
			m.logger.Warn("worker: unknown action_id in task",
				"action_id", task.ActionID, "trigger", task.TriggerType)
		}
		return
	}

	// Decode tweet event to extract text.
	var ev queue.TweetEvent
	_ = json.Unmarshal(task.Event, &ev)

	// Merge task context with tweet_id so downstream pipelines can use it.
	mergedCtx := make(map[string]any, len(task.Context)+3)
	for k, v := range task.Context {
		mergedCtx[k] = v
	}
	mergedCtx["tweet_id"] = ev.TweetID
	mergedCtx["tweet_author"] = ev.AuthorHandle

	input := inference.PipelineInput{
		Text:    ev.Text,
		Context: mergedCtx,
		Config:  configFromPipeline(entry.Pipeline),
	}

	startTime := time.Now()
	outputs, runErr := entry.Pipeline.Run(ctx, input)

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
	if saveErr := m.store.SaveActionExecution(ctx, exec); saveErr != nil && m.logger != nil {
		m.logger.Warn("worker: save action execution", "error", saveErr)
	}

	if runErr != nil || outputs == nil {
		return
	}

	// Save outputs to action_execution_metadata.
	for k, v := range outputs {
		val := fmt.Sprintf("%v", v)
		if b, err := json.Marshal(v); err == nil {
			val = string(b)
		}
		_ = m.store.SaveActionExecutionMetadata(ctx, exec.ID, k, val)
	}

	// Write categorization row for onnx/ollama pipelines (backward compat).
	if entry.Pipeline.Type() != inference.PipelineTypeInternal && ev.TweetID != "" {
		m.saveCategorization(ctx, entry.Pipeline.Name(), ev.TweetID, outputs)
	}

	// Merge outputs into context for chain evaluation.
	for k, v := range outputs {
		mergedCtx[k] = v
	}

	// Evaluate chains and push downstream tasks.
	m.evaluateChains(ctx, task.ActionID, task.TriggerType, task.Event, mergedCtx)
}

func (m *Manager) saveCategorization(ctx context.Context, pipelineName, tweetID string, outputs map[string]any) {
	b, err := json.Marshal(outputs)
	if err != nil {
		return
	}
	c := &model.Categorization{
		TweetID:   tweetID,
		ModelName: pipelineName,
		Results:   b,
		CreatedAt: time.Now(),
	}
	if err := m.store.SaveCategorization(ctx, c); err != nil && m.logger != nil {
		m.logger.Warn("worker: save categorization", "pipeline", pipelineName, "tweet_id", tweetID, "error", err)
	}
}

func (m *Manager) evaluateChains(ctx context.Context, sourceActionID int64, triggerType, event json.RawMessage, mergedCtx map[string]any) {
	chains, err := m.store.ListActionChains(ctx, store.ActionChainFilter{SourceActionID: &sourceActionID})
	if err != nil {
		if m.logger != nil {
			m.logger.Warn("worker: list chains", "source_action_id", sourceActionID, "error", err)
		}
		return
	}
	for _, chain := range chains {
		if !evaluateConditions(chain.Conditions, mergedCtx) {
			continue
		}
		downstream := &queue.Task{
			TriggerType: triggerType,
			Event:       event,
			ActionID:    chain.TargetActionID,
			Context:     mergedCtx,
		}
		if pushErr := m.producer.PushTask(ctx, downstream); pushErr != nil && m.logger != nil {
			m.logger.Warn("worker: push chain task",
				"target_action_id", chain.TargetActionID, "error", pushErr)
		}
	}
}

// evaluateConditions returns true if all conditions pass (implicit AND).
// Empty conditions always pass.
func evaluateConditions(conds []model.ChainCondition, ctx map[string]any) bool {
	for _, c := range conds {
		val := fmt.Sprintf("%v", ctx[c.Key])
		if !evalCondition(c.Operator, val, c.Value) {
			return false
		}
	}
	return true
}

func evalCondition(op, actual, expected string) bool {
	switch op {
	case "==":
		return actual == expected
	case "!=":
		return actual != expected
	case "is_empty":
		return actual == "" || actual == "null" || actual == "[]"
	case "not_empty":
		return actual != "" && actual != "null" && actual != "[]"
	case "contains":
		return strings.Contains(actual, expected)
	case "not_contains":
		return !strings.Contains(actual, expected)
	default:
		return false
	}
}

func configFromPipeline(p inference.Pipeline) map[string]any {
	// Pipelines carry their config via DB row; we don't re-pass it here.
	// Callers that need config access it from the CacheEntry.
	return nil
}

// Flush and FlushPipeline are no-ops in the simplified model.
func (m *Manager) Flush() int        { return 0 }
func (m *Manager) FlushPipeline(_ string) int { return 0 }
```

Note: add `"strings"` to imports. The `inference` package needs to be imported too.

- [ ] **Step 4: Remove now-unused files from worker/**

```bash
# Check which files are now dead code
ls internal/worker/
# pipeline_task.go — remove (ActionTask replaced by queue.Task)
# entity_matcher.go — remove (moved to internal/pipeline/entity_matcher.go)
# worker.go — check if still needed
rm internal/worker/pipeline_task.go
rm internal/worker/entity_matcher.go
```

- [ ] **Step 5: Compile**

```bash
go build ./internal/worker/... ./internal/queue/... 2>&1
```

Fix import errors.

- [ ] **Step 6: Commit**

```bash
git add internal/queue/ internal/worker/
git commit -m "feat: queue Task type; simplified worker — BRPOP tasks, chain evaluation, no dispatcher"
```

---

## Task 11: Stream Listener Update + Remove internal/action

**Files:**
- Modify: `internal/stream/client.go`
- Delete: `internal/action/` (entire package)

- [ ] **Step 1: Update stream listener to push Tasks**

In `internal/stream/client.go`, the tweet handler currently calls `q.Push(ctx, queue.QueueRaw, &streamTweet)`. Replace with:

```go
// After saving the tweet and its rules, push one Task per matching action.
ruleIDs, _ := s.store.ListTweetRuleIDs(ctx, tweet.ID)
entries := s.cache.AllForTrigger(model.TriggerTypeNewTweet, ruleIDs)

eventPayload, _ := json.Marshal(queue.TweetEvent{
    TweetID:      tweet.ID,
    Text:         tweet.Text,
    AuthorHandle: tweet.AuthorHandle,
    RuleIDs:      ruleIDs,
})

for _, e := range entries {
    task := &queue.Task{
        TriggerType: string(model.TriggerTypeNewTweet),
        Event:       eventPayload,
        ActionID:    e.Action.ID,
        Context:     map[string]any{},
    }
    if err := s.producer.PushTask(ctx, task); err != nil && s.logger != nil {
        s.logger.Warn("stream: push task",
            "tweet_id", tweet.ID, "action_id", e.Action.ID, "error", err)
    }
}
```

The stream client needs a reference to `*worker.ActionCache`. Update the constructor: `NewClient(..., cache *worker.ActionCache, ...)`. Change the field `reg *action.Registry` to `cache *worker.ActionCache`.

- [ ] **Step 2: Remove `internal/action/` package**

```bash
rm -rf internal/action/
```

- [ ] **Step 3: Fix all compilation errors** from removing action package

Search for any remaining imports of `internal/action` and update them:

```bash
grep -rl '"github.com/amir-the-h/iran-watcher/internal/action"' --include="*.go" .
```

For each file found: remove the import and update the code to use the new system (ActionCache, etc.).

- [ ] **Step 4: Update `cli/cmd/serve.go`** — rebuild wiring

Replace `loadActionRegistry`, `buildAction`, `migrateModelsToActions` with ActionCache construction. Replace `action.Registry` with `worker.ActionCache`. Register internal pipeline factories. Key changes:

```go
// Build internal pipeline registry
pipeReg := pipeline.NewRegistry()
pipeReg.Register("entity_matcher", pipeline.NewEntityMatcherFactory(s))
if twitterClient != nil {
    pipeReg.Register("twitter_retweet", pipeline.NewRetweetFactory(twitterClient))
    pipeReg.Register("twitter_like", pipeline.NewLikeFactory(twitterClient))
    pipeReg.Register("twitter_quote", pipeline.NewQuoteFactory(twitterClient))
}
if pushClient != nil {
    pipeReg.Register("notification_push", pipeline.NewPushoverFactory(pushClient))
}
if telegramClient != nil {
    pipeReg.Register("notification_tg", pipeline.NewTelegramFactory(telegramClient))
}

// Build action cache
cache := worker.NewActionCache(s, reg, pipeReg, ollamaBackends, logger)
if err := cache.Load(ctx); err != nil {
    return fmt.Errorf("failed to load action cache: %w", err)
}

// Stream client uses cache instead of action registry
streamClient := stream.NewClient(cfg.X.BearerToken, s, q, rules, emitter, cache, logger)

// Worker manager uses cache
mgr := worker.NewManager(workerCount, q, q, s, cache, logger)
```

- [ ] **Step 5: Compile everything**

```bash
go build ./... 2>&1 | head -50
```

Fix remaining errors. Common issues: old `model.ActionType` references, old `action.Registry` usages in API server, old `StreamTweet` queue references.

- [ ] **Step 6: Run unit tests**

```bash
go test ./... 2>&1 | tail -30
```

- [ ] **Step 7: Commit**

```bash
git add -A
git commit -m "feat: stream listener pushes Tasks via ActionCache; remove internal/action package; rewire serve.go"
```

---

## Task 12: REST API — /api/pipelines and /api/actions Updates

**Files:**
- Create: `internal/api/handlers_pipelines.go`
- Create: `internal/api/handlers_chains.go`
- Create: `internal/api/handlers_triggers.go`
- Modify: `internal/api/handlers_actions.go`
- Modify: `internal/api/server.go`
- Delete: `internal/api/handlers_models.go`

- [ ] **Step 1: Delete handlers_models.go**

```bash
rm internal/api/handlers_models.go
```

- [ ] **Step 2: Create `internal/api/handlers_pipelines.go`**

Implement handlers mirroring the pattern in `handlers_actions.go`. Routes: GET/POST `/pipelines`, PUT/DELETE `/pipelines/{id}`, POST `/pipelines/{id}/activate`, POST `/pipelines/{id}/deactivate`, GET `/pipelines/handlers`, POST `/pipelines/download`.

```go
package api

import (
	"encoding/json"
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"
	"github.com/amir-the-h/iran-watcher/internal/model"
	"github.com/amir-the-h/iran-watcher/internal/store"
)

func (s *Server) handleListPipelines(w http.ResponseWriter, r *http.Request) {
	var filter store.PipelineFilter
	if t := r.URL.Query().Get("type"); t != "" {
		pt := model.PipelineType(t)
		filter.Type = &pt
	}
	if a := r.URL.Query().Get("active"); a != "" {
		b := a == "true"
		filter.Active = &b
	}
	pipelines, err := s.store.ListPipelines(r.Context(), filter)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "list pipelines: "+err.Error())
		return
	}
	writeJSON(w, http.StatusOK, pipelines)
}

func (s *Server) handleCreatePipeline(w http.ResponseWriter, r *http.Request) {
	var p model.Pipeline
	if err := json.NewDecoder(r.Body).Decode(&p); err != nil {
		writeError(w, http.StatusBadRequest, "invalid body: "+err.Error())
		return
	}
	if p.Name == "" || p.Type == "" {
		writeError(w, http.StatusBadRequest, "name and type are required")
		return
	}
	if err := s.store.SavePipeline(r.Context(), &p); err != nil {
		writeError(w, http.StatusInternalServerError, "save pipeline: "+err.Error())
		return
	}
	writeJSON(w, http.StatusCreated, p)
}

func (s *Server) handleUpdatePipeline(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid id")
		return
	}
	var p model.Pipeline
	if err := json.NewDecoder(r.Body).Decode(&p); err != nil {
		writeError(w, http.StatusBadRequest, "invalid body: "+err.Error())
		return
	}
	p.ID = id
	if err := s.store.UpdatePipeline(r.Context(), &p); err != nil {
		writeError(w, http.StatusInternalServerError, "update pipeline: "+err.Error())
		return
	}
	writeJSON(w, http.StatusOK, p)
}

func (s *Server) handleDeletePipeline(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid id")
		return
	}
	if err := s.store.DeletePipeline(r.Context(), id); err != nil {
		writeError(w, http.StatusInternalServerError, "delete pipeline: "+err.Error())
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handleActivatePipeline(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid id")
		return
	}
	if err := s.store.SetPipelineActive(r.Context(), id, true); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	s.reloadCache(r.Context())
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handleDeactivatePipeline(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid id")
		return
	}
	if err := s.store.SetPipelineActive(r.Context(), id, false); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	s.reloadCache(r.Context())
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handleListPipelineHandlers(w http.ResponseWriter, r *http.Request) {
	handlers := s.pipelineRegistry.Handlers()
	writeJSON(w, http.StatusOK, handlers)
}
```

Note: `s.reloadCache(ctx)` is a helper that calls `s.actionCache.Load(ctx)`. `s.pipelineRegistry` is the `*pipeline.Registry` held by the server.

- [ ] **Step 3: Update `handlers_actions.go`** — use new `TriggerType`/`PipelineID` fields

In the create/update action handler, parse `trigger_type` and `pipeline_id` instead of `type` and `config`. Update `ListActions` filter to use `TriggerType` and `PipelineID`. After any create/update/activate/deactivate, call `s.reloadCache(ctx)`.

- [ ] **Step 4: Create `handlers_chains.go`**

```go
package api

import (
	"encoding/json"
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"
	"github.com/amir-the-h/iran-watcher/internal/model"
	"github.com/amir-the-h/iran-watcher/internal/store"
)

func (s *Server) handleListChains(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid id")
		return
	}
	chains, err := s.store.ListActionChains(r.Context(), store.ActionChainFilter{SourceActionID: &id})
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, chains)
}

func (s *Server) handleCreateChain(w http.ResponseWriter, r *http.Request) {
	sourceID, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid id")
		return
	}
	var c model.ActionChain
	if err := json.NewDecoder(r.Body).Decode(&c); err != nil {
		writeError(w, http.StatusBadRequest, "invalid body: "+err.Error())
		return
	}
	c.SourceActionID = sourceID
	if err := s.store.SaveActionChain(r.Context(), &c); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusCreated, c)
}

func (s *Server) handleUpdateChain(w http.ResponseWriter, r *http.Request) {
	chainID, err := strconv.ParseInt(chi.URLParam(r, "chain_id"), 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid chain_id")
		return
	}
	var c model.ActionChain
	if err := json.NewDecoder(r.Body).Decode(&c); err != nil {
		writeError(w, http.StatusBadRequest, "invalid body: "+err.Error())
		return
	}
	c.ID = chainID
	if err := s.store.UpdateActionChain(r.Context(), &c); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, c)
}

func (s *Server) handleDeleteChain(w http.ResponseWriter, r *http.Request) {
	chainID, err := strconv.ParseInt(chi.URLParam(r, "chain_id"), 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid chain_id")
		return
	}
	if err := s.store.DeleteActionChain(r.Context(), chainID); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
```

- [ ] **Step 5: Create `handlers_triggers.go`**

```go
package api

import "net/http"

func (s *Server) handleListTriggers(w http.ResponseWriter, r *http.Request) {
	triggers := []map[string]string{
		{"type": "new_tweet", "description": "Fires when a tweet is received from the stream"},
		{"type": "new_categorization", "description": "Fires when an ONNX/Ollama pipeline action completes"},
		{"type": "entity_matched", "description": "Fires when an entity matcher action finds a match"},
	}
	writeJSON(w, http.StatusOK, triggers)
}
```

- [ ] **Step 6: Update `server.go`** — add pipeline/chain/trigger routes; add `pipelineRegistry` and `actionCache` fields; remove model routes

In the Server struct, add:
```go
pipelineRegistry *pipeline.Registry
actionCache      *worker.ActionCache
```

Add a `reloadCache` helper:
```go
func (s *Server) reloadCache(ctx context.Context) {
	if err := s.actionCache.Load(ctx); err != nil && s.logger != nil {
		s.logger.Warn("api: reload action cache", "error", err)
	}
}
```

Add routes:
```go
// Pipelines (replaces /models)
r.Get("/pipelines", adminOnly(s.handleListPipelines))
r.Post("/pipelines", adminOnly(s.handleCreatePipeline))
r.Get("/pipelines/handlers", adminOnly(s.handleListPipelineHandlers))
r.Put("/pipelines/{id}", adminOnly(s.handleUpdatePipeline))
r.Delete("/pipelines/{id}", adminOnly(s.handleDeletePipeline))
r.Post("/pipelines/{id}/activate", adminOnly(s.handleActivatePipeline))
r.Post("/pipelines/{id}/deactivate", adminOnly(s.handleDeactivatePipeline))

// Action chains
r.Get("/actions/{id}/chains", authed(s.handleListChains))
r.Post("/actions/{id}/chains", adminOnly(s.handleCreateChain))
r.Put("/actions/{id}/chains/{chain_id}", adminOnly(s.handleUpdateChain))
r.Delete("/actions/{id}/chains/{chain_id}", adminOnly(s.handleDeleteChain))

// Triggers
r.Get("/triggers", authed(s.handleListTriggers))
```

Remove `/models` routes. Update `/workers/buffer` and `/workers/buffer/{pipeline}` handlers to be no-ops or remove since the simplified manager has no in-memory buffers.

- [ ] **Step 7: Update server constructors** in `server.go` to accept `pipelineRegistry` and `actionCache`

Update `NewServerWithConcreteRules` signature in both server.go and serve.go.

- [ ] **Step 8: Compile**

```bash
go build ./... 2>&1 | head -30
```

- [ ] **Step 9: Commit**

```bash
git add internal/api/
git commit -m "feat: API — /api/pipelines, /api/actions/{id}/chains, /api/triggers; remove /api/models"
```

---

## Task 13: Frontend — PipelinesView and ActionsView Updates

**Files:**
- Create: `web/src/views/PipelinesView.vue`
- Modify: `web/src/views/ActionsView.vue`
- Modify: `web/src/router/index.ts`

- [ ] **Step 1: Add route in `web/src/router/index.ts`**

Find the routes array and add before or after the `/actions` route:
```typescript
{
  path: '/pipelines',
  name: 'pipelines',
  component: () => import('@/views/PipelinesView.vue'),
  meta: { requiresAdmin: true }
},
```

- [ ] **Step 2: Create `web/src/views/PipelinesView.vue`**

Model this on the existing `ModelsView.vue` or `ActionsView.vue` — read those files first to follow the same patterns for API calls, loading states, form handling, and table rendering.

The view should:
- List all pipelines via `GET /api/pipelines`
- Show columns: ID, Name, Type, Active, Created At
- Allow creating a pipeline with: Name (text), Type (select: onnx/ollama/internal), Config (JSON textarea)
- For internal type: show a Handler dropdown populated from `GET /api/pipelines/handlers`
- Activate/deactivate toggle (POST `/api/pipelines/{id}/activate|deactivate`)
- Delete button (DELETE `/api/pipelines/{id}`)

Read `web/src/views/ModelsView.vue` (if it exists) or `ActionsView.vue` and adapt its structure.

- [ ] **Step 3: Update `web/src/views/ActionsView.vue`** — use trigger_type + pipeline_id

Update the action create/edit form:
- Replace "Type" dropdown (pipeline/retweet/like/quote) with "Trigger Type" dropdown populated from `GET /api/triggers`
- Replace model/config fields with "Pipeline" dropdown populated from `GET /api/pipelines?active=true`
- Add a "Chains" section below each action row showing outbound chains (GET `/api/actions/{id}/chains`)
- Chain builder UI: button "Add Chain" → form with: Target Action (dropdown from all actions), Conditions list (key / operator / value rows)
- Update API calls from old action schema to new schema

Remove `formType` and `formConfig` reactive state. Add `formTriggerType` and `formPipelineId`.

For the chains section, add after the action row a collapsible panel:
```vue
<div v-if="expandedId === action.id">
  <h4>Chains</h4>
  <div v-for="chain in chainsFor(action.id)" :key="chain.id">
    → Action #{{ chain.target_action_id }}
    <span v-for="c in chain.conditions">{{ c.key }} {{ c.op }} {{ c.value }}</span>
    <button @click="deleteChain(action.id, chain.id)">Remove</button>
  </div>
  <button @click="addChain(action.id)">+ Add Chain</button>
</div>
```

- [ ] **Step 4: Add navigation link in sidebar/nav** (wherever `/models` link currently lives, replace with `/pipelines`)

```bash
grep -r "models\|Models" web/src/components/ web/src/layouts/ --include="*.vue" -l
```

Update the navigation component to replace "Models" link with "Pipelines".

- [ ] **Step 5: Build frontend**

```bash
cd web && npm run build 2>&1 | tail -20
```

Expected: builds successfully.

- [ ] **Step 6: Commit**

```bash
git add web/src/
git commit -m "feat: frontend — PipelinesView, updated ActionsView with trigger/pipeline/chain builder"
```

---

## Task 14: Build Verification and Cleanup

- [ ] **Step 1: Build Docker image**

```bash
docker compose build --build-arg BUILDKIT_INLINE_CACHE=1 iran-watcher 2>&1 | tail -30
```

Expected: builds successfully.

- [ ] **Step 2: Run all tests**

```bash
go test ./... 2>&1
```

Fix any failing tests. Common failure points:
- Mock store missing new method stubs — add them
- Tests referencing old `model.ActionType` or `action.Registry` — update to new types
- `scanAction` test using wrong column count — update to new schema columns

- [ ] **Step 3: Update mock store** to implement all new Store interface methods if not already done

```bash
grep -n "not implemented\|TODO\|stub" $(find . -name "mock_store*.go" -o -name "*_mock*.go") 2>/dev/null
```

- [ ] **Step 4: Update CLAUDE.md** — project structure and key design decisions

In the Project Structure section, update `internal/action/` line to show it's removed. In Key Design Decisions, update the "Actions system" bullet with the new description. Add a note about `ActionCache`, `pipeline.Registry`, `action_chains` table, and the simplified worker.

- [ ] **Step 5: Update the actions design spec**

Add a note at the top of `docs/superpowers/specs/2026-03-27-actions-design.md` marking it superseded by the event-driven refactor spec.

- [ ] **Step 6: Final commit**

```bash
git add -A
git commit -m "feat: event-driven pipeline refactor complete — pipelines, chains, simplified worker, ActionCache"
```

---

## Self-Review

**Spec coverage check:**

| Spec Section | Covered By |
|---|---|
| pipelines table | Task 1 (migration), Task 2 (model), Task 4 (store) |
| actions table — trigger_type, pipeline_id | Task 1, Task 2, Task 4 |
| action_chains table | Task 1, Task 2, Task 4 |
| Updated Pipeline interface | Task 5 |
| PipelineInput, map output | Task 5 |
| RunBatch dropped | Task 5, 6, 7 |
| ONNX factory | Task 6 |
| Ollama factory | Task 7 |
| Internal pipeline registry | Task 8 |
| entity_matcher as pipeline | Task 8 |
| twitter pipelines | Task 8 |
| notification pipelines | Task 8 |
| ActionCache (action_id → entry) | Task 9 |
| AllForRules on ActionCache | Task 9 |
| Task queue (iran-watcher:tasks) | Task 10 |
| TweetEvent payload | Task 10 |
| Worker: BRPOP tasks | Task 10 |
| Worker: save outputs to metadata | Task 10 |
| Worker: write categorization for onnx/ollama | Task 10 |
| Worker: evaluate chains | Task 10 |
| Stream listener pushes Tasks | Task 11 |
| Remove internal/action package | Task 11 |
| /api/pipelines CRUD | Task 12 |
| /api/actions/{id}/chains | Task 12 |
| /api/triggers | Task 12 |
| Updated /api/actions | Task 12 |
| /pipelines frontend | Task 13 |
| Actions frontend with trigger/pipeline/chain | Task 13 |

**Gaps identified and fixed:**

1. `pipeline_labels` table — covered in Task 1 (migration) and Task 4 (store CRUD)
2. `ListTweetRuleIDs` retention — covered in Task 4 Step 6
3. Categorization backward compat — covered in Task 10 `saveCategorization`
4. `inference.Registry` simplification — covered in Task 6
5. `join` helper name collision with `strings.Join` in sqlite_action_chains.go — the helper should be removed and replaced with `strings.Join` (import strings)

**Placeholder scan:** No TBD or TODO in task code blocks.

**Type consistency check:**
- `model.TriggerType` used in action.go, store.go ActionFilter, queue.Task — consistent
- `inference.PipelineType` used in pipeline.go interface, onnx.go, ollama.go, action_cache.go — consistent
- `model.PipelineType` used in model/pipeline.go and store PipelineFilter — consistent (note: inference.PipelineType and model.PipelineType have same string values but different Go types; action_cache.go uses model.PipelineType to read DB rows and inference.PipelineType to call Pipeline.Type() — this is correct since they have the same underlying string values)
- `queue.Task` used in queue.go, redis.go, manager.go — consistent
- `worker.ActionCache` used in stream client, manager, server, serve.go — consistent
