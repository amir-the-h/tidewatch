# Chains & Triggers Redesign Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Restructure actions/chains/triggers into three decoupled concepts with a visual DAG chain builder and a triggers page.

**Architecture:** New DB tables (`chains`, `chain_nodes`, `chain_edges`, `trigger_chain_links`). Actions lose `trigger_type` and `rule_ids`. Frontend gains three new views: simplified Actions, Chains list + Vue Flow builder, Triggers page. Worker stream/search listeners are stubbed (chain execution is a follow-on spec).

**Tech Stack:** Go (chi router, SQLite), Vue 3 + Naive UI + `@vue-flow/core`

---

## File Map

**New files:**
- `migrations/022_chains_triggers.sql`
- `internal/model/chain.go`
- `internal/store/sqlite_chains.go`
- `web/src/api/chains.ts`
- `web/src/api/trigger_links.ts`
- `web/src/stores/chains.ts`
- `web/src/views/ChainsView.vue`
- `web/src/views/ChainBuilderView.vue`
- `web/src/views/TriggersView.vue`
- `web/src/components/chain/StartNode.vue`
- `web/src/components/chain/ActionNode.vue`
- `web/src/components/chain/ConditionNode.vue`

**Modified files:**
- `internal/model/action.go` — remove TriggerType, RuleIDs
- `internal/model/action_chain.go` — delete (content moves to chain.go)
- `internal/store/store.go` — remove ActionChain* + ActionChainFilter; add Chain* + TriggerChainLink*
- `internal/store/sqlite_actions.go` — remove trigger_type/rule_ids from all SQL
- `internal/store/sqlite_action_chains.go` — delete
- `internal/store/sqlite_actions_test.go` — update for simplified Action
- `internal/api/handlers_actions.go` — remove trigger/rule/chain logic
- `internal/api/handlers_chains.go` — rewrite (new chain CRUD + graph API)
- `internal/api/handlers_triggers.go` — rewrite (trigger links CRUD)
- `internal/api/server.go` — update route registrations
- `internal/worker/action_cache.go` — remove AllForTrigger, AllForRules
- `internal/worker/manager.go` — remove evaluateChains; replace TriggerTypeDigestRequested const with string literal
- `internal/stream/client.go` — stub AllForTrigger call
- `internal/search/client.go` — stub AllForTrigger call
- `web/src/api/actions.ts` — remove TriggerType, rule_ids
- `web/src/stores/actions.ts` — remove trigger/rule from createAction/updateAction/fetchActions
- `web/src/views/ActionsView.vue` — simplify
- `web/src/router/index.ts` — add /chains, /chains/:id/edit, /triggers
- `web/src/App.vue` — add Chains + Triggers menu entries

---

### Task 1: DB Migration

**Files:**
- Create: `migrations/022_chains_triggers.sql`

- [ ] **Write migration SQL**

```sql
-- migrations/022_chains_triggers.sql
PRAGMA foreign_keys = OFF;

-- Recreate actions without trigger_type and rule_ids
CREATE TABLE actions_new (
  id          INTEGER PRIMARY KEY AUTOINCREMENT,
  name        TEXT    NOT NULL,
  pipeline_id INTEGER NOT NULL REFERENCES pipelines(id),
  active      INTEGER NOT NULL DEFAULT 0,
  created_at  TEXT    NOT NULL,
  updated_at  TEXT    NOT NULL
);
INSERT INTO actions_new (id, name, pipeline_id, active, created_at, updated_at)
  SELECT id, name, pipeline_id, active, created_at, updated_at FROM actions;
DROP TABLE actions;
ALTER TABLE actions_new RENAME TO actions;

DROP TABLE IF EXISTS action_chains;

CREATE TABLE chains (
  id          INTEGER PRIMARY KEY AUTOINCREMENT,
  name        TEXT    NOT NULL,
  description TEXT,
  active      INTEGER NOT NULL DEFAULT 0,
  created_at  TEXT    NOT NULL,
  updated_at  TEXT    NOT NULL
);

CREATE TABLE chain_nodes (
  id         INTEGER PRIMARY KEY AUTOINCREMENT,
  chain_id   INTEGER NOT NULL REFERENCES chains(id) ON DELETE CASCADE,
  type       TEXT    NOT NULL,
  action_id  INTEGER REFERENCES actions(id),
  conditions TEXT,
  label      TEXT,
  pos_x      REAL    NOT NULL DEFAULT 0,
  pos_y      REAL    NOT NULL DEFAULT 0,
  created_at TEXT    NOT NULL
);

CREATE TABLE chain_edges (
  id              INTEGER PRIMARY KEY AUTOINCREMENT,
  chain_id        INTEGER NOT NULL REFERENCES chains(id) ON DELETE CASCADE,
  source_node_id  INTEGER NOT NULL REFERENCES chain_nodes(id) ON DELETE CASCADE,
  source_handle   TEXT    NOT NULL DEFAULT 'default',
  target_node_id  INTEGER NOT NULL REFERENCES chain_nodes(id) ON DELETE CASCADE,
  created_at      TEXT    NOT NULL
);

CREATE TABLE trigger_chain_links (
  id           INTEGER PRIMARY KEY AUTOINCREMENT,
  trigger_type TEXT    NOT NULL,
  chain_id     INTEGER NOT NULL REFERENCES chains(id) ON DELETE CASCADE,
  active       INTEGER NOT NULL DEFAULT 0,
  created_at   TEXT    NOT NULL
);

PRAGMA foreign_keys = ON;
```

- [ ] **Verify migration runs**

```bash
go test ./migrations/... -v
```

Expected: PASS

- [ ] **Commit**

```bash
git add migrations/022_chains_triggers.sql
git commit -m "feat: add migration 022 — chains/triggers tables, remove trigger_type/rule_ids from actions"
```

---

### Task 2: Go Model Types

**Files:**
- Create: `internal/model/chain.go`
- Modify: `internal/model/action.go`
- Delete: `internal/model/action_chain.go`

- [ ] **Write `internal/model/chain.go`**

```go
package model

import "time"

// ChainNodeType is the type of a node in a chain DAG.
type ChainNodeType string

const (
	ChainNodeTypeStart     ChainNodeType = "start"
	ChainNodeTypeAction    ChainNodeType = "action"
	ChainNodeTypeCondition ChainNodeType = "condition"
)

// Chain is a named DAG workflow of action and condition nodes.
type Chain struct {
	ID          int64     `json:"id"`
	Name        string    `json:"name"`
	Description string    `json:"description,omitempty"`
	Active      bool      `json:"active"`
	NodeCount   int       `json:"node_count"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
}

// ChainNode is one node in a chain DAG.
type ChainNode struct {
	ID         int64            `json:"id"`
	ChainID    int64            `json:"chain_id"`
	Type       ChainNodeType    `json:"type"`
	ActionID   *int64           `json:"action_id,omitempty"`
	Conditions []ChainCondition `json:"conditions,omitempty"`
	Label      string           `json:"label"`
	PosX       float64          `json:"pos_x"`
	PosY       float64          `json:"pos_y"`
	CreatedAt  time.Time        `json:"created_at"`
}

// ChainEdge is a directed edge between two chain nodes.
type ChainEdge struct {
	ID           int64     `json:"id"`
	ChainID      int64     `json:"chain_id"`
	SourceNodeID int64     `json:"source_node_id"`
	SourceHandle string    `json:"source_handle"`
	TargetNodeID int64     `json:"target_node_id"`
	CreatedAt    time.Time `json:"created_at"`
}

// ChainGraph is the full DAG for a chain.
type ChainGraph struct {
	Nodes []*ChainNode `json:"nodes"`
	Edges []*ChainEdge `json:"edges"`
}

// ChainNodeRequest is a node in a PUT /api/chains/{id}/graph request.
// ClientID is a temporary client-side ID used to resolve edge references.
type ChainNodeRequest struct {
	ClientID   string           `json:"client_id"`
	Type       ChainNodeType    `json:"type"`
	ActionID   *int64           `json:"action_id,omitempty"`
	Conditions []ChainCondition `json:"conditions,omitempty"`
	Label      string           `json:"label"`
	PosX       float64          `json:"pos_x"`
	PosY       float64          `json:"pos_y"`
}

// ChainEdgeRequest is an edge in a PUT /api/chains/{id}/graph request.
type ChainEdgeRequest struct {
	SourceClientID string `json:"source_client_id"`
	SourceHandle   string `json:"source_handle"`
	TargetClientID string `json:"target_client_id"`
}

// ChainGraphRequest is the body for PUT /api/chains/{id}/graph.
type ChainGraphRequest struct {
	Nodes []*ChainNodeRequest `json:"nodes"`
	Edges []*ChainEdgeRequest `json:"edges"`
}

// TriggerChainLink connects a system trigger type to a chain.
type TriggerChainLink struct {
	ID          int64     `json:"id"`
	TriggerType string    `json:"trigger_type"`
	ChainID     int64     `json:"chain_id"`
	Active      bool      `json:"active"`
	CreatedAt   time.Time `json:"created_at"`
}

// ChainCondition is a single predicate evaluated against a context value.
// Moved here from action_chain.go.
type ChainCondition struct {
	Key      string `json:"key"`
	Operator string `json:"op"`
	Value    string `json:"value,omitempty"`
}
```

- [ ] **Rewrite `internal/model/action.go`** — remove TriggerType type, all TriggerType constants, TriggerType and RuleIDs fields from Action:

```go
package model

import "time"

// Action subscribes a pipeline to a chain node.
type Action struct {
	ID         int64     `json:"id"`
	Name       string    `json:"name"`
	PipelineID int64     `json:"pipeline_id"`
	Active     bool      `json:"active"`
	CreatedAt  time.Time `json:"created_at"`
	UpdatedAt  time.Time `json:"updated_at"`
}

// ExecutionStatus values for ActionExecution.
const (
	ExecutionStatusSuccess = "success"
	ExecutionStatusError   = "error"
)

// ActionExecution is one log entry per action fired.
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

- [ ] **Delete `internal/model/action_chain.go`**

```bash
git rm internal/model/action_chain.go
```

- [ ] **Verify package compiles (expect errors in other packages — that is expected at this stage)**

```bash
go build ./internal/model/... 2>&1
```

Expected: no errors in model package itself.

- [ ] **Commit**

```bash
git add internal/model/chain.go internal/model/action.go
git commit -m "feat: add chain/trigger model types; simplify Action (remove trigger_type, rule_ids)"
```

---

### Task 3: Store Interface — Remove Old, Add New

**Files:**
- Modify: `internal/store/store.go`
- Delete: `internal/store/sqlite_action_chains.go`

- [ ] **Delete `internal/store/sqlite_action_chains.go`**

```bash
git rm internal/store/sqlite_action_chains.go
```

