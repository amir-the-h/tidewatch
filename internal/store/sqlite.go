package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"strings"
	"time"

	"github.com/amir-the-h/iran-watcher/internal/model"
	_ "modernc.org/sqlite" // register "sqlite" driver
)

// sqliteStore is the SQLite-backed implementation of Store.
type sqliteStore struct {
	db *sql.DB
}

// NewSQLiteStore opens (or creates) a SQLite database at path, enables WAL mode
// and foreign keys, runs migrations, and returns a Store.
func NewSQLiteStore(path string, migrationsFS fs.FS) (Store, error) {
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, fmt.Errorf("store: open: %w", err)
	}

	// SQLite only allows one concurrent writer. Cap the pool to one connection
	// so all writes are serialized within the process without SQLITE_BUSY errors.
	db.SetMaxOpenConns(1)

	if _, err := db.Exec("PRAGMA journal_mode=WAL"); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("store: pragma wal: %w", err)
	}
	if _, err := db.Exec("PRAGMA foreign_keys=ON"); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("store: pragma foreign_keys: %w", err)
	}
	// Wait up to 5 s for a write lock before returning SQLITE_BUSY.
	// Belt-and-suspenders alongside SetMaxOpenConns(1).
	if _, err := db.Exec("PRAGMA busy_timeout=5000"); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("store: pragma busy_timeout: %w", err)
	}

	if err := RunMigrations(db, migrationsFS); err != nil {
		_ = db.Close()
		return nil, err
	}

	return &sqliteStore{db: db}, nil
}

func (s *sqliteStore) Close() error {
	return s.db.Close()
}

// ── Tweets ────────────────────────────────────────────────────────────────────

func (s *sqliteStore) SaveTweet(ctx context.Context, tweet *model.Tweet) error {
	_, err := s.db.ExecContext(ctx,
		`INSERT OR IGNORE INTO tweets (id, author_id, author_handle, text, raw_json, created_at, fetched_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?)`,
		tweet.ID,
		tweet.AuthorID,
		tweet.AuthorHandle,
		tweet.Text,
		tweet.RawJSON,
		tweet.CreatedAt.UTC().Format(time.RFC3339),
		tweet.FetchedAt.UTC().Format(time.RFC3339),
	)
	if err != nil {
		return fmt.Errorf("store: save tweet: %w", err)
	}
	return nil
}

func (s *sqliteStore) GetTweet(ctx context.Context, id string) (*model.Tweet, error) {
	row := s.db.QueryRowContext(ctx,
		`SELECT id, author_id, author_handle, text, raw_json, created_at, fetched_at FROM tweets WHERE id = ?`, id)

	tweet, err := scanTweet(row)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, fmt.Errorf("store: get tweet: not found: %w", err)
		}
		return nil, fmt.Errorf("store: get tweet: %w", err)
	}

	// Populate Rules via join
	rows, err := s.db.QueryContext(ctx,
		`SELECT r.id, r.name, r.expression, r.compiled_value, r.x_rule_id, r.active, r.local, r.created_at, r.updated_at
		 FROM rules r
		 JOIN tweet_rules tr ON tr.rule_id = r.id
		 WHERE tr.tweet_id = ?`, id)
	if err != nil {
		return nil, fmt.Errorf("store: get tweet: list rules: %w", err)
	}
	defer rows.Close()

	for rows.Next() {
		rule, err := scanRule(rows)
		if err != nil {
			return nil, fmt.Errorf("store: get tweet: scan rule: %w", err)
		}
		tweet.Rules = append(tweet.Rules, rule)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("store: get tweet: rules rows: %w", err)
	}

	return tweet, nil
}

func (s *sqliteStore) ListTweets(ctx context.Context, filter TweetFilter) ([]*model.Tweet, error) {
	page := filter.Page
	if page < 1 {
		page = 1
	}
	perPage := filter.PerPage
	if perPage < 1 {
		perPage = 50
	}
	offset := (page - 1) * perPage

	query := `SELECT DISTINCT t.id, t.author_id, t.author_handle, t.text, t.raw_json, t.created_at, t.fetched_at
	          FROM tweets t`
	var args []any
	var where []string

	if filter.RuleID != nil {
		query += ` JOIN tweet_rules tr ON tr.tweet_id = t.id`
		where = append(where, "tr.rule_id = ?")
		args = append(args, *filter.RuleID)
	}
	if filter.From != nil {
		where = append(where, "t.created_at >= ?")
		args = append(args, filter.From.UTC().Format(time.RFC3339))
	}
	if filter.To != nil {
		where = append(where, "t.created_at <= ?")
		args = append(args, filter.To.UTC().Format(time.RFC3339))
	}
	if len(where) > 0 {
		query += " WHERE " + strings.Join(where, " AND ")
	}
	query += " ORDER BY t.created_at DESC LIMIT ? OFFSET ?"
	args = append(args, perPage, offset)

	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("store: list tweets: %w", err)
	}
	defer rows.Close()

	tweets := make([]*model.Tweet, 0)
	for rows.Next() {
		t, err := scanTweet(rows)
		if err != nil {
			return nil, fmt.Errorf("store: list tweets: scan: %w", err)
		}
		tweets = append(tweets, t)
	}
	return tweets, rows.Err()
}

func (s *sqliteStore) CountTweets(ctx context.Context, filter TweetFilter) (int, error) {
	query := `SELECT COUNT(DISTINCT t.id) FROM tweets t`
	var args []any
	var where []string

	if filter.RuleID != nil {
		query += ` JOIN tweet_rules tr ON tr.tweet_id = t.id`
		where = append(where, "tr.rule_id = ?")
		args = append(args, *filter.RuleID)
	}
	if filter.From != nil {
		where = append(where, "t.created_at >= ?")
		args = append(args, filter.From.UTC().Format(time.RFC3339))
	}
	if filter.To != nil {
		where = append(where, "t.created_at <= ?")
		args = append(args, filter.To.UTC().Format(time.RFC3339))
	}
	if len(where) > 0 {
		query += " WHERE " + strings.Join(where, " AND ")
	}

	var count int
	if err := s.db.QueryRowContext(ctx, query, args...).Scan(&count); err != nil {
		return 0, fmt.Errorf("store: count tweets: %w", err)
	}
	return count, nil
}

