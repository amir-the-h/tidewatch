-- migrations/022_chains_triggers.sql
PRAGMA foreign_keys = OFF;

-- Recreate actions without trigger_type and rule_ids
CREATE TABLE actions_new (
  id          INTEGER PRIMARY KEY AUTOINCREMENT,
  name        TEXT    NOT NULL,
  pipeline_id INTEGER NOT NULL REFERENCES pipelines(id),
  active      INTEGER NOT NULL DEFAULT 0,
  created_at  TEXT    NOT NULL,
  updated_at  TEXT    NOT NULL
);
INSERT INTO actions_new (id, name, pipeline_id, active, created_at, updated_at)
  SELECT id, name, pipeline_id, active, created_at, updated_at FROM actions;
DROP TABLE actions;
ALTER TABLE actions_new RENAME TO actions;

DROP TABLE IF EXISTS action_chains;

CREATE TABLE chains (
  id          INTEGER PRIMARY KEY AUTOINCREMENT,
  name        TEXT    NOT NULL,
  description TEXT,
  active      INTEGER NOT NULL DEFAULT 0,
  created_at  TEXT    NOT NULL,
  updated_at  TEXT    NOT NULL
);

CREATE TABLE chain_nodes (
  id         INTEGER PRIMARY KEY AUTOINCREMENT,
  chain_id   INTEGER NOT NULL REFERENCES chains(id) ON DELETE CASCADE,
  type       TEXT    NOT NULL,
  action_id  INTEGER REFERENCES actions(id),
  conditions TEXT,
  label      TEXT,
  pos_x      REAL    NOT NULL DEFAULT 0,
  pos_y      REAL    NOT NULL DEFAULT 0,
  created_at TEXT    NOT NULL
);

CREATE TABLE chain_edges (
  id              INTEGER PRIMARY KEY AUTOINCREMENT,
  chain_id        INTEGER NOT NULL REFERENCES chains(id) ON DELETE CASCADE,
  source_node_id  INTEGER NOT NULL REFERENCES chain_nodes(id) ON DELETE CASCADE,
  source_handle   TEXT    NOT NULL DEFAULT 'default',
  target_node_id  INTEGER NOT NULL REFERENCES chain_nodes(id) ON DELETE CASCADE,
  created_at      TEXT    NOT NULL
);

CREATE TABLE trigger_chain_links (
  id           INTEGER PRIMARY KEY AUTOINCREMENT,
  trigger_type TEXT    NOT NULL,
  chain_id     INTEGER NOT NULL REFERENCES chains(id) ON DELETE CASCADE,
  active       INTEGER NOT NULL DEFAULT 0,
  created_at   TEXT    NOT NULL
);

PRAGMA foreign_keys = ON;
