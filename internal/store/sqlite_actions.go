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

// ── Actions ───────────────────────────────────────────────────────────────────

func (s *sqliteStore) SaveAction(ctx context.Context, a *model.Action) error {
	cfg := a.Config
	if len(cfg) == 0 {
		cfg = []byte("{}")
	}
	now := time.Now().UTC().Format(time.RFC3339)
	res, err := s.db.ExecContext(ctx,
		`INSERT INTO actions (name, type, config, active, created_at, updated_at) VALUES (?,?,?,?,?,?)`,
		a.Name, string(a.Type), string(cfg), boolToInt(a.Active), now, now,
	)
	if err != nil {
		return fmt.Errorf("store: save action: %w", err)
	}
	id, _ := res.LastInsertId()
	a.ID = id
	return nil
}

func (s *sqliteStore) GetAction(ctx context.Context, id int64) (*model.Action, error) {
	row := s.db.QueryRowContext(ctx,
		`SELECT id, name, type, config, active, created_at, updated_at FROM actions WHERE id = ?`, id)
	a, err := scanAction(row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, fmt.Errorf("store: get action: not found: %w", err)
	}
	if err != nil {
		return nil, fmt.Errorf("store: get action: %w", err)
	}
	return a, nil
}

func (s *sqliteStore) ListActions(ctx context.Context, filter ActionFilter) ([]*model.Action, error) {
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
	q := "SELECT id, name, type, config, active, created_at, updated_at FROM actions"
	if len(where) > 0 {
		q += " WHERE " + strings.Join(where, " AND ")
	}
	q += " ORDER BY id ASC"
	rows, err := s.db.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, fmt.Errorf("store: list actions: %w", err)
	}
	defer rows.Close()
	var out []*model.Action
	for rows.Next() {
		a, err := scanAction(rows)
		if err != nil {
			return nil, fmt.Errorf("store: list actions: scan: %w", err)
		}
		out = append(out, a)
	}
	return out, rows.Err()
}

func (s *sqliteStore) UpdateAction(ctx context.Context, a *model.Action) error {
	cfg := a.Config
	if len(cfg) == 0 {
		cfg = []byte("{}")
	}
	now := time.Now().UTC().Format(time.RFC3339)
	_, err := s.db.ExecContext(ctx,
		`UPDATE actions SET name = ?, type = ?, config = ?, active = ?, updated_at = ? WHERE id = ?`,
		a.Name, string(a.Type), string(cfg), boolToInt(a.Active), now, a.ID,
	)
	if err != nil {
		return fmt.Errorf("store: update action: %w", err)
	}
	return nil
}

func (s *sqliteStore) DeleteAction(ctx context.Context, id int64) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM actions WHERE id = ?`, id)
	if err != nil {
		return fmt.Errorf("store: delete action: %w", err)
	}
	return nil
}

func (s *sqliteStore) BulkDeleteActions(ctx context.Context, f BulkDeleteActionsFilter) (int64, error) {
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
	if f.Type != "" {
		clauses = append(clauses, "type = ?")
		args = append(args, f.Type)
	}

	query := "DELETE FROM actions"
	if len(clauses) > 0 {
		query += " WHERE " + strings.Join(clauses, " AND ")
	}

	res, err := s.db.ExecContext(ctx, query, args...)
	if err != nil {
		return 0, fmt.Errorf("store: bulk delete actions: %w", err)
	}
	n, _ := res.RowsAffected()
	return n, nil
}

func (s *sqliteStore) SetActionActive(ctx context.Context, id int64, active bool) error {
	now := time.Now().UTC().Format(time.RFC3339)
	_, err := s.db.ExecContext(ctx,
		`UPDATE actions SET active = ?, updated_at = ? WHERE id = ?`,
		boolToInt(active), now, id,
	)
	if err != nil {
		return fmt.Errorf("store: set action active: %w", err)
	}
	return nil
}

// scanAction scans one row into a model.Action.
type actionScanner interface {
	Scan(dest ...any) error
}

func scanAction(row actionScanner) (*model.Action, error) {
	var a model.Action
	var cfgStr string
	var activeInt int
	var createdStr, updatedStr string
	err := row.Scan(&a.ID, &a.Name, (*string)(&a.Type), &cfgStr, &activeInt, &createdStr, &updatedStr)
	if err != nil {
		return nil, err
	}
	a.Config = json.RawMessage(cfgStr)
	a.Active = activeInt != 0
	a.CreatedAt, _ = time.Parse(time.RFC3339, createdStr)
	a.UpdatedAt, _ = time.Parse(time.RFC3339, updatedStr)
	return &a, nil
}

// ── Action Labels ─────────────────────────────────────────────────────────────

func (s *sqliteStore) ListActionLabels(ctx context.Context, actionID int64) ([]string, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT label FROM action_labels WHERE action_id = ? ORDER BY label ASC`, actionID)
	if err != nil {
		return nil, fmt.Errorf("store: list action labels: %w", err)
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var l string
		if err := rows.Scan(&l); err != nil {
			return nil, err
		}
		out = append(out, l)
	}
	return out, rows.Err()
}

