package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/amir-the-h/iran-watcher/internal/model"
)

// ── Pipelines ─────────────────────────────────────────────────────────────────

func (s *sqliteStore) SavePipeline(ctx context.Context, p *model.Pipeline) error {
	now := time.Now().UTC().Format(time.RFC3339)
	res, err := s.db.ExecContext(ctx,
		`INSERT INTO pipelines (name, description, active, created_at, updated_at) VALUES (?,?,?,?,?)`,
		p.Name, p.Description, boolToInt(p.Active), now, now,
	)
	if err != nil {
		return fmt.Errorf("store: save pipeline: %w", err)
	}
	id, _ := res.LastInsertId()
	p.ID = id
	return nil
}

func (s *sqliteStore) GetPipeline(ctx context.Context, id int64) (*model.Pipeline, error) {
	row := s.db.QueryRowContext(ctx, `
		SELECT p.id, p.name, COALESCE(p.description,''), p.active, p.created_at, p.updated_at,
		       (SELECT COUNT(*) FROM pipeline_nodes WHERE pipeline_id = p.id) AS node_count
		FROM pipelines p WHERE p.id = ?`, id)
	p, err := scanPipeline(row)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, fmt.Errorf("store: get pipeline: not found: %w", err)
		}
		return nil, fmt.Errorf("store: get pipeline: %w", err)
	}
	return p, nil
}

func (s *sqliteStore) ListPipelines(ctx context.Context) ([]*model.Pipeline, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT p.id, p.name, COALESCE(p.description,''), p.active, p.created_at, p.updated_at,
		       (SELECT COUNT(*) FROM pipeline_nodes WHERE pipeline_id = p.id) AS node_count
		FROM pipelines p ORDER BY p.id ASC`)
	if err != nil {
		return nil, fmt.Errorf("store: list pipelines: %w", err)
	}
	defer rows.Close()
	var out []*model.Pipeline
	for rows.Next() {
		p, err := scanPipeline(rows)
		if err != nil {
			return nil, fmt.Errorf("store: list pipelines: scan: %w", err)
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

func (s *sqliteStore) UpdatePipeline(ctx context.Context, p *model.Pipeline) error {
	now := time.Now().UTC().Format(time.RFC3339)
	_, err := s.db.ExecContext(ctx,
		`UPDATE pipelines SET name = ?, description = ?, updated_at = ? WHERE id = ?`,
		p.Name, p.Description, now, p.ID,
	)
	if err != nil {
		return fmt.Errorf("store: update pipeline: %w", err)
	}
	return nil
}

func (s *sqliteStore) DeletePipeline(ctx context.Context, id int64) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM pipelines WHERE id = ?`, id)
	if err != nil {
		return fmt.Errorf("store: delete pipeline: %w", err)
	}
	return nil
}

func (s *sqliteStore) BulkDeletePipelines(ctx context.Context, f BulkDeletePipelinesFilter) (int64, error) {
	var clauses []string
	var args []any

	if len(f.IDs) > 0 {
		ph := strings.Repeat("?,", len(f.IDs))
		clauses = append(clauses, fmt.Sprintf("id IN (%s)", ph[:len(ph)-1]))
		for _, id := range f.IDs {
			args = append(args, id)
		}
	}
	if f.Active != nil {
		clauses = append(clauses, "active = ?")
		args = append(args, boolToInt(*f.Active))
	}

	query := "DELETE FROM pipelines"
	if len(clauses) > 0 {
		query += " WHERE " + strings.Join(clauses, " AND ")
	}

	res, err := s.db.ExecContext(ctx, query, args...)
	if err != nil {
		return 0, fmt.Errorf("store: bulk delete pipelines: %w", err)
	}
	n, _ := res.RowsAffected()
	return n, nil
}

func (s *sqliteStore) SetPipelineActive(ctx context.Context, id int64, active bool) error {
	now := time.Now().UTC().Format(time.RFC3339)
	_, err := s.db.ExecContext(ctx,
		`UPDATE pipelines SET active = ?, updated_at = ? WHERE id = ?`, boolToInt(active), now, id)
	if err != nil {
		return fmt.Errorf("store: set pipeline active: %w", err)
	}
	return nil
}

