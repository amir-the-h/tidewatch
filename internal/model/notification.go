package model

import (
	"encoding/json"
	"time"
)

// NotificationProvider stores credentials for a notification delivery backend.
type NotificationProvider struct {
	ID        int64           `json:"id"`
	Name      string          `json:"name"`
	Type      string          `json:"type"` // "pushover" | "telegram"
	Config    json.RawMessage `json:"config"`
	Active    bool            `json:"active"`
	CreatedAt time.Time       `json:"created_at"`
	UpdatedAt time.Time       `json:"updated_at"`
}

// NotificationChannel stores delivery configuration and a message template
// for a specific notification_provider. Multiple channels may share one provider
// (e.g. two Telegram channels to different chat IDs on the same bot).
//
// MessageTemplate supports ${variable} substitution at execution time.
// Available variables: ${tweet}, ${author}, ${tweet_id}, ${tweet_url},
// plus any key from the action execution's mergedCtx.
type NotificationChannel struct {
	ID              int64           `json:"id"`
	Name            string          `json:"name"`
	ProviderID      int64           `json:"provider_id"`
	MessageTemplate string          `json:"message_template"`
	Config          json.RawMessage `json:"config"`
	Active          bool            `json:"active"`
	CreatedAt       time.Time       `json:"created_at"`
	UpdatedAt       time.Time       `json:"updated_at"`
}