func (s *sqliteStore) AddActionLabel(ctx context.Context, actionID int64, label string) error {
	_, err := s.db.ExecContext(ctx,
		`INSERT OR IGNORE INTO action_labels (action_id, label) VALUES (?, ?)`, actionID, label)
	return err
}

func (s *sqliteStore) RemoveActionLabel(ctx context.Context, actionID int64, label string) error {
	_, err := s.db.ExecContext(ctx,
		`DELETE FROM action_labels WHERE action_id = ? AND label = ?`, actionID, label)
	return err
}

// ── ActionExecutions ──────────────────────────────────────────────────────────

func (s *sqliteStore) SaveActionExecution(ctx context.Context, e *model.ActionExecution) error {
	errStr := sql.NullString{String: e.Error, Valid: e.Error != ""}
	res, err := s.db.ExecContext(ctx,
		`INSERT INTO action_executions (action_id, tweet_id, status, error, executed_at)
		 VALUES (?, ?, ?, ?, ?)`,
		e.ActionID,
		e.TweetID,
		e.Status,
		errStr,
		e.ExecutedAt.UTC().Format(time.RFC3339),
	)
	if err != nil {
		return fmt.Errorf("store: save action execution: %w", err)
	}
	id, err := res.LastInsertId()
	if err != nil {
		return fmt.Errorf("store: save action execution: last insert id: %w", err)
	}
	e.ID = id
	return nil
}

func (s *sqliteStore) ListActionExecutions(ctx context.Context, filter ExecutionFilter) ([]*model.ActionExecution, error) {
	var where []string
	var args []any

	if filter.ActionID != nil {
		where = append(where, "ae.action_id = ?")
		args = append(args, *filter.ActionID)
	}
	if filter.TweetID != nil {
		where = append(where, "ae.tweet_id = ?")
		args = append(args, *filter.TweetID)
	}
	if filter.Status != nil {
		where = append(where, "ae.status = ?")
		args = append(args, *filter.Status)
	}
	if filter.From != nil {
		where = append(where, "ae.executed_at >= ?")
		args = append(args, filter.From.UTC().Format(time.RFC3339))
	}
	if filter.To != nil {
		where = append(where, "ae.executed_at <= ?")
		args = append(args, filter.To.UTC().Format(time.RFC3339))
	}

	// NOTE: the correlated subquery picks the pipeline name associated with this execution.
	// When an action is a node in multiple pipelines that both ran for the same tweet,
	// LIMIT 1 returns one pipeline name non-deterministically. This is a cosmetic
	// display limitation — it does not affect execution correctness.
	q := `SELECT ae.id, ae.action_id, ae.tweet_id, ae.status, COALESCE(ae.error,''), ae.executed_at,
	       COALESCE((
	         SELECT p.name
	         FROM pipeline_nodes pn
	         JOIN pipeline_run_tracking prt ON prt.pipeline_id = pn.pipeline_id AND prt.tweet_id = ae.tweet_id
	         JOIN pipelines p ON p.id = pn.pipeline_id
	         WHERE pn.action_id = ae.action_id
	         LIMIT 1
	       ), '') AS pipeline_name
	FROM action_executions ae`
	if len(where) > 0 {
		q += " WHERE " + strings.Join(where, " AND ")
	}
	q += " ORDER BY ae.executed_at DESC"

	page, perPage := filter.Page, filter.PerPage
	if perPage > 0 {
		if page < 1 {
			page = 1
		}
		q += fmt.Sprintf(" LIMIT %d OFFSET %d", perPage, (page-1)*perPage)
	}

	rows, err := s.db.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, fmt.Errorf("store: list action executions: %w", err)
	}
	defer rows.Close()

	var out []*model.ActionExecution
	for rows.Next() {
		e := &model.ActionExecution{}
		var executedAt string
		if err := rows.Scan(&e.ID, &e.ActionID, &e.TweetID, &e.Status, &e.Error, &executedAt, &e.PipelineName); err != nil {
			return nil, fmt.Errorf("store: list action executions: scan: %w", err)
		}
		if t, err := time.Parse(time.RFC3339, executedAt); err == nil {
			e.ExecutedAt = t
		}
		out = append(out, e)
	}
	return out, rows.Err()
}

