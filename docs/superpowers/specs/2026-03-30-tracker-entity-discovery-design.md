# Tracker — Entity Discovery Design

**Date:** 2026-03-30
**Feature:** Entity Discovery for the Tracker (formerly "Tracked Entities")

---

## Overview

The Tracker feature is being extended from a purely manual entity registry into a discovery-capable system. Instead of requiring users to define every entity upfront, the system can now extract entities from incoming tweets automatically, surface them as candidates, and either auto-promote or queue them for human review.

The feature is renamed from "Tracked Entities" to **Tracker** throughout the UI (page title, menu item, route: `/tracker`). Backend API routes (`/api/tracked-entities`) retain their current paths — renaming is deferred to a later pass.

---

## Goals

- Extract entities from tweets via ONNX NER pipelines and Ollama LLM pipelines
- Accumulate discovered entities as candidates with frequency and confidence tracking
- Auto-promote candidates that cross configurable thresholds (confidence + frequency)
- Queue the rest for manual review (promote, reject, or merge into an existing entity)
- Keep `tracked_entities` and `entity_tracks` unchanged — promoted candidates become normal `TrackedEntity` rows

---

## Data Model

### New table: `entity_candidates`

```sql
CREATE TABLE entity_candidates (
    id                  INTEGER PRIMARY KEY AUTOINCREMENT,
    word                TEXT NOT NULL,
    type                TEXT NOT NULL,
    occurrence_count    INTEGER NOT NULL DEFAULT 1,
    avg_confidence      REAL NOT NULL DEFAULT 0,
    sources             TEXT NOT NULL DEFAULT '[]',  -- JSON array of pipeline IDs
    status              TEXT NOT NULL DEFAULT 'pending', -- pending | promoted | rejected
    promoted_entity_id  INTEGER REFERENCES tracked_entities(id) ON DELETE SET NULL,
    auto_promoted       INTEGER NOT NULL DEFAULT 0,
    first_seen_at       DATETIME NOT NULL,
    last_seen_at        DATETIME NOT NULL,
    UNIQUE(word, type)
);
```

Each row represents a unique `(word, type)` pair discovered across tweets. When the same pair is seen again, the row is upserted: `occurrence_count` increments, `avg_confidence` is recalculated as a rolling average (`new_avg = (old_avg * (count-1) + new_score) / count`), `last_seen_at` updates, and the contributing pipeline ID is appended to `sources` if not already present.

### Unchanged tables

- `tracked_entities` — unchanged; promoted candidates create normal rows here
- `entity_tracks` — unchanged; once an entity is promoted, `entity_matcher` creates tracks against it on future tweets

---

## `entity_discovery` Pipeline

A new configurable internal pipeline registered in the pipeline registry as `"entity_discovery"`. It shows up in the Pipelines page like any other pipeline handler, with users creating instances that each carry their own config.

### Config (stored in `pipelines.config` JSON column)

```json
{
  "min_confidence": 0.75,
  "min_frequency": 3,
  "auto_promote_types": ["LOC", "ORG"],
  "entity_key": "entities"
}
```

| Field | Type | Default | Description |
|-------|------|---------|-------------|
| `min_confidence` | float | 0.75 | Avg confidence must be ≥ this for auto-promotion |
| `min_frequency` | int | 3 | Candidate must appear in ≥ this many distinct tweets for auto-promotion |
| `auto_promote_types` | []string | [] | Entity types eligible for auto-promotion; empty = all types queue for manual review |
| `entity_key` | string | `"entities"` | Which metadata key to read from context |

Both `min_confidence` AND `min_frequency` must pass for auto-promotion to occur.

### Behavior

1. Reads the `entity_key` value from the merged execution context
2. For each entity in the list, upserts into `entity_candidates`
3. After upsert, evaluates auto-promotion:
   - `avg_confidence >= min_confidence`
   - `occurrence_count >= min_frequency`
   - `type` is in `auto_promote_types` (if configured)
   - If all pass: creates a `TrackedEntity` row, sets `status = promoted`, `auto_promoted = 1`, `promoted_entity_id`
4. Writes output to `action_execution_metadata`:

```json
{
  "discovered": 3,
  "auto_promoted": 1,
  "candidates": [
    {"word": "Tehran", "type": "LOC", "status": "promoted"},
    {"word": "IRGC", "type": "ORG", "status": "pending"},
    {"word": "Khamenei", "type": "PER", "status": "pending"}
  ]
}
```

