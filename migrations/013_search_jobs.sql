CREATE TABLE IF NOT EXISTS search_jobs (
    id             TEXT    PRIMARY KEY,
    status         TEXT    NOT NULL DEFAULT 'pending',
    params         TEXT    NOT NULL DEFAULT '{}',
    tweets_fetched INTEGER NOT NULL DEFAULT 0,
    error          TEXT,
    created_at     DATETIME NOT NULL DEFAULT (datetime('now')),
    completed_at   DATETIME
);
