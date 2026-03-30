CREATE TABLE IF NOT EXISTS entity_connections (
    from_id    INTEGER NOT NULL REFERENCES tracked_entities(id) ON DELETE CASCADE,
    to_id      INTEGER NOT NULL REFERENCES tracked_entities(id) ON DELETE CASCADE,
    weight     INTEGER NOT NULL DEFAULT 1,
    updated_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    PRIMARY KEY (from_id, to_id),
    CHECK (from_id < to_id)
);

CREATE INDEX IF NOT EXISTS idx_entity_connections_from ON entity_connections(from_id);
CREATE INDEX IF NOT EXISTS idx_entity_connections_to   ON entity_connections(to_id);