func (s *sqliteStore) DeleteTweets(ctx context.Context, ids []string) (int64, error) {
	if len(ids) == 0 {
		return 0, nil
	}
	placeholders := strings.Repeat("?,", len(ids))
	placeholders = placeholders[:len(placeholders)-1]
	args := make([]any, len(ids))
	for i, id := range ids {
		args[i] = id
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return 0, fmt.Errorf("store: delete tweets: begin tx: %w", err)
	}
	defer tx.Rollback()

	for _, table := range []string{"categorizations", "tweet_rules"} {
		if _, err := tx.ExecContext(ctx, `DELETE FROM `+table+` WHERE tweet_id IN (`+placeholders+`)`, args...); err != nil {
			return 0, fmt.Errorf("store: delete tweets: clear %s: %w", table, err)
		}
	}
	res, err := tx.ExecContext(ctx, `DELETE FROM tweets WHERE id IN (`+placeholders+`)`, args...)
	if err != nil {
		return 0, fmt.Errorf("store: delete tweets: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return 0, fmt.Errorf("store: delete tweets: commit: %w", err)
	}
	n, _ := res.RowsAffected()
	return n, nil
}

func (s *sqliteStore) DeleteNoiseTweets(ctx context.Context) (int64, error) {
	// A "noise" tweet is one that either:
	//   (a) has no entries in tweet_rules at all, OR
	//   (b) every rule it matches has local=true
	//
	// We identify them with a single query, then delete in a transaction that
	// also cleans up categorizations and tweet_rules (same as DeleteTweets).
	const noiseQuery = `
		SELECT t.id FROM tweets t
		WHERE NOT EXISTS (
			SELECT 1 FROM tweet_rules tr
			JOIN rules r ON r.id = tr.rule_id
			WHERE tr.tweet_id = t.id AND r.local = FALSE
		)`

	rows, err := s.db.QueryContext(ctx, noiseQuery)
	if err != nil {
		return 0, fmt.Errorf("store: delete noise tweets: query: %w", err)
	}
	var ids []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			rows.Close()
			return 0, fmt.Errorf("store: delete noise tweets: scan: %w", err)
		}
		ids = append(ids, id)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return 0, fmt.Errorf("store: delete noise tweets: rows: %w", err)
	}
	if len(ids) == 0 {
		return 0, nil
	}
	return s.DeleteTweets(ctx, ids)
}

// ── Sources ───────────────────────────────────────────────────────────────────

func (s *sqliteStore) SaveSource(ctx context.Context, source *model.Source) error {
	res, err := s.db.ExecContext(ctx,
		`INSERT INTO sources (name, type, class, operator, value, active, created_at, updated_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		source.Name,
		string(source.Type),
		string(source.Class),
		source.Operator,
		source.Value,
		boolToInt(source.Active),
		source.CreatedAt.UTC().Format(time.RFC3339),
		source.UpdatedAt.UTC().Format(time.RFC3339),
	)
	if err != nil {
		return fmt.Errorf("store: save source: %w", err)
	}
	id, err := res.LastInsertId()
	if err != nil {
		return fmt.Errorf("store: save source: last insert id: %w", err)
	}
	source.ID = id
	return nil
}

func (s *sqliteStore) GetSource(ctx context.Context, id int64) (*model.Source, error) {
	row := s.db.QueryRowContext(ctx,
		`SELECT id, name, type, class, operator, value, active, created_at, updated_at FROM sources WHERE id = ?`, id)
	src, err := scanSource(row)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, fmt.Errorf("store: get source: not found: %w", err)
		}
		return nil, fmt.Errorf("store: get source: %w", err)
	}
	return src, nil
}

func (s *sqliteStore) ListSources(ctx context.Context, filter SourceFilter) ([]*model.Source, error) {
	query := `SELECT id, name, type, class, operator, value, active, created_at, updated_at FROM sources`
	var args []any
	var where []string

	if filter.Type != nil {
		where = append(where, "type = ?")
		args = append(args, string(*filter.Type))
	}
	if filter.Class != nil {
		where = append(where, "class = ?")
		args = append(args, string(*filter.Class))
	}
	if filter.Active != nil {
		where = append(where, "active = ?")
		args = append(args, boolToInt(*filter.Active))
	}
	if len(where) > 0 {
		query += " WHERE " + strings.Join(where, " AND ")
	}
	query += " ORDER BY id ASC"

	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("store: list sources: %w", err)
	}
	defer rows.Close()

	sources := make([]*model.Source, 0)
	for rows.Next() {
		src, err := scanSource(rows)
		if err != nil {
			return nil, fmt.Errorf("store: list sources: scan: %w", err)
		}
		sources = append(sources, src)
	}
	return sources, rows.Err()
}

func (s *sqliteStore) UpdateSource(ctx context.Context, source *model.Source) error {
	now := time.Now().UTC().Format(time.RFC3339)
	_, err := s.db.ExecContext(ctx,
		`UPDATE sources SET name = ?, type = ?, class = ?, operator = ?, value = ?, updated_at = ? WHERE id = ?`,
		source.Name,
		string(source.Type),
		string(source.Class),
		source.Operator,
		source.Value,
		now,
		source.ID,
	)
	if err != nil {
		return fmt.Errorf("store: update source: %w", err)
	}
	source.UpdatedAt = time.Now().UTC()
	return nil
}

func (s *sqliteStore) DeleteSource(ctx context.Context, id int64) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM sources WHERE id = ?`, id)
	if err != nil {
		return fmt.Errorf("store: delete source: %w", err)
	}
	return nil
}

func (s *sqliteStore) SetSourceActive(ctx context.Context, id int64, active bool) error {
	_, err := s.db.ExecContext(ctx,
		`UPDATE sources SET active = ?, updated_at = ? WHERE id = ?`,
		boolToInt(active), time.Now().UTC().Format(time.RFC3339), id)
	if err != nil {
		return fmt.Errorf("store: set source active: %w", err)
	}
	return nil
}

func (s *sqliteStore) ActiveRuleRefCount(ctx context.Context, sourceID int64) (int, error) {
	var count int
	err := s.db.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM rules WHERE active = 1 AND expression LIKE ?`,
		fmt.Sprintf(`%%"src":%d}%%`, sourceID),
	).Scan(&count)
	if err != nil {
		return 0, fmt.Errorf("store: active rule ref count: %w", err)
	}
	return count, nil
}

func (s *sqliteStore) RuleRefCount(ctx context.Context, sourceID int64) (int, error) {
	var count int
	err := s.db.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM rules WHERE expression LIKE ?`,
		fmt.Sprintf(`%%"src":%d}%%`, sourceID),
	).Scan(&count)
	if err != nil {
		return 0, fmt.Errorf("store: rule ref count: %w", err)
	}
	return count, nil
}

