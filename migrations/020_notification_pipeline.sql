-- 020_notification_pipeline.sql
-- Replaces the notifications table with notification_providers (credentials)
-- and notification_channels (delivery config + template).

CREATE TABLE IF NOT EXISTS notification_providers (
    id         INTEGER  PRIMARY KEY AUTOINCREMENT,
    name       TEXT     NOT NULL,
    type       TEXT     NOT NULL, -- 'pushover' | 'telegram'
    config     TEXT     NOT NULL DEFAULT '{}',
    active     INTEGER  NOT NULL DEFAULT 1,
    created_at DATETIME NOT NULL DEFAULT (datetime('now')),
    updated_at DATETIME NOT NULL DEFAULT (datetime('now'))
);
CREATE INDEX IF NOT EXISTS idx_notification_providers_active ON notification_providers(active);

CREATE TABLE IF NOT EXISTS notification_channels (
    id               INTEGER  PRIMARY KEY AUTOINCREMENT,
    name             TEXT     NOT NULL,
    provider_id      INTEGER  NOT NULL REFERENCES notification_providers(id) ON DELETE CASCADE,
    message_template TEXT     NOT NULL DEFAULT '',
    config           TEXT     NOT NULL DEFAULT '{}',
    active           INTEGER  NOT NULL DEFAULT 1,
    created_at       DATETIME NOT NULL DEFAULT (datetime('now')),
    updated_at       DATETIME NOT NULL DEFAULT (datetime('now'))
);
CREATE INDEX IF NOT EXISTS idx_notification_channels_provider ON notification_channels(provider_id);
CREATE INDEX IF NOT EXISTS idx_notification_channels_active   ON notification_channels(active);

-- Migrate existing notifications rows.
-- Note: credentials (API tokens) were stored in YAML config, not DB rows,
-- so migrated providers have empty config '{}' — users must update them.
INSERT OR IGNORE INTO notification_providers (id, name, type, config, active, created_at, updated_at)
SELECT
    id,
    name,
    CASE WHEN provider = 'telegram' THEN 'telegram' ELSE 'pushover' END,
    '{}',
    active,
    created_at,
    updated_at
FROM notifications;

INSERT OR IGNORE INTO notification_channels (name, provider_id, message_template, config, active, created_at, updated_at)
SELECT
    n.name,
    n.id,
    COALESCE(n.message_template, ''),
    CASE
        WHEN n.provider = 'telegram' THEN json_object(
            'chat_id',                    COALESCE(n.telegram_chat_id, ''),
            'parse_mode',                 n.telegram_parse_mode,
            'disable_web_page_preview',   n.telegram_disable_web_page_preview,
            'disable_notification',       n.telegram_disable_notification,
            'message_thread_id',          n.telegram_message_thread_id
        )
        ELSE json_object(
            'priority',  n.pushover_priority,
            'sound',     n.pushover_sound,
            'device',    n.pushover_device,
            'title',     n.pushover_title,
            'url',       n.pushover_url,
            'url_title', n.pushover_url_title,
            'ttl',       n.pushover_ttl,
            'html',      n.pushover_html,
            'retry',     n.pushover_retry,
            'expire',    n.pushover_expire
        )
    END,
    n.active,
    n.created_at,
    n.updated_at
FROM notifications n;

DROP TABLE IF EXISTS notifications;
