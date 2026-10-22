// Package x holds the X (Twitter) connectors: x_stream (Filtered Stream) and
// x_search (recent/full-archive search), plus the rule compiler, validator
// and rules API client the rule builder (x_terms / x_rules) uses.
package x

import (
	"fmt"
	"time"

	"github.com/amir-the-h/tidewatch/internal/model"
)

// ExtNS is the dedup namespace shared by both X connectors, so a tweet seen
// by the stream and by search is stored once.
const ExtNS = "x"

// Connector types.
const (
	TypeStream = "x_stream"
	TypeSearch = "x_search"
)

// Tiers accepted by the tier config field. The validator allows longer rules
// on enterprise; search uses the full-archive endpoint on pro and enterprise.
var tiers = []string{"basic", "pro", "enterprise"}

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
// its attrs at ingest (source.WithXRuleMatches).
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
		ExtNS:       ExtNS,
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
