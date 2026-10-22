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

	"github.com/amir-the-h/tidewatch/internal/model"
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

// ── Signals ───────────────────────────────────────────────────────────────────

const signalColumns = `s.id, s.kind, s.ext_ns, s.external_id, s.author, s.author_id, s.title, s.text,
	s.url, s.lang, s.lat, s.lon, s.attrs, s.raw_json, s.published_at, s.fetched_at`

func (s *sqliteStore) SaveSignal(ctx context.Context, sig *model.Signal) (bool, error) {
	attrs := sig.Attrs
	if attrs == nil {
		attrs = map[string]any{}
	}
	attrsJSON, err := json.Marshal(attrs)
	if err != nil {
		return false, fmt.Errorf("store: save signal: marshal attrs: %w", err)
	}
	fetchedAt := sig.FetchedAt
	if fetchedAt.IsZero() {
		fetchedAt = time.Now()
	}

	res, err := s.db.ExecContext(ctx,
		`INSERT INTO signals (kind, ext_ns, external_id, author, author_id, title, text, url, lang,
		                      lat, lon, attrs, raw_json, published_at, fetched_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		 ON CONFLICT (ext_ns, external_id) DO NOTHING`,
		string(sig.Kind), sig.ExtNS, sig.ExternalID, sig.Author, sig.AuthorID, sig.Title, sig.Text,
		sig.URL, sig.Lang, sig.Lat, sig.Lon, string(attrsJSON), sig.RawJSON,
		sig.PublishedAt.UTC().Format(time.RFC3339),
		fetchedAt.UTC().Format(time.RFC3339),
	)
	if err != nil {
		return false, fmt.Errorf("store: save signal: %w", err)
	}
	n, _ := res.RowsAffected()
	if err := s.db.QueryRowContext(ctx,
		`SELECT id FROM signals WHERE ext_ns = ? AND external_id = ?`, sig.ExtNS, sig.ExternalID,
	).Scan(&sig.ID); err != nil {
		return false, fmt.Errorf("store: save signal: read id: %w", err)
	}
	return n > 0, nil
}

func (s *sqliteStore) GetSignal(ctx context.Context, id int64) (*model.Signal, error) {
	row := s.db.QueryRowContext(ctx, `SELECT `+signalColumns+` FROM signals s WHERE s.id = ?`, id)

	sig, err := scanSignal(row)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, fmt.Errorf("store: get signal: not found: %w", err)
		}
		return nil, fmt.Errorf("store: get signal: %w", err)
	}

	// Populate XRules via join
	rows, err := s.db.QueryContext(ctx,
		`SELECT r.id, r.name, r.expression, r.compiled_value, r.x_rule_id, r.active, r.local, r.created_at, r.updated_at
		 FROM x_rules r
		 JOIN x_rule_matches m ON m.rule_id = r.id
		 WHERE m.signal_id = ?`, id)
	if err != nil {
		return nil, fmt.Errorf("store: get signal: list rules: %w", err)
	}
	defer rows.Close()

	for rows.Next() {
		rule, err := scanXRule(rows)
		if err != nil {
			return nil, fmt.Errorf("store: get signal: scan rule: %w", err)
		}
		sig.XRules = append(sig.XRules, rule)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("store: get signal: rules rows: %w", err)
	}
	rows.Close()

	if sig.Sources, err = s.ListSignalSources(ctx, id); err != nil {
		return nil, fmt.Errorf("store: get signal: %w", err)
	}
	return sig, nil
}

// signalFilterSQL returns the JOIN and WHERE clauses (with args) for filter.
func signalFilterSQL(filter SignalFilter) (joins string, where []string, args []any) {
	if filter.RuleID != nil {
		joins += ` JOIN x_rule_matches m ON m.signal_id = s.id`
		where = append(where, "m.rule_id = ?")
		args = append(args, *filter.RuleID)
	}
	if filter.SourceID != nil || filter.SourceType != "" {
		joins += ` JOIN signal_sources ss ON ss.signal_id = s.id`
		if filter.SourceID != nil {
			where = append(where, "ss.source_id = ?")
			args = append(args, *filter.SourceID)
		}
		if filter.SourceType != "" {
			joins += ` JOIN sources src ON src.id = ss.source_id`
			where = append(where, "src.type = ?")
			args = append(args, filter.SourceType)
		}
	}
	if filter.EntityID != nil {
		joins += ` JOIN entity_tracks et ON et.signal_id = s.id`
		where = append(where, "et.entity_id = ?")
		args = append(args, *filter.EntityID)
	} else if filter.HasEntityTrack {
		joins += ` JOIN entity_tracks et ON et.signal_id = s.id`
	}
	if filter.Kind != nil {
		where = append(where, "s.kind = ?")
		args = append(args, string(*filter.Kind))
	}
	if filter.From != nil {
		where = append(where, "s.published_at >= ?")
		args = append(args, filter.From.UTC().Format(time.RFC3339))
	}
	if filter.To != nil {
		where = append(where, "s.published_at <= ?")
		args = append(args, filter.To.UTC().Format(time.RFC3339))
	}
	return joins, where, args
}

func (s *sqliteStore) ListSignals(ctx context.Context, filter SignalFilter) ([]*model.Signal, error) {
	page := filter.Page
	if page < 1 {
		page = 1
	}
	perPage := filter.PerPage
	if perPage < 1 {
		perPage = 50
	}
	offset := (page - 1) * perPage

	joins, where, args := signalFilterSQL(filter)
	query := `SELECT DISTINCT ` + signalColumns + ` FROM signals s` + joins
	if len(where) > 0 {
		query += " WHERE " + strings.Join(where, " AND ")
	}
	if filter.OrderAsc {
		query += " ORDER BY s.published_at ASC LIMIT ? OFFSET ?"
	} else {
		query += " ORDER BY s.published_at DESC LIMIT ? OFFSET ?"
	}
	args = append(args, perPage, offset)

	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("store: list signals: %w", err)
	}
	defer rows.Close()

	signals := make([]*model.Signal, 0)
	for rows.Next() {
		sig, err := scanSignal(rows)
		if err != nil {
			return nil, fmt.Errorf("store: list signals: scan: %w", err)
		}
		signals = append(signals, sig)
	}
	return signals, rows.Err()
}

