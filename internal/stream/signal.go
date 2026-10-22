package stream

// Temporary X ingest shim (phase 1 step 1): the X stream and search build a
// model.Signal here and share one ingest path. Phase 1 step 3 replaces it with
// the x_stream / x_search connectors and the generic source ingest.

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/amir-the-h/tidewatch/internal/hub"
	"github.com/amir-the-h/tidewatch/internal/model"
	"github.com/amir-the-h/tidewatch/internal/notifier"
	"github.com/amir-the-h/tidewatch/internal/queue"
	"github.com/amir-the-h/tidewatch/internal/store"
	"github.com/amir-the-h/tidewatch/internal/worker"
)

// XExtNS is the dedup namespace shared by every X ingest path, so a tweet seen
// by both the stream and search is stored once.
const XExtNS = "x"

// Source types reported in SignalEvent.SourceType until generic sources exist.
const (
	SourceTypeXStream = "x_stream"
	SourceTypeXSearch = "x_search"
)

// MatchingRule is an X API matching rule reference on a stream event.
type MatchingRule struct {
	ID  string `json:"id"`
	Tag string `json:"tag"`
}

// XPost holds the fields of a normalized X post.
type XPost struct {
	ID         string
	AuthorID   string
	Handle     string
	Text       string
	CreatedAt  time.Time
	RawJSON    []byte
	Referenced map[string]string // referenced tweet type (retweeted, quoted, replied_to) → tweet ID
}

// NewXSignal builds the post signal for an X tweet. x_rule_ids is added to
// its attrs by Ingest once the matching rules are resolved.
func NewXSignal(p XPost) *model.Signal {
	attrs := map[string]any{
		"x_tweet_id":  p.ID,
		"x_author_id": p.AuthorID,
	}
	if len(p.Referenced) > 0 {
		attrs["x_referenced"] = p.Referenced
	}
	return &model.Signal{
		Kind:        model.SignalKindPost,
		ExtNS:       XExtNS,
		ExternalID:  p.ID,
		Author:      p.Handle,
		AuthorID:    p.AuthorID,
		Text:        p.Text,
		URL:         fmt.Sprintf("https://x.com/%s/status/%s", p.Handle, p.ID),
		Attrs:       attrs,
		RawJSON:     p.RawJSON,
		PublishedAt: p.CreatedAt,
		FetchedAt:   time.Now(),
	}
}

// IngestDeps are the collaborators Ingest needs. Broadcaster, Emitter and
// Cache may be nil.
type IngestDeps struct {
	Store       store.Store
	Producer    queue.Producer
	Cache       *worker.ActionCache
	Broadcaster hub.Broadcaster
	Emitter     notifier.EventEmitter
	Logger      *slog.Logger
}

// Ingest persists sig and its X rule matches. When the signal is new it
// broadcasts a new_signal WebSocket event and dispatches new_signal pipeline
// tasks; a signal already stored (e.g. search finding a tweet the stream
// delivered) only gains the rule matches. Reports whether the signal was new.
func Ingest(ctx context.Context, d IngestDeps, sig *model.Signal, ruleIDs []int64, sourceType string) (bool, error) {
	if sig.Attrs == nil {
		sig.Attrs = map[string]any{}
	}
	if ruleIDs == nil {
		ruleIDs = []int64{}
	}
	sig.Attrs["x_rule_ids"] = ruleIDs

	isNew, err := d.Store.SaveSignal(ctx, sig)
	if err != nil {
		return false, fmt.Errorf("stream: save signal %s: %w", sig.ExternalID, err)
	}

	for _, ruleID := range ruleIDs {
		if err := d.Store.SaveXRuleMatch(ctx, &model.XRuleMatch{SignalID: sig.ID, RuleID: ruleID}); err != nil && d.Logger != nil {
			d.Logger.Warn("stream: save x rule match failed",
				"signal_id", sig.ID, "rule_id", ruleID, "error", err)
		}
	}

	if !isNew {
		return false, nil
	}

	if d.Emitter != nil {
		d.Emitter.Emit(notifier.NotificationEvent{
			Type:      notifier.EventSignalIngested,
			Signal:    sig,
			RuleIDs:   ruleIDs,
			Timestamp: time.Now(),
		})
	}
	if d.Broadcaster != nil {
		d.Broadcaster.Broadcast(hub.Event{Type: "new_signal", Payload: sig})
	}

	ev := queue.SignalEvent{SignalID: sig.ID, Kind: string(sig.Kind), SourceType: sourceType}
	if err := worker.PushInitialPipelineTasks(ctx, d.Store, d.Producer, d.Cache, "new_signal", ev, d.Logger, 0); err != nil && d.Logger != nil {
		d.Logger.Warn("stream: pipeline dispatch failed", "signal_id", sig.ID, "error", err)
	}
	return true, nil
}
