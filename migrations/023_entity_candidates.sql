CREATE TABLE IF NOT EXISTS entity_candidates (
    id                  INTEGER PRIMARY KEY AUTOINCREMENT,
    word                TEXT NOT NULL,
    type                TEXT NOT NULL,
    occurrence_count    INTEGER NOT NULL DEFAULT 1,
    avg_confidence      REAL NOT NULL DEFAULT 0,
    sources             TEXT NOT NULL DEFAULT '[]',
    status              TEXT NOT NULL DEFAULT 'pending',
    promoted_entity_id  INTEGER REFERENCES tracked_entities(id) ON DELETE SET NULL,
    auto_promoted       INTEGER NOT NULL DEFAULT 0,
    first_seen_at       DATETIME NOT NULL,
    last_seen_at        DATETIME NOT NULL,
    UNIQUE(word, type)
);

CREATE INDEX IF NOT EXISTS idx_entity_candidates_status ON entity_candidates(status);
CREATE INDEX IF NOT EXISTS idx_entity_candidates_type   ON entity_candidates(type);