func (s *sqliteStore) SaveActionExecutionMetadata(ctx context.Context, execID int64, key, value string) error {
	// Skip if a previous successful execution for the same (action_id, tweet_id)
	// already produced an identical value for this key — avoids storing redundant
	// rows when a tweet is reprocessed and the model output hasn't changed.
	_, err := s.db.ExecContext(ctx, `
		INSERT OR REPLACE INTO action_execution_metadata (execution_id, key, value)
		SELECT ?, ?, ?
		WHERE NOT EXISTS (
			SELECT 1
			FROM action_execution_metadata aem
			JOIN action_executions prev ON prev.id = aem.execution_id
			JOIN action_executions cur  ON cur.id  = ?
				AND prev.action_id = cur.action_id
				AND prev.tweet_id  = cur.tweet_id
				AND prev.id       != cur.id
				AND prev.status    = 'success'
			WHERE aem.key   = ?
			  AND aem.value = ?
		)`, execID, key, value, execID, key, value)
	if err != nil {
		return fmt.Errorf("store: save action execution metadata: %w", err)
	}
	return nil
}

func (s *sqliteStore) DeleteActionExecutions(ctx context.Context, tweetIDs []string, actionIDs []int64) (int64, error) {
	args := make([]any, 0, len(tweetIDs)+len(actionIDs))
	var clauses []string

	if len(tweetIDs) > 0 {
		ph := strings.Repeat("?,", len(tweetIDs))
		clauses = append(clauses, fmt.Sprintf("tweet_id IN (%s)", ph[:len(ph)-1]))
		for _, id := range tweetIDs {
			args = append(args, id)
		}
	}
	if len(actionIDs) > 0 {
		ph := strings.Repeat("?,", len(actionIDs))
		clauses = append(clauses, fmt.Sprintf("action_id IN (%s)", ph[:len(ph)-1]))
		for _, id := range actionIDs {
			args = append(args, id)
		}
	}

	filter := ""
	if len(clauses) > 0 {
		filter = strings.Join(clauses, " AND ")
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return 0, fmt.Errorf("store: delete action executions: begin tx: %w", err)
	}
	defer tx.Rollback() //nolint:errcheck

	whereClause := ""
	if filter != "" {
		whereClause = "WHERE " + filter
	}

	// Delete metadata first (FK constraint: metadata → executions)
	if _, err := tx.ExecContext(ctx,
		fmt.Sprintf(`DELETE FROM action_execution_metadata
		             WHERE execution_id IN (SELECT id FROM action_executions %s)`, whereClause),
		args...); err != nil {
		return 0, fmt.Errorf("store: delete action executions: delete metadata: %w", err)
	}

	res, err := tx.ExecContext(ctx,
		fmt.Sprintf(`DELETE FROM action_executions %s`, whereClause),
		args...)
	if err != nil {
		return 0, fmt.Errorf("store: delete action executions: %w", err)
	}

	if err := tx.Commit(); err != nil {
		return 0, fmt.Errorf("store: delete action executions: commit: %w", err)
	}

	n, _ := res.RowsAffected()
	return n, nil
}

