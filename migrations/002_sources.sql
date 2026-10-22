-- 002_sources.sql
-- Phase 1 step 2: generic sources (configured connector instances).
--
-- The X rule builder tables move out of the way first: sources → x_terms and
-- rules → x_rules. SQLite rewrites foreign keys that point at a renamed table,
-- so x_rule_matches.rule_id follows to x_rules. The renames must run before
-- the new sources table exists.

ALTER TABLE sources RENAME TO x_terms;
ALTER TABLE rules RENAME TO x_rules;

-- ── Sources ───────────────────────────────────────────────────────────────

-- A configured connector instance.
CREATE TABLE sources (
    id               INTEGER PRIMARY KEY AUTOINCREMENT,
    name             TEXT    NOT NULL UNIQUE,
    type             TEXT    NOT NULL,              -- connector type: x_stream, x_search, rss, …
    config           TEXT    NOT NULL DEFAULT '{}', -- connector config JSON, secrets included (masked by the API)
    poll_interval_s  INTEGER,                       -- poll connectors only; NULL = connector default
    tier             INTEGER CHECK (tier BETWEEN 1 AND 4),
    state_affiliated INTEGER NOT NULL DEFAULT 0,
    region           TEXT    NOT NULL DEFAULT '',
    language         TEXT    NOT NULL DEFAULT '',
    active           INTEGER NOT NULL DEFAULT 0,
    created_at       DATETIME NOT NULL DEFAULT (datetime('now')),
    updated_at       DATETIME NOT NULL DEFAULT (datetime('now'))
);
CREATE INDEX idx_sources_type ON sources(type);

-- Cursor and last-known health per source, so restarts resume where they left off.
CREATE TABLE source_state (
    source_id       INTEGER PRIMARY KEY REFERENCES sources(id) ON DELETE CASCADE,
    cursor          TEXT     NOT NULL DEFAULT '{}', -- connector-owned JSON: since_id, ETag, seen GUIDs…
    last_run_at     DATETIME,
    last_success_at DATETIME,
    last_error      TEXT     NOT NULL DEFAULT '',
    updated_at      DATETIME NOT NULL DEFAULT (datetime('now'))
);

-- Manual fetches and backfills.
CREATE TABLE source_runs (
    id            TEXT PRIMARY KEY,                   -- UUID
    source_id     INTEGER NOT NULL REFERENCES sources(id) ON DELETE CASCADE,
    kind          TEXT    NOT NULL,                   -- fetch | backfill
    params        TEXT    NOT NULL DEFAULT '{}',
    status        TEXT    NOT NULL DEFAULT 'pending', -- pending | running | done | error
    fetched_count INTEGER NOT NULL DEFAULT 0,
    new_count     INTEGER NOT NULL DEFAULT 0,
    error         TEXT,
    created_at    DATETIME NOT NULL DEFAULT (datetime('now')),
    completed_at  DATETIME
);
CREATE INDEX idx_source_runs_source_id ON source_runs(source_id, created_at);

-- signal_sources was created without a foreign key in 001 and nothing wrote
-- it, so it is rebuilt now that sources exists.
DROP TABLE signal_sources;

CREATE TABLE signal_sources (
    signal_id INTEGER NOT NULL REFERENCES signals(id) ON DELETE CASCADE,
    source_id INTEGER NOT NULL REFERENCES sources(id) ON DELETE CASCADE,
    seen_at   DATETIME NOT NULL DEFAULT (datetime('now')),
    PRIMARY KEY (signal_id, source_id)
);
CREATE INDEX idx_signal_sources_source_id ON signal_sources(source_id);
