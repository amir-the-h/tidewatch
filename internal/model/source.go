package model

import (
	"encoding/json"
	"time"
)

// Source is a configured connector instance: which feed, stream or query a
// connector type pulls from, plus metadata flow conditions can use.
type Source struct {
	ID   int64  `json:"id"`
	Name string `json:"name"`
	Type string `json:"type"` // connector type: x_stream, x_search, rss, …
	// Config is the connector config. Secret fields are masked by the API.
	Config json.RawMessage `json:"config"`
	// PollIntervalS overrides the connector's default poll interval (poll
	// connectors only); nil means the default.
	PollIntervalS   *int      `json:"poll_interval_s"`
	Tier            *int      `json:"tier"` // 1 wire/official … 4 aggregator/blog; nil = unrated
	StateAffiliated bool      `json:"state_affiliated"`
	Region          string    `json:"region"`
	Language        string    `json:"language"`
	Active          bool      `json:"active"`
	CreatedAt       time.Time `json:"created_at"`
	UpdatedAt       time.Time `json:"updated_at"`
}

// SourceRef is the short form of a source attached to a signal on read.
type SourceRef struct {
	ID     int64     `json:"id"`
	Name   string    `json:"name"`
	Type   string    `json:"type"`
	Tier   *int      `json:"tier,omitempty"`
	SeenAt time.Time `json:"seen_at"`
}

// SourceState is a source's persisted cursor and last-known health.
type SourceState struct {
	SourceID      int64           `json:"source_id"`
	Cursor        json.RawMessage `json:"cursor"`
	LastRunAt     *time.Time      `json:"last_run_at"`
	LastSuccessAt *time.Time      `json:"last_success_at"`
	LastError     string          `json:"last_error"`
	UpdatedAt     time.Time       `json:"updated_at"`
}

// SourceRunKind is what a source run does.
type SourceRunKind string

const (
	SourceRunFetch    SourceRunKind = "fetch"    // one poll, now
	SourceRunBackfill SourceRunKind = "backfill" // historical fetch
)

// SourceRunStatus is the lifecycle state of a source run.
type SourceRunStatus string

const (
	SourceRunPending SourceRunStatus = "pending"
	SourceRunRunning SourceRunStatus = "running"
	SourceRunDone    SourceRunStatus = "done"
	SourceRunError   SourceRunStatus = "error"
)

// SourceRun is a manual fetch or backfill of one source.
type SourceRun struct {
	ID           string          `json:"id"`
	SourceID     int64           `json:"source_id"`
	Kind         SourceRunKind   `json:"kind"`
	Params       json.RawMessage `json:"params"`
	Status       SourceRunStatus `json:"status"`
	FetchedCount int             `json:"fetched_count"`
	NewCount     int             `json:"new_count"`
	Error        string          `json:"error,omitempty"`
	CreatedAt    time.Time       `json:"created_at"`
	CompletedAt  *time.Time      `json:"completed_at,omitempty"`
}
