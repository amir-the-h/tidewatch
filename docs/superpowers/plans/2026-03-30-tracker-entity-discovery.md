# Tracker Entity Discovery Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Extend the Tracker feature with automatic entity discovery — tweets feed extracted entities into a candidate queue that can auto-promote based on confidence + frequency thresholds, or queue for manual review.

**Architecture:** A new `entity_discovery` configurable internal pipeline consumes the normalized `entities` key from execution context (produced by ONNX NER or Ollama), upserts candidates into a new `entity_candidates` SQLite table, and auto-promotes when thresholds pass. The existing `tracked_entities` / `entity_tracks` tables and `entity_matcher` pipeline are unchanged — promoted candidates simply become `TrackedEntity` rows. The frontend renames "Tracked Entities" to "Tracker" and adds a "Candidates" tab.

**Tech Stack:** Go (pipeline, store, API), SQLite (new table via migration), Vue 3 + Naive UI (frontend tabs + candidate list)

---

## File Map

**New files:**
- `migrations/023_entity_candidates.sql` — new table DDL
- `internal/pipeline/entity_discovery.go` — entity_discovery pipeline implementation
- `internal/pipeline/entity_discovery_test.go` — pipeline unit tests
- `internal/api/handlers_entity_candidates.go` — API handlers for candidates
- `web/src/stores/candidates.ts` — Pinia store for entity candidates
- `web/src/views/TrackerView.vue` — renamed + extended tracked-entities view

**Modified files:**
- `internal/model/entity.go` — add `EntityCandidate` struct
- `internal/store/store.go` — add `EntityCandidateFilter` type + 6 new Store interface methods
- `internal/store/sqlite.go` — implement the 6 new Store methods
- `internal/store/sqlite_test.go` — add entity candidate store tests
- `cli/cmd/serve.go` — register `entity_discovery` in pipeline registry
- `internal/api/server.go` — register candidate API routes
- `web/src/api/entities.ts` — add `EntityCandidate` types + 6 API functions
- `web/src/views/PipelinesView.vue` — add entity_discovery config fields to form
- `web/src/router/index.ts` — `/tracked-entities` → `/tracker`
- `web/src/App.vue` — menu label + key rename
- `CLAUDE.md` — update Project Structure and Key Design Decisions

---

## Task 1: Migration + EntityCandidate model type

**Files:**
- Create: `migrations/023_entity_candidates.sql`
- Modify: `internal/model/entity.go`

- [ ] **Step 1: Write the failing migration test**

Open `migrations/migrations_test.go` and verify there is a test that runs all migrations on a fresh `:memory:` DB. If one exists, simply running it after adding the file will validate the migration. If not, the existing `TestRunMigrations` pattern will cover this implicitly via `newTestStore` in `sqlite_test.go`.

Run the existing migration test to establish baseline:
```bash
cd /mnt/shared/projects/personal/iran-watcher
go test ./migrations/... -v -run TestRunMigrations
```
Expected: PASS

- [ ] **Step 2: Create the migration file**

Create `migrations/023_entity_candidates.sql`:
```sql
CREATE TABLE IF NOT EXISTS entity_candidates (
    id                  INTEGER PRIMARY KEY AUTOINCREMENT,
    word                TEXT NOT NULL,
    type                TEXT NOT NULL,
    occurrence_count    INTEGER NOT NULL DEFAULT 1,
    avg_confidence      REAL NOT NULL DEFAULT 0,
    sources             TEXT NOT NULL DEFAULT '[]',
    status              TEXT NOT NULL DEFAULT 'pending',
    promoted_entity_id  INTEGER REFERENCES tracked_entities(id) ON DELETE SET NULL,
    auto_promoted       INTEGER NOT NULL DEFAULT 0,
    first_seen_at       DATETIME NOT NULL,
    last_seen_at        DATETIME NOT NULL,
    UNIQUE(word, type)
);

CREATE INDEX IF NOT EXISTS idx_entity_candidates_status ON entity_candidates(status);
CREATE INDEX IF NOT EXISTS idx_entity_candidates_type   ON entity_candidates(type);
```

- [ ] **Step 3: Run migration test to verify it passes**

```bash
go test ./migrations/... -v
go test ./internal/store/... -v -run TestNewSQLiteStore
```
Expected: PASS — migration applies cleanly, table exists

- [ ] **Step 4: Add EntityCandidate to internal/model/entity.go**

Append to the end of `internal/model/entity.go`:
```go
// EntityCandidate is an entity discovered from tweet text that has not yet
// been reviewed. Candidates are upserted on every extraction run; once they
// cross confidence + frequency thresholds they are auto-promoted to TrackedEntity.
type EntityCandidate struct {
	ID                int64      `json:"id"`
	Word              string     `json:"word"`
	Type              string     `json:"type"`
	OccurrenceCount   int        `json:"occurrence_count"`
	AvgConfidence     float64    `json:"avg_confidence"`
	Sources           []string   `json:"sources"` // pipeline names that contributed
	Status            string     `json:"status"`  // pending | promoted | rejected
	PromotedEntityID  *int64     `json:"promoted_entity_id,omitempty"`
	AutoPromoted      bool       `json:"auto_promoted"`
	FirstSeenAt       time.Time  `json:"first_seen_at"`
	LastSeenAt        time.Time  `json:"last_seen_at"`
}
```

- [ ] **Step 5: Compile check**

```bash
go build ./internal/model/...
```
Expected: no errors

- [ ] **Step 6: Commit**

```bash
git add migrations/023_entity_candidates.sql internal/model/entity.go
git commit -m "feat: add entity_candidates migration and EntityCandidate model"
```

---

## Task 2: Store interface + SQLite implementation + tests

**Files:**
- Modify: `internal/store/store.go`
- Modify: `internal/store/sqlite.go`
- Modify: `internal/store/sqlite_test.go`

- [ ] **Step 1: Write failing store tests**