func (s *sqliteStore) ListActionExecutionMetadata(ctx context.Context, execID int64) (map[string]string, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT key, value FROM action_execution_metadata WHERE execution_id = ?`, execID)
	if err != nil {
		return nil, fmt.Errorf("store: list action execution metadata: %w", err)
	}
	defer rows.Close()

	out := make(map[string]string)
	for rows.Next() {
		var k, v string
		if err := rows.Scan(&k, &v); err != nil {
			return nil, fmt.Errorf("store: list action execution metadata: scan: %w", err)
		}
		out[k] = v
	}
	return out, rows.Err()
}

// MergeActionExecutionMetadata returns the latest value per key across all
// successful executions of actionID on tweetID. This gives the full picture of
// what an action knows about a tweet regardless of how many times it ran or
// which keys were deduped on a given run.
func (s *sqliteStore) MergeActionExecutionMetadata(ctx context.Context, actionID int64, tweetID string) (map[string]string, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT key, value FROM (
			SELECT aem.key, aem.value,
			       ROW_NUMBER() OVER (PARTITION BY aem.key ORDER BY ae.id DESC) AS rn
			FROM action_execution_metadata aem
			JOIN action_executions ae ON ae.id = aem.execution_id
			WHERE ae.action_id = ?
			  AND ae.tweet_id  = ?
			  AND ae.status    = 'success'
		) WHERE rn = 1`, actionID, tweetID)
	if err != nil {
		return nil, fmt.Errorf("store: merge action execution metadata: %w", err)
	}
	defer rows.Close()

	out := make(map[string]string)
	for rows.Next() {
		var k, v string
		if err := rows.Scan(&k, &v); err != nil {
			return nil, fmt.Errorf("store: merge action execution metadata: scan: %w", err)
		}
		out[k] = v
	}
	return out, rows.Err()
}

// ListTweetActionSummaries returns one summary per action that had at least one
// successful execution on tweetID, ordered by action id. Each summary contains
// the latest value per metadata key, parsed from its stored JSON form.
// Actions with no metadata rows are included with an empty Metadata map.
func (s *sqliteStore) ListTweetActionSummaries(ctx context.Context, tweetID string) ([]TweetActionSummary, error) {
	// LEFT JOIN so actions with no metadata rows still appear (key/value will be NULL).
	rows, err := s.db.QueryContext(ctx, `
		SELECT a.id, a.name, sub.key, sub.value
		FROM (
			SELECT DISTINCT ae.action_id
			FROM action_executions ae
			WHERE ae.tweet_id = ? AND ae.status = 'success'
		) acted
		JOIN actions a ON a.id = acted.action_id
		LEFT JOIN (
			SELECT aem.key, aem.value, ae.action_id,
			       ROW_NUMBER() OVER (PARTITION BY ae.action_id, aem.key ORDER BY ae.id DESC) AS rn
			FROM action_execution_metadata aem
			JOIN action_executions ae ON ae.id = aem.execution_id
			WHERE ae.tweet_id = ? AND ae.status = 'success'
		) sub ON sub.action_id = a.id AND sub.rn = 1
		ORDER BY a.id, sub.key`, tweetID, tweetID)
	if err != nil {
		return nil, fmt.Errorf("store: list tweet action summaries: %w", err)
	}
	defer rows.Close()

	var order []int64
	byID := map[int64]*TweetActionSummary{}

	for rows.Next() {
		var actionID int64
		var actionName string
		var key, value sql.NullString
		if err := rows.Scan(&actionID, &actionName, &key, &value); err != nil {
			return nil, fmt.Errorf("store: list tweet action summaries: scan: %w", err)
		}
		if _, exists := byID[actionID]; !exists {
			order = append(order, actionID)
			byID[actionID] = &TweetActionSummary{
				ActionID:   actionID,
				ActionName: actionName,
				Metadata:   map[string]any{},
			}
		}
		if !key.Valid {
			continue // action ran but produced no metadata rows
		}
		var parsed any
		if json.Unmarshal([]byte(value.String), &parsed) == nil {
			byID[actionID].Metadata[key.String] = parsed
		} else {
			byID[actionID].Metadata[key.String] = value.String
		}
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("store: list tweet action summaries: rows: %w", err)
	}

	out := make([]TweetActionSummary, len(order))
	for i, id := range order {
		out[i] = *byID[id]
	}
	return out, nil
}

