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

	// DELETE journal mode: no -wal/-shm files. WAL mode is unreliable on NFS
	// (shm requires shared-memory semantics NFS can't guarantee), and offers no
	// benefit here since we only ever have one connection open anyway.
	if _, err := db.Exec("PRAGMA journal_mode=DELETE"); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("store: pragma journal_mode: %w", err)
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
	// Checkpoint the WAL before closing so stale WAL frames don't corrupt the DB
	// on the next startup if the file is ever swapped out during recovery.
	_, _ = s.db.Exec("PRAGMA wal_checkpoint(FULL)")
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
	if filter.SourceID != nil {
		query += ` JOIN tweet_sources ts ON ts.tweet_id = t.id`
		where = append(where, "ts.source_id = ?")
		args = append(args, *filter.SourceID)
	}
	if filter.EntityID != nil {
		query += ` JOIN entity_tracks et ON et.tweet_id = t.id`
		where = append(where, "et.entity_id = ?")
		args = append(args, *filter.EntityID)
	} else if filter.HasEntityTrack {
		query += ` JOIN entity_tracks et ON et.tweet_id = t.id`
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
	if filter.SourceID != nil {
		query += ` JOIN tweet_sources ts ON ts.tweet_id = t.id`
		where = append(where, "ts.source_id = ?")
		args = append(args, *filter.SourceID)
	}
	if filter.EntityID != nil {
		query += ` JOIN entity_tracks et ON et.tweet_id = t.id`
		where = append(where, "et.entity_id = ?")
		args = append(args, *filter.EntityID)
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

	// Clean up tweet_rules (categorizations table was dropped in migration 018)
	if _, err := tx.ExecContext(ctx, `DELETE FROM tweet_rules WHERE tweet_id IN (`+placeholders+`)`, args...); err != nil {
		return 0, fmt.Errorf("store: delete tweets: clear tweet_rules: %w", err)
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
	// also cleans up tweet_rules (same as DeleteTweets).
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
	                 COALESCE(tw.text, ''), COALESCE(tw.author_handle, ''), tw.created_at, et.created_at
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
		where = append(where, "COALESCE(tw.created_at, et.created_at) >= ?")
		args = append(args, filter.From.UTC().Format(time.RFC3339))
	}
	if filter.To != nil {
		where = append(where, "COALESCE(tw.created_at, et.created_at) <= ?")
		args = append(args, filter.To.UTC().Format(time.RFC3339))
	}
	if len(where) > 0 {
		query += " WHERE " + strings.Join(where, " AND ")
	}
	query += " ORDER BY COALESCE(tw.created_at, et.created_at) DESC"

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
	needTweetJoin := filter.From != nil || filter.To != nil
	if needTweetJoin {
		query += ` LEFT JOIN tweets tw ON tw.id = et.tweet_id`
	}
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
		where = append(where, "COALESCE(tw.created_at, et.created_at) >= ?")
		args = append(args, filter.From.UTC().Format(time.RFC3339))
	}
	if filter.To != nil {
		where = append(where, "COALESCE(tw.created_at, et.created_at) <= ?")
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

// ListNERSuggestionsForEntity returns distinct NER context strings from the entity's own tracks.
func (s *sqliteStore) ListNERSuggestionsForEntity(ctx context.Context, entityID int64) ([]string, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT DISTINCT context FROM entity_tracks
		 WHERE entity_id = ? AND match_source = 'ner' AND context != ''
		 ORDER BY context`,
		entityID,
	)
	if err != nil {
		return nil, fmt.Errorf("store: list ner suggestions for entity: %w", err)
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var v string
		if err := rows.Scan(&v); err != nil {
			return nil, fmt.Errorf("store: list ner suggestions for entity: scan: %w", err)
		}
		out = append(out, v)
	}
	return out, rows.Err()
}

// SearchNERSuggestions returns distinct NER context strings across all entities whose
// type matches entityType (ignored when empty) and whose context contains name.
func (s *sqliteStore) SearchNERSuggestions(ctx context.Context, name string, entityType string) ([]string, error) {
	query := `SELECT DISTINCT et.context
	          FROM entity_tracks et
	          JOIN tracked_entities te ON te.id = et.entity_id
	          WHERE et.match_source = 'ner' AND et.context != '' AND et.context LIKE ?`
	args := []any{"%" + name + "%"}
	if entityType != "" {
		query += " AND te.type = ?"
		args = append(args, entityType)
	}
	query += " ORDER BY et.context"

	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("store: search ner suggestions: %w", err)
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var v string
		if err := rows.Scan(&v); err != nil {
			return nil, fmt.Errorf("store: search ner suggestions: scan: %w", err)
		}
		out = append(out, v)
	}
	return out, rows.Err()
}

// ── Summaries ─────────────────────────────────────────────────────────────────

func (s *sqliteStore) summaryFilterQuery(filter SummaryFilter) (string, []any) {
	var args []any
	var joins, where []string

	if filter.RuleID != nil {
		joins = append(joins, `JOIN tweet_rules tr ON tr.tweet_id = t.id`)
		where = append(where, "tr.rule_id = ?")
		args = append(args, *filter.RuleID)
	}
	if filter.SourceID != nil {
		joins = append(joins, `JOIN tweet_sources ts ON ts.tweet_id = t.id`)
		where = append(where, "ts.source_id = ?")
		args = append(args, *filter.SourceID)
	}
	if filter.EntityID != nil {
		joins = append(joins, `JOIN entity_tracks et ON et.tweet_id = t.id`)
		where = append(where, "et.entity_id = ?")
		args = append(args, *filter.EntityID)
	}
	if filter.From != nil {
		where = append(where, "t.created_at >= ?")
		args = append(args, filter.From.UTC().Format(time.RFC3339))
	}
	if filter.To != nil {
		where = append(where, "t.created_at <= ?")
		args = append(args, filter.To.UTC().Format(time.RFC3339))
	}

	clause := ""
	if len(joins) > 0 {
		clause += " " + strings.Join(joins, " ")
	}
	if len(where) > 0 {
		clause += " WHERE " + strings.Join(where, " AND ")
	}
	return clause, args
}

func (s *sqliteStore) ListSummaries(ctx context.Context, filter SummaryFilter) ([]*model.SummaryItem, error) {
	page := filter.Page
	if page < 1 {
		page = 1
	}
	perPage := filter.PerPage
	if perPage < 1 {
		perPage = 50
	}
	offset := (page - 1) * perPage

	filterClause, args := s.summaryFilterQuery(filter)
	query := `SELECT t.id, t.author_handle, t.text, t.created_at,
                     aem.value, p.name, ae.executed_at
              FROM tweets t
              JOIN action_executions ae ON ae.tweet_id = t.id AND ae.status = 'success'
              JOIN actions a ON a.id = ae.action_id
              JOIN pipelines p ON p.id = a.pipeline_id AND p.type = 'ollama'
              JOIN action_execution_metadata aem ON aem.execution_id = ae.id AND aem.key = 'summary'` +
		filterClause +
		` ORDER BY ae.executed_at DESC LIMIT ? OFFSET ?`
	args = append(args, perPage, offset)

	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("store: list summaries: %w", err)
	}
	defer rows.Close()

	var items []*model.SummaryItem
	for rows.Next() {
		var item model.SummaryItem
		var summaryJSON, createdAt, summarizedAt string
		if err := rows.Scan(
			&item.TweetID, &item.AuthorHandle, &item.TweetText, &createdAt,
			&summaryJSON, &item.ModelName, &summarizedAt,
		); err != nil {
			return nil, fmt.Errorf("store: list summaries: scan: %w", err)
		}
		if t, err := time.Parse(time.RFC3339, createdAt); err == nil {
			item.CreatedAt = t
		}
		if t, err := time.Parse(time.RFC3339, summarizedAt); err == nil {
			item.SummarizedAt = t
		}
		// summaryJSON is a JSON-encoded string stored by the worker, e.g. `"the summary text"`
		if err := json.Unmarshal([]byte(summaryJSON), &item.Summary); err != nil {
			item.Summary = summaryJSON // fallback: store raw value
		}
		items = append(items, &item)
	}
	if items == nil {
		items = []*model.SummaryItem{}
	}
	return items, rows.Err()
}

func (s *sqliteStore) CountSummaries(ctx context.Context, filter SummaryFilter) (int, error) {
	filterClause, args := s.summaryFilterQuery(filter)
	query := `SELECT COUNT(*) FROM tweets t
              JOIN action_executions ae ON ae.tweet_id = t.id AND ae.status = 'success'
              JOIN actions a ON a.id = ae.action_id
              JOIN pipelines p ON p.id = a.pipeline_id AND p.type = 'ollama' -- scopes to summarizer pipelines
              JOIN action_execution_metadata aem ON aem.execution_id = ae.id AND aem.key = 'summary'` +
		filterClause

	var count int
	if err := s.db.QueryRowContext(ctx, query, args...).Scan(&count); err != nil {
		return 0, fmt.Errorf("store: count summaries: %w", err)
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
	var tweetCreatedAt, createdAt *string
	if err := s.Scan(&t.ID, &t.EntityID, &t.EntityName, &t.TweetID, &t.MatchSource, &t.Context, &t.Summary, &t.TweetText, &t.TweetAuthorHandle, &tweetCreatedAt, &createdAt); err != nil {
		return nil, err
	}
	if tweetCreatedAt != nil {
		parsed, err := time.Parse(time.RFC3339, *tweetCreatedAt)
		if err != nil {
			return nil, fmt.Errorf("parse entity_track tweet_created_at: %w", err)
		}
		t.TweetCreatedAt = &parsed
	}
	if createdAt != nil {
		var err error
		t.CreatedAt, err = time.Parse(time.RFC3339, *createdAt)
		if err != nil {
			return nil, fmt.Errorf("parse entity_track created_at: %w", err)
		}
	}
	return &t, nil
}

// ── DigestJobs ────────────────────────────────────────────────────────────────

func (s *sqliteStore) SaveDigestJob(ctx context.Context, job *model.DigestJob) error {
	paramsJSON, err := json.Marshal(job.Params)
	if err != nil {
		return fmt.Errorf("store: save digest job: marshal params: %w", err)
	}
	_, err = s.db.ExecContext(ctx,
		`INSERT INTO digest_jobs (id, status, params, tweet_count, created_at)
		 VALUES (?, ?, ?, ?, ?)`,
		job.ID,
		string(job.Status),
		string(paramsJSON),
		job.TweetCount,
		job.CreatedAt.UTC().Format(time.RFC3339),
	)
	if err != nil {
		return fmt.Errorf("store: save digest job: %w", err)
	}
	return nil
}

func (s *sqliteStore) GetDigestJob(ctx context.Context, id string) (*model.DigestJob, error) {
	row := s.db.QueryRowContext(ctx,
		`SELECT id, status, params, result, error, tweet_count, created_at, completed_at
		 FROM digest_jobs WHERE id = ?`, id)
	job, err := scanDigestJob(row)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, fmt.Errorf("store: get digest job: not found: %w", err)
		}
		return nil, fmt.Errorf("store: get digest job: %w", err)
	}
	return job, nil
}

func (s *sqliteStore) UpdateDigestJob(ctx context.Context, job *model.DigestJob) error {
	var completedAt *string
	if job.CompletedAt != nil {
		s := job.CompletedAt.UTC().Format(time.RFC3339)
		completedAt = &s
	}
	_, err := s.db.ExecContext(ctx,
		`UPDATE digest_jobs SET status=?, result=?, error=?, tweet_count=?, completed_at=? WHERE id=?`,
		string(job.Status),
		nullString(job.Result),
		nullString(job.Error),
		job.TweetCount,
		completedAt,
		job.ID,
	)
	if err != nil {
		return fmt.Errorf("store: update digest job: %w", err)
	}
	return nil
}

func (s *sqliteStore) ListDigestJobs(ctx context.Context, limit int) ([]*model.DigestJob, error) {
	if limit <= 0 {
		limit = 20
	}
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, status, params, result, error, tweet_count, created_at, completed_at
		 FROM digest_jobs ORDER BY created_at DESC LIMIT ?`, limit)
	if err != nil {
		return nil, fmt.Errorf("store: list digest jobs: %w", err)
	}
	defer rows.Close()

	var jobs []*model.DigestJob
	for rows.Next() {
		job, err := scanDigestJob(rows)
		if err != nil {
			return nil, fmt.Errorf("store: list digest jobs: scan: %w", err)
		}
		jobs = append(jobs, job)
	}
	if jobs == nil {
		jobs = []*model.DigestJob{}
	}
	return jobs, rows.Err()
}

func (s *sqliteStore) DeleteDigestJob(ctx context.Context, id string) error {
	res, err := s.db.ExecContext(ctx, `DELETE FROM digest_jobs WHERE id = ?`, id)
	if err != nil {
		return fmt.Errorf("store: delete digest job: %w", err)
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return fmt.Errorf("store: delete digest job: not found")
	}
	return nil
}

type digestJobScanner interface {
	Scan(dest ...any) error
}

func scanDigestJob(s digestJobScanner) (*model.DigestJob, error) {
	var job model.DigestJob
	var status, paramsJSON, createdAt string
	var result, errStr, completedAt sql.NullString
	if err := s.Scan(&job.ID, &status, &paramsJSON, &result, &errStr, &job.TweetCount, &createdAt, &completedAt); err != nil {
		return nil, err
	}
	job.Status = model.DigestStatus(status)
	if result.Valid {
		job.Result = result.String
	}
	if errStr.Valid {
		job.Error = errStr.String
	}
	if err := json.Unmarshal([]byte(paramsJSON), &job.Params); err != nil {
		return nil, fmt.Errorf("parse digest job params: %w", err)
	}
	var err error
	job.CreatedAt, err = time.Parse(time.RFC3339, createdAt)
	if err != nil {
		return nil, fmt.Errorf("parse digest job created_at: %w", err)
	}
	if completedAt.Valid && completedAt.String != "" {
		t, err := time.Parse(time.RFC3339, completedAt.String)
		if err != nil {
			return nil, fmt.Errorf("parse digest job completed_at: %w", err)
		}
		job.CompletedAt = &t
	}
	return &job, nil
}

func nullString(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}

func (s *sqliteStore) SaveSearchJob(ctx context.Context, job *model.SearchJob) error {
	paramsJSON, err := json.Marshal(job.Params)
	if err != nil {
		return fmt.Errorf("store: save search job: marshal params: %w", err)
	}
	_, err = s.db.ExecContext(ctx,
		`INSERT INTO search_jobs (id, status, params, tweets_fetched, created_at)
		 VALUES (?, ?, ?, ?, ?)`,
		job.ID,
		string(job.Status),
		string(paramsJSON),
		job.TweetsFetched,
		job.CreatedAt.UTC().Format(time.RFC3339),
	)
	if err != nil {
		return fmt.Errorf("store: save search job: %w", err)
	}
	return nil
}

func (s *sqliteStore) GetSearchJob(ctx context.Context, id string) (*model.SearchJob, error) {
	row := s.db.QueryRowContext(ctx,
		`SELECT id, status, params, tweets_fetched, error, created_at, completed_at
		 FROM search_jobs WHERE id = ?`, id)
	job, err := scanSearchJob(row)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, fmt.Errorf("store: get search job: not found: %w", err)
		}
		return nil, fmt.Errorf("store: get search job: %w", err)
	}
	return job, nil
}