Append to `internal/store/sqlite_test.go`:
```go
func TestEntityCandidate_InsertAndGet(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	c := &model.EntityCandidate{
		Word:          "Tehran",
		Type:          "LOC",
		AvgConfidence: 0.9,
		Sources:       []string{"ner-pipeline"},
		FirstSeenAt:   time.Now().UTC(),
		LastSeenAt:    time.Now().UTC(),
	}
	if err := s.UpsertEntityCandidate(ctx, c); err != nil {
		t.Fatalf("UpsertEntityCandidate (insert): %v", err)
	}
	if c.ID == 0 {
		t.Fatal("expected non-zero ID after insert")
	}
	if c.OccurrenceCount != 1 {
		t.Errorf("occurrence_count: got %d, want 1", c.OccurrenceCount)
	}
	if c.Status != "pending" {
		t.Errorf("status: got %q, want \"pending\"", c.Status)
	}

	got, err := s.GetEntityCandidate(ctx, c.ID)
	if err != nil {
		t.Fatalf("GetEntityCandidate: %v", err)
	}
	if got.Word != "Tehran" {
		t.Errorf("word: got %q, want \"Tehran\"", got.Word)
	}
}

func TestEntityCandidate_UpsertUpdatesPendingOnly(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	c := &model.EntityCandidate{
		Word:          "IRGC",
		Type:          "ORG",
		AvgConfidence: 0.8,
		Sources:       []string{"pipe-a"},
		FirstSeenAt:   time.Now().UTC(),
		LastSeenAt:    time.Now().UTC(),
	}
	if err := s.UpsertEntityCandidate(ctx, c); err != nil {
		t.Fatalf("first upsert: %v", err)
	}

	// Second upsert — should increment count and recalculate avg
	c2 := &model.EntityCandidate{
		Word:          "IRGC",
		Type:          "ORG",
		AvgConfidence: 0.6,
		Sources:       []string{"pipe-b"},
		FirstSeenAt:   time.Now().UTC(),
		LastSeenAt:    time.Now().UTC(),
	}
	if err := s.UpsertEntityCandidate(ctx, c2); err != nil {
		t.Fatalf("second upsert: %v", err)
	}
	if c2.OccurrenceCount != 2 {
		t.Errorf("occurrence_count: got %d, want 2", c2.OccurrenceCount)
	}
	// avg = (0.8*1 + 0.6) / 2 = 0.7
	if c2.AvgConfidence < 0.69 || c2.AvgConfidence > 0.71 {
		t.Errorf("avg_confidence: got %f, want ~0.7", c2.AvgConfidence)
	}
	if len(c2.Sources) != 2 {
		t.Errorf("sources len: got %d, want 2", len(c2.Sources))
	}

	// Promote the candidate, then upsert again — count should NOT change
	if err := s.UpdateEntityCandidateStatus(ctx, c2.ID, "promoted", nil, true); err != nil {
		t.Fatalf("UpdateEntityCandidateStatus: %v", err)
	}
	c3 := &model.EntityCandidate{
		Word:          "IRGC",
		Type:          "ORG",
		AvgConfidence: 0.95,
		Sources:       []string{"pipe-c"},
		FirstSeenAt:   time.Now().UTC(),
		LastSeenAt:    time.Now().UTC(),
	}
	if err := s.UpsertEntityCandidate(ctx, c3); err != nil {
		t.Fatalf("third upsert on promoted: %v", err)
	}
	// Status is promoted — should not have updated count
	got, _ := s.GetEntityCandidate(ctx, c2.ID)
	if got.OccurrenceCount != 2 {
		t.Errorf("promoted candidate count changed: got %d, want 2", got.OccurrenceCount)
	}
}

func TestEntityCandidate_ListAndCount(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	for _, word := range []string{"Tehran", "IRGC", "Khamenei"} {
		c := &model.EntityCandidate{
			Word:          word,
			Type:          "LOC",
			AvgConfidence: 0.8,
			Sources:       []string{"test"},
			FirstSeenAt:   time.Now().UTC(),
			LastSeenAt:    time.Now().UTC(),
		}
		if err := s.UpsertEntityCandidate(ctx, c); err != nil {
			t.Fatalf("upsert %s: %v", word, err)
		}
	}

	pending := "pending"
	list, err := s.ListEntityCandidates(ctx, EntityCandidateFilter{Status: &pending, Page: 1, PerPage: 10})
	if err != nil {
		t.Fatalf("ListEntityCandidates: %v", err)
	}
	if len(list) != 3 {
		t.Errorf("list len: got %d, want 3", len(list))
	}

	n, err := s.CountEntityCandidates(ctx, EntityCandidateFilter{Status: &pending})
	if err != nil {
		t.Fatalf("CountEntityCandidates: %v", err)
	}
	if n != 3 {
		t.Errorf("count: got %d, want 3", n)
	}
}

func TestEntityCandidate_Delete(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	c := &model.EntityCandidate{
		Word:          "test",
		Type:          "MISC",
		AvgConfidence: 0.5,
		Sources:       []string{"x"},
		FirstSeenAt:   time.Now().UTC(),
		LastSeenAt:    time.Now().UTC(),
	}
	if err := s.UpsertEntityCandidate(ctx, c); err != nil {
		t.Fatalf("upsert: %v", err)
	}
	if err := s.DeleteEntityCandidate(ctx, c.ID); err != nil {
		t.Fatalf("DeleteEntityCandidate: %v", err)
	}
	_, err := s.GetEntityCandidate(ctx, c.ID)
	if err == nil {
		t.Fatal("expected error after delete, got nil")
	}
}
```

- [ ] **Step 2: Run tests to confirm they fail**

```bash
go test ./internal/store/... -v -run TestEntityCandidate
```
Expected: compile error — methods not defined yet

- [ ] **Step 3: Add EntityCandidateFilter and Store interface methods to store.go**

In `internal/store/store.go`, after the `EntityTrackFilter` type, add:
```go
// EntityCandidateFilter filters entity candidate queries.
type EntityCandidateFilter struct {
	Status  *string
	Type    *string
	Page    int
	PerPage int
}
```

After the `SearchNERSuggestions` line in the Store interface, add:
```go
// EntityCandidates
UpsertEntityCandidate(ctx context.Context, c *model.EntityCandidate) error
GetEntityCandidate(ctx context.Context, id int64) (*model.EntityCandidate, error)
ListEntityCandidates(ctx context.Context, filter EntityCandidateFilter) ([]*model.EntityCandidate, error)
CountEntityCandidates(ctx context.Context, filter EntityCandidateFilter) (int, error)
UpdateEntityCandidateStatus(ctx context.Context, id int64, status string, promotedEntityID *int64, autoPromoted bool) error
DeleteEntityCandidate(ctx context.Context, id int64) error
```

- [ ] **Step 4: Implement the 6 methods in sqlite.go**

Append to `internal/store/sqlite.go` after the entity tracks section:
```go
// ── EntityCandidates ──────────────────────────────────────────────────────────

// UpsertEntityCandidate inserts a new candidate or updates an existing pending
// one. Promoted and rejected candidates are left untouched.
// After the call, c.ID, c.OccurrenceCount, c.AvgConfidence, c.Sources, and
// c.Status are set to the final stored values.
func (s *sqliteStore) UpsertEntityCandidate(ctx context.Context, c *model.EntityCandidate) error {
	now := time.Now().UTC().Format(time.RFC3339)

	// Read existing candidate.
	var existing struct {
		id      int64
		count   int
		avg     float64
		sources string
		status  string
	}
	err := s.db.QueryRowContext(ctx,
		`SELECT id, occurrence_count, avg_confidence, sources, status
		 FROM entity_candidates WHERE word = ? AND type = ?`,
		c.Word, c.Type,
	).Scan(&existing.id, &existing.count, &existing.avg, &existing.sources, &existing.status)

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

	c.ID = existing.id
	c.Status = existing.status

	// Only update pending candidates.
	if existing.status != "pending" {
		return nil
	}

	newCount := existing.count + 1
	newAvg := (existing.avg*float64(existing.count) + c.AvgConfidence) / float64(newCount)

	var sources []string
	_ = json.Unmarshal([]byte(existing.sources), &sources)
	// Append each source name that is not already present.
	for _, src := range c.Sources {
		found := false
		for _, s := range sources {
			if s == src {
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

// scanEntityCandidate scans a single entity_candidates row from rows or a row.
type entityCandidateScanner interface {
	Scan(dest ...any) error
}

func scanEntityCandidate(row entityCandidateScanner) (*model.EntityCandidate, error) {
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
```

