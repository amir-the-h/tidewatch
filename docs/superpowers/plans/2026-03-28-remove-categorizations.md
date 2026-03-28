# Remove Categorizations — Migration Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Replace the `categorizations` table and all code that reads/writes it with `action_execution_metadata`, which is the canonical output store for all pipeline runs.

**Architecture:** Every non-internal pipeline run already writes outputs to `action_execution_metadata` (key-value pairs, JSON-encoded values). The `categorizations` table is a legacy parallel write from `worker/manager.go`. Four other subsystems depend on it: `GET /api/categorizations` (map view), `ListSummaries`/`CountSummaries` (summaries view), the `handleGetTweet` response, and the DB delete cascade. This plan removes the dual-write, adds a new `GET /api/map` endpoint backed by execution metadata, rewrites summaries queries, drops the API route and frontend view, then drops the DB table.

**Tech Stack:** Go 1.25.5, SQLite via `modernc.org/sqlite`, Vue 3 + Naive UI, chi router.

---

## File Map

| Change | File |
|--------|------|
| Remove `saveCategorization` call + method | `internal/worker/manager.go` |
| Add `GET /api/map` route | `internal/api/server.go` |
| Add `handleListMapPins` handler | `internal/api/handlers_map.go` ← new |
| Add `ListMapPins` to store interface | `internal/store/store.go` |
| Add `ListMapPins` SQLite impl | `internal/store/sqlite.go` |
| Remove `SaveCategorization`, `ListCategorizations`, `ListUncategorizedTweets` impl | `internal/store/sqlite.go` |
| Remove same methods + `CategorizationFilter` from interface | `internal/store/store.go` |
| Remove `Categorization` struct | `internal/model/categorization.go` → deleted |
| Rewrite `ListSummaries` / `CountSummaries` queries | `internal/store/sqlite.go` |
| Update `SummaryItem` comment | `internal/model/summary.go` |
| Remove `categorizations` from `handleGetTweet` response | `internal/api/handlers_tweets.go` |
| Remove `GET /api/categorizations` route | `internal/api/server.go` |
| Delete old handler file | `internal/api/handlers_categorizations.go` → deleted |
| Remove `ListCategorizations`, `SaveCategorization`, `ListUncategorizedTweets` stubs from mock stores | `internal/worker/worker_test.go`, `internal/stream/stream_test.go`, `internal/notifier/dispatcher_test.go`, `internal/api/api_test.go` |
| Add `ListMapPins` stub to mock stores | same four test files |
| Add map API client | `web/src/api/map.ts` ← new |
| Rewrite MapView to use new endpoint | `web/src/views/MapView.vue` |
| Remove route + nav entry | `web/src/router/index.ts`, `web/src/App.vue` |
| Delete frontend files | `web/src/views/CategorizationsView.vue`, `web/src/api/categorizations.ts`, `web/src/stores/categorizations.ts` → deleted |
| Remove `Categorization` type and field from tweets API | `web/src/api/tweets.ts` |
| Remove `categorizations` section from tweet drawer | `web/src/views/TweetsView.vue` |
| Add DROP TABLE migration | `migrations/018_drop_categorizations.sql` ← new |
| Update CLAUDE.md | `CLAUDE.md` |

---

### Task 1: Remove the dual-write from the worker

**Files:**
- Modify: `internal/worker/manager.go`

- [ ] **Step 1: Delete the `saveCategorization` call and the method**

In `internal/worker/manager.go`, remove the call on lines 156–158 inside `processTask`:

```go
// DELETE these 3 lines:
if entry.Pipeline.Type() != inference.PipelineTypeInternal && ev.TweetID != "" {
    m.saveCategorization(ctx, entry.Pipeline.Name(), ev.TweetID, outputs)
}
```

And remove the entire method on lines 167–181:

