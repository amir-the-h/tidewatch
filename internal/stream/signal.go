package stream

// Temporary X ingest shim (phase 1 steps 1–2): the X stream and search build a
// model.Signal here and hand it to the generic source ingest. Phase 1 step 3
// replaces it with the x_stream / x_search connectors.

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/amir-the-h/tidewatch/internal/hub"
	"github.com/amir-the-h/tidewatch/internal/model"
	"github.com/amir-the-h/tidewatch/internal/notifier"
	"github.com/amir-the-h/tidewatch/internal/queue"
	"github.com/amir-the-h/tidewatch/internal/source"
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

// Ingest persists sig and its X rule matches through the shared source ingest
// path (source.Ingester) without a source row: until phase 1 step 3 the X
// stream and search have no sources, so no signal_sources row is written and
// the dispatched event carries only sourceType. Reports whether the signal
// was new.
func Ingest(ctx context.Context, d IngestDeps, sig *model.Signal, ruleIDs []int64, sourceType string) (bool, error) {
	in := source.Ingester{
		Store:       d.Store,
		Producer:    d.Producer,
		Cache:       d.Cache,
		Broadcaster: d.Broadcaster,
		Emitter:     d.Emitter,
		Logger:      d.Logger,
	}
	return in.Ingest(ctx, nil, sourceType, sig, source.WithXRuleMatches(ruleIDs))
}
