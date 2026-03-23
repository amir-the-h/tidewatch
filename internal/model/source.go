package model

import "time"

type SourceType string

const (
	SourceTypeAccount SourceType = "account"
	SourceTypeKeyword SourceType = "keyword"
)

type Source struct {
	ID        int64      `json:"id"`
	Type      SourceType `json:"type"`
	Value     string     `json:"value"`
	XRuleID   string     `json:"x_rule_id"`
	Active    bool       `json:"active"`
	CreatedAt time.Time  `json:"created_at"`
	UpdatedAt time.Time  `json:"updated_at"`
}
