-- Move message_template and delivery options out of notification_channels.
-- Channels now carry only routing config (chat_id for Telegram, device for Pushover).
-- Message templates and delivery options live in the notification pipeline config.
ALTER TABLE notification_channels DROP COLUMN message_template;
