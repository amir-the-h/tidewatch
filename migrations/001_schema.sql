-- 001_schema.sql
-- Phase 1 schema reset: replaces migrations 001–029. A database created by the
-- old migrations is not upgraded; RunMigrations refuses to start on one.
--
-- Statements are split on ";" by the migration runner, so no semicolons inside
-- string literals or triggers.

-- ── Users ─────────────────────────────────────────────────────────────────

CREATE TABLE users (
    id            INTEGER PRIMARY KEY AUTOINCREMENT,
    username      TEXT    NOT NULL UNIQUE,
    password_hash TEXT    NOT NULL,
    role          TEXT    NOT NULL DEFAULT 'viewer', -- 'admin' or 'viewer'
    created_at    DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at    DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);

-- ── X rule builder (renamed to x_terms / x_rules in phase 1 step 3) ───────

-- Rule atoms (keyword, hashtag, from: account…) combined into X rules.
CREATE TABLE sources (
    id         INTEGER PRIMARY KEY AUTOINCREMENT,
    name       TEXT NOT NULL DEFAULT '',
    type       TEXT NOT NULL,
    class      TEXT NOT NULL DEFAULT 'atom',
    operator   TEXT NOT NULL DEFAULT '',
    value      TEXT NOT NULL,
    active     INTEGER NOT NULL DEFAULT 1,
    created_at DATETIME NOT NULL DEFAULT (datetime('now')),
    updated_at DATETIME NOT NULL DEFAULT (datetime('now'))
);

CREATE TABLE rules (
    id             INTEGER PRIMARY KEY AUTOINCREMENT,
    name           TEXT NOT NULL,
    expression     TEXT NOT NULL,
    compiled_value TEXT NOT NULL DEFAULT '',
    x_rule_id      TEXT NOT NULL DEFAULT '',
    active         INTEGER NOT NULL DEFAULT 0,
    local          INTEGER NOT NULL DEFAULT 0,
    created_at     DATETIME NOT NULL DEFAULT (datetime('now')),
    updated_at     DATETIME NOT NULL DEFAULT (datetime('now'))
);

CREATE TABLE search_jobs (
    id             TEXT    PRIMARY KEY,
    status         TEXT    NOT NULL DEFAULT 'pending',
    params         TEXT    NOT NULL DEFAULT '{}',
    tweets_fetched INTEGER NOT NULL DEFAULT 0,
    error          TEXT,
    created_at     DATETIME NOT NULL DEFAULT (datetime('now')),
    completed_at   DATETIME
);

CREATE TABLE twitter_credentials (
    id              INTEGER PRIMARY KEY DEFAULT 1,
    access_token    TEXT    NOT NULL,
    refresh_token   TEXT    NOT NULL,
    expires_at      DATETIME NOT NULL,
    twitter_user_id TEXT    NOT NULL,
    username        TEXT    NOT NULL,
    updated_at      DATETIME NOT NULL
);

-- ── Signals ───────────────────────────────────────────────────────────────

CREATE TABLE signals (
    id           INTEGER PRIMARY KEY AUTOINCREMENT,
    kind         TEXT    NOT NULL,                 -- post | article
    ext_ns       TEXT    NOT NULL,                 -- dedup namespace: x, rss, …
    external_id  TEXT    NOT NULL,
    author       TEXT    NOT NULL DEFAULT '',
    author_id    TEXT    NOT NULL DEFAULT '',
    title        TEXT    NOT NULL DEFAULT '',
    text         TEXT    NOT NULL DEFAULT '',
    url          TEXT    NOT NULL DEFAULT '',
    lang         TEXT    NOT NULL DEFAULT '',
    lat          REAL,
    lon          REAL,
    attrs        TEXT    NOT NULL DEFAULT '{}',    -- connector-specific fields, merged into pipeline context
    raw_json     BLOB,
    published_at DATETIME NOT NULL,
    fetched_at   DATETIME NOT NULL DEFAULT (datetime('now')),
    UNIQUE (ext_ns, external_id)
);
CREATE INDEX idx_signals_published_at ON signals(published_at);
CREATE INDEX idx_signals_kind         ON signals(kind);

-- Which sources saw a signal. source_id gets its foreign key to the generic
-- sources table in phase 1 step 2, which rebuilds this table; nothing writes
-- to it before then.
CREATE TABLE signal_sources (
    signal_id INTEGER NOT NULL REFERENCES signals(id) ON DELETE CASCADE,
    source_id INTEGER NOT NULL,
    seen_at   DATETIME NOT NULL DEFAULT (datetime('now')),
    PRIMARY KEY (signal_id, source_id)
);
CREATE INDEX idx_signal_sources_source_id ON signal_sources(source_id);

-- X stream rules a signal matched (rule_id points at x_rules from step 3).
CREATE TABLE x_rule_matches (
    signal_id INTEGER NOT NULL REFERENCES signals(id) ON DELETE CASCADE,
    rule_id   INTEGER NOT NULL REFERENCES rules(id)   ON DELETE CASCADE,
    PRIMARY KEY (signal_id, rule_id)
);
CREATE INDEX idx_x_rule_matches_rule_id ON x_rule_matches(rule_id);

