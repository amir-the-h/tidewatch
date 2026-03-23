package model

import (
	"encoding/json"
	"time"
)

// ExprNode is one node in a rule expression tree.
// Exactly one field is set per node.
type ExprNode struct {
	Src *int64     `json:"src,omitempty"` // source reference by ID
	Op  string     `json:"op,omitempty"`  // inline X operator literal (e.g. "is:retweet")
	AND []ExprNode `json:"AND,omitempty"`
	OR  []ExprNode `json:"OR,omitempty"`
	NOT *ExprNode  `json:"NOT,omitempty"`
}

type Rule struct {
	ID            int64           `json:"id"`
	Name          string          `json:"name"`
	Expression    json.RawMessage `json:"expression"`
	CompiledValue string          `json:"compiled_value"`
	XRuleID       string          `json:"x_rule_id"`
	Active        bool            `json:"active"`
	CreatedAt     time.Time       `json:"created_at"`
	UpdatedAt     time.Time       `json:"updated_at"`
}

type TweetRule struct {
	TweetID string `json:"tweet_id"`
	RuleID  int64  `json:"rule_id"`
}