- [ ] **Update `internal/store/store.go`** — replace ActionChainFilter + ActionChain* methods with Chain* + TriggerChainLink* methods, and simplify ActionFilter:

In `store.go`, find and remove:
```go
// ActionChainFilter defines optional filters for listing action chains.
type ActionChainFilter struct {
	SourceActionID *int64
	TargetActionID *int64
}
```

Remove `TriggerType *model.TriggerType` from `ActionFilter`. Result:
```go
// ActionFilter defines optional filters for listing actions.
type ActionFilter struct {
	PipelineID *int64
	Active     *bool
}
```

In the `Store` interface, remove:
```go
// ActionChains
SaveActionChain(ctx context.Context, c *model.ActionChain) error
GetActionChain(ctx context.Context, id int64) (*model.ActionChain, error)
ListActionChains(ctx context.Context, filter ActionChainFilter) ([]*model.ActionChain, error)
UpdateActionChain(ctx context.Context, c *model.ActionChain) error
DeleteActionChain(ctx context.Context, id int64) error
```

Add after the Actions section:
```go
// Chains
SaveChain(ctx context.Context, c *model.Chain) error
GetChain(ctx context.Context, id int64) (*model.Chain, error)
ListChains(ctx context.Context) ([]*model.Chain, error)
UpdateChain(ctx context.Context, c *model.Chain) error
DeleteChain(ctx context.Context, id int64) error
SetChainActive(ctx context.Context, id int64, active bool) error
GetChainGraph(ctx context.Context, chainID int64) (*model.ChainGraph, error)
SaveChainGraph(ctx context.Context, chainID int64, nodes []*model.ChainNodeRequest, edges []*model.ChainEdgeRequest) (*model.ChainGraph, error)

// TriggerChainLinks
SaveTriggerChainLink(ctx context.Context, l *model.TriggerChainLink) error
ListTriggerChainLinks(ctx context.Context) ([]*model.TriggerChainLink, error)
DeleteTriggerChainLink(ctx context.Context, id int64) error
SetTriggerChainLinkActive(ctx context.Context, id int64, active bool) error
```

- [ ] **Commit**

```bash
git add internal/store/store.go
git commit -m "refactor: update Store interface — remove ActionChain*, add Chain* and TriggerChainLink*"
```

---

### Task 4: Store — Simplified Actions SQL

**Files:**
- Modify: `internal/store/sqlite_actions.go`
- Modify: `internal/store/sqlite_actions_test.go`

- [ ] **Update `sqlite_actions.go`** — remove trigger_type and rule_ids from every SQL query and scan:

`SaveAction`:
```go
func (s *sqliteStore) SaveAction(ctx context.Context, a *model.Action) error {
	now := time.Now().UTC().Format(time.RFC3339)
	res, err := s.db.ExecContext(ctx,
		`INSERT INTO actions (name, pipeline_id, active, created_at, updated_at) VALUES (?,?,?,?,?)`,
		a.Name, a.PipelineID, boolToInt(a.Active), now, now,
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
```

`GetAction`:
```go
func (s *sqliteStore) GetAction(ctx context.Context, id int64) (*model.Action, error) {
	row := s.db.QueryRowContext(ctx,
		`SELECT id, name, pipeline_id, active, created_at, updated_at FROM actions WHERE id = ?`, id)
	a, err := scanAction(row)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, fmt.Errorf("store: get action: not found: %w", err)
		}
		return nil, fmt.Errorf("store: get action: %w", err)
	}
	return a, nil
}
```

`ListActions` — remove TriggerType filter branch, update SELECT:
```go
func (s *sqliteStore) ListActions(ctx context.Context, filter store.ActionFilter) ([]*model.Action, error) {
	var where []string
	var args []any
	if filter.PipelineID != nil {
		where = append(where, "pipeline_id = ?")
		args = append(args, *filter.PipelineID)
	}
	if filter.Active != nil {
		where = append(where, "active = ?")
		args = append(args, boolToInt(*filter.Active))
	}
	q := "SELECT id, name, pipeline_id, active, created_at, updated_at FROM actions"
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
```

`UpdateAction`:
```go
func (s *sqliteStore) UpdateAction(ctx context.Context, a *model.Action) error {
	now := time.Now().UTC().Format(time.RFC3339)
	_, err := s.db.ExecContext(ctx,
		`UPDATE actions SET name = ?, pipeline_id = ?, updated_at = ? WHERE id = ?`,
		a.Name, a.PipelineID, now, a.ID,
	)
	if err != nil {
		return fmt.Errorf("store: update action: %w", err)
	}
	return nil
}
```

`DeleteAction` — remove the `DELETE FROM action_chains` statement:
```go
func (s *sqliteStore) DeleteAction(ctx context.Context, id int64) error {
	if _, err := s.db.ExecContext(ctx, `
		DELETE FROM action_execution_metadata
		WHERE execution_id IN (SELECT id FROM action_executions WHERE action_id = ?)`, id); err != nil {
		return fmt.Errorf("store: delete action: delete metadata: %w", err)
	}
	if _, err := s.db.ExecContext(ctx, `DELETE FROM action_executions WHERE action_id = ?`, id); err != nil {
		return fmt.Errorf("store: delete action: delete executions: %w", err)
	}
	if _, err := s.db.ExecContext(ctx, `DELETE FROM actions WHERE id = ?`, id); err != nil {
		return fmt.Errorf("store: delete action: %w", err)
	}
	return nil
}
```

`scanAction` helper:
```go
func scanAction(r actionScanner) (*model.Action, error) {
	a := &model.Action{}
	var activeInt int
	var createdAt, updatedAt string
	if err := r.Scan(&a.ID, &a.Name, &a.PipelineID, &activeInt, &createdAt, &updatedAt); err != nil {
		return nil, err
	}
	a.Active = activeInt != 0
	if t, err := time.Parse(time.RFC3339, createdAt); err == nil {
		a.CreatedAt = t
	}
	if t, err := time.Parse(time.RFC3339, updatedAt); err == nil {
		a.UpdatedAt = t
	}
	return a, nil
}
```

- [ ] **Update `sqlite_actions_test.go`** — remove trigger_type/rule_ids from all test Action structs. Anywhere a test creates an `model.Action` or calls `SaveAction`, remove `TriggerType` and `RuleIDs` fields. Also remove any test that specifically tests `ListActions` by `TriggerType`.

- [ ] **Run action store tests**

```bash
go test ./internal/store/... -run TestAction -v
```

Expected: PASS

- [ ] **Commit**

```bash
git add internal/store/sqlite_actions.go internal/store/sqlite_actions_test.go
git commit -m "refactor: simplify actions store — remove trigger_type and rule_ids from SQL"
```

---

### Task 5: Store — New Chain + TriggerChainLink Implementation

**Files:**
- Create: `internal/store/sqlite_chains.go`

- [ ] **Write `internal/store/sqlite_chains.go`**

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

// ── Chains ────────────────────────────────────────────────────────────────────

func (s *sqliteStore) SaveChain(ctx context.Context, c *model.Chain) error {
	now := time.Now().UTC().Format(time.RFC3339)
	res, err := s.db.ExecContext(ctx,
		`INSERT INTO chains (name, description, active, created_at, updated_at) VALUES (?,?,?,?,?)`,
		c.Name, c.Description, boolToInt(c.Active), now, now,
	)
	if err != nil {
		return fmt.Errorf("store: save chain: %w", err)
	}
	id, _ := res.LastInsertId()
	c.ID = id
	return nil
}

func (s *sqliteStore) GetChain(ctx context.Context, id int64) (*model.Chain, error) {
	row := s.db.QueryRowContext(ctx, `
		SELECT c.id, c.name, COALESCE(c.description,''), c.active, c.created_at, c.updated_at,
		       (SELECT COUNT(*) FROM chain_nodes WHERE chain_id = c.id) AS node_count
		FROM chains c WHERE c.id = ?`, id)
	c, err := scanChain(row)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, fmt.Errorf("store: get chain: not found: %w", err)
		}
		return nil, fmt.Errorf("store: get chain: %w", err)
	}
	return c, nil
}

func (s *sqliteStore) ListChains(ctx context.Context) ([]*model.Chain, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT c.id, c.name, COALESCE(c.description,''), c.active, c.created_at, c.updated_at,
		       (SELECT COUNT(*) FROM chain_nodes WHERE chain_id = c.id) AS node_count
		FROM chains c ORDER BY c.id ASC`)
	if err != nil {
		return nil, fmt.Errorf("store: list chains: %w", err)
	}
	defer rows.Close()
	var out []*model.Chain
	for rows.Next() {
		c, err := scanChain(rows)
		if err != nil {
			return nil, fmt.Errorf("store: list chains: scan: %w", err)
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

func (s *sqliteStore) UpdateChain(ctx context.Context, c *model.Chain) error {
	now := time.Now().UTC().Format(time.RFC3339)
	_, err := s.db.ExecContext(ctx,
		`UPDATE chains SET name = ?, description = ?, updated_at = ? WHERE id = ?`,
		c.Name, c.Description, now, c.ID,
	)
	if err != nil {
		return fmt.Errorf("store: update chain: %w", err)
	}
	return nil
}

func (s *sqliteStore) DeleteChain(ctx context.Context, id int64) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM chains WHERE id = ?`, id)
	if err != nil {
		return fmt.Errorf("store: delete chain: %w", err)
	}
	return nil
}

func (s *sqliteStore) SetChainActive(ctx context.Context, id int64, active bool) error {
	now := time.Now().UTC().Format(time.RFC3339)
	_, err := s.db.ExecContext(ctx,
		`UPDATE chains SET active = ?, updated_at = ? WHERE id = ?`, boolToInt(active), now, id)
	if err != nil {
		return fmt.Errorf("store: set chain active: %w", err)
	}
	return nil
}

func (s *sqliteStore) GetChainGraph(ctx context.Context, chainID int64) (*model.ChainGraph, error) {
	nodeRows, err := s.db.QueryContext(ctx, `
		SELECT id, chain_id, type, action_id, COALESCE(conditions,'[]'), COALESCE(label,''), pos_x, pos_y, created_at
		FROM chain_nodes WHERE chain_id = ? ORDER BY id ASC`, chainID)
	if err != nil {
		return nil, fmt.Errorf("store: get chain graph nodes: %w", err)
	}
	defer nodeRows.Close()
	var nodes []*model.ChainNode
	for nodeRows.Next() {
		n, err := scanChainNode(nodeRows)
		if err != nil {
			return nil, fmt.Errorf("store: get chain graph nodes: scan: %w", err)
		}
		nodes = append(nodes, n)
	}
	if err := nodeRows.Err(); err != nil {
		return nil, fmt.Errorf("store: get chain graph nodes: %w", err)
	}

	edgeRows, err := s.db.QueryContext(ctx, `
		SELECT id, chain_id, source_node_id, source_handle, target_node_id, created_at
		FROM chain_edges WHERE chain_id = ? ORDER BY id ASC`, chainID)
	if err != nil {
		return nil, fmt.Errorf("store: get chain graph edges: %w", err)
	}
	defer edgeRows.Close()
	var edges []*model.ChainEdge
	for edgeRows.Next() {
		e, err := scanChainEdge(edgeRows)
		if err != nil {
			return nil, fmt.Errorf("store: get chain graph edges: scan: %w", err)
		}
		edges = append(edges, e)
	}
	if err := edgeRows.Err(); err != nil {
		return nil, fmt.Errorf("store: get chain graph edges: %w", err)
	}

	if nodes == nil {
		nodes = []*model.ChainNode{}
	}
	if edges == nil {
		edges = []*model.ChainEdge{}
	}
	return &model.ChainGraph{Nodes: nodes, Edges: edges}, nil
}

