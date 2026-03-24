package model

import "time"

// TrackedEntity is a named entity the system watches for in incoming tweets.
// It can be added manually or promoted from NER results.
type TrackedEntity struct {
	ID         int64     `json:"id"`
	Name       string    `json:"name"`
	Type       string    `json:"type"`    // PER, LOC, ORG, MISC, or custom
	Aliases    []string  `json:"aliases"` // alternative spellings / transliterations
	Notes      string    `json:"notes"`
	Active     bool      `json:"active"`
	CreatedAt  time.Time `json:"created_at"`
	UpdatedAt  time.Time `json:"updated_at"`
	TrackCount int64     `json:"track_count,omitempty"`
}

// EntityTrack records a single match of a TrackedEntity against a tweet.
type EntityTrack struct {
	ID          int64     `json:"id"`
	EntityID    int64     `json:"entity_id"`
	EntityName  string    `json:"entity_name,omitempty"`
	TweetID     string    `json:"tweet_id"`
	MatchSource string    `json:"match_source"` // "text" or "ner"
	Context     string    `json:"context"`
	CreatedAt   time.Time `json:"created_at"`
}

// NEREntity represents a single named entity extracted by a NER pipeline.
type NEREntity struct {
	Word  string  `json:"word"`
	Label string  `json:"label"`
	Score float32 `json:"score"`
}
