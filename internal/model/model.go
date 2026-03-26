package model

import "time"

type ModelType string

const (
	ModelTypeClassification ModelType = "classification"
	ModelTypeNER            ModelType = "ner"
	ModelTypeSentiment      ModelType = "sentiment"
	ModelTypeSummarizer     ModelType = "summarizer"
	ModelTypeExtraction     ModelType = "extraction"
	ModelTypeDigest         ModelType = "digest"
)

type Model struct {
	ID        int64     `json:"id"`
	Name      string    `json:"name"`
	Version   string    `json:"version"`
	Path      string    `json:"path"`
	Type      ModelType `json:"type"`
	Active    bool      `json:"active"`
	RuleIDs   []int64   `json:"rule_ids,omitempty"`
	CreatedAt time.Time `json:"created_at"`
}