func (s *sqliteStore) CountSignals(ctx context.Context, filter SignalFilter) (int, error) {
	joins, where, args := signalFilterSQL(filter)
	query := `SELECT COUNT(DISTINCT s.id) FROM signals s` + joins
	if len(where) > 0 {
		query += " WHERE " + strings.Join(where, " AND ")
	}

	var count int
	if err := s.db.QueryRowContext(ctx, query, args...).Scan(&count); err != nil {
		return 0, fmt.Errorf("store: count signals: %w", err)
	}
	return count, nil
}

func (s *sqliteStore) DeleteSignals(ctx context.Context, ids []int64) (int64, error) {
	if len(ids) == 0 {
		return 0, nil
	}
	placeholders, args := int64Placeholders(ids)
	res, err := s.db.ExecContext(ctx, `DELETE FROM signals WHERE id IN (`+placeholders+`)`, args...)
	if err != nil {
		return 0, fmt.Errorf("store: delete signals: %w", err)
	}
	n, _ := res.RowsAffected()
	return n, nil
}

func (s *sqliteStore) DeleteNoiseSignals(ctx context.Context) (int64, error) {
	// A "noise" signal is an X signal that either:
	//   (a) has no entries in x_rule_matches at all, OR
	//   (b) every rule it matches has local=true
	res, err := s.db.ExecContext(ctx, `
		DELETE FROM signals
		WHERE ext_ns = 'x' AND NOT EXISTS (
			SELECT 1 FROM x_rule_matches m
			JOIN x_rules r ON r.id = m.rule_id
			WHERE m.signal_id = signals.id AND r.local = FALSE
		)`)
	if err != nil {
		return 0, fmt.Errorf("store: delete noise signals: %w", err)
	}
	n, _ := res.RowsAffected()
	return n, nil
}

// int64Placeholders returns "?,?,…" for ids and the matching argument slice.
func int64Placeholders(ids []int64) (string, []any) {
	args := make([]any, len(ids))
	for i, id := range ids {
		args[i] = id
	}
	return strings.TrimSuffix(strings.Repeat("?,", len(ids)), ","), args
}

// ── X terms (rule atoms) ─────────────────────────────────────────────────────

func (s *sqliteStore) SaveXTerm(ctx context.Context, source *model.XTerm) error {
	res, err := s.db.ExecContext(ctx,
		`INSERT INTO x_terms (name, type, class, operator, value, active, created_at, updated_at)
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

func (s *sqliteStore) GetXTerm(ctx context.Context, id int64) (*model.XTerm, error) {
	row := s.db.QueryRowContext(ctx,
		`SELECT id, name, type, class, operator, value, active, created_at, updated_at FROM x_terms WHERE id = ?`, id)
	src, err := scanXTerm(row)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, fmt.Errorf("store: get source: not found: %w", err)
		}
		return nil, fmt.Errorf("store: get source: %w", err)
	}
	return src, nil
}

func (s *sqliteStore) ListXTerms(ctx context.Context, filter XTermFilter) ([]*model.XTerm, error) {
	query := `SELECT id, name, type, class, operator, value, active, created_at, updated_at FROM x_terms`
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

	sources := make([]*model.XTerm, 0)
	for rows.Next() {
		src, err := scanXTerm(rows)
		if err != nil {
			return nil, fmt.Errorf("store: list sources: scan: %w", err)
		}
		sources = append(sources, src)
	}
	return sources, rows.Err()
}

func (s *sqliteStore) UpdateXTerm(ctx context.Context, source *model.XTerm) error {
	now := time.Now().UTC().Format(time.RFC3339)
	_, err := s.db.ExecContext(ctx,
		`UPDATE x_terms SET name = ?, type = ?, class = ?, operator = ?, value = ?, updated_at = ? WHERE id = ?`,
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

func (s *sqliteStore) DeleteXTerm(ctx context.Context, id int64) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM x_terms WHERE id = ?`, id)
	if err != nil {
		return fmt.Errorf("store: delete source: %w", err)
	}
	return nil
}

func (s *sqliteStore) SetXTermActive(ctx context.Context, id int64, active bool) error {
	_, err := s.db.ExecContext(ctx,
		`UPDATE x_terms SET active = ?, updated_at = ? WHERE id = ?`,
		boolToInt(active), time.Now().UTC().Format(time.RFC3339), id)
	if err != nil {
		return fmt.Errorf("store: set source active: %w", err)
	}
	return nil
}

func (s *sqliteStore) ActiveRuleRefCount(ctx context.Context, sourceID int64) (int, error) {
	var count int
	err := s.db.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM x_rules WHERE active = 1 AND expression LIKE ?`,
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
		`SELECT COUNT(*) FROM x_rules WHERE expression LIKE ?`,
		fmt.Sprintf(`%%"src":%d}%%`, sourceID),
	).Scan(&count)
	if err != nil {
		return 0, fmt.Errorf("store: rule ref count: %w", err)
	}
	return count, nil
}

// ── Rules ─────────────────────────────────────────────────────────────────────

func (s *sqliteStore) SaveXRule(ctx context.Context, rule *model.XRule) error {
	res, err := s.db.ExecContext(ctx,
		`INSERT INTO x_rules (name, expression, compiled_value, x_rule_id, active, local, created_at, updated_at)
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

