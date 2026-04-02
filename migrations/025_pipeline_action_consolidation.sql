-- migrations/025_pipeline_action_consolidation.sql
PRAGMA foreign_keys = OFF;

-- ── Step 1: new actions table (absorbs pipelines data) ──────────────────────
CREATE TABLE actions_new (
    id         INTEGER PRIMARY KEY,
    name       TEXT    NOT NULL UNIQUE,
    type       TEXT    NOT NULL,
    config     TEXT    NOT NULL DEFAULT '{}',
    active     INTEGER NOT NULL DEFAULT 1,
    created_at TEXT,
    updated_at TEXT
);
INSERT INTO actions_new (id, name, type, config, active, created_at, updated_at)
    SELECT id, name, type, config, active, created_at, updated_at FROM pipelines;

-- ── Step 2: new pipeline_nodes (remap action_id via old thin actions table) ──
-- action_id: chain_nodes.action_id → old_actions.pipeline_id → new actions.id
-- (new actions.id == old pipelines.id, so the lookup old_actions.pipeline_id is correct)

-- Guard: abort if any action node references an action with NULL pipeline_id
SELECT CASE WHEN COUNT(*) > 0
    THEN RAISE(ABORT, 'migration: action node references an action with NULL pipeline_id — data integrity error')
    END
FROM chain_nodes cn
JOIN actions a ON a.id = cn.action_id
WHERE cn.type = 'action' AND a.pipeline_id IS NULL;

CREATE TABLE pipeline_nodes_new (
    id              INTEGER PRIMARY KEY,
    pipeline_id     INTEGER NOT NULL,
    type            TEXT    NOT NULL,
    action_id       INTEGER,
    sub_pipeline_id INTEGER,
    conditions      TEXT,
    label           TEXT,
    pos_x           REAL    NOT NULL DEFAULT 0,
    pos_y           REAL    NOT NULL DEFAULT 0,
    created_at      TEXT
);
INSERT INTO pipeline_nodes_new
    (id, pipeline_id, type, action_id, sub_pipeline_id, conditions, label, pos_x, pos_y, created_at)
    SELECT
        cn.id,
        cn.chain_id,
        cn.type,
        CASE WHEN cn.action_id IS NOT NULL
             THEN (SELECT a.pipeline_id FROM actions a WHERE a.id = cn.action_id)
             ELSE NULL END,
        NULL,
        cn.conditions, cn.label, cn.pos_x, cn.pos_y, cn.created_at
    FROM chain_nodes cn;

-- ── Step 3: drop old chain_nodes, promote pipeline_nodes ────────────────────
DROP TABLE chain_nodes;
ALTER TABLE pipeline_nodes_new RENAME TO pipeline_nodes;

-- ── Step 4: drop old pipelines + actions, promote actions_new ───────────────

-- Remap action_executions.action_id from old thin-action IDs to new action IDs
-- (old actions.pipeline_id == new actions.id, since new actions absorb old pipelines)

-- Guard: abort if any action_executions row references an action with NULL pipeline_id
SELECT CASE WHEN COUNT(*) > 0
    THEN RAISE(ABORT, 'migration: action_executions references an action with NULL pipeline_id — data integrity error')
    END
FROM action_executions ae
JOIN actions a ON a.id = ae.action_id
WHERE a.pipeline_id IS NULL;

UPDATE action_executions
    SET action_id = (SELECT a.pipeline_id FROM actions a WHERE a.id = action_executions.action_id)
    WHERE EXISTS (SELECT 1 FROM actions a WHERE a.id = action_executions.action_id);

-- Note: action_executions rows whose action_id has no match in the old actions table
-- (orphaned executions) are left with their original action_id. These are pre-existing
-- data quality issues and are left unchanged as the safest option.

DROP TABLE pipelines;
DROP TABLE actions;
ALTER TABLE actions_new RENAME TO actions;

-- ── Step 5: rename chains → pipelines ───────────────────────────────────────
ALTER TABLE chains RENAME TO pipelines;

