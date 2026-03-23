# Iran Watcher Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use `superpowers:subagent-driven-development` (recommended) or `superpowers:executing-plans` to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Build the iran-watcher binary: X Filtered Stream → Redis queue → SQLite → ONNX inference, with REST API and CLI.

**Architecture:** Wave-based parallel agent execution. Each wave dispatches agents that own exactly one package. Agents never touch files outside their package. Wave N+1 does not start until Wave N is reviewed and interfaces are extracted.

**Tech Stack:** Go 1.25.5, `modernc.org/sqlite`, `github.com/redis/go-redis/v9`, `github.com/yalue/onnxruntime_go`, `github.com/daulet/tokenizers`, `github.com/go-chi/chi/v5`, `github.com/spf13/cobra`, `gopkg.in/yaml.v3`, `github.com/testcontainers/testcontainers-go`

---

## File Map

| File | Responsibility | Wave |
|---|---|---|
| `internal/model/tweet.go` | Tweet, TweetSource domain types | 0 |
| `internal/model/source.go` | Source, SourceType domain types | 0 |
| `internal/model/model.go` | Model (ONNX registry entry) domain type | 0 |
| `internal/model/categorization.go` | Categorization domain type | 0 |
| `internal/config/config.go` | Config struct, YAML loading, env overrides | 0 |
| `migrations/embed.go` | embed.FS for SQL files | 0 |
| `migrations/001_initial.sql` | Full schema: sources, tweets, tweet_sources, categorizations, models | 0 |
| `internal/store/store.go` | Store interface + filter types | 1 |
| `internal/store/sqlite.go` | SQLite implementation of Store | 1 |
| `internal/store/migrations.go` | Migration runner (applies embedded SQL at startup) | 1 |
| `internal/queue/queue.go` | Producer and Consumer interfaces | 1 |
| `internal/queue/redis.go` | Redis implementation (LPUSH/BRPOP) | 1 |
| `internal/inference/pipeline.go` | Pipeline interface | 1 |
| `internal/inference/registry.go` | Model registry: loads active models, hot-reload channel | 1 |
| `internal/inference/onnx.go` | ONNX Pipeline implementation using onnxruntime_go + daulet/tokenizers | 1 |
| `internal/stream/client.go` | X SSE stream connection, reconnect with backoff | 2 |
| `internal/stream/rules.go` | X rule management: sync rules with X API | 2 |
| `internal/stream/normalize.go` | Raw X API JSON → model.Tweet | 2 |
| `internal/worker/pool.go` | Worker pool: start/stop goroutines, reload active models | 2 |
| `internal/worker/worker.go` | Single worker: BRPOP loop → run all pipelines → write categorizations | 2 |
| `internal/api/server.go` | chi router setup, middleware, server start/stop | 3 |
| `internal/api/handlers_sources.go` | GET/POST/DELETE /api/sources (triggers X rule sync) | 3 |
| `internal/api/handlers_tweets.go` | GET /api/tweets, GET /api/tweets/:id, POST /api/tweets/reprocess | 3 |
| `internal/api/handlers_models.go` | GET/POST/DELETE /api/models, activate/deactivate, download | 3 |
| `internal/api/handlers_categorizations.go` | GET /api/categorizations | 3 |
| `internal/api/handlers_health.go` | GET /api/health | 3 |
| `cli/cmd/root.go` | Cobra root command, base URL flag | 4 |
| `cli/cmd/serve.go` | `iran-watcher serve` — launches full app | 4 |
| `cli/cmd/source.go` | `iran-watcher source add/list/remove` | 4 |
| `cli/cmd/model.go` | `iran-watcher model add/list/remove/activate/deactivate` | 4 |
| `cli/cmd/tweet.go` | `iran-watcher tweet list/show/reprocess` | 4 |
| `cli/cmd/health.go` | `iran-watcher health` | 4 |
| `cmd/iran-watcher/main.go` | Lifecycle coordinator: startup order, graceful shutdown | 4 |
| `configs/iran-watcher.yaml` | Default config file | 4 (Agent K — exception: this agent owns both `cmd/iran-watcher/` and `configs/` data dir) |

---

## Wave 0: Foundation (3 agents in parallel)

> No prior context needed. All three agents are independent.

### Task 0.1: Dispatch Wave 0 Agents (parallel)

Dispatch all three agents simultaneously.

---

#### Agent A — `internal/model`

```
You are implementing the domain types package for a Go application called iran-watcher.

**Module:** github.com/amir-the-h/iran-watcher
**Go version:** 1.25.5
**Your package:** internal/model
**Your files (create all of these):**
  - internal/model/tweet.go
  - internal/model/source.go
  - internal/model/model.go
  - internal/model/categorization.go

No external dependencies needed — stdlib only (encoding/json, time).

**Do NOT create or modify any files outside internal/model/.**

---

### Types to implement

**internal/model/source.go**
```go
package model

import "time"

type SourceType string

const (
    SourceTypeAccount SourceType = "account"
    SourceTypeKeyword SourceType = "keyword"
)

type Source struct {
    ID        int64      `json:"id"`
    Type      SourceType `json:"type"`
    Value     string     `json:"value"`
    XRuleID   string     `json:"x_rule_id"`
    Active    bool       `json:"active"`
    CreatedAt time.Time  `json:"created_at"`
    UpdatedAt time.Time  `json:"updated_at"`
}
```

**internal/model/tweet.go**
```go
package model

import "time"

type Tweet struct {
    ID        string    `json:"id"`
    AuthorID  string    `json:"author_id"`
    AuthorHandle string `json:"author_handle"`
    Text      string    `json:"text"`
    RawJSON   []byte    `json:"-"`
    CreatedAt time.Time `json:"created_at"`
    FetchedAt time.Time `json:"fetched_at"`
    // populated on read joins, not stored in tweets table directly
    Sources []*Source `json:"sources,omitempty"`
}

type TweetSource struct {
    TweetID  string `json:"tweet_id"`
    SourceID int64  `json:"source_id"`
}

// MatchingRule represents an X API matching rule reference on incoming stream events.
type MatchingRule struct {
    ID  string `json:"id"`
    Tag string `json:"tag"`
}

// StreamTweet is the in-flight representation used by the stream listener
// before the tweet is persisted. It carries the tweet plus the matching rules
// from the X stream event so tweet_sources rows can be written.
type StreamTweet struct {
    Tweet         *Tweet
    MatchingRules []MatchingRule
}
```

**internal/model/model.go**
```go
package model

import "time"

type ModelType string