func (s *sqliteStore) GetXRule(ctx context.Context, id int64) (*model.XRule, error) {
	row := s.db.QueryRowContext(ctx,
		`SELECT id, name, expression, compiled_value, x_rule_id, active, local, created_at, updated_at FROM x_rules WHERE id = ?`, id)
	rule, err := scanXRule(row)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, fmt.Errorf("store: get rule: not found: %w", err)
		}
		return nil, fmt.Errorf("store: get rule: %w", err)
	}
	return rule, nil
}

func (s *sqliteStore) GetXRuleByXRuleID(ctx context.Context, xRuleID string) (*model.XRule, error) {
	row := s.db.QueryRowContext(ctx,
		`SELECT id, name, expression, compiled_value, x_rule_id, active, local, created_at, updated_at FROM x_rules WHERE x_rule_id = ?`, xRuleID)
	rule, err := scanXRule(row)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, fmt.Errorf("store: get rule by x_rule_id: not found: %w", err)
		}
		return nil, fmt.Errorf("store: get rule by x_rule_id: %w", err)
	}
	return rule, nil
}

func (s *sqliteStore) ListXRules(ctx context.Context, filter XRuleFilter) ([]*model.XRule, error) {
	query := `SELECT id, name, expression, compiled_value, x_rule_id, active, local, created_at, updated_at FROM x_rules`
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

	rules := make([]*model.XRule, 0)
	for rows.Next() {
		rule, err := scanXRule(rows)
		if err != nil {
			return nil, fmt.Errorf("store: list rules: scan: %w", err)
		}
		rules = append(rules, rule)
	}
	return rules, rows.Err()
}

func (s *sqliteStore) UpdateXRule(ctx context.Context, rule *model.XRule) error {
	_, err := s.db.ExecContext(ctx,
		`UPDATE x_rules SET name = ?, expression = ?, compiled_value = ?, x_rule_id = ?, active = ?, local = ?, updated_at = ? WHERE id = ?`,
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

func (s *sqliteStore) DeleteXRule(ctx context.Context, id int64) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM x_rules WHERE id = ?`, id)
	if err != nil {
		return fmt.Errorf("store: delete rule: %w", err)
	}
	return nil
}

func (s *sqliteStore) SetXRuleActive(ctx context.Context, id int64, active bool, xRuleID string) error {
	_, err := s.db.ExecContext(ctx,
		`UPDATE x_rules SET active = ?, x_rule_id = ?, updated_at = ? WHERE id = ?`,
		boolToInt(active), xRuleID, time.Now().UTC().Format(time.RFC3339), id)
	if err != nil {
		return fmt.Errorf("store: set rule active: %w", err)
	}
	return nil
}

func (s *sqliteStore) ListXRulesDependingOn(ctx context.Context, ruleID int64) ([]*model.XRule, error) {
	all, err := s.ListXRules(ctx, XRuleFilter{})
	if err != nil {
		return nil, fmt.Errorf("store: list rules depending on %d: %w", ruleID, err)
	}
	var out []*model.XRule
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

func (s *sqliteStore) XRuleMatchCount(ctx context.Context, ruleID int64) (int, error) {
	var count int
	err := s.db.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM x_rule_matches WHERE rule_id = ?`, ruleID).Scan(&count)
	if err != nil {
		return 0, fmt.Errorf("store: x rule match count: %w", err)
	}
	return count, nil
}

// ── XRuleMatches ──────────────────────────────────────────────────────────────

func (s *sqliteStore) SaveXRuleMatch(ctx context.Context, m *model.XRuleMatch) error {
	_, err := s.db.ExecContext(ctx,
		`INSERT OR IGNORE INTO x_rule_matches (signal_id, rule_id) VALUES (?, ?)`,
		m.SignalID, m.RuleID)
	if err != nil {
		return fmt.Errorf("store: save x rule match: %w", err)
	}
	return nil
}

