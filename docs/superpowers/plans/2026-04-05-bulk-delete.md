# Bulk Delete Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add collection-level `DELETE /api/{resource}` endpoints to 12 resources, each accepting an optional JSON body with `ids` + filter fields, and expose all 12 as MCP tools.

**Architecture:** Store layer gets `BulkDelete*Filter` structs + `BulkDelete*` functions (dynamic SQL WHERE builder). API layer gets one new handler per resource, registered alongside the existing single-delete routes. MCP layer gets 12 new `ToolDef` entries in `internal/mcpserver/tools.go`.

**Tech Stack:** Go, SQLite (`modernc.org/sqlite`), `net/http` + chi router, table-driven MCP tools.

---

## File Map

| File | Changes |
|------|---------|
| `internal/store/store.go` | +12 filter structs, +12 interface method signatures |
| `internal/store/sqlite.go` | +6 BulkDelete functions (rules, sources, tracked-entities, entity-candidates, search jobs, digest jobs) |
| `internal/store/sqlite_actions.go` | +1 BulkDeleteActions |
| `internal/store/sqlite_pipelines.go` | +2 BulkDeletePipelines, BulkDeleteTriggerPipelineLinks; add `"strings"` import |
| `internal/store/sqlite_notification_providers.go` | +2 BulkDeleteNotificationProviders, BulkDeleteNotificationChannels; add `"strings"` import |
| `internal/store/sqlite_ollama.go` | +1 BulkDeleteOllamaBackends; add `"strings"` import |
| `internal/store/sqlite_test.go` | +bulk delete tests for sqlite.go resources |
| `internal/store/sqlite_actions_test.go` | +bulk delete tests for actions |
| `internal/store/sqlite_pipelines_test.go` | NEW — bulk delete tests for pipelines + trigger links |
| `internal/store/sqlite_notification_test.go` | +bulk delete tests for providers + channels |
| `internal/store/sqlite_ollama_test.go` | NEW — bulk delete tests for ollama backends |
| `internal/api/handlers_entity_candidates.go` | +handleBulkDeleteEntityCandidates |
| `internal/api/handlers_rules.go` | +handleBulkDeleteRules |
| `internal/api/handlers_sources.go` | +handleBulkDeleteSources |
| `internal/api/handlers_entities.go` | +handleBulkDeleteTrackedEntities |
| `internal/api/handlers_search.go` | +handleBulkDeleteSearchJobs |
| `internal/api/handlers_summaries.go` | +handleBulkDeleteDigestJobs |
| `internal/api/handlers_actions.go` | +handleBulkDeleteActions |
| `internal/api/handlers_pipelines.go` | +handleBulkDeletePipelines |
| `internal/api/handlers_triggers.go` | +handleBulkDeleteTriggerPipelineLinks |
| `internal/api/handlers_notifications.go` | +handleBulkDeleteNotificationProviders, +handleBulkDeleteNotificationChannels |
| `internal/api/handlers_ollama.go` | +handleBulkDeleteOllamaBackends |
| `internal/api/server.go` | +12 `r.Delete(...)` route registrations |
| `internal/mcpserver/tools.go` | +12 ToolDef entries |

---

### Task 1: Store filter structs + interface methods

**Files:**
- Modify: `internal/store/store.go`

- [ ] **Step 1: Add all 12 filter structs and 12 interface methods to store.go**

Add the structs immediately after the existing filter structs (after `ExecutionFilter`, around line 108), and add the interface methods into the `Store` interface in the appropriate sections.

**Structs to add:**
```go
// BulkDeleteRulesFilter defines filters for bulk-deleting rules.
type BulkDeleteRulesFilter struct {
	IDs    []int64
	Active *bool
	Local  *bool
}

// BulkDeleteSourcesFilter defines filters for bulk-deleting sources.
type BulkDeleteSourcesFilter struct {
	IDs    []int64
	Active *bool
	Type   string
}

// BulkDeletePipelinesFilter defines filters for bulk-deleting pipelines.
type BulkDeletePipelinesFilter struct {
	IDs    []int64
	Active *bool
}

// BulkDeleteTriggerPipelineLinksFilter defines filters for bulk-deleting trigger-pipeline links.
type BulkDeleteTriggerPipelineLinksFilter struct {
	IDs         []int64
	PipelineID  *int64
	TriggerType string
}

// BulkDeleteTrackedEntitiesFilter defines filters for bulk-deleting tracked entities.
type BulkDeleteTrackedEntitiesFilter struct {
	IDs    []int64
	Active *bool
	Type   string
}

// BulkDeleteEntityCandidatesFilter defines filters for bulk-deleting entity candidates.
type BulkDeleteEntityCandidatesFilter struct {
	IDs            []int64
	Status         string
	Type           string
	AutoPromoted   *bool
	MinConfidence  *float64
	MaxConfidence  *float64
	MinOccurrences *int
	MaxOccurrences *int
}

// BulkDeleteActionsFilter defines filters for bulk-deleting actions.
type BulkDeleteActionsFilter struct {
	IDs    []int64
	Active *bool
	Type   string
}

// BulkDeleteOllamaBackendsFilter defines filters for bulk-deleting Ollama backends.
type BulkDeleteOllamaBackendsFilter struct {
	IDs     []int64
	Enabled *bool
}

// BulkDeleteNotificationProvidersFilter defines filters for bulk-deleting notification providers.
type BulkDeleteNotificationProvidersFilter struct {
	IDs    []int64
	Active *bool
	Type   string
}

// BulkDeleteNotificationChannelsFilter defines filters for bulk-deleting notification channels.
type BulkDeleteNotificationChannelsFilter struct {
	IDs        []int64
	Active     *bool
	ProviderID *int64
}

// BulkDeleteSearchJobsFilter defines filters for bulk-deleting search jobs.
type BulkDeleteSearchJobsFilter struct {
	IDs    []string
	Status string
}

// BulkDeleteDigestJobsFilter defines filters for bulk-deleting digest jobs.
type BulkDeleteDigestJobsFilter struct {
	IDs    []string
	Status string
}
```

**Interface methods to add** (add each in the relevant section inside the `Store` interface):

In the `// Sources` section, after `DeleteSource`:
```go
BulkDeleteSources(ctx context.Context, f BulkDeleteSourcesFilter) (int64, error)
```

In the `// Rules` section, after `DeleteRule`:
```go
BulkDeleteRules(ctx context.Context, f BulkDeleteRulesFilter) (int64, error)
```

In the `// TrackedEntities` section, after `DeleteTrackedEntity`:
```go
BulkDeleteTrackedEntities(ctx context.Context, f BulkDeleteTrackedEntitiesFilter) (int64, error)
```

In the `// EntityCandidates` section, after `DeleteEntityCandidate`:
```go
BulkDeleteEntityCandidates(ctx context.Context, f BulkDeleteEntityCandidatesFilter) (int64, error)
```

In the `// DigestJobs` section, after `DeleteDigestJob`:
```go
BulkDeleteDigestJobs(ctx context.Context, f BulkDeleteDigestJobsFilter) (int64, error)
```

In the `// SearchJobs` section, after `DeleteSearchJob`:
```go
BulkDeleteSearchJobs(ctx context.Context, f BulkDeleteSearchJobsFilter) (int64, error)
```

In the `// Actions` section, after `DeleteAction`:
```go
BulkDeleteActions(ctx context.Context, f BulkDeleteActionsFilter) (int64, error)
```

In the `// Pipelines` section, after `DeletePipeline`:
```go
BulkDeletePipelines(ctx context.Context, f BulkDeletePipelinesFilter) (int64, error)
```

In the `// TriggerPipelineLinks` section, after `DeleteTriggerPipelineLink`:
```go
BulkDeleteTriggerPipelineLinks(ctx context.Context, f BulkDeleteTriggerPipelineLinksFilter) (int64, error)
```

In the `// NotificationProviders` section, after `DeleteNotificationProvider`:
```go
BulkDeleteNotificationProviders(ctx context.Context, f BulkDeleteNotificationProvidersFilter) (int64, error)
```

In the `// NotificationChannels` section, after `DeleteNotificationChannel`:
```go
BulkDeleteNotificationChannels(ctx context.Context, f BulkDeleteNotificationChannelsFilter) (int64, error)
```

In the `// OllamaBackends` section, after `DeleteOllamaBackend`:
```go
BulkDeleteOllamaBackends(ctx context.Context, f BulkDeleteOllamaBackendsFilter) (int64, error)
```

- [ ] **Step 2: Verify the file compiles (the interface is satisfied only after all implementations are added, but the structs and interface definition itself must parse)**

```bash
cd /mnt/shared/projects/personal/iran-watcher && go vet ./internal/store/... 2>&1 | head -20
```

Expected: errors about `sqliteStore does not implement Store` for each new method — this is expected and will be fixed in subsequent tasks. No syntax errors.

- [ ] **Step 3: Commit**

```bash
git add internal/store/store.go
git commit -m "feat: add BulkDelete filter structs and Store interface methods"
```

---

### Task 2: BulkDeleteEntityCandidates — test + implementation

**Files:**
- Modify: `internal/store/sqlite_test.go`
- Modify: `internal/store/sqlite.go`

- [ ] **Step 1: Write failing tests in sqlite_test.go**

Append to `internal/store/sqlite_test.go`:

