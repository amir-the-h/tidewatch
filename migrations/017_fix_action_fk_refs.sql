-- 017_fix_action_fk_refs.sql
-- Migration 016 renamed `actions` to `actions_old` then dropped it.
-- SQLite 3.26+ auto-rewrites FK references during table rename, so
-- action_chains and action_executions were left referencing the
-- now-deleted `actions_old` table.  Rebuild them against `actions`.

PRAGMA foreign_keys = OFF;

-- Rebuild action_chains
CREATE TABLE action_chains_new (
    id               INTEGER PRIMARY KEY AUTOINCREMENT,
    source_action_id INTEGER NOT NULL REFERENCES actions(id) ON DELETE CASCADE,
    target_action_id INTEGER NOT NULL REFERENCES actions(id) ON DELETE CASCADE,
    conditions       TEXT    NOT NULL DEFAULT '[]',
    created_at       DATETIME NOT NULL DEFAULT (datetime('now'))
);
INSERT INTO action_chains_new SELECT * FROM action_chains;
DROP TABLE action_chains;
ALTER TABLE action_chains_new RENAME TO action_chains;
CREATE INDEX IF NOT EXISTS idx_action_chains_source ON action_chains(source_action_id);
CREATE INDEX IF NOT EXISTS idx_action_chains_target ON action_chains(target_action_id);

-- Rebuild action_executions
CREATE TABLE action_executions_new (
    id          INTEGER PRIMARY KEY AUTOINCREMENT,
    action_id   INTEGER NOT NULL REFERENCES actions(id) ON DELETE CASCADE,
    tweet_id    TEXT    NOT NULL,
    status      TEXT    NOT NULL,
    error       TEXT,
    executed_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);
INSERT INTO action_executions_new SELECT * FROM action_executions;
DROP TABLE action_executions;
ALTER TABLE action_executions_new RENAME TO action_executions;
CREATE INDEX IF NOT EXISTS idx_action_executions_action_id   ON action_executions(action_id);
CREATE INDEX IF NOT EXISTS idx_action_executions_tweet_id    ON action_executions(tweet_id);
CREATE INDEX IF NOT EXISTS idx_action_executions_status      ON action_executions(status);
CREATE INDEX IF NOT EXISTS idx_action_executions_executed_at ON action_executions(executed_at);

PRAGMA foreign_keys = ON;