func (s *sqliteStore) ListXRuleIDs(ctx context.Context, signalID int64) ([]int64, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT rule_id FROM x_rule_matches WHERE signal_id = ?`, signalID)
	if err != nil {
		return nil, fmt.Errorf("store: list x rule ids: %w", err)
	}
	defer rows.Close()
	var ids []int64
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			return nil, fmt.Errorf("store: list x rule ids: scan: %w", err)
		}
		ids = append(ids, id)
	}
	return ids, rows.Err()
}

// ── helpers ───────────────────────────────────────────────────────────────────

type scanner interface {
	Scan(dest ...any) error
}

func scanSignal(s scanner) (*model.Signal, error) {
	var sig model.Signal
	var kind, attrsJSON, publishedAt, fetchedAt string
	if err := s.Scan(&sig.ID, &kind, &sig.ExtNS, &sig.ExternalID, &sig.Author, &sig.AuthorID, &sig.Title,
		&sig.Text, &sig.URL, &sig.Lang, &sig.Lat, &sig.Lon, &attrsJSON, &sig.RawJSON, &publishedAt, &fetchedAt); err != nil {
		return nil, err
	}
	sig.Kind = model.SignalKind(kind)
	if err := json.Unmarshal([]byte(attrsJSON), &sig.Attrs); err != nil {
		return nil, fmt.Errorf("parse attrs: %w", err)
	}
	if sig.Attrs == nil {
		sig.Attrs = map[string]any{}
	}
	var err error
	sig.PublishedAt, err = time.Parse(time.RFC3339, publishedAt)
	if err != nil {
		return nil, fmt.Errorf("parse published_at: %w", err)
	}
	sig.FetchedAt, err = time.Parse(time.RFC3339, fetchedAt)
	if err != nil {
		return nil, fmt.Errorf("parse fetched_at: %w", err)
	}
	return &sig, nil
}

func scanXTerm(s scanner) (*model.XTerm, error) {
	var src model.XTerm
	var rawType, rawClass string
	var active int
	var createdAt, updatedAt string
	if err := s.Scan(&src.ID, &src.Name, &rawType, &rawClass, &src.Operator, &src.Value, &active, &createdAt, &updatedAt); err != nil {
		return nil, err
	}
	src.Type = model.XTermType(rawType)
	src.Class = model.XTermClass(rawClass)
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

func scanXRule(s scanner) (*model.XRule, error) {
	var r model.XRule
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

// SaveEntityTrack inserts a track record. Duplicate (entity_id, signal_id) pairs
// are silently ignored via INSERT OR IGNORE so callers can always call this safely.
func (s *sqliteStore) SaveEntityTrack(ctx context.Context, t *model.EntityTrack) error {
	res, err := s.db.ExecContext(ctx,
		`INSERT INTO entity_tracks (entity_id, signal_id, match_source, context, summary, created_at)
		 VALUES (?, ?, ?, ?, ?, ?)
		 ON CONFLICT(entity_id, signal_id) DO UPDATE SET
		     match_source = excluded.match_source,
		     context      = excluded.context,
		     summary      = excluded.summary`,
		t.EntityID, t.SignalID, t.MatchSource, t.Context, t.Summary,
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
	query := `SELECT et.id, et.entity_id, te.name, et.signal_id, et.match_source, et.context, et.summary,
	                 COALESCE(sg.text, ''), COALESCE(sg.author, ''), sg.published_at, et.published_at
	          FROM entity_tracks et
	          JOIN tracked_entities te ON te.id = et.entity_id
	          LEFT JOIN signals sg ON sg.id = et.signal_id`
	var args []any
	var where []string

	if filter.EntityID != nil {
		where = append(where, "et.entity_id = ?")
		args = append(args, *filter.EntityID)
	}
	if filter.SignalID != nil {
		where = append(where, "et.signal_id = ?")
		args = append(args, *filter.SignalID)
	}
	if filter.From != nil {
		where = append(where, "COALESCE(sg.published_at, et.published_at) >= ?")
		args = append(args, filter.From.UTC().Format(time.RFC3339))
	}
	if filter.To != nil {
		where = append(where, "COALESCE(sg.published_at, et.published_at) <= ?")
		args = append(args, filter.To.UTC().Format(time.RFC3339))
	}
	if len(where) > 0 {
		query += " WHERE " + strings.Join(where, " AND ")
	}
	query += " ORDER BY COALESCE(sg.published_at, et.published_at) DESC"

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
	needSignalJoin := filter.From != nil || filter.To != nil
	if needSignalJoin {
		query += ` LEFT JOIN signals sg ON sg.id = et.signal_id`
	}
	var args []any
	var where []string

	if filter.EntityID != nil {
		where = append(where, "et.entity_id = ?")
		args = append(args, *filter.EntityID)
	}
	if filter.SignalID != nil {
		where = append(where, "et.signal_id = ?")
		args = append(args, *filter.SignalID)
	}
	if filter.From != nil {
		where = append(where, "COALESCE(sg.published_at, et.published_at) >= ?")
		args = append(args, filter.From.UTC().Format(time.RFC3339))
	}
	if filter.To != nil {
		where = append(where, "COALESCE(sg.published_at, et.published_at) <= ?")
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
		joins = append(joins, `JOIN x_rule_matches m ON m.signal_id = t.id`)
		where = append(where, "m.rule_id = ?")
		args = append(args, *filter.RuleID)
	}
	if filter.EntityID != nil {
		joins = append(joins, `JOIN entity_tracks et ON et.signal_id = t.id`)
		where = append(where, "et.entity_id = ?")
		args = append(args, *filter.EntityID)
	}
	if filter.From != nil {
		where = append(where, "t.published_at >= ?")
		args = append(args, filter.From.UTC().Format(time.RFC3339))
	}
	if filter.To != nil {
		where = append(where, "t.published_at <= ?")
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
	query := `SELECT t.id, t.author, t.text, t.published_at,
                     aem.value, a.name, ae.executed_at
              FROM signals t
              JOIN action_executions ae ON ae.signal_id = t.id AND ae.status = 'success'
              JOIN actions a ON a.id = ae.action_id AND a.type = 'ollama'
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
		var summaryJSON, publishedAt, summarizedAt string
		if err := rows.Scan(
			&item.SignalID, &item.Author, &item.Text, &publishedAt,
			&summaryJSON, &item.ModelName, &summarizedAt,
		); err != nil {
			return nil, fmt.Errorf("store: list summaries: scan: %w", err)
		}
		if t, err := time.Parse(time.RFC3339, publishedAt); err == nil {
			item.PublishedAt = t
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
	query := `SELECT COUNT(*) FROM signals t
              JOIN action_executions ae ON ae.signal_id = t.id AND ae.status = 'success'
              JOIN actions a ON a.id = ae.action_id AND a.type = 'ollama'
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
	var signalPublishedAt, createdAt *string
	if err := s.Scan(&t.ID, &t.EntityID, &t.EntityName, &t.SignalID, &t.MatchSource, &t.Context, &t.Summary, &t.SignalText, &t.SignalAuthor, &signalPublishedAt, &createdAt); err != nil {
		return nil, err
	}
	if signalPublishedAt != nil {
		parsed, err := time.Parse(time.RFC3339, *signalPublishedAt)
		if err != nil {
			return nil, fmt.Errorf("parse entity_track signal_published_at: %w", err)
		}
		t.SignalPublishedAt = &parsed
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
		`INSERT INTO digest_jobs (id, status, params, signal_count, created_at)
		 VALUES (?, ?, ?, ?, ?)`,
		job.ID,
		string(job.Status),
		string(paramsJSON),
		job.SignalCount,
		job.CreatedAt.UTC().Format(time.RFC3339),
	)
	if err != nil {
		return fmt.Errorf("store: save digest job: %w", err)
	}
	return nil
}

func (s *sqliteStore) GetDigestJob(ctx context.Context, id string) (*model.DigestJob, error) {
	row := s.db.QueryRowContext(ctx,
		`SELECT id, status, params, result, error, signal_count, created_at, completed_at
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
		`UPDATE digest_jobs SET status=?, result=?, error=?, signal_count=?, completed_at=? WHERE id=?`,
		string(job.Status),
		nullString(job.Result),
		nullString(job.Error),
		job.SignalCount,
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
		`SELECT id, status, params, result, error, signal_count, created_at, completed_at
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
	if err := s.Scan(&job.ID, &status, &paramsJSON, &result, &errStr, &job.SignalCount, &createdAt, &completedAt); err != nil {
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
        SELECT ae.signal_id,
               COALESCE(t.published_at, ae.executed_at) AS signal_published_at,
               COALESCE(t.text, '') AS signal_text,
               a.name AS model_name,
               aem_loc.value AS coordinates_json,
               COALESCE(json_extract(a.config, '$.map_config.coordinate_type'), '') AS coordinate_type,
               COALESCE((SELECT json_group_object(aem2.key, json(aem2.value))
                         FROM action_execution_metadata aem2
                         JOIN action_executions ae2 ON ae2.id = aem2.execution_id
                         WHERE ae2.signal_id = ae.signal_id
                           AND ae2.status = 'success'), '{}') AS metadata_json,
               COALESCE(json_extract(a.config, '$.map_config.popup_template'), '') AS popup_template
        FROM action_execution_metadata aem_loc
        JOIN action_executions ae ON ae.id = aem_loc.execution_id AND ae.status = 'success'
        JOIN actions a ON a.id = ae.action_id
        LEFT JOIN signals t ON t.id = ae.signal_id
        WHERE aem_loc.key = json_extract(a.config, '$.map_config.coordinate_key')
          AND json_extract(a.config, '$.map_config.coordinate_key') IS NOT NULL
          AND json_extract(a.config, '$.map_config.coordinate_key') != ''
          AND aem_loc.value != 'null'
          AND aem_loc.value != '[]'
        ORDER BY signal_published_at DESC
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
			&pin.SignalID, &pin.SignalPublishedAt, &pin.SignalText, &pin.ModelName,
			&pin.CoordinatesJSON, &coordType,
			&pin.MetadataJSON, &pin.PopupTemplate,
		); err != nil {
			return nil, fmt.Errorf("store: list map pins: scan: %w", err)
		}
		pin.CoordinateType = model.CoordinateType(coordType)
		pins = append(pins, &pin)
	}
	return pins, rows.Err()
}

func (s *sqliteStore) GetGeocodeCache(ctx context.Context, lat, lon float64) (string, bool, error) {
	var name string
	err := s.db.QueryRowContext(ctx,
		`SELECT name FROM geocode_cache WHERE lat = ? AND lon = ?`, lat, lon,
	).Scan(&name)
	if errors.Is(err, sql.ErrNoRows) {
		return "", false, nil
	}
	if err != nil {
		return "", false, fmt.Errorf("store: get geocode cache: %w", err)
	}
	return name, true, nil
}

func (s *sqliteStore) SetGeocodeCache(ctx context.Context, lat, lon float64, name string) error {
	_, err := s.db.ExecContext(ctx,
		`INSERT OR REPLACE INTO geocode_cache (lat, lon, name, cached_at)
		 VALUES (?, ?, ?, strftime('%s','now'))`,
		lat, lon, name,
	)
	if err != nil {
		return fmt.Errorf("store: set geocode cache: %w", err)
	}
	return nil
}

// ── EntityCandidates ──────────────────────────────────────────────────────────

// UpsertEntityCandidate inserts a new candidate or updates an existing pending
// one. Promoted and rejected candidates are left untouched.
// After the call, c.ID, c.OccurrenceCount, c.AvgConfidence, c.Sources, and
// c.Status are set to the final stored values.
func (s *sqliteStore) UpsertEntityCandidate(ctx context.Context, c *model.EntityCandidate) error {
	now := time.Now().UTC().Format(time.RFC3339)

	// Read existing candidate.
	var existing struct {
		id            int64
		count         int
		avg           float64
		sources       string
		status        string
		word          string
		typ           string
		autoPromoted  int
		promotedEntID sql.NullInt64
		firstSeenAt   string
		lastSeenAt    string
	}
	err := s.db.QueryRowContext(ctx,
		`SELECT id, occurrence_count, avg_confidence, sources, status,
		        word, type, auto_promoted, promoted_entity_id, first_seen_at, last_seen_at
		 FROM entity_candidates WHERE word = ? AND type = ?`,
		c.Word, c.Type,
	).Scan(
		&existing.id, &existing.count, &existing.avg, &existing.sources, &existing.status,
		&existing.word, &existing.typ, &existing.autoPromoted, &existing.promotedEntID,
		&existing.firstSeenAt, &existing.lastSeenAt,
	)

	if errors.Is(err, sql.ErrNoRows) {
		// Insert new candidate.
		srcJSON, _ := json.Marshal(c.Sources)
		res, err := s.db.ExecContext(ctx,
			`INSERT INTO entity_candidates
			 (word, type, occurrence_count, avg_confidence, sources, status, first_seen_at, last_seen_at)
			 VALUES (?, ?, 1, ?, ?, 'pending', ?, ?)`,
			c.Word, c.Type, c.AvgConfidence, string(srcJSON), now, now,
		)
		if err != nil {
			return fmt.Errorf("store: upsert entity candidate: insert: %w", err)
		}
		id, _ := res.LastInsertId()
		c.ID = id
		c.OccurrenceCount = 1
		c.Status = "pending"
		return nil
	}
	if err != nil {
		return fmt.Errorf("store: upsert entity candidate: query: %w", err)
	}

	// Save caller-provided values before populating c from the stored row.
	// Use append to copy the slice — json.Unmarshal reuses backing arrays in-place.
	callerConfidence := c.AvgConfidence
	callerSources := append([]string(nil), c.Sources...)

	// Populate c with all stored values so the caller always sees the real state.
	c.ID = existing.id
	c.Status = existing.status
	c.OccurrenceCount = existing.count
	c.AvgConfidence = existing.avg
	c.Word = existing.word
	c.Type = existing.typ
	c.AutoPromoted = existing.autoPromoted == 1
	if existing.promotedEntID.Valid {
		v := existing.promotedEntID.Int64
		c.PromotedEntityID = &v
	}
	_ = json.Unmarshal([]byte(existing.sources), &c.Sources)
	if c.Sources == nil {
		c.Sources = []string{}
	}
	if t, err2 := time.Parse(time.RFC3339, existing.firstSeenAt); err2 == nil {
		c.FirstSeenAt = t
	}
	if t, err2 := time.Parse(time.RFC3339, existing.lastSeenAt); err2 == nil {
		c.LastSeenAt = t
	}

	// Only update pending candidates.
	if existing.status != "pending" {
		return nil
	}

	newCount := existing.count + 1
	newAvg := (existing.avg*float64(existing.count) + callerConfidence) / float64(newCount)

	var sources []string
	_ = json.Unmarshal([]byte(existing.sources), &sources)
	// Append each source name that is not already present.
	for _, src := range callerSources {
		found := false
		for _, src2 := range sources {
			if src2 == src {
				found = true
				break
			}
		}
		if !found {
			sources = append(sources, src)
		}
	}
	newSourcesJSON, _ := json.Marshal(sources)

	_, err = s.db.ExecContext(ctx,
		`UPDATE entity_candidates
		 SET occurrence_count=?, avg_confidence=?, sources=?, last_seen_at=?
		 WHERE id=?`,
		newCount, newAvg, string(newSourcesJSON), now, existing.id,
	)
	if err != nil {
		return fmt.Errorf("store: upsert entity candidate: update: %w", err)
	}

	c.OccurrenceCount = newCount
	c.AvgConfidence = newAvg
	c.Sources = sources
	return nil
}

func (s *sqliteStore) GetEntityCandidate(ctx context.Context, id int64) (*model.EntityCandidate, error) {
	row := s.db.QueryRowContext(ctx,
		`SELECT id, word, type, occurrence_count, avg_confidence, sources, status,
		        promoted_entity_id, auto_promoted, first_seen_at, last_seen_at
		 FROM entity_candidates WHERE id = ?`, id)
	c, err := scanEntityCandidate(row)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, fmt.Errorf("store: get entity candidate: not found: %w", err)
		}
		return nil, fmt.Errorf("store: get entity candidate: %w", err)
	}
	return c, nil
}

func (s *sqliteStore) ListEntityCandidates(ctx context.Context, filter EntityCandidateFilter) ([]*model.EntityCandidate, error) {
	query := `SELECT id, word, type, occurrence_count, avg_confidence, sources, status,
	                 promoted_entity_id, auto_promoted, first_seen_at, last_seen_at
	          FROM entity_candidates`
	var args []any
	var where []string

	if filter.Status != nil {
		where = append(where, "status = ?")
		args = append(args, *filter.Status)
	}
	if filter.Type != nil {
		where = append(where, "type = ?")
		args = append(args, *filter.Type)
	}
	if len(where) > 0 {
		query += " WHERE " + strings.Join(where, " AND ")
	}
	query += " ORDER BY occurrence_count DESC, last_seen_at DESC"

	page := filter.Page
	if page < 1 {
		page = 1
	}
	perPage := filter.PerPage
	if perPage < 1 {
		perPage = 50
	}
	query += fmt.Sprintf(" LIMIT %d OFFSET %d", perPage, (page-1)*perPage)

	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("store: list entity candidates: %w", err)
	}
	defer rows.Close()

	candidates := make([]*model.EntityCandidate, 0)
	for rows.Next() {
		c, err := scanEntityCandidate(rows)
		if err != nil {
			return nil, fmt.Errorf("store: list entity candidates: scan: %w", err)
		}
		candidates = append(candidates, c)
	}
	return candidates, rows.Err()
}

func (s *sqliteStore) CountEntityCandidates(ctx context.Context, filter EntityCandidateFilter) (int, error) {
	query := `SELECT COUNT(*) FROM entity_candidates`
	var args []any
	var where []string

	if filter.Status != nil {
		where = append(where, "status = ?")
		args = append(args, *filter.Status)
	}
	if filter.Type != nil {
		where = append(where, "type = ?")
		args = append(args, *filter.Type)
	}
	if len(where) > 0 {
		query += " WHERE " + strings.Join(where, " AND ")
	}

	var n int
	if err := s.db.QueryRowContext(ctx, query, args...).Scan(&n); err != nil {
		return 0, fmt.Errorf("store: count entity candidates: %w", err)
	}
	return n, nil
}

func (s *sqliteStore) UpdateEntityCandidateStatus(ctx context.Context, id int64, status string, promotedEntityID *int64, autoPromoted bool) error {
	_, err := s.db.ExecContext(ctx,
		`UPDATE entity_candidates SET status=?, promoted_entity_id=?, auto_promoted=? WHERE id=?`,
		status, promotedEntityID, boolToInt(autoPromoted), id,
	)
	if err != nil {
		return fmt.Errorf("store: update entity candidate status: %w", err)
	}
	return nil
}

func (s *sqliteStore) DeleteEntityCandidate(ctx context.Context, id int64) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM entity_candidates WHERE id = ?`, id)
	if err != nil {
		return fmt.Errorf("store: delete entity candidate: %w", err)
	}
	return nil
}