func (s *sqliteStore) GetPipelineGraph(ctx context.Context, pipelineID int64) (*model.PipelineGraph, error) {
	nodeRows, err := s.db.QueryContext(ctx, `
		SELECT id, pipeline_id, type, action_id, sub_pipeline_id, COALESCE(conditions,'[]'), COALESCE(label,''), pos_x, pos_y, created_at
		FROM pipeline_nodes WHERE pipeline_id = ? ORDER BY id ASC`, pipelineID)
	if err != nil {
		return nil, fmt.Errorf("store: get pipeline graph nodes: %w", err)
	}
	defer nodeRows.Close()
	var nodes []*model.PipelineNode
	for nodeRows.Next() {
		n, err := scanPipelineNode(nodeRows)
		if err != nil {
			return nil, fmt.Errorf("store: get pipeline graph nodes: scan: %w", err)
		}
		nodes = append(nodes, n)
	}
	if err := nodeRows.Err(); err != nil {
		return nil, fmt.Errorf("store: get pipeline graph nodes: %w", err)
	}

	edgeRows, err := s.db.QueryContext(ctx, `
		SELECT id, pipeline_id, source_node_id, source_handle, target_node_id, created_at
		FROM pipeline_edges WHERE pipeline_id = ? ORDER BY id ASC`, pipelineID)
	if err != nil {
		return nil, fmt.Errorf("store: get pipeline graph edges: %w", err)
	}
	defer edgeRows.Close()
	var edges []*model.PipelineEdge
	for edgeRows.Next() {
		e, err := scanPipelineEdge(edgeRows)
		if err != nil {
			return nil, fmt.Errorf("store: get pipeline graph edges: scan: %w", err)
		}
		edges = append(edges, e)
	}
	if err := edgeRows.Err(); err != nil {
		return nil, fmt.Errorf("store: get pipeline graph edges: %w", err)
	}

	if nodes == nil {
		nodes = []*model.PipelineNode{}
	}
	if edges == nil {
		edges = []*model.PipelineEdge{}
	}
	return &model.PipelineGraph{Nodes: nodes, Edges: edges}, nil
}

func (s *sqliteStore) SavePipelineGraph(ctx context.Context, pipelineID int64, nodes []*model.PipelineNodeRequest, edges []*model.PipelineEdgeRequest) (*model.PipelineGraph, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, fmt.Errorf("store: save pipeline graph: begin tx: %w", err)
	}
	defer tx.Rollback() //nolint:errcheck

	if _, err := tx.ExecContext(ctx, `DELETE FROM pipeline_edges WHERE pipeline_id = ?`, pipelineID); err != nil {
		return nil, fmt.Errorf("store: save pipeline graph: delete edges: %w", err)
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM pipeline_nodes WHERE pipeline_id = ?`, pipelineID); err != nil {
		return nil, fmt.Errorf("store: save pipeline graph: delete nodes: %w", err)
	}

	now := time.Now().UTC().Format(time.RFC3339)
	clientToDBID := make(map[string]int64, len(nodes))
	savedNodes := make([]*model.PipelineNode, 0, len(nodes))

	for _, n := range nodes {
		condJSON := "[]"
		if len(n.Conditions) > 0 {
			b, _ := json.Marshal(n.Conditions)
			condJSON = string(b)
		}
		res, err := tx.ExecContext(ctx,
			`INSERT INTO pipeline_nodes (pipeline_id, type, action_id, sub_pipeline_id, conditions, label, pos_x, pos_y, created_at)
			 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
			pipelineID, string(n.Type), n.ActionID, n.SubPipelineID, condJSON, n.Label, n.PosX, n.PosY, now,
		)
		if err != nil {
			return nil, fmt.Errorf("store: save pipeline graph: insert node: %w", err)
		}
		dbID, _ := res.LastInsertId()
		clientToDBID[n.ClientID] = dbID
		node := &model.PipelineNode{
			ID: dbID, PipelineID: pipelineID, Type: n.Type, ActionID: n.ActionID,
			SubPipelineID: n.SubPipelineID,
			Conditions:    n.Conditions, Label: n.Label, PosX: n.PosX, PosY: n.PosY,
		}
		if t, err := time.Parse(time.RFC3339, now); err == nil {
			node.CreatedAt = t
		}
		if node.Conditions == nil {
			node.Conditions = []model.PipelineCondition{}
		}
		savedNodes = append(savedNodes, node)
	}

	savedEdges := make([]*model.PipelineEdge, 0, len(edges))
	for _, e := range edges {
		srcID, srcOK := clientToDBID[e.SourceClientID]
		tgtID, tgtOK := clientToDBID[e.TargetClientID]
		if !srcOK || !tgtOK {
			return nil, fmt.Errorf("store: save pipeline graph: unknown node client_id in edge (%s→%s)", e.SourceClientID, e.TargetClientID)
		}
		handle := e.SourceHandle
		if handle == "" {
			handle = "default"
		}
		res, err := tx.ExecContext(ctx,
			`INSERT INTO pipeline_edges (pipeline_id, source_node_id, source_handle, target_node_id, created_at)
			 VALUES (?,?,?,?,?)`,
			pipelineID, srcID, handle, tgtID, now,
		)
		if err != nil {
			return nil, fmt.Errorf("store: save pipeline graph: insert edge: %w", err)
		}
		dbID, _ := res.LastInsertId()
		savedEdges = append(savedEdges, &model.PipelineEdge{
			ID: dbID, PipelineID: pipelineID,
			SourceNodeID: srcID, SourceHandle: handle, TargetNodeID: tgtID,
		})
	}

	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("store: save pipeline graph: commit: %w", err)
	}
	return &model.PipelineGraph{Nodes: savedNodes, Edges: savedEdges}, nil
}

