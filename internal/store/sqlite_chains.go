package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/amir-the-h/iran-watcher/internal/model"
)

// ── Chains ────────────────────────────────────────────────────────────────────

func (s *sqliteStore) SaveChain(ctx context.Context, c *model.Chain) error {
	now := time.Now().UTC().Format(time.RFC3339)
	res, err := s.db.ExecContext(ctx,
		`INSERT INTO chains (name, description, active, created_at, updated_at) VALUES (?,?,?,?,?)`,
		c.Name, c.Description, boolToInt(c.Active), now, now,
	)
	if err != nil {
		return fmt.Errorf("store: save chain: %w", err)
	}
	id, _ := res.LastInsertId()
	c.ID = id
	return nil
}

func (s *sqliteStore) GetChain(ctx context.Context, id int64) (*model.Chain, error) {
	row := s.db.QueryRowContext(ctx, `
		SELECT c.id, c.name, COALESCE(c.description,''), c.active, c.created_at, c.updated_at,
		       (SELECT COUNT(*) FROM chain_nodes WHERE chain_id = c.id) AS node_count
		FROM chains c WHERE c.id = ?`, id)
	c, err := scanChain(row)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, fmt.Errorf("store: get chain: not found: %w", err)
		}
		return nil, fmt.Errorf("store: get chain: %w", err)
	}
	return c, nil
}

func (s *sqliteStore) ListChains(ctx context.Context) ([]*model.Chain, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT c.id, c.name, COALESCE(c.description,''), c.active, c.created_at, c.updated_at,
		       (SELECT COUNT(*) FROM chain_nodes WHERE chain_id = c.id) AS node_count
		FROM chains c ORDER BY c.id ASC`)
	if err != nil {
		return nil, fmt.Errorf("store: list chains: %w", err)
	}
	defer rows.Close()
	var out []*model.Chain
	for rows.Next() {
		c, err := scanChain(rows)
		if err != nil {
			return nil, fmt.Errorf("store: list chains: scan: %w", err)
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

func (s *sqliteStore) UpdateChain(ctx context.Context, c *model.Chain) error {
	now := time.Now().UTC().Format(time.RFC3339)
	_, err := s.db.ExecContext(ctx,
		`UPDATE chains SET name = ?, description = ?, updated_at = ? WHERE id = ?`,
		c.Name, c.Description, now, c.ID,
	)
	if err != nil {
		return fmt.Errorf("store: update chain: %w", err)
	}
	return nil
}

func (s *sqliteStore) DeleteChain(ctx context.Context, id int64) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM chains WHERE id = ?`, id)
	if err != nil {
		return fmt.Errorf("store: delete chain: %w", err)
	}
	return nil
}

func (s *sqliteStore) SetChainActive(ctx context.Context, id int64, active bool) error {
	now := time.Now().UTC().Format(time.RFC3339)
	_, err := s.db.ExecContext(ctx,
		`UPDATE chains SET active = ?, updated_at = ? WHERE id = ?`, boolToInt(active), now, id)
	if err != nil {
		return fmt.Errorf("store: set chain active: %w", err)
	}
	return nil
}

func (s *sqliteStore) GetChainGraph(ctx context.Context, chainID int64) (*model.ChainGraph, error) {
	nodeRows, err := s.db.QueryContext(ctx, `
		SELECT id, chain_id, type, action_id, COALESCE(conditions,'[]'), COALESCE(label,''), pos_x, pos_y, created_at
		FROM chain_nodes WHERE chain_id = ? ORDER BY id ASC`, chainID)
	if err != nil {
		return nil, fmt.Errorf("store: get chain graph nodes: %w", err)
	}
	defer nodeRows.Close()
	var nodes []*model.ChainNode
	for nodeRows.Next() {
		n, err := scanChainNode(nodeRows)
		if err != nil {
			return nil, fmt.Errorf("store: get chain graph nodes: scan: %w", err)
		}
		nodes = append(nodes, n)
	}
	if err := nodeRows.Err(); err != nil {
		return nil, fmt.Errorf("store: get chain graph nodes: %w", err)
	}

	edgeRows, err := s.db.QueryContext(ctx, `
		SELECT id, chain_id, source_node_id, source_handle, target_node_id, created_at
		FROM chain_edges WHERE chain_id = ? ORDER BY id ASC`, chainID)
	if err != nil {
		return nil, fmt.Errorf("store: get chain graph edges: %w", err)
	}
	defer edgeRows.Close()
	var edges []*model.ChainEdge
	for edgeRows.Next() {
		e, err := scanChainEdge(edgeRows)
		if err != nil {
			return nil, fmt.Errorf("store: get chain graph edges: scan: %w", err)
		}
		edges = append(edges, e)
	}
	if err := edgeRows.Err(); err != nil {
		return nil, fmt.Errorf("store: get chain graph edges: %w", err)
	}

	if nodes == nil {
		nodes = []*model.ChainNode{}
	}
	if edges == nil {
		edges = []*model.ChainEdge{}
	}
	return &model.ChainGraph{Nodes: nodes, Edges: edges}, nil
}

