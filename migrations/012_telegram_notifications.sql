-- Add provider routing and Telegram-specific fields to notifications.
-- provider defaults to 'pushover' to keep existing rows working unchanged.

ALTER TABLE notifications ADD COLUMN provider TEXT NOT NULL DEFAULT 'pushover';

ALTER TABLE notifications ADD COLUMN telegram_chat_id TEXT NULL;
ALTER TABLE notifications ADD COLUMN telegram_parse_mode TEXT NULL;
ALTER TABLE notifications ADD COLUMN telegram_disable_web_page_preview INTEGER NOT NULL DEFAULT 0;
ALTER TABLE notifications ADD COLUMN telegram_disable_notification INTEGER NOT NULL DEFAULT 0;
ALTER TABLE notifications ADD COLUMN telegram_message_thread_id INTEGER NULL;