// ── Rules ─────────────────────────────────────────────────────────────────────

func (s *sqliteStore) SaveRule(ctx context.Context, rule *model.Rule) error {
	res, err := s.db.ExecContext(ctx,
		`INSERT INTO rules (name, expression, compiled_value, x_rule_id, active, local, created_at, updated_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		rule.Name,
		string(rule.Expression),
		rule.CompiledValue,
		rule.XRuleID,
		boolToInt(rule.Active),
		boolToInt(rule.Local),
		rule.CreatedAt.UTC().Format(time.RFC3339),
		rule.UpdatedAt.UTC().Format(time.RFC3339),
	)
	if err != nil {
		return fmt.Errorf("store: save rule: %w", err)
	}
	id, err := res.LastInsertId()
	if err != nil {
		return fmt.Errorf("store: save rule: last insert id: %w", err)
	}
	rule.ID = id
	return nil
}

func (s *sqliteStore) GetRule(ctx context.Context, id int64) (*model.Rule, error) {
	row := s.db.QueryRowContext(ctx,
		`SELECT id, name, expression, compiled_value, x_rule_id, active, local, created_at, updated_at FROM rules WHERE id = ?`, id)
	rule, err := scanRule(row)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, fmt.Errorf("store: get rule: not found: %w", err)
		}
		return nil, fmt.Errorf("store: get rule: %w", err)
	}
	return rule, nil
}

func (s *sqliteStore) GetRuleByXRuleID(ctx context.Context, xRuleID string) (*model.Rule, error) {
	row := s.db.QueryRowContext(ctx,
		`SELECT id, name, expression, compiled_value, x_rule_id, active, local, created_at, updated_at FROM rules WHERE x_rule_id = ?`, xRuleID)
	rule, err := scanRule(row)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, fmt.Errorf("store: get rule by x_rule_id: not found: %w", err)
		}
		return nil, fmt.Errorf("store: get rule by x_rule_id: %w", err)
	}
	return rule, nil
}

func (s *sqliteStore) ListRules(ctx context.Context, filter RuleFilter) ([]*model.Rule, error) {
	query := `SELECT id, name, expression, compiled_value, x_rule_id, active, local, created_at, updated_at FROM rules`
	var args []any
	var where []string
	if filter.Active != nil {
		where = append(where, "active = ?")
		args = append(args, boolToInt(*filter.Active))
	}
	if filter.Local != nil {
		where = append(where, "local = ?")
		args = append(args, boolToInt(*filter.Local))
	}
	if len(where) > 0 {
		query += " WHERE " + strings.Join(where, " AND ")
	}
	query += " ORDER BY id ASC"

	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("store: list rules: %w", err)
	}
	defer rows.Close()

	rules := make([]*model.Rule, 0)
	for rows.Next() {
		rule, err := scanRule(rows)
		if err != nil {
			return nil, fmt.Errorf("store: list rules: scan: %w", err)
		}
		rules = append(rules, rule)
	}
	return rules, rows.Err()
}

func (s *sqliteStore) UpdateRule(ctx context.Context, rule *model.Rule) error {
	_, err := s.db.ExecContext(ctx,
		`UPDATE rules SET name = ?, expression = ?, compiled_value = ?, x_rule_id = ?, active = ?, local = ?, updated_at = ? WHERE id = ?`,
		rule.Name,
		string(rule.Expression),
		rule.CompiledValue,
		rule.XRuleID,
		boolToInt(rule.Active),
		boolToInt(rule.Local),
		rule.UpdatedAt.UTC().Format(time.RFC3339),
		rule.ID,
	)
	if err != nil {
		return fmt.Errorf("store: update rule: %w", err)
	}
	return nil
}

func (s *sqliteStore) DeleteRule(ctx context.Context, id int64) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM rules WHERE id = ?`, id)
	if err != nil {
		return fmt.Errorf("store: delete rule: %w", err)
	}
	return nil
}

func (s *sqliteStore) SetRuleActive(ctx context.Context, id int64, active bool, xRuleID string) error {
	_, err := s.db.ExecContext(ctx,
		`UPDATE rules SET active = ?, x_rule_id = ?, updated_at = ? WHERE id = ?`,
		boolToInt(active), xRuleID, time.Now().UTC().Format(time.RFC3339), id)
	if err != nil {
		return fmt.Errorf("store: set rule active: %w", err)
	}
	return nil
}

func (s *sqliteStore) ListRulesDependingOn(ctx context.Context, ruleID int64) ([]*model.Rule, error) {
	all, err := s.ListRules(ctx, RuleFilter{})
	if err != nil {
		return nil, fmt.Errorf("store: list rules depending on %d: %w", ruleID, err)
	}
	var out []*model.Rule
	for _, r := range all {
		if r.ID == ruleID {
			continue
		}
		if exprReferencesRule(r.Expression, ruleID) {
			out = append(out, r)
		}
	}
	return out, nil
}

// exprReferencesRule reports whether the JSON expression contains a direct
// {"rule": ruleID} reference at any depth.
func exprReferencesRule(expr json.RawMessage, ruleID int64) bool {
	var node model.ExprNode
	if err := json.Unmarshal(expr, &node); err != nil {
		return false
	}
	return nodeReferencesRule(node, ruleID)
}

func nodeReferencesRule(node model.ExprNode, ruleID int64) bool {
	if node.Rule != nil && *node.Rule == ruleID {
		return true
	}
	for _, c := range node.AND {
		if nodeReferencesRule(c, ruleID) {
			return true
		}
	}
	for _, c := range node.OR {
		if nodeReferencesRule(c, ruleID) {
			return true
		}
	}
	if node.NOT != nil && nodeReferencesRule(*node.NOT, ruleID) {
		return true
	}
	return false
}