-- ── Actions and pipelines ─────────────────────────────────────────────────

CREATE TABLE actions (
    id         INTEGER PRIMARY KEY,
    name       TEXT    NOT NULL UNIQUE,
    type       TEXT    NOT NULL,
    config     TEXT    NOT NULL DEFAULT '{}',
    active     INTEGER NOT NULL DEFAULT 1,
    created_at TEXT,
    updated_at TEXT
);

CREATE TABLE action_labels (
    action_id INTEGER NOT NULL REFERENCES actions(id) ON DELETE CASCADE,
    label     TEXT    NOT NULL,
    PRIMARY KEY (action_id, label)
);

CREATE TABLE action_executions (
    id          INTEGER PRIMARY KEY AUTOINCREMENT,
    action_id   INTEGER NOT NULL REFERENCES actions(id) ON DELETE CASCADE,
    signal_id   INTEGER NOT NULL REFERENCES signals(id) ON DELETE CASCADE,
    status      TEXT    NOT NULL,
    error       TEXT,
    executed_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);
CREATE INDEX idx_action_executions_action_id   ON action_executions(action_id);
CREATE INDEX idx_action_executions_executed_at ON action_executions(executed_at);
CREATE INDEX idx_action_executions_status      ON action_executions(status);
CREATE INDEX idx_action_executions_signal_id   ON action_executions(signal_id);

CREATE TABLE action_execution_metadata (
    execution_id INTEGER NOT NULL REFERENCES action_executions(id) ON DELETE CASCADE,
    key          TEXT    NOT NULL,
    value        TEXT    NOT NULL,
    PRIMARY KEY (execution_id, key)
);

CREATE TABLE pipelines (
    id          INTEGER PRIMARY KEY AUTOINCREMENT,
    name        TEXT    NOT NULL,
    description TEXT,
    active      INTEGER NOT NULL DEFAULT 0,
    created_at  TEXT    NOT NULL,
    updated_at  TEXT    NOT NULL
);