func (s *sqliteStore) SaveChainGraph(ctx context.Context, chainID int64, nodes []*model.ChainNodeRequest, edges []*model.ChainEdgeRequest) (*model.ChainGraph, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, fmt.Errorf("store: save chain graph: begin tx: %w", err)
	}
	defer tx.Rollback() //nolint:errcheck

	if _, err := tx.ExecContext(ctx, `DELETE FROM chain_edges WHERE chain_id = ?`, chainID); err != nil {
		return nil, fmt.Errorf("store: save chain graph: delete edges: %w", err)
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM chain_nodes WHERE chain_id = ?`, chainID); err != nil {
		return nil, fmt.Errorf("store: save chain graph: delete nodes: %w", err)
	}

	now := time.Now().UTC().Format(time.RFC3339)
	clientToDBID := make(map[string]int64, len(nodes))
	savedNodes := make([]*model.ChainNode, 0, len(nodes))

	for _, n := range nodes {
		condJSON := "[]"
		if len(n.Conditions) > 0 {
			b, _ := json.Marshal(n.Conditions)
			condJSON = string(b)
		}
		res, err := tx.ExecContext(ctx,
			`INSERT INTO chain_nodes (chain_id, type, action_id, conditions, label, pos_x, pos_y, created_at)
			 VALUES (?,?,?,?,?,?,?,?)`,
			chainID, string(n.Type), n.ActionID, condJSON, n.Label, n.PosX, n.PosY, now,
		)
		if err != nil {
			return nil, fmt.Errorf("store: save chain graph: insert node: %w", err)
		}
		dbID, _ := res.LastInsertId()
		clientToDBID[n.ClientID] = dbID
		node := &model.ChainNode{
			ID: dbID, ChainID: chainID, Type: n.Type, ActionID: n.ActionID,
			Conditions: n.Conditions, Label: n.Label, PosX: n.PosX, PosY: n.PosY,
		}
		if t, err := time.Parse(time.RFC3339, now); err == nil {
			node.CreatedAt = t
		}
		if node.Conditions == nil {
			node.Conditions = []model.ChainCondition{}
		}
		savedNodes = append(savedNodes, node)
	}

	savedEdges := make([]*model.ChainEdge, 0, len(edges))
	for _, e := range edges {
		srcID, srcOK := clientToDBID[e.SourceClientID]
		tgtID, tgtOK := clientToDBID[e.TargetClientID]
		if !srcOK || !tgtOK {
			return nil, fmt.Errorf("store: save chain graph: unknown node client_id in edge (%s→%s)", e.SourceClientID, e.TargetClientID)
		}
		handle := e.SourceHandle
		if handle == "" {
			handle = "default"
		}
		res, err := tx.ExecContext(ctx,
			`INSERT INTO chain_edges (chain_id, source_node_id, source_handle, target_node_id, created_at)
			 VALUES (?,?,?,?,?)`,
			chainID, srcID, handle, tgtID, now,
		)
		if err != nil {
			return nil, fmt.Errorf("store: save chain graph: insert edge: %w", err)
		}
		dbID, _ := res.LastInsertId()
		savedEdges = append(savedEdges, &model.ChainEdge{
			ID: dbID, ChainID: chainID,
			SourceNodeID: srcID, SourceHandle: handle, TargetNodeID: tgtID,
		})
	}

	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("store: save chain graph: commit: %w", err)
	}
	return &model.ChainGraph{Nodes: savedNodes, Edges: savedEdges}, nil
}

// ── TriggerChainLinks ─────────────────────────────────────────────────────────

func (s *sqliteStore) SaveTriggerChainLink(ctx context.Context, l *model.TriggerChainLink) error {
	now := time.Now().UTC().Format(time.RFC3339)
	res, err := s.db.ExecContext(ctx,
		`INSERT INTO trigger_chain_links (trigger_type, chain_id, active, created_at) VALUES (?,?,?,?)`,
		l.TriggerType, l.ChainID, boolToInt(l.Active), now,
	)
	if err != nil {
		return fmt.Errorf("store: save trigger chain link: %w", err)
	}
	id, _ := res.LastInsertId()
	l.ID = id
	return nil
}

func (s *sqliteStore) ListTriggerChainLinks(ctx context.Context) ([]*model.TriggerChainLink, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, trigger_type, chain_id, active, created_at
		 FROM trigger_chain_links ORDER BY id ASC`)
	if err != nil {
		return nil, fmt.Errorf("store: list trigger chain links: %w", err)
	}
	defer rows.Close()
	var out []*model.TriggerChainLink
	for rows.Next() {
		l, err := scanTriggerChainLink(rows)
		if err != nil {
			return nil, fmt.Errorf("store: list trigger chain links: scan: %w", err)
		}
		out = append(out, l)
	}
	return out, rows.Err()
}

func (s *sqliteStore) DeleteTriggerChainLink(ctx context.Context, id int64) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM trigger_chain_links WHERE id = ?`, id)
	if err != nil {
		return fmt.Errorf("store: delete trigger chain link: %w", err)
	}
	return nil
}

func (s *sqliteStore) SetTriggerChainLinkActive(ctx context.Context, id int64, active bool) error {
	_, err := s.db.ExecContext(ctx,
		`UPDATE trigger_chain_links SET active = ? WHERE id = ?`, boolToInt(active), id)
	if err != nil {
		return fmt.Errorf("store: set trigger chain link active: %w", err)
	}
	return nil
}

// ── helpers ───────────────────────────────────────────────────────────────────

type chainScanner interface {
	Scan(dest ...any) error
}

func scanChain(r chainScanner) (*model.Chain, error) {
	c := &model.Chain{}
	var activeInt, nodeCount int
	var createdAt, updatedAt string
	if err := r.Scan(&c.ID, &c.Name, &c.Description, &activeInt, &createdAt, &updatedAt, &nodeCount); err != nil {
		return nil, err
	}
	c.Active = activeInt != 0
	c.NodeCount = nodeCount
	if t, err := time.Parse(time.RFC3339, createdAt); err == nil {
		c.CreatedAt = t
	}
	if t, err := time.Parse(time.RFC3339, updatedAt); err == nil {
		c.UpdatedAt = t
	}
	return c, nil
}

func scanChainNode(r chainScanner) (*model.ChainNode, error) {
	n := &model.ChainNode{}
	var nodeType string
	var actionID sql.NullInt64
	var condStr, label, createdAt string
	if err := r.Scan(&n.ID, &n.ChainID, &nodeType, &actionID, &condStr, &label, &n.PosX, &n.PosY, &createdAt); err != nil {
		return nil, err
	}
	n.Type = model.ChainNodeType(nodeType)
	n.Label = label
	if actionID.Valid {
		v := actionID.Int64
		n.ActionID = &v
	}
	if err := json.Unmarshal([]byte(condStr), &n.Conditions); err != nil || n.Conditions == nil {
		n.Conditions = []model.ChainCondition{}
	}
	if t, err := time.Parse(time.RFC3339, createdAt); err == nil {
		n.CreatedAt = t
	}
	return n, nil
}

func scanChainEdge(r chainScanner) (*model.ChainEdge, error) {
	e := &model.ChainEdge{}
	var createdAt string
	if err := r.Scan(&e.ID, &e.ChainID, &e.SourceNodeID, &e.SourceHandle, &e.TargetNodeID, &createdAt); err != nil {
		return nil, err
	}
	if t, err := time.Parse(time.RFC3339, createdAt); err == nil {
		e.CreatedAt = t
	}
	return e, nil
}

func scanTriggerChainLink(r chainScanner) (*model.TriggerChainLink, error) {
	l := &model.TriggerChainLink{}
	var activeInt int
	var createdAt string
	if err := r.Scan(&l.ID, &l.TriggerType, &l.ChainID, &activeInt, &createdAt); err != nil {
		return nil, err
	}
	l.Active = activeInt != 0
	if t, err := time.Parse(time.RFC3339, createdAt); err == nil {
		l.CreatedAt = t
	}
	return l, nil
}
```

- [ ] **Run store tests**

```bash
go test ./internal/store/... -v
```

Expected: PASS

- [ ] **Commit**

```bash
git add internal/store/sqlite_chains.go
git commit -m "feat: add sqlite chain store — CRUD, graph save/load, trigger links"
```

---

### Task 6: API — Actions Handlers Cleanup

**Files:**
- Modify: `internal/api/handlers_actions.go`
- Modify: `internal/api/server.go`

- [ ] **Update `handlers_actions.go`** — remove trigger_type/rule_ids from create/update/list, remove chain sub-route handlers (they move to handlers_chains.go)

`handleListActions` — remove trigger_type query param:
```go
func (s *Server) handleListActions(w http.ResponseWriter, r *http.Request) {
	filter := store.ActionFilter{}
	if v := r.URL.Query().Get("active"); v != "" {
		b := v == "true"
		filter.Active = &b
	}
	actions, err := s.store.ListActions(r.Context(), filter)
	if err != nil {
		s.logger.Error("api: list actions", "error", err)
		writeError(w, http.StatusInternalServerError, "failed to list actions")
		return
	}
	writeJSON(w, http.StatusOK, actions)
}
```