const (
    ModelTypeClassification ModelType = "classification"
    ModelTypeNER            ModelType = "ner"
    ModelTypeSentiment      ModelType = "sentiment"
)

type Model struct {
    ID        int64     `json:"id"`
    Name      string    `json:"name"`
    Version   string    `json:"version"`
    Path      string    `json:"path"`
    Type      ModelType `json:"type"`
    Active    bool      `json:"active"`
    CreatedAt time.Time `json:"created_at"`
}
```

**internal/model/categorization.go**
```go
package model

import (
    "encoding/json"
    "time"
)

type Categorization struct {
    ID           int64           `json:"id"`
    TweetID      string          `json:"tweet_id"`
    ModelName    string          `json:"model_name"`
    ModelVersion string          `json:"model_version"`
    Results      json.RawMessage `json:"results"`
    CreatedAt    time.Time       `json:"created_at"`
}
```

---

### Tests

Write a test file `internal/model/model_test.go` that:
1. Verifies Source SourceType constants have the expected string values
2. Verifies Tweet and StreamTweet can be JSON marshaled/unmarshaled without data loss
3. Verifies Categorization.Results preserves raw JSON (no re-encoding artifacts)

### TDD steps
- [ ] Write failing tests in internal/model/model_test.go
- [ ] Run: `go test ./internal/model/...` — expect compile errors (files don't exist yet)
- [ ] Create the four type files with the exact definitions above
- [ ] Run: `go test ./internal/model/...` — all tests must pass
- [ ] Run: `go vet ./internal/model/...` — no issues
- [ ] Commit: `git add internal/model/ && git commit -m "feat: add domain model types"`

### Return
Return the full contents of all four files so the next wave can inject them as context.
```

---

#### Agent B — `internal/config`

```
You are implementing the config package for a Go application called iran-watcher.

**Module:** github.com/amir-the-h/iran-watcher
**Go version:** 1.25.5
**Your package:** internal/config
**Your files (create all of these):**
  - internal/config/config.go

**External dependency to add:** `gopkg.in/yaml.v3`
Run: `go get gopkg.in/yaml.v3`

**Do NOT create or modify any files outside internal/config/.**

---

### Config struct

```go
package config

import (
    "fmt"
    "os"
    "strings"

    "gopkg.in/yaml.v3"
)

type Config struct {
    X struct {
        BearerToken string `yaml:"bearer_token"`
        Tier        string `yaml:"tier"` // "basic" or "pro"
    } `yaml:"x"`
    Redis struct {
        Addr     string `yaml:"addr"`
        Password string `yaml:"password"`
        DB       int    `yaml:"db"`
    } `yaml:"redis"`
    SQLite struct {
        Path string `yaml:"path"`
    } `yaml:"sqlite"`
    API struct {
        Bind string `yaml:"bind"`
    } `yaml:"api"`
    Worker struct {
        Count int `yaml:"count"`
    } `yaml:"worker"`
    Inference struct {
        ONNXLibPath string `yaml:"onnx_lib_path"`
        ModelsDir   string `yaml:"models_dir"`
    } `yaml:"inference"`
    Log struct {
        Level  string `yaml:"level"`
        Format string `yaml:"format"`
    } `yaml:"log"`
}

// Defaults applied before YAML parsing so YAML can override them.
func defaults() *Config {
    c := &Config{}
    c.X.Tier = "basic"
    c.Redis.Addr = "localhost:6379"
    c.Redis.DB = 0
    c.SQLite.Path = "./data/iran-watcher.db"
    c.API.Bind = "127.0.0.1:8080"
    c.Inference.ModelsDir = "./models"
    c.Log.Level = "info"
    c.Log.Format = "text"
    return c
}

// Load reads a YAML config file and overlays IRAN_WATCHER_* env vars.
// Env var format: IRAN_WATCHER_X_BEARER_TOKEN → cfg.X.BearerToken
// Full env var list matches the config table in the design spec.
func Load(path string) (*Config, error) { ... }
```

Implement Load: read YAML file (if exists), then apply env var overrides for each field.
The env var mapping is documented in the design spec; implement it explicitly (no reflection magic).

### Env vars to override (apply after YAML load)
| Env var | Field |
|---|---|
| IRAN_WATCHER_X_BEARER_TOKEN | cfg.X.BearerToken |
| IRAN_WATCHER_X_TIER | cfg.X.Tier |
| IRAN_WATCHER_REDIS_ADDR | cfg.Redis.Addr |
| IRAN_WATCHER_REDIS_PASSWORD | cfg.Redis.Password |
| IRAN_WATCHER_REDIS_DB | cfg.Redis.DB (parse int) |
| IRAN_WATCHER_SQLITE_PATH | cfg.SQLite.Path |
| IRAN_WATCHER_API_BIND | cfg.API.Bind |
| IRAN_WATCHER_WORKER_COUNT | cfg.Worker.Count (parse int) |
| IRAN_WATCHER_ONNX_LIB_PATH | cfg.Inference.ONNXLibPath |
| IRAN_WATCHER_MODELS_DIR | cfg.Inference.ModelsDir |
| IRAN_WATCHER_LOG_LEVEL | cfg.Log.Level |
| IRAN_WATCHER_LOG_FORMAT | cfg.Log.Format |

### Tests (internal/config/config_test.go)

1. Load with missing file returns defaults (no error)
2. Load with a YAML file overrides specified fields, leaves others at defaults
3. Env var IRAN_WATCHER_X_BEARER_TOKEN overrides cfg.X.BearerToken
4. Env var IRAN_WATCHER_REDIS_DB parses integer correctly

### TDD steps
- [ ] Write failing tests
- [ ] Run: `go test ./internal/config/...` — compile error (file missing)
- [ ] Implement config.go
- [ ] Run: `go test ./internal/config/...` — all pass
- [ ] Commit: `git add internal/config/ && git commit -m "feat: add config loading with env overrides"`

### Return
Return the full contents of internal/config/config.go.
```

---

#### Agent C — `migrations/`

