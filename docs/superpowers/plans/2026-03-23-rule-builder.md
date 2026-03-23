# Rule Builder Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Replace the 1:1 source→X-rule mapping with a two-layer architecture: Sources (named atomic operators) + Rules (boolean expression compositions), enabling AND/OR/NOT compound X stream filters.

**Architecture:** Each `Source` is a named atom (e.g. `from:BBCPersian`, `lang:fa`). Each `Rule` stores a recursive JSON expression tree that references sources by ID, compiles to an X API rule string, and gets its own `x_rule_id`. `tweet_rules` replaces `tweet_sources` — X's `matching_rules` now maps to Rule records, not Source records.

**Tech Stack:** Go 1.25, modernc.org/sqlite, chi router, cobra CLI, X Filtered Stream API v2.

**Spec:** `docs/superpowers/specs/2026-03-23-rule-builder-design.md`

---

## File Map

**Create:**
- `migrations/003_rule_builder.sql` — schema migration (add columns, recreate sources, rules table, tweet_rules table)
- `internal/model/rule.go` — `Rule`, `TweetRule`, `ExprNode` domain types
- `internal/stream/compiler.go` — `RuleCompiler`: expression tree → X syntax string
- `internal/stream/compiler_test.go` — compiler unit tests
- `internal/stream/validator.go` — `RuleValidator`: validate expression before save
- `internal/stream/validator_test.go` — validator unit tests
- `internal/api/handlers_rules.go` — REST handlers for rule CRUD
- `cli/cmd/rule.go` — `rule` subcommand (add/list/show/update/remove/activate/deactivate)

**Modify:**
- `internal/model/source.go` — add `Name`, `Class`, `Operator`; expand `SourceType`; add `Render()`; remove `XRuleID`
- `internal/model/tweet.go` — replace `TweetSource`/`Tweet.Sources` with `TweetRule`/`Tweet.Rules`
- `internal/store/store.go` — update `Store` interface: new rule methods, updated filters, replace `SaveTweetSource`/`UpdateSourceXRuleID`
- `internal/store/sqlite.go` — update source scan/CRUD, add rule CRUD, replace tweet_sources with tweet_rules, update `ListTweets` filter
- `internal/store/sqlite_test.go` — update existing source tests, add rule + tweet_rules tests
- `internal/stream/client.go` — update `processEvent` to use `GetRuleByXRuleID` + `SaveTweetRule`
- `internal/api/handlers_sources.go` — remove X API push/delete; update to new Source fields; add deactivate endpoint
- `internal/api/handlers_tweets.go` — replace `source_id` with `rule_id` in list filter
- `internal/api/server.go` — register rule routes, add `tier` field, add `makeResolver`
- `cli/cmd/serve.go` — pass `tier` to `NewServerWithConcreteRules`
- `cli/cmd/source.go` — update `source add` flags (`--name`, updated `--type`, `--no-active`)

---

## Task 1: Migration

**Files:**
- Create: `migrations/003_rule_builder.sql`

- [ ] **Step 1: Write the migration**

```sql
ALTER TABLE sources ADD COLUMN name TEXT;
ALTER TABLE sources ADD COLUMN class TEXT;
ALTER TABLE sources ADD COLUMN operator TEXT;

UPDATE sources SET
    name     = value,
    class    = 'atom',
    operator = CASE type WHEN 'account' THEN 'from:' ELSE '' END;

CREATE TABLE rules (
    id             INTEGER PRIMARY KEY AUTOINCREMENT,
    name           TEXT NOT NULL,
    expression     TEXT NOT NULL,
    compiled_value TEXT NOT NULL DEFAULT '',
    x_rule_id      TEXT NOT NULL DEFAULT '',
    active         INTEGER NOT NULL DEFAULT 0,
    created_at     DATETIME NOT NULL DEFAULT (datetime('now')),
    updated_at     DATETIME NOT NULL DEFAULT (datetime('now'))
);

DROP TABLE IF EXISTS tweet_sources;

-- NOTE: SQLite does not support ALTER TABLE DROP COLUMN on older versions.
-- x_rule_id is removed from sources by the table reconstruction below (sources_new).

CREATE TABLE tweet_rules (
    tweet_id TEXT    NOT NULL REFERENCES tweets(id),
    rule_id  INTEGER NOT NULL REFERENCES rules(id),
    PRIMARY KEY (tweet_id, rule_id)
);
CREATE INDEX IF NOT EXISTS idx_tweet_rules_tweet_id ON tweet_rules(tweet_id);
CREATE INDEX IF NOT EXISTS idx_tweet_rules_rule_id  ON tweet_rules(rule_id);

CREATE TABLE sources_new (
    id         INTEGER PRIMARY KEY AUTOINCREMENT,
    name       TEXT NOT NULL DEFAULT '',
    type       TEXT NOT NULL,
    class      TEXT NOT NULL DEFAULT 'atom',
    operator   TEXT NOT NULL DEFAULT '',
    value      TEXT NOT NULL,
    active     INTEGER NOT NULL DEFAULT 1,
    created_at DATETIME NOT NULL DEFAULT (datetime('now')),
    updated_at DATETIME NOT NULL DEFAULT (datetime('now'))
);
INSERT INTO sources_new (id, name, type, class, operator, value, active, created_at, updated_at)
    SELECT id, COALESCE(name,''), type, COALESCE(class,'atom'), COALESCE(operator,''), value, active, created_at, updated_at
    FROM sources;
DROP TABLE sources;
ALTER TABLE sources_new RENAME TO sources;
```

- [ ] **Step 2: Commit**

```bash
git add migrations/003_rule_builder.sql
git commit -m "feat(migrations): add rule builder schema migration"
```

---

## Task 2: Domain Models

**Files:**
- Modify: `internal/model/source.go`
- Create: `internal/model/rule.go`
- Modify: `internal/model/tweet.go`

- [ ] **Step 1: Replace `internal/model/source.go`**

Replace the entire file:

```go
package model

import "time"

type SourceType string

const (
	SourceTypeKeyword       SourceType = "keyword"
	SourceTypePhrase        SourceType = "phrase"
	SourceTypeHashtag       SourceType = "hashtag"
	SourceTypeCashtag       SourceType = "cashtag"
	SourceTypeMention       SourceType = "mention"
	SourceTypeAccount       SourceType = "account"
	SourceTypeToAccount     SourceType = "to_account"
	SourceTypeRetweetsOf    SourceType = "retweets_of"
	SourceTypeURL           SourceType = "url"
	SourceTypeURLContains   SourceType = "url_contains"
	SourceTypeContext       SourceType = "context"
	SourceTypeEntity        SourceType = "entity"
	SourceTypeConversation  SourceType = "conversation_id"
	SourceTypePlace         SourceType = "place"
	SourceTypePlaceCountry  SourceType = "place_country"
	SourceTypePointRadius   SourceType = "point_radius"
	SourceTypeBoundingBox   SourceType = "bounding_box"
	SourceTypeLang          SourceType = "lang"
	SourceTypeIs            SourceType = "is"
	SourceTypeHas           SourceType = "has"
	SourceTypeFollowers     SourceType = "followers_count"
	SourceTypeTweets        SourceType = "tweets_count"
	SourceTypeFollowing     SourceType = "following_count"
	SourceTypeListed        SourceType = "listed_count"
	SourceTypeSample        SourceType = "sample"
)

type SourceClass string

const (
	SourceClassAtom     SourceClass = "atom"
	SourceClassModifier SourceClass = "modifier"
)

// SourceTypeInfo describes the canonical X API operator and class for a SourceType.
type SourceTypeInfo struct {
	Operator string
	Class    SourceClass
}

// SourceTypeMap is the canonical mapping from SourceType to operator prefix and class.
// Used by the CLI and API to derive class/operator from type.
var SourceTypeMap = map[SourceType]SourceTypeInfo{
	SourceTypeKeyword:      {Operator: "", Class: SourceClassAtom},
	SourceTypePhrase:       {Operator: "", Class: SourceClassAtom},
	SourceTypeHashtag:      {Operator: "", Class: SourceClassAtom},
	SourceTypeCashtag:      {Operator: "", Class: SourceClassAtom},
	SourceTypeMention:      {Operator: "", Class: SourceClassAtom},
	SourceTypeAccount:      {Operator: "from:", Class: SourceClassAtom},
	SourceTypeToAccount:    {Operator: "to:", Class: SourceClassAtom},
	SourceTypeRetweetsOf:   {Operator: "retweets_of:", Class: SourceClassAtom},
	SourceTypeURL:          {Operator: "url:", Class: SourceClassAtom},
	SourceTypeURLContains:  {Operator: "url_contains:", Class: SourceClassAtom},
	SourceTypeContext:      {Operator: "context:", Class: SourceClassAtom},
	SourceTypeEntity:       {Operator: "entity:", Class: SourceClassAtom},
	SourceTypeConversation: {Operator: "conversation_id:", Class: SourceClassAtom},
	SourceTypePlace:        {Operator: "place:", Class: SourceClassAtom},
	SourceTypePlaceCountry: {Operator: "place_country:", Class: SourceClassAtom},
	SourceTypePointRadius:  {Operator: "point_radius:", Class: SourceClassAtom},
	SourceTypeBoundingBox:  {Operator: "bounding_box:", Class: SourceClassAtom},
	SourceTypeLang:         {Operator: "lang:", Class: SourceClassModifier},
	SourceTypeIs:           {Operator: "is:", Class: SourceClassModifier},
	SourceTypeHas:          {Operator: "has:", Class: SourceClassModifier},
	SourceTypeFollowers:    {Operator: "followers_count:", Class: SourceClassModifier},
	SourceTypeTweets:       {Operator: "tweets_count:", Class: SourceClassModifier},
	SourceTypeFollowing:    {Operator: "following_count:", Class: SourceClassModifier},
	SourceTypeListed:       {Operator: "listed_count:", Class: SourceClassModifier},
	SourceTypeSample:       {Operator: "sample:", Class: SourceClassModifier},
}

type Source struct {
	ID        int64       `json:"id"`
	Name      string      `json:"name"`
	Type      SourceType  `json:"type"`
	Class     SourceClass `json:"class"`
	Operator  string      `json:"operator"`
	Value     string      `json:"value"`
	Active    bool        `json:"active"`
	CreatedAt time.Time   `json:"created_at"`
	UpdatedAt time.Time   `json:"updated_at"`
}

// Render returns the X API syntax for this source (operator + value).
func (s *Source) Render() string {
	if s.Operator == "" {
		return s.Value
	}
	return s.Operator + s.Value
}
```