- [ ] **Step 5: Run the tests to verify they pass**

```bash
go test ./internal/store/... -v -run TestEntityCandidate
```
Expected: all 4 TestEntityCandidate tests PASS

- [ ] **Step 6: Commit**

```bash
git add internal/store/store.go internal/store/sqlite.go internal/store/sqlite_test.go
git commit -m "feat: add EntityCandidate store methods"
```

---

## Task 3: entity_discovery pipeline

**Files:**
- Create: `internal/pipeline/entity_discovery.go`
- Create: `internal/pipeline/entity_discovery_test.go`

- [ ] **Step 1: Write failing tests**

Create `internal/pipeline/entity_discovery_test.go`:
```go
package pipeline

import (
	"context"
	"testing"

	"github.com/amir-the-h/iran-watcher/internal/inference"
	"github.com/amir-the-h/iran-watcher/internal/model"
)

// mockDiscoveryStore records calls and controls return values for tests.
type mockDiscoveryStore struct {
	upserted  []*model.EntityCandidate
	promoted  []int64
	upsertErr error
}

func (m *mockDiscoveryStore) UpsertEntityCandidate(_ context.Context, c *model.EntityCandidate) error {
	if m.upsertErr != nil {
		return m.upsertErr
	}
	c.ID = int64(len(m.upserted) + 1)
	c.Status = "pending"
	m.upserted = append(m.upserted, c)
	return nil
}

func (m *mockDiscoveryStore) UpdateEntityCandidateStatus(_ context.Context, id int64, status string, _ *int64, _ bool) error {
	m.promoted = append(m.promoted, id)
	return nil
}

func (m *mockDiscoveryStore) SaveTrackedEntity(_ context.Context, e *model.TrackedEntity) error {
	e.ID = 99
	return nil
}

func TestEntityDiscovery_NoEntitiesInContext(t *testing.T) {
	st := &mockDiscoveryStore{}
	p := newEntityDiscoveryPipeline("disc", entityDiscoveryConfig{
		MinConfidence: 0.7,
		MinFrequency:  2,
		EntityKey:     "entities",
	}, st)

	out, err := p.Run(context.Background(), inference.PipelineInput{
		Context: map[string]any{},
	})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if out["discovered"].(int) != 0 {
		t.Errorf("discovered: got %v, want 0", out["discovered"])
	}
	if len(st.upserted) != 0 {
		t.Errorf("expected no upserts, got %d", len(st.upserted))
	}
}

func TestEntityDiscovery_BelowThreshold_StaysPending(t *testing.T) {
	st := &mockDiscoveryStore{}
	p := newEntityDiscoveryPipeline("disc", entityDiscoveryConfig{
		MinConfidence:    0.7,
		MinFrequency:     3,
		AutoPromoteTypes: []string{"LOC"},
		EntityKey:        "entities",
	}, st)

	out, err := p.Run(context.Background(), inference.PipelineInput{
		Context: map[string]any{
			"entities": []map[string]any{
				{"word": "Tehran", "label": "LOC", "score": 0.8},
			},
		},
	})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if out["discovered"].(int) != 1 {
		t.Errorf("discovered: got %v, want 1", out["discovered"])
	}
	if out["auto_promoted"].(int) != 0 {
		t.Errorf("auto_promoted: got %v, want 0", out["auto_promoted"])
	}
	// OccurrenceCount is 1 after first upsert, frequency threshold is 3 → not promoted
	if len(st.promoted) != 0 {
		t.Errorf("expected no promotions, got %d", len(st.promoted))
	}
}

func TestEntityDiscovery_AboveThreshold_AutoPromotes(t *testing.T) {
	st := &mockDiscoveryStore{}
	p := newEntityDiscoveryPipeline("disc", entityDiscoveryConfig{
		MinConfidence:    0.7,
		MinFrequency:     1, // frequency = 1 so first upsert qualifies
		AutoPromoteTypes: []string{"LOC"},
		EntityKey:        "entities",
	}, st)

	out, err := p.Run(context.Background(), inference.PipelineInput{
		Context: map[string]any{
			"entities": []map[string]any{
				{"word": "Tehran", "label": "LOC", "score": 0.9},
			},
		},
	})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if out["auto_promoted"].(int) != 1 {
		t.Errorf("auto_promoted: got %v, want 1", out["auto_promoted"])
	}
	if len(st.promoted) != 1 {
		t.Errorf("expected 1 promotion call, got %d", len(st.promoted))
	}
}

func TestEntityDiscovery_TypeNotInAutoPromoteList_StaysPending(t *testing.T) {
	st := &mockDiscoveryStore{}
	p := newEntityDiscoveryPipeline("disc", entityDiscoveryConfig{
		MinConfidence:    0.5,
		MinFrequency:     1,
		AutoPromoteTypes: []string{"LOC"}, // PER not in list
		EntityKey:        "entities",
	}, st)

	out, err := p.Run(context.Background(), inference.PipelineInput{
		Context: map[string]any{
			"entities": []map[string]any{
				{"word": "Khamenei", "label": "PER", "score": 0.95},
			},
		},
	})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if out["auto_promoted"].(int) != 0 {
		t.Errorf("auto_promoted: got %v, want 0", out["auto_promoted"])
	}
}

func TestEntityDiscovery_EmptyAutoPromoteTypes_NeverAutoPromotes(t *testing.T) {
	st := &mockDiscoveryStore{}
	p := newEntityDiscoveryPipeline("disc", entityDiscoveryConfig{
		MinConfidence:    0.5,
		MinFrequency:     1,
		AutoPromoteTypes: []string{}, // empty = nothing auto-promotes
		EntityKey:        "entities",
	}, st)

	out, err := p.Run(context.Background(), inference.PipelineInput{
		Context: map[string]any{
			"entities": []map[string]any{
				{"word": "Tehran", "label": "LOC", "score": 0.99},
			},
		},
	})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if out["auto_promoted"].(int) != 0 {
		t.Errorf("auto_promoted: got %v, want 0", out["auto_promoted"])
	}
}

func TestEntityDiscovery_CustomEntityKey(t *testing.T) {
	st := &mockDiscoveryStore{}
	p := newEntityDiscoveryPipeline("disc", entityDiscoveryConfig{
		MinConfidence: 0.5,
		MinFrequency:  1,
		EntityKey:     "my_ner_output",
	}, st)

	out, err := p.Run(context.Background(), inference.PipelineInput{
		Context: map[string]any{
			"my_ner_output": []map[string]any{
				{"word": "Tehran", "label": "LOC", "score": 0.8},
			},
		},
	})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if out["discovered"].(int) != 1 {
		t.Errorf("discovered: got %v, want 1", out["discovered"])
	}
}
```