```
You are implementing the SQL migrations for a Go application called iran-watcher.

**Module:** github.com/amir-the-h/iran-watcher
**Go version:** 1.25.5
**Your files (create all of these):**
  - migrations/001_initial.sql
  - migrations/embed.go

No external dependencies needed.

**Do NOT create or modify any files outside migrations/.**

---

### migrations/001_initial.sql

Create all five tables and their indexes. Use SQLite syntax.

**sources table**
```sql
CREATE TABLE IF NOT EXISTS sources (
    id         INTEGER PRIMARY KEY AUTOINCREMENT,
    type       TEXT NOT NULL CHECK(type IN ('account','keyword')),
    value      TEXT NOT NULL,
    x_rule_id  TEXT NOT NULL DEFAULT '',
    active     INTEGER NOT NULL DEFAULT 1,
    created_at DATETIME NOT NULL DEFAULT (datetime('now')),
    updated_at DATETIME NOT NULL DEFAULT (datetime('now'))
);
```

**tweets table**
```sql
CREATE TABLE IF NOT EXISTS tweets (
    id            TEXT PRIMARY KEY,
    author_id     TEXT NOT NULL,
    author_handle TEXT NOT NULL,
    text          TEXT NOT NULL,
    raw_json      BLOB NOT NULL,
    created_at    DATETIME NOT NULL,
    fetched_at    DATETIME NOT NULL DEFAULT (datetime('now'))
);
CREATE INDEX IF NOT EXISTS idx_tweets_created_at ON tweets(created_at);
CREATE INDEX IF NOT EXISTS idx_tweets_author_id ON tweets(author_id);
```

**tweet_sources junction table**
```sql
CREATE TABLE IF NOT EXISTS tweet_sources (
    tweet_id  TEXT NOT NULL REFERENCES tweets(id),
    source_id INTEGER NOT NULL REFERENCES sources(id),
    PRIMARY KEY (tweet_id, source_id)
);
CREATE INDEX IF NOT EXISTS idx_tweet_sources_tweet_id ON tweet_sources(tweet_id);
CREATE INDEX IF NOT EXISTS idx_tweet_sources_source_id ON tweet_sources(source_id);
```

**categorizations table**
```sql
CREATE TABLE IF NOT EXISTS categorizations (
    id            INTEGER PRIMARY KEY AUTOINCREMENT,
    tweet_id      TEXT NOT NULL REFERENCES tweets(id),
    model_name    TEXT NOT NULL,
    model_version TEXT NOT NULL,
    results       TEXT NOT NULL,  -- JSON blob
    created_at    DATETIME NOT NULL DEFAULT (datetime('now'))
);
CREATE INDEX IF NOT EXISTS idx_categorizations_tweet_id ON categorizations(tweet_id);
CREATE INDEX IF NOT EXISTS idx_categorizations_model_name ON categorizations(model_name);
CREATE INDEX IF NOT EXISTS idx_categorizations_created_at ON categorizations(created_at);
```

**models table**
```sql
CREATE TABLE IF NOT EXISTS models (
    id         INTEGER PRIMARY KEY AUTOINCREMENT,
    name       TEXT NOT NULL,
    version    TEXT NOT NULL,
    path       TEXT NOT NULL,
    type       TEXT NOT NULL,
    active     INTEGER NOT NULL DEFAULT 0,
    created_at DATETIME NOT NULL DEFAULT (datetime('now'))
);
```

### migrations/embed.go

```go
package migrations

import "embed"

//go:embed *.sql
var FS embed.FS
```

### Tests (migrations/migrations_test.go)

1. FS.ReadFile("001_initial.sql") returns non-empty content
2. The SQL file contains all five expected table names

### TDD steps
- [ ] Write failing test
- [ ] Run: `go test ./migrations/...` — compile error (files missing)
- [ ] Create embed.go and 001_initial.sql
- [ ] Run: `go test ./migrations/...` — all pass
- [ ] Commit: `git add migrations/ && git commit -m "feat: add initial SQLite schema migrations"`

### Return
Return the full contents of both files.
```

---

### Task 0.2: Review Wave 0 Output

- [ ] Read all returned file contents from Agents A, B, C
- [ ] Verify types match the spec's SQL schema (column names align with struct fields)
- [ ] Verify config env var mapping is complete (12 vars)
- [ ] Verify SQL has all 5 tables, correct FKs, correct indexes
- [ ] Run `go build ./...` to confirm no compile errors across the module
- [ ] **Extract and save** the following for injection into Wave 1 prompts:
  - Full contents of `internal/model/tweet.go`, `source.go`, `model.go`, `categorization.go`
  - Full contents of `internal/config/config.go`

---

## Wave 1: Core Services (3 agents in parallel)

> Depends on Wave 0. All three agents receive model types and config as context.

### Task 1.1: Dispatch Wave 1 Agents (parallel)

Dispatch all three agents simultaneously. Each prompt includes the injected model types.

---

#### Agent D — `internal/store`

```
You are implementing the SQLite store package for iran-watcher.

**Module:** github.com/amir-the-h/iran-watcher
**Go version:** 1.25.5
**Your package:** internal/store
**Your files:**
  - internal/store/store.go     — Store interface + filter types
  - internal/store/sqlite.go    — SQLite implementation
  - internal/store/migrations.go — migration runner

**External dependency:** `go get modernc.org/sqlite`

**Do NOT create or modify any files outside internal/store/.**

---

### Injected context: domain types

[INJECT: paste full contents of internal/model/tweet.go here]
[INJECT: paste full contents of internal/model/source.go here]
[INJECT: paste full contents of internal/model/model.go here]
[INJECT: paste full contents of internal/model/categorization.go here]

---

### internal/store/store.go — Store interface

```go
package store

import (
    "context"
    "time"

    "github.com/amir-the-h/iran-watcher/internal/model"
)

type TweetFilter struct {
    SourceID *int64
    From     *time.Time
    To       *time.Time
    Page     int
    PerPage  int
}

type SourceFilter struct {
    Type   *model.SourceType
    Active *bool
}

type CategorizationFilter struct {
    TweetID   *string
    ModelName *string
    From      *time.Time
    To        *time.Time
}

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
    UpdateSourceXRuleID(ctx context.Context, id int64, xRuleID string) error

    // TweetSources
    SaveTweetSource(ctx context.Context, ts *model.TweetSource) error

    // Models
    SaveModel(ctx context.Context, m *model.Model) error
    GetModel(ctx context.Context, id int64) (*model.Model, error)
    ListModels(ctx context.Context) ([]*model.Model, error)
    DeleteModel(ctx context.Context, id int64) error
    SetModelActive(ctx context.Context, id int64, active bool) error

    // Categorizations
    SaveCategorization(ctx context.Context, c *model.Categorization) error
    ListCategorizations(ctx context.Context, filter CategorizationFilter) ([]*model.Categorization, error)
    // Returns tweets that have no categorization row for any of the given model names.
    // If modelNames is empty, returns tweets with zero categorization rows.
    ListUncategorizedTweets(ctx context.Context, modelNames []string) ([]*model.Tweet, error)

    Close() error
}
```

### internal/store/migrations.go

```go
package store

import (
    "database/sql"
    "fmt"
    "io/fs"
    "sort"
)