func (s *sqliteStore) TweetRuleCount(ctx context.Context, ruleID int64) (int, error) {
	var count int
	err := s.db.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM tweet_rules WHERE rule_id = ?`, ruleID).Scan(&count)
	if err != nil {
		return 0, fmt.Errorf("store: tweet rule count: %w", err)
	}
	return count, nil
}

// ── TweetRules ────────────────────────────────────────────────────────────────

func (s *sqliteStore) SaveTweetRule(ctx context.Context, tr *model.TweetRule) error {
	_, err := s.db.ExecContext(ctx,
		`INSERT OR IGNORE INTO tweet_rules (tweet_id, rule_id) VALUES (?, ?)`,
		tr.TweetID, tr.RuleID)
	if err != nil {
		return fmt.Errorf("store: save tweet rule: %w", err)
	}
	return nil
}

// ── Models ────────────────────────────────────────────────────────────────────

func (s *sqliteStore) SaveModel(ctx context.Context, m *model.Model) error {
	res, err := s.db.ExecContext(ctx,
		`INSERT INTO models (name, version, path, type, active, created_at)
		 VALUES (?, ?, ?, ?, ?, ?)`,
		m.Name, m.Version, m.Path, string(m.Type), boolToInt(m.Active),
		m.CreatedAt.UTC().Format(time.RFC3339),
	)
	if err != nil {
		return fmt.Errorf("store: save model: %w", err)
	}
	id, err := res.LastInsertId()
	if err != nil {
		return fmt.Errorf("store: save model: last insert id: %w", err)
	}
	m.ID = id
	return nil
}

func (s *sqliteStore) GetModel(ctx context.Context, id int64) (*model.Model, error) {
	row := s.db.QueryRowContext(ctx,
		`SELECT id, name, version, path, type, active, created_at FROM models WHERE id = ?`, id)
	m, err := scanModel(row)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, fmt.Errorf("store: get model: not found: %w", err)
		}
		return nil, fmt.Errorf("store: get model: %w", err)
	}
	if err := s.populateModelRuleIDs(ctx, []*model.Model{m}); err != nil {
		return nil, fmt.Errorf("store: get model: rule ids: %w", err)
	}
	return m, nil
}

func (s *sqliteStore) ListModels(ctx context.Context) ([]*model.Model, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, name, version, path, type, active, created_at FROM models ORDER BY id ASC`)
	if err != nil {
		return nil, fmt.Errorf("store: list models: %w", err)
	}
	defer rows.Close()

	models := make([]*model.Model, 0)
	for rows.Next() {
		m, err := scanModel(rows)
		if err != nil {
			return nil, fmt.Errorf("store: list models: scan: %w", err)
		}
		models = append(models, m)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if err := s.populateModelRuleIDs(ctx, models); err != nil {
		return nil, fmt.Errorf("store: list models: rule ids: %w", err)
	}
	return models, nil
}

func (s *sqliteStore) DeleteModel(ctx context.Context, id int64) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM models WHERE id = ?`, id)
	if err != nil {
		return fmt.Errorf("store: delete model: %w", err)
	}
	return nil
}

func (s *sqliteStore) SetModelActive(ctx context.Context, id int64, active bool) error {
	_, err := s.db.ExecContext(ctx,
		`UPDATE models SET active = ? WHERE id = ?`, boolToInt(active), id)
	if err != nil {
		return fmt.Errorf("store: set model active: %w", err)
	}
	return nil
}

func (s *sqliteStore) ListModelLabels(ctx context.Context, modelName string) ([]string, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT label FROM model_labels WHERE model_name = ? ORDER BY created_at ASC`, modelName)
	if err != nil {
		return nil, fmt.Errorf("store: list model labels: %w", err)
	}
	defer rows.Close()
	var labels []string
	for rows.Next() {
		var l string
		if err := rows.Scan(&l); err != nil {
			return nil, fmt.Errorf("store: list model labels: scan: %w", err)
		}
		labels = append(labels, l)
	}
	return labels, rows.Err()
}

func (s *sqliteStore) AddModelLabel(ctx context.Context, modelName, label string) error {
	_, err := s.db.ExecContext(ctx,
		`INSERT OR IGNORE INTO model_labels (model_name, label) VALUES (?, ?)`, modelName, label)
	if err != nil {
		return fmt.Errorf("store: add model label: %w", err)
	}
	return nil
}

func (s *sqliteStore) RemoveModelLabel(ctx context.Context, modelName, label string) error {
	_, err := s.db.ExecContext(ctx,
		`DELETE FROM model_labels WHERE model_name = ? AND label = ?`, modelName, label)
	if err != nil {
		return fmt.Errorf("store: remove model label: %w", err)
	}
	return nil
}

func (s *sqliteStore) AddModelRule(ctx context.Context, modelID, ruleID int64) error {
	_, err := s.db.ExecContext(ctx,
		`INSERT OR IGNORE INTO rule_models (rule_id, model_id) VALUES (?, ?)`, ruleID, modelID)
	if err != nil {
		return fmt.Errorf("store: add model rule: %w", err)
	}
	return nil
}

func (s *sqliteStore) RemoveModelRule(ctx context.Context, modelID, ruleID int64) error {
	_, err := s.db.ExecContext(ctx,
		`DELETE FROM rule_models WHERE model_id = ? AND rule_id = ?`, modelID, ruleID)
	if err != nil {
		return fmt.Errorf("store: remove model rule: %w", err)
	}
	return nil
}

