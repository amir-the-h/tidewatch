package model

import "time"

// ActionChain is a conditional link from one action to another.
// After source_action completes, conditions are evaluated against the
// merged execution context. If all pass, a new Task is pushed for target_action.
type ActionChain struct {
	ID             int64            `json:"id"`
	SourceActionID int64            `json:"source_action_id"`
	TargetActionID int64            `json:"target_action_id"`
	Conditions     []ChainCondition `json:"conditions"`
	CreatedAt      time.Time        `json:"created_at"`
}

// ChainCondition is a single predicate evaluated against a string context value.
// Supported operators: ==, !=, >, >=, <, <=, contains, not_contains,
// is_empty, not_empty, is_array, is_object, array_empty, array_not_empty.
type ChainCondition struct {
	Key      string `json:"key"`
	Operator string `json:"op"`
	Value    string `json:"value,omitempty"`
}