// MergeAllTweetExecutionMetadata returns the latest value per key across all
// successful executions for tweetID, regardless of which action produced them.
// Keys from actions with a later execution ID win on conflict. This gives
// notification pipelines access to ONNX/Ollama outputs without requiring a chain.
func (s *sqliteStore) MergeAllTweetExecutionMetadata(ctx context.Context, tweetID string) (map[string]string, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT key, value FROM (
			SELECT aem.key, aem.value,
			       ROW_NUMBER() OVER (PARTITION BY aem.key ORDER BY ae.id DESC) AS rn
			FROM action_execution_metadata aem
			JOIN action_executions ae ON ae.id = aem.execution_id
			WHERE ae.tweet_id = ?
			  AND ae.status   = 'success'
		) WHERE rn = 1`, tweetID)
	if err != nil {
		return nil, fmt.Errorf("store: merge all tweet execution metadata: %w", err)
	}
	defer rows.Close()

	out := make(map[string]string)
	for rows.Next() {
		var k, v string
		if err := rows.Scan(&k, &v); err != nil {
			return nil, fmt.Errorf("store: merge all tweet execution metadata: scan: %w", err)
		}
		out[k] = v
	}
	return out, rows.Err()
}

// ── TwitterCredentials ────────────────────────────────────────────────────────

func (s *sqliteStore) SaveTwitterCredentials(ctx context.Context, c *model.TwitterCredentials) error {
	_, err := s.db.ExecContext(ctx,
		`INSERT OR REPLACE INTO twitter_credentials
		 (id, access_token, refresh_token, expires_at, twitter_user_id, username, updated_at)
		 VALUES (1, ?, ?, ?, ?, ?, ?)`,
		c.AccessToken,
		c.RefreshToken,
		c.ExpiresAt.UTC().Format(time.RFC3339),
		c.TwitterUserID,
		c.Username,
		c.UpdatedAt.UTC().Format(time.RFC3339),
	)
	if err != nil {
		return fmt.Errorf("store: save twitter credentials: %w", err)
	}
	return nil
}

func (s *sqliteStore) GetTwitterCredentials(ctx context.Context) (*model.TwitterCredentials, error) {
	row := s.db.QueryRowContext(ctx,
		`SELECT id, access_token, refresh_token, expires_at, twitter_user_id, username, updated_at
		 FROM twitter_credentials WHERE id = 1`)
	c := &model.TwitterCredentials{}
	var expiresAt, updatedAt string
	if err := row.Scan(&c.ID, &c.AccessToken, &c.RefreshToken, &expiresAt, &c.TwitterUserID, &c.Username, &updatedAt); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, fmt.Errorf("store: get twitter credentials: not found: %w", err)
		}
		return nil, fmt.Errorf("store: get twitter credentials: %w", err)
	}
	if t, err := time.Parse(time.RFC3339, expiresAt); err == nil {
		c.ExpiresAt = t
	}
	if t, err := time.Parse(time.RFC3339, updatedAt); err == nil {
		c.UpdatedAt = t
	}
	return c, nil
}

func (s *sqliteStore) DeleteTwitterCredentials(ctx context.Context) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM twitter_credentials WHERE id = 1`)
	if err != nil {
		return fmt.Errorf("store: delete twitter credentials: %w", err)
	}
	return nil
}

// ── Pipeline Node Executions (sub-pipeline fan-in) ────────────────────────────

func (s *sqliteStore) InsertPipelineNodeExecution(ctx context.Context, parentNodeID int64, tweetID string, leafNodeID int64) error {
	now := time.Now().UTC().Format(time.RFC3339)
	_, err := s.db.ExecContext(ctx,
		`INSERT OR IGNORE INTO pipeline_node_executions (parent_node_id, tweet_id, leaf_node_id, completed, created_at)
		 VALUES (?, ?, ?, 0, ?)`,
		parentNodeID, tweetID, leafNodeID, now,
	)
	if err != nil {
		return fmt.Errorf("store: insert pipeline node execution: %w", err)
	}
	return nil
}

func (s *sqliteStore) MarkPipelineNodeLeafComplete(ctx context.Context, parentNodeID int64, tweetID string, leafNodeID int64) (bool, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return false, fmt.Errorf("store: mark pipeline node leaf complete: begin tx: %w", err)
	}
	defer tx.Rollback() //nolint:errcheck

	_, err = tx.ExecContext(ctx,
		`UPDATE pipeline_node_executions SET completed = 1
		 WHERE parent_node_id = ? AND tweet_id = ? AND leaf_node_id = ?`,
		parentNodeID, tweetID, leafNodeID,
	)
	if err != nil {
		return false, fmt.Errorf("store: mark pipeline node leaf complete: update: %w", err)
	}

	var total, done int
	err = tx.QueryRowContext(ctx,
		`SELECT COUNT(*), COALESCE(SUM(completed), 0)
		 FROM pipeline_node_executions
		 WHERE parent_node_id = ? AND tweet_id = ?`,
		parentNodeID, tweetID,
	).Scan(&total, &done)
	if err != nil {
		return false, fmt.Errorf("store: mark pipeline node leaf complete: count: %w", err)
	}

	if err := tx.Commit(); err != nil {
		return false, fmt.Errorf("store: mark pipeline node leaf complete: commit: %w", err)
	}

	return total > 0 && total == done, nil
}