func (s *sqliteStore) UpdateSearchJob(ctx context.Context, job *model.SearchJob) error {
	var completedAt *string
	if job.CompletedAt != nil {
		s := job.CompletedAt.UTC().Format(time.RFC3339)
		completedAt = &s
	}
	_, err := s.db.ExecContext(ctx,
		`UPDATE search_jobs SET status=?, tweets_fetched=?, error=?, completed_at=? WHERE id=?`,
		string(job.Status),
		job.TweetsFetched,
		nullString(job.Error),
		completedAt,
		job.ID,
	)
	if err != nil {
		return fmt.Errorf("store: update search job: %w", err)
	}
	return nil
}

func (s *sqliteStore) ListSearchJobs(ctx context.Context, limit int) ([]*model.SearchJob, error) {
	if limit <= 0 {
		limit = 20
	}
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, status, params, tweets_fetched, error, created_at, completed_at
		 FROM search_jobs ORDER BY created_at DESC LIMIT ?`, limit)
	if err != nil {
		return nil, fmt.Errorf("store: list search jobs: %w", err)
	}
	defer rows.Close()

	var jobs []*model.SearchJob
	for rows.Next() {
		job, err := scanSearchJob(rows)
		if err != nil {
			return nil, fmt.Errorf("store: list search jobs: scan: %w", err)
		}
		jobs = append(jobs, job)
	}
	if jobs == nil {
		jobs = []*model.SearchJob{}
	}
	return jobs, rows.Err()
}

func (s *sqliteStore) DeleteSearchJob(ctx context.Context, id string) error {
	res, err := s.db.ExecContext(ctx, `DELETE FROM search_jobs WHERE id = ?`, id)
	if err != nil {
		return fmt.Errorf("store: delete search job: %w", err)
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return fmt.Errorf("store: delete search job: not found")
	}
	return nil
}

type searchJobScanner interface {
	Scan(dest ...any) error
}

func scanSearchJob(s searchJobScanner) (*model.SearchJob, error) {
	var job model.SearchJob
	var status, paramsJSON, createdAt string
	var errStr, completedAt sql.NullString
	if err := s.Scan(&job.ID, &status, &paramsJSON, &job.TweetsFetched, &errStr, &createdAt, &completedAt); err != nil {
		return nil, err
	}
	job.Status = model.SearchJobStatus(status)
	if errStr.Valid {
		job.Error = errStr.String
	}
	if err := json.Unmarshal([]byte(paramsJSON), &job.Params); err != nil {
		return nil, fmt.Errorf("parse search job params: %w", err)
	}
	var err error
	job.CreatedAt, err = time.Parse(time.RFC3339, createdAt)
	if err != nil {
		return nil, fmt.Errorf("parse search job created_at: %w", err)
	}
	if completedAt.Valid && completedAt.String != "" {
		t, err := time.Parse(time.RFC3339, completedAt.String)
		if err != nil {
			return nil, fmt.Errorf("parse search job completed_at: %w", err)
		}
		job.CompletedAt = &t
	}
	return &job, nil
}

// ── Users ─────────────────────────────────────────────────────────────────────

func (s *sqliteStore) SaveUser(ctx context.Context, u *model.User) error {
	res, err := s.db.ExecContext(ctx,
		`INSERT INTO users (username, password_hash, role, created_at, updated_at)
		 VALUES (?, ?, ?, ?, ?)`,
		u.Username, u.PasswordHash, u.Role,
		u.CreatedAt.UTC().Format(time.RFC3339),
		u.UpdatedAt.UTC().Format(time.RFC3339),
	)
	if err != nil {
		return fmt.Errorf("store: save user: %w", err)
	}
	id, err := res.LastInsertId()
	if err != nil {
		return fmt.Errorf("store: save user last insert id: %w", err)
	}
	u.ID = id
	return nil
}

func (s *sqliteStore) GetUser(ctx context.Context, id int64) (*model.User, error) {
	row := s.db.QueryRowContext(ctx,
		`SELECT id, username, password_hash, role, created_at, updated_at FROM users WHERE id = ?`, id)
	return scanUser(row)
}

func (s *sqliteStore) GetUserByUsername(ctx context.Context, username string) (*model.User, error) {
	row := s.db.QueryRowContext(ctx,
		`SELECT id, username, password_hash, role, created_at, updated_at FROM users WHERE username = ?`, username)
	return scanUser(row)
}

func (s *sqliteStore) ListUsers(ctx context.Context) ([]*model.User, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, username, password_hash, role, created_at, updated_at FROM users ORDER BY created_at ASC`)
	if err != nil {
		return nil, fmt.Errorf("store: list users: %w", err)
	}
	defer rows.Close()
	var out []*model.User
	for rows.Next() {
		u, err := scanUser(rows)
		if err != nil {
			return nil, fmt.Errorf("store: scan user: %w", err)
		}
		out = append(out, u)
	}
	return out, rows.Err()
}

