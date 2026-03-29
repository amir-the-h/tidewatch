package model

import "time"

// ChainNodeType is the type of a node in a chain DAG.
type ChainNodeType string

const (
	ChainNodeTypeStart     ChainNodeType = "start"
	ChainNodeTypeAction    ChainNodeType = "action"
	ChainNodeTypeCondition ChainNodeType = "condition"
)

// Chain is a named DAG workflow of action and condition nodes.
type Chain struct {
	ID          int64     `json:"id"`
	Name        string    `json:"name"`
	Description string    `json:"description,omitempty"`
	Active      bool      `json:"active"`
	NodeCount   int       `json:"node_count"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
}

// ChainNode is one node in a chain DAG.
type ChainNode struct {
	ID         int64            `json:"id"`
	ChainID    int64            `json:"chain_id"`
	Type       ChainNodeType    `json:"type"`
	ActionID   *int64           `json:"action_id,omitempty"`
	Conditions []ChainCondition `json:"conditions,omitempty"`
	Label      string           `json:"label"`
	PosX       float64          `json:"pos_x"`
	PosY       float64          `json:"pos_y"`
	CreatedAt  time.Time        `json:"created_at"`
}

// ChainEdge is a directed edge between two chain nodes.
type ChainEdge struct {
	ID           int64     `json:"id"`
	ChainID      int64     `json:"chain_id"`
	SourceNodeID int64     `json:"source_node_id"`
	SourceHandle string    `json:"source_handle"`
	TargetNodeID int64     `json:"target_node_id"`
	CreatedAt    time.Time `json:"created_at"`
}

// ChainGraph is the full DAG for a chain.
type ChainGraph struct {
	Nodes []*ChainNode `json:"nodes"`
	Edges []*ChainEdge `json:"edges"`
}

// ChainNodeRequest is a node in a PUT /api/chains/{id}/graph request.
// ClientID is a temporary client-side ID used to resolve edge references.
type ChainNodeRequest struct {
	ClientID   string           `json:"client_id"`
	Type       ChainNodeType    `json:"type"`
	ActionID   *int64           `json:"action_id,omitempty"`
	Conditions []ChainCondition `json:"conditions,omitempty"`
	Label      string           `json:"label"`
	PosX       float64          `json:"pos_x"`
	PosY       float64          `json:"pos_y"`
}

// ChainEdgeRequest is an edge in a PUT /api/chains/{id}/graph request.
type ChainEdgeRequest struct {
	SourceClientID string `json:"source_client_id"`
	SourceHandle   string `json:"source_handle"`
	TargetClientID string `json:"target_client_id"`
}

// ChainGraphRequest is the body for PUT /api/chains/{id}/graph.
type ChainGraphRequest struct {
	Nodes []*ChainNodeRequest `json:"nodes"`
	Edges []*ChainEdgeRequest `json:"edges"`
}

// TriggerChainLink connects a system trigger type to a chain.
type TriggerChainLink struct {
	ID          int64     `json:"id"`
	TriggerType string    `json:"trigger_type"`
	ChainID     int64     `json:"chain_id"`
	Active      bool      `json:"active"`
	CreatedAt   time.Time `json:"created_at"`
}

// ChainCondition is a single predicate evaluated against a context value.
// Moved here from action_chain.go.
type ChainCondition struct {
	Key      string `json:"key"`
	Operator string `json:"op"`
	Value    string `json:"value,omitempty"`
}
