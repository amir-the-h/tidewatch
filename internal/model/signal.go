package model

import "time"

// SignalKind classifies a signal by shape, independent of the source it came from.
type SignalKind string

const (
	SignalKindPost    SignalKind = "post"    // short social post (X, later Telegram)
	SignalKindArticle SignalKind = "article" // news article (RSS, later GDELT DOC)
)

// Signal is one item ingested from any source: a tweet, an article, an event.
// (ExtNS, ExternalID) is unique, so the same upstream item seen twice is stored once.
type Signal struct {
	ID          int64          `json:"id"`
	Kind        SignalKind     `json:"kind"`
	ExtNS       string         `json:"ext_ns"`
	ExternalID  string         `json:"external_id"`
	Author      string         `json:"author"`
	AuthorID    string         `json:"author_id"`
	Title       string         `json:"title"`
	Text        string         `json:"text"`
	URL         string         `json:"url"`
	Lang        string         `json:"lang"`
	Lat         *float64       `json:"lat,omitempty"`
	Lon         *float64       `json:"lon,omitempty"`
	Attrs       map[string]any `json:"attrs"`
	RawJSON     []byte         `json:"-"`
	PublishedAt time.Time      `json:"published_at"`
	FetchedAt   time.Time      `json:"fetched_at"`

	// XRules lists the X stream rules the signal matched; populated on read joins only.
	XRules []*Rule `json:"x_rules,omitempty"`
}
