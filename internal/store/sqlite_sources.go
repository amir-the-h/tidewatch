package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/amir-the-h/tidewatch/internal/model"
)

// ── Sources ───────────────────────────────────────────────────────────────────

const sourceColumns = `id, name, type, config, poll_interval_s, tier, state_affiliated, region, language,
	active, created_at, updated_at`

func (s *sqliteStore) SaveSource(ctx context.Context, src *model.Source) error {
	now := time.Now().UTC()
	if src.CreatedAt.IsZero() {
		src.CreatedAt = now
	}
	src.UpdatedAt = now
	res, err := s.db.ExecContext(ctx,
		`INSERT INTO sources (name, type, config, poll_interval_s, tier, state_affiliated, region, language,
		                      active, created_at, updated_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		src.Name, src.Type, rawOrEmptyObject(src.Config), src.PollIntervalS, src.Tier,
		boolToInt(src.StateAffiliated), src.Region, src.Language, boolToInt(src.Active),
		src.CreatedAt.Format(time.RFC3339), src.UpdatedAt.Format(time.RFC3339),
	)
	if err != nil {
		return fmt.Errorf("store: save source: %w", err)
	}
	src.ID, _ = res.LastInsertId()
	return nil
}

func (s *sqliteStore) GetSource(ctx context.Context, id int64) (*model.Source, error) {
	row := s.db.QueryRowContext(ctx, `SELECT `+sourceColumns+` FROM sources WHERE id = ?`, id)
	src, err := scanSourceRow(row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, fmt.Errorf("store: get source: %w", sql.ErrNoRows)
	}
	if err != nil {
		return nil, fmt.Errorf("store: get source: %w", err)
	}
	return src, nil
}

func (s *sqliteStore) ListSources(ctx context.Context, filter SourceFilter) ([]*model.Source, error) {
	query := `SELECT ` + sourceColumns + ` FROM sources`
	var where []string
	var args []any
	if filter.Type != "" {
		where = append(where, "type = ?")
		args = append(args, filter.Type)
	}
	if filter.Active != nil {
		where = append(where, "active = ?")
		args = append(args, boolToInt(*filter.Active))
	}
	if len(where) > 0 {
		query += " WHERE " + strings.Join(where, " AND ")
	}
	query += " ORDER BY name"

	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("store: list sources: %w", err)
	}
	defer rows.Close()
	out := make([]*model.Source, 0)
	for rows.Next() {
		src, err := scanSourceRow(rows)
		if err != nil {
			return nil, fmt.Errorf("store: list sources: scan: %w", err)
		}
		out = append(out, src)
	}
	return out, rows.Err()
}

func (s *sqliteStore) UpdateSource(ctx context.Context, src *model.Source) error {
	src.UpdatedAt = time.Now().UTC()
	res, err := s.db.ExecContext(ctx,
		`UPDATE sources SET name = ?, type = ?, config = ?, poll_interval_s = ?, tier = ?, state_affiliated = ?,
		                    region = ?, language = ?, active = ?, updated_at = ?
		 WHERE id = ?`,
		src.Name, src.Type, rawOrEmptyObject(src.Config), src.PollIntervalS, src.Tier,
		boolToInt(src.StateAffiliated), src.Region, src.Language, boolToInt(src.Active),
		src.UpdatedAt.Format(time.RFC3339), src.ID,
	)
	if err != nil {
		return fmt.Errorf("store: update source: %w", err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return fmt.Errorf("store: update source: %w", sql.ErrNoRows)
	}
	return nil
}

func (s *sqliteStore) SetSourceActive(ctx context.Context, id int64, active bool) error {
	res, err := s.db.ExecContext(ctx, `UPDATE sources SET active = ?, updated_at = ? WHERE id = ?`,
		boolToInt(active), time.Now().UTC().Format(time.RFC3339), id)
	if err != nil {
		return fmt.Errorf("store: set source active: %w", err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return fmt.Errorf("store: set source active: %w", sql.ErrNoRows)
	}
	return nil
}

func (s *sqliteStore) DeleteSource(ctx context.Context, id int64) error {
	res, err := s.db.ExecContext(ctx, `DELETE FROM sources WHERE id = ?`, id)
	if err != nil {
		return fmt.Errorf("store: delete source: %w", err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return fmt.Errorf("store: delete source: %w", sql.ErrNoRows)
	}
	return nil
}

func (s *sqliteStore) BulkDeleteSources(ctx context.Context, f BulkDeleteSourcesFilter) (int64, error) {
	query := "DELETE FROM sources"
	var where []string
	var args []any
	if len(f.IDs) > 0 {
		ph, idArgs := int64Placeholders(f.IDs)
		where = append(where, "id IN ("+ph+")")
		args = append(args, idArgs...)
	}
	if f.Active != nil {
		where = append(where, "active = ?")
		args = append(args, boolToInt(*f.Active))
	}
	if f.Type != "" {
		where = append(where, "type = ?")
		args = append(args, f.Type)
	}
	if len(where) > 0 {
		query += " WHERE " + strings.Join(where, " AND ")
	}
	res, err := s.db.ExecContext(ctx, query, args...)
	if err != nil {
		return 0, fmt.Errorf("store: bulk delete sources: %w", err)
	}
	n, _ := res.RowsAffected()
	return n, nil
}

func scanSourceRow(r scanner) (*model.Source, error) {
	var src model.Source
	var config, createdAt, updatedAt string
	var pollInterval, tier sql.NullInt64
	var stateAffiliated, active int
	if err := r.Scan(&src.ID, &src.Name, &src.Type, &config, &pollInterval, &tier, &stateAffiliated,
		&src.Region, &src.Language, &active, &createdAt, &updatedAt); err != nil {
		return nil, err
	}
	src.Config = json.RawMessage(config)
	if pollInterval.Valid {
		v := int(pollInterval.Int64)
		src.PollIntervalS = &v
	}
	if tier.Valid {
		v := int(tier.Int64)
		src.Tier = &v
	}
	src.StateAffiliated = stateAffiliated != 0
	src.Active = active != 0
	src.CreatedAt = parseDBTime(createdAt)
	src.UpdatedAt = parseDBTime(updatedAt)
	return &src, nil
}

// ── Source state ──────────────────────────────────────────────────────────────

const sourceStateColumns = `source_id, cursor, last_run_at, last_success_at, last_error, updated_at`

func (s *sqliteStore) GetSourceState(ctx context.Context, sourceID int64) (*model.SourceState, error) {
	row := s.db.QueryRowContext(ctx, `SELECT `+sourceStateColumns+` FROM source_state WHERE source_id = ?`, sourceID)
	st, err := scanSourceState(row)
	if errors.Is(err, sql.ErrNoRows) {
		return &model.SourceState{SourceID: sourceID, Cursor: json.RawMessage("{}")}, nil
	}
	if err != nil {
		return nil, fmt.Errorf("store: get source state: %w", err)
	}
	return st, nil
}

func (s *sqliteStore) ListSourceStates(ctx context.Context) ([]*model.SourceState, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT `+sourceStateColumns+` FROM source_state ORDER BY source_id`)
	if err != nil {
		return nil, fmt.Errorf("store: list source states: %w", err)
	}
	defer rows.Close()
	out := make([]*model.SourceState, 0)
	for rows.Next() {
		st, err := scanSourceState(rows)
		if err != nil {
			return nil, fmt.Errorf("store: list source states: scan: %w", err)
		}
		out = append(out, st)
	}
	return out, rows.Err()
}