func (s *sqliteStore) ListTweetRuleIDs(ctx context.Context, tweetID string) ([]int64, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT rule_id FROM tweet_rules WHERE tweet_id = ?`, tweetID)
	if err != nil {
		return nil, fmt.Errorf("store: list tweet rule ids: %w", err)
	}
	defer rows.Close()
	var ids []int64
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			return nil, fmt.Errorf("store: list tweet rule ids: scan: %w", err)
		}
		ids = append(ids, id)
	}
	return ids, rows.Err()
}

// populateModelRuleIDs fills in RuleIDs for each model from the rule_models table.
func (s *sqliteStore) populateModelRuleIDs(ctx context.Context, models []*model.Model) error {
	if len(models) == 0 {
		return nil
	}
	// Build index by model ID.
	idx := make(map[int64]*model.Model, len(models))
	placeholders := make([]string, len(models))
	args := make([]any, len(models))
	for i, m := range models {
		idx[m.ID] = m
		placeholders[i] = "?"
		args[i] = m.ID
	}
	rows, err := s.db.QueryContext(ctx,
		`SELECT model_id, rule_id FROM rule_models WHERE model_id IN (`+strings.Join(placeholders, ",")+`)`,
		args...)
	if err != nil {
		return fmt.Errorf("store: populate model rule ids: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var modelID, ruleID int64
		if err := rows.Scan(&modelID, &ruleID); err != nil {
			return fmt.Errorf("store: populate model rule ids: scan: %w", err)
		}
		if m, ok := idx[modelID]; ok {
			m.RuleIDs = append(m.RuleIDs, ruleID)
		}
	}
	return rows.Err()
}

// ── Categorizations ───────────────────────────────────────────────────────────

func (s *sqliteStore) SaveCategorization(ctx context.Context, c *model.Categorization) error {
	res, err := s.db.ExecContext(ctx,
		`INSERT INTO categorizations (tweet_id, model_name, model_version, results, created_at)
		 VALUES (?, ?, ?, ?, ?)
		 ON CONFLICT(tweet_id, model_name) DO UPDATE SET
		     model_version = excluded.model_version,
		     results       = excluded.results,
		     created_at    = excluded.created_at`,
		c.TweetID, c.ModelName, c.ModelVersion, string(c.Results),
		c.CreatedAt.UTC().Format(time.RFC3339),
	)
	if err != nil {
		return fmt.Errorf("store: save categorization: %w", err)
	}
	id, err := res.LastInsertId()
	if err != nil {
		return fmt.Errorf("store: save categorization: last insert id: %w", err)
	}
	c.ID = id
	return nil
}

func (s *sqliteStore) ListCategorizations(ctx context.Context, filter CategorizationFilter) ([]*model.Categorization, error) {
	query := `SELECT id, tweet_id, model_name, model_version, results, created_at FROM categorizations`
	var args []any
	var where []string

	if filter.TweetID != nil {
		where = append(where, "tweet_id = ?")
		args = append(args, *filter.TweetID)
	}
	if filter.ModelName != nil {
		where = append(where, "model_name = ?")
		args = append(args, *filter.ModelName)
	}
	if filter.From != nil {
		where = append(where, "created_at >= ?")
		args = append(args, filter.From.UTC().Format(time.RFC3339))
	}
	if filter.To != nil {
		where = append(where, "created_at <= ?")
		args = append(args, filter.To.UTC().Format(time.RFC3339))
	}
	if len(where) > 0 {
		query += " WHERE " + strings.Join(where, " AND ")
	}
	query += " ORDER BY id ASC"

	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("store: list categorizations: %w", err)
	}
	defer rows.Close()

	cats := make([]*model.Categorization, 0)
	for rows.Next() {
		c, err := scanCategorization(rows)
		if err != nil {
			return nil, fmt.Errorf("store: list categorizations: scan: %w", err)
		}
		cats = append(cats, c)
	}
	return cats, rows.Err()
}

// ── Notifications ─────────────────────────────────────────────────────────────

const notificationColumns = `id, name, event_type, active, filter_rule_id, filter_model_name,
	filter_model_result, pushover_priority, pushover_sound, pushover_device, pushover_title,
	pushover_url, pushover_url_title, pushover_ttl, pushover_html, pushover_retry, pushover_expire,
	message_template, created_at, updated_at`

func (s *sqliteStore) SaveNotification(ctx context.Context, n *model.Notification) error {
	var filterModelResult *string
	if n.FilterModelResult != nil {
		s := string(*n.FilterModelResult)
		filterModelResult = &s
	}
	res, err := s.db.ExecContext(ctx,
		`INSERT INTO notifications
		 (name, event_type, active, filter_rule_id, filter_model_name, filter_model_result,
		  pushover_priority, pushover_sound, pushover_device, pushover_title, pushover_url,
		  pushover_url_title, pushover_ttl, pushover_html, pushover_retry, pushover_expire,
		  message_template, created_at, updated_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		n.Name,
		n.EventType,
		boolToInt(n.Active),
		n.FilterRuleID,
		n.FilterModelName,
		filterModelResult,
		n.PushoverPriority,
		n.PushoverSound,
		n.PushoverDevice,
		n.PushoverTitle,
		n.PushoverURL,
		n.PushoverURLTitle,
		n.PushoverTTL,
		boolToInt(n.PushoverHTML),
		n.PushoverRetry,
		n.PushoverExpire,
		n.MessageTemplate,
		n.CreatedAt.UTC().Format(time.RFC3339),
		n.UpdatedAt.UTC().Format(time.RFC3339),
	)
	if err != nil {
		return fmt.Errorf("store: save notification: %w", err)
	}
	id, err := res.LastInsertId()
	if err != nil {
		return fmt.Errorf("store: save notification: last insert id: %w", err)
	}
	n.ID = id
	return nil
}

func (s *sqliteStore) GetNotification(ctx context.Context, id int64) (*model.Notification, error) {
	row := s.db.QueryRowContext(ctx,
		`SELECT `+notificationColumns+` FROM notifications WHERE id = ?`, id)
	n, err := scanNotification(row)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, fmt.Errorf("store: get notification: not found: %w", err)
		}
		return nil, fmt.Errorf("store: get notification: %w", err)
	}
	return n, nil
}

func (s *sqliteStore) ListNotifications(ctx context.Context, filter NotificationFilter) ([]*model.Notification, error) {
	query := `SELECT ` + notificationColumns + ` FROM notifications`
	var args []any
	var where []string

	if filter.EventType != nil {
		where = append(where, "event_type = ?")
		args = append(args, *filter.EventType)
	}
	if filter.Active != nil {
		where = append(where, "active = ?")
		args = append(args, boolToInt(*filter.Active))
	}
	if filter.RuleID != nil {
		where = append(where, "filter_rule_id = ?")
		args = append(args, *filter.RuleID)
	}
	if len(where) > 0 {
		query += " WHERE " + strings.Join(where, " AND ")
	}
	query += " ORDER BY id ASC"

	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("store: list notifications: %w", err)
	}
	defer rows.Close()

	notifications := make([]*model.Notification, 0)
	for rows.Next() {
		n, err := scanNotification(rows)
		if err != nil {
			return nil, fmt.Errorf("store: list notifications: scan: %w", err)
		}
		notifications = append(notifications, n)
	}
	return notifications, rows.Err()
}

func (s *sqliteStore) UpdateNotification(ctx context.Context, n *model.Notification) error {
	var filterModelResult *string
	if n.FilterModelResult != nil {
		s := string(*n.FilterModelResult)
		filterModelResult = &s
	}
	_, err := s.db.ExecContext(ctx,
		`UPDATE notifications SET
		 name = ?, event_type = ?, active = ?, filter_rule_id = ?, filter_model_name = ?,
		 filter_model_result = ?, pushover_priority = ?, pushover_sound = ?, pushover_device = ?,
		 pushover_title = ?, pushover_url = ?, pushover_url_title = ?, pushover_ttl = ?,
		 pushover_html = ?, pushover_retry = ?, pushover_expire = ?, message_template = ?,
		 updated_at = ?
		 WHERE id = ?`,
		n.Name,
		n.EventType,
		boolToInt(n.Active),
		n.FilterRuleID,
		n.FilterModelName,
		filterModelResult,
		n.PushoverPriority,
		n.PushoverSound,
		n.PushoverDevice,
		n.PushoverTitle,
		n.PushoverURL,
		n.PushoverURLTitle,
		n.PushoverTTL,
		boolToInt(n.PushoverHTML),
		n.PushoverRetry,
		n.PushoverExpire,
		n.MessageTemplate,
		n.UpdatedAt.UTC().Format(time.RFC3339),
		n.ID,
	)
	if err != nil {
		return fmt.Errorf("store: update notification: %w", err)
	}
	return nil
}