// ── TriggerPipelineLinks ──────────────────────────────────────────────────────

func (s *sqliteStore) SaveTriggerPipelineLink(ctx context.Context, l *model.TriggerPipelineLink) error {
	now := time.Now().UTC().Format(time.RFC3339)
	res, err := s.db.ExecContext(ctx,
		`INSERT INTO trigger_pipeline_links (trigger_type, pipeline_id, active, created_at) VALUES (?,?,?,?)`,
		l.TriggerType, l.PipelineID, boolToInt(l.Active), now,
	)
	if err != nil {
		return fmt.Errorf("store: save trigger pipeline link: %w", err)
	}
	id, _ := res.LastInsertId()
	l.ID = id
	return nil
}

func (s *sqliteStore) ListTriggerPipelineLinks(ctx context.Context) ([]*model.TriggerPipelineLink, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, trigger_type, pipeline_id, active, created_at
		 FROM trigger_pipeline_links ORDER BY id ASC`)
	if err != nil {
		return nil, fmt.Errorf("store: list trigger pipeline links: %w", err)
	}
	defer rows.Close()
	var out []*model.TriggerPipelineLink
	for rows.Next() {
		l, err := scanTriggerPipelineLink(rows)
		if err != nil {
			return nil, fmt.Errorf("store: list trigger pipeline links: scan: %w", err)
		}
		out = append(out, l)
	}
	return out, rows.Err()
}

func (s *sqliteStore) ListActiveTriggerPipelineLinks(ctx context.Context, triggerType string) ([]*model.TriggerPipelineLink, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, trigger_type, pipeline_id, active, created_at
		 FROM trigger_pipeline_links WHERE trigger_type = ? AND active = 1 ORDER BY id ASC`, triggerType)
	if err != nil {
		return nil, fmt.Errorf("store: list active trigger pipeline links: %w", err)
	}
	defer rows.Close()
	var out []*model.TriggerPipelineLink
	for rows.Next() {
		l, err := scanTriggerPipelineLink(rows)
		if err != nil {
			return nil, fmt.Errorf("store: list active trigger pipeline links: scan: %w", err)
		}
		out = append(out, l)
	}
	return out, rows.Err()
}

func (s *sqliteStore) GetPipelineIDForNode(ctx context.Context, nodeID int64) (int64, error) {
	var pipelineID int64
	err := s.db.QueryRowContext(ctx, `SELECT pipeline_id FROM pipeline_nodes WHERE id = ?`, nodeID).Scan(&pipelineID)
	if err != nil {
		return 0, fmt.Errorf("store: get pipeline id for node %d: %w", nodeID, err)
	}
	return pipelineID, nil
}

