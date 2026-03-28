package notifier

import "context"

// Provider delivers a rendered notification message via a specific backend.
// text is the already-rendered message body.
// config holds provider-specific delivery settings (e.g. chat_id, priority).
type Provider interface {
	Deliver(ctx context.Context, text string, config map[string]any) error
}
