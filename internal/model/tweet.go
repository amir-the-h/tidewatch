package model

import "time"

type Tweet struct {
	ID           string    `json:"id"`
	AuthorID     string    `json:"author_id"`
	AuthorHandle string    `json:"author_handle"`
	Text         string    `json:"text"`
	RawJSON      []byte    `json:"-"`
	CreatedAt    time.Time `json:"created_at"`
	FetchedAt    time.Time `json:"fetched_at"`
	// populated on read joins, not stored in tweets table directly
	Rules []*Rule `json:"rules,omitempty"`
}

// MatchingRule represents an X API matching rule reference on incoming stream events.
type MatchingRule struct {
	ID  string `json:"id"`
	Tag string `json:"tag"`
}

// StreamTweet is the in-flight representation used by the stream listener
// before the tweet is persisted. It carries the tweet plus the matching rules
// from the X stream event so tweet_rules rows can be written.
//
// When TargetModels is non-empty, workers run only those named pipelines
// instead of the full applicable set. Used by the manual pipeline-run endpoint.
type StreamTweet struct {
	Tweet         *Tweet
	MatchingRules []MatchingRule
	TargetModels  []string `json:"TargetModels,omitempty"`
}