- [ ] **Step 2: Create `internal/model/rule.go`**

```go
package model

import (
	"encoding/json"
	"time"
)

// ExprNode is one node in a rule expression tree.
// Exactly one field is set per node.
type ExprNode struct {
	Src *int64     `json:"src,omitempty"` // source reference by ID
	Op  string     `json:"op,omitempty"`  // inline X operator literal (e.g. "is:retweet")
	AND []ExprNode `json:"AND,omitempty"`
	OR  []ExprNode `json:"OR,omitempty"`
	NOT *ExprNode  `json:"NOT,omitempty"`
}

type Rule struct {
	ID            int64           `json:"id"`
	Name          string          `json:"name"`
	Expression    json.RawMessage `json:"expression"`
	CompiledValue string          `json:"compiled_value"`
	XRuleID       string          `json:"x_rule_id"`
	Active        bool            `json:"active"`
	CreatedAt     time.Time       `json:"created_at"`
	UpdatedAt     time.Time       `json:"updated_at"`
}

type TweetRule struct {
	TweetID string `json:"tweet_id"`
	RuleID  int64  `json:"rule_id"`
}
```

- [ ] **Step 3: Update `internal/model/tweet.go`**

Replace `TweetSource` with `TweetRule` and update the `Tweet.Sources` field to `Tweet.Rules`:

```go
package model

import "time"

type Tweet struct {
	ID           string    `json:"id"`
	AuthorID     string    `json:"author_id"`
	AuthorHandle string    `json:"author_handle"`
	Text         string    `json:"text"`
	RawJSON      []byte    `json:"-"`
	CreatedAt    time.Time `json:"created_at"`
	FetchedAt    time.Time `json:"fetched_at"`
	// populated on read joins, not stored in tweets table directly
	Rules []*Rule `json:"rules,omitempty"`
}

// MatchingRule represents an X API matching rule reference on incoming stream events.
type MatchingRule struct {
	ID  string `json:"id"`
	Tag string `json:"tag"`
}

// StreamTweet is the in-flight representation used by the stream listener
// before the tweet is persisted. It carries the tweet plus the matching rules
// from the X stream event so tweet_rules rows can be written.
type StreamTweet struct {
	Tweet         *Tweet
	MatchingRules []MatchingRule
}
```

- [ ] **Step 4: Commit**

```bash
git add internal/model/
git commit -m "feat(model): add Rule/ExprNode types, update Source with Name/Class/Operator, replace TweetSource with TweetRule"
```

---

## Task 3: Store Interface

**Files:**
- Modify: `internal/store/store.go`

- [ ] **Step 1: Update the Store interface**

Replace `internal/store/store.go` entirely:

```go
package store

import (
	"context"
	"time"

	"github.com/amir-the-h/iran-watcher/internal/model"
)

// TweetFilter defines optional filters for listing tweets.
type TweetFilter struct {
	RuleID  *int64
	From    *time.Time
	To      *time.Time
	Page    int
	PerPage int
}

// SourceFilter defines optional filters for listing sources.
type SourceFilter struct {
	Type   *model.SourceType
	Class  *model.SourceClass
	Active *bool
}

// RuleFilter defines optional filters for listing rules.
type RuleFilter struct {
	Active *bool
}

// CategorizationFilter defines optional filters for listing categorizations.
type CategorizationFilter struct {
	TweetID   *string
	ModelName *string
	From      *time.Time
	To        *time.Time
}

// Store is the repository interface for all persistent data.
type Store interface {
	// Tweets
	SaveTweet(ctx context.Context, tweet *model.Tweet) error
	GetTweet(ctx context.Context, id string) (*model.Tweet, error)
	ListTweets(ctx context.Context, filter TweetFilter) ([]*model.Tweet, error)

	// Sources
	SaveSource(ctx context.Context, source *model.Source) error
	GetSource(ctx context.Context, id int64) (*model.Source, error)
	ListSources(ctx context.Context, filter SourceFilter) ([]*model.Source, error)
	DeleteSource(ctx context.Context, id int64) error
	SetSourceActive(ctx context.Context, id int64, active bool) error
	// ActiveRuleRefCount returns how many active rules reference this source.
	ActiveRuleRefCount(ctx context.Context, sourceID int64) (int, error)
	// RuleRefCount returns total rules (active or not) referencing this source.
	RuleRefCount(ctx context.Context, sourceID int64) (int, error)

	// Rules
	SaveRule(ctx context.Context, rule *model.Rule) error
	GetRule(ctx context.Context, id int64) (*model.Rule, error)
	GetRuleByXRuleID(ctx context.Context, xRuleID string) (*model.Rule, error)
	ListRules(ctx context.Context, filter RuleFilter) ([]*model.Rule, error)
	UpdateRule(ctx context.Context, rule *model.Rule) error
	DeleteRule(ctx context.Context, id int64) error
	SetRuleActive(ctx context.Context, id int64, active bool, xRuleID string) error
	// TweetRuleCount returns how many tweet_rules rows reference this rule.
	TweetRuleCount(ctx context.Context, ruleID int64) (int, error)

	// TweetRules
	SaveTweetRule(ctx context.Context, tr *model.TweetRule) error

	// Models
	SaveModel(ctx context.Context, m *model.Model) error
	GetModel(ctx context.Context, id int64) (*model.Model, error)
	ListModels(ctx context.Context) ([]*model.Model, error)
	DeleteModel(ctx context.Context, id int64) error
	SetModelActive(ctx context.Context, id int64, active bool) error

	// Categorizations
	SaveCategorization(ctx context.Context, c *model.Categorization) error
	ListCategorizations(ctx context.Context, filter CategorizationFilter) ([]*model.Categorization, error)
	ListUncategorizedTweets(ctx context.Context, modelNames []string) ([]*model.Tweet, error)

	Close() error
}
```

- [ ] **Step 2: Commit**

```bash
git add internal/store/store.go
git commit -m "feat(store): update Store interface for rule builder (rules, tweet_rules, updated source methods)"
```

---

## Task 4: SQLite Store — Sources

**Files:**
- Modify: `internal/store/sqlite.go`
- Modify: `internal/store/sqlite_test.go`

- [ ] **Step 1: Write failing tests for updated source methods**

In `internal/store/sqlite_test.go`, add a test section (keep existing tests, update them):

```go
func TestSaveAndGetSource(t *testing.T) {
    s := newTestStore(t)
    src := &model.Source{
        Name:      "BBC Persian",
        Type:      model.SourceTypeAccount,
        Class:     model.SourceClassAtom,
        Operator:  "from:",
        Value:     "BBCPersian",
        Active:    true,
        CreatedAt: time.Now().UTC().Truncate(time.Second),
        UpdatedAt: time.Now().UTC().Truncate(time.Second),
    }
    if err := s.SaveSource(ctx, src); err != nil {
        t.Fatalf("SaveSource: %v", err)
    }
    if src.ID == 0 {
        t.Fatal("expected non-zero ID after save")
    }
    got, err := s.GetSource(ctx, src.ID)
    if err != nil {
        t.Fatalf("GetSource: %v", err)
    }
    if got.Name != "BBC Persian" || got.Operator != "from:" || got.Class != model.SourceClassAtom {
        t.Errorf("unexpected source: %+v", got)
    }
}

func TestSetSourceActive(t *testing.T) {
    s := newTestStore(t)
    src := &model.Source{Name: "test", Type: model.SourceTypeKeyword, Class: model.SourceClassAtom, Operator: "", Value: "iran", Active: true, CreatedAt: time.Now().UTC(), UpdatedAt: time.Now().UTC()}
    _ = s.SaveSource(ctx, src)
    if err := s.SetSourceActive(ctx, src.ID, false); err != nil {
        t.Fatalf("SetSourceActive: %v", err)
    }
    got, _ := s.GetSource(ctx, src.ID)
    if got.Active {
        t.Error("expected Active=false")
    }
}

func TestActiveRuleRefCount(t *testing.T) {
    s := newTestStore(t)
    src := &model.Source{Name: "test", Type: model.SourceTypeKeyword, Class: model.SourceClassAtom, Operator: "", Value: "iran", Active: true, CreatedAt: time.Now().UTC(), UpdatedAt: time.Now().UTC()}
    _ = s.SaveSource(ctx, src)
    n, err := s.ActiveRuleRefCount(ctx, src.ID)
    if err != nil {
        t.Fatalf("ActiveRuleRefCount: %v", err)
    }
    if n != 0 {
        t.Errorf("expected 0 refs, got %d", n)
    }
}
```

- [ ] **Step 2: Run tests to confirm they fail**

```bash
go test ./internal/store/... -run "TestSaveAndGetSource|TestSetSourceActive|TestActiveRuleRefCount" -v
```
Expected: compilation error (methods missing) or test failures.

- [ ] **Step 3: Update `scanSource` in `sqlite.go`**

Replace the old `scanSource` (which scans `x_rule_id`) with one that scans the new columns. Also update `SaveSource`, `GetSource`, `ListSources`, `DeleteSource`, remove `UpdateSourceXRuleID`, add `SetSourceActive`, `ActiveRuleRefCount`, `RuleRefCount`.