```go
// DELETE this entire method:
func (m *Manager) saveCategorization(ctx context.Context, pipelineName, tweetID string, outputs map[string]any) {
    b, err := json.Marshal(outputs)
    if err != nil {
        return
    }
    c := &model.Categorization{
        TweetID:   tweetID,
        ModelName: pipelineName,
        Results:   b,
        CreatedAt: time.Now(),
    }
    if err := m.store.SaveCategorization(ctx, c); err != nil && m.logger != nil {
        m.logger.Warn("worker: save categorization", "pipeline", pipelineName, "tweet_id", tweetID, "error", err)
    }
}
```

The `model` import is still needed for `model.ActionExecution` and `model.ExecutionStatus*` — keep it.

- [ ] **Step 2: Commit**

```bash
git add internal/worker/manager.go
git commit -m "feat: remove categorization dual-write from worker"
```

---

### Task 2: Add `GET /api/map` backed by `action_execution_metadata`

**Files:**
- Create: `internal/api/handlers_map.go`
- Modify: `internal/api/server.go`
- Modify: `internal/store/store.go`
- Modify: `internal/store/sqlite.go`
- Modify four mock store files (add `ListMapPins` stub)

The map view needs: tweet_id, tweet_created_at (for date-heat coloring), model_name (for popup), description (from metadata), and locations array (from metadata). Each row in the result is one action execution that produced a non-empty `locations` array.

- [ ] **Step 1: Add `MapPin` model type and `ListMapPins` to the store interface**

Add to `internal/store/store.go` (near the Summaries section, or its own section):

```go
// MapPin is a geo-event pin derived from action_execution_metadata.
type MapPin struct {
    TweetID        string  `json:"tweet_id"`
    TweetCreatedAt string  `json:"tweet_created_at"`
    ModelName      string  `json:"model_name"`
    Description    string  `json:"description,omitempty"`
    LocationsJSON  string  `json:"-"` // raw JSON, parsed by handler
}
```

Add `ListMapPins` to the `Store` interface:

```go
// Map
ListMapPins(ctx context.Context) ([]*MapPin, error)
```

- [ ] **Step 2: Implement `ListMapPins` in `sqlite.go`**

Add to `internal/store/sqlite.go` (after the Summaries section):

```go
// ── Map ───────────────────────────────────────────────────────────────────────

func (s *sqliteStore) ListMapPins(ctx context.Context) ([]*MapPin, error) {
    query := `
        SELECT ae.tweet_id,
               COALESCE(t.created_at, ae.executed_at) AS tweet_created_at,
               p.name AS model_name,
               COALESCE(aem_desc.value, '') AS description,
               aem_loc.value AS locations_json
        FROM action_execution_metadata aem_loc
        JOIN action_executions ae ON ae.id = aem_loc.execution_id AND ae.status = 'success'
        JOIN actions a ON a.id = ae.action_id
        JOIN pipelines p ON p.id = a.pipeline_id
        LEFT JOIN tweets t ON t.id = ae.tweet_id
        LEFT JOIN action_execution_metadata aem_desc
               ON aem_desc.execution_id = ae.id AND aem_desc.key = 'description'
        WHERE aem_loc.key = 'locations'
          AND aem_loc.value != 'null'
          AND aem_loc.value != '[]'
        ORDER BY tweet_created_at DESC`

    rows, err := s.db.QueryContext(ctx, query)
    if err != nil {
        return nil, fmt.Errorf("store: list map pins: %w", err)
    }
    defer rows.Close()

    var pins []*MapPin
    for rows.Next() {
        var pin MapPin
        if err := rows.Scan(
            &pin.TweetID, &pin.TweetCreatedAt, &pin.ModelName,
            &pin.Description, &pin.LocationsJSON,
        ); err != nil {
            return nil, fmt.Errorf("store: list map pins: scan: %w", err)
        }
        pins = append(pins, &pin)
    }
    return pins, rows.Err()
}
```

- [ ] **Step 3: Create `internal/api/handlers_map.go`**

