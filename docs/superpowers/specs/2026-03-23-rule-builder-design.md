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

Two new columns added, `x_rule_id` removed:

| Column | Type | Notes |
|---|---|---|
| `id` | INTEGER PK | unchanged |
| `name` | TEXT | human label (new — was implicit from value) |
| `type` | TEXT | logical category name (see type/operator mapping below) |
| `class` | TEXT | `atom` (standalone) or `modifier` (conjunction-required) |
| `operator` | TEXT | X syntax prefix stored explicitly (see below). `type` is a logical name; `operator` is the X API string. They are related but stored independently. |
| `value` | TEXT | the operator argument; for bare types, includes the sigil (`#iran`, `@bbc`) |
| `active` | BOOLEAN | defaults to `true` on creation |
| `created_at` | DATETIME | unchanged |
| `updated_at` | DATETIME | unchanged |

`x_rule_id` is removed. Sources no longer map to X rules.

**Type/operator/class mapping** (canonical reference for the CLI and validator):

| type | operator | class |
|---|---|---|
| `keyword` | `""` (bare) | atom |
| `phrase` | `""` (bare, value is quoted string) | atom |
| `hashtag` | `""` (bare, value includes `#`) | atom |
| `cashtag` | `""` (bare, value includes `$`) | atom |
| `mention` | `""` (bare, value includes `@`) | atom |
| `account` | `from:` | atom |
| `to_account` | `to:` | atom |
| `retweets_of` | `retweets_of:` | atom |
| `url` | `url:` | atom |
| `url_contains` | `url_contains:` | atom |
| `context` | `context:` | atom |
| `entity` | `entity:` | atom |
| `conversation_id` | `conversation_id:` | atom |
| `place` | `place:` | atom |
| `place_country` | `place_country:` | atom |
| `point_radius` | `point_radius:` | atom |
| `bounding_box` | `bounding_box:` | atom |
| `lang` | `lang:` | modifier |
| `is` | `is:` | modifier |
| `has` | `has:` | modifier |
| `followers_count` | `followers_count:` | modifier |
| `tweets_count` | `tweets_count:` | modifier |
| `following_count` | `following_count:` | modifier |
| `listed_count` | `listed_count:` | modifier |
| `sample` | `sample:` | modifier |

`type` is the logical category; `operator` is the X API prefix stored in the DB. For bare types, `operator = ""` and the value already contains any sigil.

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
| `x_rule_id` | TEXT | X-assigned rule ID after push; empty if inactive |
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
Renders literally. Inline ops are classified for validation using a hardcoded modifier set in the validator (see validation rule 2 below).

### Logical nodes

```json
{"AND": [node, node, ...]}
{"OR":  [node, node, ...]}
{"NOT": node}
```

- `AND` → children joined by space: `a b c`
- `OR` → children wrapped in parens: `(a OR b OR c)`
- `NOT` → prefixed with `-`: wraps child in parens if child node type is AND, OR, or phrase (i.e., based on node type, not string content). Single-token children render as `-child` directly.

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
2. Tree must contain at least one standalone atom. A tree is atom-free if every leaf is either a `{"src": N}` with `class = modifier` or a `{"op": "..."}` whose value appears in the hardcoded modifier op set: `{"is:retweet", "is:reply", "is:quote", "is:verified", "has:hashtags", "has:cashtags", "has:links", "has:mentions", "has:media", "has:images", "has:video_link", "has:geo"}`. Modifier-only trees are rejected with a descriptive error.
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
  {"NOT": child}   → "-" + wrap(child, compile(child))
