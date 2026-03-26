package model

import (
	"encoding/json"
	"time"
)

// Notification defines a push notification configuration.
type Notification struct {
	ID                int64            `json:"id"`
	Name              string           `json:"name"`
	EventType         string           `json:"event_type"`
	Active            bool             `json:"active"`
	// Provider selects the delivery backend: "pushover" (default) or "telegram".
	Provider          string           `json:"provider"`
	FilterRuleID      *int64           `json:"filter_rule_id,omitempty"`
	FilterModelName   *string          `json:"filter_model_name,omitempty"`
	FilterModelResult *json.RawMessage `json:"filter_model_result,omitempty"`
	// Pushover-specific fields.
	PushoverPriority  int              `json:"pushover_priority"`
	PushoverSound     *string          `json:"pushover_sound,omitempty"`
	PushoverDevice    *string          `json:"pushover_device,omitempty"`
	PushoverTitle     *string          `json:"pushover_title,omitempty"`
	PushoverURL       *string          `json:"pushover_url,omitempty"`
	PushoverURLTitle  *string          `json:"pushover_url_title,omitempty"`
	PushoverTTL       *int             `json:"pushover_ttl,omitempty"`
	PushoverHTML      bool             `json:"pushover_html"`
	PushoverRetry     *int             `json:"pushover_retry,omitempty"`
	PushoverExpire    *int             `json:"pushover_expire,omitempty"`
	// Telegram-specific fields.
	TelegramChatID                *string `json:"telegram_chat_id,omitempty"`
	TelegramParseMode             *string `json:"telegram_parse_mode,omitempty"`
	TelegramDisableWebPagePreview bool    `json:"telegram_disable_web_page_preview"`
	TelegramDisableNotification   bool    `json:"telegram_disable_notification"`
	TelegramMessageThreadID       *int64  `json:"telegram_message_thread_id,omitempty"`
	MessageTemplate   *string          `json:"message_template,omitempty"`
	CreatedAt         time.Time        `json:"created_at"`
	UpdatedAt         time.Time        `json:"updated_at"`
}
