# Chains & Triggers Redesign

**Date:** 2026-03-29
**Status:** Approved

## Overview

Restructure the action/chain/trigger system into three distinct, decoupled concepts:

- **Actions** — a named pipeline step (name + pipeline only; no trigger type, no rule filter)
- **Chains** — a named DAG workflow of action and condition nodes, built visually
- **Triggers** — system events that fire one or more chains when they occur

The current tight coupling (Action owns trigger_type; ActionChain links action-to-action) is replaced by a proper workflow model. All existing `actions` and `action_chains` data can be dropped; tweets are preserved.

---

## Data Model

### Modified: `actions` table

Remove `trigger_type` and `rule_ids` columns. Final shape:

```
id          INTEGER PRIMARY KEY
name        TEXT NOT NULL
pipeline_id INTEGER NOT NULL REFERENCES pipelines(id)
active      BOOLEAN DEFAULT false
created_at  DATETIME
updated_at  DATETIME
```

### Dropped: `action_chains` table

Replaced entirely by `chain_nodes` and `chain_edges`.

### New: `chains`

```
id          INTEGER PRIMARY KEY
name        TEXT NOT NULL
description TEXT
active      BOOLEAN DEFAULT false
created_at  DATETIME
updated_at  DATETIME
```

### New: `chain_nodes`

```
id          INTEGER PRIMARY KEY
chain_id    INTEGER NOT NULL REFERENCES chains(id) ON DELETE CASCADE
type        TEXT NOT NULL  -- 'start' | 'action' | 'condition'
action_id   INTEGER REFERENCES actions(id)  -- only for type='action'
conditions  TEXT           -- JSON []ChainCondition, only for type='condition'
label       TEXT           -- display label shown on canvas node
pos_x       REAL           -- canvas x position
pos_y       REAL           -- canvas y position
created_at  DATETIME
```

Node types:
- `start` — one per chain; the entry point; no action_id, no conditions
- `action` — references an action, runs its pipeline when reached
- `condition` — evaluates a ChainCondition array; routes via labeled output handles

### New: `chain_edges`

```
id              INTEGER PRIMARY KEY
chain_id        INTEGER NOT NULL REFERENCES chains(id) ON DELETE CASCADE
source_node_id  INTEGER NOT NULL REFERENCES chain_nodes(id) ON DELETE CASCADE
source_handle   TEXT NOT NULL  -- 'default' for start/action; 'pass'/'fail' or custom for condition
target_node_id  INTEGER NOT NULL REFERENCES chain_nodes(id) ON DELETE CASCADE
created_at      DATETIME
```

`source_handle` meanings:
- Action / start nodes: always `'default'` (single outgoing path)
- Condition nodes: `'pass'` and `'fail'` by default; user may rename to any label

### New: `trigger_chain_links`

```
id           INTEGER PRIMARY KEY
trigger_type TEXT NOT NULL   -- matches model.TriggerType values
chain_id     INTEGER NOT NULL REFERENCES chains(id) ON DELETE CASCADE
active       BOOLEAN DEFAULT false
created_at   DATETIME
```

Many-to-many between trigger types and chains. One trigger type can fire multiple chains; one chain can be reused across multiple trigger types.

---

## Backend: ChainCondition

Reuses the existing `model.ChainCondition` type (key / op / value). Supported operators remain: `==`, `!=`, `>`, `>=`, `<`, `<=`, `contains`, `not_contains`, `is_empty`, `not_empty`.

Rule matching (formerly `rule_ids` on Action) becomes a condition with key `rule_id` and op `==` or `contains` inside a Condition node.

---

## API Routes

### Chains

```
GET    /api/chains                list chains
POST   /api/chains                create chain  { name, description }
GET    /api/chains/{id}           get chain metadata
PUT    /api/chains/{id}           update name/description
DELETE /api/chains/{id}           delete chain (cascades nodes+edges)
POST   /api/chains/{id}/activate
POST   /api/chains/{id}/deactivate
GET    /api/chains/{id}/graph     get full DAG: { nodes: [], edges: [] }
PUT    /api/chains/{id}/graph     replace full DAG (atomic: delete all + insert)
```

`GET /api/chains/{id}/graph` response:
```json
{
  "nodes": [
    { "id": 1, "type": "start", "label": "Start", "pos_x": 100, "pos_y": 200 },
    { "id": 2, "type": "action", "action_id": 5, "label": "Classify", "pos_x": 300, "pos_y": 200 },
    { "id": 3, "type": "condition", "label": "High confidence?",
      "conditions": [{"key": "confidence", "op": ">=", "value": "0.8"}],
      "pos_x": 500, "pos_y": 200 }
  ],
  "edges": [
    { "id": 1, "source_node_id": 1, "source_handle": "default", "target_node_id": 2 },
    { "id": 2, "source_node_id": 2, "source_handle": "default", "target_node_id": 3 },
    { "id": 3, "source_node_id": 3, "source_handle": "pass",    "target_node_id": 2 }
  ]
}
```

