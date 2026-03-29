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

func (s *sqliteStore) SavePipeline(ctx context.Context, p *model.Pipeline) error {
	cfg := p.Config
	if len(cfg) == 0 {
		cfg = []byte("{}")
	}
	now := time.Now().UTC().Format(time.RFC3339)
	res, err := s.db.ExecContext(ctx,
		`INSERT INTO pipelines (name, type, config, active, created_at, updated_at) VALUES (?,?,?,?,?,?)`,
		p.Name, string(p.Type), string(cfg), boolToInt(p.Active), now, now,
	)
	if err != nil {
		return fmt.Errorf("store: save pipeline: %w", err)
	}
	id, err := res.LastInsertId()
	if err != nil {
		return fmt.Errorf("store: save pipeline: last insert id: %w", err)
	}
	p.ID = id
	return nil
}

func (s *sqliteStore) GetPipeline(ctx context.Context, id int64) (*model.Pipeline, error) {
	row := s.db.QueryRowContext(ctx,
		`SELECT id, name, type, config, active, created_at, updated_at FROM pipelines WHERE id = ?`, id)
	p, err := scanPipeline(row)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, fmt.Errorf("store: get pipeline: not found: %w", err)
		}
		return nil, fmt.Errorf("store: get pipeline: %w", err)
	}
	return p, nil
}

func (s *sqliteStore) ListPipelines(ctx context.Context, filter PipelineFilter) ([]*model.Pipeline, error) {
	var where []string
	var args []any
	if filter.Type != nil {
		where = append(where, "type = ?")
		args = append(args, string(*filter.Type))
	}
	if filter.Active != nil {
		where = append(where, "active = ?")
		args = append(args, boolToInt(*filter.Active))
	}
	if filter.Usecase != nil {
		where = append(where, "json_extract(config, '$.usecase') = ?")
		args = append(args, *filter.Usecase)
	}
	q := "SELECT id, name, type, config, active, created_at, updated_at FROM pipelines"
	if len(where) > 0 {
		q += " WHERE " + strings.Join(where, " AND ")
	}
	q += " ORDER BY id ASC"

	rows, err := s.db.QueryContext(ctx, q, args...)
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
	cfg := p.Config
	if len(cfg) == 0 {
		cfg = []byte("{}")
	}
	now := time.Now().UTC().Format(time.RFC3339)
	_, err := s.db.ExecContext(ctx,
		`UPDATE pipelines SET name = ?, type = ?, config = ?, updated_at = ? WHERE id = ?`,
		p.Name, string(p.Type), string(cfg), now, p.ID,
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

func (s *sqliteStore) SetPipelineActive(ctx context.Context, id int64, active bool) error {
	now := time.Now().UTC().Format(time.RFC3339)
	_, err := s.db.ExecContext(ctx,
		`UPDATE pipelines SET active = ?, updated_at = ? WHERE id = ?`, boolToInt(active), now, id)
	if err != nil {
		return fmt.Errorf("store: set pipeline active: %w", err)
	}
	return nil
}

func (s *sqliteStore) ListPipelineLabels(ctx context.Context, pipelineID int64) ([]string, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT label FROM pipeline_labels WHERE pipeline_id = ? ORDER BY label ASC`, pipelineID)
	if err != nil {
		return nil, fmt.Errorf("store: list pipeline labels: %w", err)
	}
	defer rows.Close()

	var out []string
	for rows.Next() {
		var label string
		if err := rows.Scan(&label); err != nil {
			return nil, fmt.Errorf("store: list pipeline labels: scan: %w", err)
		}
		out = append(out, label)
	}
	return out, rows.Err()
}

func (s *sqliteStore) AddPipelineLabel(ctx context.Context, pipelineID int64, label string) error {
	_, err := s.db.ExecContext(ctx,
		`INSERT OR IGNORE INTO pipeline_labels (pipeline_id, label) VALUES (?, ?)`, pipelineID, label)
	if err != nil {
		return fmt.Errorf("store: add pipeline label: %w", err)
	}
	return nil
}

func (s *sqliteStore) RemovePipelineLabel(ctx context.Context, pipelineID int64, label string) error {
	_, err := s.db.ExecContext(ctx,
		`DELETE FROM pipeline_labels WHERE pipeline_id = ? AND label = ?`, pipelineID, label)
	if err != nil {
		return fmt.Errorf("store: remove pipeline label: %w", err)
	}
	return nil
}

// ── helpers ───────────────────────────────────────────────────────────────────

type pipelineScanner interface {
	Scan(dest ...any) error
}

func scanPipeline(r pipelineScanner) (*model.Pipeline, error) {
	p := &model.Pipeline{}
	var activeInt int
	var configStr, createdAt, updatedAt string
	if err := r.Scan(&p.ID, &p.Name, &p.Type, &configStr, &activeInt, &createdAt, &updatedAt); err != nil {
		return nil, err
	}
	p.Active = activeInt != 0
	p.Config = json.RawMessage(configStr)
	if t, err := time.Parse(time.RFC3339, createdAt); err == nil {
		p.CreatedAt = t
	}
	if t, err := time.Parse(time.RFC3339, updatedAt); err == nil {
		p.UpdatedAt = t
	}
	return p, nil
}
