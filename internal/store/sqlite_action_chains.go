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

func (s *sqliteStore) SaveActionChain(ctx context.Context, c *model.ActionChain) error {
	condJSON, err := json.Marshal(c.Conditions)
	if err != nil {
		return fmt.Errorf("store: save action chain: marshal conditions: %w", err)
	}
	now := time.Now().UTC().Format(time.RFC3339)
	res, err := s.db.ExecContext(ctx,
		`INSERT INTO action_chains (source_action_id, target_action_id, conditions, created_at) VALUES (?,?,?,?)`,
		c.SourceActionID, c.TargetActionID, string(condJSON), now,
	)
	if err != nil {
		return fmt.Errorf("store: save action chain: %w", err)
	}
	id, err := res.LastInsertId()
	if err != nil {
		return fmt.Errorf("store: save action chain: last insert id: %w", err)
	}
	c.ID = id
	return nil
}

func (s *sqliteStore) GetActionChain(ctx context.Context, id int64) (*model.ActionChain, error) {
	row := s.db.QueryRowContext(ctx,
		`SELECT id, source_action_id, target_action_id, conditions, created_at FROM action_chains WHERE id = ?`, id)
	c, err := scanActionChain(row)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, fmt.Errorf("store: get action chain: not found: %w", err)
		}
		return nil, fmt.Errorf("store: get action chain: %w", err)
	}
	return c, nil
}

func (s *sqliteStore) ListActionChains(ctx context.Context, filter ActionChainFilter) ([]*model.ActionChain, error) {
	var where []string
	var args []any
	if filter.SourceActionID != nil {
		where = append(where, "source_action_id = ?")
		args = append(args, *filter.SourceActionID)
	}
	if filter.TargetActionID != nil {
		where = append(where, "target_action_id = ?")
		args = append(args, *filter.TargetActionID)
	}
	q := "SELECT id, source_action_id, target_action_id, conditions, created_at FROM action_chains"
	if len(where) > 0 {
		q += " WHERE " + strings.Join(where, " AND ")
	}
	q += " ORDER BY id ASC"

	rows, err := s.db.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, fmt.Errorf("store: list action chains: %w", err)
	}
	defer rows.Close()

	var out []*model.ActionChain
	for rows.Next() {
		c, err := scanActionChain(rows)
		if err != nil {
			return nil, fmt.Errorf("store: list action chains: scan: %w", err)
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

func (s *sqliteStore) UpdateActionChain(ctx context.Context, c *model.ActionChain) error {
	condJSON, err := json.Marshal(c.Conditions)
	if err != nil {
		return fmt.Errorf("store: update action chain: marshal conditions: %w", err)
	}
	_, err = s.db.ExecContext(ctx,
		`UPDATE action_chains SET conditions = ? WHERE id = ?`,
		string(condJSON), c.ID,
	)
	if err != nil {
		return fmt.Errorf("store: update action chain: %w", err)
	}
	return nil
}

func (s *sqliteStore) DeleteActionChain(ctx context.Context, id int64) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM action_chains WHERE id = ?`, id)
	if err != nil {
		return fmt.Errorf("store: delete action chain: %w", err)
	}
	return nil
}

// ── helpers ───────────────────────────────────────────────────────────────────

type chainScanner interface {
	Scan(dest ...any) error
}

func scanActionChain(r chainScanner) (*model.ActionChain, error) {
	c := &model.ActionChain{}
	var condStr, createdAt string
	if err := r.Scan(&c.ID, &c.SourceActionID, &c.TargetActionID, &condStr, &createdAt); err != nil {
		return nil, err
	}
	if err := json.Unmarshal([]byte(condStr), &c.Conditions); err != nil || c.Conditions == nil {
		c.Conditions = []model.ChainCondition{}
	}
	if t, err := time.Parse(time.RFC3339, createdAt); err == nil {
		c.CreatedAt = t
	}
	return c, nil
}