func (s *sqliteStore) BulkDeleteEntityCandidates(ctx context.Context, f BulkDeleteEntityCandidatesFilter) (int64, error) {
	var clauses []string
	var args []any

	if len(f.IDs) > 0 {
		ph := strings.Repeat("?,", len(f.IDs))
		clauses = append(clauses, fmt.Sprintf("id IN (%s)", ph[:len(ph)-1]))
		for _, id := range f.IDs {
			args = append(args, id)
		}
	}
	if f.Status != "" {
		clauses = append(clauses, "status = ?")
		args = append(args, f.Status)
	}
	if f.Type != "" {
		clauses = append(clauses, "type = ?")
		args = append(args, f.Type)
	}
	if f.AutoPromoted != nil {
		clauses = append(clauses, "auto_promoted = ?")
		args = append(args, boolToInt(*f.AutoPromoted))
	}
	if f.MinConfidence != nil {
		clauses = append(clauses, "avg_confidence >= ?")
		args = append(args, *f.MinConfidence)
	}
	if f.MaxConfidence != nil {
		clauses = append(clauses, "avg_confidence <= ?")
		args = append(args, *f.MaxConfidence)
	}
	if f.MinOccurrences != nil {
		clauses = append(clauses, "occurrence_count >= ?")
		args = append(args, *f.MinOccurrences)
	}
	if f.MaxOccurrences != nil {
		clauses = append(clauses, "occurrence_count <= ?")
		args = append(args, *f.MaxOccurrences)
	}

	query := "DELETE FROM entity_candidates"
	if len(clauses) > 0 {
		query += " WHERE " + strings.Join(clauses, " AND ")
	}

	res, err := s.db.ExecContext(ctx, query, args...)
	if err != nil {
		return 0, fmt.Errorf("store: bulk delete entity candidates: %w", err)
	}
	n, _ := res.RowsAffected()
	return n, nil
}