```

`wrap(node, compiled)`: parenthesizes based on child **node type**, not string content:
- child is `AND` or `OR` node → `"(" + compiled + ")"`
- child is `{"src": N}` or `{"op": "..."}` → no wrapping (single-token)

This avoids false positives from phrase values that contain internal spaces (e.g., `"breaking news"` renders as `"breaking news"` and is a single X token even though it contains a space).

`compiled_value` on the rule is populated by running the compiler at creation/update time and persisted for human inspection. Before pushing to X, the rule is always recompiled fresh to pick up any source value changes.

The compiler is fully unit-testable in isolation with a mock resolver.

---

## Section 4: API

### Modified endpoints

| Endpoint | Change |
|---|---|
| `GET /api/sources` | Adds `?class=atom\|modifier` query filter |
| `POST /api/sources` | Body gains `name`, `class`, `operator` fields. Defaults `active=true`. No longer pushes to X. No `x_rule_id` in response. |
| `DELETE /api/sources/:id` | Rejects with 409 if source is referenced by any rule (active or inactive) |
| `POST /api/sources/:id/deactivate` | Rejects with 409 if source is referenced by any **active** rule. Deactivating a source used by inactive rules is allowed. |
| `GET /api/tweets` | `?source_id=` filter replaced by `?rule_id=` |

### New endpoints

| Endpoint | Description |
|---|---|
| `GET /api/rules` | List rules. Supports `?active=true\|false`. |
| `POST /api/rules` | Create rule. Body: `{"name": "...", "expression": {...}}`. Validates tree, compiles, pushes to X (sets `x_rule_id`), sets `active=true`. |
| `GET /api/rules/:id` | Single rule including `expression` and `compiled_value`. |
| `PUT /api/rules/:id` | Update `name` or `expression`. If expression changed and rule is active: deletes existing X rule, creates new one, updates `x_rule_id`. If rule is inactive: recompiles and stores without X API calls. |
| `DELETE /api/rules/:id` | Allowed only if rule has no associated `tweet_rules` rows. Removes from X if active, then deletes locally. If the rule has tweet history, returns 409 — use deactivate to stop matching without losing history. |
| `POST /api/rules/:id/activate` | Pushes to X, stores `x_rule_id`, sets `active=true`. Validates that all referenced sources are still active before pushing. |
| `POST /api/rules/:id/deactivate` | Deletes X rule, clears `x_rule_id`, sets `active=false`. Keeps local record and tweet history intact. |

**Rule update X API note:** X Filtered Stream has no PATCH operation for rules. Updating an active rule's expression requires deleting the old X rule and creating a new one. The old `x_rule_id` is replaced with the new one. `tweet_rules` rows written under the old `x_rule_id` retain their `rule_id` reference (unchanged — same rule, new X rule ID).

### Stream normalization

`normalize.go` is unchanged. X still returns `matching_rules` with rule IDs. The stream listener resolves `x_rule_id` → `rules.id` (instead of `sources.id`) when writing `tweet_rules` junction rows.

---

## Section 5: CLI

### Source commands — updated flags

```
iran-watcher source add \
  --name "BBC Persian" \
  --type account \
  --value "BBCPersian"

iran-watcher source add \
  --name "Farsi only" \
  --type lang \
  --value "fa"
```

`--type` determines `class` and `operator` automatically via the canonical mapping table. Users specify `--type`, not `--class`/`--operator` directly — the CLI derives those. `active` defaults to `true`; pass `--no-active` to create an inactive source.

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

1. Add `name`, `class`, `operator` columns to `sources` (nullable initially for backfill).
2. Backfill existing sources:
   - `type = 'account'` → `class = 'atom'`, `operator = 'from:'`, `name = value`
   - `type = 'keyword'` → `class = 'atom'`, `operator = ''`, `name = value`
   - `name` is set to the raw `value` as a placeholder; users are expected to update names post-migration to human-readable labels.
3. Apply NOT NULL constraint on `name`, `class`, `operator` after backfill.
4. Drop `x_rule_id` column from `sources`.
5. Create `rules` table.
6. Create `tweet_rules` table with indexes.
7. Drop `tweet_sources` table.

Existing sources are preserved as atoms. Existing X rules (previously pushed per-source) remain active on X but are now orphaned — no local `rules` record references them. The user should delete stale X rules via the X developer portal or a one-time cleanup command, and recreate them as proper Rule records.

---

## Section 7: Testing

- **`RuleCompiler` unit tests** — table-driven, mock resolver, cover: single leaf, AND/OR/NOT, nested combinations, `NOT` of AND/OR child (parens), `NOT` of single source (no parens), `NOT` of phrase source (no extra parens), modifier-only rejection, inline modifier op rejection, length limit, missing source.
- **Validation unit tests** — each of the 6 validation rules tested independently, including the hardcoded modifier op set check.
- **API handler tests** — mock store, cover: create/update/activate/deactivate/delete rule; source delete rejection when referenced; source deactivate rejection when used by active rule; `PUT` expression change triggers X delete+create; `PUT` on inactive rule skips X API.
- **Store tests** — in-memory SQLite, cover `SaveRule`, `GetRule`, `ListRules`, `DeleteRule`, `SetRuleActive`, `SaveTweetRule`, `ListTweetsByRuleID`, migration backfill correctness.
- **Integration tests** (`//go:build integration`) — full round-trip: create sources → create rule → simulate stream event with matching `x_rule_id` → verify `tweet_rules` row written, `GET /api/tweets?rule_id=` returns the tweet.
