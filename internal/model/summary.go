package model

import "time"

// SummaryItem pairs a signal with its summarizer output from action_execution_metadata.
type SummaryItem struct {
	SignalID     int64     `json:"signal_id"`
	Author       string    `json:"author"`
	Text         string    `json:"text"`
	PublishedAt  time.Time `json:"published_at"`
	Summary      string    `json:"summary"`
	ModelName    string    `json:"model_name"`
	SummarizedAt time.Time `json:"summarized_at"`
}
