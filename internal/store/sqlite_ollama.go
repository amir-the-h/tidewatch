package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/amir-the-h/iran-watcher/internal/model"
)

func (s *sqliteStore) ListOllamaBackends(ctx context.Context) ([]model.OllamaBackend, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, name, url, enabled, max_concurrent, created_at FROM ollama_backends ORDER BY id`)
	if err != nil {
		return nil, fmt.Errorf("store: list ollama backends: %w", err)
	}
	defer rows.Close()
	var out []model.OllamaBackend
	for rows.Next() {
		var b model.OllamaBackend
		var enabledInt int
		if err := rows.Scan(&b.ID, &b.Name, &b.URL, &enabledInt, &b.MaxConcurrent, &b.CreatedAt); err != nil {
			return nil, fmt.Errorf("store: scan ollama backend: %w", err)
		}
		b.Enabled = enabledInt != 0
		if b.MaxConcurrent < 1 {
			b.MaxConcurrent = 1
		}
		out = append(out, b)
	}
	return out, rows.Err()
}

func (s *sqliteStore) GetOllamaBackend(ctx context.Context, id int64) (model.OllamaBackend, error) {
	var b model.OllamaBackend
	var enabledInt int
	err := s.db.QueryRowContext(ctx,
		`SELECT id, name, url, enabled, max_concurrent, created_at FROM ollama_backends WHERE id = ?`, id).
		Scan(&b.ID, &b.Name, &b.URL, &enabledInt, &b.MaxConcurrent, &b.CreatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return b, fmt.Errorf("store: get ollama backend: not found: %w", err)
	}
	if err != nil {
		return b, fmt.Errorf("store: get ollama backend: %w", err)
	}
	b.Enabled = enabledInt != 0
	if b.MaxConcurrent < 1 {
		b.MaxConcurrent = 1
	}
	return b, nil
}

func (s *sqliteStore) CreateOllamaBackend(ctx context.Context, name, url string, maxConcurrent int) (model.OllamaBackend, error) {
	if maxConcurrent < 1 {
		maxConcurrent = 1
	}
	now := time.Now().UTC()
	res, err := s.db.ExecContext(ctx,
		`INSERT INTO ollama_backends (name, url, enabled, max_concurrent, created_at) VALUES (?, ?, 1, ?, ?)`,
		name, url, maxConcurrent, now)
	if err != nil {
		return model.OllamaBackend{}, fmt.Errorf("store: create ollama backend: %w", err)
	}
	id, _ := res.LastInsertId()
	return model.OllamaBackend{ID: id, Name: name, URL: url, Enabled: true, MaxConcurrent: maxConcurrent, CreatedAt: now}, nil
}

func (s *sqliteStore) UpdateOllamaBackend(ctx context.Context, id int64, name, url string, maxConcurrent int) error {
	if maxConcurrent < 1 {
		maxConcurrent = 1
	}
	_, err := s.db.ExecContext(ctx,
		`UPDATE ollama_backends SET name = ?, url = ?, max_concurrent = ? WHERE id = ?`,
		name, url, maxConcurrent, id)
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

func (s *sqliteStore) BulkDeleteOllamaBackends(ctx context.Context, f BulkDeleteOllamaBackendsFilter) (int64, error) {
	var clauses []string
	var args []any

	if len(f.IDs) > 0 {
		ph := strings.Repeat("?,", len(f.IDs))
		clauses = append(clauses, fmt.Sprintf("id IN (%s)", ph[:len(ph)-1]))
		for _, id := range f.IDs {
			args = append(args, id)
		}
	}
	if f.Enabled != nil {
		clauses = append(clauses, "enabled = ?")
		args = append(args, boolToInt(*f.Enabled))
	}

	query := "DELETE FROM ollama_backends"
	if len(clauses) > 0 {
		query += " WHERE " + strings.Join(clauses, " AND ")
	}

	res, err := s.db.ExecContext(ctx, query, args...)
	if err != nil {
		return 0, fmt.Errorf("store: bulk delete ollama backends: %w", err)
	}
	n, _ := res.RowsAffected()
	return n, nil
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

func (s *sqliteStore) ListActionBackends(ctx context.Context, actionID int64) ([]model.ActionBackend, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT action_id, backend_id, priority FROM action_backends
		 WHERE action_id = ? ORDER BY priority`, actionID)
	if err != nil {
		return nil, fmt.Errorf("store: list action backends: %w", err)
	}
	defer rows.Close()
	out := make([]model.ActionBackend, 0)
	for rows.Next() {
		var ab model.ActionBackend
		if err := rows.Scan(&ab.ActionID, &ab.BackendID, &ab.Priority); err != nil {
			return nil, fmt.Errorf("store: scan action backend: %w", err)
		}
		out = append(out, ab)
	}
	return out, rows.Err()
}

func (s *sqliteStore) SetActionBackends(ctx context.Context, actionID int64, assignments []model.ActionBackend) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("store: set action backends tx: %w", err)
	}
	defer tx.Rollback() //nolint:errcheck

	if _, err := tx.ExecContext(ctx,
		`DELETE FROM action_backends WHERE action_id = ?`, actionID); err != nil {
		return fmt.Errorf("store: clear action backends: %w", err)
	}
	for _, a := range assignments {
		if _, err := tx.ExecContext(ctx,
			`INSERT INTO action_backends (action_id, backend_id, priority) VALUES (?, ?, ?)`,
			actionID, a.BackendID, a.Priority); err != nil {
			return fmt.Errorf("store: insert action backend: %w", err)
		}
	}
	return tx.Commit()
}

func (s *sqliteStore) RemoveActionBackend(ctx context.Context, actionID, backendID int64) error {
	_, err := s.db.ExecContext(ctx,
		`DELETE FROM action_backends WHERE action_id = ? AND backend_id = ?`,
		actionID, backendID)
	if err != nil {
		return fmt.Errorf("store: remove action backend: %w", err)
	}
	return nil
}

func (s *sqliteStore) ListAllActionBackendIDs(ctx context.Context) (map[int64][]int64, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT action_id, backend_id FROM action_backends ORDER BY action_id, priority`)
	if err != nil {
		return nil, fmt.Errorf("store: list all action backend ids: %w", err)
	}
	defer rows.Close()
	out := make(map[int64][]int64)
	for rows.Next() {
		var actionID, backendID int64
		if err := rows.Scan(&actionID, &backendID); err != nil {
			return nil, fmt.Errorf("store: scan action backend ids: %w", err)
		}
		out[actionID] = append(out[actionID], backendID)
	}
	return out, rows.Err()
}
