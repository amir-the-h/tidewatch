package notifier

import "context"

// Provider delivers a rendered notification message via a specific backend.
// text is the already-rendered message body.
// config holds provider-specific delivery settings (e.g. chat_id, priority).
// The returned map carries any provider-assigned identifiers (e.g. telegram_message_id)
// that the pipeline should persist as action execution metadata for future runs.
type Provider interface {
	Deliver(ctx context.Context, text string, config map[string]any) (map[string]any, error)
}