In `sqlite.go`, replace the `// ── Sources ──` section:

```go
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
	// ExprNode marshals source refs as {"src":N} (compact, no spaces).
	// Matching on `"src":N}` is safe: the `}` boundary prevents false
	// positives where N is a prefix of another ID (e.g. 1 vs 10).
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
```

Also remove the `// ── TweetSources ──` section and remove `SaveTweetSource`.

Update `scanSource`:

```go
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
```

Also update `GetTweet` to query `tweet_rules` + `rules` instead of `tweet_sources` + `sources`, and update `ListTweets` to use `RuleID` instead of `SourceID`.

In `GetTweet`, replace the sources join:
```go
rows, err := s.db.QueryContext(ctx,
    `SELECT r.id, r.name, r.expression, r.compiled_value, r.x_rule_id, r.active, r.created_at, r.updated_at
     FROM rules r
     JOIN tweet_rules tr ON tr.rule_id = r.id
     WHERE tr.tweet_id = ?`, id)
// ... scan into tweet.Rules using scanRule
```

In `ListTweets`, replace `SourceID` join:
```go
if filter.RuleID != nil {
    query += ` JOIN tweet_rules tr ON tr.tweet_id = t.id`
    where = append(where, "tr.rule_id = ?")
    args = append(args, *filter.RuleID)
}
```

- [ ] **Step 4: Run source tests**

```bash
go test ./internal/store/... -run "TestSaveAndGetSource|TestSetSourceActive|TestActiveRuleRefCount" -v
```
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/store/
git commit -m "feat(store): update SQLite source CRUD for new schema (Name/Class/Operator, no x_rule_id)"
```

---

## Task 5: SQLite Store — Rules + TweetRules

**Files:**
- Modify: `internal/store/sqlite.go`
- Modify: `internal/store/sqlite_test.go`

- [ ] **Step 1: Write failing tests**

In `sqlite_test.go`:

```go
func TestSaveAndGetRule(t *testing.T) {
    s := newTestStore(t)
    expr := json.RawMessage(`{"src":1}`)
    rule := &model.Rule{
        Name:          "Test Rule",
        Expression:    expr,
        CompiledValue: "iran",
        XRuleID:       "",
        Active:        false,
        CreatedAt:     time.Now().UTC().Truncate(time.Second),
        UpdatedAt:     time.Now().UTC().Truncate(time.Second),
    }
    if err := s.SaveRule(ctx, rule); err != nil {
        t.Fatalf("SaveRule: %v", err)
    }
    if rule.ID == 0 {
        t.Fatal("expected non-zero ID")
    }
    got, err := s.GetRule(ctx, rule.ID)
    if err != nil {
        t.Fatalf("GetRule: %v", err)
    }
    if got.Name != "Test Rule" || got.CompiledValue != "iran" {
        t.Errorf("unexpected rule: %+v", got)
    }
}

func TestGetRuleByXRuleID(t *testing.T) {
    s := newTestStore(t)
    rule := &model.Rule{Name: "r", Expression: json.RawMessage(`{"op":"iran"}`), CompiledValue: "iran", XRuleID: "abc123", Active: true, CreatedAt: time.Now().UTC(), UpdatedAt: time.Now().UTC()}
    _ = s.SaveRule(ctx, rule)
    _ = s.SetRuleActive(ctx, rule.ID, true, "abc123")
    got, err := s.GetRuleByXRuleID(ctx, "abc123")
    if err != nil {
        t.Fatalf("GetRuleByXRuleID: %v", err)
    }
    if got.ID != rule.ID {
        t.Errorf("wrong rule returned")
    }
    _, err = s.GetRuleByXRuleID(ctx, "nonexistent")
    if err == nil {
        t.Error("expected error for missing x_rule_id")
    }
}

func TestTweetRuleCount(t *testing.T) {
    s := newTestStore(t)
    rule := &model.Rule{Name: "r", Expression: json.RawMessage(`{"op":"iran"}`), CompiledValue: "iran", CreatedAt: time.Now().UTC(), UpdatedAt: time.Now().UTC()}
    _ = s.SaveRule(ctx, rule)
    n, err := s.TweetRuleCount(ctx, rule.ID)
    if err != nil {
        t.Fatalf("TweetRuleCount: %v", err)
    }
    if n != 0 {
        t.Errorf("expected 0, got %d", n)
    }
}
```

- [ ] **Step 2: Run tests to confirm they fail**

```bash
go test ./internal/store/... -run "TestSaveAndGetRule|TestGetRuleByXRuleID|TestTweetRuleCount" -v
```

- [ ] **Step 3: Add rule CRUD + tweet_rules to `sqlite.go`**

Add a new `// ── Rules ──` section:

```go
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
		r, err := scanRule(rows)
		if err != nil {
			return nil, fmt.Errorf("store: list rules: scan: %w", err)
		}
		rules = append(rules, r)
	}
	return rules, rows.Err()
}

func (s *sqliteStore) UpdateRule(ctx context.Context, rule *model.Rule) error {
	_, err := s.db.ExecContext(ctx,
		`UPDATE rules SET name = ?, expression = ?, compiled_value = ?, x_rule_id = ?, updated_at = ? WHERE id = ?`,
		rule.Name,
		string(rule.Expression),
		rule.CompiledValue,
		rule.XRuleID,
		time.Now().UTC().Format(time.RFC3339),
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
```

Add `scanRule` helper:

```go
func scanRule(s scanner) (*model.Rule, error) {
	var r model.Rule
	var expression, createdAt, updatedAt string
	var active int
	if err := s.Scan(&r.ID, &r.Name, &expression, &r.CompiledValue, &r.XRuleID, &active, &createdAt, &updatedAt); err != nil {
		return nil, err
	}
	r.Active = active != 0
	r.Expression = json.RawMessage(expression)
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
```

- [ ] **Step 4: Run all store tests**

```bash
go test ./internal/store/... -v
```
Expected: all PASS

- [ ] **Step 5: Commit**

```bash
git add internal/store/
git commit -m "feat(store): add rule and tweet_rules CRUD to SQLite store"
```

---

## Task 6: Rule Compiler

**Files:**
- Create: `internal/stream/compiler.go`
- Create: `internal/stream/compiler_test.go`

- [ ] **Step 1: Write failing tests in `compiler_test.go`**

```go
package stream_test

import (
	"errors"
	"testing"

	"github.com/amir-the-h/iran-watcher/internal/model"
	"github.com/amir-the-h/iran-watcher/internal/stream"
)

func ptr[T any](v T) *T { return &v }

func mockResolver(sources map[int64]*model.Source) stream.SourceResolver {
	return func(id int64) (*model.Source, error) {
		s, ok := sources[id]
		if !ok {
			return nil, errors.New("not found")
		}
		return s, nil
	}
}

var testSources = map[int64]*model.Source{
	1: {ID: 1, Operator: "from:", Value: "BBCPersian", Class: model.SourceClassAtom},
	2: {ID: 2, Operator: "from:", Value: "IranIntl", Class: model.SourceClassAtom},
	3: {ID: 3, Operator: "", Value: "ایران", Class: model.SourceClassAtom},
	4: {ID: 4, Operator: "lang:", Value: "fa", Class: model.SourceClassModifier},
	5: {ID: 5, Operator: "", Value: `"breaking news"`, Class: model.SourceClassAtom},
}

func TestCompileSingleSrc(t *testing.T) {
	c := stream.NewRuleCompiler()
	result, err := c.Compile(model.ExprNode{Src: ptr[int64](1)}, mockResolver(testSources))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result != "from:BBCPersian" {
		t.Errorf("got %q, want %q", result, "from:BBCPersian")
	}
}

func TestCompileInlineOp(t *testing.T) {
	c := stream.NewRuleCompiler()
	result, err := c.Compile(model.ExprNode{Op: "is:retweet"}, mockResolver(testSources))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result != "is:retweet" {
		t.Errorf("got %q, want %q", result, "is:retweet")
	}
}

func TestCompileAND(t *testing.T) {
	c := stream.NewRuleCompiler()
	node := model.ExprNode{AND: []model.ExprNode{{Src: ptr[int64](1)}, {Src: ptr[int64](3)}}}
	result, err := c.Compile(node, mockResolver(testSources))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result != "from:BBCPersian ایران" {
		t.Errorf("got %q", result)
	}
}

func TestCompileOR(t *testing.T) {
	c := stream.NewRuleCompiler()
	node := model.ExprNode{OR: []model.ExprNode{{Src: ptr[int64](1)}, {Src: ptr[int64](2)}}}
	result, err := c.Compile(node, mockResolver(testSources))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result != "(from:BBCPersian OR from:IranIntl)" {
		t.Errorf("got %q", result)
	}
}

func TestCompileNOTSingleSource(t *testing.T) {
	c := stream.NewRuleCompiler()
	// NOT of single source → -from:BBCPersian (no parens)
	node := model.ExprNode{NOT: &model.ExprNode{Src: ptr[int64](1)}}
	result, err := c.Compile(node, mockResolver(testSources))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result != "-from:BBCPersian" {
		t.Errorf("got %q, want -from:BBCPersian", result)
	}
}

func TestCompileNOTOfOR(t *testing.T) {
	c := stream.NewRuleCompiler()
	// NOT of OR → -(a OR b) (parens around OR child)
	node := model.ExprNode{NOT: &model.ExprNode{OR: []model.ExprNode{{Src: ptr[int64](1)}, {Src: ptr[int64](2)}}}}
	result, err := c.Compile(node, mockResolver(testSources))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result != "-(from:BBCPersian OR from:IranIntl)" {
		t.Errorf("got %q", result)
	}
}

func TestCompileNOTOfAND(t *testing.T) {
	c := stream.NewRuleCompiler()
	// NOT of AND → -(a b) (parens around AND child)
	node := model.ExprNode{NOT: &model.ExprNode{AND: []model.ExprNode{{Src: ptr[int64](1)}, {Src: ptr[int64](3)}}}}
	result, err := c.Compile(node, mockResolver(testSources))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result != "-(from:BBCPersian ایران)" {
		t.Errorf("got %q", result)
	}
}

func TestCompileNOTOfInlineOp(t *testing.T) {
	c := stream.NewRuleCompiler()
	// NOT of inline op → -is:retweet (no parens)
	node := model.ExprNode{NOT: &model.ExprNode{Op: "is:retweet"}}
	result, err := c.Compile(node, mockResolver(testSources))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result != "-is:retweet" {
		t.Errorf("got %q", result)
	}
}

func TestCompileComplex(t *testing.T) {
	c := stream.NewRuleCompiler()
	// (from:BBCPersian OR from:IranIntl) lang:fa -is:retweet
	node := model.ExprNode{AND: []model.ExprNode{
		{OR: []model.ExprNode{{Src: ptr[int64](1)}, {Src: ptr[int64](2)}}},
		{Src: ptr[int64](4)},
		{NOT: &model.ExprNode{Op: "is:retweet"}},
	}}
	result, err := c.Compile(node, mockResolver(testSources))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result != "(from:BBCPersian OR from:IranIntl) lang:fa -is:retweet" {
		t.Errorf("got %q", result)
	}
}

func TestCompileMissingSource(t *testing.T) {
	c := stream.NewRuleCompiler()
	_, err := c.Compile(model.ExprNode{Src: ptr[int64](99)}, mockResolver(testSources))
	if err == nil {
		t.Error("expected error for missing source")
	}
}
```