func (s *sqliteStore) DeleteTriggerPipelineLink(ctx context.Context, id int64) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM trigger_pipeline_links WHERE id = ?`, id)
	if err != nil {
		return fmt.Errorf("store: delete trigger pipeline link: %w", err)
	}
	return nil
}

func (s *sqliteStore) BulkDeleteTriggerPipelineLinks(ctx context.Context, f BulkDeleteTriggerPipelineLinksFilter) (int64, error) {
	var clauses []string
	var args []any

	if len(f.IDs) > 0 {
		ph := strings.Repeat("?,", len(f.IDs))
		clauses = append(clauses, fmt.Sprintf("id IN (%s)", ph[:len(ph)-1]))
		for _, id := range f.IDs {
			args = append(args, id)
		}
	}
	if f.PipelineID != nil {
		clauses = append(clauses, "pipeline_id = ?")
		args = append(args, *f.PipelineID)
	}
	if f.TriggerType != "" {
		clauses = append(clauses, "trigger_type = ?")
		args = append(args, f.TriggerType)
	}

	query := "DELETE FROM trigger_pipeline_links"
	if len(clauses) > 0 {
		query += " WHERE " + strings.Join(clauses, " AND ")
	}

	res, err := s.db.ExecContext(ctx, query, args...)
	if err != nil {
		return 0, fmt.Errorf("store: bulk delete trigger pipeline links: %w", err)
	}
	n, _ := res.RowsAffected()
	return n, nil
}

func (s *sqliteStore) SetTriggerPipelineLinkActive(ctx context.Context, id int64, active bool) error {
	_, err := s.db.ExecContext(ctx,
		`UPDATE trigger_pipeline_links SET active = ? WHERE id = ?`, boolToInt(active), id)
	if err != nil {
		return fmt.Errorf("store: set trigger pipeline link active: %w", err)
	}
	return nil
}

// ── helpers ───────────────────────────────────────────────────────────────────

type pipelineScanner interface {
	Scan(dest ...any) error
}

func scanPipeline(r pipelineScanner) (*model.Pipeline, error) {
	p := &model.Pipeline{}
	var activeInt, nodeCount int
	var createdAt, updatedAt string
	if err := r.Scan(&p.ID, &p.Name, &p.Description, &activeInt, &createdAt, &updatedAt, &nodeCount); err != nil {
		return nil, err
	}
	p.Active = activeInt != 0
	p.NodeCount = nodeCount
	if t, err := time.Parse(time.RFC3339, createdAt); err == nil {
		p.CreatedAt = t
	}
	if t, err := time.Parse(time.RFC3339, updatedAt); err == nil {
		p.UpdatedAt = t
	}
	return p, nil
}

func scanPipelineNode(r pipelineScanner) (*model.PipelineNode, error) {
	n := &model.PipelineNode{}
	var nodeType string
	var actionID sql.NullInt64
	var subPipelineID sql.NullInt64
	var condStr, label, createdAt string
	if err := r.Scan(&n.ID, &n.PipelineID, &nodeType, &actionID, &subPipelineID, &condStr, &label, &n.PosX, &n.PosY, &createdAt); err != nil {
		return nil, err
	}
	n.Type = model.PipelineNodeType(nodeType)
	n.Label = label
	if actionID.Valid {
		v := actionID.Int64
		n.ActionID = &v
	}
	if subPipelineID.Valid {
		n.SubPipelineID = &subPipelineID.Int64
	}
	if err := json.Unmarshal([]byte(condStr), &n.Conditions); err != nil || n.Conditions == nil {
		n.Conditions = []model.PipelineCondition{}
	}
	if t, err := time.Parse(time.RFC3339, createdAt); err == nil {
		n.CreatedAt = t
	}
	return n, nil
}

func scanPipelineEdge(r pipelineScanner) (*model.PipelineEdge, error) {
	e := &model.PipelineEdge{}
	var createdAt string
	if err := r.Scan(&e.ID, &e.PipelineID, &e.SourceNodeID, &e.SourceHandle, &e.TargetNodeID, &createdAt); err != nil {
		return nil, err
	}
	if t, err := time.Parse(time.RFC3339, createdAt); err == nil {
		e.CreatedAt = t
	}
	return e, nil
}

func scanTriggerPipelineLink(r pipelineScanner) (*model.TriggerPipelineLink, error) {
	l := &model.TriggerPipelineLink{}
	var activeInt int
	var createdAt string
	if err := r.Scan(&l.ID, &l.TriggerType, &l.PipelineID, &activeInt, &createdAt); err != nil {
		return nil, err
	}
	l.Active = activeInt != 0
	if t, err := time.Parse(time.RFC3339, createdAt); err == nil {
		l.CreatedAt = t
	}
	return l, nil
}
