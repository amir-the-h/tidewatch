-- 016_event_driven.sql: event-driven pipeline refactor
-- Replaces models table with pipelines, adds action_chains, restructures actions.

-- 1. Create pipelines table (replaces models, same IDs preserved)
CREATE TABLE IF NOT EXISTS pipelines (
    id          INTEGER PRIMARY KEY AUTOINCREMENT,
    name        TEXT    NOT NULL UNIQUE,
    type        TEXT    NOT NULL,    -- onnx | ollama | internal
    config      TEXT    NOT NULL DEFAULT '{}',
    active      INTEGER NOT NULL DEFAULT 1,
    created_at  DATETIME NOT NULL DEFAULT (datetime('now')),
    updated_at  DATETIME NOT NULL DEFAULT (datetime('now'))
);

-- 2. Migrate models → pipelines (preserve IDs so existing action config model_id references remain valid)
INSERT OR IGNORE INTO pipelines (id, name, type, config, active, created_at, updated_at)
SELECT
    m.id,
    m.name,
    CASE
        WHEN m.type IN ('summarizer', 'extraction', 'digest') THEN 'ollama'
        ELSE 'onnx'
    END,
    json_object('path', m.path, 'model_type', m.type),
    m.active,
    m.created_at,
    m.created_at
FROM models m;

-- 3. Create internal pipeline rows for twitter action types that exist in actions table
INSERT OR IGNORE INTO pipelines (name, type, config, active, created_at, updated_at)
SELECT DISTINCT
    'twitter-' || type,
    'internal',
    json_object('handler', 'twitter_' || type),
    1,
    datetime('now'),
    datetime('now')
FROM actions WHERE type IN ('retweet', 'like', 'quote');

-- 4. pipeline_labels table (replaces model_labels, references pipelines)
CREATE TABLE IF NOT EXISTS pipeline_labels (
    pipeline_id  INTEGER NOT NULL REFERENCES pipelines(id) ON DELETE CASCADE,
    label        TEXT    NOT NULL,
    PRIMARY KEY (pipeline_id, label)
);

INSERT OR IGNORE INTO pipeline_labels (pipeline_id, label)
SELECT model_id, label FROM model_labels;

-- 5. action_chains table
CREATE TABLE IF NOT EXISTS action_chains (
    id               INTEGER PRIMARY KEY AUTOINCREMENT,
    source_action_id INTEGER NOT NULL REFERENCES actions(id) ON DELETE CASCADE,
    target_action_id INTEGER NOT NULL REFERENCES actions(id) ON DELETE CASCADE,
    conditions       TEXT    NOT NULL DEFAULT '[]',
    created_at       DATETIME NOT NULL DEFAULT (datetime('now'))
);
CREATE INDEX IF NOT EXISTS idx_action_chains_source ON action_chains(source_action_id);
CREATE INDEX IF NOT EXISTS idx_action_chains_target ON action_chains(target_action_id);

-- 6. Restructure actions table (rename + recreate — SQLite can't drop/change columns directly)
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

-- 7. Migrate pipeline-type actions (pipeline_id = model_id from config JSON)
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

-- 8. Migrate twitter-type actions (pipeline_id = the internal pipeline we just created)
INSERT INTO actions (id, name, trigger_type, pipeline_id, rule_ids, active, created_at, updated_at)
SELECT
    a.id,
    a.name,
    'new_tweet',
    (SELECT p.id FROM pipelines p
     WHERE p.name = 'twitter-' || a.type AND p.type = 'internal'
     LIMIT 1),
    a.rule_ids,
    a.active,
    a.created_at,
    a.created_at
FROM actions_old a
WHERE a.type IN ('retweet', 'like', 'quote');

DROP TABLE actions_old;

-- 9. Drop superseded tables
DROP TABLE IF EXISTS rule_models;
DROP TABLE IF EXISTS model_labels;
DROP TABLE IF EXISTS models;
