package worker

import (
	"context"
	"encoding/json"
	"time"

	"github.com/amir-the-h/tidewatch/internal/model"
	"github.com/amir-the-h/tidewatch/internal/queue"
	"github.com/amir-the-h/tidewatch/internal/store"
)

// textShortLen is the rune length of the text_short context key.
const textShortLen = 280

// signalContext returns the context keys every task gets for its signal:
// every attrs key (e.g. x_tweet_id), then signal_id, signal_kind, author,
// title, text, text_short, url, lang, published_at and, when the event
// carries it, source_type. The generic keys are written after attrs so a
// connector can't shadow them.
func signalContext(sig *model.Signal, ev queue.SignalEvent) map[string]any {
	out := make(map[string]any, len(sig.Attrs)+11)
	for k, v := range sig.Attrs {
		out[k] = v
	}
	textShort := sig.Text
	if textShort == "" {
		textShort = sig.Title
	}
	out["signal_id"] = sig.ID
	out["signal_kind"] = string(sig.Kind)
	out["author"] = sig.Author
	out["title"] = sig.Title
	out["text"] = sig.Text
	out["text_short"] = truncateText(textShort, textShortLen)
	out["url"] = sig.URL
	out["lang"] = sig.Lang
	out["published_at"] = sig.PublishedAt.UTC().Format(time.RFC3339)
	if ev.SourceType != "" {
		out["source_type"] = ev.SourceType
	}
	if ev.SourceID != 0 {
		out["source_id"] = ev.SourceID
	}
	return out
}

// signalInputText is the PipelineInput.Text for a signal: the text for posts,
// title + "\n\n" + text for articles (or whichever of the two is set).
func signalInputText(sig *model.Signal) string {
	if sig.Kind == model.SignalKindArticle && sig.Title != "" {
		if sig.Text == "" {
			return sig.Title
		}
		return sig.Title + "\n\n" + sig.Text
	}
	return sig.Text
}

// loadSignalContext decodes the SignalEvent from eventPayload, loads the signal
// and returns its context keys. It returns an empty map when the payload has no
// signal (e.g. a digest event) or the signal can't be loaded.
func loadSignalContext(ctx context.Context, st store.Store, eventPayload []byte) (map[string]any, int64) {
	var ev queue.SignalEvent
	if err := json.Unmarshal(eventPayload, &ev); err != nil || ev.SignalID == 0 {
		return map[string]any{}, 0
	}
	sig, err := st.GetSignal(ctx, ev.SignalID)
	if err != nil {
		return map[string]any{"signal_id": ev.SignalID}, ev.SignalID
	}
	return signalContext(sig, ev), ev.SignalID
}
