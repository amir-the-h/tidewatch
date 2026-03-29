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

// ── Actions ───────────────────────────────────────────────────────────────────

func (s *sqliteStore) SaveAction(ctx context.Context, a *model.Action) error {
	ruleIDsJSON, err := json.Marshal(a.RuleIDs)
	if err != nil {
		return fmt.Errorf("store: save action: marshal rule_ids: %w", err)
	}
	now := time.Now().UTC().Format(time.RFC3339)

	res, err := s.db.ExecContext(ctx,
		`INSERT INTO actions (name, trigger_type, pipeline_id, active, rule_ids, created_at, updated_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?)`,
		a.Name,
		string(a.TriggerType),
		a.PipelineID,
		boolToInt(a.Active),
		string(ruleIDsJSON),
		now,
		now,
	)
	if err != nil {
		return fmt.Errorf("store: save action: %w", err)
	}
	id, err := res.LastInsertId()
	if err != nil {
		return fmt.Errorf("store: save action: last insert id: %w", err)
	}
	a.ID = id
	return nil
}

func (s *sqliteStore) GetAction(ctx context.Context, id int64) (*model.Action, error) {
	row := s.db.QueryRowContext(ctx,
		`SELECT id, name, trigger_type, pipeline_id, active, rule_ids, created_at, updated_at FROM actions WHERE id = ?`, id)
	a, err := scanAction(row)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, fmt.Errorf("store: get action: not found: %w", err)
		}
		return nil, fmt.Errorf("store: get action: %w", err)
	}
	return a, nil
}

func (s *sqliteStore) ListActions(ctx context.Context, filter ActionFilter) ([]*model.Action, error) {
	var where []string
	var args []any

	if filter.TriggerType != nil {
		where = append(where, "trigger_type = ?")
		args = append(args, string(*filter.TriggerType))
	}
	if filter.PipelineID != nil {
		where = append(where, "pipeline_id = ?")
		args = append(args, *filter.PipelineID)
	}
	if filter.Active != nil {
		where = append(where, "active = ?")
		args = append(args, boolToInt(*filter.Active))
	}

	q := "SELECT id, name, trigger_type, pipeline_id, active, rule_ids, created_at, updated_at FROM actions"
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
	ruleIDsJSON, err := json.Marshal(a.RuleIDs)
	if err != nil {
		return fmt.Errorf("store: update action: marshal rule_ids: %w", err)
	}
	now := time.Now().UTC().Format(time.RFC3339)
	_, err = s.db.ExecContext(ctx,
		`UPDATE actions SET name = ?, trigger_type = ?, pipeline_id = ?, rule_ids = ?, updated_at = ? WHERE id = ?`,
		a.Name, string(a.TriggerType), a.PipelineID, string(ruleIDsJSON), now, a.ID,
	)
	if err != nil {
		return fmt.Errorf("store: update action: %w", err)
	}
	return nil
}

func (s *sqliteStore) DeleteAction(ctx context.Context, id int64) error {
	// Chains may not cascade due to migration 016 table-rename re-association;
	// delete explicitly to avoid FK constraint errors.
	if _, err := s.db.ExecContext(ctx,
		`DELETE FROM action_chains WHERE source_action_id = ? OR target_action_id = ?`, id, id); err != nil {
		return fmt.Errorf("store: delete action: delete chains: %w", err)
	}
	if _, err := s.db.ExecContext(ctx, `
		DELETE FROM action_execution_metadata
		WHERE execution_id IN (SELECT id FROM action_executions WHERE action_id = ?)`, id); err != nil {
		return fmt.Errorf("store: delete action: delete metadata: %w", err)
	}
	if _, err := s.db.ExecContext(ctx, `DELETE FROM action_executions WHERE action_id = ?`, id); err != nil {
		return fmt.Errorf("store: delete action: delete executions: %w", err)
	}
	if _, err := s.db.ExecContext(ctx, `DELETE FROM actions WHERE id = ?`, id); err != nil {
		return fmt.Errorf("store: delete action: %w", err)
	}
	return nil
}

