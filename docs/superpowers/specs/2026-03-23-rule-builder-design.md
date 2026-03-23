# Rule Builder — Design Spec

**Date:** 2026-03-23
**Status:** Approved

## Overview

Introduce a two-layer filter architecture: **Sources** (named atomic operators) and **Rules** (boolean compositions of sources). Each Rule maps to one X Filtered Stream rule. Sources are no longer pushed to X directly; they are atoms that Rules reference.

This replaces the current 1:1 `source → x_rule` mapping and enables complex filter expressions (AND/OR/NOT) over named, reusable source atoms.

## Motivation

The current design creates one X API rule per source, meaning all sources are OR-combined by X. There is no way to express AND logic, negation, or compound conditions (e.g., "tweets from BBCPersian in Farsi that are not retweets"). The new design separates the concept of a named filter atom (Source) from a composed filter rule (Rule), enabling arbitrary boolean combinations.

---

## Section 1: Data Model

### `sources` table — modified

Two new columns, `x_rule_id` removed:

| Column | Type | Notes |
|---|---|---|
| `id` | INTEGER PK | unchanged |
| `name` | TEXT | human label (new — was implicit from value) |
| `type` | TEXT | `account \| keyword \| hashtag \| cashtag \| mention \| phrase \| url \| url_contains \| to_account \| retweets_of \| context \| entity \| conversation \| place \| place_country \| point_radius \| bounding_box \| lang \| is \| has \| followers_count \| tweets_count \| following_count \| listed_count \| sample` |
| `class` | TEXT | `atom` (standalone) or `modifier` (conjunction-required) |
| `operator` | TEXT | X syntax prefix: `from:`, `to:`, `retweets_of:`, `url:`, `url_contains:`, `lang:`, `place_country:`, `place:`, `point_radius:`, `bounding_box:`, `context:`, `entity:`, `conversation_id:`, `is:`, `has:`, `followers_count:`, `tweets_count:`, `following_count:`, `listed_count:`, `sample:`, or `""` (bare — keyword, `#hashtag`, `@mention`, `$cashtag`, phrase) |
| `value` | TEXT | the operator argument; for bare types, includes the sigil (`#iran`, `@bbc`) |
| `active` | BOOLEAN | unchanged |
| `created_at` | DATETIME | unchanged |
| `updated_at` | DATETIME | unchanged |

`x_rule_id` is removed. Sources no longer map to X rules.

**Atom operators** (class = `atom`, can stand alone in a rule):
keyword, phrase, hashtag (`#`), cashtag (`$`), mention (`@`), `from:`, `to:`, `retweets_of:`, `url:`, `url_contains:`, `context:`, `entity:`, `conversation_id:`, `place:`, `place_country:`, `point_radius:`, `bounding_box:`

**Modifier operators** (class = `modifier`, conjunction-required — must be paired with at least one atom):
`lang:`, `is:retweet`, `is:reply`, `is:quote`, `is:verified`, `has:hashtags`, `has:cashtags`, `has:links`, `has:mentions`, `has:media`, `has:images`, `has:video_link`, `has:geo`, `followers_count:`, `tweets_count:`, `following_count:`, `listed_count:`, `sample:`

Source `render()` logic:
- `operator == ""` → bare `value`
- otherwise → `operator + value`

### `rules` table — new

| Column | Type | Notes |
|---|---|---|
| `id` | INTEGER PK | |
| `name` | TEXT | human label |
| `expression` | TEXT (JSON) | boolean expression tree (see Section 2) |
| `compiled_value` | TEXT | X syntax string derived from tree, stored for inspection |
| `x_rule_id` | TEXT | X-assigned rule ID after push |
| `active` | BOOLEAN | |
| `created_at` | DATETIME | |
| `updated_at` | DATETIME | |

### `tweet_rules` table — replaces `tweet_sources`

| Column | Type | Notes |
|---|---|---|
| `tweet_id` | TEXT FK → tweets | |
| `rule_id` | INTEGER FK → rules | |

Composite PK on `(tweet_id, rule_id)`. Indexed on both columns individually.

`tweet_sources` is dropped in the migration.

---

## Section 2: Expression Tree

The `expression` JSON field stores a recursive tree. Every node is one of:

### Leaf nodes

**Source reference:**
```json
{"src": 42}
```
Renders as the source's compiled value (e.g., `from:BBCPersian`).

**Inline operator** (one-off, not saved as a source):
```json
{"op": "is:retweet"}
```
Renders literally.

### Logical nodes

```json
{"AND": [node, node, ...]}
{"OR":  [node, node, ...]}
{"NOT": node}
```

- `AND` → children joined by space: `a b c`
- `OR` → children wrapped in parens: `(a OR b OR c)`
- `NOT` → prefixed with `-`: `-child` (compound children wrapped in parens: `-(a b)`)

### Example

"Tweets from BBCPersian or IranIntl, in Farsi, not a retweet":

```json
{
  "AND": [
    {"OR": [{"src": 1}, {"src": 2}]},
    {"src": 10},
    {"NOT": {"op": "is:retweet"}}
  ]
}
```

Compiles to: `(from:BBCPersian OR from:IranIntl) lang:fa -is:retweet`

### Validation rules (enforced at save time)