Chain conditions can react to this output — e.g., trigger a notification when `auto_promoted > 0`.

---

## Extraction Sources

The `entity_discovery` pipeline is source-agnostic — it reads a normalized `entities` array from context regardless of which upstream pipeline produced it.

### ONNX NER

Already produces the expected format. No changes needed:

```json
{
  "entities": [
    {"word": "Tehran", "label": "LOC", "score": 0.95}
  ]
}
```

The `entity_discovery` pipeline maps `label` → `type` when reading ONNX output.

### Ollama

The Ollama pipeline's prompt must be written to produce the same normalized format. Example system prompt fragment:

```
Extract all named entities from the tweet text. Return a JSON object with an "entities" array. Each entity must have: "word" (the extracted string), "label" (one of: PER, LOC, ORG, MISC), and "score" (your confidence as a float 0–1).
```

The `entity_discovery` pipeline maps `label` → `type` from Ollama output as well. The Ollama pipeline stores its output under its configured output key, which becomes the `entity_key` the discovery pipeline reads.

---

## Chain Topology

A typical discovery chain looks like:

```
tweet arrives
    └── ONNX NER or Ollama extraction  →  "entities" in metadata
            ├── entity_discovery        →  writes candidates, auto-promotes if thresholds met
            └── entity_matcher          →  matches against tracked_entities, writes entity_tracks
```

Both `entity_discovery` and `entity_matcher` can run in parallel from the same extraction node since they both read `entities` from context and write to different stores.

---

## API Routes

New routes for candidate management (admin only):

| Method | Route | Description |
|--------|-------|-------------|
| GET | `/api/entity-candidates` | List candidates. Filters: `?status=`, `?type=`, `?page=`, `?per_page=` |
| GET | `/api/entity-candidates/{id}` | Get single candidate |
| POST | `/api/entity-candidates/{id}/promote` | Manually promote → creates `TrackedEntity`, sets `status=promoted` |
| POST | `/api/entity-candidates/{id}/reject` | Mark as rejected |
| POST | `/api/entity-candidates/{id}/merge` | Add candidate's `word` as alias on an existing entity. Body: `{"entity_id": N}` |
| DELETE | `/api/entity-candidates/{id}` | Delete candidate entirely |

**promote**: Creates a `TrackedEntity` with `name = word`, `type = type`. Sets `status = promoted`, `auto_promoted = 0`, `promoted_entity_id`.

**merge**: Appends `word` to the target entity's `aliases` JSON array. Sets `status = promoted`, `promoted_entity_id` = target entity ID.

---

## UI Changes

### Rename

- Menu item: "Tracked Entities" → **"Tracker"**
- Page title: **"Tracker"**
- Route: `/tracked-entities` → `/tracker`

### Tabs on `/tracker`

Two tabs are added to the Tracker page:

1. **Entities** — existing tracked entities list (unchanged behavior)
2. **Candidates (N)** — new tab; `N` is the live count of `status = pending` candidates

### Candidates tab

- List view showing: word, type, occurrence count, avg confidence, first/last seen, status badge
- Filters: status (`pending` / `promoted` / `rejected`), type
- Pagination (same pattern as entities list)
- Per-row actions:
  - **Promote** — one-click, creates `TrackedEntity` immediately
  - **Reject** — marks done, removes from pending count
  - **Merge** — opens a small modal to search and select an existing entity; candidate's word is added as an alias

### Pipelines page

The `entity_discovery` handler appears in the pipeline type dropdown. Its config form shows the four config fields (`min_confidence`, `min_frequency`, `auto_promote_types`, `entity_key`) with sensible defaults pre-filled.

---

## Error Handling

- If `entity_key` is absent from context, the pipeline logs a warning and returns an empty result (no error) — this allows the pipeline to be wired into chains where extraction may be optional
- If auto-promotion creates a `TrackedEntity` that conflicts with an existing name (UNIQUE violation), the candidate is left as `pending` and a warning is logged — the user resolves via the Candidates tab
- Upsert conflicts on `entity_candidates(word, type)` are handled with `ON CONFLICT DO UPDATE` — safe for concurrent worker execution

---

## Testing

- Unit tests for `entity_discovery` pipeline: upsert logic, rolling average calculation, auto-promotion threshold evaluation
- Unit tests for each API handler: promote, reject, merge edge cases (duplicate name, missing entity)
- In-memory SQLite for store tests
- No integration tests required for this feature (no external dependencies beyond SQLite)
