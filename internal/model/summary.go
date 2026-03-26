package model

import "time"

// SummaryItem pairs a tweet with its summarizer output from the categorizations table.
type SummaryItem struct {
	TweetID      string    `json:"tweet_id"`
	AuthorHandle string    `json:"author_handle"`
	TweetText    string    `json:"tweet_text"`
	CreatedAt    time.Time `json:"created_at"`
	Summary      string    `json:"summary"`
	ModelName    string    `json:"model_name"`
	SummarizedAt time.Time `json:"summarized_at"`
}