func (s *sqliteStore) DeleteNotification(ctx context.Context, id int64) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM notifications WHERE id = ?`, id)
	if err != nil {
		return fmt.Errorf("store: delete notification: %w", err)
	}
	return nil
}

func (s *sqliteStore) SetNotificationActive(ctx context.Context, id int64, active bool) error {
	_, err := s.db.ExecContext(ctx,
		`UPDATE notifications SET active = ?, updated_at = ? WHERE id = ?`,
		boolToInt(active), time.Now().UTC().Format(time.RFC3339), id)
	if err != nil {
		return fmt.Errorf("store: set notification active: %w", err)
	}
	return nil
}

func (s *sqliteStore) ListUncategorizedTweets(ctx context.Context, modelNames []string) ([]*model.Tweet, error) {
	var query string
	var args []any

	if len(modelNames) == 0 {
		query = `SELECT t.id, t.author_id, t.author_handle, t.text, t.raw_json, t.created_at, t.fetched_at
		         FROM tweets t
		         LEFT JOIN categorizations c ON c.tweet_id = t.id
		         WHERE c.id IS NULL
		         ORDER BY t.created_at DESC`
	} else {
		placeholders := strings.Repeat("?,", len(modelNames))
		placeholders = placeholders[:len(placeholders)-1]

		query = fmt.Sprintf(`
		SELECT DISTINCT t.id, t.author_id, t.author_handle, t.text, t.raw_json, t.created_at, t.fetched_at
		FROM tweets t
		WHERE (
		    SELECT COUNT(DISTINCT c.model_name)
		    FROM categorizations c
		    WHERE c.tweet_id = t.id AND c.model_name IN (%s)
		) < ?
		ORDER BY t.created_at DESC`, placeholders)

		for _, name := range modelNames {
			args = append(args, name)
		}
		args = append(args, len(modelNames))
	}

	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("store: list uncategorized tweets: %w", err)
	}
	defer rows.Close()

	tweets := make([]*model.Tweet, 0)
	for rows.Next() {
		t, err := scanTweet(rows)
		if err != nil {
			return nil, fmt.Errorf("store: list uncategorized tweets: scan: %w", err)
		}
		tweets = append(tweets, t)
	}
	return tweets, rows.Err()
}

// ── helpers ───────────────────────────────────────────────────────────────────

type scanner interface {
	Scan(dest ...any) error
}

func scanTweet(s scanner) (*model.Tweet, error) {
	var t model.Tweet
	var createdAt, fetchedAt string
	if err := s.Scan(&t.ID, &t.AuthorID, &t.AuthorHandle, &t.Text, &t.RawJSON, &createdAt, &fetchedAt); err != nil {
		return nil, err
	}
	var err error
	t.CreatedAt, err = time.Parse(time.RFC3339, createdAt)
	if err != nil {
		return nil, fmt.Errorf("parse created_at: %w", err)
	}
	t.FetchedAt, err = time.Parse(time.RFC3339, fetchedAt)
	if err != nil {
		return nil, fmt.Errorf("parse fetched_at: %w", err)
	}
	return &t, nil
}

func scanSource(s scanner) (*model.Source, error) {
	var src model.Source
	var rawType, rawClass string
	var active int
	var createdAt, updatedAt string
	if err := s.Scan(&src.ID, &src.Name, &rawType, &rawClass, &src.Operator, &src.Value, &active, &createdAt, &updatedAt); err != nil {
		return nil, err
	}
	src.Type = model.SourceType(rawType)
	src.Class = model.SourceClass(rawClass)
	src.Active = active != 0
	var err error
	src.CreatedAt, err = time.Parse(time.RFC3339, createdAt)
	if err != nil {
		return nil, fmt.Errorf("parse source created_at: %w", err)
	}
	src.UpdatedAt, err = time.Parse(time.RFC3339, updatedAt)
	if err != nil {
		return nil, fmt.Errorf("parse source updated_at: %w", err)
	}
	return &src, nil
}

func scanRule(s scanner) (*model.Rule, error) {
	var r model.Rule
	var expression string
	var active, local int
	var createdAt, updatedAt string
	if err := s.Scan(&r.ID, &r.Name, &expression, &r.CompiledValue, &r.XRuleID, &active, &local, &createdAt, &updatedAt); err != nil {
		return nil, err
	}
	r.Expression = json.RawMessage(expression)
	r.Active = active != 0
	r.Local = local != 0
	var err error
	r.CreatedAt, err = time.Parse(time.RFC3339, createdAt)
	if err != nil {
		return nil, fmt.Errorf("parse rule created_at: %w", err)
	}
	r.UpdatedAt, err = time.Parse(time.RFC3339, updatedAt)
	if err != nil {
		return nil, fmt.Errorf("parse rule updated_at: %w", err)
	}
	return &r, nil
}

func scanModel(s scanner) (*model.Model, error) {
	var m model.Model
	var active int
	var createdAt string
	if err := s.Scan(&m.ID, &m.Name, &m.Version, &m.Path, &m.Type, &active, &createdAt); err != nil {
		return nil, err
	}
	m.Active = active != 0
	var err error
	m.CreatedAt, err = time.Parse(time.RFC3339, createdAt)
	if err != nil {
		return nil, fmt.Errorf("parse model created_at: %w", err)
	}
	return &m, nil
}

func scanCategorization(s scanner) (*model.Categorization, error) {
	var c model.Categorization
	var results, createdAt string
	if err := s.Scan(&c.ID, &c.TweetID, &c.ModelName, &c.ModelVersion, &results, &createdAt); err != nil {
		return nil, err
	}
	c.Results = json.RawMessage(results)
	var err error
	c.CreatedAt, err = time.Parse(time.RFC3339, createdAt)
	if err != nil {
		return nil, fmt.Errorf("parse categorization created_at: %w", err)
	}
	return &c, nil
}

func scanNotification(s scanner) (*model.Notification, error) {
	var n model.Notification
	var active, html int
	var filterRuleID sql.NullInt64
	var filterModelName, filterModelResult sql.NullString
	var pushoverSound, pushoverDevice, pushoverTitle, pushoverURL, pushoverURLTitle sql.NullString
	var pushoverTTL, pushoverRetry, pushoverExpire sql.NullInt64
	var messageTemplate sql.NullString
	var createdAt, updatedAt string

	if err := s.Scan(
		&n.ID, &n.Name, &n.EventType, &active,
		&filterRuleID, &filterModelName, &filterModelResult,
		&n.PushoverPriority, &pushoverSound, &pushoverDevice, &pushoverTitle,
		&pushoverURL, &pushoverURLTitle, &pushoverTTL, &html,
		&pushoverRetry, &pushoverExpire, &messageTemplate,
		&createdAt, &updatedAt,
	); err != nil {
		return nil, err
	}

	n.Active = active != 0
	n.PushoverHTML = html != 0

	if filterRuleID.Valid {
		n.FilterRuleID = &filterRuleID.Int64
	}
	if filterModelName.Valid {
		n.FilterModelName = &filterModelName.String
	}
	if filterModelResult.Valid {
		raw := json.RawMessage(filterModelResult.String)
		n.FilterModelResult = &raw
	}
	if pushoverSound.Valid {
		n.PushoverSound = &pushoverSound.String
	}
	if pushoverDevice.Valid {
		n.PushoverDevice = &pushoverDevice.String
	}
	if pushoverTitle.Valid {
		n.PushoverTitle = &pushoverTitle.String
	}
	if pushoverURL.Valid {
		n.PushoverURL = &pushoverURL.String
	}
	if pushoverURLTitle.Valid {
		n.PushoverURLTitle = &pushoverURLTitle.String
	}
	if pushoverTTL.Valid {
		v := int(pushoverTTL.Int64)
		n.PushoverTTL = &v
	}
	if pushoverRetry.Valid {
		v := int(pushoverRetry.Int64)
		n.PushoverRetry = &v
	}
	if pushoverExpire.Valid {
		v := int(pushoverExpire.Int64)
		n.PushoverExpire = &v
	}
	if messageTemplate.Valid {
		n.MessageTemplate = &messageTemplate.String
	}

	var err error
	n.CreatedAt, err = time.Parse(time.RFC3339, createdAt)
	if err != nil {
		return nil, fmt.Errorf("parse notification created_at: %w", err)
	}
	n.UpdatedAt, err = time.Parse(time.RFC3339, updatedAt)
	if err != nil {
		return nil, fmt.Errorf("parse notification updated_at: %w", err)
	}
	return &n, nil
}

func boolToInt(b bool) int {
	if b {
		return 1
	}
	return 0
}

// ── TrackedEntities ───────────────────────────────────────────────────────────

func (s *sqliteStore) SaveTrackedEntity(ctx context.Context, e *model.TrackedEntity) error {
	aliasesJSON, err := json.Marshal(e.Aliases)
	if err != nil {
		return fmt.Errorf("store: save tracked entity: marshal aliases: %w", err)
	}
	now := time.Now().UTC().Format(time.RFC3339)
	res, err := s.db.ExecContext(ctx,
		`INSERT INTO tracked_entities (name, type, aliases, notes, active, created_at, updated_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?)`,
		e.Name, e.Type, string(aliasesJSON), e.Notes, boolToInt(e.Active), now, now,
	)
	if err != nil {
		return fmt.Errorf("store: save tracked entity: %w", err)
	}
	id, err := res.LastInsertId()
	if err != nil {
		return fmt.Errorf("store: save tracked entity: last insert id: %w", err)
	}
	e.ID = id
	return nil
}

func (s *sqliteStore) GetTrackedEntity(ctx context.Context, id int64) (*model.TrackedEntity, error) {
	row := s.db.QueryRowContext(ctx,
		`SELECT te.id, te.name, te.type, te.aliases, te.notes, te.active, te.created_at, te.updated_at,
		        COUNT(et.id) AS track_count
		 FROM tracked_entities te
		 LEFT JOIN entity_tracks et ON et.entity_id = te.id
		 WHERE te.id = ?
		 GROUP BY te.id`, id)
	e, err := scanTrackedEntity(row)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, fmt.Errorf("store: get tracked entity: not found: %w", err)
		}
		return nil, fmt.Errorf("store: get tracked entity: %w", err)
	}
	return e, nil
}

func (s *sqliteStore) ListTrackedEntities(ctx context.Context, filter TrackedEntityFilter) ([]*model.TrackedEntity, error) {
	query := `SELECT te.id, te.name, te.type, te.aliases, te.notes, te.active, te.created_at, te.updated_at,
	                 COUNT(et.id) AS track_count
	          FROM tracked_entities te
	          LEFT JOIN entity_tracks et ON et.entity_id = te.id`
	var args []any
	var where []string

	if filter.Type != nil {
		where = append(where, "te.type = ?")
		args = append(args, *filter.Type)
	}
	if filter.Active != nil {
		where = append(where, "te.active = ?")
		args = append(args, boolToInt(*filter.Active))
	}
	if len(where) > 0 {
		query += " WHERE " + strings.Join(where, " AND ")
	}
	query += " GROUP BY te.id ORDER BY te.name ASC"

	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("store: list tracked entities: %w", err)
	}
	defer rows.Close()

	entities := make([]*model.TrackedEntity, 0)
	for rows.Next() {
		e, err := scanTrackedEntity(rows)
		if err != nil {
			return nil, fmt.Errorf("store: list tracked entities: scan: %w", err)
		}
		entities = append(entities, e)
	}
	return entities, rows.Err()
}

func (s *sqliteStore) ListActiveTrackedEntities(ctx context.Context) ([]*model.TrackedEntity, error) {
	active := true
	return s.ListTrackedEntities(ctx, TrackedEntityFilter{Active: &active})
}

func (s *sqliteStore) UpdateTrackedEntity(ctx context.Context, e *model.TrackedEntity) error {
	aliasesJSON, err := json.Marshal(e.Aliases)
	if err != nil {
		return fmt.Errorf("store: update tracked entity: marshal aliases: %w", err)
	}
	_, err = s.db.ExecContext(ctx,
		`UPDATE tracked_entities SET name=?, type=?, aliases=?, notes=?, updated_at=? WHERE id=?`,
		e.Name, e.Type, string(aliasesJSON), e.Notes,
		time.Now().UTC().Format(time.RFC3339), e.ID,
	)
	if err != nil {
		return fmt.Errorf("store: update tracked entity: %w", err)
	}
	return nil
}

func (s *sqliteStore) DeleteTrackedEntity(ctx context.Context, id int64) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM tracked_entities WHERE id = ?`, id)
	if err != nil {
		return fmt.Errorf("store: delete tracked entity: %w", err)
	}
	return nil
}