```go
package api

import (
    "encoding/json"
    "net/http"
)

type mapPinResponse struct {
    TweetID        string              `json:"tweet_id"`
    TweetCreatedAt string              `json:"tweet_created_at"`
    ModelName      string              `json:"model_name"`
    Description    string              `json:"description,omitempty"`
    Locations      []json.RawMessage   `json:"locations"`
}

func (s *Server) handleListMapPins(w http.ResponseWriter, r *http.Request) {
    pins, err := s.store.ListMapPins(r.Context())
    if err != nil {
        s.logger.Error("api: list map pins", "error", err)
        writeError(w, http.StatusInternalServerError, "failed to load map pins")
        return
    }

    out := make([]mapPinResponse, 0, len(pins))
    for _, pin := range pins {
        var locs []json.RawMessage
        if err := json.Unmarshal([]byte(pin.LocationsJSON), &locs); err != nil {
            continue // skip rows where locations JSON is malformed
        }
        // Strip entries without coordinates (guard against partial extractor output)
        valid := locs[:0]
        for _, l := range locs {
            var check struct {
                Coordinates []float64 `json:"coordinates"`
            }
            if json.Unmarshal(l, &check) == nil && len(check.Coordinates) == 2 {
                valid = append(valid, l)
            }
        }
        if len(valid) == 0 {
            continue
        }

        desc := pin.Description
        if len(desc) > 0 {
            // description is stored as a JSON-encoded string: `"text"` → strip quotes
            var s string
            if json.Unmarshal([]byte(desc), &s) == nil {
                desc = s
            }
        }

        out = append(out, mapPinResponse{
            TweetID:        pin.TweetID,
            TweetCreatedAt: pin.TweetCreatedAt,
            ModelName:      pin.ModelName,
            Description:    desc,
            Locations:      valid,
        })
    }

    writeJSON(w, http.StatusOK, out)
}
```

- [ ] **Step 4: Register the route in `server.go`**

In `internal/api/server.go`, inside the authenticated viewer-accessible route group (same area as `r.Get("/categorizations", ...)`), add:

```go
r.Get("/map", srv.handleListMapPins)
```

Keep the old `r.Get("/categorizations", ...)` line for now — it will be removed in Task 4.

- [ ] **Step 5: Add `ListMapPins` stub to the four mock stores**

Add to `internal/worker/worker_test.go`, `internal/stream/stream_test.go`, `internal/notifier/dispatcher_test.go`, and `internal/api/api_test.go`:

```go
func (m *mockStore) ListMapPins(_ context.Context) ([]*store.MapPin, error) {
    return nil, nil
}
```

- [ ] **Step 6: Run tests**

```bash
go test ./internal/store/... ./internal/api/... ./internal/worker/... ./internal/stream/... ./internal/notifier/...
```

Expected: All packages compile and tests pass.

- [ ] **Step 7: Commit**

```bash
git add internal/store/store.go internal/store/sqlite.go \
        internal/api/handlers_map.go internal/api/server.go \
        internal/worker/worker_test.go internal/stream/stream_test.go \
        internal/notifier/dispatcher_test.go internal/api/api_test.go
git commit -m "feat: add GET /api/map backed by action_execution_metadata"
```

---

### Task 3: Update `MapView.vue` to use the new `/api/map` endpoint

**Files:**
- Create: `web/src/api/map.ts`
- Modify: `web/src/views/MapView.vue`

- [ ] **Step 1: Create `web/src/api/map.ts`**

```ts
// web/src/api/map.ts
import { apiFetch } from './client'

export interface MapLocation {
  label?: string
  coordinates: [number, number]
  place?: string
}

export interface MapPin {
  tweet_id: string
  tweet_created_at: string
  model_name: string
  description?: string
  locations: MapLocation[]
}

export const listMapPins = () => apiFetch<MapPin[]>('/api/map')
```

- [ ] **Step 2: Rewrite `MapView.vue`**

Replace the entire `<script setup>` block. The template and `<style>` are unchanged.

