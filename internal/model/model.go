package model

import "time"

type ModelType string

const (
	ModelTypeClassification ModelType = "classification"
	ModelTypeNER            ModelType = "ner"
	ModelTypeSentiment      ModelType = "sentiment"
)

type Model struct {
	ID        int64     `json:"id"`
	Name      string    `json:"name"`
	Version   string    `json:"version"`
	Path      string    `json:"path"`
	Type      ModelType `json:"type"`
	Active    bool      `json:"active"`
	CreatedAt time.Time `json:"created_at"`
}