func (s *sqliteStore) SaveChainGraph(ctx context.Context, chainID int64, nodes []*model.ChainNodeRequest, edges []*model.ChainEdgeRequest) (*model.ChainGraph, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, fmt.Errorf("store: save chain graph: begin tx: %w", err)
	}
	defer tx.Rollback() //nolint:errcheck

	if _, err := tx.ExecContext(ctx, `DELETE FROM chain_edges WHERE chain_id = ?`, chainID); err != nil {
		return nil, fmt.Errorf("store: save chain graph: delete edges: %w", err)
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM chain_nodes WHERE chain_id = ?`, chainID); err != nil {
		return nil, fmt.Errorf("store: save chain graph: delete nodes: %w", err)
	}

	now := time.Now().UTC().Format(time.RFC3339)
	clientToDBID := make(map[string]int64, len(nodes))
	savedNodes := make([]*model.ChainNode, 0, len(nodes))

	for _, n := range nodes {
		condJSON := "[]"
		if len(n.Conditions) > 0 {
			b, _ := json.Marshal(n.Conditions)
			condJSON = string(b)
		}
		res, err := tx.ExecContext(ctx,
			`INSERT INTO chain_nodes (chain_id, type, action_id, conditions, label, pos_x, pos_y, created_at)
			 VALUES (?,?,?,?,?,?,?,?)`,
			chainID, string(n.Type), n.ActionID, condJSON, n.Label, n.PosX, n.PosY, now,
		)
		if err != nil {
			return nil, fmt.Errorf("store: save chain graph: insert node: %w", err)
		}
		dbID, _ := res.LastInsertId()
		clientToDBID[n.ClientID] = dbID
		node := &model.ChainNode{
			ID: dbID, ChainID: chainID, Type: n.Type, ActionID: n.ActionID,
			Conditions: n.Conditions, Label: n.Label, PosX: n.PosX, PosY: n.PosY,
		}
		if t, err := time.Parse(time.RFC3339, now); err == nil {
			node.CreatedAt = t
		}
		if node.Conditions == nil {
			node.Conditions = []model.ChainCondition{}
		}
		savedNodes = append(savedNodes, node)
	}

	savedEdges := make([]*model.ChainEdge, 0, len(edges))
	for _, e := range edges {
		srcID, srcOK := clientToDBID[e.SourceClientID]
		tgtID, tgtOK := clientToDBID[e.TargetClientID]
		if !srcOK || !tgtOK {
			return nil, fmt.Errorf("store: save chain graph: unknown node client_id in edge (%s→%s)", e.SourceClientID, e.TargetClientID)
		}
		handle := e.SourceHandle
		if handle == "" {
			handle = "default"
		}
		res, err := tx.ExecContext(ctx,
			`INSERT INTO chain_edges (chain_id, source_node_id, source_handle, target_node_id, created_at)
			 VALUES (?,?,?,?,?)`,
			chainID, srcID, handle, tgtID, now,
		)
		if err != nil {
			return nil, fmt.Errorf("store: save chain graph: insert edge: %w", err)
		}
		dbID, _ := res.LastInsertId()
		savedEdges = append(savedEdges, &model.ChainEdge{
			ID: dbID, ChainID: chainID,
			SourceNodeID: srcID, SourceHandle: handle, TargetNodeID: tgtID,
		})
	}

	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("store: save chain graph: commit: %w", err)
	}
	return &model.ChainGraph{Nodes: savedNodes, Edges: savedEdges}, nil
}

// ── TriggerChainLinks ─────────────────────────────────────────────────────────

func (s *sqliteStore) SaveTriggerChainLink(ctx context.Context, l *model.TriggerChainLink) error {
	now := time.Now().UTC().Format(time.RFC3339)
	res, err := s.db.ExecContext(ctx,
		`INSERT INTO trigger_chain_links (trigger_type, chain_id, active, created_at) VALUES (?,?,?,?)`,
		l.TriggerType, l.ChainID, boolToInt(l.Active), now,
	)
	if err != nil {
		return fmt.Errorf("store: save trigger chain link: %w", err)
	}
	id, _ := res.LastInsertId()
	l.ID = id
	return nil
}

func (s *sqliteStore) ListTriggerChainLinks(ctx context.Context) ([]*model.TriggerChainLink, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, trigger_type, chain_id, active, created_at
		 FROM trigger_chain_links ORDER BY id ASC`)
	if err != nil {
		return nil, fmt.Errorf("store: list trigger chain links: %w", err)
	}
	defer rows.Close()
	var out []*model.TriggerChainLink
	for rows.Next() {
		l, err := scanTriggerChainLink(rows)
		if err != nil {
			return nil, fmt.Errorf("store: list trigger chain links: scan: %w", err)
		}
		out = append(out, l)
	}
	return out, rows.Err()
}