// ── Pipeline Run Tracking (pipeline_finished trigger) ─────────────────────────

func (s *sqliteStore) InitPipelineRunTracking(ctx context.Context, pipelineID int64, tweetID string, initialCount int, eventPayload []byte) error {
	now := time.Now().UTC().Format(time.RFC3339)
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO pipeline_run_tracking (pipeline_id, tweet_id, active_tasks, event_payload, created_at, completed_at)
		VALUES (?, ?, ?, ?, ?, NULL)
		ON CONFLICT (pipeline_id, tweet_id) DO UPDATE SET
		    active_tasks  = excluded.active_tasks,
		    event_payload = excluded.event_payload,
		    completed_at  = NULL,
		    created_at    = excluded.created_at`,
		pipelineID, tweetID, initialCount, eventPayload, now,
	)
	if err != nil {
		return fmt.Errorf("store: init pipeline run tracking: %w", err)
	}
	return nil
}

func (s *sqliteStore) IncrementPipelineRunTracking(ctx context.Context, pipelineID int64, tweetID string) error {
	_, err := s.db.ExecContext(ctx,
		`UPDATE pipeline_run_tracking SET active_tasks = active_tasks + 1
		 WHERE pipeline_id = ? AND tweet_id = ?`,
		pipelineID, tweetID,
	)
	if err != nil {
		return fmt.Errorf("store: increment pipeline run tracking: %w", err)
	}
	return nil
}

func (s *sqliteStore) DecrementPipelineRunTracking(ctx context.Context, pipelineID int64, tweetID string) (bool, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return false, fmt.Errorf("store: decrement pipeline run tracking: begin tx: %w", err)
	}
	defer tx.Rollback() //nolint:errcheck

	res, err := tx.ExecContext(ctx,
		`UPDATE pipeline_run_tracking SET active_tasks = active_tasks - 1
		 WHERE pipeline_id = ? AND tweet_id = ? AND active_tasks > 0`,
		pipelineID, tweetID,
	)
	if err != nil {
		return false, fmt.Errorf("store: decrement pipeline run tracking: update: %w", err)
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		// No tracking row, or already at 0 — untracked pipeline or re-entry guard.
		_ = tx.Commit()
		return false, nil
	}

	var remaining int
	if err := tx.QueryRowContext(ctx,
		`SELECT active_tasks FROM pipeline_run_tracking WHERE pipeline_id = ? AND tweet_id = ?`,
		pipelineID, tweetID,
	).Scan(&remaining); err != nil {
		return false, fmt.Errorf("store: decrement pipeline run tracking: query: %w", err)
	}

	if remaining == 0 {
		now := time.Now().UTC().Format(time.RFC3339)
		if _, err := tx.ExecContext(ctx,
			`UPDATE pipeline_run_tracking SET completed_at = ? WHERE pipeline_id = ? AND tweet_id = ?`,
			now, pipelineID, tweetID,
		); err != nil {
			return false, fmt.Errorf("store: decrement pipeline run tracking: set completed_at: %w", err)
		}
	}

	if err := tx.Commit(); err != nil {
		return false, fmt.Errorf("store: decrement pipeline run tracking: commit: %w", err)
	}
	return remaining == 0, nil
}

func (s *sqliteStore) GetPipelineRunEventPayload(ctx context.Context, pipelineID int64, tweetID string) ([]byte, error) {
	var payload []byte
	err := s.db.QueryRowContext(ctx,
		`SELECT event_payload FROM pipeline_run_tracking WHERE pipeline_id = ? AND tweet_id = ?`,
		pipelineID, tweetID,
	).Scan(&payload)
	if err != nil {
		return nil, fmt.Errorf("store: get pipeline run event payload: %w", err)
	}
	return payload, nil
}