```ts
<script setup lang="ts">
import { ref, onMounted, onUnmounted } from 'vue'
import L from 'leaflet'
import 'leaflet/dist/leaflet.css'
import { listMapPins } from '../api/map'
import type { MapPin, MapLocation } from '../api/map'

// Fix Leaflet's default icon paths broken by Vite bundling
import markerIcon2x from 'leaflet/dist/images/marker-icon-2x.png'
import markerIcon from 'leaflet/dist/images/marker-icon.png'
import markerShadow from 'leaflet/dist/images/marker-shadow.png'
delete (L.Icon.Default.prototype as any)._getIconUrl
L.Icon.Default.mergeOptions({ iconUrl: markerIcon, iconRetinaUrl: markerIcon2x, shadowUrl: markerShadow })

interface PinWithLoc {
  pin: MapPin
  loc: MapLocation
}

const mapEl    = ref<HTMLDivElement | null>(null)
const loading  = ref(true)
const pinCount = ref(0)
const error    = ref<string | null>(null)
const oldestDate = ref<string>('')
const newestDate = ref<string>('')

let map: L.Map | null = null

// Maps a normalized t ∈ [0,1] (0=oldest, 1=newest) to a hex color
// Spectrum: blue → cyan → green → yellow → red
function tToColor(t: number): string {
  const hue = Math.round((1 - t) * 240)
  return `hsl(${hue},90%,60%)`
}

onMounted(async () => {
  if (!mapEl.value) return

  map = L.map(mapEl.value, {
    center: [32.4, 53.7], // Iran centroid
    zoom: 5,
  })

  L.tileLayer('https://{s}.tile.openstreetmap.org/{z}/{x}/{y}.png', {
    attribution: '© <a href="https://www.openstreetmap.org/copyright">OpenStreetMap</a>',
    maxZoom: 18,
  }).addTo(map)

  try {
    const pins = await listMapPins()
    const items: PinWithLoc[] = []

    for (const pin of pins ?? []) {
      for (const loc of pin.locations ?? []) {
        if (loc.coordinates?.length === 2) items.push({ pin, loc })
      }
    }

    pinCount.value = items.length

    // Compute date range for the spectrum using the tweet's own created_at
    let minMs = Infinity
    let maxMs = -Infinity
    for (const { pin } of items) {
      const ms = new Date(pin.tweet_created_at).getTime()
      if (ms < minMs) minMs = ms
      if (ms > maxMs) maxMs = ms
    }
    const range = maxMs - minMs

    if (items.length > 0) {
      oldestDate.value = fmtDate(new Date(minMs))
      newestDate.value = fmtDate(new Date(maxMs))
    }

    for (const { pin, loc } of items) {
      const [lat, lon] = loc.coordinates
      const ms = new Date(pin.tweet_created_at).getTime()
      const t = range === 0 ? 0.5 : (ms - minMs) / range
      const color = tToColor(t)

      const marker = L.circleMarker([lat, lon], {
        radius: 6,
        color,
        fillColor: color,
        fillOpacity: 0.85,
        weight: 1.5,
      })

      const lines: string[] = []
      if (loc.label) lines.push(`<b>${escHtml(loc.label)}</b>`)
      if (loc.place) lines.push(escHtml(loc.place))
      if (pin.description) lines.push(escHtml(pin.description))
      lines.push(`<a href="/tweets?tweet=${pin.tweet_id}" style="color:${color}">Tweet ${pin.tweet_id}</a>`)
      lines.push(`<span style="color:#a6adc8;font-size:11px">${fmtDate(new Date(pin.tweet_created_at))}</span>`)
      lines.push(`<span style="color:#6c7086;font-size:11px">${escHtml(pin.model_name)}</span>`)

      marker.bindPopup(lines.join('<br>'), { maxWidth: 280 })
      marker.addTo(map!)
    }
  } catch (e: any) {
    error.value = e?.message ?? 'Failed to load data'
  } finally {
    loading.value = false
  }
})

onUnmounted(() => {
  map?.remove()
  map = null
})

function escHtml(s: string): string {
  return s.replace(/&/g,'&amp;').replace(/</g,'&lt;').replace(/>/g,'&gt;').replace(/"/g,'&quot;')
}

function fmtDate(d: Date): string {
  return d.toLocaleString(undefined, { month: 'short', day: 'numeric', hour: '2-digit', minute: '2-digit' })
}
</script>
```

The `<template>` and `<style>` blocks are **unchanged** — copy them as-is from the original file.

- [ ] **Step 3: Build the frontend**

