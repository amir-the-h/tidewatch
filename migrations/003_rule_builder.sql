ALTER TABLE sources ADD COLUMN name TEXT;
ALTER TABLE sources ADD COLUMN class TEXT;
ALTER TABLE sources ADD COLUMN operator TEXT;

UPDATE sources SET
    name     = value,
    class    = 'atom',
    operator = CASE type WHEN 'account' THEN 'from:' ELSE '' END;

CREATE TABLE rules (
    id             INTEGER PRIMARY KEY AUTOINCREMENT,
    name           TEXT NOT NULL,
    expression     TEXT NOT NULL,
    compiled_value TEXT NOT NULL DEFAULT '',
    x_rule_id      TEXT NOT NULL DEFAULT '',
    active         INTEGER NOT NULL DEFAULT 0,
    created_at     DATETIME NOT NULL DEFAULT (datetime('now')),
    updated_at     DATETIME NOT NULL DEFAULT (datetime('now'))
);

DROP TABLE IF EXISTS tweet_sources;

CREATE TABLE tweet_rules (
    tweet_id TEXT    NOT NULL REFERENCES tweets(id),
    rule_id  INTEGER NOT NULL REFERENCES rules(id),
    PRIMARY KEY (tweet_id, rule_id)
);
CREATE INDEX IF NOT EXISTS idx_tweet_rules_tweet_id ON tweet_rules(tweet_id);
CREATE INDEX IF NOT EXISTS idx_tweet_rules_rule_id  ON tweet_rules(rule_id);

CREATE TABLE sources_new (
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
INSERT INTO sources_new (id, name, type, class, operator, value, active, created_at, updated_at)
    SELECT id, COALESCE(name,''), type, COALESCE(class,'atom'), COALESCE(operator,''), value, active, created_at, updated_at
    FROM sources;
DROP TABLE sources;
ALTER TABLE sources_new RENAME TO sources;
