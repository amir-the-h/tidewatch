package model

import "time"

// PipelineNodeType is the type of a node in a pipeline DAG.
type PipelineNodeType string

const (
	PipelineNodeTypeStart     PipelineNodeType = "start"
	PipelineNodeTypeAction    PipelineNodeType = "action"
	PipelineNodeTypeCondition PipelineNodeType = "condition"
	PipelineNodeTypePipeline  PipelineNodeType = "pipeline"
)

// Pipeline is a named DAG workflow of action and condition nodes.
type Pipeline struct {
	ID          int64     `json:"id"`
	Name        string    `json:"name"`
	Description string    `json:"description,omitempty"`
	Active      bool      `json:"active"`
	NodeCount   int       `json:"node_count"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
}

// PipelineNode is one node in a pipeline DAG.
type PipelineNode struct {
	ID            int64               `json:"id"`
	PipelineID    int64               `json:"pipeline_id"`
	Type          PipelineNodeType    `json:"type"`
	ActionID      *int64              `json:"action_id,omitempty"`
	SubPipelineID *int64              `json:"sub_pipeline_id,omitempty"`
	Conditions    []PipelineCondition `json:"conditions,omitempty"`
	Label         string              `json:"label"`
	PosX          float64             `json:"pos_x"`
	PosY          float64             `json:"pos_y"`
	CreatedAt     time.Time           `json:"created_at"`
}

// PipelineEdge is a directed edge between two pipeline nodes.
type PipelineEdge struct {
	ID           int64     `json:"id"`
	PipelineID   int64     `json:"pipeline_id"`
	SourceNodeID int64     `json:"source_node_id"`
	SourceHandle string    `json:"source_handle"`
	TargetNodeID int64     `json:"target_node_id"`
	CreatedAt    time.Time `json:"created_at"`
}

// PipelineGraph is the full DAG for a pipeline.
type PipelineGraph struct {
	Nodes []*PipelineNode `json:"nodes"`
	Edges []*PipelineEdge `json:"edges"`
}

// PipelineCondition is a single predicate evaluated against a context value.
type PipelineCondition struct {
	Key      string `json:"key"`
	Operator string `json:"op"`
	Value    string `json:"value,omitempty"`
}

// PipelineNodeRequest is a node in a PUT /api/pipelines/{id}/graph request.
type PipelineNodeRequest struct {
	ClientID      string              `json:"client_id"`
	Type          PipelineNodeType    `json:"type"`
	ActionID      *int64              `json:"action_id,omitempty"`
	SubPipelineID *int64              `json:"sub_pipeline_id,omitempty"`
	Conditions    []PipelineCondition `json:"conditions,omitempty"`
	Label         string              `json:"label"`
	PosX          float64             `json:"pos_x"`
	PosY          float64             `json:"pos_y"`
}

// PipelineEdgeRequest is an edge in a PUT /api/pipelines/{id}/graph request.
type PipelineEdgeRequest struct {
	SourceClientID string `json:"source_client_id"`
	SourceHandle   string `json:"source_handle"`
	TargetClientID string `json:"target_client_id"`
}

// PipelineGraphRequest is the body for PUT /api/pipelines/{id}/graph.
type PipelineGraphRequest struct {
	Nodes []*PipelineNodeRequest `json:"nodes"`
	Edges []*PipelineEdgeRequest `json:"edges"`
}

// TriggerPipelineLink connects a system trigger type to a pipeline.
type TriggerPipelineLink struct {
	ID          int64     `json:"id"`
	TriggerType string    `json:"trigger_type"`
	PipelineID  int64     `json:"pipeline_id"`
	Active      bool      `json:"active"`
	CreatedAt   time.Time `json:"created_at"`
}

// ActionBackend is a priority-ordered assignment of an Ollama backend to an action.
type ActionBackend struct {
	ActionID  int64 `json:"action_id"`
	BackendID int64 `json:"backend_id"`
	Priority  int   `json:"priority"`
}