- [ ] **Step 2: Run tests to confirm they fail**

```bash
go test ./internal/stream/... -run "TestCompile" -v
```

- [ ] **Step 3: Create `internal/stream/compiler.go`**

```go
package stream

import (
	"fmt"
	"strings"

	"github.com/amir-the-h/iran-watcher/internal/model"
)

// SourceResolver looks up a Source by ID.
type SourceResolver func(id int64) (*model.Source, error)

// RuleCompiler compiles an ExprNode tree into an X Filtered Stream rule string.
// It is pure (no I/O) and safe to use concurrently.
type RuleCompiler struct{}

// NewRuleCompiler returns a new RuleCompiler.
func NewRuleCompiler() *RuleCompiler { return &RuleCompiler{} }

// Compile walks node and returns the X rule syntax string.
func (c *RuleCompiler) Compile(node model.ExprNode, resolve SourceResolver) (string, error) {
	return c.compile(node, resolve)
}

func (c *RuleCompiler) compile(node model.ExprNode, resolve SourceResolver) (string, error) {
	switch {
	case node.Src != nil:
		src, err := resolve(*node.Src)
		if err != nil {
			return "", fmt.Errorf("compiler: resolve source %d: %w", *node.Src, err)
		}
		return src.Render(), nil

	case node.Op != "":
		return node.Op, nil

	case len(node.AND) > 0:
		parts := make([]string, 0, len(node.AND))
		for _, child := range node.AND {
			s, err := c.compile(child, resolve)
			if err != nil {
				return "", err
			}
			parts = append(parts, s)
		}
		return strings.Join(parts, " "), nil

	case len(node.OR) > 0:
		parts := make([]string, 0, len(node.OR))
		for _, child := range node.OR {
			s, err := c.compile(child, resolve)
			if err != nil {
				return "", err
			}
			parts = append(parts, s)
		}
		return "(" + strings.Join(parts, " OR ") + ")", nil

	case node.NOT != nil:
		inner, err := c.compile(*node.NOT, resolve)
		if err != nil {
			return "", err
		}
		// Wrap in parens only if child is AND or OR (compound).
		if len(node.NOT.AND) > 0 || len(node.NOT.OR) > 0 {
			return "-(" + inner + ")", nil
		}
		return "-" + inner, nil

	default:
		return "", fmt.Errorf("compiler: empty node")
	}
}
```

- [ ] **Step 4: Run tests**

```bash
go test ./internal/stream/... -run "TestCompile" -v
```
Expected: all PASS

- [ ] **Step 5: Commit**

```bash
git add internal/stream/compiler.go internal/stream/compiler_test.go
git commit -m "feat(stream): add RuleCompiler — expression tree to X syntax string"
```

---

## Task 7: Rule Validator

**Files:**
- Create: `internal/stream/validator.go`
- Create: `internal/stream/validator_test.go`

- [ ] **Step 1: Write failing tests in `validator_test.go`**

```go
package stream_test

import (
	"strings"
	"testing"

	"github.com/amir-the-h/iran-watcher/internal/model"
	"github.com/amir-the-h/iran-watcher/internal/stream"
)

func TestValidateOK(t *testing.T) {
	v := stream.NewRuleValidator("basic")
	// Valid: atom + modifier
	node := model.ExprNode{AND: []model.ExprNode{
		{Src: ptr[int64](1)}, // atom: from:BBCPersian
		{Src: ptr[int64](4)}, // modifier: lang:fa
	}}
	if err := v.Validate(node, mockResolver(testSources)); err != nil {
		t.Errorf("expected valid, got: %v", err)
	}
}

func TestValidateModifierOnly(t *testing.T) {
	v := stream.NewRuleValidator("basic")
	// Invalid: only modifier sources
	node := model.ExprNode{Src: ptr[int64](4)} // lang:fa alone
	if err := v.Validate(node, mockResolver(testSources)); err == nil {
		t.Error("expected error for modifier-only rule")
	}
}

func TestValidateInlineModifierOnly(t *testing.T) {
	v := stream.NewRuleValidator("basic")
	// Invalid: only inline modifier op
	node := model.ExprNode{Op: "is:retweet"}
	if err := v.Validate(node, mockResolver(testSources)); err == nil {
		t.Error("expected error for inline modifier-only rule")
	}
}

func TestValidateInlineAtomOnly(t *testing.T) {
	v := stream.NewRuleValidator("basic")
	// Valid: inline non-modifier op
	node := model.ExprNode{Op: "iran"}
	if err := v.Validate(node, mockResolver(testSources)); err != nil {
		t.Errorf("expected valid, got: %v", err)
	}
}

func TestValidateTooLong(t *testing.T) {
	v := stream.NewRuleValidator("basic") // limit 1024
	// Build a node that compiles to >1024 chars
	src := &model.Source{ID: 99, Operator: "from:", Value: strings.Repeat("a", 1020), Class: model.SourceClassAtom}
	sources := map[int64]*model.Source{99: src}
	node := model.ExprNode{Src: ptr[int64](99)}
	if err := v.Validate(node, mockResolver(sources)); err == nil {
		t.Error("expected error for too-long rule")
	}
}

func TestValidateANDRequiresTwoChildren(t *testing.T) {
	v := stream.NewRuleValidator("basic")
	node := model.ExprNode{AND: []model.ExprNode{{Src: ptr[int64](1)}}}
	if err := v.Validate(node, mockResolver(testSources)); err == nil {
		t.Error("expected error for AND with one child")
	}
}

func TestValidateORRequiresTwoChildren(t *testing.T) {
	v := stream.NewRuleValidator("basic")
	node := model.ExprNode{OR: []model.ExprNode{{Src: ptr[int64](1)}}}
	if err := v.Validate(node, mockResolver(testSources)); err == nil {
		t.Error("expected error for OR with one child")
	}
}

func TestValidateMissingSource(t *testing.T) {
	v := stream.NewRuleValidator("basic")
	node := model.ExprNode{Src: ptr[int64](999)}
	if err := v.Validate(node, mockResolver(testSources)); err == nil {
		t.Error("expected error for missing source")
	}
}
```

Note: add `"strings"` import to the test file.

- [ ] **Step 2: Run tests to confirm they fail**

```bash
go test ./internal/stream/... -run "TestValidate" -v
```

- [ ] **Step 3: Create `internal/stream/validator.go`**