- [ ] **Step 2: Run tests to confirm they fail**

```bash
go test ./internal/pipeline/... -v -run TestEntityDiscovery
```
Expected: compile error — `newEntityDiscoveryPipeline` not defined

- [ ] **Step 3: Implement entity_discovery.go**

Create `internal/pipeline/entity_discovery.go`:
```go
package pipeline

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"

	"github.com/amir-the-h/iran-watcher/internal/inference"
	"github.com/amir-the-h/iran-watcher/internal/model"
)

// discoveryStore is the minimal store interface required by EntityDiscoveryPipeline.
type discoveryStore interface {
	UpsertEntityCandidate(ctx context.Context, c *model.EntityCandidate) error
	UpdateEntityCandidateStatus(ctx context.Context, id int64, status string, promotedEntityID *int64, autoPromoted bool) error
	SaveTrackedEntity(ctx context.Context, e *model.TrackedEntity) error
}

// entityDiscoveryConfig holds the user-configured thresholds and settings.
type entityDiscoveryConfig struct {
	MinConfidence    float64  `json:"min_confidence"`
	MinFrequency     int      `json:"min_frequency"`
	AutoPromoteTypes []string `json:"auto_promote_types"`
	EntityKey        string   `json:"entity_key"`
}

// EntityDiscoveryPipeline extracts entities from the execution context,
// upserts them as candidates, and auto-promotes those that cross thresholds.
// Outputs: {"discovered": N, "auto_promoted": M, "candidates": [...]}
type EntityDiscoveryPipeline struct {
	name   string
	config entityDiscoveryConfig
	store  discoveryStore
	logger *slog.Logger
}

// NewEntityDiscoveryFactory returns a Factory for EntityDiscoveryPipeline.
func NewEntityDiscoveryFactory(st discoveryStore) Factory {
	return func(name string, cfg map[string]any) (inference.Pipeline, error) {
		b, err := json.Marshal(cfg)
		if err != nil {
			return nil, fmt.Errorf("entity_discovery: marshal config: %w", err)
		}
		var c entityDiscoveryConfig
		if err := json.Unmarshal(b, &c); err != nil {
			return nil, fmt.Errorf("entity_discovery: parse config: %w", err)
		}
		if c.EntityKey == "" {
			c.EntityKey = "entities"
		}
		if c.MinConfidence == 0 {
			c.MinConfidence = 0.75
		}
		if c.MinFrequency == 0 {
			c.MinFrequency = 3
		}
		return newEntityDiscoveryPipeline(name, c, st), nil
	}
}

// newEntityDiscoveryPipeline creates a pipeline instance — used by factory and tests.
func newEntityDiscoveryPipeline(name string, cfg entityDiscoveryConfig, st discoveryStore) *EntityDiscoveryPipeline {
	return &EntityDiscoveryPipeline{name: name, config: cfg, store: st, logger: slog.Default()}
}

func (p *EntityDiscoveryPipeline) Name() string                 { return p.name }
func (p *EntityDiscoveryPipeline) Type() inference.PipelineType { return inference.PipelineTypeInternal }
func (p *EntityDiscoveryPipeline) Close() error                 { return nil }

func (p *EntityDiscoveryPipeline) Run(ctx context.Context, input inference.PipelineInput) (map[string]any, error) {
	nerEntities := p.extractEntities(input)
	if len(nerEntities) == 0 {
		return map[string]any{
			"discovered":    0,
			"auto_promoted": 0,
			"candidates":    []any{},
		}, nil
	}

	type result struct {
		Word   string `json:"word"`
		Type   string `json:"type"`
		Status string `json:"status"`
	}
	var results []result
	autoPromotedCount := 0

	for _, ne := range nerEntities {
		c := &model.EntityCandidate{
			Word:          ne.Word,
			Type:          ne.Label,
			AvgConfidence: float64(ne.Score),
			Sources:       []string{p.name},
		}

		if err := p.store.UpsertEntityCandidate(ctx, c); err != nil {
			p.logger.Warn("entity_discovery: upsert candidate", "word", ne.Word, "error", err)
			continue
		}

		if c.Status == "pending" && p.shouldAutoPromote(c) {
			if err := p.autoPromote(ctx, c); err != nil {
				p.logger.Warn("entity_discovery: auto-promote", "word", c.Word, "error", err)
			} else {
				autoPromotedCount++
			}
		}

		results = append(results, result{Word: c.Word, Type: c.Type, Status: c.Status})
	}

	return map[string]any{
		"discovered":    len(results),
		"auto_promoted": autoPromotedCount,
		"candidates":    results,
	}, nil
}

// extractEntities reads and parses the entities array from the pipeline context.
// Accepts both []model.NEREntity and []map[string]any shapes.
func (p *EntityDiscoveryPipeline) extractEntities(input inference.PipelineInput) []model.NEREntity {
	raw, ok := input.Context[p.config.EntityKey]
	if !ok {
		return nil
	}
	b, err := json.Marshal(raw)
	if err != nil {
		return nil
	}
	var entities []model.NEREntity
	if err := json.Unmarshal(b, &entities); err != nil {
		return nil
	}
	return entities
}

// shouldAutoPromote returns true when the candidate meets all configured thresholds.
// An empty AutoPromoteTypes list means nothing auto-promotes.
func (p *EntityDiscoveryPipeline) shouldAutoPromote(c *model.EntityCandidate) bool {
	if len(p.config.AutoPromoteTypes) == 0 {
		return false
	}
	if c.AvgConfidence < p.config.MinConfidence {
		return false
	}
	if c.OccurrenceCount < p.config.MinFrequency {
		return false
	}
	for _, t := range p.config.AutoPromoteTypes {
		if t == c.Type {
			return true
		}
	}
	return false
}

// autoPromote creates a TrackedEntity and marks the candidate as promoted.
// On UNIQUE name conflict, the candidate is left as pending (user resolves via UI).
func (p *EntityDiscoveryPipeline) autoPromote(ctx context.Context, c *model.EntityCandidate) error {
	e := &model.TrackedEntity{
		Name:    c.Word,
		Type:    c.Type,
		Aliases: []string{},
		Active:  true,
	}
	if err := p.store.SaveTrackedEntity(ctx, e); err != nil {
		return fmt.Errorf("entity_discovery: save tracked entity: %w", err)
	}
	if err := p.store.UpdateEntityCandidateStatus(ctx, c.ID, "promoted", &e.ID, true); err != nil {
		return fmt.Errorf("entity_discovery: update candidate status: %w", err)
	}
	c.Status = "promoted"
	c.PromotedEntityID = &e.ID
	return nil
}
```

