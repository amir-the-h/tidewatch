CREATE TABLE IF NOT EXISTS sources (
    id         INTEGER PRIMARY KEY AUTOINCREMENT,
    type       TEXT NOT NULL CHECK(type IN ('account','keyword')),
    value      TEXT NOT NULL,
    x_rule_id  TEXT NOT NULL DEFAULT '',
    active     INTEGER NOT NULL DEFAULT 1,
    created_at DATETIME NOT NULL DEFAULT (datetime('now')),
    updated_at DATETIME NOT NULL DEFAULT (datetime('now'))
);

CREATE TABLE IF NOT EXISTS tweets (
    id            TEXT PRIMARY KEY,
    author_id     TEXT NOT NULL,
    author_handle TEXT NOT NULL,
    text          TEXT NOT NULL,
    raw_json      BLOB NOT NULL,
    created_at    DATETIME NOT NULL,
    fetched_at    DATETIME NOT NULL DEFAULT (datetime('now'))
);
CREATE INDEX IF NOT EXISTS idx_tweets_created_at ON tweets(created_at);
CREATE INDEX IF NOT EXISTS idx_tweets_author_id ON tweets(author_id);

CREATE TABLE IF NOT EXISTS tweet_sources (
    tweet_id  TEXT NOT NULL REFERENCES tweets(id),
    source_id INTEGER NOT NULL REFERENCES sources(id),
    PRIMARY KEY (tweet_id, source_id)
);
CREATE INDEX IF NOT EXISTS idx_tweet_sources_tweet_id ON tweet_sources(tweet_id);
CREATE INDEX IF NOT EXISTS idx_tweet_sources_source_id ON tweet_sources(source_id);

CREATE TABLE IF NOT EXISTS categorizations (
    id            INTEGER PRIMARY KEY AUTOINCREMENT,
    tweet_id      TEXT NOT NULL REFERENCES tweets(id),
    model_name    TEXT NOT NULL,
    model_version TEXT NOT NULL,
    results       TEXT NOT NULL,
    created_at    DATETIME NOT NULL DEFAULT (datetime('now'))
);
CREATE INDEX IF NOT EXISTS idx_categorizations_tweet_id ON categorizations(tweet_id);
CREATE INDEX IF NOT EXISTS idx_categorizations_model_name ON categorizations(model_name);
CREATE INDEX IF NOT EXISTS idx_categorizations_created_at ON categorizations(created_at);

CREATE TABLE IF NOT EXISTS models (
    id         INTEGER PRIMARY KEY AUTOINCREMENT,
    name       TEXT NOT NULL,
    version    TEXT NOT NULL,
    path       TEXT NOT NULL,
    type       TEXT NOT NULL,
    active     INTEGER NOT NULL DEFAULT 0,
    created_at DATETIME NOT NULL DEFAULT (datetime('now'))
);