1. Tree must contain at least one leaf node.
2. Tree must contain at least one atom-class source or inline op that is not a conjunction-required modifier. Modifier-only rules are rejected.
3. Compiled string length must not exceed tier limit: 1024 chars (pay-per-use) or 2048 chars (enterprise).
4. All `{"src": N}` references must point to existing, active sources.
5. `NOT` node must have exactly one child.
6. `AND`/`OR` nodes must have at least two children.

---

## Section 3: Compiler

Package: `internal/stream`

`RuleCompiler` is a pure, stateless struct with no I/O dependencies. It accepts an expression tree and a `SourceResolver` (function `func(id int64) (*model.Source, error)`) and returns the compiled X syntax string.

```
compile(node, resolver) → string:
  {"src": id}      → resolver(id).render()
  {"op": "..."}    → literal string
  {"AND": [...]}   → strings.Join(map(compile, children), " ")
  {"OR":  [...]}   → "(" + strings.Join(map(compile, children), " OR ") + ")"
  {"NOT": child}   → "-" + maybeWrap(compile(child))
```

`maybeWrap`: wraps the compiled child in parentheses if it contains a space (i.e., is a compound expression).

`compiled_value` on the rule is populated by running the compiler at creation/update time and persisted for human inspection. Before pushing to X, the rule is always recompiled fresh (in case source values changed).

The compiler is fully unit-testable in isolation with a mock resolver.

---

## Section 4: API

### Modified endpoints

| Endpoint | Change |
|---|---|
| `GET /api/sources` | Adds `?class=atom\|modifier` query filter |
| `POST /api/sources` | Body gains `name`, `class`, `operator` fields. No longer pushes to X. No `x_rule_id` in response. |
| `DELETE /api/sources/:id` | Rejects with 409 if source is referenced by any active rule |

### New endpoints

| Endpoint | Description |
|---|---|
| `GET /api/rules` | List rules. Supports `?active=true\|false`. |
| `POST /api/rules` | Create rule. Body: `{"name": "...", "expression": {...}}`. Validates tree, compiles, pushes to X, stores `x_rule_id`. |
| `GET /api/rules/:id` | Single rule including `expression` and `compiled_value`. |
| `PUT /api/rules/:id` | Update name or expression. Recompiles and updates X rule if expression changed. |
| `DELETE /api/rules/:id` | Removes from X, deletes locally. Fails if rule has associated tweets (use deactivate instead). |
| `POST /api/rules/:id/activate` | Pushes to X if not already active. |
| `POST /api/rules/:id/deactivate` | Removes from X rule, keeps record locally. |

### Stream normalization

`normalize.go` is unchanged. X still returns `matching_rules` with rule IDs. The stream listener resolves `x_rule_id` → `rules.id` (instead of `sources.id`) when writing `tweet_rules` junction rows.

---

## Section 5: CLI

### Source commands — updated flags

```
iran-watcher source add \
  --name "BBC Persian" \
  --class atom \
  --operator "from:" \
  --value "BBCPersian"

iran-watcher source add \
  --name "Farsi only" \
  --class modifier \
  --operator "lang:" \
  --value "fa"
```

`iran-watcher source list` gains `--class atom|modifier` filter.

### Rule commands — new subcommand

```
iran-watcher rule add "Iran BBC" --expr '{"AND":[{"src":1},{"src":10}]}'
iran-watcher rule add "Iran BBC" --file rule.json
iran-watcher rule list [--active]
iran-watcher rule show <id>          # shows expression tree + compiled X syntax
iran-watcher rule update <id> --expr '...'
iran-watcher rule update <id> --file rule.json
iran-watcher rule remove <id>
iran-watcher rule activate <id>
iran-watcher rule deactivate <id>
```

`rule show` output:

```
Name:     Iran BBC
Active:   true
X Rule:   1234567890

Expression:
  {"AND": [{"OR": [{"src": 1}, {"src": 2}]}, {"src": 10}, {"NOT": {"op": "is:retweet"}}]}

Compiled:
  (from:BBCPersian OR from:IranIntl) lang:fa -is:retweet
```

---

## Section 6: Migration

Migration file: `migrations/003_rule_builder.sql`

1. Add `name`, `class`, `operator` columns to `sources`.
2. Backfill existing sources: set `class = 'atom'`, `operator` based on existing `type` (`account` → `from:`, `keyword` → `""`), `name = value`.
3. Drop `x_rule_id` from `sources`.
4. Create `rules` table.
5. Create `tweet_rules` table with indexes.
6. Drop `tweet_sources` table.

Existing sources are preserved as atoms. No existing X rules are deleted (they remain active on X until the user creates Rule records and deactivates/deletes old ones — or a cleanup command can be provided).

---

## Section 7: Testing

- **`RuleCompiler` unit tests** — table-driven, mock resolver, cover: single leaf, AND/OR/NOT, nested combinations, `NOT` of compound, modifier-only rejection, length limit, missing source.
- **Validation unit tests** — each validation rule tested independently.
- **API handler tests** — mock store, cover create/update/activate/deactivate/delete rule; source delete rejection when referenced.
- **Store tests** — in-memory SQLite, cover `SaveRule`, `GetRule`, `ListRules`, `DeleteRule`, `SetRuleActive`, `SaveTweetRule`, migration backfill.
- **Integration tests** (`//go:build integration`) — full round-trip: create sources → create rule → simulate stream event with matching `x_rule_id` → verify `tweet_rules` row written.