`handleCreateAction`:
```go
func (s *Server) handleCreateAction(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Name       string `json:"name"`
		PipelineID int64  `json:"pipeline_id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if req.Name == "" {
		writeError(w, http.StatusBadRequest, "name is required")
		return
	}
	if req.PipelineID == 0 {
		writeError(w, http.StatusBadRequest, "pipeline_id is required")
		return
	}
	a := &model.Action{Name: req.Name, PipelineID: req.PipelineID}
	if err := s.store.SaveAction(r.Context(), a); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if err := s.actionCache.Load(r.Context()); err != nil {
		s.logger.Warn("api: reload action cache after create", "error", err)
	}
	writeJSON(w, http.StatusCreated, a)
}
```

`handleUpdateAction`:
```go
func (s *Server) handleUpdateAction(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid id")
		return
	}
	var req struct {
		Name       string `json:"name"`
		PipelineID int64  `json:"pipeline_id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	a, err := s.store.GetAction(r.Context(), id)
	if err != nil {
		writeError(w, http.StatusNotFound, "action not found")
		return
	}
	if req.Name != "" {
		a.Name = req.Name
	}
	if req.PipelineID != 0 {
		a.PipelineID = req.PipelineID
	}
	if err := s.store.UpdateAction(r.Context(), a); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if err := s.actionCache.Load(r.Context()); err != nil {
		s.logger.Warn("api: reload action cache after update", "error", err)
	}
	writeJSON(w, http.StatusOK, a)
}
```

- [ ] **Update `server.go` route registrations** — remove old chain sub-routes, add new chain + trigger link routes. Find and remove:
```go
// Action chains (read) and triggers — all authenticated users.
r.Get("/actions/{id}/chains", srv.handleListChains)
r.Get("/triggers", srv.handleListTriggers)
```
Replace with:
```go
r.Get("/triggers", srv.handleListTriggers)
r.Get("/triggers/links", srv.handleListTriggerChainLinks)
r.Get("/chains", srv.handleListChains)
r.Get("/chains/{id}", srv.handleGetChain)
r.Get("/chains/{id}/graph", srv.handleGetChainGraph)
```

Remove from admin group:
```go
// Action chains (mutations only — GET is in the authed group above)
r.Post("/actions/{id}/chains", srv.handleCreateChain)
r.Put("/actions/{id}/chains/{chain_id}", srv.handleUpdateChain)
r.Delete("/actions/{id}/chains/{chain_id}", srv.handleDeleteChain)
```

Add to admin group (after Actions section):
```go
// Chains
r.Post("/chains", srv.handleCreateChain)
r.Put("/chains/{id}", srv.handleUpdateChain)
r.Delete("/chains/{id}", srv.handleDeleteChain)
r.Post("/chains/{id}/activate", srv.handleActivateChain)
r.Post("/chains/{id}/deactivate", srv.handleDeactivateChain)
r.Put("/chains/{id}/graph", srv.handleSaveChainGraph)

// Trigger chain links
r.Post("/triggers/links", srv.handleCreateTriggerChainLink)
r.Delete("/triggers/links/{id}", srv.handleDeleteTriggerChainLink)
r.Post("/triggers/links/{id}/activate", srv.handleActivateTriggerChainLink)
r.Post("/triggers/links/{id}/deactivate", srv.handleDeactivateTriggerChainLink)
```

- [ ] **Commit**

```bash
git add internal/api/handlers_actions.go internal/api/server.go
git commit -m "refactor: simplify actions API — remove trigger/rule/chain; register new chain + trigger link routes"
```

---

### Task 7: API — New Chain + Trigger Link Handlers

**Files:**
- Rewrite: `internal/api/handlers_chains.go`
- Rewrite: `internal/api/handlers_triggers.go`

- [ ] **Rewrite `internal/api/handlers_chains.go`**

```go
package api

import (
	"encoding/json"
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"

	"github.com/amir-the-h/iran-watcher/internal/model"
)

func (s *Server) handleListChains(w http.ResponseWriter, r *http.Request) {
	chains, err := s.store.ListChains(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, chains)
}

func (s *Server) handleGetChain(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid id")
		return
	}
	chain, err := s.store.GetChain(r.Context(), id)
	if err != nil {
		writeError(w, http.StatusNotFound, "chain not found")
		return
	}
	writeJSON(w, http.StatusOK, chain)
}

func (s *Server) handleCreateChain(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Name        string `json:"name"`
		Description string `json:"description"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid body")
		return
	}
	if req.Name == "" {
		writeError(w, http.StatusBadRequest, "name is required")
		return
	}
	c := &model.Chain{Name: req.Name, Description: req.Description}
	if err := s.store.SaveChain(r.Context(), c); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusCreated, c)
}

func (s *Server) handleUpdateChain(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid id")
		return
	}
	var req struct {
		Name        string `json:"name"`
		Description string `json:"description"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid body")
		return
	}
	c, err := s.store.GetChain(r.Context(), id)
	if err != nil {
		writeError(w, http.StatusNotFound, "chain not found")
		return
	}
	if req.Name != "" {
		c.Name = req.Name
	}
	c.Description = req.Description
	if err := s.store.UpdateChain(r.Context(), c); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, c)
}

func (s *Server) handleDeleteChain(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid id")
		return
	}
	if err := s.store.DeleteChain(r.Context(), id); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handleActivateChain(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid id")
		return
	}
	if err := s.store.SetChainActive(r.Context(), id, true); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handleDeactivateChain(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid id")
		return
	}
	if err := s.store.SetChainActive(r.Context(), id, false); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handleGetChainGraph(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid id")
		return
	}
	graph, err := s.store.GetChainGraph(r.Context(), id)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, graph)
}

func (s *Server) handleSaveChainGraph(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid id")
		return
	}
	var req model.ChainGraphRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid body: "+err.Error())
		return
	}
	graph, err := s.store.SaveChainGraph(r.Context(), id, req.Nodes, req.Edges)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, graph)
}
```

- [ ] **Rewrite `internal/api/handlers_triggers.go`**

```go
package api

import (
	"encoding/json"
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"

	"github.com/amir-the-h/iran-watcher/internal/model"
)

func (s *Server) handleListTriggers(w http.ResponseWriter, r *http.Request) {
	triggers := []map[string]string{
		{"type": "new_tweet",          "description": "Fires when a tweet is received from the stream"},
		{"type": "new_categorization", "description": "Fires when an ONNX/Ollama pipeline action completes"},
		{"type": "entity_matched",     "description": "Fires when an entity matcher action finds a match"},
		{"type": "digest_requested",   "description": "Fires when a digest job is created via POST /api/digest"},
		{"type": "server_started",     "description": "Fires once when the server process starts"},
		{"type": "stream_connected",   "description": "Fires when the X stream connects"},
		{"type": "digest_completed",   "description": "Fires when a digest job finishes"},
	}
	writeJSON(w, http.StatusOK, triggers)
}

func (s *Server) handleListTriggerChainLinks(w http.ResponseWriter, r *http.Request) {
	links, err := s.store.ListTriggerChainLinks(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, links)
}

func (s *Server) handleCreateTriggerChainLink(w http.ResponseWriter, r *http.Request) {
	var req struct {
		TriggerType string `json:"trigger_type"`
		ChainID     int64  `json:"chain_id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid body")
		return
	}
	if req.TriggerType == "" || req.ChainID == 0 {
		writeError(w, http.StatusBadRequest, "trigger_type and chain_id are required")
		return
	}
	l := &model.TriggerChainLink{TriggerType: req.TriggerType, ChainID: req.ChainID}
	if err := s.store.SaveTriggerChainLink(r.Context(), l); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusCreated, l)
}

func (s *Server) handleDeleteTriggerChainLink(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid id")
		return
	}
	if err := s.store.DeleteTriggerChainLink(r.Context(), id); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handleActivateTriggerChainLink(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid id")
		return
	}
	if err := s.store.SetTriggerChainLinkActive(r.Context(), id, true); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handleDeactivateTriggerChainLink(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid id")
		return
	}
	if err := s.store.SetTriggerChainLinkActive(r.Context(), id, false); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
