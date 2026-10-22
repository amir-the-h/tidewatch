package model

import (
	"encoding/json"
	"time"
)

// ExprNode is one node in a rule expression tree.
// Exactly one field is set per node.
type ExprNode struct {
	Src  *int64     `json:"src,omitempty"`  // single source reference by ID
	Srcs []int64    `json:"srcs,omitempty"` // multi-source OR shorthand: compiles to (s1 OR s2 OR ...)
	Op   string     `json:"op,omitempty"`   // inline X operator literal (e.g. "is:retweet")
	AND  []ExprNode `json:"AND,omitempty"`
	OR   []ExprNode `json:"OR,omitempty"`
	NOT  *ExprNode  `json:"NOT,omitempty"`
	Rule *int64     `json:"rule,omitempty"` // reference to another local rule by ID; inlined at compile time
}

type XRule struct {
	ID            int64           `json:"id"`
	Name          string          `json:"name"`
	Expression    json.RawMessage `json:"expression"`
	CompiledValue string          `json:"compiled_value"`
	XRuleID       string          `json:"x_rule_id"`
	Active        bool            `json:"active"`
	Local         bool            `json:"local"`
	CreatedAt     time.Time       `json:"created_at"`
	UpdatedAt     time.Time       `json:"updated_at"`
}

// XRuleMatch records that a signal matched an X stream rule.
type XRuleMatch struct {
	SignalID int64 `json:"signal_id"`
	RuleID   int64 `json:"rule_id"`
}