func (s *sqliteStore) BulkDeleteXRules(ctx context.Context, f BulkDeleteXRulesFilter) (int64, error) {
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
	if f.Local != nil {
		clauses = append(clauses, "local = ?")
		args = append(args, boolToInt(*f.Local))
	}

	query := "DELETE FROM x_rules"
	if len(clauses) > 0 {
		query += " WHERE " + strings.Join(clauses, " AND ")
	}

	res, err := s.db.ExecContext(ctx, query, args...)
	if err != nil {
		return 0, fmt.Errorf("store: bulk delete rules: %w", err)
	}
	n, _ := res.RowsAffected()
	return n, nil
}

func (s *sqliteStore) BulkDeleteXTerms(ctx context.Context, f BulkDeleteXTermsFilter) (int64, error) {
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

	query := "DELETE FROM x_terms"
	if len(clauses) > 0 {
		query += " WHERE " + strings.Join(clauses, " AND ")
	}

	res, err := s.db.ExecContext(ctx, query, args...)
	if err != nil {
		return 0, fmt.Errorf("store: bulk delete sources: %w", err)
	}
	n, _ := res.RowsAffected()
	return n, nil
}

func (s *sqliteStore) BulkDeleteTrackedEntities(ctx context.Context, f BulkDeleteTrackedEntitiesFilter) (int64, error) {
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

	query := "DELETE FROM tracked_entities"
	if len(clauses) > 0 {
		query += " WHERE " + strings.Join(clauses, " AND ")
	}

	res, err := s.db.ExecContext(ctx, query, args...)
	if err != nil {
		return 0, fmt.Errorf("store: bulk delete tracked entities: %w", err)
	}
	n, _ := res.RowsAffected()
	return n, nil
}

