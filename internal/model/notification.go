package model

import (
	"encoding/json"
	"time"
)

// NotificationProvider stores credentials for a notification delivery backend.
type NotificationProvider struct {
	ID        int64           `json:"id"`
	Name      string          `json:"name"`
	Type      string          `json:"type"` // "pushover" | "telegram" | "discord"
	Config    json.RawMessage `json:"config"`
	Active    bool            `json:"active"`
	CreatedAt time.Time       `json:"created_at"`
	UpdatedAt time.Time       `json:"updated_at"`
}

// NotificationChannel identifies a delivery endpoint for a notification_provider.
// Multiple channels may share one provider (e.g. two Telegram channels to different
// chat IDs on the same bot).
//
// Config holds only routing info: {"chat_id":"..."} for Telegram,
// {"device":"..."} (optional) for Pushover.
// Message templates and delivery options live in the notification pipeline config.
type NotificationChannel struct {
	ID         int64           `json:"id"`
	Name       string          `json:"name"`
	ProviderID int64           `json:"provider_id"`
	Config     json.RawMessage `json:"config"`
	Active     bool            `json:"active"`
	CreatedAt  time.Time       `json:"created_at"`
	UpdatedAt  time.Time       `json:"updated_at"`
}