```go
package stream

import (
	"fmt"

	"github.com/amir-the-h/iran-watcher/internal/model"
)

// modifierInlineOps is the hardcoded set of inline op values that are
// conjunction-required (modifier) operators in the X API.
var modifierInlineOps = map[string]bool{
	"is:retweet": true, "is:reply": true, "is:quote": true, "is:verified": true,
	"has:hashtags": true, "has:cashtags": true, "has:links": true,
	"has:mentions": true, "has:media": true, "has:images": true,
	"has:video_link": true, "has:geo": true,
}

const (
	maxLengthBasic      = 1024
	maxLengthEnterprise = 2048
)

// RuleValidator validates an ExprNode tree before it is saved and pushed to X.
type RuleValidator struct {
	tier     string
	compiler *RuleCompiler
}

// NewRuleValidator returns a validator for the given tier ("basic" or "enterprise").
func NewRuleValidator(tier string) *RuleValidator {
	return &RuleValidator{tier: tier, compiler: NewRuleCompiler()}
}

// Validate checks the expression tree and returns an error if any rule is violated.
func (v *RuleValidator) Validate(node model.ExprNode, resolve SourceResolver) error {
	// Rule 5 & 6: structural integrity
	if err := validateStructure(node); err != nil {
		return err
	}
	// Rule 4: all src references must resolve
	if err := validateSourceRefs(node, resolve); err != nil {
		return err
	}
	// Rule 2: at least one standalone atom
	if !hasAtom(node, resolve) {
		return fmt.Errorf("rule must contain at least one standalone atom operator (not modifier-only)")
	}
	// Rule 3: compiled length
	compiled, err := v.compiler.Compile(node, resolve)
	if err != nil {
		return fmt.Errorf("validator: compile: %w", err)
	}
	limit := maxLengthBasic
	if v.tier == "enterprise" {
		limit = maxLengthEnterprise
	}
	if len(compiled) > limit {
		return fmt.Errorf("compiled rule length %d exceeds tier limit %d", len(compiled), limit)
	}
	return nil
}

func validateStructure(node model.ExprNode) error {
	if len(node.AND) == 1 {
		return fmt.Errorf("AND requires at least 2 children")
	}
	if len(node.OR) == 1 {
		return fmt.Errorf("OR requires at least 2 children")
	}
	for _, child := range node.AND {
		if err := validateStructure(child); err != nil {
			return err
		}
	}
	for _, child := range node.OR {
		if err := validateStructure(child); err != nil {
			return err
		}
	}
	if node.NOT != nil {
		if err := validateStructure(*node.NOT); err != nil {
			return err
		}
	}
	return nil
}

func validateSourceRefs(node model.ExprNode, resolve SourceResolver) error {
	if node.Src != nil {
		if _, err := resolve(*node.Src); err != nil {
			return fmt.Errorf("source %d not found or inactive: %w", *node.Src, err)
		}
	}
	for _, child := range node.AND {
		if err := validateSourceRefs(child, resolve); err != nil {
			return err
		}
	}
	for _, child := range node.OR {
		if err := validateSourceRefs(child, resolve); err != nil {
			return err
		}
	}
	if node.NOT != nil {
		if err := validateSourceRefs(*node.NOT, resolve); err != nil {
			return err
		}
	}
	return nil
}

// hasAtom returns true if the tree contains at least one atom-class source
// or an inline op that is not in the modifier set.
func hasAtom(node model.ExprNode, resolve SourceResolver) bool {
	if node.Src != nil {
		src, err := resolve(*node.Src)
		if err != nil {
			return false
		}
		return src.Class == model.SourceClassAtom
	}
	if node.Op != "" {
		return !modifierInlineOps[node.Op]
	}
	for _, child := range node.AND {
		if hasAtom(child, resolve) {
			return true
		}
	}
	for _, child := range node.OR {
		if hasAtom(child, resolve) {
			return true
		}
	}
	if node.NOT != nil {
		return hasAtom(*node.NOT, resolve)
	}
	return false
}
```

- [ ] **Step 4: Run tests**

```bash
go test ./internal/stream/... -run "TestValidate" -v
```
Expected: all PASS

- [ ] **Step 5: Run all stream tests**

```bash
go test ./internal/stream/... -v
```

- [ ] **Step 6: Commit**

```bash
git add internal/stream/validator.go internal/stream/validator_test.go
git commit -m "feat(stream): add RuleValidator — enforce atom requirement, structure, and length limits"
```

---

## Task 8: Update Stream Client

**Files:**
- Modify: `internal/stream/client.go`

The stream client's `processEvent` currently looks up `sources` by `x_rule_id` and writes `tweet_sources`. It must now look up `rules` by `x_rule_id` and write `tweet_rules`.

- [ ] **Step 1: Update `processEvent` in `client.go`**

Replace the rule-matching section in `processEvent`:

```go
// 2. For each matching rule, look up the Rule record and write tweet_rules.
for _, rule := range st.MatchingRules {
    r, err := c.store.GetRuleByXRuleID(ctx, rule.ID)
    if err != nil {
        c.logger.Warn("stream: rule lookup failed",
            "component", "stream",
            "x_rule_id", rule.ID,
            "error", err,
        )
        continue
    }
    tr := &model.TweetRule{
        TweetID: st.Tweet.ID,
        RuleID:  r.ID,
    }
    if err := c.store.SaveTweetRule(ctx, tr); err != nil {
        c.logger.Warn("stream: save tweet_rule failed",
            "component", "stream",
            "tweet_id", st.Tweet.ID,
            "rule_id", r.ID,
            "error", err,
        )
    }
}
```

- [ ] **Step 2: Build to confirm no compilation errors**

```bash
go build ./internal/stream/...
```

- [ ] **Step 3: Commit**

```bash
git add internal/stream/client.go
git commit -m "feat(stream): update processEvent to write tweet_rules via rule x_rule_id lookup"
```

---

## Task 9: API — Source Handlers

**Files:**
- Modify: `internal/api/handlers_sources.go`
- Modify: `internal/api/api_test.go`

- [ ] **Step 1: Write failing handler tests in `api_test.go`**

Add the following test cases to the existing test file (follow the existing mock store pattern):

```go
func TestCreateSourceDerivedClassOperator(t *testing.T) {
    // POST /api/sources with type=account should derive class=atom, operator=from:
    srv := newTestServer(t)
    body := `{"type":"account","value":"BBCPersian","name":"BBC Persian"}`
    resp := doRequest(t, srv, http.MethodPost, "/api/sources", body)
    if resp.Code != http.StatusCreated {
        t.Fatalf("expected 201, got %d: %s", resp.Code, resp.Body)
    }
    var src map[string]interface{}
    json.NewDecoder(resp.Body).Decode(&src)
    if src["class"] != "atom" {
        t.Errorf("expected class=atom, got %v", src["class"])
    }
    if src["operator"] != "from:" {
        t.Errorf("expected operator=from:, got %v", src["operator"])
    }
}

func TestDeleteSourceBlockedByRule(t *testing.T) {
    // DELETE /api/sources/:id should return 409 if source is referenced by a rule
    srv := newTestServerWithRefCount(t, 1) // mock returns refcount=1
    resp := doRequest(t, srv, http.MethodDelete, "/api/sources/1", "")
    if resp.Code != http.StatusConflict {
        t.Fatalf("expected 409, got %d", resp.Code)
    }
}

func TestDeactivateSourceBlockedByActiveRule(t *testing.T) {
    // POST /api/sources/:id/deactivate returns 409 if active rule refs exist
    srv := newTestServerWithActiveRefCount(t, 1)
    resp := doRequest(t, srv, http.MethodPost, "/api/sources/1/deactivate", "")
    if resp.Code != http.StatusConflict {
        t.Fatalf("expected 409, got %d", resp.Code)
    }
}
```

Note: update the mock store in `api_test.go` to implement the new `Store` interface methods (`SetSourceActive`, `ActiveRuleRefCount`, `RuleRefCount`, plus all rule/tweet_rules methods). Stub unneeded methods with `panic("not implemented")`.

- [ ] **Step 2: Run tests to confirm they fail**

```bash
go test ./internal/api/... -run "TestCreateSource|TestDeleteSource|TestDeactivateSource" -v
```

The source handlers no longer push to X. `POST /api/sources` creates a local source only. `DELETE /api/sources/:id` checks for rule references instead of deleting an X rule. Add `POST /api/sources/:id/deactivate`.

- [ ] **Step 1: Replace `handlers_sources.go`**

```go
package api

import (
	"encoding/json"
	"net/http"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/amir-the-h/iran-watcher/internal/model"
	"github.com/amir-the-h/iran-watcher/internal/store"
)

func (s *Server) handleListSources(w http.ResponseWriter, r *http.Request) {
	filter := store.SourceFilter{}

	if t := r.URL.Query().Get("type"); t != "" {
		st := model.SourceType(t)
		filter.Type = &st
	}
	if c := r.URL.Query().Get("class"); c != "" {
		sc := model.SourceClass(c)
		filter.Class = &sc
	}
	if a := r.URL.Query().Get("active"); a != "" {
		active, err := strconv.ParseBool(a)
		if err != nil {
			writeError(w, http.StatusBadRequest, "invalid active parameter")
			return
		}
		filter.Active = &active
	}

	sources, err := s.store.ListSources(r.Context(), filter)
	if err != nil {
		s.logger.Error("api: list sources", "error", err)
		writeError(w, http.StatusInternalServerError, "failed to list sources")
		return
	}
	writeJSON(w, http.StatusOK, sources)
}

func (s *Server) handleCreateSource(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Name  string           `json:"name"`
		Type  model.SourceType `json:"type"`
		Value string           `json:"value"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if req.Type == "" {
		writeError(w, http.StatusBadRequest, "type is required")
		return
	}
	if req.Value == "" {
		writeError(w, http.StatusBadRequest, "value is required")
		return
	}
	if req.Name == "" {
		req.Name = req.Value
	}

	info, ok := model.SourceTypeMap[req.Type]
	if !ok {
		writeError(w, http.StatusBadRequest, "unknown source type")
		return
	}

	now := time.Now().UTC()
	src := &model.Source{
		Name:      req.Name,
		Type:      req.Type,
		Class:     info.Class,
		Operator:  info.Operator,
		Value:     req.Value,
		Active:    true,
		CreatedAt: now,
		UpdatedAt: now,
	}

	if err := s.store.SaveSource(r.Context(), src); err != nil {
		s.logger.Error("api: save source", "error", err)
		writeError(w, http.StatusInternalServerError, "failed to save source")
		return
	}

	writeJSON(w, http.StatusCreated, src)
}

