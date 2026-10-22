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
// title, text, text_short, url, lang, published_at and, when known, the
// dispatching source's source_id, source_name, source_type and source_tier.
// The generic keys are written after attrs so a connector can't shadow them.
// src may be nil.
func signalContext(sig *model.Signal, ev queue.SignalEvent, src *model.SourceRef) map[string]any {
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
	if src != nil {
		out["source_id"] = src.ID
		out["source_name"] = src.Name
		out["source_type"] = src.Type
		if src.Tier != nil {
			out["source_tier"] = *src.Tier
		}
	}
	return out
}

// dispatchingSource returns the source the event names, or — for events
// without one (reprocess, pipeline-run backfills) — the first source that saw
// the signal. It returns nil when neither is known.
func dispatchingSource(ctx context.Context, st store.Store, ev queue.SignalEvent) *model.SourceRef {
	if ev.SourceID != 0 {
		src, err := st.GetSource(ctx, ev.SourceID)
		if err != nil {
			return nil
		}
		return &model.SourceRef{ID: src.ID, Name: src.Name, Type: src.Type, Tier: src.Tier}
	}
	refs, err := st.ListSignalSources(ctx, ev.SignalID)
	if err != nil || len(refs) == 0 {
		return nil
	}
	return refs[0]
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
	return signalContext(sig, ev, dispatchingSource(ctx, st, ev)), ev.SignalID
}