func (s *sqliteStore) UpdateUser(ctx context.Context, u *model.User) error {
	_, err := s.db.ExecContext(ctx,
		`UPDATE users SET username = ?, password_hash = ?, role = ?, updated_at = ? WHERE id = ?`,
		u.Username, u.PasswordHash, u.Role,
		time.Now().UTC().Format(time.RFC3339),
		u.ID,
	)
	if err != nil {
		return fmt.Errorf("store: update user: %w", err)
	}
	return nil
}

func (s *sqliteStore) DeleteUser(ctx context.Context, id int64) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM users WHERE id = ?`, id)
	if err != nil {
		return fmt.Errorf("store: delete user: %w", err)
	}
	return nil
}

func (s *sqliteStore) CountAdmins(ctx context.Context) (int, error) {
	var count int
	err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM users WHERE role = 'admin'`).Scan(&count)
	if err != nil {
		return 0, fmt.Errorf("store: count admins: %w", err)
	}
	return count, nil
}

type userScanner interface {
	Scan(dest ...any) error
}

func scanUser(s userScanner) (*model.User, error) {
	var u model.User
	var createdAt, updatedAt string
	if err := s.Scan(&u.ID, &u.Username, &u.PasswordHash, &u.Role, &createdAt, &updatedAt); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}
	var err error
	u.CreatedAt, err = time.Parse(time.RFC3339, createdAt)
	if err != nil {
		return nil, fmt.Errorf("parse user created_at: %w", err)
	}
	u.UpdatedAt, err = time.Parse(time.RFC3339, updatedAt)
	if err != nil {
		return nil, fmt.Errorf("parse user updated_at: %w", err)
	}
	return &u, nil
}

