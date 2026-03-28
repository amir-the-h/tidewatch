package store

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"github.com/amir-the-h/iran-watcher/internal/model"
)

func (s *sqliteStore) ListOllamaBackends(ctx context.Context) ([]model.OllamaBackend, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, name, url, enabled, created_at FROM ollama_backends ORDER BY id`)
	if err != nil {
		return nil, fmt.Errorf("store: list ollama backends: %w", err)
	}
	defer rows.Close()
	var out []model.OllamaBackend
	for rows.Next() {
		var b model.OllamaBackend
		var enabledInt int
		if err := rows.Scan(&b.ID, &b.Name, &b.URL, &enabledInt, &b.CreatedAt); err != nil {
			return nil, fmt.Errorf("store: scan ollama backend: %w", err)
		}
		b.Enabled = enabledInt != 0
		out = append(out, b)
	}
	return out, rows.Err()
}

func (s *sqliteStore) GetOllamaBackend(ctx context.Context, id int64) (model.OllamaBackend, error) {
	var b model.OllamaBackend
	var enabledInt int
	err := s.db.QueryRowContext(ctx,
		`SELECT id, name, url, enabled, created_at FROM ollama_backends WHERE id = ?`, id).
		Scan(&b.ID, &b.Name, &b.URL, &enabledInt, &b.CreatedAt)
	if err == sql.ErrNoRows {
		return b, fmt.Errorf("store: ollama backend %d not found", id)
	}
	if err != nil {
		return b, fmt.Errorf("store: get ollama backend: %w", err)
	}
	b.Enabled = enabledInt != 0
	return b, nil
}

func (s *sqliteStore) CreateOllamaBackend(ctx context.Context, name, url string) (model.OllamaBackend, error) {
	now := time.Now().UTC()
	res, err := s.db.ExecContext(ctx,
		`INSERT INTO ollama_backends (name, url, enabled, created_at) VALUES (?, ?, 1, ?)`,
		name, url, now)
	if err != nil {
		return model.OllamaBackend{}, fmt.Errorf("store: create ollama backend: %w", err)
	}
	id, _ := res.LastInsertId()
	return model.OllamaBackend{ID: id, Name: name, URL: url, Enabled: true, CreatedAt: now}, nil
}

func (s *sqliteStore) UpdateOllamaBackend(ctx context.Context, id int64, name, url string) error {
	_, err := s.db.ExecContext(ctx,
		`UPDATE ollama_backends SET name = ?, url = ? WHERE id = ?`, name, url, id)
	if err != nil {
		return fmt.Errorf("store: update ollama backend: %w", err)
	}
	return nil
}

func (s *sqliteStore) DeleteOllamaBackend(ctx context.Context, id int64) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM ollama_backends WHERE id = ?`, id)
	if err != nil {
		return fmt.Errorf("store: delete ollama backend: %w", err)
	}
	return nil
}

func (s *sqliteStore) SetOllamaBackendEnabled(ctx context.Context, id int64, enabled bool) error {
	v := 0
	if enabled {
		v = 1
	}
	_, err := s.db.ExecContext(ctx,
		`UPDATE ollama_backends SET enabled = ? WHERE id = ?`, v, id)
	if err != nil {
		return fmt.Errorf("store: set ollama backend enabled: %w", err)
	}
	return nil
}

func (s *sqliteStore) ListPipelineBackends(ctx context.Context, pipelineID int64) ([]model.PipelineBackend, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT pipeline_id, backend_id, priority FROM pipeline_backends
		 WHERE pipeline_id = ? ORDER BY priority`, pipelineID)
	if err != nil {
		return nil, fmt.Errorf("store: list pipeline backends: %w", err)
	}
	defer rows.Close()
	var out []model.PipelineBackend
	for rows.Next() {
		var pb model.PipelineBackend
		if err := rows.Scan(&pb.PipelineID, &pb.BackendID, &pb.Priority); err != nil {
			return nil, fmt.Errorf("store: scan pipeline backend: %w", err)
		}
		out = append(out, pb)
	}
	return out, rows.Err()
}

func (s *sqliteStore) SetPipelineBackends(ctx context.Context, pipelineID int64, assignments []model.PipelineBackend) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("store: set pipeline backends tx: %w", err)
	}
	defer tx.Rollback() //nolint:errcheck

	if _, err := tx.ExecContext(ctx,
		`DELETE FROM pipeline_backends WHERE pipeline_id = ?`, pipelineID); err != nil {
		return fmt.Errorf("store: clear pipeline backends: %w", err)
	}
	for _, a := range assignments {
		if _, err := tx.ExecContext(ctx,
			`INSERT INTO pipeline_backends (pipeline_id, backend_id, priority) VALUES (?, ?, ?)`,
			pipelineID, a.BackendID, a.Priority); err != nil {
			return fmt.Errorf("store: insert pipeline backend: %w", err)
		}
	}
	return tx.Commit()
}

func (s *sqliteStore) RemovePipelineBackend(ctx context.Context, pipelineID, backendID int64) error {
	_, err := s.db.ExecContext(ctx,
		`DELETE FROM pipeline_backends WHERE pipeline_id = ? AND backend_id = ?`,
		pipelineID, backendID)
	if err != nil {
		return fmt.Errorf("store: remove pipeline backend: %w", err)
	}
	return nil
}

func (s *sqliteStore) ListAllPipelineBackendIDs(ctx context.Context) (map[int64][]int64, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT pipeline_id, backend_id FROM pipeline_backends ORDER BY pipeline_id, priority`)
	if err != nil {
		return nil, fmt.Errorf("store: list all pipeline backend ids: %w", err)
	}
	defer rows.Close()
	out := make(map[int64][]int64)
	for rows.Next() {
		var pipelineID, backendID int64
		if err := rows.Scan(&pipelineID, &backendID); err != nil {
			return nil, fmt.Errorf("store: scan pipeline backend ids: %w", err)
		}
		out[pipelineID] = append(out[pipelineID], backendID)
	}
	return out, rows.Err()
}
