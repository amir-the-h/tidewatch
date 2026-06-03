-- 029_pipeline_nodes_action_fk_cascade.sql
-- Fix pipeline_nodes.action_id FK to use ON DELETE SET NULL.
-- Without this, deleting an action referenced by any pipeline node fails
-- with "FOREIGN KEY constraint failed".
-- SET NULL preserves the pipeline structure and makes the broken node visible
-- in the UI rather than silently cascading deletes.

PRAGMA foreign_keys = OFF;

CREATE TABLE pipeline_nodes_new (
    id              INTEGER PRIMARY KEY,
    pipeline_id     INTEGER NOT NULL REFERENCES pipelines(id) ON DELETE CASCADE,
    type            TEXT    NOT NULL,
    action_id       INTEGER REFERENCES actions(id) ON DELETE SET NULL,
    sub_pipeline_id INTEGER REFERENCES pipelines(id),
    conditions      TEXT,
    label           TEXT,
    pos_x           REAL    NOT NULL DEFAULT 0,
    pos_y           REAL    NOT NULL DEFAULT 0,
    created_at      TEXT
);

INSERT INTO pipeline_nodes_new
    (id, pipeline_id, type, action_id, sub_pipeline_id, conditions, label, pos_x, pos_y, created_at)
    SELECT id, pipeline_id, type, action_id, sub_pipeline_id, conditions, label, pos_x, pos_y, created_at
    FROM pipeline_nodes;

DROP TABLE pipeline_nodes;
ALTER TABLE pipeline_nodes_new RENAME TO pipeline_nodes;

PRAGMA foreign_keys = ON;