- [ ] **Step 4: Run tests to verify they pass**

```bash
go test ./internal/pipeline/... -v -run TestEntityDiscovery
```
Expected: all 6 tests PASS

- [ ] **Step 5: Commit**

```bash
git add internal/pipeline/entity_discovery.go internal/pipeline/entity_discovery_test.go
git commit -m "feat: add entity_discovery pipeline"
```

---

## Task 4: Register entity_discovery in serve.go

**Files:**
- Modify: `cli/cmd/serve.go`

- [ ] **Step 1: Add registration line**

In `cli/cmd/serve.go`, after the `entity_matcher` registration line (line ~133):
```go
pipeReg.Register("entity_matcher", pipeline.NewEntityMatcherFactory(s))
```

Add immediately after:
```go
pipeReg.Register("entity_discovery", pipeline.NewEntityDiscoveryFactory(s))
```

- [ ] **Step 2: Build the Docker image to verify**

```bash
docker compose build --build-arg BUILDKIT_INLINE_CACHE=1 iran-watcher
```
Expected: build succeeds

- [ ] **Step 3: Commit**

```bash
git add cli/cmd/serve.go
git commit -m "feat: register entity_discovery pipeline"
```

---

## Task 5: API handlers for entity candidates

**Files:**
- Create: `internal/api/handlers_entity_candidates.go`

- [ ] **Step 1: Create the handler file**

Create `internal/api/handlers_entity_candidates.go`:
```go
package api

import (
	"encoding/json"
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"

	"github.com/amir-the-h/iran-watcher/internal/model"
	"github.com/amir-the-h/iran-watcher/internal/store"
)

// handleListEntityCandidates GET /api/entity-candidates
func (s *Server) handleListEntityCandidates(w http.ResponseWriter, r *http.Request) {
	filter := store.EntityCandidateFilter{Page: 1, PerPage: 50}
	q := r.URL.Query()

	if v := q.Get("status"); v != "" {
		filter.Status = &v
	}
	if v := q.Get("type"); v != "" {
		filter.Type = &v
	}
	if v := q.Get("page"); v != "" {
		if p, err := strconv.Atoi(v); err == nil && p > 0 {
			filter.Page = p
		}
	}
	if v := q.Get("per_page"); v != "" {
		if p, err := strconv.Atoi(v); err == nil && p > 0 {
			filter.PerPage = p
		}
	}

	candidates, err := s.store.ListEntityCandidates(r.Context(), filter)
	if err != nil {
		s.logger.Error("api: list entity candidates", "error", err)
		writeError(w, http.StatusInternalServerError, "failed to list entity candidates")
		return
	}

	total, err := s.store.CountEntityCandidates(r.Context(), store.EntityCandidateFilter{
		Status: filter.Status,
		Type:   filter.Type,
	})
	if err != nil {
		s.logger.Error("api: count entity candidates", "error", err)
		writeError(w, http.StatusInternalServerError, "failed to count entity candidates")
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"candidates": candidates,
		"total":      total,
		"page":       filter.Page,
		"per_page":   filter.PerPage,
	})
}

// handleGetEntityCandidate GET /api/entity-candidates/{id}
func (s *Server) handleGetEntityCandidate(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid id")
		return
	}
	c, err := s.store.GetEntityCandidate(r.Context(), id)
	if err != nil {
		s.logger.Error("api: get entity candidate", "id", id, "error", err)
		writeError(w, http.StatusNotFound, "entity candidate not found")
		return
	}
	writeJSON(w, http.StatusOK, c)
}

// handlePromoteEntityCandidate POST /api/entity-candidates/{id}/promote
func (s *Server) handlePromoteEntityCandidate(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid id")
		return
	}
	c, err := s.store.GetEntityCandidate(r.Context(), id)
	if err != nil {
		writeError(w, http.StatusNotFound, "entity candidate not found")
		return
	}
	if c.Status != "pending" {
		writeError(w, http.StatusBadRequest, "candidate is not pending")
		return
	}

	e := &model.TrackedEntity{
		Name:    c.Word,
		Type:    c.Type,
		Aliases: []string{},
		Active:  true,
	}
	if err := s.store.SaveTrackedEntity(r.Context(), e); err != nil {
		s.logger.Error("api: promote candidate: save entity", "id", id, "error", err)
		writeError(w, http.StatusInternalServerError, "failed to create tracked entity (name may already exist)")
		return
	}
	if err := s.store.UpdateEntityCandidateStatus(r.Context(), id, "promoted", &e.ID, false); err != nil {
		s.logger.Error("api: promote candidate: update status", "id", id, "error", err)
		writeError(w, http.StatusInternalServerError, "failed to update candidate status")
		return
	}
	writeJSON(w, http.StatusOK, e)
}

// handleRejectEntityCandidate POST /api/entity-candidates/{id}/reject
func (s *Server) handleRejectEntityCandidate(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid id")
		return
	}
	if err := s.store.UpdateEntityCandidateStatus(r.Context(), id, "rejected", nil, false); err != nil {
		s.logger.Error("api: reject candidate", "id", id, "error", err)
		writeError(w, http.StatusInternalServerError, "failed to reject candidate")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// handleMergeEntityCandidate POST /api/entity-candidates/{id}/merge
// Body: {"entity_id": N}
func (s *Server) handleMergeEntityCandidate(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid id")
		return
	}
	var body struct {
		EntityID int64 `json:"entity_id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON")
		return
	}
	if body.EntityID == 0 {
		writeError(w, http.StatusBadRequest, "entity_id is required")
		return
	}

	candidate, err := s.store.GetEntityCandidate(r.Context(), id)
	if err != nil {
		writeError(w, http.StatusNotFound, "entity candidate not found")
		return
	}

	target, err := s.store.GetTrackedEntity(r.Context(), body.EntityID)
	if err != nil {
		writeError(w, http.StatusNotFound, "target tracked entity not found")
		return
	}

	// Append candidate word as alias if not already present.
	alreadyAlias := false
	for _, a := range target.Aliases {
		if a == candidate.Word {
			alreadyAlias = true
			break
		}
	}
	if !alreadyAlias {
		target.Aliases = append(target.Aliases, candidate.Word)
		if err := s.store.UpdateTrackedEntity(r.Context(), target); err != nil {
			s.logger.Error("api: merge candidate: update entity", "entity_id", body.EntityID, "error", err)
			writeError(w, http.StatusInternalServerError, "failed to update entity aliases")
			return
		}
	}

	if err := s.store.UpdateEntityCandidateStatus(r.Context(), id, "promoted", &body.EntityID, false); err != nil {
		s.logger.Error("api: merge candidate: update status", "id", id, "error", err)
		writeError(w, http.StatusInternalServerError, "failed to update candidate status")
		return
	}

	writeJSON(w, http.StatusOK, target)
}

