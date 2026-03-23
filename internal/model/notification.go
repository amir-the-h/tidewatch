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
	FilterRuleID      *int64           `json:"filter_rule_id,omitempty"`
	FilterModelName   *string          `json:"filter_model_name,omitempty"`
	FilterModelResult *json.RawMessage `json:"filter_model_result,omitempty"`
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
	MessageTemplate   *string          `json:"message_template,omitempty"`
	CreatedAt         time.Time        `json:"created_at"`
	UpdatedAt         time.Time        `json:"updated_at"`
}