// RunMigrations applies all .sql files from the provided FS in lexicographic order.
// It uses a simple migrations table to track which have been applied.
func RunMigrations(db *sql.DB, migrationsFS fs.FS) error { ... }
```

### internal/store/sqlite.go

- Open SQLite in WAL mode: `PRAGMA journal_mode=WAL`
- Enable foreign keys: `PRAGMA foreign_keys=ON`
- Implement all Store interface methods
- SaveTweet: INSERT OR IGNORE (idempotent — stream listener may retry)
- ListTweets: dynamic WHERE clause from TweetFilter; default Page=1, PerPage=50
- ListUncategorizedTweets: LEFT JOIN on categorizations, filter NULL or missing model rows

### Tests (internal/store/sqlite_test.go)

Use in-memory SQLite (`:memory:`). Import migrations FS from `github.com/amir-the-h/iran-watcher/migrations`.

1. SaveTweet then GetTweet returns same data
2. SaveTweet is idempotent (second save doesn't error)
3. SaveSource then ListSources with Active=true filter
4. DeleteSource removes the row
5. SaveTweetSource links tweet to source; GetTweet populates Sources
6. SaveModel, SetModelActive(true), ListModels returns it with Active=true
7. SaveCategorization then ListCategorizations by tweet ID
8. ListUncategorizedTweets returns tweet with no categorization rows
9. ListUncategorizedTweets skips tweets that have all requested model names covered
10. RunMigrations is idempotent (run twice, no error)

### TDD steps
- [ ] `go get modernc.org/sqlite`
- [ ] Write failing tests in internal/store/sqlite_test.go
- [ ] Run: `go test ./internal/store/...` — compile errors expected
- [ ] Implement store.go (interface), migrations.go, sqlite.go
- [ ] Run: `go test ./internal/store/...` — all pass
- [ ] Run: `go vet ./internal/store/...` — no issues
- [ ] Commit: `git add internal/store/ && git commit -m "feat: add SQLite store with repository pattern"`

### Return
Return the full contents of internal/store/store.go (the interface file only — implementation details not needed by other agents). Also return the constructor signature for the SQLite implementation (function name + parameter types) so Wave 4's main.go agent knows how to instantiate the store.
```

---

#### Agent E — `internal/queue`

```
You are implementing the Redis queue package for iran-watcher.

**Module:** github.com/amir-the-h/iran-watcher
**Go version:** 1.25.5
**Your package:** internal/queue
**Your files:**
  - internal/queue/queue.go    — interfaces
  - internal/queue/redis.go    — Redis implementation

**External dependency:** `go get github.com/redis/go-redis/v9`

**Do NOT create or modify any files outside internal/queue/.**

**Queue names used by the application:**
- `iran-watcher:raw` — incoming tweets from stream listener
- `iran-watcher:failed` — dead-letter queue for failed inference

---

### Injected context: domain types

[INJECT: paste full contents of internal/model/tweet.go here]

---

### internal/queue/queue.go

```go
package queue

import (
    "context"
    "time"

    "github.com/amir-the-h/iran-watcher/internal/model"
)

const (
    QueueRaw    = "iran-watcher:raw"
    QueueFailed = "iran-watcher:failed"
)

// FailedTweet wraps a StreamTweet with error annotations for dead-letter storage.
type FailedTweet struct {
    Tweet         *model.StreamTweet `json:"tweet"`
    FailedModels  []FailedModel      `json:"failed_models"`
}

type FailedModel struct {
    Name      string `json:"name"`
    Error     string `json:"error"`
    Timestamp string `json:"timestamp"`
}

type Producer interface {
    // Push serializes a StreamTweet to JSON and LPUSHes it onto the named queue.
    Push(ctx context.Context, queue string, tweet *model.StreamTweet) error
    // PushFailed pushes a FailedTweet to the dead-letter queue.
    PushFailed(ctx context.Context, ft *FailedTweet) error
}

type Consumer interface {
    // Pop blocks until a StreamTweet is available or timeout elapses.
    // Returns (nil, nil) on timeout (no message, no error).
    Pop(ctx context.Context, queue string, timeout time.Duration) (*model.StreamTweet, error)
    // PopFailed pops from the dead-letter queue (used by reprocess tooling).
    PopFailed(ctx context.Context, timeout time.Duration) (*FailedTweet, error)
}
```

### internal/queue/redis.go

Implement both interfaces with a single `RedisQueue` struct that satisfies both.
- Push: `LPUSH queue JSON(tweet)`
- Pop: `BRPOP queue timeout` — parse result, return nil on timeout

### Tests

Unit test with a mock Redis (no real Redis needed). Use a fake in-memory store or mock the go-redis Client interface.

1. Push serializes to JSON and calls LPUSH with the correct key
2. Pop deserializes JSON and returns a StreamTweet
3. Pop returns nil, nil on timeout (BRPOP returns empty result)
4. PushFailed includes failed model annotations in the serialized JSON

For integration test (gated behind `//go:build integration`):
- Use `testcontainers-go` with a real Redis container
- Push then Pop round-trips correctly

### TDD steps
- [ ] `go get github.com/redis/go-redis/v9`
- [ ] Write failing unit tests
- [ ] Run: `go test ./internal/queue/...` — compile errors
- [ ] Implement queue.go and redis.go
- [ ] Run: `go test ./internal/queue/...` — all pass
- [ ] Run: `go vet ./internal/queue/...` — no issues
- [ ] Commit: `git add internal/queue/ && git commit -m "feat: add Redis queue producer/consumer"`

### Return
Return the full contents of internal/queue/queue.go (interfaces only).
```

---

#### Agent F — `internal/inference`

```
You are implementing the ONNX inference package for iran-watcher.

**Module:** github.com/amir-the-h/iran-watcher
**Go version:** 1.25.5
**Your package:** internal/inference
**Your files:**
  - internal/inference/pipeline.go  — Pipeline interface
  - internal/inference/registry.go  — Model registry
  - internal/inference/onnx.go      — ONNX Pipeline implementation

**External dependencies:**
  `go get github.com/yalue/onnxruntime_go`
  `go get github.com/daulet/tokenizers`

Note: `daulet/tokenizers` requires CGO enabled and a Rust toolchain. Ensure CGO_ENABLED=1.

**Do NOT create or modify any files outside internal/inference/.**

---

### Injected context: domain types

[INJECT: paste full contents of internal/model/model.go here]

---

### internal/inference/pipeline.go