func (s *sqliteStore) UpsertSourceState(ctx context.Context, st *model.SourceState) error {
	st.UpdatedAt = time.Now().UTC()
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO source_state (source_id, cursor, last_run_at, last_success_at, last_error, updated_at)
		 VALUES (?, ?, ?, ?, ?, ?)
		 ON CONFLICT (source_id) DO UPDATE SET
		     cursor = excluded.cursor, last_run_at = excluded.last_run_at,
		     last_success_at = excluded.last_success_at, last_error = excluded.last_error,
		     updated_at = excluded.updated_at`,
		st.SourceID, rawOrEmptyObject(st.Cursor), formatTimePtr(st.LastRunAt), formatTimePtr(st.LastSuccessAt),
		st.LastError, st.UpdatedAt.Format(time.RFC3339),
	)
	if err != nil {
		return fmt.Errorf("store: upsert source state: %w", err)
	}
	return nil
}

func scanSourceState(r scanner) (*model.SourceState, error) {
	var st model.SourceState
	var cursor, updatedAt string
	var lastRun, lastSuccess sql.NullString
	if err := r.Scan(&st.SourceID, &cursor, &lastRun, &lastSuccess, &st.LastError, &updatedAt); err != nil {
		return nil, err
	}
	st.Cursor = json.RawMessage(cursor)
	st.LastRunAt = parseNullTime(lastRun)
	st.LastSuccessAt = parseNullTime(lastSuccess)
	st.UpdatedAt = parseDBTime(updatedAt)
	return &st, nil
}

// ── Source runs ───────────────────────────────────────────────────────────────

const sourceRunColumns = `id, source_id, kind, params, status, fetched_count, new_count, error, created_at, completed_at`

func (s *sqliteStore) SaveSourceRun(ctx context.Context, run *model.SourceRun) error {
	if run.CreatedAt.IsZero() {
		run.CreatedAt = time.Now().UTC()
	}
	if run.Status == "" {
		run.Status = model.SourceRunPending
	}
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO source_runs (id, source_id, kind, params, status, fetched_count, new_count, error, created_at, completed_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		run.ID, run.SourceID, string(run.Kind), rawOrEmptyObject(run.Params), string(run.Status),
		run.FetchedCount, run.NewCount, nullIfEmpty(run.Error), run.CreatedAt.UTC().Format(time.RFC3339),
		formatTimePtr(run.CompletedAt),
	)
	if err != nil {
		return fmt.Errorf("store: save source run: %w", err)
	}
	return nil
}

func (s *sqliteStore) UpdateSourceRun(ctx context.Context, run *model.SourceRun) error {
	_, err := s.db.ExecContext(ctx,
		`UPDATE source_runs SET status = ?, fetched_count = ?, new_count = ?, error = ?, completed_at = ? WHERE id = ?`,
		string(run.Status), run.FetchedCount, run.NewCount, nullIfEmpty(run.Error), formatTimePtr(run.CompletedAt), run.ID,
	)
	if err != nil {
		return fmt.Errorf("store: update source run: %w", err)
	}
	return nil
}

func (s *sqliteStore) GetSourceRun(ctx context.Context, id string) (*model.SourceRun, error) {
	row := s.db.QueryRowContext(ctx, `SELECT `+sourceRunColumns+` FROM source_runs WHERE id = ?`, id)
	run, err := scanSourceRun(row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, fmt.Errorf("store: get source run: %w", sql.ErrNoRows)
	}
	if err != nil {
		return nil, fmt.Errorf("store: get source run: %w", err)
	}
	return run, nil
}

func (s *sqliteStore) ListSourceRuns(ctx context.Context, sourceID int64, limit int) ([]*model.SourceRun, error) {
	if limit <= 0 {
		limit = 50
	}
	rows, err := s.db.QueryContext(ctx,
		`SELECT `+sourceRunColumns+` FROM source_runs WHERE source_id = ? ORDER BY created_at DESC, rowid DESC LIMIT ?`,
		sourceID, limit)
	if err != nil {
		return nil, fmt.Errorf("store: list source runs: %w", err)
	}
	defer rows.Close()
	out := make([]*model.SourceRun, 0)
	for rows.Next() {
		run, err := scanSourceRun(rows)
		if err != nil {
			return nil, fmt.Errorf("store: list source runs: scan: %w", err)
		}
		out = append(out, run)
	}
	return out, rows.Err()
}

func (s *sqliteStore) FailUnfinishedSourceRuns(ctx context.Context, msg string) (int64, error) {
	res, err := s.db.ExecContext(ctx,
		`UPDATE source_runs SET status = 'error', error = ?, completed_at = ? WHERE status IN ('pending', 'running')`,
		msg, time.Now().UTC().Format(time.RFC3339))
	if err != nil {
		return 0, fmt.Errorf("store: fail unfinished source runs: %w", err)
	}
	n, _ := res.RowsAffected()
	return n, nil
}

func scanSourceRun(r scanner) (*model.SourceRun, error) {
	var run model.SourceRun
	var kind, params, status, createdAt string
	var errStr, completedAt sql.NullString
	if err := r.Scan(&run.ID, &run.SourceID, &kind, &params, &status, &run.FetchedCount, &run.NewCount,
		&errStr, &createdAt, &completedAt); err != nil {
		return nil, err
	}
	run.Kind = model.SourceRunKind(kind)
	run.Params = json.RawMessage(params)
	run.Status = model.SourceRunStatus(status)
	run.Error = errStr.String
	run.CreatedAt = parseDBTime(createdAt)
	run.CompletedAt = parseNullTime(completedAt)
	return &run, nil
}

// ── Signal sources ────────────────────────────────────────────────────────────

func (s *sqliteStore) AddSignalSource(ctx context.Context, signalID, sourceID int64) error {
	_, err := s.db.ExecContext(ctx,
		`INSERT OR IGNORE INTO signal_sources (signal_id, source_id, seen_at) VALUES (?, ?, ?)`,
		signalID, sourceID, time.Now().UTC().Format(time.RFC3339))
	if err != nil {
		return fmt.Errorf("store: add signal source: %w", err)
	}
	return nil
}

func (s *sqliteStore) ListSignalSources(ctx context.Context, signalID int64) ([]*model.SourceRef, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT src.id, src.name, src.type, src.tier, ss.seen_at
		 FROM signal_sources ss JOIN sources src ON src.id = ss.source_id
		 WHERE ss.signal_id = ?
		 ORDER BY ss.seen_at, src.id`, signalID)
	if err != nil {
		return nil, fmt.Errorf("store: list signal sources: %w", err)
	}
	defer rows.Close()
	out := make([]*model.SourceRef, 0)
	for rows.Next() {
		var ref model.SourceRef
		var tier sql.NullInt64
		var seenAt string
		if err := rows.Scan(&ref.ID, &ref.Name, &ref.Type, &tier, &seenAt); err != nil {
			return nil, fmt.Errorf("store: list signal sources: scan: %w", err)
		}
		if tier.Valid {
			v := int(tier.Int64)
			ref.Tier = &v
		}
		ref.SeenAt = parseDBTime(seenAt)
		out = append(out, &ref)
	}
	return out, rows.Err()
}

// ── helpers ───────────────────────────────────────────────────────────────────

// rawOrEmptyObject returns raw as a string, or "{}" when it is empty.
func rawOrEmptyObject(raw json.RawMessage) string {
	if len(raw) == 0 {
		return "{}"
	}
	return string(raw)
}

func nullIfEmpty(s string) any {
	if s == "" {
		return nil
	}
	return s
}

func formatTimePtr(t *time.Time) any {
	if t == nil {
		return nil
	}
	return t.UTC().Format(time.RFC3339)
}

// parseDBTime parses a timestamp written by this package (RFC 3339) or by a
// SQLite datetime('now') column default ("YYYY-MM-DD HH:MM:SS", UTC).
func parseDBTime(v string) time.Time {
	if t, err := time.Parse(time.RFC3339, v); err == nil {
		return t
	}
	if t, err := time.Parse(time.DateTime, v); err == nil {
		return t.UTC()
	}
	return time.Time{}
}

func parseNullTime(v sql.NullString) *time.Time {
	if !v.Valid || v.String == "" {
		return nil
	}
	t := parseDBTime(v.String)
	if t.IsZero() {
		return nil
	}
	return &t
}