func (s *Server) handleDeleteSource(w http.ResponseWriter, r *http.Request) {
	idStr := chi.URLParam(r, "id")
	id, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid source ID")
		return
	}

	if _, err := s.store.GetSource(r.Context(), id); err != nil {
		writeError(w, http.StatusNotFound, "source not found")
		return
	}

	count, err := s.store.RuleRefCount(r.Context(), id)
	if err != nil {
		s.logger.Error("api: rule ref count", "source_id", id, "error", err)
		writeError(w, http.StatusInternalServerError, "failed to check rule references")
		return
	}
	if count > 0 {
		writeError(w, http.StatusConflict, "source is referenced by one or more rules; remove those rules first")
		return
	}

	if err := s.store.DeleteSource(r.Context(), id); err != nil {
		s.logger.Error("api: delete source", "source_id", id, "error", err)
		writeError(w, http.StatusInternalServerError, "failed to delete source")
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handleDeactivateSource(w http.ResponseWriter, r *http.Request) {
	idStr := chi.URLParam(r, "id")
	id, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid source ID")
		return
	}

	if _, err := s.store.GetSource(r.Context(), id); err != nil {
		writeError(w, http.StatusNotFound, "source not found")
		return
	}

	count, err := s.store.ActiveRuleRefCount(r.Context(), id)
	if err != nil {
		s.logger.Error("api: active rule ref count", "source_id", id, "error", err)
		writeError(w, http.StatusInternalServerError, "failed to check active rule references")
		return
	}
	if count > 0 {
		writeError(w, http.StatusConflict, "source is referenced by one or more active rules; deactivate those rules first")
		return
	}

	if err := s.store.SetSourceActive(r.Context(), id, false); err != nil {
		s.logger.Error("api: deactivate source", "source_id", id, "error", err)
		writeError(w, http.StatusInternalServerError, "failed to deactivate source")
		return
	}

	w.WriteHeader(http.StatusNoContent)
}
```

- [ ] **Step 3: Build**

```bash
go build ./internal/api/...
```

- [ ] **Step 4: Run source handler tests**

```bash
go test ./internal/api/... -run "TestCreateSource|TestDeleteSource|TestDeactivateSource" -v
```
Expected: all PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/api/handlers_sources.go internal/api/api_test.go
git commit -m "feat(api): update source handlers — remove X push, add type-derived class/operator, add deactivate"
```

---

## Task 10: API Server — Wire Routes + Tier + Resolver

**Files:**
- Modify: `internal/api/server.go`
- Modify: `internal/api/handlers_tweets.go`
- Modify: `cli/cmd/serve.go`

> **Must be done before Task 11.** `handlers_rules.go` references `s.tier` and `s.makeResolver`, which are added here.

- [ ] **Step 1: Update `server.go`**

Add `tier` field to `Server`, add `makeResolver` helper, register rule routes, add `source deactivate` route. Also add `"fmt"` to the import block.

```go
// In Server struct, add:
tier string // "basic" or "enterprise"

// In NewServer signature, add tier parameter (last before logger):
func NewServer(bind string, s store.Store, q queue.Producer, reg *inference.Registry, rules ruleManagerInterface, tier string, logger *slog.Logger) *Server

// In NewServer body, assign:
srv.tier = tier

// makeResolver returns a SourceResolver backed by the store,
// returning only active sources (matches validation rule 4).
func (s *Server) makeResolver(r *http.Request) stream.SourceResolver {
	return func(id int64) (*model.Source, error) {
		src, err := s.store.GetSource(r.Context(), id)
		if err != nil {
			return nil, err
		}
		if !src.Active {
			return nil, fmt.Errorf("source %d is inactive", id)
		}
		return src, nil
	}
}
```

Update the sources route block in `NewServer` to add the deactivate route:
```go
// Sources
r.Get("/sources", srv.handleListSources)
r.Post("/sources", srv.handleCreateSource)
r.Delete("/sources/{id}", srv.handleDeleteSource)
r.Post("/sources/{id}/deactivate", srv.handleDeactivateSource)
```

Do NOT add rule routes yet — those handler methods don't exist until Task 11. Rule routes are registered in Task 11 Step 4.

Update `NewServerWithConcreteRules` signature to accept `tier string` and pass it through to `NewServer`.

- [ ] **Step 2: Update `handlers_tweets.go`**

Replace `source_id` filter with `rule_id`:

```go
if v := q.Get("rule_id"); v != "" {
    id, err := strconv.ParseInt(v, 10, 64)
    if err != nil {
        writeError(w, http.StatusBadRequest, "invalid rule_id")
        return
    }
    filter.RuleID = &id
}
```

Remove the old `source_id` block.

- [ ] **Step 3: Update `cli/cmd/serve.go`**

`NewServerWithConcreteRules` now requires a `tier string` argument. Pass `cfg.X.Tier`:

```go
apiSrv := api.NewServerWithConcreteRules(cfg.API.Bind, s, q, reg, rules, cfg.X.Tier, logger)
```

- [ ] **Step 4: Build**

```bash
go build ./...
```
Expected: clean build. Rule routes are NOT registered here — handler methods don't exist until Task 11.

- [ ] **Step 5: Commit**

```bash
git add internal/api/server.go internal/api/handlers_tweets.go cli/cmd/serve.go
git commit -m "feat(api): add tier+makeResolver to Server, source deactivate route, rule_id tweet filter"
```

---

## Task 11: API — Rule Handlers

**Files:**
- Create: `internal/api/handlers_rules.go`
- Modify: `internal/api/api_test.go`

- [ ] **Step 1: Write failing rule handler tests in `api_test.go`**

Add the following test cases (using the mock store from Task 9):

```go
func TestCreateRuleValidates(t *testing.T) {
    // POST /api/rules with modifier-only expression should return 422
    srv := newTestServer(t)
    body := `{"name":"bad rule","expression":{"op":"is:retweet"}}`
    resp := doRequest(t, srv, http.MethodPost, "/api/rules", body)
    if resp.Code != http.StatusUnprocessableEntity {
        t.Fatalf("expected 422, got %d: %s", resp.Code, resp.Body)
    }
}

func TestDeleteRuleBlockedByTweetHistory(t *testing.T) {
    // DELETE /api/rules/:id returns 409 if tweet_rules rows exist
    srv := newTestServerWithTweetRuleCount(t, 1)
    resp := doRequest(t, srv, http.MethodDelete, "/api/rules/1", "")
    if resp.Code != http.StatusConflict {
        t.Fatalf("expected 409, got %d", resp.Code)
    }
}

func TestDeactivateRuleRemovesXRule(t *testing.T) {
    // POST /api/rules/:id/deactivate calls rules.DeleteRule and clears x_rule_id
    srv, mockRules := newTestServerWithMockRules(t)
    // seed an active rule in the mock store
    resp := doRequest(t, srv, http.MethodPost, "/api/rules/1/deactivate", "")
    if resp.Code != http.StatusNoContent {
        t.Fatalf("expected 204, got %d: %s", resp.Code, resp.Body)
    }
    if !mockRules.DeleteRuleCalled {
        t.Error("expected DeleteRule to be called on X rule manager")
    }
}

func TestUpdateActiveRuleReplacesXRule(t *testing.T) {
    // PUT /api/rules/:id with new expression on active rule: delete + recreate X rule
    srv, mockRules := newTestServerWithMockRules(t)
    body := `{"expression":{"src":1}}`
    resp := doRequest(t, srv, http.MethodPut, "/api/rules/1", body)
    if resp.Code != http.StatusOK {
        t.Fatalf("expected 200, got %d: %s", resp.Code, resp.Body)
    }
    if !mockRules.DeleteRuleCalled || !mockRules.AddRuleCalled {
        t.Error("expected both DeleteRule and AddRule to be called for active rule update")
    }
}
```

- [ ] **Step 2: Run tests to confirm they fail**

```bash
go test ./internal/api/... -run "TestCreateRule|TestDeleteRule|TestDeactivateRule|TestUpdateActiveRule" -v
```

- [ ] **Step 3: Write the rule handlers**

```go
package api

import (
	"encoding/json"
	"net/http"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/amir-the-h/iran-watcher/internal/model"
	"github.com/amir-the-h/iran-watcher/internal/store"
	"github.com/amir-the-h/iran-watcher/internal/stream"
)

func (s *Server) handleListRules(w http.ResponseWriter, r *http.Request) {
	filter := store.RuleFilter{}
	if a := r.URL.Query().Get("active"); a != "" {
		active, err := strconv.ParseBool(a)
		if err != nil {
			writeError(w, http.StatusBadRequest, "invalid active parameter")
			return
		}
		filter.Active = &active
	}
	rules, err := s.store.ListRules(r.Context(), filter)
	if err != nil {
		s.logger.Error("api: list rules", "error", err)
		writeError(w, http.StatusInternalServerError, "failed to list rules")
		return
	}
	writeJSON(w, http.StatusOK, rules)
}

func (s *Server) handleGetRule(w http.ResponseWriter, r *http.Request) {
	id, err := parseID(r)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid rule ID")
		return
	}
	rule, err := s.store.GetRule(r.Context(), id)
	if err != nil {
		writeError(w, http.StatusNotFound, "rule not found")
		return
	}
	writeJSON(w, http.StatusOK, rule)
}

func (s *Server) handleCreateRule(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Name       string          `json:"name"`
		Expression json.RawMessage `json:"expression"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if req.Name == "" {
		writeError(w, http.StatusBadRequest, "name is required")
		return
	}
	if len(req.Expression) == 0 {
		writeError(w, http.StatusBadRequest, "expression is required")
		return
	}

	var node model.ExprNode
	if err := json.Unmarshal(req.Expression, &node); err != nil {
		writeError(w, http.StatusBadRequest, "invalid expression JSON")
		return
	}

	resolver := s.makeResolver(r)
	validator := stream.NewRuleValidator(s.tier)
	if err := validator.Validate(node, resolver); err != nil {
		writeError(w, http.StatusUnprocessableEntity, err.Error())
		return
	}

	compiler := stream.NewRuleCompiler()
	compiled, err := compiler.Compile(node, resolver)
	if err != nil {
		writeError(w, http.StatusUnprocessableEntity, err.Error())
		return
	}

	now := time.Now().UTC()
	rule := &model.Rule{
		Name:          req.Name,
		Expression:    req.Expression,
		CompiledValue: compiled,
		Active:        false,
		CreatedAt:     now,
		UpdatedAt:     now,
	}

	if err := s.store.SaveRule(r.Context(), rule); err != nil {
		s.logger.Error("api: save rule", "error", err)
		writeError(w, http.StatusInternalServerError, "failed to save rule")
		return
	}

	// Push to X and activate.
	xRuleID, err := s.rules.AddRule(r.Context(), rule.Name, compiled)
	if err != nil {
		s.logger.Error("api: add x rule", "rule_id", rule.ID, "error", err)
		writeError(w, http.StatusInternalServerError, "failed to add stream rule to X")
		return
	}
	if err := s.store.SetRuleActive(r.Context(), rule.ID, true, xRuleID); err != nil {
		s.logger.Error("api: set rule active", "rule_id", rule.ID, "error", err)
		writeError(w, http.StatusInternalServerError, "failed to activate rule")
		return
	}
	rule.XRuleID = xRuleID
	rule.Active = true

	writeJSON(w, http.StatusCreated, rule)
}

