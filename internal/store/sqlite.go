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

	if _, err := db.Exec("PRAGMA journal_mode=WAL"); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("store: pragma wal: %w", err)
	}
	if _, err := db.Exec("PRAGMA foreign_keys=ON"); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("store: pragma foreign_keys: %w", err)
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

	// Populate Sources via join
	rows, err := s.db.QueryContext(ctx,
		`SELECT s.id, s.type, s.value, s.x_rule_id, s.active, s.created_at, s.updated_at
		 FROM sources s
		 JOIN tweet_sources ts ON ts.source_id = s.id
		 WHERE ts.tweet_id = ?`, id)
	if err != nil {
		return nil, fmt.Errorf("store: get tweet: list sources: %w", err)
	}
	defer rows.Close()

	for rows.Next() {
		src, err := scanSource(rows)
		if err != nil {
			return nil, fmt.Errorf("store: get tweet: scan source: %w", err)
		}
		tweet.Sources = append(tweet.Sources, src)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("store: get tweet: sources rows: %w", err)
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

	if filter.SourceID != nil {
		query += ` JOIN tweet_sources ts ON ts.tweet_id = t.id`
		where = append(where, "ts.source_id = ?")
		args = append(args, *filter.SourceID)
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

	var tweets []*model.Tweet
	for rows.Next() {
		t, err := scanTweet(rows)
		if err != nil {
			return nil, fmt.Errorf("store: list tweets: scan: %w", err)
		}
		tweets = append(tweets, t)
	}
	return tweets, rows.Err()
}

// ── Sources ───────────────────────────────────────────────────────────────────

func (s *sqliteStore) SaveSource(ctx context.Context, source *model.Source) error {
	res, err := s.db.ExecContext(ctx,
		`INSERT INTO sources (type, value, x_rule_id, active, created_at, updated_at)
		 VALUES (?, ?, ?, ?, ?, ?)`,
		string(source.Type),
		source.Value,
		source.XRuleID,
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
		`SELECT id, type, value, x_rule_id, active, created_at, updated_at FROM sources WHERE id = ?`, id)
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
	query := `SELECT id, type, value, x_rule_id, active, created_at, updated_at FROM sources`
	var args []any
	var where []string

	if filter.Type != nil {
		where = append(where, "type = ?")
		args = append(args, string(*filter.Type))
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

	var sources []*model.Source
	for rows.Next() {
		src, err := scanSource(rows)
		if err != nil {
			return nil, fmt.Errorf("store: list sources: scan: %w", err)
		}
		sources = append(sources, src)
	}
	return sources, rows.Err()
}

func (s *sqliteStore) DeleteSource(ctx context.Context, id int64) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM sources WHERE id = ?`, id)
	if err != nil {
		return fmt.Errorf("store: delete source: %w", err)
	}
	return nil
}

func (s *sqliteStore) UpdateSourceXRuleID(ctx context.Context, id int64, xRuleID string) error {
	_, err := s.db.ExecContext(ctx,
		`UPDATE sources SET x_rule_id = ?, updated_at = ? WHERE id = ?`,
		xRuleID, time.Now().UTC().Format(time.RFC3339), id)
	if err != nil {
		return fmt.Errorf("store: update source x_rule_id: %w", err)
	}
	return nil
}

// ── TweetSources ──────────────────────────────────────────────────────────────

func (s *sqliteStore) SaveTweetSource(ctx context.Context, ts *model.TweetSource) error {
	_, err := s.db.ExecContext(ctx,
		`INSERT OR IGNORE INTO tweet_sources (tweet_id, source_id) VALUES (?, ?)`,
		ts.TweetID, ts.SourceID)
	if err != nil {
		return fmt.Errorf("store: save tweet source: %w", err)
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
	return m, nil
}

func (s *sqliteStore) ListModels(ctx context.Context) ([]*model.Model, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, name, version, path, type, active, created_at FROM models ORDER BY id ASC`)
	if err != nil {
		return nil, fmt.Errorf("store: list models: %w", err)
	}
	defer rows.Close()

	var models []*model.Model
	for rows.Next() {
		m, err := scanModel(rows)
		if err != nil {
			return nil, fmt.Errorf("store: list models: scan: %w", err)
		}
		models = append(models, m)
	}
	return models, rows.Err()
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

// ── Categorizations ───────────────────────────────────────────────────────────

func (s *sqliteStore) SaveCategorization(ctx context.Context, c *model.Categorization) error {
	res, err := s.db.ExecContext(ctx,
		`INSERT INTO categorizations (tweet_id, model_name, model_version, results, created_at)
		 VALUES (?, ?, ?, ?, ?)`,
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

	var cats []*model.Categorization
	for rows.Next() {
		c, err := scanCategorization(rows)
		if err != nil {
			return nil, fmt.Errorf("store: list categorizations: scan: %w", err)
		}
		cats = append(cats, c)
	}
	return cats, rows.Err()
}

func (s *sqliteStore) ListUncategorizedTweets(ctx context.Context, modelNames []string) ([]*model.Tweet, error) {
	var query string
	var args []any

	if len(modelNames) == 0 {
		// Tweets with zero categorization rows
		query = `SELECT t.id, t.author_id, t.author_handle, t.text, t.raw_json, t.created_at, t.fetched_at
		         FROM tweets t
		         LEFT JOIN categorizations c ON c.tweet_id = t.id
		         WHERE c.id IS NULL
		         ORDER BY t.created_at DESC`
	} else {
		// Tweets missing at least one of the specified model names
		// Use NOT IN subquery counting distinct models covered
		placeholders := strings.Repeat("?,", len(modelNames))
		placeholders = placeholders[:len(placeholders)-1] // trim trailing comma

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

	var tweets []*model.Tweet
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
	var active int
	var createdAt, updatedAt string
	if err := s.Scan(&src.ID, &src.Type, &src.Value, &src.XRuleID, &active, &createdAt, &updatedAt); err != nil {
		return nil, err
	}
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

func boolToInt(b bool) int {
	if b {
		return 1
	}
	return 0
}