// handleDeleteEntityCandidate DELETE /api/entity-candidates/{id}
func (s *Server) handleDeleteEntityCandidate(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid id")
		return
	}
	if err := s.store.DeleteEntityCandidate(r.Context(), id); err != nil {
		s.logger.Error("api: delete entity candidate", "id", id, "error", err)
		writeError(w, http.StatusInternalServerError, "failed to delete entity candidate")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
```

- [ ] **Step 2: Compile check**

```bash
go build ./internal/api/...
```
Expected: no errors

- [ ] **Step 3: Commit**

```bash
git add internal/api/handlers_entity_candidates.go
git commit -m "feat: add entity candidate API handlers"
```

---

## Task 6: Register candidate routes in server.go

**Files:**
- Modify: `internal/api/server.go`

- [ ] **Step 1: Add routes**

In `internal/api/server.go`, inside the admin-only route group after the tracked entities mutations block (after the `handleDeactivateTrackedEntity` line), add:

```go
// Entity Candidates
r.Get("/entity-candidates", srv.handleListEntityCandidates)
r.Get("/entity-candidates/{id}", srv.handleGetEntityCandidate)
r.Post("/entity-candidates/{id}/promote", srv.handlePromoteEntityCandidate)
r.Post("/entity-candidates/{id}/reject", srv.handleRejectEntityCandidate)
r.Post("/entity-candidates/{id}/merge", srv.handleMergeEntityCandidate)
r.Delete("/entity-candidates/{id}", srv.handleDeleteEntityCandidate)
```

- [ ] **Step 2: Build Docker image to verify**

```bash
docker compose build --build-arg BUILDKIT_INLINE_CACHE=1 iran-watcher
```
Expected: build succeeds

- [ ] **Step 3: Run all tests**

```bash
go test ./...
```
Expected: all tests PASS

- [ ] **Step 4: Commit**

```bash
git add internal/api/server.go
git commit -m "feat: register entity candidate routes"
```

---

## Task 7: Frontend — API client + Pinia store

**Files:**
- Modify: `web/src/api/entities.ts`
- Create: `web/src/stores/candidates.ts`

- [ ] **Step 1: Add types and API functions to entities.ts**

Append to `web/src/api/entities.ts`:
```ts
export interface EntityCandidate {
  id: number
  word: string
  type: string
  occurrence_count: number
  avg_confidence: number
  sources: string[]
  status: 'pending' | 'promoted' | 'rejected'
  promoted_entity_id?: number
  auto_promoted: boolean
  first_seen_at: string
  last_seen_at: string
}

export interface EntityCandidatesResponse {
  candidates: EntityCandidate[]
  total: number
  page: number
  per_page: number
}

export interface EntityCandidateFilter {
  status?: string
  type?: string
}

export const listEntityCandidates = (filter: EntityCandidateFilter = {}, page = 1, perPage = 50) => {
  const params = new URLSearchParams({ page: String(page), per_page: String(perPage) })
  if (filter.status) params.set('status', filter.status)
  if (filter.type)   params.set('type',   filter.type)
  return apiFetch<EntityCandidatesResponse>(`/api/entity-candidates?${params}`)
}

export const promoteEntityCandidate = (id: number) =>
  apiFetch<TrackedEntity>(`/api/entity-candidates/${id}/promote`, { method: 'POST' })

export const rejectEntityCandidate = (id: number) =>
  apiFetch<void>(`/api/entity-candidates/${id}/reject`, { method: 'POST' })

export const mergeEntityCandidate = (id: number, entityId: number) =>
  apiFetch<TrackedEntity>(`/api/entity-candidates/${id}/merge`, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ entity_id: entityId }),
  })

export const deleteEntityCandidate = (id: number) =>
  apiFetch<void>(`/api/entity-candidates/${id}`, { method: 'DELETE' })
```

- [ ] **Step 2: Create the Pinia store for candidates**

Create `web/src/stores/candidates.ts`:
```ts
import { ref } from 'vue'
import { defineStore } from 'pinia'
import {
  listEntityCandidates,
  promoteEntityCandidate,
  rejectEntityCandidate,
  mergeEntityCandidate,
  deleteEntityCandidate,
  type EntityCandidate,
  type EntityCandidatesResponse,
  type EntityCandidateFilter,
} from '../api/entities'

export const useCandidatesStore = defineStore('candidates', () => {
  const response  = ref<EntityCandidatesResponse | null>(null)
  const loading   = ref(false)
  const pendingCount = ref(0)

  async function fetchCandidates(filter: EntityCandidateFilter = {}, page = 1, perPage = 50) {
    loading.value = true
    try {
      response.value = await listEntityCandidates(filter, page, perPage)
    } finally {
      loading.value = false
    }
  }

  async function fetchPendingCount() {
    try {
      const r = await listEntityCandidates({ status: 'pending' }, 1, 1)
      pendingCount.value = r.total
    } catch {}
  }

  async function promote(id: number) {
    const entity = await promoteEntityCandidate(id)
    removeFromList(id)
    pendingCount.value = Math.max(0, pendingCount.value - 1)
    return entity
  }

  async function reject(id: number) {
    await rejectEntityCandidate(id)
    removeFromList(id)
    pendingCount.value = Math.max(0, pendingCount.value - 1)
  }

  async function merge(id: number, entityId: number) {
    const entity = await mergeEntityCandidate(id, entityId)
    removeFromList(id)
    pendingCount.value = Math.max(0, pendingCount.value - 1)
    return entity
  }

  async function remove(id: number) {
    await deleteEntityCandidate(id)
    removeFromList(id)
  }

  function removeFromList(id: number) {
    if (response.value) {
      response.value.candidates = response.value.candidates.filter(c => c.id !== id)
      response.value.total = Math.max(0, response.value.total - 1)
    }
  }

  return {
    response, loading, pendingCount,
    fetchCandidates, fetchPendingCount, promote, reject, merge, remove,
  }
})
```

- [ ] **Step 3: Commit**

```bash
git add web/src/api/entities.ts web/src/stores/candidates.ts
git commit -m "feat: add EntityCandidate API client and Pinia store"
```

---

## Task 8: TrackerView — rename + Candidates tab

**Files:**
- Create: `web/src/views/TrackerView.vue` (replaces TrackedEntitiesView.vue)
- The old `TrackedEntitiesView.vue` is replaced; keep all existing entities content, add Candidates tab.

- [ ] **Step 1: Copy existing view and rename**

```bash
cp /mnt/shared/projects/personal/iran-watcher/web/src/views/TrackedEntitiesView.vue \
   /mnt/shared/projects/personal/iran-watcher/web/src/views/TrackerView.vue
rm /mnt/shared/projects/personal/iran-watcher/web/src/views/TrackedEntitiesView.vue
```

- [ ] **Step 2: Update TrackerView.vue — add imports and candidates tab state**

Change the `vue` import line from:
```ts
import { ref, onMounted, computed, watch } from 'vue'
```
To:
```ts
import { ref, onMounted, computed, watch, h } from 'vue'
```

Change the `naive-ui` import line from:
```ts
import { useMessage } from 'naive-ui'
```
To:
```ts
import { useMessage, NButton, NSpace, NBadge } from 'naive-ui'
```

After the existing `../api/entities` import line, add:
```ts
import { listTrackedEntities, type EntityCandidate } from '../api/entities'
import { useCandidatesStore } from '../stores/candidates'
```

After the existing `const store = useEntitiesStore()` line, add:
```ts
const candidatesStore = useCandidatesStore()
const activeTab = ref<'entities' | 'candidates'>('entities')