### Triggers

```
GET    /api/triggers                    list system trigger types (existing)
GET    /api/triggers/links              list all trigger→chain links
POST   /api/triggers/links              create link { trigger_type, chain_id }
DELETE /api/triggers/links/{id}         remove link
POST   /api/triggers/links/{id}/activate
POST   /api/triggers/links/{id}/deactivate
```

### Actions (simplified)

```
GET    /api/actions                     list actions (no trigger_type filter)
POST   /api/actions                     create { name, pipeline_id }
PUT    /api/actions/{id}                update { name, pipeline_id }
DELETE /api/actions/{id}
POST   /api/actions/{id}/activate
POST   /api/actions/{id}/deactivate
POST   /api/actions/{id}/run            manual reprocess
GET    /api/actions/{id}/executions
```

Dropped: `GET|POST|DELETE /api/actions/{id}/chains`

---

## Frontend

### Actions (`/actions`)

Simplified table — columns: ID, Name, Pipeline, Active, Created, Actions (edit/activate/delete/run/executions). Remove the trigger column, rule count column, and expandable chains panel. Create/edit form has only name + pipeline fields.

Twitter OAuth card stays on this page.

### Chains (`/chains`)

Table view with columns: ID, Name, Active, Node count (computed from `chain_nodes`), Created, Actions (Edit / Activate / Delete).

"New Chain" button opens a small name/description form, then navigates to the builder on save.

### Chain Builder (`/chains/:id/edit`)

Full-page layout (no `.page-content` padding):

```
┌─────────────────────────────────────────┬──────────────────────┐
│  ← Back to Chains    [Chain Name]       │  Right panel         │
├─────────────────────────────────────────┤                      │
│                                         │  [+ Condition node]  │
│     Vue Flow canvas                     │  ─────────────────   │
│     (dark background, dot grid)         │  Search actions…     │
│                                         │  • Classify Tweet    │
│     Nodes are draggable                 │  • Summarize         │
│     Edges connect handles               │  • Notify Pushover   │
│                                         │  • …                 │
│  [Save]  [Discard]          bottom bar  │                      │
└─────────────────────────────────────────┴──────────────────────┘
```

**Node types on canvas:**

- **Start node** — rounded rectangle, single output handle (`default`); one per chain, not deletable
- **Action node** — rounded rectangle showing action name + pipeline type badge; single input handle, single output handle (`default`)
- **Condition node** — diamond shape (or distinct style); shows label + condition summary; single input handle, two output handles: `pass` (green) and `fail` (red); handles are renameable

**Interactions:**
- Drag an action from the right panel onto canvas → creates an action node at drop position
- Click "+ Condition node" → creates a condition node near the center
- Draw edges by dragging from a source handle to a target node's input handle
- Click a node → inline editing panel slides in (right panel replaces palette): edit label, conditions (for condition nodes), or swap action (for action nodes)
- Delete selected node/edge with Backspace/Delete key
- "Save" calls `PUT /api/chains/{id}/graph` with current nodes + edges
- "Discard" reloads from server

**Vue Flow** (vueflow/core) is used for the canvas. Custom node components are registered for each node type.

### Triggers (`/triggers`)

Table of system trigger types (fetched from `GET /api/triggers`):

| Trigger type | Description | Connected chains | |
|---|---|---|---|
| new_tweet | New tweet matched stream rule | [Classify Chain] [Analysis Chain] | + |
| digest_completed | Digest job finished | — | + |
| stream_connected | Stream came online | — | + |

- Each connected chain renders as a tag with a remove (×) button
- `+` button opens a small drawer to select a chain and set active on link
- Active/inactive toggle per link
- Removing a link calls `DELETE /api/triggers/links/{id}`

### Navigation

Add `Chains` and `Triggers` to the admin menu in `App.vue` (between Actions and Digest). Remove chains expand from Actions table.

---

## Worker Execution

The existing worker processes tasks by action_id. With chains, when a trigger fires:

1. All active `trigger_chain_links` for that trigger_type are fetched
2. For each linked chain, a `chain_execute` task is pushed to Redis
3. Workers walk the DAG: start at the `start` node, traverse edges, execute action nodes (push action tasks), evaluate condition nodes (follow pass/fail handle), continue until no outgoing edges

This is a follow-on backend change; the worker execution model is out of scope for this spec and will be designed separately. The UI and data model are the primary scope here.

---

## Migration

- Drop `action_chains` table
- Drop `trigger_type` and `rule_ids` columns from `actions`
- Remove `chain_only` from the `TriggerType` enum (it was used only for actions triggered by other actions; this concept is now absorbed by chain node traversal)
- Create `chains`, `chain_nodes`, `chain_edges`, `trigger_chain_links` tables
- `tweets` table and all tweet data are preserved
- All other data (actions, pipelines, etc.) can be re-entered via the UI