```bash
cd web && npm run build 2>&1 | tail -20
```

Expected: No TypeScript errors.

- [ ] **Step 4: Commit**

```bash
git add web/src/api/map.ts web/src/views/MapView.vue
git commit -m "feat: rewrite MapView to use GET /api/map (action_execution_metadata)"
```

---

### Task 4: Rewrite `ListSummaries` and `CountSummaries` against `action_execution_metadata`

**Files:**
- Modify: `internal/store/sqlite.go` (`summaryFilterQuery`, `ListSummaries`, `CountSummaries`)
- Modify: `internal/model/summary.go` (update stale comment)

The current queries JOIN `categorizations` and `models` — `models` was already dropped in migration 016, so `ListSummaries` is currently broken for any DB that has run migration 016.

- [ ] **Step 1: Replace `summaryFilterQuery`, `ListSummaries`, and `CountSummaries` in `sqlite.go`**

Locate the `// ── Summaries ──` comment block (around line 1411) and replace the three functions:

```go
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
              JOIN pipelines p ON p.id = a.pipeline_id
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
              JOIN pipelines p ON p.id = a.pipeline_id
              JOIN action_execution_metadata aem ON aem.execution_id = ae.id AND aem.key = 'summary'` +
        filterClause

    var count int
    if err := s.db.QueryRowContext(ctx, query, args...).Scan(&count); err != nil {
        return 0, fmt.Errorf("store: count summaries: %w", err)
    }
    return count, nil
}
```

- [ ] **Step 2: Update the stale comment in `model/summary.go`**

Change line 5 from:
```go
// SummaryItem pairs a tweet with its summarizer output from the categorizations table.
```
to:
```go
// SummaryItem pairs a tweet with its summarizer output from action_execution_metadata.
```

- [ ] **Step 3: Run tests**

```bash
go test ./internal/store/... ./internal/api/...
```

Expected: All pass.

- [ ] **Step 4: Commit**

```bash
git add internal/store/sqlite.go internal/model/summary.go
git commit -m "feat: rewrite ListSummaries/CountSummaries against action_execution_metadata"
```

---

### Task 5: Remove `SaveCategorization`, `ListCategorizations`, `ListUncategorizedTweets` from the store

**Files:**
- Modify: `internal/store/store.go`
- Modify: `internal/store/sqlite.go`
- Delete: `internal/model/categorization.go`
- Modify: `internal/worker/worker_test.go`, `internal/stream/stream_test.go`, `internal/notifier/dispatcher_test.go`, `internal/api/api_test.go`

- [ ] **Step 1: Remove `CategorizationFilter` and the three interface methods from `store.go`**

In `internal/store/store.go`:

Delete the `CategorizationFilter` struct:
```go
// DELETE:
type CategorizationFilter struct {
    TweetID   *string
    ModelName *string
    From      *time.Time
    To        *time.Time
}
```

Delete the Categorizations section from the `Store` interface:
```go
// DELETE:
// Categorizations
SaveCategorization(ctx context.Context, c *model.Categorization) error
ListCategorizations(ctx context.Context, filter CategorizationFilter) ([]*model.Categorization, error)
ListUncategorizedTweets(ctx context.Context, modelNames []string) ([]*model.Tweet, error)
```

- [ ] **Step 2: Remove the three implementations + scanner from `sqlite.go`**

Delete `SaveCategorization` (~lines 641–661), `ListCategorizations` (~lines 663–707), `ListUncategorizedTweets` (~lines 887–932), and `scanCategorization` (~lines 1005–1023) from `internal/store/sqlite.go`.

- [ ] **Step 3: Delete the Categorization model**

```bash
rm internal/model/categorization.go
```

- [ ] **Step 4: Clean up mock stores**

In `internal/api/api_test.go`, the mock has functional stubs (not just no-ops) — remove:
- The `categorizations` field from the mock struct
- `SaveCategorization`, `ListCategorizations`, `ListUncategorizedTweets` methods
- The `Categorizations []*model.Categorization` field in the inline response struct in the `handleGetTweet` test (~line 500)

In `internal/worker/worker_test.go`, `internal/stream/stream_test.go`, and `internal/notifier/dispatcher_test.go`, delete the three stub methods:

```go
// DELETE from each:
func (m *mockStore) SaveCategorization(_ context.Context, _ *model.Categorization) error { return nil }
func (m *mockStore) ListCategorizations(_ context.Context, _ store.CategorizationFilter) ([]*model.Categorization, error) { return nil, nil }
func (m *mockStore) ListUncategorizedTweets(_ context.Context, _ []string) ([]*model.Tweet, error) { return nil, nil }
```

- [ ] **Step 5: Run tests**

```bash
go test ./internal/store/... ./internal/worker/... ./internal/stream/... ./internal/notifier/... ./internal/api/...
```

Expected: All packages compile and tests pass.

- [ ] **Step 6: Commit**

```bash
git add internal/store/store.go internal/store/sqlite.go internal/model/categorization.go \
        internal/worker/worker_test.go internal/stream/stream_test.go \
        internal/notifier/dispatcher_test.go internal/api/api_test.go