func (s *sqliteStore) DeleteTriggerChainLink(ctx context.Context, id int64) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM trigger_chain_links WHERE id = ?`, id)
	if err != nil {
		return fmt.Errorf("store: delete trigger chain link: %w", err)
	}
	return nil
}

func (s *sqliteStore) SetTriggerChainLinkActive(ctx context.Context, id int64, active bool) error {
	_, err := s.db.ExecContext(ctx,
		`UPDATE trigger_chain_links SET active = ? WHERE id = ?`, boolToInt(active), id)
	if err != nil {
		return fmt.Errorf("store: set trigger chain link active: %w", err)
	}
	return nil
}

// ── helpers ───────────────────────────────────────────────────────────────────

type chainScanner interface {
	Scan(dest ...any) error
}

func scanChain(r chainScanner) (*model.Chain, error) {
	c := &model.Chain{}
	var activeInt, nodeCount int
	var createdAt, updatedAt string
	if err := r.Scan(&c.ID, &c.Name, &c.Description, &activeInt, &createdAt, &updatedAt, &nodeCount); err != nil {
		return nil, err
	}
	c.Active = activeInt != 0
	c.NodeCount = nodeCount
	if t, err := time.Parse(time.RFC3339, createdAt); err == nil {
		c.CreatedAt = t
	}
	if t, err := time.Parse(time.RFC3339, updatedAt); err == nil {
		c.UpdatedAt = t
	}
	return c, nil
}

func scanChainNode(r chainScanner) (*model.ChainNode, error) {
	n := &model.ChainNode{}
	var nodeType string
	var actionID sql.NullInt64
	var condStr, label, createdAt string
	if err := r.Scan(&n.ID, &n.ChainID, &nodeType, &actionID, &condStr, &label, &n.PosX, &n.PosY, &createdAt); err != nil {
		return nil, err
	}
	n.Type = model.ChainNodeType(nodeType)
	n.Label = label
	if actionID.Valid {
		v := actionID.Int64
		n.ActionID = &v
	}
	if err := json.Unmarshal([]byte(condStr), &n.Conditions); err != nil || n.Conditions == nil {
		n.Conditions = []model.ChainCondition{}
	}
	if t, err := time.Parse(time.RFC3339, createdAt); err == nil {
		n.CreatedAt = t
	}
	return n, nil
}

func scanChainEdge(r chainScanner) (*model.ChainEdge, error) {
	e := &model.ChainEdge{}
	var createdAt string
	if err := r.Scan(&e.ID, &e.ChainID, &e.SourceNodeID, &e.SourceHandle, &e.TargetNodeID, &createdAt); err != nil {
		return nil, err
	}
	if t, err := time.Parse(time.RFC3339, createdAt); err == nil {
		e.CreatedAt = t
	}
	return e, nil
}

func scanTriggerChainLink(r chainScanner) (*model.TriggerChainLink, error) {
	l := &model.TriggerChainLink{}
	var activeInt int
	var createdAt string
	if err := r.Scan(&l.ID, &l.TriggerType, &l.ChainID, &activeInt, &createdAt); err != nil {
		return nil, err
	}
	l.Active = activeInt != 0
	if t, err := time.Parse(time.RFC3339, createdAt); err == nil {
		l.CreatedAt = t
	}
	return l, nil
}
