-- 015_actions.sql: actions system — generic dispatch units (pipeline, retweet, like, quote)

CREATE TABLE IF NOT EXISTS actions (
    id          INTEGER PRIMARY KEY AUTOINCREMENT,
    name        TEXT    NOT NULL,
    type        TEXT    NOT NULL,           -- pipeline | retweet | like | quote
    active      INTEGER NOT NULL DEFAULT 1,
    rule_ids    TEXT    NOT NULL DEFAULT '[]',  -- JSON array of int64 rule IDs, empty = all rules
    config      TEXT    NOT NULL DEFAULT '{}',  -- type-specific JSON config
    created_at  DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE IF NOT EXISTS action_executions (
    id          INTEGER PRIMARY KEY AUTOINCREMENT,
    action_id   INTEGER NOT NULL REFERENCES actions(id),
    tweet_id    TEXT    NOT NULL,
    status      TEXT    NOT NULL,   -- success | error
    error       TEXT,
    executed_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE INDEX IF NOT EXISTS idx_action_executions_action_id  ON action_executions(action_id);
CREATE INDEX IF NOT EXISTS idx_action_executions_tweet_id   ON action_executions(tweet_id);
CREATE INDEX IF NOT EXISTS idx_action_executions_status     ON action_executions(status);
CREATE INDEX IF NOT EXISTS idx_action_executions_executed_at ON action_executions(executed_at);

CREATE TABLE IF NOT EXISTS action_execution_metadata (
    execution_id  INTEGER NOT NULL REFERENCES action_executions(id),
    key           TEXT    NOT NULL,
    value         TEXT    NOT NULL,
    PRIMARY KEY (execution_id, key)
);

CREATE TABLE IF NOT EXISTS twitter_credentials (
    id              INTEGER PRIMARY KEY DEFAULT 1,
    access_token    TEXT    NOT NULL,
    refresh_token   TEXT    NOT NULL,
    expires_at      DATETIME NOT NULL,
    twitter_user_id TEXT    NOT NULL,
    username        TEXT    NOT NULL,
    updated_at      DATETIME NOT NULL
);