```go
// ── BulkDeleteEntityCandidates ───────────────────────────────────────────────

func seedCandidate(t *testing.T, s Store, word, typ, status string, conf float64) *model.EntityCandidate {
	t.Helper()
	c := &model.EntityCandidate{
		Word:          word,
		Type:          typ,
		Status:        status,
		AvgConfidence: conf,
		Sources:       []string{"test"},
		FirstSeenAt:   time.Now().UTC(),
		LastSeenAt:    time.Now().UTC(),
	}
	if err := s.UpsertEntityCandidate(context.Background(), c); err != nil {
		t.Fatalf("UpsertEntityCandidate(%q): %v", word, err)
	}
	return c
}

func TestBulkDeleteEntityCandidates_All(t *testing.T) {
	s := newTestStore(t)
	seedCandidate(t, s, "Tehran", "I-LOC", "pending", 0.9)
	seedCandidate(t, s, "Rouhani", "I-PER", "pending", 0.8)

	n, err := s.BulkDeleteEntityCandidates(context.Background(), BulkDeleteEntityCandidatesFilter{})
	if err != nil {
		t.Fatalf("BulkDeleteEntityCandidates: %v", err)
	}
	if n != 2 {
		t.Errorf("expected 2 deleted, got %d", n)
	}
	pending := "pending"
	count, _ := s.CountEntityCandidates(context.Background(), EntityCandidateFilter{Status: &pending})
	if count != 0 {
		t.Errorf("expected 0 remaining, got %d", count)
	}
}

func TestBulkDeleteEntityCandidates_ByIDs(t *testing.T) {
	s := newTestStore(t)
	c1 := seedCandidate(t, s, "Tehran", "I-LOC", "pending", 0.9)
	c2 := seedCandidate(t, s, "Rouhani", "I-PER", "pending", 0.8)

	n, err := s.BulkDeleteEntityCandidates(context.Background(), BulkDeleteEntityCandidatesFilter{IDs: []int64{c1.ID}})
	if err != nil {
		t.Fatalf("BulkDeleteEntityCandidates: %v", err)
	}
	if n != 1 {
		t.Errorf("expected 1 deleted, got %d", n)
	}
	// c2 must still exist
	if _, err := s.GetEntityCandidate(context.Background(), c2.ID); err != nil {
		t.Errorf("c2 should still exist: %v", err)
	}
}

func TestBulkDeleteEntityCandidates_ByStatus(t *testing.T) {
	s := newTestStore(t)
	seedCandidate(t, s, "Tehran", "I-LOC", "pending", 0.9)
	c2 := seedCandidate(t, s, "Rouhani", "I-PER", "promoted", 0.95)

	n, err := s.BulkDeleteEntityCandidates(context.Background(), BulkDeleteEntityCandidatesFilter{Status: "pending"})
	if err != nil {
		t.Fatalf("BulkDeleteEntityCandidates: %v", err)
	}
	if n != 1 {
		t.Errorf("expected 1 deleted, got %d", n)
	}
	// promoted must survive
	if _, err := s.GetEntityCandidate(context.Background(), c2.ID); err != nil {
		t.Errorf("promoted candidate should still exist: %v", err)
	}
}

func TestBulkDeleteEntityCandidates_ByType(t *testing.T) {
	s := newTestStore(t)
	seedCandidate(t, s, "Tehran", "I-LOC", "pending", 0.9)
	c2 := seedCandidate(t, s, "Rouhani", "I-PER", "pending", 0.8)

	n, err := s.BulkDeleteEntityCandidates(context.Background(), BulkDeleteEntityCandidatesFilter{Type: "I-LOC"})
	if err != nil {
		t.Fatalf("BulkDeleteEntityCandidates: %v", err)
	}
	if n != 1 {
		t.Errorf("expected 1 deleted, got %d", n)
	}
	if _, err := s.GetEntityCandidate(context.Background(), c2.ID); err != nil {
		t.Errorf("I-PER candidate should still exist: %v", err)
	}
}

func TestBulkDeleteEntityCandidates_ByConfidenceRange(t *testing.T) {
	s := newTestStore(t)
	seedCandidate(t, s, "Tehran", "I-LOC", "pending", 0.3)
	c2 := seedCandidate(t, s, "Rouhani", "I-PER", "pending", 0.9)

	maxConf := 0.5
	n, err := s.BulkDeleteEntityCandidates(context.Background(), BulkDeleteEntityCandidatesFilter{MaxConfidence: &maxConf})
	if err != nil {
		t.Fatalf("BulkDeleteEntityCandidates: %v", err)
	}
	if n != 1 {
		t.Errorf("expected 1 deleted, got %d", n)
	}
	if _, err := s.GetEntityCandidate(context.Background(), c2.ID); err != nil {
		t.Errorf("high-confidence candidate should still exist: %v", err)
	}
}

func TestBulkDeleteEntityCandidates_NoMatch(t *testing.T) {
	s := newTestStore(t)

	n, err := s.BulkDeleteEntityCandidates(context.Background(), BulkDeleteEntityCandidatesFilter{Status: "pending"})
	if err != nil {
		t.Fatalf("BulkDeleteEntityCandidates: %v", err)
	}
	if n != 0 {
		t.Errorf("expected 0 deleted, got %d", n)
	}
}
```

- [ ] **Step 2: Run tests — expect FAIL (method not defined)**

```bash
cd /mnt/shared/projects/personal/iran-watcher && go test ./internal/store/... -run TestBulkDeleteEntityCandidates -count=1 2>&1 | tail -5
```

Expected: compile error `sqliteStore does not implement Store`.

- [ ] **Step 3: Implement BulkDeleteEntityCandidates in sqlite.go**

Append after `func (s *sqliteStore) DeleteEntityCandidate(...)` (around line 1811):

```go
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
```

- [ ] **Step 4: Run tests — expect PASS**

```bash
cd /mnt/shared/projects/personal/iran-watcher && go test ./internal/store/... -run TestBulkDeleteEntityCandidates -count=1 -v 2>&1 | tail -15
```

Expected: all 6 subtests PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/store/sqlite_test.go internal/store/sqlite.go
git commit -m "feat: add BulkDeleteEntityCandidates store function"
```

---

### Task 3: BulkDeleteRules, BulkDeleteSources, BulkDeleteTrackedEntities — test + implementation

**Files:**
- Modify: `internal/store/sqlite_test.go`
- Modify: `internal/store/sqlite.go`

- [ ] **Step 1: Write failing tests in sqlite_test.go**

Append to `internal/store/sqlite_test.go`:

```go
// ── BulkDeleteRules ──────────────────────────────────────────────────────────

func TestBulkDeleteRules_All(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	r1 := sampleRule()
	r1.Name = "rule-1"
	if err := s.SaveRule(ctx, r1); err != nil {
		t.Fatalf("SaveRule: %v", err)
	}
	r2 := sampleRule()
	r2.Name = "rule-2"
	if err := s.SaveRule(ctx, r2); err != nil {
		t.Fatalf("SaveRule: %v", err)
	}

	n, err := s.BulkDeleteRules(ctx, BulkDeleteRulesFilter{})
	if err != nil {
		t.Fatalf("BulkDeleteRules: %v", err)
	}
	if n != 2 {
		t.Errorf("expected 2 deleted, got %d", n)
	}
	rules, _ := s.ListRules(ctx, RuleFilter{})
	if len(rules) != 0 {
		t.Errorf("expected 0 rules remaining, got %d", len(rules))
	}
}

func TestBulkDeleteRules_ByIDs(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	r1 := sampleRule()
	r1.Name = "rule-1"
	_ = s.SaveRule(ctx, r1)
	r2 := sampleRule()
	r2.Name = "rule-2"
	_ = s.SaveRule(ctx, r2)

	n, err := s.BulkDeleteRules(ctx, BulkDeleteRulesFilter{IDs: []int64{r1.ID}})
	if err != nil {
		t.Fatalf("BulkDeleteRules: %v", err)
	}
	if n != 1 {
		t.Errorf("expected 1 deleted, got %d", n)
	}
	rules, _ := s.ListRules(ctx, RuleFilter{})
	if len(rules) != 1 || rules[0].ID != r2.ID {
		t.Errorf("expected only r2 remaining")
	}
}

func TestBulkDeleteRules_ByLocal(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	r1 := sampleRule()
	r1.Name = "local-rule"
	r1.Local = true
	_ = s.SaveRule(ctx, r1)
	r2 := sampleRule()
	r2.Name = "remote-rule"
	r2.Local = false
	_ = s.SaveRule(ctx, r2)

	localTrue := true
	n, err := s.BulkDeleteRules(ctx, BulkDeleteRulesFilter{Local: &localTrue})
	if err != nil {
		t.Fatalf("BulkDeleteRules: %v", err)
	}
	if n != 1 {
		t.Errorf("expected 1 deleted, got %d", n)
	}
	rules, _ := s.ListRules(ctx, RuleFilter{})
	if len(rules) != 1 || rules[0].ID != r2.ID {
		t.Errorf("expected only remote rule remaining")
	}
}

// ── BulkDeleteSources ────────────────────────────────────────────────────────

func TestBulkDeleteSources_All(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	src1 := sampleSource()
	src1.Name = "src-1"
	_ = s.SaveSource(ctx, src1)
	src2 := sampleSource()
	src2.Name = "src-2"
	_ = s.SaveSource(ctx, src2)

	n, err := s.BulkDeleteSources(ctx, BulkDeleteSourcesFilter{})
	if err != nil {
		t.Fatalf("BulkDeleteSources: %v", err)
	}
	if n != 2 {
		t.Errorf("expected 2 deleted, got %d", n)
	}
	srcs, _ := s.ListSources(ctx, SourceFilter{})
	if len(srcs) != 0 {
		t.Errorf("expected 0 sources remaining, got %d", len(srcs))
	}
}

func TestBulkDeleteSources_ByType(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	src1 := sampleSource()
	src1.Name = "kw-src"
	src1.Type = model.SourceTypeKeyword
	_ = s.SaveSource(ctx, src1)
	src2 := sampleSource()
	src2.Name = "ph-src"
	src2.Type = model.SourceTypePhrase
	_ = s.SaveSource(ctx, src2)

	n, err := s.BulkDeleteSources(ctx, BulkDeleteSourcesFilter{Type: string(model.SourceTypeKeyword)})
	if err != nil {
		t.Fatalf("BulkDeleteSources: %v", err)
	}
	if n != 1 {
		t.Errorf("expected 1 deleted, got %d", n)
	}
	srcs, _ := s.ListSources(ctx, SourceFilter{})
	if len(srcs) != 1 || srcs[0].ID != src2.ID {
		t.Errorf("expected only phrase source remaining")
	}
}