```

- [ ] **Build to check for compile errors**

```bash
go build ./internal/api/... 2>&1
```

Expected: no errors (other packages may still fail).

- [ ] **Commit**

```bash
git add internal/api/handlers_chains.go internal/api/handlers_triggers.go
git commit -m "feat: add chain CRUD + graph API handlers; add trigger link handlers"
```

---

### Task 8: Worker + Stream Cleanup

**Files:**
- Modify: `internal/worker/action_cache.go`
- Modify: `internal/worker/manager.go`
- Modify: `internal/stream/client.go`
- Modify: `internal/search/client.go`

- [ ] **Update `action_cache.go`** — remove `AllForTrigger` and `AllForRules` methods (they referenced `Action.TriggerType` and `Action.RuleIDs`). Keep `Get`, `Load`, `FindByType`. The `Load` method already works without TriggerType.

Delete the entire `AllForRules` function and the entire `AllForTrigger` function from `action_cache.go`.

- [ ] **Update `manager.go`** — remove `evaluateChains` call and the `evaluateChains` / `evaluateConditions` / `evalCondition` functions. Also replace the `model.TriggerTypeDigestRequested` constant reference with the string literal:

At line ~129, change:
```go
if task.TriggerType == string(model.TriggerTypeDigestRequested) {
```
to:
```go
if task.TriggerType == "digest_requested" {
```

At line ~240, remove the call:
```go
m.evaluateChains(ctx, task.ActionID, task.TriggerType, task.Event, mergedCtx)
```

Delete the `evaluateChains`, `evaluateConditions`, and `evalCondition` functions from `manager.go`. Remove the `strings` import if it is only used by those functions.

- [ ] **Update `internal/stream/client.go`** — find the `cache.AllForTrigger(model.TriggerTypeNewTweet, resolvedRuleIDs)` call (line ~314). Replace the entire block that calls it with a comment stub:

```go
// TODO(chains): chain execution — deferred to worker execution spec.
// When implemented, look up trigger_chain_links for "new_tweet" and
// walk the chain DAG for each matched tweet.
```

Remove the import of `model` from stream/client.go if `model.TriggerTypeNewTweet` was the only reference to that package (check for other usages first).

- [ ] **Update `internal/search/client.go`** — same pattern: find the `cache.AllForTrigger` call (~line 260) and replace with:

```go
// TODO(chains): chain execution — deferred to worker execution spec.
```

Remove any now-unused imports.

- [ ] **Fix `worker_test.go`** — remove any references to `TriggerType` on `model.Action` or `AllForTrigger`/`AllForRules` on `ActionCache`.

- [ ] **Build entire Go module**

```bash
go build ./... 2>&1
```

Expected: no errors.

- [ ] **Run all tests**

```bash
go test ./... 2>&1
```

Expected: PASS (some integration tests may be skipped without Redis).

- [ ] **Commit**

```bash
git add internal/worker/action_cache.go internal/worker/manager.go \
        internal/stream/client.go internal/search/client.go \
        internal/worker/worker_test.go
git commit -m "refactor: remove trigger/rule from ActionCache and worker; stub stream/search AllForTrigger calls"
```

---

### Task 9: Frontend — Package, API, Stores, Router, Nav

**Files:**
- Modify: `web/package.json`
- Create: `web/src/api/chains.ts`
- Create: `web/src/api/trigger_links.ts`
- Create: `web/src/stores/chains.ts`
- Modify: `web/src/api/actions.ts`
- Modify: `web/src/stores/actions.ts`
- Modify: `web/src/router/index.ts`
- Modify: `web/src/App.vue`

- [ ] **Install Vue Flow**

```bash
cd web && npm install @vue-flow/core
```

Expected: package added to node_modules and package.json.

- [ ] **Write `web/src/api/chains.ts`**

```typescript
import { apiFetch } from './client'

export interface Chain {
  id: number
  name: string
  description?: string
  active: boolean
  node_count: number
  created_at: string
  updated_at: string
}

export interface ChainNode {
  id: number
  chain_id: number
  type: 'start' | 'action' | 'condition'
  action_id?: number
  conditions?: ChainCondition[]
  label: string
  pos_x: number
  pos_y: number
  created_at: string
}

export interface ChainEdge {
  id: number
  chain_id: number
  source_node_id: number
  source_handle: string
  target_node_id: number
  created_at: string
}

export interface ChainGraph {
  nodes: ChainNode[]
  edges: ChainEdge[]
}

export interface ChainCondition {
  key: string
  op: string
  value?: string
}

export interface ChainNodeRequest {
  client_id: string
  type: 'start' | 'action' | 'condition'
  action_id?: number
  conditions?: ChainCondition[]
  label: string
  pos_x: number
  pos_y: number
}

export interface ChainEdgeRequest {
  source_client_id: string
  source_handle: string
  target_client_id: string
}

export const listChains = () => apiFetch<Chain[]>('/api/chains')
export const getChain = (id: number) => apiFetch<Chain>(`/api/chains/${id}`)
export const createChain = (body: { name: string; description?: string }) =>
  apiFetch<Chain>('/api/chains', { method: 'POST', body: JSON.stringify(body) })
export const updateChain = (id: number, body: { name?: string; description?: string }) =>
  apiFetch<Chain>(`/api/chains/${id}`, { method: 'PUT', body: JSON.stringify(body) })
export const deleteChain = (id: number) =>
  apiFetch<void>(`/api/chains/${id}`, { method: 'DELETE' })
export const activateChain = (id: number) =>
  apiFetch<void>(`/api/chains/${id}/activate`, { method: 'POST' })
export const deactivateChain = (id: number) =>
  apiFetch<void>(`/api/chains/${id}/deactivate`, { method: 'POST' })
export const getChainGraph = (id: number) =>
  apiFetch<ChainGraph>(`/api/chains/${id}/graph`)
export const saveChainGraph = (id: number, nodes: ChainNodeRequest[], edges: ChainEdgeRequest[]) =>
  apiFetch<ChainGraph>(`/api/chains/${id}/graph`, {
    method: 'PUT',
    body: JSON.stringify({ nodes, edges }),
  })
```

- [ ] **Write `web/src/api/trigger_links.ts`**

```typescript
import { apiFetch } from './client'

export interface TriggerInfo {
  type: string
  description: string
}

export interface TriggerChainLink {
  id: number
  trigger_type: string
  chain_id: number
  active: boolean
  created_at: string
}

export const listTriggers = () => apiFetch<TriggerInfo[]>('/api/triggers')
export const listTriggerChainLinks = () => apiFetch<TriggerChainLink[]>('/api/triggers/links')
export const createTriggerChainLink = (body: { trigger_type: string; chain_id: number }) =>
  apiFetch<TriggerChainLink>('/api/triggers/links', { method: 'POST', body: JSON.stringify(body) })
export const deleteTriggerChainLink = (id: number) =>
  apiFetch<void>(`/api/triggers/links/${id}`, { method: 'DELETE' })
export const activateTriggerChainLink = (id: number) =>
  apiFetch<void>(`/api/triggers/links/${id}/activate`, { method: 'POST' })
export const deactivateTriggerChainLink = (id: number) =>
  apiFetch<void>(`/api/triggers/links/${id}/deactivate`, { method: 'POST' })
```

- [ ] **Write `web/src/stores/chains.ts`**

```typescript
import { defineStore } from 'pinia'
import { ref } from 'vue'
import { listChains, createChain, deleteChain, activateChain, deactivateChain, type Chain } from '../api/chains'

export const useChainsStore = defineStore('chains', () => {
  const chains = ref<Chain[]>([])
  const loading = ref(false)

  async function fetchChains() {
    loading.value = true
    try { chains.value = await listChains() }
    finally { loading.value = false }
  }

  async function create(body: { name: string; description?: string }) {
    const c = await createChain(body)
    chains.value.push(c)
    return c
  }

  async function remove(id: number) {
    await deleteChain(id)
    chains.value = chains.value.filter(c => c.id !== id)
  }

  async function activate(id: number) {
    await activateChain(id)
    const c = chains.value.find(c => c.id === id)
    if (c) c.active = true
  }

  async function deactivate(id: number) {
    await deactivateChain(id)
    const c = chains.value.find(c => c.id === id)
    if (c) c.active = false
  }

  return { chains, loading, fetchChains, create, remove, activate, deactivate }
})
```

- [ ] **Update `web/src/api/actions.ts`** — remove `TriggerType` type, `ActionType` alias, `trigger_type`/`rule_ids` from `Action`, remove `trigger_type` from `createAction` and `listActions`:

```typescript
import { apiFetch } from './client'

export interface Action {
  id: number
  name: string
  pipeline_id: number
  active: boolean
  created_at: string
  updated_at: string
}

export interface ActionExecution {
  id: number
  action_id: number
  tweet_id: string
  status: 'success' | 'error'
  error?: string
  executed_at: string
  metadata?: Record<string, string>
}

export interface TwitterCredentials {
  id: number
  expires_at: string
  twitter_user_id: string
  username: string
  updated_at: string
}

export interface ExecutionFilter {
  page?: number
  per_page?: number
  from?: string
  to?: string
  tweet_id?: string
  status?: string
}

export const listActions = (filter: { active?: boolean } = {}) => {
  const params = new URLSearchParams()
  if (filter.active !== undefined) params.set('active', String(filter.active))
  const qs = params.toString() ? `?${params}` : ''
  return apiFetch<Action[]>(`/api/actions${qs}`)
}

export const createAction = (body: { name: string; pipeline_id: number }) =>
  apiFetch<Action>('/api/actions', { method: 'POST', body: JSON.stringify(body) })

export const updateAction = (id: number, body: { name?: string; pipeline_id?: number }) =>
  apiFetch<Action>(`/api/actions/${id}`, { method: 'PUT', body: JSON.stringify(body) })

export const deleteAction = (id: number) =>
  apiFetch<void>(`/api/actions/${id}`, { method: 'DELETE' })

export const activateAction = (id: number) =>
  apiFetch<Action>(`/api/actions/${id}/activate`, { method: 'POST' })

export const deactivateAction = (id: number) =>
  apiFetch<Action>(`/api/actions/${id}/deactivate`, { method: 'POST' })

export const runAction = (id: number) =>
  apiFetch<{ queued: number }>(`/api/actions/${id}/run`, { method: 'POST' })

export const listActionExecutions = (id: number, filter: ExecutionFilter = {}) => {
  const params = new URLSearchParams()
  if (filter.page !== undefined) params.set('page', String(filter.page))
  if (filter.per_page !== undefined) params.set('per_page', String(filter.per_page))
  const qs = params.toString() ? `?${params}` : ''
  return apiFetch<ActionExecution[]>(`/api/actions/${id}/executions${qs}`)
}

export const listAllExecutions = (filter: ExecutionFilter = {}) => {
  const params = new URLSearchParams()
  if (filter.page !== undefined) params.set('page', String(filter.page))
  if (filter.per_page !== undefined) params.set('per_page', String(filter.per_page))
  if (filter.from) params.set('from', filter.from)
  if (filter.to) params.set('to', filter.to)
  if (filter.tweet_id) params.set('tweet_id', filter.tweet_id)
  if (filter.status) params.set('status', filter.status)
  const qs = params.toString() ? `?${params}` : ''
  return apiFetch<ActionExecution[]>(`/api/executions${qs}`)
}

export const twitterAuthStart = () => apiFetch<{ url: string }>('/api/twitter/auth/start')
export const twitterAuthStatus = () => apiFetch<TwitterCredentials>('/api/twitter/auth/status')
export const twitterDisconnect = () => apiFetch<void>('/api/twitter/auth', { method: 'DELETE' })
```

- [ ] **Update `web/src/stores/actions.ts`** — remove `TriggerType` references from `fetchActions`, `createAction`, `updateAction`:

```typescript
async function fetchActions(filter: { active?: boolean } = {}) { ... }
async function createAction(body: { name: string; pipeline_id: number }) { ... }
async function updateAction(id: number, body: { name?: string; pipeline_id?: number }) { ... }
```

Remove the `TriggerType` import at the top.

- [ ] **Update `web/src/router/index.ts`** — add new routes:

```typescript
{ path: '/chains', component: () => import('../views/ChainsView.vue') },
{ path: '/chains/:id/edit', component: () => import('../views/ChainBuilderView.vue') },
{ path: '/triggers', component: () => import('../views/TriggersView.vue') },
```

- [ ] **Update `web/src/App.vue`** — add Chains and Triggers to admin menu (after Actions, before Digest):

```typescript
{ label: 'Chains',   key: '/chains'   },
{ label: 'Triggers', key: '/triggers' },
```

- [ ] **Commit**

```bash
git add web/package.json web/package-lock.json \
        web/src/api/chains.ts web/src/api/trigger_links.ts \
        web/src/stores/chains.ts \
        web/src/api/actions.ts web/src/stores/actions.ts \
        web/src/router/index.ts web/src/App.vue
git commit -m "feat: add chain/trigger-link API + stores; simplify actions API; update router + nav"
```

---

### Task 10: Frontend — Simplify ActionsView

**Files:**
- Modify: `web/src/views/ActionsView.vue`

- [ ] **Rewrite `ActionsView.vue`** — remove trigger column, rule_ids column, expandable chains panel, chain-related state/logic, and chain-related imports. Keep Twitter OAuth card, executions drawer, activate/deactivate/delete/run/edit. Simplify the create/edit form to name + pipeline only.

Key changes from the current file:
- Remove all `chain*` state vars and functions (`expandedRowKeys`, `chainsMap`, `chainsLoading`, `showAddChain`, `addChain*`, `savingChain`, `onExpandChange`, `loadChains`, `openAddChain`, `addConditionRow`, `removeConditionRow`, `onSaveChain`, `onRemoveChain`, `formatConditions`, `renderChainsPanel`)
- Remove the `type: 'expand'` column from `columns`
- Remove the `Trigger` and `Rules` columns from `columns`
- Remove `filterTrigger` state and its `<n-select>` in the template
- Remove `formTriggerType`, `formRuleIds` state from form
- Remove `triggerInfos`, `triggerOptions`, `ruleOptions` computed/state
- Remove the `listRules`, `listTriggers`, `listActionChains`, `createActionChain`, `deleteActionChain` imports
- Remove `AddOutline` icon import if no longer needed
- Simplify `openCreate` and `openEdit` to not set trigger/rule fields
- Simplify `onSave` — only name + pipeline_id

The simplified `columns` array:
```typescript
const columns: DataTableColumns<Action> = [
  { title: 'ID', key: 'id', width: 60, sorter: 'default' },
  { title: 'Name', key: 'name', sorter: 'default', ellipsis: { tooltip: true } },
  {
    title: 'Pipeline', key: 'pipeline_id', width: 80,
    render: (row) => h(NTag, { size: 'small', round: true }, { default: () => `#${row.pipeline_id}` }),
  },
  {
    title: 'Active', key: 'active', width: 80,
    render: (row) => activeTag(row.active),
  },
  {
    title: 'Created', key: 'created_at', width: 160, sorter: 'default',
    render: (row) => new Date(row.created_at).toLocaleString(),
  },
  {
    title: 'Actions', key: 'actions', width: 200,
    render: (row) => h(NSpace, { size: 4 }, {
      default: () => [
        row.active
          ? iconBtn(PlayOutline, 'Run on all tweets', () => onRun(row), { type: 'primary', loading: runningIds.value.has(row.id) })
          : null,
        iconBtn(ListOutline, 'Executions', () => openExecutions(row)),
        iconBtn(CreateOutline, 'Edit', () => openEdit(row)),
        row.active
          ? iconBtn(PauseCircleOutline, 'Deactivate', () =>
              store.deactivate(row.id).catch((e: any) => msg.error(e.message)))
          : iconBtn(CheckmarkCircleOutline, 'Activate', () =>
              store.activate(row.id).catch((e: any) => msg.error(e.message)), { type: 'primary' }),
        iconBtnConfirm(TrashOutline, 'Delete', 'Delete this action?', () =>
          store.remove(row.id).catch((e: any) => msg.error(e.message)), { type: 'error' }),
      ],
    }),
  },
]
```

Simplified form in template (remove trigger + rule fields):
```html
<FormDrawer ...>
  <n-form-item label="Name">
    <n-input v-model:value="formName" placeholder="Action name" />
  </n-form-item>
  <n-form-item label="Pipeline">
    <n-select v-model:value="formPipelineId" :options="pipelineOptions" placeholder="Select pipeline" clearable />
  </n-form-item>
</FormDrawer>
```

- [ ] **Commit**

```bash
git add web/src/views/ActionsView.vue
git commit -m "refactor: simplify ActionsView — remove trigger/rule/chains, plain name+pipeline form"
```

---

### Task 11: Frontend — ChainsView (List Page)

**Files:**
- Create: `web/src/views/ChainsView.vue`

- [ ] **Write `web/src/views/ChainsView.vue`**

```vue
<script setup lang="ts">
import { ref, onMounted, h } from 'vue'
import {
  NDataTable, NButton, NIcon, NSpace, NTag, NFlex, NInput, NFormItem,
  useMessage, type DataTableColumns,
} from 'naive-ui'
import { CreateOutline, TrashOutline, PauseCircleOutline, CheckmarkCircleOutline } from '@vicons/ionicons5'
import { useRouter } from 'vue-router'
import PageHeader from '../components/PageHeader.vue'
import FormDrawer from '../components/FormDrawer.vue'
import { iconBtn, iconBtnConfirm, activeTag } from '../utils/renderActions'
import { useChainsStore } from '../stores/chains'
import type { Chain } from '../api/chains'

const router = useRouter()
const store = useChainsStore()
const msg = useMessage()

const showForm = ref(false)
const saving = ref(false)
const formName = ref('')
const formDescription = ref('')

onMounted(() => store.fetchChains())

function openCreate() {
  formName.value = ''
  formDescription.value = ''
  showForm.value = true
}

async function onSave() {
  if (!formName.value.trim()) { msg.error('Name is required'); return }
  saving.value = true
  try {
    const c = await store.create({ name: formName.value, description: formDescription.value })
    msg.success('Chain created')
    showForm.value = false
    router.push(`/chains/${c.id}/edit`)
  } catch (e: any) { msg.error(e.message) }
  finally { saving.value = false }
}

const columns: DataTableColumns<Chain> = [
  { title: 'ID', key: 'id', width: 60, sorter: 'default' },
  { title: 'Name', key: 'name', sorter: 'default', ellipsis: { tooltip: true } },
  {
    title: 'Active', key: 'active', width: 80,
    render: (row) => activeTag(row.active),
  },
  {
    title: 'Nodes', key: 'node_count', width: 80,
    render: (row) => h(NTag, { size: 'small', round: true }, { default: () => String(row.node_count) }),
  },
  {
    title: 'Created', key: 'created_at', width: 160, sorter: 'default',
    render: (row) => new Date(row.created_at).toLocaleString(),
  },
  {
    title: 'Actions', key: 'actions', width: 200,
    render: (row) => h(NSpace, { size: 4 }, {
      default: () => [
        h(NButton, { size: 'tiny', onClick: () => router.push(`/chains/${row.id}/edit`) }, {
          icon: () => h(NIcon, null, { default: () => h(CreateOutline) }),
          default: () => 'Edit',
        }),
        row.active
          ? iconBtn(PauseCircleOutline, 'Deactivate', () =>
              store.deactivate(row.id).catch((e: any) => msg.error(e.message)))
          : iconBtn(CheckmarkCircleOutline, 'Activate', () =>
              store.activate(row.id).catch((e: any) => msg.error(e.message)), { type: 'primary' }),
        iconBtnConfirm(TrashOutline, 'Delete', 'Delete this chain?', () =>
          store.remove(row.id).catch((e: any) => msg.error(e.message)), { type: 'error' }),
      ],
    }),
  },
]
</script>

<template>
  <PageHeader title="Chains" add-label="New Chain" @add="openCreate" />

  <n-data-table
    :columns="columns"
    :data="store.chains"
    :loading="store.loading"
    :bordered="false"
    :row-key="(row: Chain) => row.id"
    size="small"
  />

  <FormDrawer v-model:show="showForm" title="New Chain" :loading="saving" @save="onSave">
    <n-form-item label="Name">
      <n-input v-model:value="formName" placeholder="Chain name" />
    </n-form-item>
    <n-form-item label="Description">
      <n-input v-model:value="formDescription" placeholder="Optional description" type="textarea" :rows="2" />
    </n-form-item>
  </FormDrawer>
</template>
```

- [ ] **Commit**

```bash
git add web/src/views/ChainsView.vue
git commit -m "feat: add ChainsView — list page with activate/delete and new chain form"
```

---

### Task 12: Frontend — Chain Node Components

**Files:**
- Create: `web/src/components/chain/StartNode.vue`
- Create: `web/src/components/chain/ActionNode.vue`
- Create: `web/src/components/chain/ConditionNode.vue`

Vue Flow node components receive `data` prop. Each registers its handles using `<Handle>` from `@vue-flow/core`.

- [ ] **Write `web/src/components/chain/StartNode.vue`**

```vue
<script setup lang="ts">
import { Handle, Position } from '@vue-flow/core'
</script>

<template>
  <div style="
    padding: 8px 16px; border-radius: 20px;
    background: #313244; border: 2px solid #89dceb;
    font-size: 12px; color: #89dceb; font-weight: 600;
    min-width: 80px; text-align: center;
  ">
    Start
    <Handle type="source" :position="Position.Right" id="default" style="background: #89dceb" />
  </div>
</template>
```

- [ ] **Write `web/src/components/chain/ActionNode.vue`**

```vue
<script setup lang="ts">
import { Handle, Position } from '@vue-flow/core'

defineProps<{
  data: { label: string; pipelineType?: string; selected?: boolean }
}>()
</script>

<template>
  <div style="
    padding: 8px 12px; border-radius: 6px;
    background: #1e1e2e; border: 1px solid #585b70;
    min-width: 120px; font-size: 12px; color: #cdd6f4;
  " :style="{ borderColor: data.selected ? '#cba6f7' : '#585b70' }">
    <Handle type="target" :position="Position.Left" id="input" style="background: #585b70" />
    <div style="font-weight: 600; margin-bottom: 2px">{{ data.label }}</div>
    <div v-if="data.pipelineType" style="font-size: 10px; color: #6c7086">{{ data.pipelineType }}</div>
    <Handle type="source" :position="Position.Right" id="default" style="background: #a6e3a1" />
  </div>
</template>
```

- [ ] **Write `web/src/components/chain/ConditionNode.vue`**

```vue
<script setup lang="ts">
import { Handle, Position } from '@vue-flow/core'
import type { ChainCondition } from '../../api/chains'

defineProps<{
  data: { label: string; conditions?: ChainCondition[]; selected?: boolean }
}>()
</script>

<template>
  <div style="
    padding: 8px 12px;
    background: #1e1e2e; border: 1px solid #f9e2af;
    transform: rotate(45deg); width: 100px; height: 100px;
    display: flex; align-items: center; justify-content: center;
    position: relative;
  " :style="{ borderColor: data.selected ? '#cba6f7' : '#f9e2af' }">
    <Handle type="target" :position="Position.Left" id="input"
      style="background: #585b70; left: -6px; top: 50%; transform: rotate(-45deg)" />

    <div style="transform: rotate(-45deg); text-align: center; font-size: 11px; color: #f9e2af;">
      <div style="font-weight: 600">{{ data.label || 'Condition' }}</div>
      <div v-if="data.conditions?.length" style="font-size: 10px; color: #a6adc8; margin-top: 2px">
        {{ data.conditions.length }} rule{{ data.conditions.length !== 1 ? 's' : '' }}
      </div>
    </div>

    <!-- pass handle: right side -->
    <Handle type="source" :position="Position.Right" id="pass"
      style="background: #a6e3a1; right: -6px; top: 50%; transform: rotate(-45deg)" />
    <!-- fail handle: bottom -->
    <Handle type="source" :position="Position.Bottom" id="fail"
      style="background: #f38ba8; bottom: -6px; left: 50%; transform: rotate(-45deg)" />
  </div>
</template>
```

- [ ] **Commit**

```bash
git add web/src/components/chain/
git commit -m "feat: add Start/Action/Condition Vue Flow node components"
```

---

### Task 13: Frontend — ChainBuilderView

**Files:**
- Create: `web/src/views/ChainBuilderView.vue`

- [ ] **Write `web/src/views/ChainBuilderView.vue`**

This is the full-page Vue Flow builder. It loads the chain graph on mount and saves it via `PUT /api/chains/{id}/graph`.

```vue
<script setup lang="ts">
import { ref, computed, onMounted, markRaw } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import {
  VueFlow, useVueFlow, type Node, type Edge,
  MiniMap, Controls, Background, BackgroundVariant,
} from '@vue-flow/core'
import '@vue-flow/core/dist/style.css'
import '@vue-flow/core/dist/theme-default.css'
import {
  NButton, NFlex, NText, NInput, NSelect, NDivider, NInputNumber,
  NSpin, NScrollbar, useMessage,
} from 'naive-ui'
import { ArrowBackOutline } from '@vicons/ionicons5'
import StartNode from '../components/chain/StartNode.vue'
import ActionNode from '../components/chain/ActionNode.vue'
import ConditionNode from '../components/chain/ConditionNode.vue'
import { getChain, getChainGraph, saveChainGraph, type ChainCondition } from '../api/chains'
import { listActions } from '../api/actions'
import type { Action } from '../api/actions'
import type { Chain } from '../api/chains'

const route  = useRoute()
const router = useRouter()
const msg    = useMessage()
const chainId = Number(route.params.id)

// Vue Flow instance
const { nodes, edges, addNodes, addEdges, onNodesChange, onEdgesChange, applyNodeChanges, applyEdgeChanges, findNode } = useVueFlow()

const nodeTypes = {
  start:     markRaw(StartNode),
  action:    markRaw(ActionNode),
  condition: markRaw(ConditionNode),
}

// State
const chain        = ref<Chain | null>(null)
const actions      = ref<Action[]>([])
const loading      = ref(true)
const saving       = ref(false)
const dirty        = ref(false)
const searchText   = ref('')
const selectedNodeId = ref<string | null>(null)

// Right panel: 'palette' | 'edit'
const panel = ref<'palette' | 'edit'>('palette')

// Edit form state (for selected node)
const editLabel      = ref('')
const editActionId   = ref<number | null>(null)
const editConditions = ref<ChainCondition[]>([])

const filteredActions = computed(() =>
  actions.value.filter(a =>
    !searchText.value || a.name.toLowerCase().includes(searchText.value.toLowerCase())
  )
)

const conditionOpOptions = [
  { label: '==', value: '==' }, { label: '!=', value: '!=' },
  { label: '>', value: '>' }, { label: '>=', value: '>=' },
  { label: '<', value: '<' }, { label: '<=', value: '<=' },
  { label: 'contains', value: 'contains' }, { label: 'not_contains', value: 'not_contains' },
  { label: 'is_empty', value: 'is_empty' }, { label: 'not_empty', value: 'not_empty' },
]

const numericOps = new Set(['>', '>=', '<', '<='])

onMounted(async () => {
  try {
    const [c, graph, acts] = await Promise.all([
      getChain(chainId),
      getChainGraph(chainId),
      listActions(),
    ])
    chain.value = c
    actions.value = acts

    // Convert DB graph → Vue Flow nodes/edges
    const vfNodes: Node[] = graph.nodes.map(n => ({
      id:       String(n.id),
      type:     n.type,
      position: { x: n.pos_x, y: n.pos_y },
      data: {
        label:        n.label || (n.type === 'start' ? 'Start' : ''),
        actionId:     n.action_id,
        conditions:   n.conditions ?? [],
        pipelineType: n.action_id
          ? acts.find(a => a.id === n.action_id)?.name
          : undefined,
      },
    }))

    const vfEdges: Edge[] = graph.edges.map(e => ({
      id:           String(e.id),
      source:       String(e.source_node_id),
      sourceHandle: e.source_handle,
      target:       String(e.target_node_id),
      animated:     e.source_handle === 'pass',
      style:        { stroke: e.source_handle === 'fail' ? '#f38ba8' : e.source_handle === 'pass' ? '#a6e3a1' : '#585b70' },
    }))

    nodes.value = vfNodes
    edges.value = vfEdges

    // If no start node, create one
    if (!vfNodes.some(n => n.type === 'start')) {
      nodes.value = [...nodes.value, {
        id: 'start-0', type: 'start', position: { x: 80, y: 200 },
        data: { label: 'Start' },
      }]
    }
  } catch (e: any) {
    msg.error(e.message)
  } finally {
    loading.value = false
  }
})

onNodesChange((changes) => {
  applyNodeChanges(changes)
  dirty.value = true
})

onEdgesChange((changes) => {
  applyEdgeChanges(changes)
  dirty.value = true
})

function addActionNode(action: Action) {
  const id = `action-${Date.now()}`
  addNodes([{
    id,
    type: 'action',
    position: { x: 300, y: 200 },
    data: { label: action.name, actionId: action.id, pipelineType: `#${action.id}` },
  }])
  dirty.value = true
}

function addConditionNode() {
  const id = `condition-${Date.now()}`
  addNodes([{
    id,
    type: 'condition',
    position: { x: 400, y: 300 },
    data: { label: 'Condition', conditions: [] },
  }])
  dirty.value = true
}

function onNodeClick(_: MouseEvent, node: Node) {
  if (node.type === 'start') return
  selectedNodeId.value = node.id
  editLabel.value = node.data.label ?? ''
  editActionId.value = node.data.actionId ?? null
  editConditions.value = (node.data.conditions ?? []).map((c: ChainCondition) => ({ ...c }))
  panel.value = 'edit'
}

function closeEdit() {
  selectedNodeId.value = null
  panel.value = 'palette'
}

function applyEdit() {
  if (!selectedNodeId.value) return
  const node = findNode(selectedNodeId.value)
  if (!node) return
  node.data = {
    ...node.data,
    label: editLabel.value,
    actionId: editActionId.value,
    conditions: editConditions.value.filter(c => c.key.trim()),
  }
  if (node.type === 'action' && editActionId.value) {
    const a = actions.value.find(a => a.id === editActionId.value)
    if (a) node.data.pipelineType = a.name
  }
  dirty.value = true
  closeEdit()
}

function addConditionRow() {
  editConditions.value = [...editConditions.value, { key: '', op: '==', value: '' }]
}

function removeConditionRow(idx: number) {
  editConditions.value = editConditions.value.filter((_, i) => i !== idx)
}

async function onSave() {
  saving.value = true
  try {
    const nodeRequests = nodes.value.map(n => ({
      client_id:  n.id,
      type:       (n.type ?? 'action') as 'start' | 'action' | 'condition',
      action_id:  n.data.actionId ?? undefined,
      conditions: n.data.conditions ?? [],
      label:      n.data.label ?? '',
      pos_x:      n.position.x,
      pos_y:      n.position.y,
    }))
    const edgeRequests = edges.value.map(e => ({
      source_client_id: e.source,
      source_handle:    e.sourceHandle ?? 'default',
      target_client_id: e.target,
    }))
    const saved = await saveChainGraph(chainId, nodeRequests, edgeRequests)
    // Sync saved DB IDs back into Vue Flow node IDs
    // (rebuild nodes/edges from saved graph so IDs are stable)
    nodes.value = saved.nodes.map(n => ({
      id:       String(n.id),
      type:     n.type,
      position: { x: n.pos_x, y: n.pos_y },
      data: { label: n.label, actionId: n.action_id, conditions: n.conditions ?? [] },
    }))
    edges.value = saved.edges.map(e => ({
      id:           String(e.id),
      source:       String(e.source_node_id),
      sourceHandle: e.source_handle,
      target:       String(e.target_node_id),
      animated:     e.source_handle === 'pass',
      style:        { stroke: e.source_handle === 'fail' ? '#f38ba8' : e.source_handle === 'pass' ? '#a6e3a1' : '#585b70' },
    }))
    dirty.value = false
    msg.success('Chain saved')
  } catch (e: any) {
    msg.error(e.message)
  } finally {
    saving.value = false
  }
}

async function onDiscard() {
  // Reload from server
  loading.value = true
  try {
    const graph = await getChainGraph(chainId)
    nodes.value = graph.nodes.map(n => ({
      id: String(n.id), type: n.type, position: { x: n.pos_x, y: n.pos_y },
      data: { label: n.label, actionId: n.action_id, conditions: n.conditions ?? [] },
    }))
    edges.value = graph.edges.map(e => ({
      id: String(e.id), source: String(e.source_node_id),
      sourceHandle: e.source_handle, target: String(e.target_node_id),
    }))
    dirty.value = false
  } catch (e: any) { msg.error(e.message) }
  finally { loading.value = false }
}

const actionOptions = computed(() =>
  actions.value.map(a => ({ label: `${a.name} (#${a.id})`, value: a.id }))
)
</script>

<template>
  <div style="display: flex; flex-direction: column; height: 100vh; background: #181825;">
    <!-- Header bar -->
    <div style="
      height: 48px; display: flex; align-items: center; gap: 12px;
      padding: 0 16px; background: #1e1e2e;
      border-bottom: 1px solid rgba(255,255,255,0.08); flex-shrink: 0;
    ">
      <n-button text @click="router.push('/chains')">
        <template #icon><n-icon><ArrowBackOutline /></n-icon></template>
        Chains
      </n-button>
      <n-text strong style="color: #cdd6f4">{{ chain?.name ?? '…' }}</n-text>
      <n-text v-if="dirty" depth="3" style="font-size: 12px; color: #f9e2af">● unsaved</n-text>
      <div style="margin-left: auto; display: flex; gap: 8px;">
        <n-button size="small" @click="onDiscard" :disabled="saving || !dirty">Discard</n-button>
        <n-button size="small" type="primary" @click="onSave" :loading="saving" :disabled="!dirty">Save</n-button>
      </div>
    </div>

    <!-- Main content: canvas + right panel -->
    <div style="flex: 1; display: flex; overflow: hidden;">
      <!-- Vue Flow canvas -->
      <div style="flex: 1; position: relative;">
        <n-spin v-if="loading" style="position: absolute; inset: 0; display: flex; align-items: center; justify-content: center;" />
        <VueFlow
          v-else
          :node-types="nodeTypes"
          :default-edge-options="{ animated: false, style: { stroke: '#585b70' } }"
          fit-view-on-init
          @node-click="onNodeClick"
          style="background: #181825; height: 100%;"
        >
          <Background :variant="BackgroundVariant.Dots" :gap="20" :size="1" pattern-color="#313244" />
          <MiniMap style="background: #1e1e2e; border: 1px solid #313244;" />
          <Controls style="background: #1e1e2e; border: 1px solid #313244;" />
        </VueFlow>
      </div>

      <!-- Right panel -->
      <div style="
        width: 240px; background: #1e1e2e;
        border-left: 1px solid rgba(255,255,255,0.08);
        display: flex; flex-direction: column; overflow: hidden;
      ">
        <!-- Palette mode -->
        <template v-if="panel === 'palette'">
          <div style="padding: 12px;">
            <n-button size="small" dashed block @click="addConditionNode" style="margin-bottom: 10px;">
              + Condition node
            </n-button>
            <n-input v-model:value="searchText" placeholder="Search actions…" size="small" clearable />
          </div>
          <n-scrollbar style="flex: 1;">
            <div style="padding: 0 12px 12px;">
              <div
                v-for="action in filteredActions"
                :key="action.id"
                @click="addActionNode(action)"
                style="
                  padding: 6px 8px; margin-bottom: 4px; border-radius: 4px;
                  cursor: pointer; font-size: 12px; color: #cdd6f4;
                  background: #181825; border: 1px solid #313244;
                  user-select: none;
                "
                :style="{ opacity: action.active ? 1 : 0.5 }"
              >
                {{ action.name }}
                <span style="font-size: 10px; color: #6c7086; float: right;">#{{ action.id }}</span>
              </div>
            </div>
          </n-scrollbar>
        </template>

        <!-- Edit mode -->
        <template v-else>
          <div style="padding: 12px; flex: 1; overflow-y: auto;">
            <n-button text size="small" @click="closeEdit" style="margin-bottom: 8px; color: #a6adc8;">
              ← Back to palette
            </n-button>

            <n-text depth="3" style="font-size: 11px; display: block; margin-bottom: 6px;">Label</n-text>
            <n-input v-model:value="editLabel" size="small" placeholder="Node label" style="margin-bottom: 10px;" />

            <template v-if="findNode(selectedNodeId ?? '')?.type === 'action'">
              <n-text depth="3" style="font-size: 11px; display: block; margin-bottom: 6px;">Action</n-text>
              <n-select
                v-model:value="editActionId"
                :options="actionOptions"
                size="small"
                clearable
                style="margin-bottom: 10px;"
              />
            </template>

            <template v-if="findNode(selectedNodeId ?? '')?.type === 'condition'">
              <n-divider style="margin: 8px 0">
                <n-text depth="3" style="font-size: 11px;">Conditions</n-text>
              </n-divider>
              <div v-for="(cond, idx) in editConditions" :key="idx" style="margin-bottom: 6px;">
                <n-input v-model:value="cond.key" size="small" placeholder="key" style="margin-bottom: 3px;" />
                <div style="display: flex; gap: 4px;">
                  <n-select v-model:value="cond.op" :options="conditionOpOptions" size="small" style="flex: 1;" />
                  <n-button size="small" type="error" ghost @click="removeConditionRow(idx)">×</n-button>
                </div>
                <n-input-number
                  v-if="numericOps.has(cond.op)"
                  :value="cond.value === '' ? null : Number(cond.value)"
                  @update:value="v => cond.value = v == null ? '' : String(v)"
                  size="small" placeholder="value" style="width: 100%; margin-top: 3px;"
                />
                <n-input
                  v-else-if="cond.op !== 'is_empty' && cond.op !== 'not_empty'"
                  v-model:value="cond.value"
                  size="small" placeholder="value" style="margin-top: 3px;"
                />
              </div>
              <n-button size="small" dashed block @click="addConditionRow" style="margin-bottom: 10px;">
                + Add condition
              </n-button>
            </template>

            <n-button type="primary" size="small" block @click="applyEdit">Apply</n-button>
          </div>
        </template>
      </div>
    </div>
  </div>
</template>
```

- [ ] **Commit**

```bash
git add web/src/views/ChainBuilderView.vue
git commit -m "feat: add ChainBuilderView — Vue Flow DAG editor with palette, condition editor, save/discard"
```

---

### Task 14: Frontend — TriggersView

**Files:**
- Create: `web/src/views/TriggersView.vue`

- [ ] **Write `web/src/views/TriggersView.vue`**

```vue
<script setup lang="ts">
import { ref, computed, onMounted } from 'vue'
import {
  NFlex, NText, NTag, NButton, NIcon, NSelect, NFormItem, NSwitch,
  NSpin, NCard, NDivider, useMessage,
} from 'naive-ui'
import { AddOutline } from '@vicons/ionicons5'
import PageHeader from '../components/PageHeader.vue'
import FormDrawer from '../components/FormDrawer.vue'
import {
  listTriggers, listTriggerChainLinks, createTriggerChainLink,
  deleteTriggerChainLink, activateTriggerChainLink, deactivateTriggerChainLink,
  type TriggerInfo, type TriggerChainLink,
} from '../api/trigger_links'
import { listChains, type Chain } from '../api/chains'

const msg = useMessage()

const triggers   = ref<TriggerInfo[]>([])
const links      = ref<TriggerChainLink[]>([])
const chains     = ref<Chain[]>([])
const loading    = ref(true)

// Add-link form
const showForm     = ref(false)
const formTrigger  = ref<string>('')
const formChainId  = ref<number | null>(null)
const savingLink   = ref(false)

onMounted(async () => {
  try {
    const [t, l, c] = await Promise.all([listTriggers(), listTriggerChainLinks(), listChains()])
    triggers.value = t
    links.value    = l
    chains.value   = c
  } catch (e: any) { msg.error(e.message) }
  finally { loading.value = false }
})

// Group links by trigger type
const linksByTrigger = computed(() => {
  const map: Record<string, TriggerChainLink[]> = {}
  for (const l of links.value) {
    if (!map[l.trigger_type]) map[l.trigger_type] = []
    map[l.trigger_type].push(l)
  }
  return map
})

const chainOptions = computed(() =>
  chains.value.map(c => ({ label: c.name, value: c.id }))
)

function openAddLink(triggerType: string) {
  formTrigger.value = triggerType
  formChainId.value = null
  showForm.value = true
}

async function onSaveLink() {
  if (!formChainId.value) { msg.error('Chain is required'); return }
  savingLink.value = true
  try {
    const l = await createTriggerChainLink({ trigger_type: formTrigger.value, chain_id: formChainId.value })
    links.value.push(l)
    msg.success('Link created')
    showForm.value = false
  } catch (e: any) { msg.error(e.message) }
  finally { savingLink.value = false }
}

async function removeLink(id: number) {
  try {
    await deleteTriggerChainLink(id)
    links.value = links.value.filter(l => l.id !== id)
  } catch (e: any) { msg.error(e.message) }
}

async function toggleLink(link: TriggerChainLink) {
  try {
    if (link.active) {
      await deactivateTriggerChainLink(link.id)
      link.active = false
    } else {
      await activateTriggerChainLink(link.id)
      link.active = true
    }
  } catch (e: any) { msg.error(e.message) }
}

function chainName(chainId: number) {
  return chains.value.find(c => c.id === chainId)?.name ?? `#${chainId}`
}
</script>

<template>
  <PageHeader title="Triggers" />

  <n-spin v-if="loading" />

  <div v-else>
    <n-card
      v-for="trigger in triggers"
      :key="trigger.type"
      size="small"
      style="margin-bottom: 12px;"
    >
      <n-flex align="center" justify="space-between" style="margin-bottom: 8px;">
        <div>
          <n-tag type="info" size="small" style="font-family: monospace; margin-right: 8px;">
            {{ trigger.type }}
          </n-tag>
          <n-text depth="3" style="font-size: 12px;">{{ trigger.description }}</n-text>
        </div>
        <n-button size="tiny" ghost type="primary" @click="openAddLink(trigger.type)">
          <template #icon><n-icon><AddOutline /></n-icon></template>
          Connect chain
        </n-button>
      </n-flex>

      <n-flex v-if="linksByTrigger[trigger.type]?.length" :size="6" wrap>
        <n-flex
          v-for="link in linksByTrigger[trigger.type]"
          :key="link.id"
          align="center" :size="4"
          style="
            background: #313244; border-radius: 4px; padding: 2px 6px;
            font-size: 12px;
          "
        >
          <n-switch :value="link.active" size="small" @update:value="toggleLink(link)" />
          <span :style="{ color: link.active ? '#cdd6f4' : '#6c7086' }">{{ chainName(link.chain_id) }}</span>
          <n-button text size="tiny" style="color: #f38ba8;" @click="removeLink(link.id)">×</n-button>
        </n-flex>
      </n-flex>
      <n-text v-else depth="3" style="font-size: 12px;">No chains connected.</n-text>
    </n-card>
  </div>

  <FormDrawer
    v-model:show="showForm"
    :title="`Connect chain to ${formTrigger}`"
    save-label="Connect"
    :loading="savingLink"
    @save="onSaveLink"
  >
    <n-form-item label="Chain">
      <n-select
        v-model:value="formChainId"
        :options="chainOptions"
        placeholder="Select a chain"
        clearable
      />
    </n-form-item>
  </FormDrawer>
</template>
```

- [ ] **Commit**

```bash
git add web/src/views/TriggersView.vue
git commit -m "feat: add TriggersView — system triggers with chain link management"
```

---

### Task 15: Build Verification

- [ ] **Build the Go binary in Docker**

```bash
docker compose build --build-arg BUILDKIT_INLINE_CACHE=1 iran-watcher 2>&1 | tail -20
```

Expected: `Successfully built ...` with no errors.

- [ ] **Build the frontend**

```bash
cd web && npm run build 2>&1 | tail -20
```

Expected: build succeeded with no TypeScript errors.

- [ ] **If frontend build fails due to TypeScript errors**, find and fix any remaining references to the old `TriggerType`, `rule_ids`, `trigger_type`, `listActionChains`, `createActionChain`, `deleteActionChain`, `listTriggers` (from `api/pipelines`), `type TriggerInfo` (from `api/pipelines`), or `ActionType` in all `.vue` and `.ts` files under `web/src/`.

Check for stale imports:
```bash
grep -r "TriggerType\|rule_ids\|trigger_type\|listActionChains\|createActionChain\|deleteActionChain\|from.*api/pipelines.*TriggerInfo\|ActionType" web/src/ --include="*.ts" --include="*.vue" -l
```

- [ ] **Run Go tests one final time**

```bash
go test ./... 2>&1
```

Expected: PASS.

- [ ] **Final commit**

```bash
git add -A
git commit -m "build: verify docker + frontend build pass after chains/triggers redesign"
```

---

## Self-Review

**Spec coverage:**
- ✅ Actions simplified (no trigger/rule)
- ✅ Chains: DB tables, CRUD API, graph API
- ✅ Chain builder: Vue Flow canvas, action/condition nodes, save/discard
- ✅ Triggers page: system triggers + chain link management
- ✅ Router + nav updated
- ✅ Worker/stream cleaned up (chain execution deferred with TODO comments)
- ✅ Migration drops action_chains, modifies actions, creates new tables
- ✅ `chain_only` trigger type removed from model constants; `TriggerType` type removed

**Placeholder scan:** None found.

**Type consistency:**
- `ChainNodeRequest.client_id` (Go) ↔ `client_id` (TS) ✅
- `ChainEdgeRequest.source_client_id` / `target_client_id` ✅
- `model.ChainCondition.Operator` field: note the Go struct uses `Operator string \`json:"op"\`` — the JSON key is `op`, matching the TS `ChainCondition.op` ✅
- Store method `SaveChainGraph(ctx, chainID, []*ChainNodeRequest, []*ChainEdgeRequest)` is consistent with how the handler decodes `model.ChainGraphRequest` and passes `.Nodes` and `.Edges` ✅