CREATE TABLE pipeline_nodes (
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

CREATE TABLE pipeline_edges (
    id             INTEGER PRIMARY KEY,
    pipeline_id    INTEGER NOT NULL REFERENCES pipelines(id) ON DELETE CASCADE,
    source_node_id INTEGER NOT NULL REFERENCES pipeline_nodes(id) ON DELETE CASCADE,
    source_handle  TEXT    NOT NULL DEFAULT 'default',
    target_node_id INTEGER NOT NULL REFERENCES pipeline_nodes(id) ON DELETE CASCADE,
    created_at     TEXT    NOT NULL
);

CREATE TABLE pipeline_node_executions (
    id             INTEGER PRIMARY KEY,
    parent_node_id INTEGER NOT NULL REFERENCES pipeline_nodes(id) ON DELETE CASCADE,
    signal_id      INTEGER NOT NULL REFERENCES signals(id) ON DELETE CASCADE,
    leaf_node_id   INTEGER NOT NULL REFERENCES pipeline_nodes(id) ON DELETE CASCADE,
    completed      INTEGER NOT NULL DEFAULT 0,
    created_at     TEXT
);
CREATE INDEX idx_pne_lookup ON pipeline_node_executions (parent_node_id, signal_id);

CREATE TABLE pipeline_run_tracking (
    pipeline_id   INTEGER NOT NULL,
    signal_id     INTEGER NOT NULL REFERENCES signals(id) ON DELETE CASCADE,
    active_tasks  INTEGER NOT NULL DEFAULT 0,
    event_payload BLOB,
    created_at    DATETIME NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%SZ','now')),
    completed_at  DATETIME,
    PRIMARY KEY (pipeline_id, signal_id)
);

CREATE TABLE trigger_pipeline_links (
    id                 INTEGER PRIMARY KEY,
    trigger_type       TEXT    NOT NULL,
    pipeline_id        INTEGER NOT NULL REFERENCES pipelines(id) ON DELETE CASCADE,
    source_pipeline_id INTEGER REFERENCES pipelines(id) ON DELETE CASCADE,
    active             INTEGER NOT NULL DEFAULT 0,
    created_at         TEXT    NOT NULL
);

-- ── Ollama ────────────────────────────────────────────────────────────────

CREATE TABLE ollama_backends (
    id             INTEGER PRIMARY KEY AUTOINCREMENT,
    name           TEXT    NOT NULL,
    url            TEXT    NOT NULL UNIQUE,
    enabled        INTEGER NOT NULL DEFAULT 1,
    max_concurrent INTEGER NOT NULL DEFAULT 1,
    created_at     DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE action_backends (
    action_id  INTEGER NOT NULL REFERENCES actions(id)         ON DELETE CASCADE,
    backend_id INTEGER NOT NULL REFERENCES ollama_backends(id) ON DELETE CASCADE,
    priority   INTEGER NOT NULL,
    PRIMARY KEY (action_id, backend_id)
);

CREATE TABLE digest_jobs (
    id           TEXT    PRIMARY KEY,
    status       TEXT    NOT NULL DEFAULT 'pending',
    params       TEXT    NOT NULL DEFAULT '{}',
    result       TEXT,
    error        TEXT,
    signal_count INTEGER NOT NULL DEFAULT 0,
    created_at   DATETIME NOT NULL DEFAULT (datetime('now')),
    completed_at DATETIME
);

-- ── Notifications ─────────────────────────────────────────────────────────

CREATE TABLE notification_providers (
    id         INTEGER  PRIMARY KEY AUTOINCREMENT,
    name       TEXT     NOT NULL,
    type       TEXT     NOT NULL, -- 'pushover' | 'telegram' | 'discord'
    config     TEXT     NOT NULL DEFAULT '{}',
    active     INTEGER  NOT NULL DEFAULT 1,
    created_at DATETIME NOT NULL DEFAULT (datetime('now')),
    updated_at DATETIME NOT NULL DEFAULT (datetime('now'))
);
CREATE INDEX idx_notification_providers_active ON notification_providers(active);

CREATE TABLE notification_channels (
    id          INTEGER  PRIMARY KEY AUTOINCREMENT,
    name        TEXT     NOT NULL,
    provider_id INTEGER  NOT NULL REFERENCES notification_providers(id) ON DELETE CASCADE,
    config      TEXT     NOT NULL DEFAULT '{}',
    active      INTEGER  NOT NULL DEFAULT 1,
    created_at  DATETIME NOT NULL DEFAULT (datetime('now')),
    updated_at  DATETIME NOT NULL DEFAULT (datetime('now'))
);
CREATE INDEX idx_notification_channels_active   ON notification_channels(active);
CREATE INDEX idx_notification_channels_provider ON notification_channels(provider_id);

-- ── Tracker ───────────────────────────────────────────────────────────────

CREATE TABLE tracked_entities (
    id         INTEGER PRIMARY KEY AUTOINCREMENT,
    name       TEXT NOT NULL UNIQUE,
    type       TEXT NOT NULL DEFAULT '',
    aliases    TEXT NOT NULL DEFAULT '[]',
    notes      TEXT NOT NULL DEFAULT '',
    active     INTEGER NOT NULL DEFAULT 1,
    created_at DATETIME NOT NULL DEFAULT (datetime('now')),
    updated_at DATETIME NOT NULL DEFAULT (datetime('now'))
);

CREATE TABLE entity_tracks (
    id           INTEGER PRIMARY KEY AUTOINCREMENT,
    entity_id    INTEGER NOT NULL REFERENCES tracked_entities(id) ON DELETE CASCADE,
    signal_id    INTEGER NOT NULL REFERENCES signals(id) ON DELETE CASCADE,
    match_source TEXT    NOT NULL DEFAULT 'text', -- 'text' or 'ner'
    context      TEXT    NOT NULL DEFAULT '',
    summary      TEXT    NOT NULL DEFAULT '',
    created_at   DATETIME NOT NULL DEFAULT (datetime('now')),
    UNIQUE (entity_id, signal_id)
);
CREATE INDEX idx_entity_tracks_created_at ON entity_tracks(created_at);
CREATE INDEX idx_entity_tracks_entity_id  ON entity_tracks(entity_id);
CREATE INDEX idx_entity_tracks_signal_id  ON entity_tracks(signal_id);

CREATE TABLE entity_candidates (
    id                 INTEGER PRIMARY KEY AUTOINCREMENT,
    word               TEXT    NOT NULL,
    type               TEXT    NOT NULL,
    occurrence_count   INTEGER NOT NULL DEFAULT 1,
    avg_confidence     REAL    NOT NULL DEFAULT 0,
    sources            TEXT    NOT NULL DEFAULT '[]', -- JSON array of pipeline names
    status             TEXT    NOT NULL DEFAULT 'pending',
    promoted_entity_id INTEGER REFERENCES tracked_entities(id) ON DELETE SET NULL,
    auto_promoted      INTEGER NOT NULL DEFAULT 0,
    first_seen_at      DATETIME NOT NULL,
    last_seen_at       DATETIME NOT NULL,
    UNIQUE (word, type)
);
CREATE INDEX idx_entity_candidates_status ON entity_candidates(status);
CREATE INDEX idx_entity_candidates_type   ON entity_candidates(type);

CREATE TABLE entity_connections (
    from_id    INTEGER NOT NULL REFERENCES tracked_entities(id) ON DELETE CASCADE,
    to_id      INTEGER NOT NULL REFERENCES tracked_entities(id) ON DELETE CASCADE,
    weight     INTEGER NOT NULL DEFAULT 1,
    updated_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    PRIMARY KEY (from_id, to_id),
    CHECK (from_id < to_id)
);
CREATE INDEX idx_entity_connections_from ON entity_connections(from_id);
CREATE INDEX idx_entity_connections_to   ON entity_connections(to_id);

-- ── Geocoding ─────────────────────────────────────────────────────────────

CREATE TABLE geocode_cache (
    lat       REAL    NOT NULL,
    lon       REAL    NOT NULL,
    name      TEXT    NOT NULL,
    cached_at INTEGER NOT NULL DEFAULT (strftime('%s','now')),
    PRIMARY KEY (lat, lon)
)