func (s *sqliteStore) BulkDeleteDigestJobs(ctx context.Context, f BulkDeleteDigestJobsFilter) (int64, error) {
	var clauses []string
	var args []any

	if len(f.IDs) > 0 {
		ph := strings.Repeat("?,", len(f.IDs))
		clauses = append(clauses, fmt.Sprintf("id IN (%s)", ph[:len(ph)-1]))
		for _, id := range f.IDs {
			args = append(args, id)
		}
	}
	if f.Status != "" {
		clauses = append(clauses, "status = ?")
		args = append(args, f.Status)
	}

	query := "DELETE FROM digest_jobs"
	if len(clauses) > 0 {
		query += " WHERE " + strings.Join(clauses, " AND ")
	}

	res, err := s.db.ExecContext(ctx, query, args...)
	if err != nil {
		return 0, fmt.Errorf("store: bulk delete digest jobs: %w", err)
	}
	n, _ := res.RowsAffected()
	return n, nil
}

func (s *sqliteStore) BulkDeleteSearchJobs(ctx context.Context, f BulkDeleteSearchJobsFilter) (int64, error) {
	var clauses []string
	var args []any

	if len(f.IDs) > 0 {
		ph := strings.Repeat("?,", len(f.IDs))
		clauses = append(clauses, fmt.Sprintf("id IN (%s)", ph[:len(ph)-1]))
		for _, id := range f.IDs {
			args = append(args, id)
		}
	}
	if f.Status != "" {
		clauses = append(clauses, "status = ?")
		args = append(args, f.Status)
	}

	query := "DELETE FROM search_jobs"
	if len(clauses) > 0 {
		query += " WHERE " + strings.Join(clauses, " AND ")
	}

	res, err := s.db.ExecContext(ctx, query, args...)
	if err != nil {
		return 0, fmt.Errorf("store: bulk delete search jobs: %w", err)
	}
	n, _ := res.RowsAffected()
	return n, nil
}