func (s *sqliteStore) SetActionActive(ctx context.Context, id int64, active bool) error {
	now := time.Now().UTC().Format(time.RFC3339)
	_, err := s.db.ExecContext(ctx,
		`UPDATE actions SET active = ?, updated_at = ? WHERE id = ?`, boolToInt(active), now, id)
	if err != nil {
		return fmt.Errorf("store: set action active: %w", err)
	}
	return nil
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
		where = append(where, "action_id = ?")
		args = append(args, *filter.ActionID)
	}
	if filter.TweetID != nil {
		where = append(where, "tweet_id = ?")
		args = append(args, *filter.TweetID)
	}
	if filter.Status != nil {
		where = append(where, "status = ?")
		args = append(args, *filter.Status)
	}
	if filter.From != nil {
		where = append(where, "executed_at >= ?")
		args = append(args, filter.From.UTC().Format(time.RFC3339))
	}
	if filter.To != nil {
		where = append(where, "executed_at <= ?")
		args = append(args, filter.To.UTC().Format(time.RFC3339))
	}

	q := "SELECT id, action_id, tweet_id, status, COALESCE(error,''), executed_at FROM action_executions"
	if len(where) > 0 {
		q += " WHERE " + strings.Join(where, " AND ")
	}
	q += " ORDER BY executed_at DESC"

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
		if err := rows.Scan(&e.ID, &e.ActionID, &e.TweetID, &e.Status, &e.Error, &executedAt); err != nil {
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
	if len(tweetIDs) == 0 {
		return 0, nil
	}

	// Build tweet_id IN (?) placeholders
	tweetPlaceholders := strings.Repeat("?,", len(tweetIDs))
	tweetPlaceholders = tweetPlaceholders[:len(tweetPlaceholders)-1]

	args := make([]any, 0, len(tweetIDs)+len(actionIDs))
	for _, id := range tweetIDs {
		args = append(args, id)
	}

	filter := fmt.Sprintf("tweet_id IN (%s)", tweetPlaceholders)
	if len(actionIDs) > 0 {
		actionPlaceholders := strings.Repeat("?,", len(actionIDs))
		actionPlaceholders = actionPlaceholders[:len(actionPlaceholders)-1]
		filter += fmt.Sprintf(" AND action_id IN (%s)", actionPlaceholders)
		for _, id := range actionIDs {
			args = append(args, id)
		}
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return 0, fmt.Errorf("store: delete action executions: begin tx: %w", err)
	}
	defer tx.Rollback() //nolint:errcheck

	// Delete metadata first (FK constraint: metadata → executions)
	if _, err := tx.ExecContext(ctx,
		fmt.Sprintf(`DELETE FROM action_execution_metadata
		             WHERE execution_id IN (SELECT id FROM action_executions WHERE %s)`, filter),
		args...); err != nil {
		return 0, fmt.Errorf("store: delete action executions: delete metadata: %w", err)
	}

	res, err := tx.ExecContext(ctx,
		fmt.Sprintf(`DELETE FROM action_executions WHERE %s`, filter),
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

// ── helpers ───────────────────────────────────────────────────────────────────

type actionScanner interface {
	Scan(dest ...any) error
}

func scanAction(r actionScanner) (*model.Action, error) {
	a := &model.Action{}
	var activeInt int
	var ruleIDsStr, createdAt, updatedAt string
	if err := r.Scan(&a.ID, &a.Name, &a.TriggerType, &a.PipelineID, &activeInt, &ruleIDsStr, &createdAt, &updatedAt); err != nil {
		return nil, err
	}
	a.Active = activeInt != 0
	if err := json.Unmarshal([]byte(ruleIDsStr), &a.RuleIDs); err != nil || a.RuleIDs == nil {
		a.RuleIDs = []int64{}
	}
	if t, err := time.Parse(time.RFC3339, createdAt); err == nil {
		a.CreatedAt = t
	}
	if t, err := time.Parse(time.RFC3339, updatedAt); err == nil {
		a.UpdatedAt = t
	}
	return a, nil
}