func (s *sqliteStore) SetTrackedEntityActive(ctx context.Context, id int64, active bool) error {
	_, err := s.db.ExecContext(ctx,
		`UPDATE tracked_entities SET active=?, updated_at=? WHERE id=?`,
		boolToInt(active), time.Now().UTC().Format(time.RFC3339), id,
	)
	if err != nil {
		return fmt.Errorf("store: set tracked entity active: %w", err)
	}
	return nil
}

// ── EntityTracks ──────────────────────────────────────────────────────────────

// SaveEntityTrack inserts a track record. Duplicate (entity_id, tweet_id) pairs
// are silently ignored via INSERT OR IGNORE so callers can always call this safely.
func (s *sqliteStore) SaveEntityTrack(ctx context.Context, t *model.EntityTrack) error {
	res, err := s.db.ExecContext(ctx,
		`INSERT INTO entity_tracks (entity_id, tweet_id, match_source, context, summary, created_at)
		 VALUES (?, ?, ?, ?, ?, ?)
		 ON CONFLICT(entity_id, tweet_id) DO UPDATE SET
		     match_source = excluded.match_source,
		     context      = excluded.context,
		     summary      = excluded.summary`,
		t.EntityID, t.TweetID, t.MatchSource, t.Context, t.Summary,
		time.Now().UTC().Format(time.RFC3339),
	)
	if err != nil {
		return fmt.Errorf("store: save entity track: %w", err)
	}
	id, _ := res.LastInsertId()
	t.ID = id
	return nil
}