```go
package inference

import "encoding/json"

// Pipeline is the interface every ONNX model integration must implement.
// Each Pipeline handles its own tokenization and pre/post-processing.
type Pipeline interface {
    // Name returns the model name as registered in the models table.
    Name() string
    // Load initializes the model from the given local .onnx file path.
    Load(modelPath string) error
    // Run takes raw tweet text (UTF-8) and returns model-specific JSON output.
    Run(text []byte) (json.RawMessage, error)
    // RunBatch runs inference on multiple texts. Default implementation loops Run().
    RunBatch(texts [][]byte) ([]json.RawMessage, error)
    // Close releases ONNX Runtime resources.
    Close() error
}
```

### internal/inference/registry.go

```go
package inference

import (
    "context"
    "sync"

    "github.com/amir-the-h/iran-watcher/internal/model"
)

// Registry loads and manages active Pipeline instances.
// Workers call All() to get the current active set.
// When the API activates/deactivates a model, call Reload() to update.
type Registry struct {
    mu       sync.RWMutex
    active   []Pipeline
    reloadCh chan struct{}
}

func NewRegistry() *Registry { ... }

// Load fetches active models from the store and initializes their pipelines.
// factory is a function that creates a Pipeline given a model.Model.
func (r *Registry) Load(ctx context.Context, models []*model.Model, factory func(*model.Model) (Pipeline, error)) error { ... }

// All returns a snapshot of currently active pipelines (safe for concurrent use).
func (r *Registry) All() []Pipeline { ... }

// Reload signals workers to reload the active model set.
// Call this after activating/deactivating a model via the API.
func (r *Registry) Reload(ctx context.Context, models []*model.Model, factory func(*model.Model) (Pipeline, error)) error { ... }

// NotifyChan returns a channel that receives a struct{} whenever Reload is called.
// Workers select on this channel between tweet iterations to pick up model changes.
func (r *Registry) NotifyChan() <-chan struct{} { ... }
```

### internal/inference/onnx.go

Implement an `ONNXPipeline` struct that satisfies `Pipeline`:
- `Load()`: calls `onnxruntime_go` to load the .onnx file; loads tokenizer config from same directory using `daulet/tokenizers`
- `Run()`: tokenize text → run ONNX session → post-process logits into label/score JSON
- `RunBatch()`: loop over Run() (extension point, not optimized initially)
- `Close()`: release ONNX session

Output JSON shape (for classification): `{"label": "...", "score": 0.95}`

### Tests

For unit tests: use a mock Pipeline (implement the interface with a stub).
Test Registry:
1. All() returns empty slice before Load
2. After Load with two mock pipelines, All() returns both
3. Reload() replaces the active set atomically
4. NotifyChan() receives a signal after Reload()

For ONNX integration (gated behind `//go:build integration`):
- Use a trivial ONNX model in `testdata/` — a single-layer identity model is sufficient
- Load it, Run a short string, verify JSON output is non-empty

### TDD steps
- [ ] `go get github.com/yalue/onnxruntime_go && go get github.com/daulet/tokenizers`
- [ ] Write failing unit tests for Registry
- [ ] Run: `go test ./internal/inference/...` — compile errors
- [ ] Implement pipeline.go and registry.go
- [ ] Implement onnx.go (stub Run() to return `{"label":"test","score":1.0}` if ONNX not available in test env)
- [ ] Run: `go test ./internal/inference/...` — all pass
- [ ] Run: `go vet ./internal/inference/...` — no issues
- [ ] Commit: `git add internal/inference/ && git commit -m "feat: add ONNX pipeline interface and registry"`

### Return
Return the full contents of internal/inference/pipeline.go and registry.go (interfaces/structs, not the ONNX implementation).
```

---

### Task 1.2: Review Wave 1 Output

- [ ] Read returned interfaces from Agents D, E, F
- [ ] Verify Store interface covers all queries the API and stream listener will need
- [ ] Verify Queue interfaces cover producer (stream) and consumer (worker) use cases
- [ ] Verify Pipeline and Registry interfaces are sufficient for worker hot-reload
- [ ] Run `go build ./...` to confirm no compile errors
- [ ] **Extract and save** for Wave 2 injection:
  - Full contents of `internal/store/store.go`
  - SQLite store constructor signature (from Agent D's return)
  - Full contents of `internal/queue/queue.go`
  - Full contents of `internal/inference/pipeline.go` and `registry.go`

---

## Wave 2: Pipeline Components (2 agents in parallel)

> Depends on Wave 1. Both agents depend on model types, store, and queue, but own separate packages.

### Task 2.1: Dispatch Wave 2 Agents (parallel)

---

#### Agent G — `internal/stream`

```
You are implementing the X Filtered Stream client package for iran-watcher.

**Module:** github.com/amir-the-h/iran-watcher
**Go version:** 1.25.5
**Your package:** internal/stream
**Your files:**
  - internal/stream/normalize.go — raw X API JSON → model.Tweet
  - internal/stream/rules.go     — X rule sync (add/remove rules via X API)
  - internal/stream/client.go    — SSE stream connection + reconnect loop

**No new external dependencies needed** (stdlib net/http is sufficient for SSE).

**Do NOT create or modify any files outside internal/stream/.**

---

### Injected context: domain types

[INJECT: paste full contents of internal/model/tweet.go here]
[INJECT: paste full contents of internal/model/source.go here]

### Injected context: Store interface

[INJECT: paste full contents of internal/store/store.go here]

### Injected context: Queue interface

[INJECT: paste full contents of internal/queue/queue.go here]

---

### internal/stream/normalize.go

Parse the X API v2 Filtered Stream event format:
```json
{
  "data": {"id":"...","text":"...","author_id":"...","created_at":"..."},
  "includes": {"users":[{"id":"...","username":"..."}]},
  "matching_rules": [{"id":"...","tag":"..."}]
}
```
Return a `*model.StreamTweet`. Set `FetchedAt` to `time.Now()`. Map `raw_json` to the full event bytes.

### internal/stream/rules.go

```go
package stream

import "context"

// RuleManager syncs stream rules with the X Filtered Stream rules API.
type RuleManager struct {
    bearerToken string
    httpClient  *http.Client
}

// AddRule creates a new rule on X's rules API. Returns the assigned X rule ID.
func (rm *RuleManager) AddRule(ctx context.Context, tag, value string) (string, error) { ... }

// DeleteRule removes a rule from X's rules API by X rule ID.
func (rm *RuleManager) DeleteRule(ctx context.Context, xRuleID string) error { ... }

// ListRules returns all current rules from X's rules API.
func (rm *RuleManager) ListRules(ctx context.Context) ([]XRule, error) { ... }

type XRule struct {
    ID    string `json:"id"`
    Value string `json:"value"`
    Tag   string `json:"tag"`
}
```

