package model

import "time"

// SearchJobStatus represents the lifecycle state of a search job.
type SearchJobStatus string

const (
	SearchJobStatusPending SearchJobStatus = "pending"
	SearchJobStatusRunning SearchJobStatus = "running"
	SearchJobStatusDone    SearchJobStatus = "done"
	SearchJobStatusError   SearchJobStatus = "error"
)

// SearchJob represents a background search task.
type SearchJob struct {
	ID            string          `json:"id"`
	Status        SearchJobStatus `json:"status"`
	Params        SearchJobParams `json:"params"`
	TweetsFetched int             `json:"tweets_fetched"`
	Error         string          `json:"error,omitempty"`
	CreatedAt     time.Time       `json:"created_at"`
	CompletedAt   *time.Time      `json:"completed_at,omitempty"`
}

// SearchJobParams holds the input parameters for a search job.
type SearchJobParams struct {
	RuleIDs    []int64 `json:"rule_ids,omitempty"`
	StartTime  string  `json:"start_time,omitempty"`
	EndTime    string  `json:"end_time,omitempty"`
	SinceID    string  `json:"since_id,omitempty"`
	UntilID    string  `json:"until_id,omitempty"`
	MaxResults int     `json:"max_results,omitempty"`
	Limit      int     `json:"limit,omitempty"`
}