func (s *sqliteStore) ListEntityTracks(ctx context.Context, filter EntityTrackFilter) ([]*model.EntityTrack, error) {
	query := `SELECT et.id, et.entity_id, te.name, et.tweet_id, et.match_source, et.context, et.summary,
	                 COALESCE(tw.text, ''), COALESCE(tw.author_handle, ''), et.created_at
	          FROM entity_tracks et
	          JOIN tracked_entities te ON te.id = et.entity_id
	          LEFT JOIN tweets tw ON tw.id = et.tweet_id`
	var args []any
	var where []string

	if filter.EntityID != nil {
		where = append(where, "et.entity_id = ?")
		args = append(args, *filter.EntityID)
	}
	if filter.TweetID != nil {
		where = append(where, "et.tweet_id = ?")
		args = append(args, *filter.TweetID)
	}
	if filter.From != nil {
		where = append(where, "et.created_at >= ?")
		args = append(args, filter.From.UTC().Format(time.RFC3339))
	}
	if filter.To != nil {
		where = append(where, "et.created_at <= ?")
		args = append(args, filter.To.UTC().Format(time.RFC3339))
	}
	if len(where) > 0 {
		query += " WHERE " + strings.Join(where, " AND ")
	}
	query += " ORDER BY et.created_at DESC"

	if filter.PerPage > 0 {
		query += fmt.Sprintf(" LIMIT %d OFFSET %d", filter.PerPage, (filter.Page-1)*filter.PerPage)
	}

	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("store: list entity tracks: %w", err)
	}
	defer rows.Close()

	tracks := make([]*model.EntityTrack, 0)
	for rows.Next() {
		t, err := scanEntityTrack(rows)
		if err != nil {
			return nil, fmt.Errorf("store: list entity tracks: scan: %w", err)
		}
		tracks = append(tracks, t)
	}
	return tracks, rows.Err()
}

func (s *sqliteStore) CountEntityTracks(ctx context.Context, filter EntityTrackFilter) (int, error) {
	query := `SELECT COUNT(*) FROM entity_tracks et`
	var args []any
	var where []string

	if filter.EntityID != nil {
		where = append(where, "et.entity_id = ?")
		args = append(args, *filter.EntityID)
	}
	if filter.TweetID != nil {
		where = append(where, "et.tweet_id = ?")
		args = append(args, *filter.TweetID)
	}
	if filter.From != nil {
		where = append(where, "et.created_at >= ?")
		args = append(args, filter.From.UTC().Format(time.RFC3339))
	}
	if filter.To != nil {
		where = append(where, "et.created_at <= ?")
		args = append(args, filter.To.UTC().Format(time.RFC3339))
	}
	if len(where) > 0 {
		query += " WHERE " + strings.Join(where, " AND ")
	}

	var count int
	err := s.db.QueryRowContext(ctx, query, args...).Scan(&count)
	if err != nil {
		return 0, fmt.Errorf("store: count entity tracks: %w", err)
	}
	return count, nil
}

func scanTrackedEntity(s scanner) (*model.TrackedEntity, error) {
	var e model.TrackedEntity
	var active int
	var aliasesRaw string
	var createdAt, updatedAt string
	if err := s.Scan(&e.ID, &e.Name, &e.Type, &aliasesRaw, &e.Notes, &active, &createdAt, &updatedAt, &e.TrackCount); err != nil {
		return nil, err
	}
	e.Active = active != 0
	if err := json.Unmarshal([]byte(aliasesRaw), &e.Aliases); err != nil {
		e.Aliases = []string{}
	}
	var err error
	e.CreatedAt, err = time.Parse(time.RFC3339, createdAt)
	if err != nil {
		return nil, fmt.Errorf("parse tracked_entity created_at: %w", err)
	}
	e.UpdatedAt, err = time.Parse(time.RFC3339, updatedAt)
	if err != nil {
		return nil, fmt.Errorf("parse tracked_entity updated_at: %w", err)
	}
	return &e, nil
}

func scanEntityTrack(s scanner) (*model.EntityTrack, error) {
	var t model.EntityTrack
	var createdAt string
	if err := s.Scan(&t.ID, &t.EntityID, &t.EntityName, &t.TweetID, &t.MatchSource, &t.Context, &t.Summary, &t.TweetText, &t.TweetAuthorHandle, &createdAt); err != nil {
		return nil, err
	}
	var err error
	t.CreatedAt, err = time.Parse(time.RFC3339, createdAt)
	if err != nil {
		return nil, fmt.Errorf("parse entity_track created_at: %w", err)
	}
	return &t, nil
}
