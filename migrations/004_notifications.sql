CREATE TABLE IF NOT EXISTS notifications (
    id                   INTEGER PRIMARY KEY AUTOINCREMENT,
    name                 TEXT    NOT NULL,
    event_type           TEXT    NOT NULL,
    active               INTEGER NOT NULL DEFAULT 1,
    filter_rule_id       INTEGER NULL REFERENCES rules(id),
    filter_model_name    TEXT NULL,
    filter_model_result  TEXT NULL,
    pushover_priority    INTEGER NOT NULL DEFAULT 0,
    pushover_sound       TEXT NULL,
    pushover_device      TEXT NULL,
    pushover_title       TEXT NULL,
    pushover_url         TEXT NULL,
    pushover_url_title   TEXT NULL,
    pushover_ttl         INTEGER NULL,
    pushover_html        INTEGER NOT NULL DEFAULT 0,
    pushover_retry       INTEGER NULL,
    pushover_expire      INTEGER NULL,
    message_template     TEXT NULL,
    created_at           DATETIME NOT NULL DEFAULT (datetime('now')),
    updated_at           DATETIME NOT NULL DEFAULT (datetime('now'))
);

CREATE INDEX IF NOT EXISTS idx_notifications_event_type ON notifications(event_type);
CREATE INDEX IF NOT EXISTS idx_notifications_active ON notifications(active);