// ── BulkDeleteTrackedEntities ────────────────────────────────────────────────

func TestBulkDeleteTrackedEntities_All(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	seedTrackedEntity(t, s, "Tehran")
	seedTrackedEntity(t, s, "Rouhani")

	n, err := s.BulkDeleteTrackedEntities(ctx, BulkDeleteTrackedEntitiesFilter{})
	if err != nil {
		t.Fatalf("BulkDeleteTrackedEntities: %v", err)
	}
	if n != 2 {
		t.Errorf("expected 2 deleted, got %d", n)
	}
	entities, _ := s.ListTrackedEntities(ctx, TrackedEntityFilter{})
	if len(entities) != 0 {
		t.Errorf("expected 0 entities remaining, got %d", len(entities))
	}
}

func TestBulkDeleteTrackedEntities_ByType(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	e1 := seedTrackedEntity(t, s, "Tehran")   // type = "PER" from seedTrackedEntity
	e2 := &model.TrackedEntity{Name: "IRGC", Type: "ORG", Aliases: []string{}, Active: true}
	_ = s.SaveTrackedEntity(ctx, e2)

	n, err := s.BulkDeleteTrackedEntities(ctx, BulkDeleteTrackedEntitiesFilter{Type: "PER"})
	if err != nil {
		t.Fatalf("BulkDeleteTrackedEntities: %v", err)
	}
	if n != 1 {
		t.Errorf("expected 1 deleted, got %d", n)
	}
	entities, _ := s.ListTrackedEntities(ctx, TrackedEntityFilter{})
	if len(entities) != 1 || entities[0].ID != e2.ID {
		t.Errorf("expected only ORG entity remaining, e1.ID=%d", e1.ID)
	}
}
```

- [ ] **Step 2: Run tests — expect FAIL**

```bash
cd /mnt/shared/projects/personal/iran-watcher && go test ./internal/store/... -run "TestBulkDeleteRules|TestBulkDeleteSources|TestBulkDeleteTrackedEntities" -count=1 2>&1 | tail -5
```

Expected: compile error (methods not yet implemented).

- [ ] **Step 3: Implement the three functions in sqlite.go**

Append after `func (s *sqliteStore) DeleteRule(...)` (around line 546):

```go
func (s *sqliteStore) BulkDeleteRules(ctx context.Context, f BulkDeleteRulesFilter) (int64, error) {
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

	query := "DELETE FROM rules"
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
```

Append after `func (s *sqliteStore) DeleteSource(...)` (around line 400):

```go
func (s *sqliteStore) BulkDeleteSources(ctx context.Context, f BulkDeleteSourcesFilter) (int64, error) {
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

	query := "DELETE FROM sources"
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
```

Append after `func (s *sqliteStore) DeleteTrackedEntity(...)` (around line 830):

```go
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
```

- [ ] **Step 4: Run tests — expect PASS**

```bash
cd /mnt/shared/projects/personal/iran-watcher && go test ./internal/store/... -run "TestBulkDeleteRules|TestBulkDeleteSources|TestBulkDeleteTrackedEntities" -count=1 -v 2>&1 | tail -15
```

Expected: all subtests PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/store/sqlite_test.go internal/store/sqlite.go
git commit -m "feat: add BulkDeleteRules, BulkDeleteSources, BulkDeleteTrackedEntities store functions"
```

---

### Task 4: BulkDeleteSearchJobs, BulkDeleteDigestJobs — test + implementation

**Files:**
- Modify: `internal/store/sqlite_test.go`
- Modify: `internal/store/sqlite.go`

- [ ] **Step 1: Write failing tests**

Append to `internal/store/sqlite_test.go`:

```go
// ── BulkDeleteSearchJobs ─────────────────────────────────────────────────────

func seedSearchJob(t *testing.T, s Store, status model.SearchJobStatus) *model.SearchJob {
	t.Helper()
	job := &model.SearchJob{
		ID:        fmt.Sprintf("search-%d", time.Now().UnixNano()),
		Status:    status,
		CreatedAt: time.Now().UTC(),
	}
	if err := s.SaveSearchJob(context.Background(), job); err != nil {
		t.Fatalf("SaveSearchJob: %v", err)
	}
	return job
}

func TestBulkDeleteSearchJobs_All(t *testing.T) {
	s := newTestStore(t)
	seedSearchJob(t, s, model.SearchJobStatusDone)
	seedSearchJob(t, s, model.SearchJobStatusError)

	n, err := s.BulkDeleteSearchJobs(context.Background(), BulkDeleteSearchJobsFilter{})
	if err != nil {
		t.Fatalf("BulkDeleteSearchJobs: %v", err)
	}
	if n != 2 {
		t.Errorf("expected 2 deleted, got %d", n)
	}
}

func TestBulkDeleteSearchJobs_ByStatus(t *testing.T) {
	s := newTestStore(t)
	j1 := seedSearchJob(t, s, model.SearchJobStatusDone)
	j2 := seedSearchJob(t, s, model.SearchJobStatusError)

	n, err := s.BulkDeleteSearchJobs(context.Background(), BulkDeleteSearchJobsFilter{Status: string(model.SearchJobStatusDone)})
	if err != nil {
		t.Fatalf("BulkDeleteSearchJobs: %v", err)
	}
	if n != 1 {
		t.Errorf("expected 1 deleted, got %d", n)
	}
	// j2 must still exist
	if _, err := s.GetSearchJob(context.Background(), j2.ID); err != nil {
		t.Errorf("error job should still exist: %v", err)
	}
	_ = j1
}

func TestBulkDeleteSearchJobs_ByIDs(t *testing.T) {
	s := newTestStore(t)
	j1 := seedSearchJob(t, s, model.SearchJobStatusDone)
	j2 := seedSearchJob(t, s, model.SearchJobStatusDone)

	n, err := s.BulkDeleteSearchJobs(context.Background(), BulkDeleteSearchJobsFilter{IDs: []string{j1.ID}})
	if err != nil {
		t.Fatalf("BulkDeleteSearchJobs: %v", err)
	}
	if n != 1 {
		t.Errorf("expected 1 deleted, got %d", n)
	}
	if _, err := s.GetSearchJob(context.Background(), j2.ID); err != nil {
		t.Errorf("j2 should still exist: %v", err)
	}
}

// ── BulkDeleteDigestJobs ─────────────────────────────────────────────────────

func seedDigestJob(t *testing.T, s Store, status model.DigestStatus) *model.DigestJob {
	t.Helper()
	job := &model.DigestJob{
		ID:        fmt.Sprintf("digest-%d", time.Now().UnixNano()),
		Status:    status,
		CreatedAt: time.Now().UTC(),
	}
	if err := s.SaveDigestJob(context.Background(), job); err != nil {
		t.Fatalf("SaveDigestJob: %v", err)
	}
	return job
}

func TestBulkDeleteDigestJobs_All(t *testing.T) {
	s := newTestStore(t)
	seedDigestJob(t, s, model.DigestStatusDone)
	seedDigestJob(t, s, model.DigestStatusError)

	n, err := s.BulkDeleteDigestJobs(context.Background(), BulkDeleteDigestJobsFilter{})
	if err != nil {
		t.Fatalf("BulkDeleteDigestJobs: %v", err)
	}
	if n != 2 {
		t.Errorf("expected 2 deleted, got %d", n)
	}
}

func TestBulkDeleteDigestJobs_ByStatus(t *testing.T) {
	s := newTestStore(t)
	j1 := seedDigestJob(t, s, model.DigestStatusDone)
	j2 := seedDigestJob(t, s, model.DigestStatusError)

	n, err := s.BulkDeleteDigestJobs(context.Background(), BulkDeleteDigestJobsFilter{Status: string(model.DigestStatusDone)})
	if err != nil {
		t.Fatalf("BulkDeleteDigestJobs: %v", err)
	}
	if n != 1 {
		t.Errorf("expected 1 deleted, got %d", n)
	}
	if _, err := s.GetDigestJob(context.Background(), j2.ID); err != nil {
		t.Errorf("error digest job should still exist: %v", err)
	}
	_ = j1
}
```

Note: `sqlite_test.go` may not import `"fmt"`. The helpers `seedSearchJob` and `seedDigestJob` use `fmt.Sprintf`. Add `"fmt"` to the import block in `sqlite_test.go` if it is not already there.

- [ ] **Step 2: Run tests — expect FAIL**

```bash
cd /mnt/shared/projects/personal/iran-watcher && go test ./internal/store/... -run "TestBulkDeleteSearchJobs|TestBulkDeleteDigestJobs" -count=1 2>&1 | tail -5
```

- [ ] **Step 3: Implement BulkDeleteSearchJobs and BulkDeleteDigestJobs in sqlite.go**

Append after `func (s *sqliteStore) DeleteSearchJob(...)` (around line 1393):

```go
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
```

Append after `func (s *sqliteStore) DeleteDigestJob(...)` (around line 1258):

```go
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
```

- [ ] **Step 4: Run tests — expect PASS**

```bash
cd /mnt/shared/projects/personal/iran-watcher && go test ./internal/store/... -run "TestBulkDeleteSearchJobs|TestBulkDeleteDigestJobs" -count=1 -v 2>&1 | tail -15
```

- [ ] **Step 5: Commit**

```bash
git add internal/store/sqlite_test.go internal/store/sqlite.go
git commit -m "feat: add BulkDeleteSearchJobs, BulkDeleteDigestJobs store functions"
```

---

### Task 5: BulkDeleteActions — test + implementation

**Files:**
- Modify: `internal/store/sqlite_actions_test.go`
- Modify: `internal/store/sqlite_actions.go`

- [ ] **Step 1: Write failing tests in sqlite_actions_test.go**

Append to `internal/store/sqlite_actions_test.go`:

```go
// ── BulkDeleteActions ────────────────────────────────────────────────────────

func TestBulkDeleteActions_All(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	a1 := sampleAction()
	a1.Name = "action-1"
	_ = s.SaveAction(ctx, a1)
	a2 := sampleAction()
	a2.Name = "action-2"
	_ = s.SaveAction(ctx, a2)

	n, err := s.BulkDeleteActions(ctx, BulkDeleteActionsFilter{})
	if err != nil {
		t.Fatalf("BulkDeleteActions: %v", err)
	}
	if n != 2 {
		t.Errorf("expected 2 deleted, got %d", n)
	}
	actions, _ := s.ListActions(ctx, ActionFilter{})
	if len(actions) != 0 {
		t.Errorf("expected 0 actions remaining, got %d", len(actions))
	}
}

func TestBulkDeleteActions_ByIDs(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	a1 := sampleAction()
	a1.Name = "action-1"
	_ = s.SaveAction(ctx, a1)
	a2 := sampleAction()
	a2.Name = "action-2"
	_ = s.SaveAction(ctx, a2)

	n, err := s.BulkDeleteActions(ctx, BulkDeleteActionsFilter{IDs: []int64{a1.ID}})
	if err != nil {
		t.Fatalf("BulkDeleteActions: %v", err)
	}
	if n != 1 {
		t.Errorf("expected 1 deleted, got %d", n)
	}
	actions, _ := s.ListActions(ctx, ActionFilter{})
	if len(actions) != 1 || actions[0].ID != a2.ID {
		t.Errorf("expected only a2 remaining")
	}
}

func TestBulkDeleteActions_ByType(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	a1 := sampleAction() // type = ActionTypeInternal
	a1.Name = "internal-action"
	_ = s.SaveAction(ctx, a1)
	a2 := sampleAction()
	a2.Name = "onnx-action"
	a2.Type = model.ActionTypeONNX
	a2.Config = []byte(`{"model":"test"}`)
	_ = s.SaveAction(ctx, a2)

	n, err := s.BulkDeleteActions(ctx, BulkDeleteActionsFilter{Type: string(model.ActionTypeInternal)})
	if err != nil {
		t.Fatalf("BulkDeleteActions: %v", err)
	}
	if n != 1 {
		t.Errorf("expected 1 deleted, got %d", n)
	}
	actions, _ := s.ListActions(ctx, ActionFilter{})
	if len(actions) != 1 || actions[0].ID != a2.ID {
		t.Errorf("expected only ONNX action remaining")
	}
}

func TestBulkDeleteActions_ByActive(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	a1 := sampleAction() // active = true
	a1.Name = "active-action"
	_ = s.SaveAction(ctx, a1)
	a2 := sampleAction()
	a2.Name = "inactive-action"
	a2.Active = false
	_ = s.SaveAction(ctx, a2)

	activeFalse := false
	n, err := s.BulkDeleteActions(ctx, BulkDeleteActionsFilter{Active: &activeFalse})
	if err != nil {
		t.Fatalf("BulkDeleteActions: %v", err)
	}
	if n != 1 {
		t.Errorf("expected 1 deleted, got %d", n)
	}
	actions, _ := s.ListActions(ctx, ActionFilter{})
	if len(actions) != 1 || actions[0].ID != a1.ID {
		t.Errorf("expected only active action remaining")
	}
}
```

- [ ] **Step 2: Run tests — expect FAIL**

```bash
cd /mnt/shared/projects/personal/iran-watcher && go test ./internal/store/... -run TestBulkDeleteActions -count=1 2>&1 | tail -5
```

- [ ] **Step 3: Implement BulkDeleteActions in sqlite_actions.go**

Append after `func (s *sqliteStore) DeleteAction(...)` (around line 102):

```go
func (s *sqliteStore) BulkDeleteActions(ctx context.Context, f BulkDeleteActionsFilter) (int64, error) {
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

	query := "DELETE FROM actions"
	if len(clauses) > 0 {
		query += " WHERE " + strings.Join(clauses, " AND ")
	}

	res, err := s.db.ExecContext(ctx, query, args...)
	if err != nil {
		return 0, fmt.Errorf("store: bulk delete actions: %w", err)
	}
	n, _ := res.RowsAffected()
	return n, nil
}
```

- [ ] **Step 4: Run tests — expect PASS**

```bash
cd /mnt/shared/projects/personal/iran-watcher && go test ./internal/store/... -run TestBulkDeleteActions -count=1 -v 2>&1 | tail -15
```

- [ ] **Step 5: Commit**

```bash
git add internal/store/sqlite_actions_test.go internal/store/sqlite_actions.go
git commit -m "feat: add BulkDeleteActions store function"
```

---

### Task 6: BulkDeletePipelines + BulkDeleteTriggerPipelineLinks — test + implementation

**Files:**
- Create: `internal/store/sqlite_pipelines_test.go`
- Modify: `internal/store/sqlite_pipelines.go`

- [ ] **Step 1: Add `"strings"` to sqlite_pipelines.go imports**

Edit `internal/store/sqlite_pipelines.go` — change the import block from:

```go
import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/amir-the-h/iran-watcher/internal/model"
)
```

to:

```go
import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/amir-the-h/iran-watcher/internal/model"
)
```

- [ ] **Step 2: Create sqlite_pipelines_test.go with failing tests**

Create `internal/store/sqlite_pipelines_test.go`:

```go
package store

import (
	"context"
	"testing"
	"time"

	"github.com/amir-the-h/iran-watcher/internal/model"
)

func seedPipeline(t *testing.T, s Store, name string, active bool) *model.Pipeline {
	t.Helper()
	p := &model.Pipeline{
		Name:      name,
		Active:    active,
		CreatedAt: time.Now().UTC(),
		UpdatedAt: time.Now().UTC(),
	}
	if err := s.SavePipeline(context.Background(), p); err != nil {
		t.Fatalf("SavePipeline(%q): %v", name, err)
	}
	return p
}

func TestBulkDeletePipelines_All(t *testing.T) {
	s := newTestStore(t)
	seedPipeline(t, s, "pipe-1", true)
	seedPipeline(t, s, "pipe-2", false)

	n, err := s.BulkDeletePipelines(context.Background(), BulkDeletePipelinesFilter{})
	if err != nil {
		t.Fatalf("BulkDeletePipelines: %v", err)
	}
	if n != 2 {
		t.Errorf("expected 2 deleted, got %d", n)
	}
	pipes, _ := s.ListPipelines(context.Background())
	if len(pipes) != 0 {
		t.Errorf("expected 0 pipelines remaining, got %d", len(pipes))
	}
}

func TestBulkDeletePipelines_ByIDs(t *testing.T) {
	s := newTestStore(t)
	p1 := seedPipeline(t, s, "pipe-1", true)
	p2 := seedPipeline(t, s, "pipe-2", true)

	n, err := s.BulkDeletePipelines(context.Background(), BulkDeletePipelinesFilter{IDs: []int64{p1.ID}})
	if err != nil {
		t.Fatalf("BulkDeletePipelines: %v", err)
	}
	if n != 1 {
		t.Errorf("expected 1 deleted, got %d", n)
	}
	pipes, _ := s.ListPipelines(context.Background())
	if len(pipes) != 1 || pipes[0].ID != p2.ID {
		t.Errorf("expected only p2 remaining")
	}
}

func TestBulkDeletePipelines_ByActive(t *testing.T) {
	s := newTestStore(t)
	p1 := seedPipeline(t, s, "active-pipe", true)
	p2 := seedPipeline(t, s, "inactive-pipe", false)

	activeFalse := false
	n, err := s.BulkDeletePipelines(context.Background(), BulkDeletePipelinesFilter{Active: &activeFalse})
	if err != nil {
		t.Fatalf("BulkDeletePipelines: %v", err)
	}
	if n != 1 {
		t.Errorf("expected 1 deleted, got %d", n)
	}
	pipes, _ := s.ListPipelines(context.Background())
	if len(pipes) != 1 || pipes[0].ID != p1.ID {
		t.Errorf("expected only active pipeline remaining, p2.ID=%d", p2.ID)
	}
}

func seedTriggerLink(t *testing.T, s Store, triggerType string, pipelineID int64) *model.TriggerPipelineLink {
	t.Helper()
	l := &model.TriggerPipelineLink{
		TriggerType: triggerType,
		PipelineID:  pipelineID,
		Active:      true,
	}
	if err := s.SaveTriggerPipelineLink(context.Background(), l); err != nil {
		t.Fatalf("SaveTriggerPipelineLink: %v", err)
	}
	return l
}

func TestBulkDeleteTriggerPipelineLinks_All(t *testing.T) {
	s := newTestStore(t)
	p := seedPipeline(t, s, "pipe", true)
	seedTriggerLink(t, s, "new_tweet", p.ID)
	seedTriggerLink(t, s, "entity_matched", p.ID)

	n, err := s.BulkDeleteTriggerPipelineLinks(context.Background(), BulkDeleteTriggerPipelineLinksFilter{})
	if err != nil {
		t.Fatalf("BulkDeleteTriggerPipelineLinks: %v", err)
	}
	if n != 2 {
		t.Errorf("expected 2 deleted, got %d", n)
	}
	links, _ := s.ListTriggerPipelineLinks(context.Background())
	if len(links) != 0 {
		t.Errorf("expected 0 links remaining, got %d", len(links))
	}
}

func TestBulkDeleteTriggerPipelineLinks_ByPipelineID(t *testing.T) {
	s := newTestStore(t)
	p1 := seedPipeline(t, s, "pipe-1", true)
	p2 := seedPipeline(t, s, "pipe-2", true)
	l1 := seedTriggerLink(t, s, "new_tweet", p1.ID)
	l2 := seedTriggerLink(t, s, "new_tweet", p2.ID)

	n, err := s.BulkDeleteTriggerPipelineLinks(context.Background(), BulkDeleteTriggerPipelineLinksFilter{PipelineID: &p1.ID})
	if err != nil {
		t.Fatalf("BulkDeleteTriggerPipelineLinks: %v", err)
	}
	if n != 1 {
		t.Errorf("expected 1 deleted, got %d", n)
	}
	links, _ := s.ListTriggerPipelineLinks(context.Background())
	if len(links) != 1 || links[0].ID != l2.ID {
		t.Errorf("expected only l2 remaining, l1.ID=%d", l1.ID)
	}
}

func TestBulkDeleteTriggerPipelineLinks_ByTriggerType(t *testing.T) {
	s := newTestStore(t)
	p := seedPipeline(t, s, "pipe", true)
	seedTriggerLink(t, s, "new_tweet", p.ID)
	l2 := seedTriggerLink(t, s, "entity_matched", p.ID)

	n, err := s.BulkDeleteTriggerPipelineLinks(context.Background(), BulkDeleteTriggerPipelineLinksFilter{TriggerType: "new_tweet"})
	if err != nil {
		t.Fatalf("BulkDeleteTriggerPipelineLinks: %v", err)
	}
	if n != 1 {
		t.Errorf("expected 1 deleted, got %d", n)
	}
	links, _ := s.ListTriggerPipelineLinks(context.Background())
	if len(links) != 1 || links[0].ID != l2.ID {
		t.Errorf("expected only entity_matched link remaining")
	}
}
```

- [ ] **Step 3: Run tests — expect FAIL**

```bash
cd /mnt/shared/projects/personal/iran-watcher && go test ./internal/store/... -run "TestBulkDeletePipelines|TestBulkDeleteTriggerPipelineLinks" -count=1 2>&1 | tail -5
```

- [ ] **Step 4: Implement BulkDeletePipelines and BulkDeleteTriggerPipelineLinks in sqlite_pipelines.go**

Append after `func (s *sqliteStore) DeletePipeline(...)` (around line 83):

```go
func (s *sqliteStore) BulkDeletePipelines(ctx context.Context, f BulkDeletePipelinesFilter) (int64, error) {
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

	query := "DELETE FROM pipelines"
	if len(clauses) > 0 {
		query += " WHERE " + strings.Join(clauses, " AND ")
	}

	res, err := s.db.ExecContext(ctx, query, args...)
	if err != nil {
		return 0, fmt.Errorf("store: bulk delete pipelines: %w", err)
	}
	n, _ := res.RowsAffected()
	return n, nil
}
```

Append after `func (s *sqliteStore) DeleteTriggerPipelineLink(...)` (around line 292):

```go
func (s *sqliteStore) BulkDeleteTriggerPipelineLinks(ctx context.Context, f BulkDeleteTriggerPipelineLinksFilter) (int64, error) {
	var clauses []string
	var args []any

	if len(f.IDs) > 0 {
		ph := strings.Repeat("?,", len(f.IDs))
		clauses = append(clauses, fmt.Sprintf("id IN (%s)", ph[:len(ph)-1]))
		for _, id := range f.IDs {
			args = append(args, id)
		}
	}
	if f.PipelineID != nil {
		clauses = append(clauses, "pipeline_id = ?")
		args = append(args, *f.PipelineID)
	}
	if f.TriggerType != "" {
		clauses = append(clauses, "trigger_type = ?")
		args = append(args, f.TriggerType)
	}

	query := "DELETE FROM trigger_pipeline_links"
	if len(clauses) > 0 {
		query += " WHERE " + strings.Join(clauses, " AND ")
	}

	res, err := s.db.ExecContext(ctx, query, args...)
	if err != nil {
		return 0, fmt.Errorf("store: bulk delete trigger pipeline links: %w", err)
	}
	n, _ := res.RowsAffected()
	return n, nil
}
```

- [ ] **Step 5: Run tests — expect PASS**

```bash
cd /mnt/shared/projects/personal/iran-watcher && go test ./internal/store/... -run "TestBulkDeletePipelines|TestBulkDeleteTriggerPipelineLinks" -count=1 -v 2>&1 | tail -15
```

- [ ] **Step 6: Commit**

```bash
git add internal/store/sqlite_pipelines_test.go internal/store/sqlite_pipelines.go
git commit -m "feat: add BulkDeletePipelines, BulkDeleteTriggerPipelineLinks store functions"
```

---

### Task 7: BulkDeleteNotificationProviders + BulkDeleteNotificationChannels — test + implementation

**Files:**
- Modify: `internal/store/sqlite_notification_test.go`
- Modify: `internal/store/sqlite_notification_providers.go`

- [ ] **Step 1: Add `"strings"` to sqlite_notification_providers.go imports**

Edit the import block in `internal/store/sqlite_notification_providers.go` — add `"strings"`:

```go
import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/amir-the-h/iran-watcher/internal/model"
)
```

- [ ] **Step 2: Write failing tests in sqlite_notification_test.go**

Append to `internal/store/sqlite_notification_test.go`:

```go
// ── BulkDeleteNotificationProviders ─────────────────────────────────────────

func TestBulkDeleteNotificationProviders_All(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	p1 := sampleProvider()
	p1.Name = "prov-1"
	_ = s.SaveNotificationProvider(ctx, p1)
	p2 := sampleProvider()
	p2.Name = "prov-2"
	_ = s.SaveNotificationProvider(ctx, p2)

	n, err := s.BulkDeleteNotificationProviders(ctx, BulkDeleteNotificationProvidersFilter{})
	if err != nil {
		t.Fatalf("BulkDeleteNotificationProviders: %v", err)
	}
	if n != 2 {
		t.Errorf("expected 2 deleted, got %d", n)
	}
	provs, _ := s.ListNotificationProviders(ctx)
	if len(provs) != 0 {
		t.Errorf("expected 0 providers remaining, got %d", len(provs))
	}
}

func TestBulkDeleteNotificationProviders_ByType(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	p1 := sampleProvider() // type = "pushover"
	p1.Name = "pushover-prov"
	_ = s.SaveNotificationProvider(ctx, p1)
	p2 := sampleProvider()
	p2.Name = "telegram-prov"
	p2.Type = "telegram"
	_ = s.SaveNotificationProvider(ctx, p2)

	n, err := s.BulkDeleteNotificationProviders(ctx, BulkDeleteNotificationProvidersFilter{Type: "pushover"})
	if err != nil {
		t.Fatalf("BulkDeleteNotificationProviders: %v", err)
	}
	if n != 1 {
		t.Errorf("expected 1 deleted, got %d", n)
	}
	provs, _ := s.ListNotificationProviders(ctx)
	if len(provs) != 1 || provs[0].ID != p2.ID {
		t.Errorf("expected only telegram provider remaining")
	}
}

func TestBulkDeleteNotificationProviders_ByIDs(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	p1 := sampleProvider()
	p1.Name = "prov-1"
	_ = s.SaveNotificationProvider(ctx, p1)
	p2 := sampleProvider()
	p2.Name = "prov-2"
	_ = s.SaveNotificationProvider(ctx, p2)

	n, err := s.BulkDeleteNotificationProviders(ctx, BulkDeleteNotificationProvidersFilter{IDs: []int64{p1.ID}})
	if err != nil {
		t.Fatalf("BulkDeleteNotificationProviders: %v", err)
	}
	if n != 1 {
		t.Errorf("expected 1 deleted, got %d", n)
	}
	provs, _ := s.ListNotificationProviders(ctx)
	if len(provs) != 1 || provs[0].ID != p2.ID {
		t.Errorf("expected only p2 remaining")
	}
}

// ── BulkDeleteNotificationChannels ──────────────────────────────────────────

func TestBulkDeleteNotificationChannels_All(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	p := sampleProvider()
	_ = s.SaveNotificationProvider(ctx, p)
	ch1 := sampleChannel(p.ID)
	ch1.Name = "chan-1"
	_ = s.SaveNotificationChannel(ctx, ch1)
	ch2 := sampleChannel(p.ID)
	ch2.Name = "chan-2"
	_ = s.SaveNotificationChannel(ctx, ch2)

	n, err := s.BulkDeleteNotificationChannels(ctx, BulkDeleteNotificationChannelsFilter{})
	if err != nil {
		t.Fatalf("BulkDeleteNotificationChannels: %v", err)
	}
	if n != 2 {
		t.Errorf("expected 2 deleted, got %d", n)
	}
	chans, _ := s.ListNotificationChannels(ctx, nil)
	if len(chans) != 0 {
		t.Errorf("expected 0 channels remaining, got %d", len(chans))
	}
}

func TestBulkDeleteNotificationChannels_ByProviderID(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	p1 := sampleProvider()
	p1.Name = "prov-1"
	_ = s.SaveNotificationProvider(ctx, p1)
	p2 := sampleProvider()
	p2.Name = "prov-2"
	_ = s.SaveNotificationProvider(ctx, p2)
	ch1 := sampleChannel(p1.ID)
	_ = s.SaveNotificationChannel(ctx, ch1)
	ch2 := sampleChannel(p2.ID)
	ch2.Name = "chan-p2"
	_ = s.SaveNotificationChannel(ctx, ch2)

	n, err := s.BulkDeleteNotificationChannels(ctx, BulkDeleteNotificationChannelsFilter{ProviderID: &p1.ID})
	if err != nil {
		t.Fatalf("BulkDeleteNotificationChannels: %v", err)
	}
	if n != 1 {
		t.Errorf("expected 1 deleted, got %d", n)
	}
	chans, _ := s.ListNotificationChannels(ctx, nil)
	if len(chans) != 1 || chans[0].ID != ch2.ID {
		t.Errorf("expected only ch2 remaining")
	}
}
```

- [ ] **Step 3: Run tests — expect FAIL**

```bash
cd /mnt/shared/projects/personal/iran-watcher && go test ./internal/store/... -run "TestBulkDeleteNotification" -count=1 2>&1 | tail -5
```

- [ ] **Step 4: Implement both functions in sqlite_notification_providers.go**

Append after `func (s *sqliteStore) DeleteNotificationProvider(...)` (around line 83):

```go
func (s *sqliteStore) BulkDeleteNotificationProviders(ctx context.Context, f BulkDeleteNotificationProvidersFilter) (int64, error) {
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

	query := "DELETE FROM notification_providers"
	if len(clauses) > 0 {
		query += " WHERE " + strings.Join(clauses, " AND ")
	}

	res, err := s.db.ExecContext(ctx, query, args...)
	if err != nil {
		return 0, fmt.Errorf("store: bulk delete notification providers: %w", err)
	}
	n, _ := res.RowsAffected()
	return n, nil
}
```

Append after `func (s *sqliteStore) DeleteNotificationChannel(...)` (around line 186):

```go
func (s *sqliteStore) BulkDeleteNotificationChannels(ctx context.Context, f BulkDeleteNotificationChannelsFilter) (int64, error) {
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
	if f.ProviderID != nil {
		clauses = append(clauses, "provider_id = ?")
		args = append(args, *f.ProviderID)
	}

	query := "DELETE FROM notification_channels"
	if len(clauses) > 0 {
		query += " WHERE " + strings.Join(clauses, " AND ")
	}

	res, err := s.db.ExecContext(ctx, query, args...)
	if err != nil {
		return 0, fmt.Errorf("store: bulk delete notification channels: %w", err)
	}
	n, _ := res.RowsAffected()
	return n, nil
}
```

- [ ] **Step 5: Run tests — expect PASS**

```bash
cd /mnt/shared/projects/personal/iran-watcher && go test ./internal/store/... -run "TestBulkDeleteNotification" -count=1 -v 2>&1 | tail -15
```

- [ ] **Step 6: Commit**

```bash
git add internal/store/sqlite_notification_test.go internal/store/sqlite_notification_providers.go
git commit -m "feat: add BulkDeleteNotificationProviders, BulkDeleteNotificationChannels store functions"
```

---

### Task 8: BulkDeleteOllamaBackends — test + implementation

**Files:**
- Create: `internal/store/sqlite_ollama_test.go`
- Modify: `internal/store/sqlite_ollama.go`

- [ ] **Step 1: Add `"strings"` to sqlite_ollama.go imports**

Edit `internal/store/sqlite_ollama.go` — change import block:

```go
import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/amir-the-h/iran-watcher/internal/model"
)
```

- [ ] **Step 2: Create sqlite_ollama_test.go with failing tests**

Create `internal/store/sqlite_ollama_test.go`:

```go
package store

import (
	"context"
	"testing"
)

func seedOllamaBackend(t *testing.T, s Store, name string, enabled bool) int64 {
	t.Helper()
	b, err := s.CreateOllamaBackend(context.Background(), name, "http://localhost:11434")
	if err != nil {
		t.Fatalf("CreateOllamaBackend(%q): %v", name, err)
	}
	if !enabled {
		if err := s.SetOllamaBackendEnabled(context.Background(), b.ID, false); err != nil {
			t.Fatalf("SetOllamaBackendEnabled: %v", err)
		}
	}
	return b.ID
}

func TestBulkDeleteOllamaBackends_All(t *testing.T) {
	s := newTestStore(t)
	seedOllamaBackend(t, s, "backend-1", true)
	seedOllamaBackend(t, s, "backend-2", false)

	n, err := s.BulkDeleteOllamaBackends(context.Background(), BulkDeleteOllamaBackendsFilter{})
	if err != nil {
		t.Fatalf("BulkDeleteOllamaBackends: %v", err)
	}
	if n != 2 {
		t.Errorf("expected 2 deleted, got %d", n)
	}
	backends, _ := s.ListOllamaBackends(context.Background())
	if len(backends) != 0 {
		t.Errorf("expected 0 backends remaining, got %d", len(backends))
	}
}

func TestBulkDeleteOllamaBackends_ByIDs(t *testing.T) {
	s := newTestStore(t)
	id1 := seedOllamaBackend(t, s, "backend-1", true)
	id2 := seedOllamaBackend(t, s, "backend-2", true)

	n, err := s.BulkDeleteOllamaBackends(context.Background(), BulkDeleteOllamaBackendsFilter{IDs: []int64{id1}})
	if err != nil {
		t.Fatalf("BulkDeleteOllamaBackends: %v", err)
	}
	if n != 1 {
		t.Errorf("expected 1 deleted, got %d", n)
	}
	backends, _ := s.ListOllamaBackends(context.Background())
	if len(backends) != 1 || backends[0].ID != id2 {
		t.Errorf("expected only backend-2 remaining")
	}
}

func TestBulkDeleteOllamaBackends_ByEnabled(t *testing.T) {
	s := newTestStore(t)
	id1 := seedOllamaBackend(t, s, "enabled-backend", true)
	id2 := seedOllamaBackend(t, s, "disabled-backend", false)

	enabledFalse := false
	n, err := s.BulkDeleteOllamaBackends(context.Background(), BulkDeleteOllamaBackendsFilter{Enabled: &enabledFalse})
	if err != nil {
		t.Fatalf("BulkDeleteOllamaBackends: %v", err)
	}
	if n != 1 {
		t.Errorf("expected 1 deleted, got %d", n)
	}
	backends, _ := s.ListOllamaBackends(context.Background())
	if len(backends) != 1 || backends[0].ID != id1 {
		t.Errorf("expected only enabled backend remaining, id2=%d", id2)
	}
}
```

- [ ] **Step 3: Run tests — expect FAIL**

```bash
cd /mnt/shared/projects/personal/iran-watcher && go test ./internal/store/... -run TestBulkDeleteOllamaBackends -count=1 2>&1 | tail -5
```

- [ ] **Step 4: Implement BulkDeleteOllamaBackends in sqlite_ollama.go**

Append after `func (s *sqliteStore) DeleteOllamaBackend(...)` (around line 76):

```go
func (s *sqliteStore) BulkDeleteOllamaBackends(ctx context.Context, f BulkDeleteOllamaBackendsFilter) (int64, error) {
	var clauses []string
	var args []any

	if len(f.IDs) > 0 {
		ph := strings.Repeat("?,", len(f.IDs))
		clauses = append(clauses, fmt.Sprintf("id IN (%s)", ph[:len(ph)-1]))
		for _, id := range f.IDs {
			args = append(args, id)
		}
	}
	if f.Enabled != nil {
		clauses = append(clauses, "enabled = ?")
		args = append(args, boolToInt(*f.Enabled))
	}

	query := "DELETE FROM ollama_backends"
	if len(clauses) > 0 {
		query += " WHERE " + strings.Join(clauses, " AND ")
	}

	res, err := s.db.ExecContext(ctx, query, args...)
	if err != nil {
		return 0, fmt.Errorf("store: bulk delete ollama backends: %w", err)
	}
	n, _ := res.RowsAffected()
	return n, nil
}
```

- [ ] **Step 5: Run tests — expect PASS**

```bash
cd /mnt/shared/projects/personal/iran-watcher && go test ./internal/store/... -run TestBulkDeleteOllamaBackends -count=1 -v 2>&1 | tail -10
```

- [ ] **Step 6: Run full store test suite to confirm nothing is broken**

```bash
cd /mnt/shared/projects/personal/iran-watcher && go test ./internal/store/... -count=1 2>&1 | tail -5
```

Expected: `ok github.com/amir-the-h/iran-watcher/internal/store`

- [ ] **Step 7: Commit**

```bash
git add internal/store/sqlite_ollama_test.go internal/store/sqlite_ollama.go
git commit -m "feat: add BulkDeleteOllamaBackends store function"
```

---

### Task 9: API handlers for entity-candidates, rules, sources, tracked-entities, search jobs, digest jobs

**Files:**
- Modify: `internal/api/handlers_entity_candidates.go`
- Modify: `internal/api/handlers_rules.go`
- Modify: `internal/api/handlers_sources.go`
- Modify: `internal/api/handlers_entities.go`
- Modify: `internal/api/handlers_search.go`
- Modify: `internal/api/handlers_summaries.go`

- [ ] **Step 1: Add handleBulkDeleteEntityCandidates to handlers_entity_candidates.go**

Append to `internal/api/handlers_entity_candidates.go`:

```go
// handleBulkDeleteEntityCandidates DELETE /api/entity-candidates
func (s *Server) handleBulkDeleteEntityCandidates(w http.ResponseWriter, r *http.Request) {
	var f store.BulkDeleteEntityCandidatesFilter
	if err := json.NewDecoder(r.Body).Decode(&f); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	n, err := s.store.BulkDeleteEntityCandidates(r.Context(), f)
	if err != nil {
		s.logger.Error("api: bulk delete entity candidates", "error", err)
		writeError(w, http.StatusInternalServerError, "failed to bulk delete entity candidates")
		return
	}
	writeJSON(w, http.StatusOK, map[string]int64{"deleted": n})
}
```

- [ ] **Step 2: Add handleBulkDeleteRules to handlers_rules.go**

Append to `internal/api/handlers_rules.go`:

```go
// handleBulkDeleteRules DELETE /api/rules
func (s *Server) handleBulkDeleteRules(w http.ResponseWriter, r *http.Request) {
	var f store.BulkDeleteRulesFilter
	if err := json.NewDecoder(r.Body).Decode(&f); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	n, err := s.store.BulkDeleteRules(r.Context(), f)
	if err != nil {
		s.logger.Error("api: bulk delete rules", "error", err)
		writeError(w, http.StatusInternalServerError, "failed to bulk delete rules")
		return
	}
	writeJSON(w, http.StatusOK, map[string]int64{"deleted": n})
}
```

- [ ] **Step 3: Add handleBulkDeleteSources to handlers_sources.go**

Append to `internal/api/handlers_sources.go`:

```go
// handleBulkDeleteSources DELETE /api/sources
func (s *Server) handleBulkDeleteSources(w http.ResponseWriter, r *http.Request) {
	var f store.BulkDeleteSourcesFilter
	if err := json.NewDecoder(r.Body).Decode(&f); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	n, err := s.store.BulkDeleteSources(r.Context(), f)
	if err != nil {
		s.logger.Error("api: bulk delete sources", "error", err)
		writeError(w, http.StatusInternalServerError, "failed to bulk delete sources")
		return
	}
	writeJSON(w, http.StatusOK, map[string]int64{"deleted": n})
}
```

- [ ] **Step 4: Add handleBulkDeleteTrackedEntities to handlers_entities.go**

Append to `internal/api/handlers_entities.go`:

```go
// handleBulkDeleteTrackedEntities DELETE /api/tracked-entities
func (s *Server) handleBulkDeleteTrackedEntities(w http.ResponseWriter, r *http.Request) {
	var f store.BulkDeleteTrackedEntitiesFilter
	if err := json.NewDecoder(r.Body).Decode(&f); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	n, err := s.store.BulkDeleteTrackedEntities(r.Context(), f)
	if err != nil {
		s.logger.Error("api: bulk delete tracked entities", "error", err)
		writeError(w, http.StatusInternalServerError, "failed to bulk delete tracked entities")
		return
	}
	writeJSON(w, http.StatusOK, map[string]int64{"deleted": n})
}
```

- [ ] **Step 5: Add handleBulkDeleteSearchJobs to handlers_search.go**

Append to `internal/api/handlers_search.go`:

```go
// handleBulkDeleteSearchJobs DELETE /api/search
func (s *Server) handleBulkDeleteSearchJobs(w http.ResponseWriter, r *http.Request) {
	var f store.BulkDeleteSearchJobsFilter
	if err := json.NewDecoder(r.Body).Decode(&f); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	n, err := s.store.BulkDeleteSearchJobs(r.Context(), f)
	if err != nil {
		s.logger.Error("api: bulk delete search jobs", "error", err)
		writeError(w, http.StatusInternalServerError, "failed to bulk delete search jobs")
		return
	}
	writeJSON(w, http.StatusOK, map[string]int64{"deleted": n})
}
```

- [ ] **Step 6: Add handleBulkDeleteDigestJobs to handlers_summaries.go**

Append to `internal/api/handlers_summaries.go`:

```go
// handleBulkDeleteDigestJobs DELETE /api/digest
func (s *Server) handleBulkDeleteDigestJobs(w http.ResponseWriter, r *http.Request) {
	var f store.BulkDeleteDigestJobsFilter
	if err := json.NewDecoder(r.Body).Decode(&f); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	n, err := s.store.BulkDeleteDigestJobs(r.Context(), f)
	if err != nil {
		s.logger.Error("api: bulk delete digest jobs", "error", err)
		writeError(w, http.StatusInternalServerError, "failed to bulk delete digest jobs")
		return
	}
	writeJSON(w, http.StatusOK, map[string]int64{"deleted": n})
}
```

Each handler file already imports `"encoding/json"` and the `store` package. If a file is missing either import, add it.

- [ ] **Step 7: Verify all 6 files compile**

```bash
cd /mnt/shared/projects/personal/iran-watcher && go build ./internal/api/... 2>&1
```

Expected: errors about unregistered routes only (handlers exist but routes not wired yet). If there are compile errors, fix them before continuing.

- [ ] **Step 8: Commit**

```bash
git add internal/api/handlers_entity_candidates.go internal/api/handlers_rules.go \
        internal/api/handlers_sources.go internal/api/handlers_entities.go \
        internal/api/handlers_search.go internal/api/handlers_summaries.go
git commit -m "feat: add bulk delete API handlers for entity-candidates, rules, sources, tracked-entities, search jobs, digest jobs"
```

---

### Task 10: API handlers for actions, pipelines, trigger-links

**Files:**
- Modify: `internal/api/handlers_actions.go`
- Modify: `internal/api/handlers_pipelines.go`
- Modify: `internal/api/handlers_triggers.go`

- [ ] **Step 1: Add handleBulkDeleteActions to handlers_actions.go**

Append to `internal/api/handlers_actions.go`:

```go
// handleBulkDeleteActions DELETE /api/actions
func (s *Server) handleBulkDeleteActions(w http.ResponseWriter, r *http.Request) {
	var f store.BulkDeleteActionsFilter
	if err := json.NewDecoder(r.Body).Decode(&f); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	n, err := s.store.BulkDeleteActions(r.Context(), f)
	if err != nil {
		s.logger.Error("api: bulk delete actions", "error", err)
		writeError(w, http.StatusInternalServerError, "failed to bulk delete actions")
		return
	}
	writeJSON(w, http.StatusOK, map[string]int64{"deleted": n})
}
```

- [ ] **Step 2: Add handleBulkDeletePipelines to handlers_pipelines.go**

Append to `internal/api/handlers_pipelines.go`:

```go
// handleBulkDeletePipelines DELETE /api/pipelines
func (s *Server) handleBulkDeletePipelines(w http.ResponseWriter, r *http.Request) {
	var f store.BulkDeletePipelinesFilter
	if err := json.NewDecoder(r.Body).Decode(&f); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	n, err := s.store.BulkDeletePipelines(r.Context(), f)
	if err != nil {
		s.logger.Error("api: bulk delete pipelines", "error", err)
		writeError(w, http.StatusInternalServerError, "failed to bulk delete pipelines")
		return
	}
	writeJSON(w, http.StatusOK, map[string]int64{"deleted": n})
}
```

- [ ] **Step 3: Add handleBulkDeleteTriggerPipelineLinks to handlers_triggers.go**

Append to `internal/api/handlers_triggers.go`:

```go
// handleBulkDeleteTriggerPipelineLinks DELETE /api/triggers/links
func (s *Server) handleBulkDeleteTriggerPipelineLinks(w http.ResponseWriter, r *http.Request) {
	var f store.BulkDeleteTriggerPipelineLinksFilter
	if err := json.NewDecoder(r.Body).Decode(&f); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	n, err := s.store.BulkDeleteTriggerPipelineLinks(r.Context(), f)
	if err != nil {
		s.logger.Error("api: bulk delete trigger pipeline links", "error", err)
		writeError(w, http.StatusInternalServerError, "failed to bulk delete trigger pipeline links")
		return
	}
	writeJSON(w, http.StatusOK, map[string]int64{"deleted": n})
}
```

- [ ] **Step 4: Verify compile**

```bash
cd /mnt/shared/projects/personal/iran-watcher && go build ./internal/api/... 2>&1
```

- [ ] **Step 5: Commit**

```bash
git add internal/api/handlers_actions.go internal/api/handlers_pipelines.go internal/api/handlers_triggers.go
git commit -m "feat: add bulk delete API handlers for actions, pipelines, trigger-links"
```

---

### Task 11: API handlers for notification providers, channels, ollama backends

**Files:**
- Modify: `internal/api/handlers_notifications.go`
- Modify: `internal/api/handlers_ollama.go`

- [ ] **Step 1: Add handleBulkDeleteNotificationProviders to handlers_notifications.go**

Append to `internal/api/handlers_notifications.go`:

```go
// handleBulkDeleteNotificationProviders DELETE /api/notification-providers
func (s *Server) handleBulkDeleteNotificationProviders(w http.ResponseWriter, r *http.Request) {
	var f store.BulkDeleteNotificationProvidersFilter
	if err := json.NewDecoder(r.Body).Decode(&f); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	n, err := s.store.BulkDeleteNotificationProviders(r.Context(), f)
	if err != nil {
		s.logger.Error("api: bulk delete notification providers", "error", err)
		writeError(w, http.StatusInternalServerError, "failed to bulk delete notification providers")
		return
	}
	writeJSON(w, http.StatusOK, map[string]int64{"deleted": n})
}

// handleBulkDeleteNotificationChannels DELETE /api/notification-channels
func (s *Server) handleBulkDeleteNotificationChannels(w http.ResponseWriter, r *http.Request) {
	var f store.BulkDeleteNotificationChannelsFilter
	if err := json.NewDecoder(r.Body).Decode(&f); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	n, err := s.store.BulkDeleteNotificationChannels(r.Context(), f)
	if err != nil {
		s.logger.Error("api: bulk delete notification channels", "error", err)
		writeError(w, http.StatusInternalServerError, "failed to bulk delete notification channels")
		return
	}
	writeJSON(w, http.StatusOK, map[string]int64{"deleted": n})
}
```

- [ ] **Step 2: Add handleBulkDeleteOllamaBackends to handlers_ollama.go**

Append to `internal/api/handlers_ollama.go`:

```go
// handleBulkDeleteOllamaBackends DELETE /api/ollama/backends
func (s *Server) handleBulkDeleteOllamaBackends(w http.ResponseWriter, r *http.Request) {
	var f store.BulkDeleteOllamaBackendsFilter
	if err := json.NewDecoder(r.Body).Decode(&f); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	n, err := s.store.BulkDeleteOllamaBackends(r.Context(), f)
	if err != nil {
		s.logger.Error("api: bulk delete ollama backends", "error", err)
		writeError(w, http.StatusInternalServerError, "failed to bulk delete ollama backends")
		return
	}
	writeJSON(w, http.StatusOK, map[string]int64{"deleted": n})
}
```

- [ ] **Step 3: Verify compile**

```bash
cd /mnt/shared/projects/personal/iran-watcher && go build ./internal/api/... 2>&1
```

- [ ] **Step 4: Commit**

```bash
git add internal/api/handlers_notifications.go internal/api/handlers_ollama.go
git commit -m "feat: add bulk delete API handlers for notification providers, channels, ollama backends"
```

---

### Task 12: Register all 12 bulk delete routes in server.go

**Files:**
- Modify: `internal/api/server.go`

- [ ] **Step 1: Register all 12 new routes**

In `internal/api/server.go`, inside the admin-only `r.Group(...)` block, add each bulk delete route immediately before the corresponding single-delete route. Find each existing single-delete and insert the bulk route above it:

Before `r.Delete("/rules/{id}", srv.handleDeleteRule)`:
```go
r.Delete("/rules", srv.handleBulkDeleteRules)
```

Before `r.Delete("/sources/{id}", srv.handleDeleteSource)`:
```go
r.Delete("/sources", srv.handleBulkDeleteSources)
```

Before `r.Delete("/pipelines/{id}", srv.handleDeletePipeline)`:
```go
r.Delete("/pipelines", srv.handleBulkDeletePipelines)
```

Before `r.Delete("/triggers/links/{id}", srv.handleDeleteTriggerPipelineLink)`:
```go
r.Delete("/triggers/links", srv.handleBulkDeleteTriggerPipelineLinks)
```

Before `r.Delete("/tracked-entities/{id}", srv.handleDeleteTrackedEntity)`:
```go
r.Delete("/tracked-entities", srv.handleBulkDeleteTrackedEntities)
```

Before `r.Delete("/entity-candidates/{id}", srv.handleDeleteEntityCandidate)`:
```go
r.Delete("/entity-candidates", srv.handleBulkDeleteEntityCandidates)
```

Before `r.Delete("/actions/{id}", srv.handleDeleteAction)`:
```go
r.Delete("/actions", srv.handleBulkDeleteActions)
```

Before `r.Delete("/ollama/backends/{id}", srv.handleDeleteOllamaBackend)`:
```go
r.Delete("/ollama/backends", srv.handleBulkDeleteOllamaBackends)
```

Before `r.Delete("/notification-providers/{id}", srv.handleDeleteNotificationProvider)`:
```go
r.Delete("/notification-providers", srv.handleBulkDeleteNotificationProviders)
```

Before `r.Delete("/notification-channels/{id}", srv.handleDeleteNotificationChannel)`:
```go
r.Delete("/notification-channels", srv.handleBulkDeleteNotificationChannels)
```

Before `r.Delete("/search/{id}", srv.handleDeleteSearchJob)`:
```go
r.Delete("/search", srv.handleBulkDeleteSearchJobs)
```

Before `r.Delete("/digest/{id}", srv.handleDeleteDigest)`:
```go
r.Delete("/digest", srv.handleBulkDeleteDigestJobs)
```

- [ ] **Step 2: Verify compile**

```bash
cd /mnt/shared/projects/personal/iran-watcher && go build ./... 2>&1
```

Expected: no errors.

- [ ] **Step 3: Run all tests**

```bash
cd /mnt/shared/projects/personal/iran-watcher && go test ./... -count=1 2>&1 | tail -20
```

Expected: all packages pass.

- [ ] **Step 4: Commit**

```bash
git add internal/api/server.go
git commit -m "feat: register 12 bulk delete collection routes"
```

---

### Task 13: Add 12 MCP tool entries in tools.go

**Files:**
- Modify: `internal/mcpserver/tools.go`

- [ ] **Step 1: Add bulk delete ToolDef entries**

In `internal/mcpserver/tools.go`, add each `*_bulk_delete` tool immediately after the corresponding `*_delete` tool. Find each existing delete entry and insert below it:

After `rules_delete` entry:
```go
{
    Name:        "rules_bulk_delete",
    Description: "Bulk-delete rules. All filters optional — omit all to delete every rule. Filters combine with AND.",
    Method:      "DELETE",
    Path:        "/api/rules",
    Body: &BodyDef{
        Description: "ids ([]int, optional), active (bool, optional), local (bool, optional). All optional.",
    },
},
```

After `sources_delete` entry:
```go
{
    Name:        "sources_bulk_delete",
    Description: "Bulk-delete sources. All filters optional — omit all to delete every source. Filters combine with AND.",
    Method:      "DELETE",
    Path:        "/api/sources",
    Body: &BodyDef{
        Description: "ids ([]int, optional), active (bool, optional), type (string, optional). All optional.",
    },
},
```

After `tracked_entities_delete` entry:
```go
{
    Name:        "tracked_entities_bulk_delete",
    Description: "Bulk-delete tracked entities. All filters optional — omit all to delete every tracked entity. Filters combine with AND.",
    Method:      "DELETE",
    Path:        "/api/tracked-entities",
    Body: &BodyDef{
        Description: "ids ([]int, optional), active (bool, optional), type (string, optional). All optional.",
    },
},
```

After `entity_candidates_delete` entry:
```go
{
    Name:        "entity_candidates_bulk_delete",
    Description: "Bulk-delete entity candidates. All filters optional — omit all to delete every candidate. Filters combine with AND.",
    Method:      "DELETE",
    Path:        "/api/entity-candidates",
    Body: &BodyDef{
        Description: "ids ([]int, optional), status (string: pending/promoted/rejected, optional), type (string: I-PER/I-LOC/I-ORG/I-MISC, optional), auto_promoted (bool, optional), min_confidence (float, optional), max_confidence (float, optional), min_occurrences (int, optional), max_occurrences (int, optional). All optional.",
    },
},
```

After `pipelines_delete` entry:
```go
{
    Name:        "pipelines_bulk_delete",
    Description: "Bulk-delete pipelines. All filters optional — omit all to delete every pipeline. Filters combine with AND.",
    Method:      "DELETE",
    Path:        "/api/pipelines",
    Body: &BodyDef{
        Description: "ids ([]int, optional), active (bool, optional). All optional.",
    },
},
```

After `triggers_links_delete` entry:
```go
{
    Name:        "triggers_links_bulk_delete",
    Description: "Bulk-delete trigger-to-pipeline links. All filters optional — omit all to delete every link. Filters combine with AND.",
    Method:      "DELETE",
    Path:        "/api/triggers/links",
    Body: &BodyDef{
        Description: "ids ([]int, optional), pipeline_id (int, optional), trigger_type (string, optional). All optional.",
    },
},
```

After `actions_delete` entry:
```go
{
    Name:        "actions_bulk_delete",
    Description: "Bulk-delete actions. All filters optional — omit all to delete every action. Filters combine with AND.",
    Method:      "DELETE",
    Path:        "/api/actions",
    Body: &BodyDef{
        Description: "ids ([]int, optional), active (bool, optional), type (string: onnx/ollama/internal, optional). All optional.",
    },
},
```

After `ollama_backends_delete` entry:
```go
{
    Name:        "ollama_backends_bulk_delete",
    Description: "Bulk-delete Ollama backends. All filters optional — omit all to delete every backend. Filters combine with AND.",
    Method:      "DELETE",
    Path:        "/api/ollama/backends",
    Body: &BodyDef{
        Description: "ids ([]int, optional), enabled (bool, optional). All optional.",
    },
},
```

After `notification_providers_delete` entry:
```go
{
    Name:        "notification_providers_bulk_delete",
    Description: "Bulk-delete notification providers. All filters optional — omit all to delete every provider. Filters combine with AND.",
    Method:      "DELETE",
    Path:        "/api/notification-providers",
    Body: &BodyDef{
        Description: "ids ([]int, optional), active (bool, optional), type (string: pushover/telegram, optional). All optional.",
    },
},
```

After `notification_channels_delete` entry:
```go
{
    Name:        "notification_channels_bulk_delete",
    Description: "Bulk-delete notification channels. All filters optional — omit all to delete every channel. Filters combine with AND.",
    Method:      "DELETE",
    Path:        "/api/notification-channels",
    Body: &BodyDef{
        Description: "ids ([]int, optional), active (bool, optional), provider_id (int, optional). All optional.",
    },
},
```

After `search_job_delete` entry:
```go
{
    Name:        "search_jobs_bulk_delete",
    Description: "Bulk-delete search jobs. All filters optional — omit all to delete every search job. Filters combine with AND.",
    Method:      "DELETE",
    Path:        "/api/search",
    Body: &BodyDef{
        Description: "ids ([]string, optional), status (string: pending/done/error, optional). All optional.",
    },
},
```

After `digest_delete` entry:
```go
{
    Name:        "digest_bulk_delete",
    Description: "Bulk-delete digest jobs. All filters optional — omit all to delete every digest job. Filters combine with AND.",
    Method:      "DELETE",
    Path:        "/api/digest",
    Body: &BodyDef{
        Description: "ids ([]string, optional), status (string: pending/done/error, optional). All optional.",
    },
},
```

- [ ] **Step 2: Verify compile**

```bash
cd /mnt/shared/projects/personal/iran-watcher && go build ./internal/mcpserver/... 2>&1
```

- [ ] **Step 3: Commit**

```bash
git add internal/mcpserver/tools.go
git commit -m "feat: add 12 bulk_delete MCP tool entries"
```

---

### Task 14: Build verification

**Files:** None (verification only)

- [ ] **Step 1: Run full test suite**

```bash
cd /mnt/shared/projects/personal/iran-watcher && go test ./... -count=1 2>&1 | tail -20
```

Expected: all packages pass, no failures.

- [ ] **Step 2: Build the Docker image**

```bash
cd /mnt/shared/projects/personal/iran-watcher && docker compose build iran-watcher 2>&1 | tail -20
```

Expected: `Successfully built ...` or `=> exporting to image` with no errors.

- [ ] **Step 3: Update CLAUDE.md**

In `CLAUDE.md`, in the **Key Design Decisions** section, find the bullet about `executions_delete` (which already documents the bulk delete pattern for executions). Extend the existing `executions_delete` description or add a new bullet after it:

```
- **Bulk delete endpoints** — every resource with a single-delete-by-ID also has a collection-level `DELETE /api/{resource}` endpoint (body: optional `ids` array + resource-specific filter fields; all optional; empty body = delete all; response: `{"deleted": N}`); affected resources: rules, sources, pipelines, triggers/links, tracked-entities, entity-candidates, actions, ollama/backends, notification-providers, notification-channels, search jobs, digest jobs; each is exposed as a `*_bulk_delete` MCP tool; existing single-delete routes are unchanged
```

- [ ] **Step 4: Commit**

```bash
git add CLAUDE.md
git commit -m "docs: document bulk delete endpoints in CLAUDE.md"
```
