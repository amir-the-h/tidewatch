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

	// Populate Rules via join
	rows, err := s.db.QueryContext(ctx,
		`SELECT r.id, r.name, r.expression, r.compiled_value, r.x_rule_id, r.active, r.created_at, r.updated_at
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
		`INSERT INTO rules (name, expression, compiled_value, x_rule_id, active, created_at, updated_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?)`,
		rule.Name,
		string(rule.Expression),
		rule.CompiledValue,
		rule.XRuleID,
		boolToInt(rule.Active),
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
		`SELECT id, name, expression, compiled_value, x_rule_id, active, created_at, updated_at FROM rules WHERE id = ?`, id)
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
		`SELECT id, name, expression, compiled_value, x_rule_id, active, created_at, updated_at FROM rules WHERE x_rule_id = ?`, xRuleID)
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
	query := `SELECT id, name, expression, compiled_value, x_rule_id, active, created_at, updated_at FROM rules`
	var args []any
	if filter.Active != nil {
		query += " WHERE active = ?"
		args = append(args, boolToInt(*filter.Active))
	}
	query += " ORDER BY id ASC"

	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("store: list rules: %w", err)
	}
	defer rows.Close()

	var rules []*model.Rule
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
		`UPDATE rules SET name = ?, expression = ?, compiled_value = ?, x_rule_id = ?, active = ?, updated_at = ? WHERE id = ?`,
		rule.Name,
		string(rule.Expression),
		rule.CompiledValue,
		rule.XRuleID,
		boolToInt(rule.Active),
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

	var notifications []*model.Notification
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
	var active int
	var createdAt, updatedAt string
	if err := s.Scan(&r.ID, &r.Name, &expression, &r.CompiledValue, &r.XRuleID, &active, &createdAt, &updatedAt); err != nil {
		return nil, err
	}
	r.Expression = json.RawMessage(expression)
	r.Active = active != 0
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