// Candidates tab state
const candFilterStatus = ref<string>('pending')
const candFilterType   = ref<string | null>(null)
const candPage         = ref(1)
const candPerPage      = ref(50)
const showMergeModal   = ref(false)
const mergeCandidate   = ref<import('../api/entities').EntityCandidate | null>(null)
const mergeTargetId    = ref<number | null>(null)
const allEntities      = ref<TE[]>([])
const mergeEntityOptions = computed(() =>
  allEntities.value.map(e => ({ label: `${e.name} (${e.type})`, value: e.id }))
)

async function loadCandidates() {
  await candidatesStore.fetchCandidates(
    { status: candFilterStatus.value || undefined, type: candFilterType.value || undefined },
    candPage.value,
    candPerPage.value,
  )
}

async function onTabChange(tab: string) {
  if (tab === 'candidates') {
    await loadCandidates()
    await candidatesStore.fetchPendingCount()
  }
}

async function onPromote(c: EntityCandidate) {
  try {
    await candidatesStore.promote(c.id)
    message.success(`"${c.word}" promoted to Tracker`)
    store.fetchEntities()
    candidatesStore.fetchPendingCount()
  } catch (e: any) {
    message.error('Promote failed: ' + e.message)
  }
}

async function onReject(c: EntityCandidate) {
  try {
    await candidatesStore.reject(c.id)
    message.success(`"${c.word}" rejected`)
    candidatesStore.fetchPendingCount()
  } catch (e: any) {
    message.error('Reject failed: ' + e.message)
  }
}

async function openMerge(c: EntityCandidate) {
  mergeCandidate.value = c
  mergeTargetId.value  = null
  allEntities.value    = await listTrackedEntities()
  showMergeModal.value = true
}

async function onMergeConfirm() {
  if (!mergeCandidate.value || !mergeTargetId.value) return
  try {
    await candidatesStore.merge(mergeCandidate.value.id, mergeTargetId.value)
    message.success(`"${mergeCandidate.value.word}" merged as alias`)
    showMergeModal.value = false
    store.fetchEntities()
    candidatesStore.fetchPendingCount()
  } catch (e: any) {
    message.error('Merge failed: ' + e.message)
  }
}
```

Replace the existing `onMounted` call:
```ts
// Before (remove this):
onMounted(() => store.fetchEntities())

// After (replace with this):
onMounted(() => {
  store.fetchEntities()
  candidatesStore.fetchPendingCount()
})
```

- [ ] **Step 3: Update the template — wrap in n-tabs, add Candidates pane**

Replace the top-level `<div>` opening and `<h2>` in the template `<template>` section with:
```html
<n-tabs v-model:value="activeTab" type="line" animated @update:value="onTabChange">
  <n-tab-pane name="entities" tab="Entities">
```

Then wrap the entire existing entities content inside this pane and close it before adding the Candidates pane:
```html
  </n-tab-pane>

  <n-tab-pane name="candidates">
    <template #tab>
      Candidates<n-badge v-if="candidatesStore.pendingCount > 0" :value="candidatesStore.pendingCount" style="margin-left:6px" />
    </template>

    <!-- Filters -->
    <n-space align="center" style="margin-bottom:12px">
      <n-select
        v-model:value="candFilterStatus"
        :options="[
          { label: 'Pending',  value: 'pending'  },
          { label: 'Promoted', value: 'promoted' },
          { label: 'Rejected', value: 'rejected' },
          { label: 'All',      value: ''         },
        ]"
        style="width:130px"
        @update:value="() => { candPage = 1; loadCandidates() }"
      />
      <n-select
        v-model:value="candFilterType"
        clearable
        placeholder="Type"
        :options="[
          { label: 'LOC',  value: 'LOC'  },
          { label: 'PER',  value: 'PER'  },
          { label: 'ORG',  value: 'ORG'  },
          { label: 'MISC', value: 'MISC' },
        ]"
        style="width:120px"
        @update:value="() => { candPage = 1; loadCandidates() }"
      />
    </n-space>

    <!-- Candidates table -->
    <n-data-table
      :loading="candidatesStore.loading"
      :data="candidatesStore.response?.candidates ?? []"
      :columns="candidateColumns"
      :row-key="(row: any) => row.id"
    />

    <!-- Pagination -->
    <n-pagination
      v-if="(candidatesStore.response?.total ?? 0) > candPerPage"
      v-model:page="candPage"
      :page-size="candPerPage"
      :item-count="candidatesStore.response?.total ?? 0"
      style="margin-top:12px; justify-content:flex-end"
      @update:page="loadCandidates"
    />
  </n-tab-pane>
</n-tabs>
```

- [ ] **Step 4: Add candidateColumns definition in script**

In the `<script setup>` section, add anywhere after the `candidatesStore` declaration:
```ts
const candidateColumns = [
  { title: 'Word',        key: 'word' },
  { title: 'Type',        key: 'type' },
  { title: 'Count',       key: 'occurrence_count', width: 80 },
  {
    title: 'Confidence',
    key: 'avg_confidence',
    width: 110,
    render: (row: any) => (row.avg_confidence * 100).toFixed(1) + '%',
  },
  {
    title: 'First Seen',
    key: 'first_seen_at',
    render: (row: any) => formatDateTime(row.first_seen_at),
  },
  {
    title: 'Last Seen',
    key: 'last_seen_at',
    render: (row: any) => formatDateTime(row.last_seen_at),
  },
  { title: 'Status', key: 'status' },
  {
    title: 'Actions',
    key: 'actions',
    render(row: any) {
      if (row.status !== 'pending') return null
      return h(NSpace, {}, {
        default: () => [
          h(NButton, { size: 'small', type: 'primary', onClick: () => onPromote(row) }, { default: () => 'Promote' }),
          h(NButton, { size: 'small', onClick: () => openMerge(row) }, { default: () => 'Merge' }),
          h(NButton, { size: 'small', type: 'error', onClick: () => onReject(row) }, { default: () => 'Reject' }),
        ],
      })
    },
  },
]
```

- [ ] **Step 5: Add merge modal to template**

Before the closing `</template>` tag, add:
```html
<!-- Merge modal -->
<n-modal v-model:show="showMergeModal" preset="dialog" title="Merge as Alias" positive-text="Confirm" negative-text="Cancel" @positive-click="onMergeConfirm">
  <p style="margin-bottom:8px">Merge <strong>{{ mergeCandidate?.word }}</strong> as an alias of:</p>
  <n-select
    v-model:value="mergeTargetId"
    :options="mergeEntityOptions"
    filterable
    placeholder="Search tracked entities..."
  />