// ── Map ───────────────────────────────────────────────────────────────────────

func (s *sqliteStore) ListMapPins(ctx context.Context, limit, offset int) ([]*MapPin, error) {
	query := `
        SELECT ae.tweet_id,
               COALESCE(t.created_at, ae.executed_at) AS tweet_created_at,
               p.name AS model_name,
               COALESCE(aem_desc.value, '') AS description,
               aem_loc.value AS coordinates_json,
               COALESCE(json_extract(p.config, '$.map_config.coordinate_type'), '') AS coordinate_type
        FROM action_execution_metadata aem_loc
        JOIN action_executions ae ON ae.id = aem_loc.execution_id AND ae.status = 'success'
        JOIN (
            SELECT tweet_id, action_id, MAX(id) AS latest_id
            FROM action_executions
            WHERE status = 'success'
            GROUP BY tweet_id, action_id
        ) latest ON latest.latest_id = ae.id
        JOIN actions a ON a.id = ae.action_id
        JOIN pipelines p ON p.id = a.pipeline_id
        LEFT JOIN tweets t ON t.id = ae.tweet_id
        LEFT JOIN action_execution_metadata aem_desc
               ON aem_desc.execution_id = ae.id AND aem_desc.key = 'description'
        WHERE aem_loc.key = json_extract(p.config, '$.map_config.coordinate_key')
          AND json_extract(p.config, '$.map_config.coordinate_key') IS NOT NULL
          AND json_extract(p.config, '$.map_config.coordinate_key') != ''
          AND aem_loc.value != 'null'
          AND aem_loc.value != '[]'
        ORDER BY tweet_created_at DESC
        LIMIT ? OFFSET ?`

	rows, err := s.db.QueryContext(ctx, query, limit, offset)
	if err != nil {
		return nil, fmt.Errorf("store: list map pins: %w", err)
	}
	defer rows.Close()

	var pins []*MapPin
	for rows.Next() {
		var pin MapPin
		var coordType string
		if err := rows.Scan(
			&pin.TweetID, &pin.TweetCreatedAt, &pin.ModelName,
			&pin.Description, &pin.CoordinatesJSON, &coordType,
		); err != nil {
			return nil, fmt.Errorf("store: list map pins: scan: %w", err)
		}
		pin.CoordinateType = model.CoordinateType(coordType)
		pins = append(pins, &pin)
	}
	return pins, rows.Err()
}