### internal/stream/client.go

```go
package stream

// Client maintains a persistent connection to the X Filtered Stream.
// On disconnect, it reconnects with exponential backoff (starting at 1s, cap at 5min).
type Client struct {
    bearerToken string
    store       store.Store
    producer    queue.Producer
    httpClient  *http.Client
    logger      *slog.Logger
}

func NewClient(bearerToken string, s store.Store, p queue.Producer, logger *slog.Logger) *Client

// Start begins the stream connection loop. Blocks until ctx is cancelled.
// For each event received:
//   1. Normalize to StreamTweet
//   2. SaveTweet + SaveTweetSource to store (one TweetSource per matching rule, looked up by x_rule_id)
//   3. Push StreamTweet to QueueRaw
func (c *Client) Start(ctx context.Context) error
```

Reconnect logic: On any read error, log at warn level, wait with exponential backoff, retry. Respect ctx cancellation.

### Tests (internal/stream/stream_test.go)

Use `httptest.NewServer` to serve fake SSE events.

1. Normalize() correctly parses a valid X stream event JSON
2. Normalize() returns an error on malformed JSON
3. Client.Start() with a mock SSE server: receives one event, calls store.SaveTweet, store.SaveTweetSource, and queue.Push
4. Client.Start() reconnects after server closes the connection (reconnect detected within 5 seconds)
5. Client.Start() stops cleanly when context is cancelled

Use mock implementations of store.Store and queue.Producer.

### TDD steps
- [ ] Write failing tests using mock store and queue
- [ ] Run: `go test ./internal/stream/...`
- [ ] Implement normalize.go, rules.go, client.go
- [ ] Run: `go test ./internal/stream/...` — all pass
- [ ] Run: `go vet ./internal/stream/...` — no issues
- [ ] Commit: `git add internal/stream/ && git commit -m "feat: add X filtered stream client with reconnect"`

### Return
Return a summary of the Client struct API (exported methods and their signatures).
```

---

#### Agent H — `internal/worker`

```
You are implementing the worker pool package for iran-watcher.

**Module:** github.com/amir-the-h/iran-watcher
**Go version:** 1.25.5
**Your package:** internal/worker
**Your files:**
  - internal/worker/worker.go — single worker loop
  - internal/worker/pool.go   — pool lifecycle + hot-reload

**Do NOT create or modify any files outside internal/worker/.**

---

### Injected context: domain types

[INJECT: paste full contents of internal/model/tweet.go here]
[INJECT: paste full contents of internal/model/categorization.go here]

### Injected context: Store interface

[INJECT: paste full contents of internal/store/store.go here]

### Injected context: Queue interfaces

[INJECT: paste full contents of internal/queue/queue.go here]

### Injected context: Inference interfaces

[INJECT: paste full contents of internal/inference/pipeline.go here]
[INJECT: paste full contents of internal/inference/registry.go here]

---

### internal/worker/worker.go

```go
package worker

// worker is a single goroutine that:
//   1. BRPOPs a StreamTweet from QueueRaw (3s timeout, then loop)
//   2. Runs it through every active Pipeline from the registry
//   3. For each pipeline: writes a Categorization row to store
//   4. If any pipelines failed: PushFailed to dead-letter queue
//   5. Between iterations: check registry.NotifyChan() for reload signal
type worker struct {
    id       int
    consumer queue.Consumer
    producer queue.Producer
    store    store.Store
    registry *inference.Registry
    logger   *slog.Logger
}

func (w *worker) run(ctx context.Context)
```

Processing rules:
- One bad model does NOT stop other models (log error, continue)
- A tweet with partial failures gets written to dead-letter with failed model annotations
- A tweet with ALL models succeeding does NOT go to dead-letter

### internal/worker/pool.go

```go
package worker

// Pool manages N worker goroutines.
type Pool struct {
    count    int
    consumer queue.Consumer
    producer queue.Producer
    store    store.Store
    registry *inference.Registry
    logger   *slog.Logger
}

func NewPool(count int, consumer queue.Consumer, producer queue.Producer, s store.Store, reg *inference.Registry, logger *slog.Logger) *Pool

// Start launches count workers. Blocks until ctx is cancelled, then waits for workers to finish.
func (p *Pool) Start(ctx context.Context) error
```

### Tests (internal/worker/worker_test.go)

Use mock Consumer, Producer, Store, and a stub Pipeline.

1. Worker processes a tweet: calls Pipeline.Run, writes Categorization to store
2. Worker with two pipelines — if one fails, it still runs the second and writes its result
3. Worker with two pipelines — if both fail, it calls PushFailed with both model names annotated
4. Worker with two pipelines — if both succeed, PushFailed is NOT called
5. Pool.Start with count=2 launches two workers (verify via WaitGroup or channel)
6. Pool.Start respects ctx cancellation: returns cleanly when ctx is done

### TDD steps
- [ ] Write failing tests with mocks
- [ ] Run: `go test ./internal/worker/...`
- [ ] Implement worker.go and pool.go
- [ ] Run: `go test ./internal/worker/...` — all pass
- [ ] Run: `go vet ./internal/worker/...` — no issues
- [ ] Commit: `git add internal/worker/ && git commit -m "feat: add worker pool with dead-letter and hot-reload"`

### Return
Return a summary of Pool's exported API (NewPool signature and Start method).
```

---

### Task 2.2: Review Wave 2 Output

- [ ] Verify stream.Client.Start and worker.Pool.Start both use context for shutdown
- [ ] Verify dead-letter logic matches spec (partial failures still write to SQLite)
- [ ] Verify stream client writes tweet before pushing to queue (persistence-first)
- [ ] Run `go build ./...`
- [ ] **Extract and save** for Wave 3 injection:
  - stream.Client constructor signature and exported methods
  - worker.Pool constructor signature and exported methods

---

## Wave 3: REST API (single agent)

> Depends on all prior waves. Single agent to avoid handler file conflicts.

### Task 3.1: Dispatch API Agent

---

#### Agent I — `internal/api`

```
You are implementing the REST API package for iran-watcher.

**Module:** github.com/amir-the-h/iran-watcher
**Go version:** 1.25.5
**Your package:** internal/api
**Your files:**
  - internal/api/server.go                      — chi router, middleware, Server struct
  - internal/api/handlers_sources.go            — /api/sources endpoints
  - internal/api/handlers_tweets.go             — /api/tweets endpoints
  - internal/api/handlers_models.go             — /api/models endpoints
  - internal/api/handlers_categorizations.go    — /api/categorizations
  - internal/api/handlers_health.go             — /api/health