func (s *Server) handleUpdateRule(w http.ResponseWriter, r *http.Request) {
	id, err := parseID(r)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid rule ID")
		return
	}

	existing, err := s.store.GetRule(r.Context(), id)
	if err != nil {
		writeError(w, http.StatusNotFound, "rule not found")
		return
	}

	var req struct {
		Name       *string         `json:"name"`
		Expression json.RawMessage `json:"expression"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	if req.Name != nil {
		existing.Name = *req.Name
	}

	expressionChanged := false
	if len(req.Expression) > 0 {
		var node model.ExprNode
		if err := json.Unmarshal(req.Expression, &node); err != nil {
			writeError(w, http.StatusBadRequest, "invalid expression JSON")
			return
		}

		resolver := s.makeResolver(r)
		validator := stream.NewRuleValidator(s.tier)
		if err := validator.Validate(node, resolver); err != nil {
			writeError(w, http.StatusUnprocessableEntity, err.Error())
			return
		}

		compiler := stream.NewRuleCompiler()
		compiled, err := compiler.Compile(node, resolver)
		if err != nil {
			writeError(w, http.StatusUnprocessableEntity, err.Error())
			return
		}

		existing.Expression = req.Expression
		existing.CompiledValue = compiled
		expressionChanged = true
	}

	// If active and expression changed: delete old X rule + create new.
	if existing.Active && expressionChanged {
		if existing.XRuleID != "" {
			if err := s.rules.DeleteRule(r.Context(), existing.XRuleID); err != nil {
				s.logger.Error("api: delete old x rule on update", "rule_id", id, "error", err)
				writeError(w, http.StatusInternalServerError, "failed to update stream rule on X")
				return
			}
		}
		xRuleID, err := s.rules.AddRule(r.Context(), existing.Name, existing.CompiledValue)
		if err != nil {
			s.logger.Error("api: add new x rule on update", "rule_id", id, "error", err)
			writeError(w, http.StatusInternalServerError, "failed to update stream rule on X")
			return
		}
		existing.XRuleID = xRuleID
	}

	if err := s.store.UpdateRule(r.Context(), existing); err != nil {
		s.logger.Error("api: update rule", "rule_id", id, "error", err)
		writeError(w, http.StatusInternalServerError, "failed to update rule")
		return
	}

	// If active and expression changed, sync the active flag + new x_rule_id.
	if existing.Active && expressionChanged {
		if err := s.store.SetRuleActive(r.Context(), id, true, existing.XRuleID); err != nil {
			s.logger.Error("api: set rule active after update", "rule_id", id, "error", err)
			writeError(w, http.StatusInternalServerError, "failed to sync rule x_rule_id")
			return
		}
	}

	writeJSON(w, http.StatusOK, existing)
}

func (s *Server) handleDeleteRule(w http.ResponseWriter, r *http.Request) {
	id, err := parseID(r)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid rule ID")
		return
	}

	rule, err := s.store.GetRule(r.Context(), id)
	if err != nil {
		writeError(w, http.StatusNotFound, "rule not found")
		return
	}

	count, err := s.store.TweetRuleCount(r.Context(), id)
	if err != nil {
		s.logger.Error("api: tweet rule count", "rule_id", id, "error", err)
		writeError(w, http.StatusInternalServerError, "failed to check tweet history")
		return
	}
	if count > 0 {
		writeError(w, http.StatusConflict, "rule has tweet history; deactivate it instead of deleting")
		return
	}

	if rule.Active && rule.XRuleID != "" {
		if err := s.rules.DeleteRule(r.Context(), rule.XRuleID); err != nil {
			s.logger.Error("api: delete x rule", "rule_id", id, "error", err)
			writeError(w, http.StatusInternalServerError, "failed to delete stream rule from X")
			return
		}
	}

	if err := s.store.DeleteRule(r.Context(), id); err != nil {
		s.logger.Error("api: delete rule", "rule_id", id, "error", err)
		writeError(w, http.StatusInternalServerError, "failed to delete rule")
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handleActivateRule(w http.ResponseWriter, r *http.Request) {
	id, err := parseID(r)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid rule ID")
		return
	}

	rule, err := s.store.GetRule(r.Context(), id)
	if err != nil {
		writeError(w, http.StatusNotFound, "rule not found")
		return
	}
	if rule.Active {
		writeJSON(w, http.StatusOK, rule)
		return
	}

	// Recompile fresh to validate sources are still active.
	var node model.ExprNode
	if err := json.Unmarshal(rule.Expression, &node); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to parse stored expression")
		return
	}
	resolver := s.makeResolver(r)
	validator := stream.NewRuleValidator(s.tier)
	if err := validator.Validate(node, resolver); err != nil {
		writeError(w, http.StatusUnprocessableEntity, "rule is no longer valid: "+err.Error())
		return
	}
	compiler := stream.NewRuleCompiler()
	compiled, err := compiler.Compile(node, resolver)
	if err != nil {
		writeError(w, http.StatusUnprocessableEntity, err.Error())
		return
	}

	xRuleID, err := s.rules.AddRule(r.Context(), rule.Name, compiled)
	if err != nil {
		s.logger.Error("api: activate rule — add x rule", "rule_id", id, "error", err)
		writeError(w, http.StatusInternalServerError, "failed to add stream rule to X")
		return
	}
	if err := s.store.SetRuleActive(r.Context(), id, true, xRuleID); err != nil {
		s.logger.Error("api: activate rule — set active", "rule_id", id, "error", err)
		writeError(w, http.StatusInternalServerError, "failed to activate rule")
		return
	}
	rule.Active = true
	rule.XRuleID = xRuleID

	writeJSON(w, http.StatusOK, rule)
}

func (s *Server) handleDeactivateRule(w http.ResponseWriter, r *http.Request) {
	id, err := parseID(r)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid rule ID")
		return
	}

	rule, err := s.store.GetRule(r.Context(), id)
	if err != nil {
		writeError(w, http.StatusNotFound, "rule not found")
		return
	}
	if !rule.Active {
		w.WriteHeader(http.StatusNoContent)
		return
	}

	if rule.XRuleID != "" {
		if err := s.rules.DeleteRule(r.Context(), rule.XRuleID); err != nil {
			s.logger.Error("api: deactivate rule — delete x rule", "rule_id", id, "error", err)
			writeError(w, http.StatusInternalServerError, "failed to remove stream rule from X")
			return
		}
	}
	if err := s.store.SetRuleActive(r.Context(), id, false, ""); err != nil {
		s.logger.Error("api: deactivate rule — set inactive", "rule_id", id, "error", err)
		writeError(w, http.StatusInternalServerError, "failed to deactivate rule")
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

// parseID extracts and parses the {id} URL parameter.
func parseID(r *http.Request) (int64, error) {
	return strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
}
```

- [ ] **Step 4: Register rule routes in `server.go`**

Now that the handler methods exist, add the rule routes to `NewServer` in `server.go`:

```go
// Rules — add after the Sources block
r.Get("/rules", srv.handleListRules)
r.Post("/rules", srv.handleCreateRule)
r.Get("/rules/{id}", srv.handleGetRule)
r.Put("/rules/{id}", srv.handleUpdateRule)
r.Delete("/rules/{id}", srv.handleDeleteRule)
r.Post("/rules/{id}/activate", srv.handleActivateRule)
r.Post("/rules/{id}/deactivate", srv.handleDeactivateRule)
```

- [ ] **Step 5: Build**

```bash
go build ./...
```
Expected: clean build.

- [ ] **Step 6: Run rule handler tests**

```bash
go test ./internal/api/... -run "TestCreateRule|TestDeleteRule|TestDeactivateRule|TestUpdateActiveRule" -v
```
Expected: all PASS.

- [ ] **Step 7: Commit**

```bash
git add internal/api/handlers_rules.go internal/api/api_test.go internal/api/server.go
git commit -m "feat(api): add rule CRUD handlers with create/update/activate/deactivate/delete; register rule routes"
```

---

## Task 12: CLI — Update Source Command

**Files:**
- Modify: `cli/cmd/source.go`

- [ ] **Step 1: Update `source.go`**

Replace the `source add` command body. The key change: use `--type` (unchanged flag name, but now maps to the full type set), add `--name`, add `--no-active`. The type determines class/operator server-side.

```go
var (
	sourceName   string
	sourceType   string
	sourceValue  string
	sourceActive bool
)

var sourceAddCmd = &cobra.Command{
	Use:   "add",
	Short: "Add a new source",
	RunE: func(cmd *cobra.Command, args []string) error {
		body := map[string]interface{}{
			"name":  sourceName,
			"type":  sourceType,
			"value": sourceValue,
		}
		// ... (same HTTP POST pattern as before)
	},
}

func init() {
	sourceAddCmd.Flags().StringVar(&sourceName, "name", "", "Human-readable label (defaults to value)")
	sourceAddCmd.Flags().StringVar(&sourceType, "type", "", "Source type (account|keyword|hashtag|cashtag|mention|phrase|url|url_contains|to_account|retweets_of|context|entity|conversation_id|place|place_country|point_radius|bounding_box|lang|is|has|followers_count|tweets_count|following_count|listed_count|sample)")
	sourceAddCmd.Flags().StringVar(&sourceValue, "value", "", "Source value")
	sourceAddCmd.Flags().BoolVar(&sourceActive, "active", true, "Create as active (use --no-active to create inactive)")
	_ = sourceAddCmd.MarkFlagRequired("type")
	_ = sourceAddCmd.MarkFlagRequired("value")
	// ...
}
```

Update `source list` table to show `NAME`, `CLASS`, `TYPE`, `VALUE` columns.

- [ ] **Step 2: Build**

```bash
go build ./cli/...
```

- [ ] **Step 3: Commit**

```bash
git add cli/cmd/source.go
git commit -m "feat(cli): update source add — --name, full --type set, --no-active flag; update list columns"
```

---

## Task 13: CLI — Rule Command

**Files:**
- Create: `cli/cmd/rule.go`

- [ ] **Step 1: Create `rule.go`**

```go
package cmd

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"text/tabwriter"

	"github.com/spf13/cobra"
)

var ruleCmd = &cobra.Command{
	Use:   "rule",
	Short: "Manage stream filter rules",
}

var ruleListCmd = &cobra.Command{
	Use:   "list",
	Short: "List all rules",
	RunE: func(cmd *cobra.Command, args []string) error {
		resp, err := http.Get(baseURL + "/api/rules")
		if err != nil {
			return fmt.Errorf("rule list: %w", err)
		}
		defer resp.Body.Close()
		if err := handleAPIError(cmd, resp); err != nil {
			return err
		}
		var rules []map[string]interface{}
		if err := json.NewDecoder(resp.Body).Decode(&rules); err != nil {
			return fmt.Errorf("rule list: decode: %w", err)
		}
		w := tabwriter.NewWriter(cmd.OutOrStdout(), 0, 0, 2, ' ', 0)
		fmt.Fprintln(w, "ID\tNAME\tACTIVE\tX_RULE_ID\tCOMPILED")
		for _, r := range rules {
			fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\n",
				formatField(r["id"]),
				formatField(r["name"]),
				formatField(r["active"]),
				formatField(r["x_rule_id"]),
				formatField(r["compiled_value"]),
			)
		}
		return w.Flush()
	},
}

var ruleShowCmd = &cobra.Command{
	Use:   "show <id>",
	Short: "Show a rule with expression and compiled syntax",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		id := url.PathEscape(args[0])
		resp, err := http.Get(baseURL + "/api/rules/" + id)
		if err != nil {
			return fmt.Errorf("rule show: %w", err)
		}
		defer resp.Body.Close()
		if err := handleAPIError(cmd, resp); err != nil {
			return err
		}
		var rule map[string]interface{}
		if err := json.NewDecoder(resp.Body).Decode(&rule); err != nil {
			return fmt.Errorf("rule show: decode: %w", err)
		}
		exprBytes, _ := json.MarshalIndent(rule["expression"], "  ", "  ")
		fmt.Fprintf(cmd.OutOrStdout(), "Name:     %s\nActive:   %s\nX Rule:   %s\n\nExpression:\n  %s\n\nCompiled:\n  %s\n",
			formatField(rule["name"]),
			formatField(rule["active"]),
			formatField(rule["x_rule_id"]),
			string(exprBytes),
			formatField(rule["compiled_value"]),
		)
		return nil
	},
}

var (
	ruleExprStr  string
	ruleExprFile string
	ruleName     string
)

func parseRuleExpr(exprStr, exprFile string) (json.RawMessage, error) {
	if exprStr != "" {
		return json.RawMessage(exprStr), nil
	}
	if exprFile != "" {
		data, err := os.ReadFile(exprFile)
		if err != nil {
			return nil, fmt.Errorf("read file: %w", err)
		}
		return json.RawMessage(data), nil
	}
	return nil, fmt.Errorf("one of --expr or --file is required")
}

var ruleAddCmd = &cobra.Command{
	Use:   "add <name>",
	Short: "Create a new rule",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		expr, err := parseRuleExpr(ruleExprStr, ruleExprFile)
		if err != nil {
			return fmt.Errorf("rule add: %w", err)
		}
		body := map[string]interface{}{
			"name":       args[0],
			"expression": expr,
		}
		data, err := json.Marshal(body)
		if err != nil {
			return fmt.Errorf("rule add: marshal: %w", err)
		}
		resp, err := http.Post(baseURL+"/api/rules", "application/json", bytes.NewReader(data))
		if err != nil {
			return fmt.Errorf("rule add: %w", err)
		}
		defer resp.Body.Close()
		if err := handleAPIError(cmd, resp); err != nil {
			return err
		}
		var created map[string]interface{}
		if err := json.NewDecoder(resp.Body).Decode(&created); err != nil {
			return fmt.Errorf("rule add: decode: %w", err)
		}
		fmt.Fprintf(cmd.OutOrStdout(), "Rule created: id=%s name=%s compiled=%s\n",
			formatField(created["id"]),
			formatField(created["name"]),
			formatField(created["compiled_value"]),
		)
		return nil
	},
}

var ruleUpdateCmd = &cobra.Command{
	Use:   "update <id>",
	Short: "Update a rule's name or expression",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		id := url.PathEscape(args[0])
		body := map[string]interface{}{}
		if ruleName != "" {
			body["name"] = ruleName
		}
		if ruleExprStr != "" || ruleExprFile != "" {
			expr, err := parseRuleExpr(ruleExprStr, ruleExprFile)
			if err != nil {
				return fmt.Errorf("rule update: %w", err)
			}
			body["expression"] = expr
		}
		data, _ := json.Marshal(body)
		req, _ := http.NewRequest(http.MethodPut, baseURL+"/api/rules/"+id, bytes.NewReader(data))
		req.Header.Set("Content-Type", "application/json")
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			return fmt.Errorf("rule update: %w", err)
		}
		defer resp.Body.Close()
		if err := handleAPIError(cmd, resp); err != nil {
			return err
		}
		fmt.Fprintf(cmd.OutOrStdout(), "Rule %s updated\n", id)
		return nil
	},
}

var ruleRemoveCmd = &cobra.Command{
	Use:   "remove <id>",
	Short: "Delete a rule (only if it has no tweet history)",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		id := url.PathEscape(args[0])
		req, _ := http.NewRequest(http.MethodDelete, baseURL+"/api/rules/"+id, nil)
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			return fmt.Errorf("rule remove: %w", err)
		}
		defer resp.Body.Close()
		if err := handleAPIError(cmd, resp); err != nil {
			return err
		}
		fmt.Fprintf(cmd.OutOrStdout(), "Rule %s removed\n", id)
		return nil
	},
}

var ruleActivateCmd = &cobra.Command{
	Use:   "activate <id>",
	Short: "Activate a rule (push to X)",
	Args:  cobra.ExactArgs(1),
	RunE: ruleLifecycleAction("activate"),
}

var ruleDeactivateCmd = &cobra.Command{
	Use:   "deactivate <id>",
	Short: "Deactivate a rule (remove from X, keep locally)",
	Args:  cobra.ExactArgs(1),
	RunE: ruleLifecycleAction("deactivate"),
}

func ruleLifecycleAction(action string) func(*cobra.Command, []string) error {
	return func(cmd *cobra.Command, args []string) error {
		id := url.PathEscape(args[0])
		resp, err := http.Post(baseURL+"/api/rules/"+id+"/"+action, "application/json", nil)
		if err != nil {
			return fmt.Errorf("rule %s: %w", action, err)
		}
		defer resp.Body.Close()
		if err := handleAPIError(cmd, resp); err != nil {
			return err
		}
		fmt.Fprintf(cmd.OutOrStdout(), "Rule %s %sd\n", id, action)
		return nil
	}
}

func init() {
	ruleAddCmd.Flags().StringVar(&ruleExprStr, "expr", "", "Expression JSON string")
	ruleAddCmd.Flags().StringVar(&ruleExprFile, "file", "", "Path to expression JSON file")

	ruleUpdateCmd.Flags().StringVar(&ruleName, "name", "", "New name")
	ruleUpdateCmd.Flags().StringVar(&ruleExprStr, "expr", "", "New expression JSON string")
	ruleUpdateCmd.Flags().StringVar(&ruleExprFile, "file", "", "Path to new expression JSON file")

	ruleCmd.AddCommand(ruleListCmd)
	ruleCmd.AddCommand(ruleShowCmd)
	ruleCmd.AddCommand(ruleAddCmd)
	ruleCmd.AddCommand(ruleUpdateCmd)
	ruleCmd.AddCommand(ruleRemoveCmd)
	ruleCmd.AddCommand(ruleActivateCmd)
	ruleCmd.AddCommand(ruleDeactivateCmd)
	rootCmd.AddCommand(ruleCmd)
}
```

- [ ] **Step 2: Build**

```bash
go build ./cli/...
```

- [ ] **Step 3: Run all tests**

```bash
go test ./...
```
Expected: all PASS

- [ ] **Step 4: Commit**

```bash
git add cli/cmd/rule.go
git commit -m "feat(cli): add rule subcommand — add/list/show/update/remove/activate/deactivate"
```

---

## Task 14: Final Verification

- [ ] **Step 1: Run full test suite**

```bash
go test ./...
```
Expected: all PASS, no compilation errors.

- [ ] **Step 2: Build the binary**

```bash
go build ./cmd/iran-watcher
go build ./cli/...
```

- [ ] **Step 3: Run integration tests (if Redis available)**

```bash
go test -tags integration ./...
```

- [ ] **Step 4: Final commit**

```bash
git add .
git commit -m "feat: complete rule builder implementation

- Two-layer Source/Rule architecture
- RuleCompiler: ExprNode tree → X syntax (AND/OR/NOT)
- RuleValidator: atom requirement, structure, length limits
- Store: rule CRUD, tweet_rules junction, updated source methods
- API: full rule CRUD with activate/deactivate, source deactivate endpoint
- CLI: rule subcommand, updated source add flags
- Migration 003: schema backfill and rules/tweet_rules tables"
```