// scanEntityCandidate scans a single entity_candidates row from rows or a row.
func scanEntityCandidate(row scanner) (*model.EntityCandidate, error) {
	var c model.EntityCandidate
	var sourcesJSON string
	var promotedEntityID sql.NullInt64
	var autoPromoted int
	var firstSeenAt, lastSeenAt string

	if err := row.Scan(
		&c.ID, &c.Word, &c.Type, &c.OccurrenceCount, &c.AvgConfidence,
		&sourcesJSON, &c.Status, &promotedEntityID, &autoPromoted,
		&firstSeenAt, &lastSeenAt,
	); err != nil {
		return nil, err
	}

	_ = json.Unmarshal([]byte(sourcesJSON), &c.Sources)
	if c.Sources == nil {
		c.Sources = []string{}
	}
	if promotedEntityID.Valid {
		v := promotedEntityID.Int64
		c.PromotedEntityID = &v
	}
	c.AutoPromoted = autoPromoted == 1

	if t, err := time.Parse(time.RFC3339, firstSeenAt); err == nil {
		c.FirstSeenAt = t
	}
	if t, err := time.Parse(time.RFC3339, lastSeenAt); err == nil {
		c.LastSeenAt = t
	}
	return &c, nil
}

// UpsertEntityConnection inserts a new connection or increments its weight.
// Always normalises pair order so from_id < to_id.
func (s *sqliteStore) UpsertEntityConnection(ctx context.Context, fromID, toID int64) error {
	if fromID > toID {
		fromID, toID = toID, fromID
	}
	now := time.Now().UTC().Format(time.RFC3339)
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO entity_connections (from_id, to_id, weight, updated_at)
		 VALUES (?, ?, 1, ?)
		 ON CONFLICT(from_id, to_id) DO UPDATE SET
		   weight     = weight + 1,
		   updated_at = excluded.updated_at`,
		fromID, toID, now,
	)
	if err != nil {
		return fmt.Errorf("store: upsert entity connection: %w", err)
	}
	return nil
}

// ListEntityConnections returns all connection edges with entity names joined.
func (s *sqliteStore) ListEntityConnections(ctx context.Context) ([]*model.EntityConnection, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT ec.from_id, ec.to_id, ec.weight, ec.updated_at,
		        f.name, f.type, t.name, t.type
		 FROM entity_connections ec
		 JOIN tracked_entities f ON f.id = ec.from_id
		 JOIN tracked_entities t ON t.id = ec.to_id
		 ORDER BY ec.weight DESC, ec.from_id, ec.to_id`,
	)
	if err != nil {
		return nil, fmt.Errorf("store: list entity connections: %w", err)
	}
	defer rows.Close()

	var out []*model.EntityConnection
	for rows.Next() {
		var c model.EntityConnection
		var updatedAt string
		if err := rows.Scan(
			&c.FromID, &c.ToID, &c.Weight, &updatedAt,
			&c.FromName, &c.FromType, &c.ToName, &c.ToType,
		); err != nil {
			return nil, fmt.Errorf("store: list entity connections: scan: %w", err)
		}
		c.UpdatedAt, _ = time.Parse(time.RFC3339, updatedAt)
		out = append(out, &c)
	}
	if out == nil {
		out = []*model.EntityConnection{}
	}
	return out, rows.Err()
}

// DeleteAllEntityConnections removes every row from entity_connections.
func (s *sqliteStore) DeleteAllEntityConnections(ctx context.Context) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM entity_connections`)
	if err != nil {
		return fmt.Errorf("store: delete all entity connections: %w", err)
	}
	return nil
}

// GetEntityConnectionSubgraph returns the BFS spanning tree of entity connections
// reachable from entityID within depth hops, including only edges with weight >= minWeight.
// Only the discovery edges from the BFS traversal are returned (the edge that first reached
// each node), so depth=1 yields a star and each additional hop adds exactly one ring of
// new nodes without including intra-layer edges or cross-edges between previously-visited nodes.
// This keeps the result size proportional to the number of reachable nodes (at most N-1 edges
// for N nodes) regardless of how dense the underlying graph is.
func (s *sqliteStore) GetEntityConnectionSubgraph(ctx context.Context, entityID int64, depth, minWeight int) ([]*model.EntityConnection, error) {
	all, err := s.ListEntityConnections(ctx)
	if err != nil {
		return nil, fmt.Errorf("store: get entity connection subgraph: %w", err)
	}

	if depth < 1 {
		depth = 1
	}

	if minWeight < 1 {
		minWeight = 1
	}

	// Build adjacency list using only edges that meet the weight threshold.
	adj := make(map[int64][]int64)
	for _, c := range all {
		if c.Weight >= minWeight {
			adj[c.FromID] = append(adj[c.FromID], c.ToID)
			adj[c.ToID] = append(adj[c.ToID], c.FromID)
		}
	}

	// BFS from entityID. For each newly discovered node record the spanning tree edge
	// (the edge that first reached it). entity_connections enforces from_id < to_id so
	// we normalise the pair the same way for easy lookup.
	type edgeKey = [2]int64
	treeEdges := make(map[edgeKey]bool)
	visited := map[int64]bool{entityID: true}
	frontier := []int64{entityID}
	for d := 0; d < depth && len(frontier) > 0; d++ {
		var next []int64
		for _, id := range frontier {
			for _, neighbor := range adj[id] {
				if !visited[neighbor] {
					visited[neighbor] = true
					lo, hi := id, neighbor
					if lo > hi {
						lo, hi = hi, lo
					}
					treeEdges[edgeKey{lo, hi}] = true
					next = append(next, neighbor)
				}
			}
		}
		frontier = next
	}

	// Return only the spanning tree edges (all already satisfy minWeight).
	out := make([]*model.EntityConnection, 0, len(treeEdges))
	for _, c := range all {
		if treeEdges[edgeKey{c.FromID, c.ToID}] {
			out = append(out, c)
		}
	}
	return out, nil
}