**External dependency:** `go get github.com/go-chi/chi/v5`

**Do NOT create or modify any files outside internal/api/.**

---

### Injected context: domain types

[INJECT: paste all internal/model/*.go contents here]

### Injected context: Store interface

[INJECT: paste full contents of internal/store/store.go here]

### Injected context: Queue interface

[INJECT: paste full contents of internal/queue/queue.go here]

### Injected context: Inference registry

[INJECT: paste full contents of internal/inference/registry.go here]

### Injected context: Stream client API

[INJECT: paste stream.Client exported method signatures here]

### Injected context: Worker pool API

[INJECT: paste worker.Pool exported method signatures here]

---

### internal/api/server.go

```go
package api

type Server struct {
    store    store.Store
    queue    queue.Producer
    registry *inference.Registry
    rules    *stream.RuleManager
    logger   *slog.Logger
    router   chi.Router
    http     *http.Server
}

func NewServer(
    bind string,
    s store.Store,
    q queue.Producer,
    reg *inference.Registry,
    rules *stream.RuleManager,
    logger *slog.Logger,
) *Server

// Start begins listening. Returns when the server is shut down.
func (s *Server) Start(ctx context.Context) error

// Shutdown gracefully closes the HTTP server.
func (s *Server) Shutdown(ctx context.Context) error
```

Middleware: request logging (slog), JSON Content-Type for all responses.

### Endpoints to implement

| Method | Path | Handler | Notes |
|---|---|---|---|
| GET | /api/sources | ListSources | query params: type, active |
| POST | /api/sources | CreateSource | body: {type, value}; triggers AddRule on RuleManager; saves x_rule_id |
| DELETE | /api/sources/{id} | DeleteSource | calls DeleteRule then store.DeleteSource |
| GET | /api/tweets | ListTweets | query params: source_id, from, to, page, per_page |
| GET | /api/tweets/{id} | GetTweet | returns tweet + its categorizations |
| POST | /api/tweets/reprocess | ReprocessTweets | body: {tweet_ids:[], model_names:[]}; re-enqueues to QueueRaw |
| GET | /api/models | ListModels | |
| POST | /api/models | CreateModel | body: {name, version, path, type} |
| DELETE | /api/models/{id} | DeleteModel | |
| POST | /api/models/{id}/activate | ActivateModel | calls store.SetModelActive(true) + registry.Reload() |
| POST | /api/models/{id}/deactivate | DeactivateModel | calls store.SetModelActive(false) + registry.Reload() |
| POST | /api/models/download | DownloadModel | shells out: `hf download {repo_id} --include "*.onnx" --local-dir ./models/{repo_id}` |
| GET | /api/categorizations | ListCategorizations | query params: model_name, tweet_id, from, to |
| GET | /api/health | Health | returns {stream: ok/error, redis: ok/error, sqlite: ok/error, models: [...]} |

**Security:** DownloadModel uses `exec.CommandContext` with a fixed argument list — do NOT build shell strings from user input (command injection risk). Additionally, validate that `repo_id` matches the pattern `^[a-zA-Z0-9_.-]+/[a-zA-Z0-9_.-]+$` before calling exec; return 400 if invalid. This prevents argument injection even without shell involvement.

**ReprocessTweets:** Parse the request body `{tweet_ids: [], model_names: []}`. Call `store.ListUncategorizedTweets(ctx, modelNames)` — pass `model_names` from the request body so only tweets missing results for the requested models are re-enqueued (partial-failure support). For each returned tweet, push a StreamTweet to QueueRaw via producer. Do NOT read from dead-letter queue.

### Error responses

All errors return JSON: `{"error": "message"}` with appropriate HTTP status codes.
- 400 for bad request bodies / invalid params
- 404 for not-found resources
- 500 for internal errors (log the actual error, return generic message)

### Tests (internal/api/api_test.go, one file)

Use `httptest.NewRecorder` and mock store/queue/registry.

1. POST /api/sources creates source, calls RuleManager.AddRule, returns 201
2. DELETE /api/sources/{id} calls RuleManager.DeleteRule and store.DeleteSource
3. GET /api/tweets with pagination params calls ListTweets with correct filter
4. GET /api/tweets/{id} returns tweet with categorizations (join in handler or store)
5. POST /api/tweets/reprocess enqueues the correct tweets
6. POST /api/models/{id}/activate calls store.SetModelActive and registry.Reload
7. POST /api/models/download with a mock exec returns 200 on success
8. GET /api/health returns JSON with all components

### TDD steps
- [ ] `go get github.com/go-chi/chi/v5`
- [ ] Write failing tests with mock store/queue/registry
- [ ] Run: `go test ./internal/api/...`
- [ ] Implement server.go + all handler files
- [ ] Run: `go test ./internal/api/...` — all pass
- [ ] Run: `go vet ./internal/api/...` — no issues
- [ ] Commit: `git add internal/api/ && git commit -m "feat: add REST API with all endpoints"`

### Return
Return the NewServer constructor signature and a list of all registered routes.
```

---

### Task 3.2: Review API Output

- [ ] Verify all 14 endpoints from the spec are present
- [ ] Verify DownloadModel uses `exec.CommandContext` with fixed args (no shell injection)
- [ ] Verify ReprocessTweets queries SQLite, not dead-letter queue
- [ ] Verify activate/deactivate calls registry.Reload
- [ ] Run `go test ./internal/api/...`
- [ ] Run `go build ./...`

---

## Wave 4: Entrypoints (2 agents in parallel)

> Depends on all prior waves. cli/ and cmd/ are independent of each other.

### Task 4.1: Dispatch Wave 4 Agents (parallel)

---

#### Agent J — `cli/`

```
You are implementing the CLI package for iran-watcher.

**Module:** github.com/amir-the-h/iran-watcher
**Go version:** 1.25.5
**Your package:** cli (cobra commands)
**Your files:**
  - cli/cmd/root.go    — root command, --base-url flag (default: http://localhost:8080)
  - cli/cmd/serve.go   — `iran-watcher serve` subcommand
  - cli/cmd/source.go  — `iran-watcher source add/list/remove`
  - cli/cmd/model.go   — `iran-watcher model add/list/remove/activate/deactivate`
  - cli/cmd/tweet.go   — `iran-watcher tweet list/show/reprocess`
  - cli/cmd/health.go  — `iran-watcher health`

**External dependency:** `go get github.com/spf13/cobra`

**The CLI is a thin HTTP client** — it makes requests to the REST API and prints results.
Do NOT import internal/ packages directly. Use only net/http and encoding/json.

**Do NOT create or modify any files outside cli/.**

---

### Injected context: API routes

[INJECT: paste the list of routes returned by Agent I here]

---

### CLI commands

```
iran-watcher serve              — prints "Use 'iran-watcher serve' from cmd/iran-watcher binary"
iran-watcher source list        — GET /api/sources, print table
iran-watcher source add         — POST /api/sources, flags: --type (account|keyword), --value
iran-watcher source remove {id} — DELETE /api/sources/{id}
iran-watcher model list         — GET /api/models
iran-watcher model add          — POST /api/models, flags: --name, --version, --path, --type
iran-watcher model remove {id}  — DELETE /api/models/{id}
iran-watcher model activate {id}   — POST /api/models/{id}/activate
iran-watcher model deactivate {id} — POST /api/models/{id}/deactivate
iran-watcher tweet list         — GET /api/tweets, flags: --source-id, --from, --to, --page, --per-page
iran-watcher tweet show {id}    — GET /api/tweets/{id}
iran-watcher tweet reprocess    — POST /api/tweets/reprocess, flags: --tweet-ids (comma-sep), --model-names (comma-sep)
iran-watcher health             — GET /api/health, print status
```

Output format: human-readable tables for lists, JSON for show/health.

### Tests (cli/cmd/cli_test.go)

Use `httptest.NewServer` to simulate the API.

1. `source list` makes GET /api/sources and prints results
2. `source add --type account --value @handle` makes POST /api/sources with correct body
3. `model activate 5` makes POST /api/models/5/activate
4. `health` prints all component statuses
5. Non-200 response prints the error message and exits non-zero

### TDD steps
- [ ] `go get github.com/spf13/cobra`
- [ ] Write failing tests
- [ ] Run: `go test ./cli/...`
- [ ] Implement all command files
- [ ] Run: `go test ./cli/...` — all pass
- [ ] Run: `go vet ./cli/...` — no issues
- [ ] Commit: `git add cli/ && git commit -m "feat: add cobra CLI as thin REST API client"`

### Return
Return a summary of the cobra command tree.
```

---

#### Agent K — `cmd/iran-watcher/` + `configs/`

```
You are implementing the main binary entrypoint and default config for iran-watcher.

**Module:** github.com/amir-the-h/iran-watcher
**Go version:** 1.25.5
**Your files:**
  - cmd/iran-watcher/main.go    — lifecycle coordinator
  - configs/iran-watcher.yaml   — default config file

**Do NOT create or modify any files outside cmd/iran-watcher/ and configs/.**

---

### Injected context: All constructor signatures

[INJECT: paste NewServer, NewPool, stream.NewClient, inference.NewRegistry, store constructor signatures here]

### Injected context: Config struct

[INJECT: paste full contents of internal/config/config.go here]

---

### configs/iran-watcher.yaml

```yaml
x:
  tier: basic

redis:
  addr: localhost:6379
  db: 0

sqlite:
  path: ./data/iran-watcher.db

api:
  bind: 127.0.0.1:8080

worker:
  count: 0  # 0 means use number of active models

inference:
  models_dir: ./models

log:
  level: info
  format: text
```

(bearer_token intentionally absent — must be set via env var)

### cmd/iran-watcher/main.go

Wire the application. Startup order (from spec):
1. Load config (config.Load, path from --config flag, default: configs/iran-watcher.yaml)
2. Init slog logger (level and format from config)
3. Connect SQLite, run migrations
4. Connect Redis
5. Load active ONNX models into inference.Registry
6. Start stream.Client (goroutine)
7. Start worker.Pool (goroutine)
8. Start api.Server (goroutine)
9. Wait for SIGINT or SIGTERM

Shutdown order:
1. Cancel root context
2. api.Server.Shutdown (5s timeout)
3. Wait for worker pool to drain
4. Close SQLite and Redis connections
5. Log "shutdown complete"

Central error channel: each component sends fatal errors here. On receipt, trigger shutdown.

CLI entry: the `iran-watcher serve` command (from cli/) should call into this main function's logic — OR main.go is the serve command itself and cli/ contains a thin wrapper. Keep it simple: main.go does the serving, cli/ for everything else.

### Tests (cmd/iran-watcher/main_test.go)

1. Startup with missing bearer token logs error and exits non-zero
2. With all config present (mocked components), server starts and responds to GET /api/health
3. SIGTERM causes graceful shutdown within 10 seconds

### TDD steps
- [ ] Write failing tests using component mocks
- [ ] Run: `go test ./cmd/iran-watcher/...`
- [ ] Implement main.go and configs/iran-watcher.yaml
- [ ] Run: `go test ./cmd/iran-watcher/...` — all pass
- [ ] Run: `go vet ./cmd/iran-watcher/...` — no issues
- [ ] Run: `go build ./cmd/iran-watcher/` — binary compiles
- [ ] Commit: `git add cmd/ configs/ && git commit -m "feat: add main entrypoint with graceful shutdown"`

### Return
Confirm the binary compiles and report the output of `go build ./cmd/iran-watcher/ && echo 'build ok'`.
```

---

### Task 4.2: Final Verification

- [ ] `go build ./cmd/iran-watcher/` — binary builds
- [ ] `go test ./...` — all unit tests pass
- [ ] `go vet ./...` — no issues
- [ ] `go test -tags integration ./...` — integration tests pass (requires Redis in env)
- [ ] Review CLI command tree matches spec
- [ ] Commit: `git tag v0.1.0 -m "Initial implementation"`

---

## Quick Reference: Dependency Injection Chain

```
config.Load()
    ↓
store.NewSQLiteStore(cfg.SQLite.Path, migrations.FS)  →  store.Store
    ↓
redis.NewRedisQueue(cfg.Redis)  →  queue.Producer + queue.Consumer
    ↓
inference.NewRegistry()  +  store.ListModels(active=true)  →  registry
    ↓
stream.NewClient(cfg.X.BearerToken, store, producer, logger)
worker.NewPool(cfg.Worker.Count, consumer, producer, store, registry, logger)
api.NewServer(cfg.API.Bind, store, producer, registry, rules, logger)
```

---

## Constraints Summary (apply to every agent)

1. **Own your package** — never create or edit files outside the package you were assigned
2. **Import, don't redefine** — use `internal/model` types, never redeclare them
3. **TDD** — write failing test first, implement to make it pass
4. **No mocks in production code** — mocks go in `_test.go` files only
5. **Slog everywhere** — structured logging with component field: `logger.With("component", "stream")`
6. **Context everywhere** — all blocking operations take `context.Context`
7. **Wrap errors** — `fmt.Errorf("store: save tweet: %w", err)`