git commit -m "feat: remove SaveCategorization, ListCategorizations, ListUncategorizedTweets"
```

---

### Task 6: Remove the `GET /api/categorizations` route, handler, and `categorizations` from tweet detail

**Files:**
- Modify: `internal/api/server.go`
- Delete: `internal/api/handlers_categorizations.go`
- Modify: `internal/api/handlers_tweets.go`

- [ ] **Step 1: Remove the route from `server.go`**

Delete the line:
```go
r.Get("/categorizations", srv.handleListCategorizations)
```

- [ ] **Step 2: Delete the handler file**

```bash
rm internal/api/handlers_categorizations.go
```

- [ ] **Step 3: Remove `categorizations` from `handleGetTweet` in `handlers_tweets.go`**

Delete the block that calls `s.store.ListCategorizations` (~lines 90–97):
```go
// DELETE:
cats, err := s.store.ListCategorizations(r.Context(), store.CategorizationFilter{
    TweetID: &tweetID,
})
if err != nil {
    s.logger.Error("api: list categorizations for tweet", "tweet_id", id, "error", err)
    writeError(w, http.StatusInternalServerError, "failed to list categorizations")
    return
}
```

Update the final `writeJSON` call to remove `"categorizations": cats`:
```go
writeJSON(w, http.StatusOK, map[string]any{
    "tweet":      tweet,
    "executions": execResults,
})
```

- [ ] **Step 4: Run tests**

```bash
go test ./internal/api/...
```

Expected: All pass.

- [ ] **Step 5: Commit**

```bash
git add internal/api/server.go internal/api/handlers_categorizations.go internal/api/handlers_tweets.go
git commit -m "feat: remove GET /api/categorizations and categorizations from tweet detail"
```

---

### Task 7: Remove categorizations from the frontend

**Files:**
- Delete: `web/src/views/CategorizationsView.vue`, `web/src/api/categorizations.ts`, `web/src/stores/categorizations.ts`
- Modify: `web/src/router/index.ts`, `web/src/App.vue`, `web/src/api/tweets.ts`, `web/src/views/TweetsView.vue`

- [ ] **Step 1: Delete the three frontend files**

```bash
rm web/src/views/CategorizationsView.vue
rm web/src/api/categorizations.ts
rm web/src/stores/categorizations.ts
```

- [ ] **Step 2: Remove the route from `router/index.ts`**

Delete:
```ts
{ path: '/categorizations', component: () => import('../views/CategorizationsView.vue') },
```

- [ ] **Step 3: Remove the nav entry from `App.vue`**

Delete:
```ts
{ label: 'Categorizations',  key: '/categorizations' },
```

- [ ] **Step 4: Update `web/src/api/tweets.ts`**

Remove the import line at line 3:
```ts
// DELETE:
import type { CategorizationResults } from './categorizations'
```

Delete the `Categorization` interface (~lines 37–45):
```ts
// DELETE:
export interface Categorization {
  id: number
  tweet_id: string
  model_name: string
  model_version: string
  results: CategorizationResults
  created_at: string
  tweet_created_at: string
}
```

Update `TweetDetail` — remove `categorizations: Categorization[]`:
```ts
export interface TweetDetail {
  tweet: Tweet
  executions: TweetExecution[]
}
```

- [ ] **Step 5: Remove the Categorizations section from the tweet drawer in `TweetsView.vue`**

Search for any template block that renders `selectedTweet.categorizations` (or loops over it) and delete it. Also remove any import of `Categorization` from `'../api/tweets'` or `'../api/categorizations'` in the `<script setup>` block.

- [ ] **Step 6: Build the frontend**

```bash
cd web && npm run build 2>&1 | tail -30
```

Expected: Build succeeds with no TypeScript errors.

- [ ] **Step 7: Commit**

```bash
git add web/src/
git commit -m "feat: remove CategorizationsView and all categorizations from frontend"
```

---

### Task 8: DB migration to drop the categorizations table

**Files:**
- Create: `migrations/018_drop_categorizations.sql`

- [ ] **Step 1: Write the migration**

```sql
-- 018_drop_categorizations.sql
-- Superseded by action_execution_metadata.
-- All pipeline outputs are now stored as key-value pairs there instead.
DROP TABLE IF EXISTS categorizations;
```

> SQLite automatically drops all indexes on a table when the table is dropped — no explicit index cleanup needed.

- [ ] **Step 2: Verify against the live DB**

```bash
# Read the current row count so you know data is being dropped
sqlite3 ./data/iran-watcher.db "SELECT COUNT(*) FROM categorizations;"
# Apply the migration
sqlite3 ./data/iran-watcher.db < migrations/018_drop_categorizations.sql
# Confirm gone
sqlite3 ./data/iran-watcher.db ".tables" | tr ' ' '\n' | grep categoriz
```

Expected: Last command returns no output.

- [ ] **Step 3: Commit**

```bash
git add migrations/018_drop_categorizations.sql
git commit -m "feat: migration 018 — drop categorizations table"
```

---

### Task 9: Build verification and docs update

**Files:**
- Modify: `CLAUDE.md`

- [ ] **Step 1: Build the full Docker image**

```bash
docker compose build --build-arg BUILDKIT_INLINE_CACHE=1 iran-watcher
```

Expected: Build exits 0.

- [ ] **Step 2: Update CLAUDE.md**

In the **Key Design Decisions** section:

- Remove `categorizations` from the DB tables list under the event-driven actions bullet.
- Update the `GET /api/categorizations` route to `GET /api/map` in the REST API list.
- Remove the note about `ListUncategorizedTweets` under the reprocess endpoint.
- Add a note: `GET /api/map` returns geo-event pins sourced from `action_execution_metadata` (key `locations`), with `tweet_created_at` joined from the `tweets` table for date-heat coloring.
- Update **Map date-heat coloring**: replace mention of `GET /api/categorizations` with `GET /api/map`.

- [ ] **Step 3: Commit**

```bash
git add CLAUDE.md
git commit -m "docs: update CLAUDE.md — remove categorizations, document /api/map"
```

---

## Self-Review

**Spec coverage:**
- ✅ Worker dual-write removed (Task 1)
- ✅ New `/api/map` endpoint backed by action_execution_metadata (Task 2)
- ✅ MapView.vue rewritten to use new endpoint (Task 3)
- ✅ Summaries rewritten against action_execution_metadata (Task 4)
- ✅ Store interface + implementation cleaned up (Task 5)
- ✅ API route + handler deleted (Task 6)
- ✅ Frontend view + API client + store deleted (Task 7)
- ✅ DB table dropped via migration (Task 8)
- ✅ Build verified + docs updated (Task 9)

**Out of scope (follow-up tasks, not blocking):**
- `TriggerType = 'new_categorization'` in `web/src/api/actions.ts` — this is a trigger type string that exists in the DB schema and the Go model; renaming it would require a data migration and is a separate clean-up.
- `'tweet_categorized'` / `'categorization_failed'` in `web/src/api/notifications.ts` — notification event type names in the backend notifier; renaming is a separate refactor.