</n-modal>
```

- [ ] **Step 6: Update page title in template**

Change the `<h2>` or page heading text from "Tracked Entities" to "Tracker".

- [ ] **Step 7: Commit**

```bash
git add web/src/views/TrackerView.vue
git rm web/src/views/TrackedEntitiesView.vue 2>/dev/null || true
git commit -m "feat: TrackerView with Candidates tab (rename from TrackedEntitiesView)"
```

---

## Task 9: PipelinesView — entity_discovery config form

**Files:**
- Modify: `web/src/views/PipelinesView.vue`

- [ ] **Step 1: Add form state variables for entity_discovery config**

In `PipelinesView.vue`, after the `formHandler` ref declaration, add:
```ts
// entity_discovery-specific config
const formDiscoveryMinConfidence   = ref(0.75)
const formDiscoveryMinFrequency    = ref(3)
const formDiscoveryAutoPromoteTypes = ref<string[]>([])
const formDiscoveryEntityKey       = ref('entities')
```

- [ ] **Step 2: Reset them in openModal**

In the `openModal` / reset function (where `formHandler.value = null` is set), add:
```ts
formDiscoveryMinConfidence.value    = 0.75
formDiscoveryMinFrequency.value     = 3
formDiscoveryAutoPromoteTypes.value = []
formDiscoveryEntityKey.value        = 'entities'
```

- [ ] **Step 3: Load them in openEdit**

After `formHandler.value = row.config?.handler ?? null`, add:
```ts
formDiscoveryMinConfidence.value    = row.config?.min_confidence    ?? 0.75
formDiscoveryMinFrequency.value     = row.config?.min_frequency     ?? 3
formDiscoveryAutoPromoteTypes.value = Array.isArray(row.config?.auto_promote_types) ? [...row.config.auto_promote_types] : []
formDiscoveryEntityKey.value        = row.config?.entity_key        ?? 'entities'
```

- [ ] **Step 4: Save them in onSave**

In the `onSave` function, after `config.handler = formHandler.value`, add:
```ts
if (formHandler.value === 'entity_discovery') {
  config.min_confidence     = formDiscoveryMinConfidence.value
  config.min_frequency      = formDiscoveryMinFrequency.value
  config.auto_promote_types = formDiscoveryAutoPromoteTypes.value
  config.entity_key         = formDiscoveryEntityKey.value
}
```

- [ ] **Step 5: Add template block for entity_discovery config**

After the handler dropdown `</n-form-item>` block, add:
```html
<!-- entity_discovery config fields -->
<template v-if="formType === 'internal' && formHandler === 'entity_discovery'">
  <n-form-item label="Min Confidence (0–1)">
    <n-input-number
      v-model:value="formDiscoveryMinConfidence"
      :min="0" :max="1" :step="0.05"
      placeholder="0.75"
      style="width:160px"
    />
  </n-form-item>
  <n-form-item label="Min Frequency">
    <n-input-number
      v-model:value="formDiscoveryMinFrequency"
      :min="1" :step="1"
      placeholder="3"
      style="width:120px"
    />
  </n-form-item>
  <n-form-item label="Auto-Promote Types">
    <n-select
      v-model:value="formDiscoveryAutoPromoteTypes"
      multiple
      clearable
      :options="[
        { label: 'LOC — Location',     value: 'LOC'  },
        { label: 'PER — Person',       value: 'PER'  },
        { label: 'ORG — Organization', value: 'ORG'  },
        { label: 'MISC — Misc',        value: 'MISC' },
      ]"
      placeholder="None (all queue for review)"
    />
  </n-form-item>
  <n-form-item label="Entity Key">
    <n-input v-model:value="formDiscoveryEntityKey" placeholder="entities" />
  </n-form-item>
</template>
```

- [ ] **Step 6: Commit**

```bash
git add web/src/views/PipelinesView.vue
git commit -m "feat: add entity_discovery config fields to PipelinesView"
```

---

## Task 10: Router + App.vue rename + CLAUDE.md + build verification

**Files:**
- Modify: `web/src/router/index.ts`
- Modify: `web/src/App.vue`
- Modify: `CLAUDE.md`

- [ ] **Step 1: Update router**

In `web/src/router/index.ts`:

Change:
```ts
const VIEWER_ROUTES = new Set(['/map', '/tweets', '/tracked-entities'])
```
To:
```ts
const VIEWER_ROUTES = new Set(['/map', '/tweets', '/tracker'])
```

Change:
```ts
{ path: '/tracked-entities', component: () => import('../views/TrackedEntitiesView.vue') },
```
To:
```ts
{ path: '/tracked-entities', redirect: '/tracker' },
{ path: '/tracker', component: () => import('../views/TrackerView.vue') },
```

- [ ] **Step 2: Update App.vue nav menu**

In `web/src/App.vue`, change both occurrences of:
```ts
{ label: 'Tracked Entities', key: '/tracked-entities' },
```
To:
```ts
{ label: 'Tracker', key: '/tracker' },
```

- [ ] **Step 3: Update CLAUDE.md**

In `CLAUDE.md`:

1. In **Project Structure**, update the `internal/pipeline/` entry to mention `entity_discovery`:
   - Change: `→ Internal pipeline registry + entity_matcher, twitter, notification pipelines`
   - To: `→ Internal pipeline registry + entity_matcher, entity_discovery, twitter, notification pipelines`

2. In **Key Design Decisions**, add a new bullet after the `entity_matcher` / `entity_tracks` bullet:
   ```
   - **Entity discovery** — `entity_discovery` is a configurable internal pipeline registered in the pipeline registry; it reads a normalized `entities` array (from ONNX NER or Ollama output) from the execution context key configured in `entity_key` (default `"entities"`), upserts each `(word, type)` pair into `entity_candidates` table with rolling avg confidence and occurrence count, and auto-promotes candidates that exceed both `min_confidence` and `min_frequency` thresholds for types listed in `auto_promote_types` (empty = no auto-promotion); candidate management API: `GET /api/entity-candidates`, `POST /api/entity-candidates/{id}/promote`, `POST /api/entity-candidates/{id}/reject`, `POST /api/entity-candidates/{id}/merge`, `DELETE /api/entity-candidates/{id}`; UI: "Candidates" tab on the Tracker page (`/tracker`) with badge count showing pending candidates; the `/tracked-entities` route redirects to `/tracker`
   ```

- [ ] **Step 4: Final Docker build**

```bash
docker compose build --build-arg BUILDKIT_INLINE_CACHE=1 iran-watcher
```
Expected: build succeeds

- [ ] **Step 5: Run all backend tests**

```bash
go test ./...
```
Expected: all tests PASS

- [ ] **Step 6: Commit**

```bash
git add web/src/router/index.ts web/src/App.vue CLAUDE.md
git commit -m "feat: rename Tracked Entities to Tracker, add /tracker route"
```
