CREATE TABLE IF NOT EXISTS tracked_entities (
    id         INTEGER PRIMARY KEY AUTOINCREMENT,
    name       TEXT NOT NULL UNIQUE,
    type       TEXT NOT NULL DEFAULT '',
    aliases    TEXT NOT NULL DEFAULT '[]',
    notes      TEXT NOT NULL DEFAULT '',
    active     INTEGER NOT NULL DEFAULT 1,
    created_at DATETIME NOT NULL DEFAULT (datetime('now')),
    updated_at DATETIME NOT NULL DEFAULT (datetime('now'))
);

-- entity_tracks records each time a tracked entity is matched in a tweet.
-- UNIQUE(entity_id, tweet_id) prevents duplicate tracks for the same pair.
CREATE TABLE IF NOT EXISTS entity_tracks (
    id           INTEGER PRIMARY KEY AUTOINCREMENT,
    entity_id    INTEGER NOT NULL REFERENCES tracked_entities(id) ON DELETE CASCADE,
    tweet_id     TEXT    NOT NULL REFERENCES tweets(id) ON DELETE CASCADE,
    match_source TEXT    NOT NULL DEFAULT 'text', -- 'text' or 'ner'
    context      TEXT    NOT NULL DEFAULT '',
    created_at   DATETIME NOT NULL DEFAULT (datetime('now')),
    UNIQUE(entity_id, tweet_id)
);

CREATE INDEX IF NOT EXISTS idx_entity_tracks_entity_id  ON entity_tracks(entity_id);
CREATE INDEX IF NOT EXISTS idx_entity_tracks_tweet_id   ON entity_tracks(tweet_id);
CREATE INDEX IF NOT EXISTS idx_entity_tracks_created_at ON entity_tracks(created_at);
