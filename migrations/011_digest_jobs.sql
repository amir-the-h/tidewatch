CREATE TABLE IF NOT EXISTS digest_jobs (
    id           TEXT    PRIMARY KEY,
    status       TEXT    NOT NULL DEFAULT 'pending',
    params       TEXT    NOT NULL DEFAULT '{}',
    result       TEXT,
    error        TEXT,
    tweet_count  INTEGER NOT NULL DEFAULT 0,
    created_at   DATETIME NOT NULL DEFAULT (datetime('now')),
    completed_at DATETIME
);
