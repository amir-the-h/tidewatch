# Phase 1 design — Signals, connectors, RSS

Phase 1 of [vision.md](vision.md). It turns Tidewatch from an X monitor into a system where X is one source among many, and proves it with a second source type (RSS/Atom).

**Status:** step 1 (schema reset and rename), step 2 (connector framework) and step 3 (X as connectors) done; steps 4–7 not started. See [Step 1 notes](#step-1-notes), [Step 2 notes](#step-2-notes) and [Step 3 notes](#step-3-notes) for where they depart from this design.

## Goals

- `tweets` becomes `signals`, and "tweet" disappears from the backend, API, CLI, MCP tools and UI except where something is genuinely X-specific.
- A connector framework: a connector type is code, a source is a configured instance of it managed at runtime.
- X Filtered Stream and X search become the `x_stream` and `x_search` connectors.
- An `rss` connector for RSS and Atom feeds.
- A `new_signal` trigger whose links to pipelines can filter by source, source type, signal kind and tier.
- Source metadata (tier, state affiliation, region, language) available to flow conditions.

Done when an RSS article and a tweet go through the same pipeline and appear side by side on the Signals page.

## Non-goals

- Telegram, GDELT, OREF (phase 2), story clustering (phase 2).
- Tracks, zones, detectors (phases 3–4).
- Map changes beyond renaming the join (phase 6). The `ListMapPins` slowness stays as it is.
- Full-text article extraction for RSS. Feeds deliver what they deliver; a `fetch_full_text` option can come later.

## No backward compatibility

The existing database is disposable and nothing outside this repo depends on the API shape. So:

- **Migrations reset.** `migrations/001`–`029` are deleted and replaced by one `001_schema.sql` describing the target schema. Dead tables left behind by earlier refactors (`categorizations`, `models`, `model_labels`, `rule_models`, `pipeline_backends`, `pipeline_labels`, `notifications`, `chains`, `chain_*`, `trigger_chain_links`, `*_new` copies) are simply not recreated. Deploying phase 1 means deleting `data/tidewatch.db` (keep a copy; nothing reads it).
- **No aliases.** Old routes (`/api/tweets`, `/api/stream/*`, `/api/search/*`), CLI commands (`tidewatch tweet`, `tidewatch stream`), MCP tools (`tweets_*`, `stream_*`, `search_*`) and context keys (`tweet`, `tweet_url`, …) are removed, not redirected.
- **Config keys removed:** `x.bearer_token`, `x.tier`, `stream.auto_connect`, the whole `search.*` block and their `TIDEWATCH_*` env vars. Their values move into source configs.
- Leftovers cleaned up on the way: the empty `internal/api/handlers_chains.go`, `cli/cmd/chain.go`.

## Naming collision: today's "sources"

Today `sources` are **X rule atoms** (keyword, hashtag, `from:` account…) that the rule builder combines into X Filtered Stream rules. In the new model "source" means a configured connector instance. The X atoms are renamed:

| Today | Phase 1 |
|---|---|
| `sources` table (rule atoms) | `x_terms` |
| `rules` | `x_rules` |
| `tweet_rules` | `x_rule_matches` |
| `/api/sources`, `/api/rules` | `/api/x/terms`, `/api/x/rules` |
| `tidewatch source`, `tidewatch rule` | `tidewatch x term`, `tidewatch x rule` |
| `sources_*`, `rules_*` MCP tools | `x_terms_*`, `x_rules_*` |
| `model.Source`, `model.Rule` | `model.XTerm`, `model.XRule` |

`sources`, `/api/sources`, `tidewatch source` and `sources_*` are then free for generic sources. (Done in step 2, not step 3; see [Step 2 notes](#step-2-notes).)

## Schema

### New and changed tables

```sql
-- A configured connector instance.
CREATE TABLE sources (
    id               INTEGER PRIMARY KEY AUTOINCREMENT,
    name             TEXT    NOT NULL UNIQUE,
    type             TEXT    NOT NULL,              -- connector type: x_stream, x_search, rss, …
    config           TEXT    NOT NULL DEFAULT '{}', -- connector config JSON, secrets included (masked by the API)
    poll_interval_s  INTEGER,                       -- poll connectors only; NULL = connector default
    tier             INTEGER CHECK (tier BETWEEN 1 AND 4),
    state_affiliated INTEGER NOT NULL DEFAULT 0,
    region           TEXT    NOT NULL DEFAULT '',
    language         TEXT    NOT NULL DEFAULT '',
    active           INTEGER NOT NULL DEFAULT 0,
    created_at       DATETIME NOT NULL DEFAULT (datetime('now')),
    updated_at       DATETIME NOT NULL DEFAULT (datetime('now'))
);

-- Cursor and last-known health per source, so restarts resume where they left off.
-- (Fixes today's in-memory since_id that resets on restart.)
CREATE TABLE source_state (
    source_id       INTEGER PRIMARY KEY REFERENCES sources(id) ON DELETE CASCADE,
    cursor          TEXT     NOT NULL DEFAULT '{}', -- connector-owned JSON: since_id, ETag, Last-Modified, seen GUIDs…
    last_run_at     DATETIME,
    last_success_at DATETIME,
    last_error      TEXT     NOT NULL DEFAULT '',
    updated_at      DATETIME NOT NULL DEFAULT (datetime('now'))
);

-- Manual fetches and backfills (replaces search_jobs).
CREATE TABLE source_runs (
    id            TEXT PRIMARY KEY,                -- UUID
    source_id     INTEGER NOT NULL REFERENCES sources(id) ON DELETE CASCADE,
    kind          TEXT    NOT NULL,                -- fetch | backfill
    params        TEXT    NOT NULL DEFAULT '{}',
    status        TEXT    NOT NULL DEFAULT 'pending', -- pending | running | done | error
    fetched_count INTEGER NOT NULL DEFAULT 0,
    new_count     INTEGER NOT NULL DEFAULT 0,
    error         TEXT,
    created_at    DATETIME NOT NULL DEFAULT (datetime('now')),
    completed_at  DATETIME
);

CREATE TABLE signals (
    id           INTEGER PRIMARY KEY AUTOINCREMENT,
    kind         TEXT    NOT NULL,                 -- post | article (later: event, alert, track.*)
    ext_ns       TEXT    NOT NULL,                 -- dedup namespace: x, rss, …
    external_id  TEXT    NOT NULL,
    author       TEXT    NOT NULL DEFAULT '',
    author_id    TEXT    NOT NULL DEFAULT '',
    title        TEXT    NOT NULL DEFAULT '',
    text         TEXT    NOT NULL DEFAULT '',
    url          TEXT    NOT NULL DEFAULT '',
    lang         TEXT    NOT NULL DEFAULT '',
    lat          REAL,
    lon          REAL,
    attrs        TEXT    NOT NULL DEFAULT '{}',    -- connector-specific fields, merged into pipeline context
    raw_json     BLOB,
    published_at DATETIME NOT NULL,
    fetched_at   DATETIME NOT NULL DEFAULT (datetime('now')),
    UNIQUE (ext_ns, external_id)
);
CREATE INDEX idx_signals_published_at ON signals(published_at);
CREATE INDEX idx_signals_kind         ON signals(kind);

-- Which sources saw a signal (x_stream and x_search can both see one tweet).
CREATE TABLE signal_sources (
    signal_id INTEGER NOT NULL REFERENCES signals(id) ON DELETE CASCADE,
    source_id INTEGER NOT NULL REFERENCES sources(id) ON DELETE CASCADE,
    seen_at   DATETIME NOT NULL DEFAULT (datetime('now')),
    PRIMARY KEY (signal_id, source_id)
);
CREATE INDEX idx_signal_sources_source_id ON signal_sources(source_id);

-- X-specific, renamed from sources / rules / tweet_rules.
CREATE TABLE x_terms (… same columns as today's sources …);
CREATE TABLE x_rules (… same columns as today's rules …);
CREATE TABLE x_rule_matches (
    signal_id INTEGER NOT NULL REFERENCES signals(id) ON DELETE CASCADE,
    rule_id   INTEGER NOT NULL REFERENCES x_rules(id) ON DELETE CASCADE,
    PRIMARY KEY (signal_id, rule_id)
);
```

`ext_ns` is declared by the connector, not derived from the source type, so that `x_stream` and `x_search` share the namespace `x` and a tweet seen by both is stored once. Two RSS feeds that carry the same GUID also collapse to one signal; the same story under different GUIDs is phase 2's clustering problem.

### `tweet_id TEXT` → `signal_id INTEGER`

Signals get an integer primary key; the upstream ID lives in `external_id`. Every reference changes type and name, with real foreign keys where there were none:

| Table | Change |
|---|---|
| `action_executions` | `signal_id INTEGER NOT NULL REFERENCES signals(id) ON DELETE CASCADE`; `status` gains `skipped` |
| `action_execution_metadata` | `execution_id … ON DELETE CASCADE`, so retention and signal deletes clean up outputs |
| `pipeline_node_executions` | `signal_id INTEGER … ON DELETE CASCADE` |
| `pipeline_run_tracking` | PK `(pipeline_id, signal_id)` |
| `entity_tracks` | `signal_id INTEGER … ON DELETE CASCADE`, `UNIQUE(entity_id, signal_id)` |
| `digest_jobs` | `tweet_count` → `signal_count`; `params` gains source/kind filters |
| `trigger_pipeline_links` | new `filter TEXT NOT NULL DEFAULT '{}'` (see Triggers) |

Everything else (actions, pipelines, nodes, edges, Ollama, notifications, users, entities, geocode cache, Twitter OAuth credentials) is recreated unchanged.

## Go model

```go
// internal/model/signal.go
type SignalKind string

const (
    SignalKindPost    SignalKind = "post"    // short social post (X, later Telegram)
    SignalKindArticle SignalKind = "article" // news article (RSS, later GDELT DOC)
)

type Signal struct {
    ID          int64          `json:"id"`
    Kind        SignalKind     `json:"kind"`
    ExtNS       string         `json:"ext_ns"`
    ExternalID  string         `json:"external_id"`
    Author      string         `json:"author"`
    AuthorID    string         `json:"author_id"`
    Title       string         `json:"title"`
    Text        string         `json:"text"`
    URL         string         `json:"url"`
    Lang        string         `json:"lang"`
    Lat, Lon    *float64       `json:"lat,omitempty"`
    Attrs       map[string]any `json:"attrs"`
    RawJSON     []byte         `json:"-"`
    PublishedAt time.Time      `json:"published_at"`
    FetchedAt   time.Time      `json:"fetched_at"`

    Sources []*SourceRef `json:"sources,omitempty"` // read joins only
}
```

`model.Tweet`, `model.StreamTweet` and `model.TweetRule` are deleted. `queue.TweetEvent` becomes:

```go
// SignalEvent is the Event payload for trigger_type=new_signal.
type SignalEvent struct {
    SignalID   int64  `json:"signal_id"`
    Kind       string `json:"kind"`
    SourceID   int64  `json:"source_id"`   // the source whose ingest dispatched it
    SourceType string `json:"source_type"`
}
```

The event no longer carries text or author. The worker loads the signal by ID when it builds the context (one indexed read per task, the same pattern already used for execution metadata). This keeps Redis payloads small regardless of article length. `inference.PipelineInput.TweetID string` becomes `SignalID int64`.

## Connector framework

New package `internal/source`.

```go
type Mode string

const (
    ModeStream Mode = "stream" // long-lived connection; Run blocks
    ModePoll   Mode = "poll"   // the manager calls Poll on an interval
)

// Descriptor tells the API and UI how to configure a connector type.
type Descriptor struct {
    Type            string        // "rss"
    Label           string        // "RSS / Atom feed"
    Mode            Mode
    ExtNS           string        // dedup namespace
    Kinds           []model.SignalKind
    Fields          []Field       // config form: key, label, type, required, secret, default, help
    MaxInstances    int           // 0 = unlimited; x_stream = 1 (one stream per X app)
    DefaultInterval time.Duration // poll only
    MinInterval     time.Duration // poll only; floor enforced by the API
    Backfill        bool          // supports source_runs kind=backfill
}

type Connector interface {
    Descriptor() Descriptor
    Validate(cfg json.RawMessage) error
}

type StreamConnector interface {
    Connector
    Run(ctx context.Context, src *model.Source, rt Runtime) error
}

type PollConnector interface {
    Connector
    // Poll fetches once. It reads its cursor from rt, emits signals and returns the new cursor.
    Poll(ctx context.Context, src *model.Source, rt Runtime) error
}

type Backfiller interface {
    Backfill(ctx context.Context, src *model.Source, params json.RawMessage, rt Runtime) error
}

// Runtime is what a connector gets from the manager.
type Runtime interface {
    Emit(ctx context.Context, s *model.Signal, opts ...EmitOption) (isNew bool, err error)
    Cursor() json.RawMessage
    SetCursor(json.RawMessage)
    HTTPClient() *http.Client // shared client: timeouts, User-Agent "Tidewatch/<version> (+repo URL)"
    Logger() *slog.Logger
}
```

`EmitOption` lets X attach rule matches (`WithXRuleMatches(ids)`) without the ingest path knowing about X.

### Registry

`source.Registry` maps type → connector, populated in `cli/cmd/serve.go` like `internal/pipeline.Registry`. `GET /api/sources/types` returns all descriptors for the UI form.

### Ingest

One code path for every connector, replacing `stream.Client.processEvent`:

1. `INSERT … ON CONFLICT (ext_ns, external_id) DO NOTHING`, then read back the ID. Report whether the row is new.
2. `INSERT OR IGNORE` into `signal_sources` (and X rule matches, when attached).
3. If new, broadcast the WebSocket `new_signal` event and call `worker.PushInitialPipelineTasks(ctx, …, "new_signal", SignalEvent{…})`.
4. If not new, do nothing else. A tweet that arrives through search after the stream already delivered it isn't processed twice.

Signals are persisted before they are queued, so at-most-once delivery stays acceptable.

### Manager

`source.Manager` runs one supervised goroutine per active source:

- **Stream:** call `Run`; on error, restart with exponential backoff (5 s → 5 min, reset after 10 minutes of healthy running).
- **Poll:** call `Poll` every `poll_interval_s` (or the connector default) with ±10% jitter; at most one poll in flight per source; a global semaphore caps concurrent polls (default 8).
- **Hot reload:** activate, deactivate, update or delete a source → stop and restart its goroutine. Same idea as the `ActionCache` rebuild.
- **State:** after every run, write `source_state` (cursor, `last_run_at`, `last_success_at` or `last_error`).
- **Status:** in-memory status per source (`running | idle | backoff | error | stopped`, last run, next run, counters for fetched/new since start) exposed at `GET /api/sources/status` and pushed as the WebSocket `source_status` event (replaces `stream_status`).
- **Runs:** `POST /api/sources/{id}/fetch` (poll now) and `POST /api/sources/{id}/backfill` create `source_runs` rows, return 202, and run asynchronously, like search jobs today.

Active sources start with the server; there is no separate "connect" step. `stream.auto_connect` is gone: an active `x_stream` source is a connected stream.

## Connectors in phase 1

### `x_stream`

Wraps today's `internal/stream` (SSE client, rule compiler, validator, rule sync) and moves it to `internal/source/x`.

- Config: `bearer_token` (secret), `tier` (`basic` | `pro`, drives the validator limits). `MaxInstances: 1`.
- Rules (`x_rules` / `x_terms`) stay global; the active `x_stream` source syncs them to X on start and whenever a rule is activated or deactivated, as `RuleManager` does now.
- Emits `kind = post`, `ext_ns = x`, `external_id` = tweet ID, `author` = handle, `url` = `https://x.com/{handle}/status/{id}`, `published_at` = `created_at`. Long-form and retweet text handling stays as in today's `Normalize`.
- `attrs`: `x_tweet_id`, `x_author_id`, `x_referenced` (retweeted / quoted / replied_to IDs), `x_rule_ids`.

### `x_search`

Wraps `internal/search` and replaces the search scheduler and search jobs.

- Config: `bearer_token` (secret), `query` (an X search query) or `rule_ids` (search with the compiled rules, as the scheduler does now), `max_results`, `limit`, `start_time`, field and expansion lists. Mode `poll`, `Backfill: true`.
- The cursor holds `since_id` per query in `source_state`, so restarts no longer re-fetch.
- A backfill (`start_time`, `end_time`, `limit` params) is what `POST /api/search/run` does today.

### `rss`

New, in `internal/source/rss`, using `github.com/mmcdole/gofeed` (pure Go, MIT; handles RSS 0.9x–2.0, Atom and JSON Feed).

- Config: `url` (required), `user_agent` (optional override), `include_content` (bool, default true: use `content:encoded` when the feed has it, else `description`). Default interval 10 minutes, minimum 2 minutes.
- Conditional GET: the cursor stores `etag` and `last_modified`; a 304 is a successful empty poll.
- The cursor also stores the last 500 item keys, so the database is only hit for items not seen recently. Dedup correctness still comes from the unique index.
- Emits `kind = article`, `ext_ns = rss`, with fields:
  - `external_id` = `guid`, else `link`, else a SHA-256 of `title + published`
  - `title`, `url` = `link`, `author` = item author or feed title
  - `published_at` = published, else updated, else fetch time
  - `lang` = feed language, falling back to the source's `language`
  - `text` = HTML converted to plain text with `golang.org/x/net/html` (scripts and styles dropped, block elements become newlines), capped at 20,000 characters
  - `attrs`: `rss_feed_title`, `rss_categories`, `rss_enclosure_url`
- The first poll of a new source can flood the queue with a feed's whole history. Option `skip_existing_on_first_poll` (default true) stores the first poll's items without dispatching them to pipelines.

## Triggers

- `new_tweet` is replaced by `new_signal`.
- `trigger_pipeline_links.filter` narrows which signals a link dispatches. Every field is optional; an empty filter matches everything.

```json
{
  "source_ids":   [3, 7],
  "source_types": ["rss"],
  "kinds":        ["article"],
  "max_tier":     2,
  "exclude_state_affiliated": true
}
```

`PushInitialPipelineTasks` evaluates the filter against the event and the source row (sources are cached in memory like actions) before pushing any tasks. In the builder, the Start node shows each linked trigger with its filter summary ("New signal · RSS · tier ≤ 2"), and the trigger link form gets the filter fields.

`pipeline_finished`, `entity_matched`, `digest_requested` and `chain_only` keep their meaning, keyed by `signal_id`.

## Pipeline context

Context keys set by the worker for every task, replacing the `tweet*` keys:

| Key | Value |
|---|---|
| `signal_id` | integer ID |
| `signal_kind` | `post`, `article` |
| `signal_url` | link to the signal's detail page in the UI; only set when `ui.base_url` is configured (see [UI base URL](#ui-base-url)) |
| `source_id`, `source_name`, `source_type`, `source_tier` | from the dispatching source |
| `author`, `title`, `text`, `url`, `lang`, `published_at` | from the signal |
| `text_short` | `text` truncated to 280 characters (`title` if `text` is empty) |
| `rule_ids` | X only; removed from the generic set, now `x_rule_ids` via attrs |
| *every `attrs` key* | e.g. `x_tweet_id`, `rss_feed_title` |

Merge order stays as today: all prior metadata, then the previous node's metadata, then the task context, then the keys above (so they can't be overwritten by an action's output).

ONNX and Ollama actions receive `PipelineInput.Text` = `text` for posts and `title + "\n\n" + text` for articles. ONNX models already cut input at 512 tokens (`maxSeqLen` in `internal/inference/onnx.go`), so NER and classifiers see only the opening of a long article. That's acceptable for phase 1, since the lead usually carries the who and where; chunking can come later. Ollama prompts get the full text unless the action sets `max_input_chars`.

Notification templates and Ollama prompts use the new keys: `${text_short}` and `${url}` replace `${tweet_short}` and `${tweet_url}`. `docs/examples/geopolitical-monitoring` is updated to match.

## Actions that need a specific source

Some internal handlers only make sense for X signals: `twitter_retweet`, `twitter_like`, `twitter_quote`. Handlers gain an optional declaration:

```go
type SourceRequirement interface {
    SourceTypes() []string // e.g. ["x_stream", "x_search"]; empty = any source
}
```

- **At run time:** when a signal's source type isn't listed, the worker records the execution as `skipped` (new status), produces no output, and continues the graph as if the node succeeded with no outputs. It doesn't dead-letter.
- **In the builder:** the inspector warns when a node requires X but a linked trigger's filter doesn't restrict to X sources.

The handlers read `x_tweet_id` instead of `tweet_id`.

## API, CLI and MCP changes

| Area | Today | Phase 1 |
|---|---|---|
| Signals | `GET /api/tweets`, `GET /api/tweets/{id}`, `DELETE /api/tweets`, `POST /api/tweets/reprocess`, `DELETE /api/tweets/noise` | `/api/signals…` with the same verbs. List filters gain `source_id`, `source_type`, `kind`. Bulk delete gains `older_than`. `{id}` is the integer ID |
| Sources | — | `GET\|POST /api/sources`, `GET\|PUT\|DELETE /api/sources/{id}`, `DELETE /api/sources` (bulk), `POST /api/sources/{id}/{activate,deactivate,fetch,backfill}`, `POST /api/sources/import-starter`, `GET /api/sources/types`, `GET /api/sources/status`, `GET /api/sources/{id}/runs`, `GET /api/source-runs/{id}` |
| X | `/api/sources`, `/api/rules` (rule atoms and rules) | `/api/x/terms`, `/api/x/rules` |
| Stream | `GET /api/stream/status`, `POST /api/stream/{connect,disconnect}` | removed (source status and activate/deactivate) |
| Search | `/api/search…` | removed (`x_search` source + `source_runs`) |
| Triggers | `new_tweet` | `new_signal`; link create/update accept `filter` |
| Executions | `tweet_id` filters and fields | `signal_id` |
| Digest | tweet window | signal window, filterable by `source_ids`, `source_types`, `kinds` |
| Map | joins `tweets` | joins `signals` (no other change) |
| WebSocket | `new_tweet`, `stream_status` | `new_signal`, `source_status` |
| CLI | `tweet`, `stream`, `source`, `rule` | `signal`, `source` (generic), `x term`, `x rule` |
| MCP | `tweets_*`, `stream_*`, `search_*`, `sources_*`, `rules_*` | `signals_*`, `sources_*` (generic, incl. `sources_fetch`, `sources_backfill`, `sources_status_get`, `source_types_list`), `source_runs_*`, `x_terms_*`, `x_rules_*` |

The API masks secret fields (declared by `Field.Secret`) on read: they come back as `"••••"` and a PUT that sends `"••••"` keeps the stored value. Notification providers should follow the same rule while we're at it.

The user-level skills (`tidewatch-flow-builder`, `tidewatch-pipeline-checker`, `tidewatch-fresh-start`) refer to tweets and the old tool names and need the same update.

## Web UI

- **Signals page** (`/signals`, replaces `/tweets`; viewers keep access). The table gets a source column (name + type icon) and a kind badge, and shows the title for articles and the text for posts. Filters gain source and kind. The detail sheet renders by kind: a post shows author and text; an article shows title, source, link and text. Execution output is unchanged. `components/tweets/` → `components/signals/`. The deep link becomes `/signals?signal={id}`.
- **Sources page** (`/sources`, admin). Lists sources with type, status dot, last success, last error, fetched/new counters and an active toggle. "Add source" picks a connector type, then renders the form from its descriptor fields. Actions: Fetch now, Backfill (when supported), Runs history. The existing Search tab moves here as the `x_search` backfill dialog.
- **X source detail.** Today's rule builder (terms and rules) moves to a panel of the `x_stream` source, since rules only exist for it.
- **Status bar.** The stream connect/disconnect control is replaced by a sources health summary ("5 sources · 1 error") linking to `/sources`.
- **Pipeline builder.** The Start node shows trigger filters; the trigger link form edits them; the inspector shows the source-requirement warning.
- The Map, Tracker, Digest and Chat pages only change wording and field names.

`npm run check:ui`'s `CONVERTED` list follows the renamed paths.

## Work order

Each step is one PR that leaves `main` green and deployable.

1. **Schema reset and rename.** New `001_schema.sql`; `model.Signal`; store, queue, worker, inference, internal handlers, API, CLI, MCP and UI renamed from tweets to signals; integer `signal_id`; context keys. X ingestion keeps working through a temporary shim in `internal/stream` that builds a `model.Signal`. Large but mechanical.
2. **Connector framework.** `sources`, `source_state`, `source_runs`; registry, manager, ingest; sources API, CLI and MCP; WebSocket `source_status`. (Also did the X rename planned for step 3.)
3. **X as connectors.** `x_stream` and `x_search`; removal of `/api/stream`, `/api/search`, the search scheduler and the removed config keys.
4. **RSS connector and starter feeds.** The `rss` connector, `configs/feeds/starter.yaml` with every URL checked by hand, the import endpoint, and the weekly feed-check workflow.
5. **Trigger filters, source requirements, retention, base URL.** Link `filter`, `skipped` executions, handler `SourceTypes()`, the retention job, `ui.base_url` and `signal_url`.
6. **UI.** Sources page, X source detail, status bar, builder filter UI (the Signals page rename happens in step 1).
7. **Docs.** CLAUDE.md, README, `configs/`, `docs/examples`, the user skills. Each PR also updates the docs it touches; this step sweeps up whatever is left.

### Step 1 notes

What step 1 did differently from the design above, and what later steps inherit:

- **`signal_sources` has no foreign key yet.** It exists in `001_schema.sql` with `source_id INTEGER NOT NULL` and nothing writes it. Step 2 drops and recreates it with `REFERENCES sources(id) ON DELETE CASCADE` once the generic `sources` table exists. An FK now would have pointed at today's rule-atom `sources`, and SQLite rewrites FK targets on `ALTER TABLE … RENAME`, so the `sources → x_terms` rename would have silently moved it.
- **`x_rule_matches` already has its final name.** `tweet_rules` became `x_rule_matches (signal_id, rule_id → rules.id)`; step 3 only repoints `rule_id` at `x_rules`.
- **Later steps add numbered migrations** (`002_…`), so `main` stays deployable without another database wipe. `RunMigrations` refuses a database that recorded a migration which is no longer embedded (an old `001_initial.sql` database) with a "move the SQLite file aside" error.
- **`pipeline_run_tracking.signal_id`** also references `signals(id) ON DELETE CASCADE`.
- **Not in the schema yet:** `trigger_pipeline_links.filter` (step 5 adds it with `ALTER TABLE … ADD COLUMN`); the `skipped` execution status needs no schema change (`status` has no CHECK).
- **Context keys.** `source_id`, `source_name` and `source_tier` arrive with step 2. `source_type` is set from the ingest path (`x_stream`, `x_search`) and is absent on reprocess and pipeline-run backfills, whose events carry only `signal_id` and `kind`. The `rule_matches` / `rule_not_matches` conditions read `x_rule_ids` (they read `rule_ids` before); `x_rule_ids` holds the rules known when the signal was first stored.
- **The X shim** lives in `internal/stream/signal.go` (`NewXSignal`, `Ingest`), not in a new package. Search imports it. `x_referenced` is a map of reference type to tweet ID.
- **Removed, not renamed:** the `source_id` filter on tweets, summaries and digests (it joined a `tweet_sources` table that no longer existed, so it never worked; step 2 adds real source filters), the unused notifier events `tweet_categorized` and `categorization_failed` (`tweet_matched` is now `signal_ingested`), and the broken `tidewatch tweet` command (rewritten as `tidewatch signal list|show|reprocess|delete`).
- **Dead-letter UI.** The Queue page now reads the real `{task, failed_models}` shape. Nothing calls `PushFailed` yet, so the dead-letter list stays empty; that predates phase 1.

### Step 2 notes

What step 2 did differently from the design above, and what later steps inherit:

- **The X rename moved here from step 3.** Freeing the name `sources` meant moving the rule atoms anyway, and terms and rules are coupled, so `002_sources.sql` renames `sources → x_terms` and `rules → x_rules` (SQLite repoints `x_rule_matches.rule_id` itself), and the code follows: `model.XTerm`/`model.XRule`, store `*XTerm*`/`*XRule*` methods, `/api/x/terms` and `/api/x/rules`, `tidewatch x term|rule`, MCP `x_terms_*`/`x_rules_*`, WebSocket list events `x_terms`/`x_rules`. The web UI only changed its API paths and event names; the rule-builder page still sits at `/sources` until step 6. Expression JSON still references terms as `src`/`srcs`.
- **Migration order.** `002_sources.sql`: the two renames, then `sources`, `source_state`, `source_runs`, then `DROP`/`CREATE signal_sources` with `source_id REFERENCES sources(id) ON DELETE CASCADE`. Deleting a source keeps its signals.
- **No connectors are registered yet.** `GET /api/sources/types` is `[]` and creating a source is refused until step 3 (`x_stream`, `x_search`) and step 4 (`rss`) register theirs in `cli/cmd/serve.go`.
- **X still runs on the shim**, now routed through `source.Ingester` with no source row: X signals get no `signal_sources` row, `SignalEvent.SourceID` stays 0 and only `source_type` reaches the context. `stream_status` and the `stream.auto_connect`/`search.*` config stay until step 3, alongside the new `source_status`.
- **Context keys** `source_id`, `source_name`, `source_type`, `source_tier` are read from the source row per task (one indexed read, no in-memory source cache). Events without a source (reprocess, pipeline-run backfills) fall back to the signal's first `signal_sources` row. Step 5's trigger filters can reuse `dispatchingSource` or add the cache the design mentions.
- **Manager details the design left open.** The first poll after a (re)start waits until `last_run_at + interval`, so restarts don't re-poll every feed at once. A stream `Run` that returns `nil` counts as a failure ("stream ended") and is restarted with backoff. Polls time out after 5 minutes, backfills after an hour. Only a successful poll writes the cursor; a backfill never does. Fetch works on inactive sources (useful for trying a feed before activating it) and shares the per-source poll lock. Runs left `pending`/`running` by a previous process are marked `error: interrupted by restart` at startup. A finished run is pushed as WebSocket `source_run`.
- **API shape.** `PUT /api/sources/{id}` changes only the fields sent (`clear_poll_interval`, `clear_tier` reset those); the type can't change. Validation errors are 400, a duplicate name 409, more active sources than `MaxInstances` 400. MCP also has `sources_get`. Notification providers mask `app_token`, `user_key` and `bot_token` the same way.
- **Shared HTTP client** sends `User-Agent: Tidewatch (+https://github.com/amir-the-h/tidewatch)`: the binary has no version string to include.
- **Deferred:** source filters on digests and summaries go with step 5's trigger filters.

### Step 3 notes

What step 3 did differently from the design above, and what later steps inherit:

- **Package.** Everything X lives in `internal/source/x` (package `x`): `Stream` (`x_stream`), `Search` (`x_search`), `NewXSignal`, both normalizers, the rule compiler and validator, and the rules API client (`RuleManager`, interface `RuleAPI`). `internal/stream` and `internal/search` are gone, and so are the shim and the search scheduler.
- **Rule sync.** It's a reconcile, not a replay of individual changes. When `x_stream` starts, it lists X's rules. It keeps every active non-local `x_rule` whose `x_rule_id` is on X with the same value, adopts an unclaimed X rule with the same value, adds the rest, and deletes whatever no rule claims. While the stream runs, the `x_rules` handlers still add and delete single rules on X, through `Stream.WithRules`. That call finds the active `x_stream` source and holds the same mutex as the reconcile, so the two never race. With no active `x_stream` source, rule changes are only stored (activate clears `x_rule_id`), and the next stream start syncs them. A rule that X rejects during the reconcile is logged and left active with no `x_rule_id`. An X refusal through the API is now a 502 (it was 500). Bulk delete now also removes the deleted rules from X, which it never did before.
- **`tier` config key.** Both connectors have a `tier` select (`basic`, `pro`, `enterprise`): the X API plan, labelled "X API tier". `enterprise` was added because the validator only raises the rule-length limit (1024 → 2048) for it. On `x_search`, `pro` and `enterprise` use full-archive search. The name overlaps with the source's 1–4 reliability `tier` column; step 6's form should label the two clearly.
- **`x_search` details the design left open.**
  - `query` and `rule_ids` are exclusive; with neither set, the source searches every active non-local rule, as the old scheduler did. `rule_ids` uses a new field type, `int_list`.
  - The cursor is `{"since_ids": {"rule:<id>" | "query": "<newest_id>"}}`, and `start_time` is only sent until a query has a `since_id`.
  - A query that fails keeps its old `since_id`, and the poll only fails when every query fails, so one broken rule can't make the others re-fetch.
  - The default interval is 15 minutes and the minimum 5.
  - A 429 sleeps until `x-rate-limit-reset` and retries once. The old client also did this on a successful response with `x-rate-limit-remaining: 0`, which threw that response away; that's fixed.
  - `until_id` and the media, place and poll field lists from `/api/search/run` were not carried over.
- **Stream connection.** `x_stream` has its own HTTP client (the shared one has a 60 s overall timeout): a 60 s header timeout, a 1 MB event limit and a 90 s idle watchdog, since X sends a keep-alive every 20 s. The old client never noticed a stalled connection. Reconnects are the manager's backoff; the connector's own retry loop is gone.
- **Removed alongside.** The notifier events `stream_connected`, `stream_disconnected` and `stream_error` (the dispatcher only logged them), the `stream` field of `GET /api/health`, and the serve-time requirement for an X bearer token.
- **UI until step 6** (decided with the user):
  - The status bar's stream control became a read-only sources summary ("N sources · M errors", from `GET /api/sources/status` and the `source_status` event, in `stores/sourceStatus.ts`). Step 6 links it to the Sources page.
  - The Signals page's Search tab was removed, and the Signals page has no tabs now. Backfills go through `tidewatch source backfill` or MCP `sources_backfill` until step 6's `x_search` backfill dialog.
  - Sources are created with `tidewatch source create` or MCP.
- **Web naming clash, left for step 6.** `web/src/stores/sources.ts` and `api/sources.ts` are still the X terms (rule atoms). The generic source status lives in `stores/sourceStatus.ts` and `api/sourceStatus.ts`.

## Testing

- **Store:** in-memory SQLite tests for signal insert and dedup (same `ext_ns` + `external_id` from two sources → one signal, two `signal_sources` rows), cascades from `signals` to executions and entity tracks, and source state round-trips.
- **Ingest:** a duplicate emit doesn't push tasks; a new emit pushes one task per matching link; trigger filters include and exclude as expected.
- **Manager:** with a fake clock and fake connectors: poll interval and jitter, one poll in flight, stream restart with backoff, hot reload on update or deactivate.
- **RSS:** `httptest` server with fixtures for RSS 2.0, Atom, JSON Feed, items without GUIDs, HTML-heavy content, 304 responses, and the first-poll skip.
- **X:** existing normalize, compiler and validator tests move with the code; add tests that `x_stream` and `x_search` share `ext_ns`.
- **Worker:** context keys for a post and an article; `skipped` for an X-only handler on an RSS signal.
- **MCP:** the tool table test covers the renamed and new tools.
- **Retention:** old signals are deleted with their cascades; matched signals are kept when `retention_keep_matched` is on; deletion runs in batches.
- **Starter feeds:** the embedded YAML parses, every entry passes `rss` validation, and import is idempotent (a second import adds nothing).

## UI base URL

New config key `ui.base_url` (env `TIDEWATCH_UI_BASE_URL`, e.g. `https://tidewatch.test`). When set, the worker adds `signal_url` = `{base_url}/signals?signal={id}` to every task context, so notification templates and Pushover `url` keys can use `${signal_url}` instead of hard-coding the host. When it isn't set, `signal_url` is absent and templates that use it render empty. `serve` logs a warning at startup if any active notification action references `${signal_url}` while the key is unset.

## Starter feed set

Phase 1 ships a curated list of RSS/Atom feeds that can be imported as sources.

- **Format.** `configs/feeds/starter.yaml`, embedded in the binary. One entry per feed: `name`, `url`, `tier`, `state_affiliated`, `region`, `language`, optional `poll_interval`, and `notes` for anything worth knowing (paywall, partial content, rate limits).
- **Import, not auto-seed.** `POST /api/sources/import-starter` (CLI `tidewatch source import-starter`, MCP `sources_import_starter`, a button on the empty Sources page) creates any missing entries as **inactive** sources, matched by URL. Re-running it adds new entries without touching existing ones. Nothing polls until the user activates feeds, so a fresh install doesn't suddenly fetch 50 feeds.
- **Compiled from primary sources.** Each URL comes from the publisher's own site or feed directory and is checked by hand during step 4. Nothing is copied from worldmonitor or any other aggregator's list.
- **Coverage, by region:**

  | Region | Candidates |
  |---|---|
  | Wires and global | Reuters, AP, AFP, BBC World, Al Jazeera English, The Guardian World, DW, France 24 |
  | Middle East | BBC Persian, Iran International, IranWire, Al-Monitor, Arab News, The National, Times of Israel, Haaretz, Rudaw, L'Orient Today |
  | State-affiliated (flagged, default inactive) | IRNA, Tasnim, Press TV, TASS, RT, Xinhua |
  | Official bodies | UN News, US State Department, US DoD/CENTCOM, UK FCDO |
  | Defence and analysis | The War Zone, Breaking Defense, Defense News, Crisis Group, ISW, Long War Journal |

  Feeds that turn out to be dead, heavily paywalled or summary-only are dropped rather than kept for show. Tiers follow the scale in [vision.md](vision.md#sources-and-connectors).
- **CI check.** A weekly workflow (`feed-check.yml`) fetches every starter URL and fails on a non-2xx response or an unparseable feed, so dead entries get noticed.

## Signal retention

New config key `signals.retention` (env `TIDEWATCH_SIGNALS_RETENTION`), a duration such as `2160h` (90 days). `0` disables it, and that's the default, so nothing is deleted unless asked.

- **What goes.** A daily job deletes signals with `published_at` older than the window, which cascades to `signal_sources`, `x_rule_matches`, `action_executions` → `action_execution_metadata`, `pipeline_node_executions` and `entity_tracks`. Map pins come from execution metadata, so old pins disappear with them. `action_execution_metadata.execution_id` gets `ON DELETE CASCADE` in the new schema for this.
- **What stays.** `signals.retention_keep_matched` (default `true`) keeps signals that matched a tracked entity, so the Tracker's history and connections don't erode. Entity connection weights are counters and are never touched.
- **NFS-friendly.** Deletes run in batches of 1,000 rows with a short pause between batches, so the database is never locked for long. The job logs how many signals it removed.
- **Manual.** `DELETE /api/signals` with an `older_than` filter (and the matching CLI flag and MCP field) runs the same deletion on demand.

## Config changes

| Key | Change |
|---|---|
| `x.bearer_token`, `x.tier` | removed → `x_stream` / `x_search` source config |
| `stream.auto_connect` | removed → active sources start with the server |
| `search.*` | removed → `x_search` source config |
| `ui.base_url` | new |
| `signals.retention`, `signals.retention_keep_matched` | new |
| `sources.max_concurrent_polls` | new, default 8 |

`configs/` and the README configuration table are updated to match.