-- ── Step 6: recreate pipeline_edges with correct FKs ────────────────────────
CREATE TABLE pipeline_edges (
    id              INTEGER PRIMARY KEY,
    pipeline_id     INTEGER NOT NULL REFERENCES pipelines(id) ON DELETE CASCADE,
    source_node_id  INTEGER NOT NULL REFERENCES pipeline_nodes(id) ON DELETE CASCADE,
    source_handle   TEXT    NOT NULL DEFAULT 'default',
    target_node_id  INTEGER NOT NULL REFERENCES pipeline_nodes(id) ON DELETE CASCADE,
    created_at      TEXT    NOT NULL
);
INSERT INTO pipeline_edges (id, pipeline_id, source_node_id, source_handle, target_node_id, created_at)
    SELECT id, chain_id, source_node_id, source_handle, target_node_id, created_at FROM chain_edges;
DROP TABLE chain_edges;

-- ── Step 7: recreate trigger_pipeline_links with correct FKs ────────────────
CREATE TABLE trigger_pipeline_links (
    id           INTEGER PRIMARY KEY,
    trigger_type TEXT    NOT NULL,
    pipeline_id  INTEGER NOT NULL REFERENCES pipelines(id) ON DELETE CASCADE,
    active       INTEGER NOT NULL DEFAULT 0,
    created_at   TEXT    NOT NULL
);
INSERT INTO trigger_pipeline_links (id, trigger_type, pipeline_id, active, created_at)
    SELECT id, trigger_type, chain_id, active, created_at FROM trigger_chain_links;
DROP TABLE trigger_chain_links;

-- ── Step 8: recreate action_labels with correct FKs ─────────────────────────
CREATE TABLE action_labels (
    action_id INTEGER NOT NULL REFERENCES actions(id) ON DELETE CASCADE,
    label     TEXT    NOT NULL,
    PRIMARY KEY (action_id, label)
);
INSERT INTO action_labels (action_id, label)
    SELECT pipeline_id, label FROM pipeline_labels;
DROP TABLE pipeline_labels;

-- ── Step 9: recreate action_backends with correct FKs ───────────────────────
CREATE TABLE action_backends (
    action_id  INTEGER NOT NULL REFERENCES actions(id)         ON DELETE CASCADE,
    backend_id INTEGER NOT NULL REFERENCES ollama_backends(id) ON DELETE CASCADE,
    priority   INTEGER NOT NULL,
    PRIMARY KEY (action_id, backend_id)
);
INSERT INTO action_backends (action_id, backend_id, priority)
    SELECT pipeline_id, backend_id, priority FROM pipeline_backends;
DROP TABLE pipeline_backends;

-- ── Step 10: add proper FKs to pipeline_nodes (now pipelines/actions exist) ─
CREATE TABLE pipeline_nodes_final (
    id              INTEGER PRIMARY KEY,
    pipeline_id     INTEGER NOT NULL REFERENCES pipelines(id) ON DELETE CASCADE,
    type            TEXT    NOT NULL,
    action_id       INTEGER REFERENCES actions(id),
    sub_pipeline_id INTEGER REFERENCES pipelines(id),
    conditions      TEXT,
    label           TEXT,
    pos_x           REAL    NOT NULL DEFAULT 0,
    pos_y           REAL    NOT NULL DEFAULT 0,
    created_at      TEXT
);
INSERT INTO pipeline_nodes_final
    (id, pipeline_id, type, action_id, sub_pipeline_id, conditions, label, pos_x, pos_y, created_at)
    SELECT id, pipeline_id, type, action_id, sub_pipeline_id, conditions, label, pos_x, pos_y, created_at
    FROM pipeline_nodes;
DROP TABLE pipeline_nodes;
ALTER TABLE pipeline_nodes_final RENAME TO pipeline_nodes;

-- ── Step 11: pipeline_node_executions (new table for sub-pipeline fan-in) ───
CREATE TABLE pipeline_node_executions (
    id             INTEGER PRIMARY KEY,
    parent_node_id INTEGER NOT NULL REFERENCES pipeline_nodes(id) ON DELETE CASCADE,
    tweet_id       TEXT    NOT NULL,
    leaf_node_id   INTEGER NOT NULL REFERENCES pipeline_nodes(id) ON DELETE CASCADE,
    completed      INTEGER NOT NULL DEFAULT 0,
    created_at     TEXT
);
CREATE INDEX idx_pne_lookup ON pipeline_node_executions (parent_node_id, tweet_id);

PRAGMA foreign_keys = ON;
