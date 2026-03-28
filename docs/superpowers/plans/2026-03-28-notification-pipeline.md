# Notification Pipeline Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Replace the event-driven `Dispatcher` with a first-class `notification` pipeline type backed by `notification_providers` (credentials) and `notification_channels` (delivery config + template), wired through the action/chain system.

**Architecture:** DB records for providers (credentials) and channels (config + `${var}` templates) replace the old `notifications` table. `ActionCache.resolvePipeline` builds a `NotificationPipeline` from a channel+provider pair at cache-load time. `notifier.Dispatcher` and all event-emission code are removed entirely.

**Tech Stack:** Go, SQLite migrations, `modernc.org/sqlite`, `internal/notifier` (PushoverClient, TelegramClient), `internal/inference` (Pipeline interface), `internal/worker` (ActionCache), `internal/api` (chi router), cobra CLI.

---

## File Map

| Action | Path | Responsibility |
|--------|------|----------------|
| Create | `migrations/020_notification_pipeline.sql` | Schema + data migration |
| Modify | `internal/model/notification.go` | Replace `Notification` with `NotificationProvider` + `NotificationChannel` |
| Modify | `internal/model/pipeline.go` | Add `PipelineTypeNotification` |
| Modify | `internal/inference/pipeline.go` | Add `PipelineTypeNotification` to inference type |
| Create | `internal/notifier/provider.go` | `Provider` interface |
| Modify | `internal/notifier/pushover.go` | Add `Deliver` method |
| Modify | `internal/notifier/telegram.go` | Add `Deliver` method |
| Delete | `internal/notifier/dispatcher.go` | Retired |
| Delete | `internal/notifier/dispatcher_test.go` | Retired |
| Delete | `internal/notifier/event.go` | Retired |
| Delete | `internal/notifier/template.go` | Retired |
| Delete | `internal/notifier/template_test.go` | Retired |
| Rewrite | `internal/pipeline/notification.go` | `NotificationPipeline` with `${var}` substitution |
| Create | `internal/pipeline/notification_test.go` | Tests for pipeline |
| Create | `internal/store/sqlite_notification_providers.go` | CRUD for providers + channels |
| Modify | `internal/store/store.go` | Replace old Notification methods with new ones |
| Rewrite | `internal/store/sqlite_notification_test.go` | Tests for new CRUD |
| Modify | `internal/worker/action_cache.go` | Handle `PipelineTypeNotification` in `resolvePipeline` |
| Modify | `internal/worker/worker_test.go` | Update `mockStore` to match new `Store` interface |
| Create | `internal/api/handlers_notification_providers.go` | REST handlers for providers |
| Create | `internal/api/handlers_notification_channels.go` | REST handlers for channels |
| Delete | `internal/api/handlers_notifications.go` | Retired |
| Modify | `internal/api/server.go` | Remove dispatcher field/param, swap routes |
| Modify | `internal/api/handlers_pipelines.go` | Accept `notification` as valid pipeline type |
| Modify | `internal/api/handlers_summaries.go` | Remove `emitDigestEvent` |
| Modify | `internal/stream/client.go` | Remove emitter field + all Emit calls |
| Modify | `cli/cmd/serve.go` | Remove dispatcher/emitter wiring; providers built from DB now |
| Rewrite | `cli/cmd/notification.go` | New `notification providers` + `notification channels` commands |

---

## Task 1: DB Migration

**Files:**
- Create: `migrations/020_notification_pipeline.sql`

- [ ] **Step 1: Write the migration**

```sql
-- 020_notification_pipeline.sql
-- Replaces the notifications table with notification_providers (credentials)
-- and notification_channels (delivery config + template).

CREATE TABLE IF NOT EXISTS notification_providers (
    id         INTEGER  PRIMARY KEY AUTOINCREMENT,
    name       TEXT     NOT NULL,
    type       TEXT     NOT NULL, -- 'pushover' | 'telegram'
    config     TEXT     NOT NULL DEFAULT '{}',
    active     INTEGER  NOT NULL DEFAULT 1,
    created_at DATETIME NOT NULL DEFAULT (datetime('now')),
    updated_at DATETIME NOT NULL DEFAULT (datetime('now'))
);
CREATE INDEX IF NOT EXISTS idx_notification_providers_active ON notification_providers(active);

CREATE TABLE IF NOT EXISTS notification_channels (
    id               INTEGER  PRIMARY KEY AUTOINCREMENT,
    name             TEXT     NOT NULL,
    provider_id      INTEGER  NOT NULL REFERENCES notification_providers(id) ON DELETE CASCADE,
    message_template TEXT     NOT NULL DEFAULT '',
    config           TEXT     NOT NULL DEFAULT '{}',
    active           INTEGER  NOT NULL DEFAULT 1,
    created_at       DATETIME NOT NULL DEFAULT (datetime('now')),
    updated_at       DATETIME NOT NULL DEFAULT (datetime('now'))
);
CREATE INDEX IF NOT EXISTS idx_notification_channels_provider ON notification_channels(provider_id);
CREATE INDEX IF NOT EXISTS idx_notification_channels_active   ON notification_channels(active);

-- Migrate existing notifications rows.
-- Note: credentials (API tokens) were stored in YAML config, not DB rows,
-- so migrated providers have empty config '{}' — users must update them.
INSERT OR IGNORE INTO notification_providers (id, name, type, config, active, created_at, updated_at)
SELECT
    id,
    name,
    CASE WHEN provider = 'telegram' THEN 'telegram' ELSE 'pushover' END,
    '{}',
    active,
    created_at,
    updated_at
FROM notifications;

INSERT OR IGNORE INTO notification_channels (name, provider_id, message_template, config, active, created_at, updated_at)
SELECT
    n.name,
    n.id,
    COALESCE(n.message_template, ''),
    CASE
        WHEN n.provider = 'telegram' THEN json_object(
            'chat_id',                    COALESCE(n.telegram_chat_id, ''),
            'parse_mode',                 n.telegram_parse_mode,
            'disable_web_page_preview',   n.telegram_disable_web_page_preview,
            'disable_notification',       n.telegram_disable_notification,
            'message_thread_id',          n.telegram_message_thread_id
        )
        ELSE json_object(
            'priority',  n.pushover_priority,
            'sound',     n.pushover_sound,
            'device',    n.pushover_device,
            'title',     n.pushover_title,
            'url',       n.pushover_url,
            'url_title', n.pushover_url_title,
            'ttl',       n.pushover_ttl,
            'html',      n.pushover_html,
            'retry',     n.pushover_retry,
            'expire',    n.pushover_expire
        )
    END,
    n.active,
    n.created_at,
    n.updated_at
FROM notifications n;

DROP TABLE IF EXISTS notifications;
```

- [ ] **Step 2: Commit**

```bash
git add migrations/020_notification_pipeline.sql
git commit -m "feat: add notification_providers and notification_channels migration"
```

---

## Task 2: New Model Types

**Files:**
- Modify: `internal/model/notification.go`
- Modify: `internal/model/pipeline.go`
- Modify: `internal/inference/pipeline.go`

- [ ] **Step 1: Replace model/notification.go**

Replace the entire file content:

```go
package model

import (
	"encoding/json"
	"time"
)

// NotificationProvider stores credentials for a notification delivery backend.
type NotificationProvider struct {
	ID        int64           `json:"id"`
	Name      string          `json:"name"`
	Type      string          `json:"type"` // "pushover" | "telegram"
	Config    json.RawMessage `json:"config"`
	Active    bool            `json:"active"`
	CreatedAt time.Time       `json:"created_at"`
	UpdatedAt time.Time       `json:"updated_at"`
}

// NotificationChannel stores delivery configuration and a message template
// for a specific notification_provider. Multiple channels may share one provider
// (e.g. two Telegram channels to different chat IDs on the same bot).
//
// MessageTemplate supports ${variable} substitution at execution time.
// Available variables: ${tweet}, ${author}, ${tweet_id}, ${tweet_url},
// plus any key from the action execution's mergedCtx.
type NotificationChannel struct {
	ID              int64           `json:"id"`
	Name            string          `json:"name"`
	ProviderID      int64           `json:"provider_id"`
	MessageTemplate string          `json:"message_template"`
	Config          json.RawMessage `json:"config"`
	Active          bool            `json:"active"`
	CreatedAt       time.Time       `json:"created_at"`
	UpdatedAt       time.Time       `json:"updated_at"`
}
```

- [ ] **Step 2: Add PipelineTypeNotification to model/pipeline.go**

In `internal/model/pipeline.go`, add to the const block:

```go
const (
	PipelineTypeONNX         PipelineType = "onnx"
	PipelineTypeOllama       PipelineType = "ollama"
	PipelineTypeInternal     PipelineType = "internal"
	PipelineTypeNotification PipelineType = "notification"
)
```

- [ ] **Step 3: Add PipelineTypeNotification to inference/pipeline.go**

In `internal/inference/pipeline.go`, add to the const block:

```go
const (
	PipelineTypeONNX         PipelineType = "onnx"
	PipelineTypeOllama       PipelineType = "ollama"
	PipelineTypeInternal     PipelineType = "internal"
	PipelineTypeNotification PipelineType = "notification"
)
```

- [ ] **Step 4: Commit**

```bash
git add internal/model/notification.go internal/model/pipeline.go internal/inference/pipeline.go
git commit -m "feat: add NotificationProvider, NotificationChannel models and PipelineTypeNotification"
```

---

## Task 3: Store CRUD + Interface

**Files:**
- Create: `internal/store/sqlite_notification_providers.go`
- Modify: `internal/store/store.go`

- [ ] **Step 1: Write failing tests first**

Create `internal/store/sqlite_notification_test.go` (full replacement):

```go
package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/amir-the-h/iran-watcher/internal/model"
)

func sampleProvider() *model.NotificationProvider {
	return &model.NotificationProvider{
		Name:      "my-pushover",
		Type:      "pushover",
		Config:    json.RawMessage(`{"app_token":"tok","user_key":"key"}`),
		Active:    true,
		CreatedAt: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
		UpdatedAt: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
	}
}

func sampleChannel(providerID int64) *model.NotificationChannel {
	return &model.NotificationChannel{
		Name:            "alert-channel",
		ProviderID:      providerID,
		MessageTemplate: "Tweet by ${author}: ${tweet}",
		Config:          json.RawMessage(`{"priority":1,"title":"Alert"}`),
		Active:          true,
		CreatedAt:       time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
		UpdatedAt:       time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
	}
}

// --- Provider tests ---

func TestNotificationProvider_SaveGet(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)

	p := sampleProvider()
	if err := s.SaveNotificationProvider(ctx, p); err != nil {
		t.Fatalf("SaveNotificationProvider: %v", err)
	}
	if p.ID == 0 {
		t.Fatal("expected non-zero ID after save")
	}

	got, err := s.GetNotificationProvider(ctx, p.ID)
	if err != nil {
		t.Fatalf("GetNotificationProvider: %v", err)
	}
	if got.Name != p.Name {
		t.Errorf("Name: got %q, want %q", got.Name, p.Name)
	}
	if got.Type != "pushover" {
		t.Errorf("Type: got %q, want pushover", got.Type)
	}
	if string(got.Config) != string(p.Config) {
		t.Errorf("Config: got %s, want %s", got.Config, p.Config)
	}
	if !got.Active {
		t.Error("expected Active=true")
	}
}

func TestNotificationProvider_GetNotFound(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)

	_, err := s.GetNotificationProvider(ctx, 999)
	if !errors.Is(err, sql.ErrNoRows) {
		t.Errorf("expected sql.ErrNoRows, got: %v", err)
	}
}

func TestNotificationProvider_List(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)

	p1 := sampleProvider()
	p1.Active = true
	if err := s.SaveNotificationProvider(ctx, p1); err != nil {
		t.Fatalf("save p1: %v", err)
	}
	p2 := sampleProvider()
	p2.Name = "my-telegram"
	p2.Type = "telegram"
	p2.Active = false
	if err := s.SaveNotificationProvider(ctx, p2); err != nil {
		t.Fatalf("save p2: %v", err)
	}

	all, err := s.ListNotificationProviders(ctx)
	if err != nil {
		t.Fatalf("ListNotificationProviders: %v", err)
	}
	if len(all) != 2 {
		t.Errorf("expected 2 providers, got %d", len(all))
	}
}

func TestNotificationProvider_Update(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)

	p := sampleProvider()
	if err := s.SaveNotificationProvider(ctx, p); err != nil {
		t.Fatalf("save: %v", err)
	}

	p.Name = "updated"
	p.Config = json.RawMessage(`{"app_token":"new","user_key":"new"}`)
	p.UpdatedAt = time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC)
	if err := s.UpdateNotificationProvider(ctx, p); err != nil {
		t.Fatalf("UpdateNotificationProvider: %v", err)
	}

	got, _ := s.GetNotificationProvider(ctx, p.ID)
	if got.Name != "updated" {
		t.Errorf("Name: got %q, want updated", got.Name)
	}
}

func TestNotificationProvider_Delete(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)

	p := sampleProvider()
	if err := s.SaveNotificationProvider(ctx, p); err != nil {
		t.Fatalf("save: %v", err)
	}
	if err := s.DeleteNotificationProvider(ctx, p.ID); err != nil {
		t.Fatalf("delete: %v", err)
	}
	_, err := s.GetNotificationProvider(ctx, p.ID)
	if !errors.Is(err, sql.ErrNoRows) {
		t.Errorf("expected sql.ErrNoRows after delete, got: %v", err)
	}
}

func TestNotificationProvider_SetActive(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)

	p := sampleProvider()
	p.Active = true
	if err := s.SaveNotificationProvider(ctx, p); err != nil {
		t.Fatalf("save: %v", err)
	}
	if err := s.SetNotificationProviderActive(ctx, p.ID, false); err != nil {
		t.Fatalf("SetNotificationProviderActive: %v", err)
	}
	got, _ := s.GetNotificationProvider(ctx, p.ID)
	if got.Active {
		t.Error("expected Active=false")
	}
}

// --- Channel tests ---

func TestNotificationChannel_SaveGet(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)

	p := sampleProvider()
	if err := s.SaveNotificationProvider(ctx, p); err != nil {
		t.Fatalf("save provider: %v", err)
	}

	ch := sampleChannel(p.ID)
	if err := s.SaveNotificationChannel(ctx, ch); err != nil {
		t.Fatalf("SaveNotificationChannel: %v", err)
	}
	if ch.ID == 0 {
		t.Fatal("expected non-zero ID")
	}

	got, err := s.GetNotificationChannel(ctx, ch.ID)
	if err != nil {
		t.Fatalf("GetNotificationChannel: %v", err)
	}
	if got.Name != ch.Name {
		t.Errorf("Name: got %q, want %q", got.Name, ch.Name)
	}
	if got.ProviderID != p.ID {
		t.Errorf("ProviderID: got %d, want %d", got.ProviderID, p.ID)
	}
	if got.MessageTemplate != ch.MessageTemplate {
		t.Errorf("MessageTemplate: got %q, want %q", got.MessageTemplate, ch.MessageTemplate)
	}
}

func TestNotificationChannel_GetNotFound(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)

	_, err := s.GetNotificationChannel(ctx, 999)
	if !errors.Is(err, sql.ErrNoRows) {
		t.Errorf("expected sql.ErrNoRows, got: %v", err)
	}
}

func TestNotificationChannel_ListByProvider(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)

	p1 := sampleProvider()
	if err := s.SaveNotificationProvider(ctx, p1); err != nil {
		t.Fatalf("save p1: %v", err)
	}
	p2 := sampleProvider()
	p2.Name = "p2"
	if err := s.SaveNotificationProvider(ctx, p2); err != nil {
		t.Fatalf("save p2: %v", err)
	}

	ch1 := sampleChannel(p1.ID)
	if err := s.SaveNotificationChannel(ctx, ch1); err != nil {
		t.Fatalf("save ch1: %v", err)
	}
	ch2 := sampleChannel(p2.ID)
	ch2.Name = "ch2"
	if err := s.SaveNotificationChannel(ctx, ch2); err != nil {
		t.Fatalf("save ch2: %v", err)
	}

	// Filter by provider
	results, err := s.ListNotificationChannels(ctx, &p1.ID)
	if err != nil {
		t.Fatalf("ListNotificationChannels: %v", err)
	}
	if len(results) != 1 || results[0].ID != ch1.ID {
		t.Errorf("expected 1 channel for p1, got %d", len(results))
	}

	// All channels
	all, err := s.ListNotificationChannels(ctx, nil)
	if err != nil {
		t.Fatalf("ListNotificationChannels all: %v", err)
	}
	if len(all) != 2 {
		t.Errorf("expected 2 channels total, got %d", len(all))
	}
}

func TestNotificationChannel_Update(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)

	p := sampleProvider()
	_ = s.SaveNotificationProvider(ctx, p)
	ch := sampleChannel(p.ID)
	_ = s.SaveNotificationChannel(ctx, ch)

	ch.MessageTemplate = "updated: ${tweet}"
	ch.Config = json.RawMessage(`{"priority":2}`)
	if err := s.UpdateNotificationChannel(ctx, ch); err != nil {
		t.Fatalf("UpdateNotificationChannel: %v", err)
	}
	got, _ := s.GetNotificationChannel(ctx, ch.ID)
	if got.MessageTemplate != "updated: ${tweet}" {
		t.Errorf("MessageTemplate: got %q", got.MessageTemplate)
	}
}

func TestNotificationChannel_Delete(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)

	p := sampleProvider()
	_ = s.SaveNotificationProvider(ctx, p)
	ch := sampleChannel(p.ID)
	_ = s.SaveNotificationChannel(ctx, ch)

	if err := s.DeleteNotificationChannel(ctx, ch.ID); err != nil {
		t.Fatalf("DeleteNotificationChannel: %v", err)
	}
	_, err := s.GetNotificationChannel(ctx, ch.ID)
	if !errors.Is(err, sql.ErrNoRows) {
		t.Errorf("expected sql.ErrNoRows after delete, got: %v", err)
	}
}

func TestNotificationChannel_SetActive(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)

	p := sampleProvider()
	_ = s.SaveNotificationProvider(ctx, p)
	ch := sampleChannel(p.ID)
	ch.Active = true
	_ = s.SaveNotificationChannel(ctx, ch)

	if err := s.SetNotificationChannelActive(ctx, ch.ID, false); err != nil {
		t.Fatalf("SetNotificationChannelActive: %v", err)
	}
	got, _ := s.GetNotificationChannel(ctx, ch.ID)
	if got.Active {
		t.Error("expected Active=false")
	}
}

func TestNotificationChannel_DeleteProvider_CascadesChannel(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)

	p := sampleProvider()
	_ = s.SaveNotificationProvider(ctx, p)
	ch := sampleChannel(p.ID)
	_ = s.SaveNotificationChannel(ctx, ch)

	_ = s.DeleteNotificationProvider(ctx, p.ID)

	_, err := s.GetNotificationChannel(ctx, ch.ID)
	if !errors.Is(err, sql.ErrNoRows) {
		t.Errorf("expected channel to be cascade-deleted, got: %v", err)
	}
}
```

- [ ] **Step 2: Run tests — expect compile error (methods not yet implemented)**

```bash
go test ./internal/store/... 2>&1 | head -30
```
Expected: compile errors about missing `SaveNotificationProvider`, `GetNotificationProvider`, etc.

- [ ] **Step 3: Update Store interface in internal/store/store.go**

Remove the old `Notifications` section:
```go
// Notifications
SaveNotification(ctx context.Context, n *model.Notification) error
GetNotification(ctx context.Context, id int64) (*model.Notification, error)
ListNotifications(ctx context.Context, filter NotificationFilter) ([]*model.Notification, error)
UpdateNotification(ctx context.Context, n *model.Notification) error
DeleteNotification(ctx context.Context, id int64) error
SetNotificationActive(ctx context.Context, id int64, active bool) error
```

Replace with:
```go
// NotificationProviders
SaveNotificationProvider(ctx context.Context, p *model.NotificationProvider) error
GetNotificationProvider(ctx context.Context, id int64) (*model.NotificationProvider, error)
ListNotificationProviders(ctx context.Context) ([]*model.NotificationProvider, error)
UpdateNotificationProvider(ctx context.Context, p *model.NotificationProvider) error
DeleteNotificationProvider(ctx context.Context, id int64) error
SetNotificationProviderActive(ctx context.Context, id int64, active bool) error

// NotificationChannels
SaveNotificationChannel(ctx context.Context, c *model.NotificationChannel) error
GetNotificationChannel(ctx context.Context, id int64) (*model.NotificationChannel, error)
ListNotificationChannels(ctx context.Context, providerID *int64) ([]*model.NotificationChannel, error)
UpdateNotificationChannel(ctx context.Context, c *model.NotificationChannel) error
DeleteNotificationChannel(ctx context.Context, id int64) error
SetNotificationChannelActive(ctx context.Context, id int64, active bool) error
```

Also remove `NotificationFilter` from `store.go` (the struct definition near the top).

- [ ] **Step 4: Create internal/store/sqlite_notification_providers.go**

```go
package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/amir-the-h/iran-watcher/internal/model"
)

// ── NotificationProviders ─────────────────────────────────────────────────────

func (s *sqliteStore) SaveNotificationProvider(ctx context.Context, p *model.NotificationProvider) error {
	res, err := s.db.ExecContext(ctx,
		`INSERT INTO notification_providers (name, type, config, active, created_at, updated_at)
		 VALUES (?, ?, ?, ?, ?, ?)`,
		p.Name, p.Type, configJSON(p.Config), boolInt(p.Active),
		p.CreatedAt.UTC().Format(time.RFC3339),
		p.UpdatedAt.UTC().Format(time.RFC3339),
	)
	if err != nil {
		return fmt.Errorf("store: save notification provider: %w", err)
	}
	id, _ := res.LastInsertId()
	p.ID = id
	return nil
}

func (s *sqliteStore) GetNotificationProvider(ctx context.Context, id int64) (*model.NotificationProvider, error) {
	row := s.db.QueryRowContext(ctx,
		`SELECT id, name, type, config, active, created_at, updated_at
		 FROM notification_providers WHERE id = ?`, id)
	p, err := scanNotificationProvider(row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, fmt.Errorf("store: get notification provider: %w", err)
	}
	if err != nil {
		return nil, fmt.Errorf("store: get notification provider: %w", err)
	}
	return p, nil
}

func (s *sqliteStore) ListNotificationProviders(ctx context.Context) ([]*model.NotificationProvider, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, name, type, config, active, created_at, updated_at
		 FROM notification_providers ORDER BY id`)
	if err != nil {
		return nil, fmt.Errorf("store: list notification providers: %w", err)
	}
	defer rows.Close()
	var out []*model.NotificationProvider
	for rows.Next() {
		p, err := scanNotificationProvider(rows)
		if err != nil {
			return nil, fmt.Errorf("store: scan notification provider: %w", err)
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

func (s *sqliteStore) UpdateNotificationProvider(ctx context.Context, p *model.NotificationProvider) error {
	_, err := s.db.ExecContext(ctx,
		`UPDATE notification_providers SET name=?, type=?, config=?, active=?, updated_at=? WHERE id=?`,
		p.Name, p.Type, configJSON(p.Config), boolInt(p.Active),
		p.UpdatedAt.UTC().Format(time.RFC3339), p.ID,
	)
	if err != nil {
		return fmt.Errorf("store: update notification provider: %w", err)
	}
	return nil
}

func (s *sqliteStore) DeleteNotificationProvider(ctx context.Context, id int64) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM notification_providers WHERE id = ?`, id)
	if err != nil {
		return fmt.Errorf("store: delete notification provider: %w", err)
	}
	return nil
}

func (s *sqliteStore) SetNotificationProviderActive(ctx context.Context, id int64, active bool) error {
	_, err := s.db.ExecContext(ctx,
		`UPDATE notification_providers SET active=?, updated_at=? WHERE id=?`,
		boolInt(active), time.Now().UTC().Format(time.RFC3339), id,
	)
	if err != nil {
		return fmt.Errorf("store: set notification provider active: %w", err)
	}
	return nil
}

func scanNotificationProvider(s scanner) (*model.NotificationProvider, error) {
	var p model.NotificationProvider
	var activeInt int
	var cfg string
	var createdAt, updatedAt string
	if err := s.Scan(&p.ID, &p.Name, &p.Type, &cfg, &activeInt, &createdAt, &updatedAt); err != nil {
		return nil, err
	}
	p.Active = activeInt != 0
	p.Config = []byte(cfg)
	p.CreatedAt, _ = parseTime(createdAt)
	p.UpdatedAt, _ = parseTime(updatedAt)
	return &p, nil
}

// ── NotificationChannels ──────────────────────────────────────────────────────

func (s *sqliteStore) SaveNotificationChannel(ctx context.Context, c *model.NotificationChannel) error {
	res, err := s.db.ExecContext(ctx,
		`INSERT INTO notification_channels (name, provider_id, message_template, config, active, created_at, updated_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?)`,
		c.Name, c.ProviderID, c.MessageTemplate, configJSON(c.Config), boolInt(c.Active),
		c.CreatedAt.UTC().Format(time.RFC3339),
		c.UpdatedAt.UTC().Format(time.RFC3339),
	)
	if err != nil {
		return fmt.Errorf("store: save notification channel: %w", err)
	}
	id, _ := res.LastInsertId()
	c.ID = id
	return nil
}

func (s *sqliteStore) GetNotificationChannel(ctx context.Context, id int64) (*model.NotificationChannel, error) {
	row := s.db.QueryRowContext(ctx,
		`SELECT id, name, provider_id, message_template, config, active, created_at, updated_at
		 FROM notification_channels WHERE id = ?`, id)
	ch, err := scanNotificationChannel(row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, fmt.Errorf("store: get notification channel: %w", err)
	}
	if err != nil {
		return nil, fmt.Errorf("store: get notification channel: %w", err)
	}
	return ch, nil
}

func (s *sqliteStore) ListNotificationChannels(ctx context.Context, providerID *int64) ([]*model.NotificationChannel, error) {
	q := `SELECT id, name, provider_id, message_template, config, active, created_at, updated_at
	      FROM notification_channels`
	var args []any
	if providerID != nil {
		q += ` WHERE provider_id = ?`
		args = append(args, *providerID)
	}
	q += ` ORDER BY id`

	rows, err := s.db.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, fmt.Errorf("store: list notification channels: %w", err)
	}
	defer rows.Close()
	var out []*model.NotificationChannel
	for rows.Next() {
		ch, err := scanNotificationChannel(rows)
		if err != nil {
			return nil, fmt.Errorf("store: scan notification channel: %w", err)
		}
		out = append(out, ch)
	}
	return out, rows.Err()
}

func (s *sqliteStore) UpdateNotificationChannel(ctx context.Context, c *model.NotificationChannel) error {
	_, err := s.db.ExecContext(ctx,
		`UPDATE notification_channels SET name=?, message_template=?, config=?, active=?, updated_at=? WHERE id=?`,
		c.Name, c.MessageTemplate, configJSON(c.Config), boolInt(c.Active),
		c.UpdatedAt.UTC().Format(time.RFC3339), c.ID,
	)
	if err != nil {
		return fmt.Errorf("store: update notification channel: %w", err)
	}
	return nil
}

func (s *sqliteStore) DeleteNotificationChannel(ctx context.Context, id int64) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM notification_channels WHERE id = ?`, id)
	if err != nil {
		return fmt.Errorf("store: delete notification channel: %w", err)
	}
	return nil
}

func (s *sqliteStore) SetNotificationChannelActive(ctx context.Context, id int64, active bool) error {
	_, err := s.db.ExecContext(ctx,
		`UPDATE notification_channels SET active=?, updated_at=? WHERE id=?`,
		boolInt(active), time.Now().UTC().Format(time.RFC3339), id,
	)
	if err != nil {
		return fmt.Errorf("store: set notification channel active: %w", err)
	}
	return nil
}

func scanNotificationChannel(s scanner) (*model.NotificationChannel, error) {
	var c model.NotificationChannel
	var activeInt int
	var cfg string
	var createdAt, updatedAt string
	if err := s.Scan(&c.ID, &c.Name, &c.ProviderID, &c.MessageTemplate, &cfg, &activeInt, &createdAt, &updatedAt); err != nil {
		return nil, err
	}
	c.Active = activeInt != 0
	c.Config = []byte(cfg)
	c.CreatedAt, _ = parseTime(createdAt)
	c.UpdatedAt, _ = parseTime(updatedAt)
	return &c, nil
}
```

> **Note:** `configJSON`, `boolInt`, `parseTime`, and `scanner` are helpers already defined in `sqlite.go`. Verify they exist; if `configJSON` is not present, use `string(p.Config)` inline, and add a helper at the bottom of the new file:
> ```go
> func configJSON(raw json.RawMessage) string {
>     if len(raw) == 0 {
>         return "{}"
>     }
>     return string(raw)
> }
> ```

- [ ] **Step 5: Run tests**

```bash
go test ./internal/store/... -run TestNotificationProvider -v
go test ./internal/store/... -run TestNotificationChannel -v
```
Expected: all PASS.

- [ ] **Step 6: Commit**

```bash
git add internal/store/store.go internal/store/sqlite_notification_providers.go internal/store/sqlite_notification_test.go
git commit -m "feat: add notification provider/channel store CRUD"
```

---

## Task 4: Provider Interface + Client Deliver Methods

**Files:**
- Create: `internal/notifier/provider.go`
- Modify: `internal/notifier/pushover.go`
- Modify: `internal/notifier/telegram.go`

- [ ] **Step 1: Create internal/notifier/provider.go**

```go
package notifier

import "context"

// Provider sends a notification message via a specific backend.
// text is the rendered message body; config contains provider-specific
// delivery settings read from the NotificationChannel.Config JSON.
type Provider interface {
	Deliver(ctx context.Context, text string, config map[string]any) error
}
```

- [ ] **Step 2: Add Deliver to PushoverClient in internal/notifier/pushover.go**

Append at the end of the file:

```go
// Deliver implements notifier.Provider.
// It maps text and config keys to a PushoverMessage and calls Send.
func (c *PushoverClient) Deliver(ctx context.Context, text string, config map[string]any) error {
	msg := PushoverMessage{Message: text}
	if v, ok := config["title"].(string); ok {
		msg.Title = v
	}
	if v, ok := config["priority"].(float64); ok {
		msg.Priority = int(v)
	}
	if v, ok := config["sound"].(string); ok {
		msg.Sound = v
	}
	if v, ok := config["device"].(string); ok {
		msg.Device = v
	}
	if v, ok := config["url"].(string); ok {
		msg.URL = v
	}
	if v, ok := config["url_title"].(string); ok {
		msg.URLTitle = v
	}
	if v, ok := config["ttl"].(float64); ok {
		msg.TTL = int(v)
	}
	if v, ok := config["html"].(bool); ok {
		msg.HTML = v
	}
	if v, ok := config["retry"].(float64); ok {
		msg.Retry = int(v)
	}
	if v, ok := config["expire"].(float64); ok {
		msg.Expire = int(v)
	}
	return c.Send(ctx, msg)
}
```

- [ ] **Step 3: Add Deliver to TelegramClient in internal/notifier/telegram.go**

Append at the end of the file:

```go
// Deliver implements notifier.Provider.
// It maps text and config keys to a TelegramMessage and calls Send.
func (c *TelegramClient) Deliver(ctx context.Context, text string, config map[string]any) error {
	chatID, _ := config["chat_id"].(string)
	if chatID == "" {
		return fmt.Errorf("notifier: telegram: chat_id missing from channel config")
	}
	msg := TelegramMessage{ChatID: chatID, Text: text}
	if v, ok := config["parse_mode"].(string); ok {
		msg.ParseMode = v
	}
	if v, ok := config["disable_web_page_preview"].(bool); ok {
		msg.DisableWebPagePreview = v
	}
	if v, ok := config["disable_notification"].(bool); ok {
		msg.DisableNotification = v
	}
	if v, ok := config["message_thread_id"].(float64); ok {
		msg.MessageThreadID = int64(v)
	}
	return c.Send(ctx, msg)
}
```

- [ ] **Step 4: Verify both clients satisfy the interface**

```bash
go build ./internal/notifier/...
```
Expected: no errors.

- [ ] **Step 5: Commit**

```bash
git add internal/notifier/provider.go internal/notifier/pushover.go internal/notifier/telegram.go
git commit -m "feat: add Provider interface and Deliver methods to notifier clients"
```

---

## Task 5: NotificationPipeline

**Files:**
- Rewrite: `internal/pipeline/notification.go`
- Create: `internal/pipeline/notification_test.go`

- [ ] **Step 1: Write failing tests**

Create `internal/pipeline/notification_test.go`:

```go
package pipeline_test

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"

	"github.com/amir-the-h/iran-watcher/internal/inference"
	"github.com/amir-the-h/iran-watcher/internal/model"
	"github.com/amir-the-h/iran-watcher/internal/pipeline"
)

type stubProvider struct {
	sentText   string
	sentConfig map[string]any
	returnErr  error
}

func (s *stubProvider) Deliver(_ context.Context, text string, config map[string]any) error {
	s.sentText = text
	s.sentConfig = config
	return s.returnErr
}

func TestNotificationPipeline_SubstitutesVariables(t *testing.T) {
	prov := &stubProvider{}
	ch := &model.NotificationChannel{
		MessageTemplate: "Tweet by ${author}: ${tweet}",
		Config:          json.RawMessage(`{"priority":1}`),
	}
	p := pipeline.NewNotificationPipeline("test-notif", ch, prov)

	out, err := p.Run(context.Background(), inference.PipelineInput{
		Text: "ignored",
		Context: map[string]any{
			"tweet_id": "123",
			"author":   "bbcpersian",
			"tweet":    "Breaking news",
		},
	})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if prov.sentText != "Tweet by bbcpersian: Breaking news" {
		t.Errorf("text: got %q", prov.sentText)
	}
	if out["sent"] != true {
		t.Error("expected sent=true in outputs")
	}
}

func TestNotificationPipeline_UnknownVarLeftAsIs(t *testing.T) {
	prov := &stubProvider{}
	ch := &model.NotificationChannel{
		MessageTemplate: "Hello ${unknown_var}",
		Config:          json.RawMessage(`{}`),
	}
	p := pipeline.NewNotificationPipeline("test", ch, prov)

	_, err := p.Run(context.Background(), inference.PipelineInput{
		Context: map[string]any{"tweet_id": "1"},
	})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if prov.sentText != "Hello ${unknown_var}" {
		t.Errorf("expected unknown var left as-is, got %q", prov.sentText)
	}
}

func TestNotificationPipeline_Type(t *testing.T) {
	ch := &model.NotificationChannel{}
	p := pipeline.NewNotificationPipeline("n", ch, &stubProvider{})
	if p.Type() != inference.PipelineTypeNotification {
		t.Errorf("expected PipelineTypeNotification, got %q", p.Type())
	}
}

func TestNotificationPipeline_ProviderError(t *testing.T) {
	prov := &stubProvider{returnErr: fmt.Errorf("API down")}
	ch := &model.NotificationChannel{
		MessageTemplate: "hello",
		Config:          json.RawMessage(`{}`),
	}
	p := pipeline.NewNotificationPipeline("n", ch, prov)
	_, err := p.Run(context.Background(), inference.PipelineInput{
		Context: map[string]any{},
	})
	if err == nil {
		t.Fatal("expected error from provider, got nil")
	}
}

func TestNotificationPipeline_NilConfig(t *testing.T) {
	prov := &stubProvider{}
	ch := &model.NotificationChannel{
		MessageTemplate: "hi",
		Config:          nil,
	}
	p := pipeline.NewNotificationPipeline("n", ch, prov)
	_, err := p.Run(context.Background(), inference.PipelineInput{Context: map[string]any{}})
	if err != nil {
		t.Fatalf("expected no error for nil config, got: %v", err)
	}
}
```

- [ ] **Step 2: Run tests — expect compile error**

```bash
go test ./internal/pipeline/... -run TestNotificationPipeline 2>&1 | head -20
```
Expected: compile error — `pipeline.NewNotificationPipeline` not found.

- [ ] **Step 3: Rewrite internal/pipeline/notification.go**

```go
package pipeline

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/amir-the-h/iran-watcher/internal/inference"
	"github.com/amir-the-h/iran-watcher/internal/model"
	"github.com/amir-the-h/iran-watcher/internal/notifier"
)

// NotificationPipeline delivers a message to a notification channel when run.
// The channel's MessageTemplate is rendered by substituting ${variable}
// placeholders with values from the task's mergedCtx (tweet fields + upstream
// pipeline outputs from action chains).
type NotificationPipeline struct {
	name     string
	channel  *model.NotificationChannel
	provider notifier.Provider
}

// NewNotificationPipeline creates a NotificationPipeline.
func NewNotificationPipeline(name string, channel *model.NotificationChannel, provider notifier.Provider) *NotificationPipeline {
	return &NotificationPipeline{name: name, channel: channel, provider: provider}
}

func (p *NotificationPipeline) Name() string                 { return p.name }
func (p *NotificationPipeline) Type() inference.PipelineType { return inference.PipelineTypeNotification }
func (p *NotificationPipeline) Close() error                 { return nil }

// Run renders the channel template, reads delivery config, and calls the provider.
// Returns {"sent": true} on success so downstream chains can condition on it.
func (p *NotificationPipeline) Run(ctx context.Context, input inference.PipelineInput) (map[string]any, error) {
	text := substituteVars(p.channel.MessageTemplate, input.Context)

	var cfg map[string]any
	if len(p.channel.Config) > 0 {
		if err := json.Unmarshal(p.channel.Config, &cfg); err != nil {
			cfg = make(map[string]any)
		}
	} else {
		cfg = make(map[string]any)
	}

	if err := p.provider.Deliver(ctx, text, cfg); err != nil {
		return nil, fmt.Errorf("notification: deliver: %w", err)
	}

	return map[string]any{"sent": true}, nil
}

// substituteVars replaces every ${key} occurrence in tmpl with the string
// representation of the matching value from ctx. Unknown keys are left as-is.
func substituteVars(tmpl string, ctx map[string]any) string {
	for k, v := range ctx {
		tmpl = strings.ReplaceAll(tmpl, "${"+k+"}", fmt.Sprintf("%v", v))
	}
	return tmpl
}
```

- [ ] **Step 4: Run tests**

```bash
go test ./internal/pipeline/... -run TestNotificationPipeline -v
```
Expected: all PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/pipeline/notification.go internal/pipeline/notification_test.go
git commit -m "feat: add NotificationPipeline with template variable substitution"
```

---

## Task 6: ActionCache — Notification Pipeline Support

**Files:**
- Modify: `internal/worker/action_cache.go`

- [ ] **Step 1: Add notification case to resolvePipeline**

In `internal/worker/action_cache.go`, add these imports if not present:

```go
import (
    // existing imports...
    "log/slog"

    "github.com/amir-the-h/iran-watcher/internal/notifier"
    "github.com/amir-the-h/iran-watcher/internal/pipeline"
)
```

In `resolvePipeline`, add after the `PipelineTypeInternal` case (before `default`):

```go
case model.PipelineTypeNotification:
    channelID, _ := cfg["channel_id"].(float64)
    if channelID == 0 {
        return nil, fmt.Errorf("notification pipeline: config missing 'channel_id'")
    }
    ch, err := c.store.GetNotificationChannel(ctx, int64(channelID))
    if err != nil {
        return nil, fmt.Errorf("notification pipeline: load channel %d: %w", int64(channelID), err)
    }
    prov, err := c.store.GetNotificationProvider(ctx, ch.ProviderID)
    if err != nil {
        return nil, fmt.Errorf("notification pipeline: load provider %d: %w", ch.ProviderID, err)
    }
    client, err := buildNotificationProvider(prov, c.logger)
    if err != nil {
        return nil, fmt.Errorf("notification pipeline: build client: %w", err)
    }
    return pipeline.NewNotificationPipeline(row.Name, ch, client), nil
```

- [ ] **Step 2: Add buildNotificationProvider helper to action_cache.go**

Append at the bottom of `action_cache.go`:

```go
// buildNotificationProvider instantiates the correct notifier.Provider from
// the credentials in prov.Config.
func buildNotificationProvider(prov *model.NotificationProvider, logger *slog.Logger) (notifier.Provider, error) {
	var cfg map[string]any
	if len(prov.Config) > 0 {
		if err := json.Unmarshal(prov.Config, &cfg); err != nil {
			return nil, fmt.Errorf("unmarshal provider config: %w", err)
		}
	}
	switch prov.Type {
	case "pushover":
		appToken, _ := cfg["app_token"].(string)
		userKey, _ := cfg["user_key"].(string)
		if appToken == "" || userKey == "" {
			return nil, fmt.Errorf("pushover provider %d: missing app_token or user_key in config", prov.ID)
		}
		return notifier.NewPushoverClient(appToken, userKey, logger), nil
	case "telegram":
		botToken, _ := cfg["bot_token"].(string)
		if botToken == "" {
			return nil, fmt.Errorf("telegram provider %d: missing bot_token in config", prov.ID)
		}
		return notifier.NewTelegramClient(botToken, logger), nil
	default:
		return nil, fmt.Errorf("unknown notification provider type: %q", prov.Type)
	}
}
```

- [ ] **Step 3: Verify build**

```bash
go build ./internal/worker/...
```
Expected: no errors.

- [ ] **Step 4: Commit**

```bash
git add internal/worker/action_cache.go
git commit -m "feat: handle notification pipeline type in ActionCache"
```

---

## Task 7: New API Handlers

**Files:**
- Create: `internal/api/handlers_notification_providers.go`
- Create: `internal/api/handlers_notification_channels.go`

- [ ] **Step 1: Create handlers_notification_providers.go**

```go
package api

import (
	"encoding/json"
	"net/http"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/amir-the-h/iran-watcher/internal/model"
)

func (s *Server) handleListNotificationProviders(w http.ResponseWriter, r *http.Request) {
	providers, err := s.store.ListNotificationProviders(r.Context())
	if err != nil {
		s.logger.Error("api: list notification providers", "error", err)
		writeError(w, http.StatusInternalServerError, "failed to list notification providers")
		return
	}
	writeJSON(w, http.StatusOK, providers)
}

func (s *Server) handleCreateNotificationProvider(w http.ResponseWriter, r *http.Request) {
	var p model.NotificationProvider
	if err := json.NewDecoder(r.Body).Decode(&p); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if p.Name == "" {
		writeError(w, http.StatusBadRequest, "name is required")
		return
	}
	if p.Type != "pushover" && p.Type != "telegram" {
		writeError(w, http.StatusBadRequest, "type must be 'pushover' or 'telegram'")
		return
	}
	now := time.Now()
	p.Active = true
	p.CreatedAt = now
	p.UpdatedAt = now
	if err := s.store.SaveNotificationProvider(r.Context(), &p); err != nil {
		s.logger.Error("api: save notification provider", "error", err)
		writeError(w, http.StatusInternalServerError, "failed to save notification provider")
		return
	}
	writeJSON(w, http.StatusCreated, &p)
}

func (s *Server) handleGetNotificationProvider(w http.ResponseWriter, r *http.Request) {
	id, ok := parseNotificationProviderID(w, r)
	if !ok {
		return
	}
	p, err := s.store.GetNotificationProvider(r.Context(), id)
	if err != nil {
		writeError(w, http.StatusNotFound, "notification provider not found")
		return
	}
	writeJSON(w, http.StatusOK, p)
}

func (s *Server) handleUpdateNotificationProvider(w http.ResponseWriter, r *http.Request) {
	id, ok := parseNotificationProviderID(w, r)
	if !ok {
		return
	}
	p, err := s.store.GetNotificationProvider(r.Context(), id)
	if err != nil {
		writeError(w, http.StatusNotFound, "notification provider not found")
		return
	}
	if err := json.NewDecoder(r.Body).Decode(p); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if p.Type != "pushover" && p.Type != "telegram" {
		writeError(w, http.StatusBadRequest, "type must be 'pushover' or 'telegram'")
		return
	}
	p.UpdatedAt = time.Now()
	if err := s.store.UpdateNotificationProvider(r.Context(), p); err != nil {
		s.logger.Error("api: update notification provider", "id", id, "error", err)
		writeError(w, http.StatusInternalServerError, "failed to update notification provider")
		return
	}
	writeJSON(w, http.StatusOK, p)
}

func (s *Server) handleDeleteNotificationProvider(w http.ResponseWriter, r *http.Request) {
	id, ok := parseNotificationProviderID(w, r)
	if !ok {
		return
	}
	if err := s.store.DeleteNotificationProvider(r.Context(), id); err != nil {
		s.logger.Error("api: delete notification provider", "id", id, "error", err)
		writeError(w, http.StatusInternalServerError, "failed to delete notification provider")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handleActivateNotificationProvider(w http.ResponseWriter, r *http.Request) {
	id, ok := parseNotificationProviderID(w, r)
	if !ok {
		return
	}
	if err := s.store.SetNotificationProviderActive(r.Context(), id, true); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to activate notification provider")
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "activated"})
}

func (s *Server) handleDeactivateNotificationProvider(w http.ResponseWriter, r *http.Request) {
	id, ok := parseNotificationProviderID(w, r)
	if !ok {
		return
	}
	if err := s.store.SetNotificationProviderActive(r.Context(), id, false); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to deactivate notification provider")
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "deactivated"})
}

func parseNotificationProviderID(w http.ResponseWriter, r *http.Request) (int64, bool) {
	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid notification provider ID")
		return 0, false
	}
	return id, true
}
```

- [ ] **Step 2: Create handlers_notification_channels.go**

```go
package api

import (
	"encoding/json"
	"net/http"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/amir-the-h/iran-watcher/internal/model"
)

func (s *Server) handleListNotificationChannels(w http.ResponseWriter, r *http.Request) {
	var providerID *int64
	if pidStr := r.URL.Query().Get("provider_id"); pidStr != "" {
		pid, err := strconv.ParseInt(pidStr, 10, 64)
		if err != nil {
			writeError(w, http.StatusBadRequest, "invalid provider_id")
			return
		}
		providerID = &pid
	}
	channels, err := s.store.ListNotificationChannels(r.Context(), providerID)
	if err != nil {
		s.logger.Error("api: list notification channels", "error", err)
		writeError(w, http.StatusInternalServerError, "failed to list notification channels")
		return
	}
	writeJSON(w, http.StatusOK, channels)
}

func (s *Server) handleCreateNotificationChannel(w http.ResponseWriter, r *http.Request) {
	var c model.NotificationChannel
	if err := json.NewDecoder(r.Body).Decode(&c); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if c.Name == "" {
		writeError(w, http.StatusBadRequest, "name is required")
		return
	}
	if c.ProviderID == 0 {
		writeError(w, http.StatusBadRequest, "provider_id is required")
		return
	}
	// Verify provider exists.
	if _, err := s.store.GetNotificationProvider(r.Context(), c.ProviderID); err != nil {
		writeError(w, http.StatusBadRequest, "notification provider not found")
		return
	}
	now := time.Now()
	c.Active = true
	c.CreatedAt = now
	c.UpdatedAt = now
	if err := s.store.SaveNotificationChannel(r.Context(), &c); err != nil {
		s.logger.Error("api: save notification channel", "error", err)
		writeError(w, http.StatusInternalServerError, "failed to save notification channel")
		return
	}
	writeJSON(w, http.StatusCreated, &c)
}

func (s *Server) handleGetNotificationChannel(w http.ResponseWriter, r *http.Request) {
	id, ok := parseNotificationChannelID(w, r)
	if !ok {
		return
	}
	c, err := s.store.GetNotificationChannel(r.Context(), id)
	if err != nil {
		writeError(w, http.StatusNotFound, "notification channel not found")
		return
	}
	writeJSON(w, http.StatusOK, c)
}

func (s *Server) handleUpdateNotificationChannel(w http.ResponseWriter, r *http.Request) {
	id, ok := parseNotificationChannelID(w, r)
	if !ok {
		return
	}
	c, err := s.store.GetNotificationChannel(r.Context(), id)
	if err != nil {
		writeError(w, http.StatusNotFound, "notification channel not found")
		return
	}
	if err := json.NewDecoder(r.Body).Decode(c); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	c.UpdatedAt = time.Now()
	if err := s.store.UpdateNotificationChannel(r.Context(), c); err != nil {
		s.logger.Error("api: update notification channel", "id", id, "error", err)
		writeError(w, http.StatusInternalServerError, "failed to update notification channel")
		return
	}
	writeJSON(w, http.StatusOK, c)
}

func (s *Server) handleDeleteNotificationChannel(w http.ResponseWriter, r *http.Request) {
	id, ok := parseNotificationChannelID(w, r)
	if !ok {
		return
	}
	if err := s.store.DeleteNotificationChannel(r.Context(), id); err != nil {
		s.logger.Error("api: delete notification channel", "id", id, "error", err)
		writeError(w, http.StatusInternalServerError, "failed to delete notification channel")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handleActivateNotificationChannel(w http.ResponseWriter, r *http.Request) {
	id, ok := parseNotificationChannelID(w, r)
	if !ok {
		return
	}
	if err := s.store.SetNotificationChannelActive(r.Context(), id, true); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to activate notification channel")
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "activated"})
}

func (s *Server) handleDeactivateNotificationChannel(w http.ResponseWriter, r *http.Request) {
	id, ok := parseNotificationChannelID(w, r)
	if !ok {
		return
	}
	if err := s.store.SetNotificationChannelActive(r.Context(), id, false); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to deactivate notification channel")
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "deactivated"})
}

func parseNotificationChannelID(w http.ResponseWriter, r *http.Request) (int64, bool) {
	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid notification channel ID")
		return 0, false
	}
	return id, true
}
```

- [ ] **Step 3: Verify build**

```bash
go build ./internal/api/...
```
Expected: no errors (old handlers_notifications.go still present, that's fine for now).

- [ ] **Step 4: Commit**

```bash
git add internal/api/handlers_notification_providers.go internal/api/handlers_notification_channels.go
git commit -m "feat: add notification provider/channel API handlers"
```

---

## Task 8: Remove Old Notification System

**Files:**
- Delete: `internal/notifier/dispatcher.go`
- Delete: `internal/notifier/dispatcher_test.go`
- Delete: `internal/notifier/event.go`
- Delete: `internal/notifier/template.go`
- Delete: `internal/notifier/template_test.go`
- Delete: `internal/api/handlers_notifications.go`
- Modify: `internal/api/server.go` — remove `dispatcher` field/param, swap routes
- Modify: `internal/api/handlers_pipelines.go` — allow `notification` type
- Modify: `internal/api/handlers_summaries.go` — remove `emitDigestEvent`
- Modify: `internal/stream/client.go` — remove emitter
- Modify: `cli/cmd/serve.go` — remove dispatcher/emitter wiring

- [ ] **Step 1: Delete retired notifier files**

```bash
rm internal/notifier/dispatcher.go
rm internal/notifier/dispatcher_test.go
rm internal/notifier/event.go
rm internal/notifier/template.go
rm internal/notifier/template_test.go
```

- [ ] **Step 2: Remove emitter from stream/client.go**

In `internal/stream/client.go`:

1. Remove the `emitter notifier.EventEmitter` field from the `Client` struct.
2. Remove the `emitter notifier.EventEmitter` parameter from `NewClient`.
3. Remove the `emitter: emitter` line in the `NewClient` body.
4. Remove all `if c.emitter != nil { c.emitter.Emit(...) }` blocks (there are 4 of them).
5. Remove the `notifier` import (if no other notifier symbols remain — check if `notifier.EventEmitter` was the only use).

After editing, run:
```bash
go build ./internal/stream/...
```
Expected: no errors.

- [ ] **Step 3: Remove dispatcher from api/server.go**

In `internal/api/server.go`:

1. Remove `dispatcher *notifier.Dispatcher` from the `Server` struct.
2. Remove `dispatcher *notifier.Dispatcher` parameter from `NewServer`.
3. Remove `dispatcher: dispatcher` assignment in `NewServer` body.
4. Remove `dispatcher *notifier.Dispatcher` parameter from `NewServerWithConcreteRules`.
5. Remove `dispatcher` from the `NewServer(...)` call inside `NewServerWithConcreteRules`.
6. Replace the old `/api/notifications/*` route block with the new provider/channel routes:

Remove this block:
```go
// Notifications — static routes before parameterized
r.Get("/notifications", srv.handleListNotifications)
r.Post("/notifications", srv.handleCreateNotification)
r.Post("/notifications/test", srv.handleTestNotification)
r.Get("/notifications/{id}", srv.handleGetNotification)
r.Put("/notifications/{id}", srv.handleUpdateNotification)
r.Delete("/notifications/{id}", srv.handleDeleteNotification)
r.Post("/notifications/{id}/activate", srv.handleActivateNotification)
r.Post("/notifications/{id}/deactivate", srv.handleDeactivateNotification)
```

Add in its place:
```go
// Notification providers
r.Get("/notification/providers", srv.handleListNotificationProviders)
r.Post("/notification/providers", srv.handleCreateNotificationProvider)
r.Get("/notification/providers/{id}", srv.handleGetNotificationProvider)
r.Put("/notification/providers/{id}", srv.handleUpdateNotificationProvider)
r.Delete("/notification/providers/{id}", srv.handleDeleteNotificationProvider)
r.Post("/notification/providers/{id}/activate", srv.handleActivateNotificationProvider)
r.Post("/notification/providers/{id}/deactivate", srv.handleDeactivateNotificationProvider)

// Notification channels
r.Get("/notification/channels", srv.handleListNotificationChannels)
r.Post("/notification/channels", srv.handleCreateNotificationChannel)
r.Get("/notification/channels/{id}", srv.handleGetNotificationChannel)
r.Put("/notification/channels/{id}", srv.handleUpdateNotificationChannel)
r.Delete("/notification/channels/{id}", srv.handleDeleteNotificationChannel)
r.Post("/notification/channels/{id}/activate", srv.handleActivateNotificationChannel)
r.Post("/notification/channels/{id}/deactivate", srv.handleDeactivateNotificationChannel)
```

7. Remove the `notifier` import from `server.go` imports (if no other symbols remain).

- [ ] **Step 4: Remove emitDigestEvent from handlers_summaries.go**

In `internal/api/handlers_summaries.go`:

1. Remove the `emitDigestEvent` method entirely (lines ~291–307).
2. Remove all calls to `s.emitDigestEvent(...)` in `handleDigest` and similar handlers.
3. Remove `notifier` import if it's now unused.

- [ ] **Step 5: Delete handlers_notifications.go**

```bash
rm internal/api/handlers_notifications.go
```

- [ ] **Step 6: Allow 'notification' in pipeline type validation (handlers_pipelines.go)**

In `internal/api/handlers_pipelines.go`, find the type validation switch in `handleCreatePipeline`:

```go
switch p.Type {
case model.PipelineTypeONNX, model.PipelineTypeOllama, model.PipelineTypeInternal:
    // valid
default:
    writeError(w, http.StatusBadRequest, "invalid type: must be onnx, ollama, or internal")
    return
}
```

Change to:

```go
switch p.Type {
case model.PipelineTypeONNX, model.PipelineTypeOllama, model.PipelineTypeInternal, model.PipelineTypeNotification:
    // valid
default:
    writeError(w, http.StatusBadRequest, "invalid type: must be onnx, ollama, internal, or notification")
    return
}
```

- [ ] **Step 7: Remove dispatcher wiring from serve.go**

In `cli/cmd/serve.go`:

1. Remove the `pushClient` / `telegramClient` variable declarations and their config-based instantiation blocks (lines ~109–123).
2. Remove `pipeReg.Register("notification_push", ...)` and `pipeReg.Register("notification_tg", ...)` lines.
3. Remove the `dispatcher` and `emitter` variable declarations and their creation block (lines ~147–152).
4. Remove the `emitter` argument from `stream.NewClient(...)` call — update the call to match the new signature (no emitter parameter).
5. Remove the `dispatcher` argument from `api.NewServerWithConcreteRules(...)` call.
6. Remove the `if dispatcher != nil { go func() { dispatcher.Start(ctx) }() }` goroutine block (lines ~215–220).
7. Remove the `notifier` import from `serve.go` imports.

- [ ] **Step 8: Remove old notification CRUD from sqlite.go**

In `internal/store/sqlite.go`:

Find and remove these functions:
- `SaveNotification`
- `GetNotification`
- `ListNotifications`
- `UpdateNotification`
- `DeleteNotification`
- `SetNotificationActive`
- `scanNotification` helper

Also remove the `scanner` interface if it is only used by `scanNotification` — check if it's still needed by the new `scanNotificationProvider`/`scanNotificationChannel` helpers in the new file. If it's defined in `sqlite.go` and also used by new code, keep it.

- [ ] **Step 9: Update worker_test.go mockStore to match new Store interface**

In `internal/worker/worker_test.go`, find the `// Notifications` section:

```go
// Notifications
func (m *mockStore) SaveNotification(_ context.Context, _ *model.Notification) error { return nil }
func (m *mockStore) GetNotification(_ context.Context, _ int64) (*model.Notification, error) {
	return nil, nil
}
func (m *mockStore) ListNotifications(_ context.Context, _ store.NotificationFilter) ([]*model.Notification, error) {
	return nil, nil
}
func (m *mockStore) UpdateNotification(_ context.Context, _ *model.Notification) error { return nil }
func (m *mockStore) DeleteNotification(_ context.Context, _ int64) error               { return nil }
func (m *mockStore) SetNotificationActive(_ context.Context, _ int64, _ bool) error    { return nil }
```

Replace with:

```go
// NotificationProviders
func (m *mockStore) SaveNotificationProvider(_ context.Context, _ *model.NotificationProvider) error {
	return nil
}
func (m *mockStore) GetNotificationProvider(_ context.Context, _ int64) (*model.NotificationProvider, error) {
	return nil, fmt.Errorf("not found")
}
func (m *mockStore) ListNotificationProviders(_ context.Context) ([]*model.NotificationProvider, error) {
	return nil, nil
}
func (m *mockStore) UpdateNotificationProvider(_ context.Context, _ *model.NotificationProvider) error {
	return nil
}
func (m *mockStore) DeleteNotificationProvider(_ context.Context, _ int64) error            { return nil }
func (m *mockStore) SetNotificationProviderActive(_ context.Context, _ int64, _ bool) error { return nil }

// NotificationChannels
func (m *mockStore) SaveNotificationChannel(_ context.Context, _ *model.NotificationChannel) error {
	return nil
}
func (m *mockStore) GetNotificationChannel(_ context.Context, _ int64) (*model.NotificationChannel, error) {
	return nil, fmt.Errorf("not found")
}
func (m *mockStore) ListNotificationChannels(_ context.Context, _ *int64) ([]*model.NotificationChannel, error) {
	return nil, nil
}
func (m *mockStore) UpdateNotificationChannel(_ context.Context, _ *model.NotificationChannel) error {
	return nil
}
func (m *mockStore) DeleteNotificationChannel(_ context.Context, _ int64) error            { return nil }
func (m *mockStore) SetNotificationChannelActive(_ context.Context, _ int64, _ bool) error { return nil }
```

- [ ] **Step 10: Build and test**

```bash
go build ./...
go test ./...
```
Expected: build succeeds, all tests pass.

- [ ] **Step 11: Commit**

```bash
git add -A
git commit -m "feat: remove legacy notification dispatcher; wire notification pipeline system"
```

---

## Task 9: Update CLI

**Files:**
- Rewrite: `cli/cmd/notification.go`

- [ ] **Step 1: Rewrite cli/cmd/notification.go**

Replace the entire file:

```go
package cmd

import (
	"encoding/json"
	"fmt"
	"net/url"
	"text/tabwriter"

	"github.com/spf13/cobra"
)

// ── notification providers ────────────────────────────────────────────────────

var notificationCmd = &cobra.Command{
	Use:   "notification",
	Short: "Manage notification providers and channels",
}

var notifProviderCmd = &cobra.Command{
	Use:   "providers",
	Short: "Manage notification providers (credential stores)",
}

var notifProviderListCmd = &cobra.Command{
	Use:   "list",
	Short: "List notification providers",
	RunE: func(cmd *cobra.Command, args []string) error {
		resp, err := apiGet(baseURL + "/api/notification/providers")
		if err != nil {
			return fmt.Errorf("notification providers list: %w", err)
		}
		defer resp.Body.Close()
		if err := handleAPIError(cmd, resp); err != nil {
			return err
		}
		var providers []map[string]interface{}
		if err := json.NewDecoder(resp.Body).Decode(&providers); err != nil {
			return fmt.Errorf("notification providers list: decode: %w", err)
		}
		w := tabwriter.NewWriter(cmd.OutOrStdout(), 0, 0, 2, ' ', 0)
		fmt.Fprintln(w, "ID\tNAME\tTYPE\tACTIVE")
		for _, p := range providers {
			fmt.Fprintf(w, "%s\t%s\t%s\t%s\n",
				formatField(p["id"]), formatField(p["name"]),
				formatField(p["type"]), formatField(p["active"]))
		}
		return w.Flush()
	},
}

var (
	notifProviderName   string
	notifProviderType   string
	notifProviderConfig string
)

var notifProviderCreateCmd = &cobra.Command{
	Use:   "create",
	Short: "Create a notification provider",
	RunE: func(cmd *cobra.Command, args []string) error {
		var cfg interface{}
		if notifProviderConfig != "" {
			if err := json.Unmarshal([]byte(notifProviderConfig), &cfg); err != nil {
				return fmt.Errorf("notification provider create: parse config: %w", err)
			}
		}
		body := map[string]interface{}{
			"name":   notifProviderName,
			"type":   notifProviderType,
			"config": cfg,
		}
		r, err := jsonBody(body)
		if err != nil {
			return fmt.Errorf("notification provider create: %w", err)
		}
		resp, err := apiPost(baseURL+"/api/notification/providers", r)
		if err != nil {
			return fmt.Errorf("notification provider create: %w", err)
		}
		defer resp.Body.Close()
		if err := handleAPIError(cmd, resp); err != nil {
			return err
		}
		var created map[string]interface{}
		_ = json.NewDecoder(resp.Body).Decode(&created)
		fmt.Fprintf(cmd.OutOrStdout(), "Provider created: id=%s name=%s type=%s\n",
			formatField(created["id"]), formatField(created["name"]), formatField(created["type"]))
		return nil
	},
}

var notifProviderGetCmd = &cobra.Command{
	Use:  "get <id>",
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		resp, err := apiGet(baseURL + "/api/notification/providers/" + url.PathEscape(args[0]))
		if err != nil {
			return fmt.Errorf("notification provider get: %w", err)
		}
		defer resp.Body.Close()
		if err := handleAPIError(cmd, resp); err != nil {
			return err
		}
		var result interface{}
		_ = json.NewDecoder(resp.Body).Decode(&result)
		out, _ := json.MarshalIndent(result, "", "  ")
		fmt.Fprintln(cmd.OutOrStdout(), string(out))
		return nil
	},
}

var notifProviderDeleteCmd = &cobra.Command{
	Use:  "delete <id>",
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		resp, err := apiDelete(baseURL + "/api/notification/providers/" + url.PathEscape(args[0]))
		if err != nil {
			return fmt.Errorf("notification provider delete: %w", err)
		}
		defer resp.Body.Close()
		if err := handleAPIError(cmd, resp); err != nil {
			return err
		}
		fmt.Fprintf(cmd.OutOrStdout(), "Provider %s deleted\n", args[0])
		return nil
	},
}

var notifProviderActivateCmd = &cobra.Command{
	Use:  "activate <id>",
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		resp, err := apiPost(baseURL+"/api/notification/providers/"+url.PathEscape(args[0])+"/activate", nil)
		if err != nil {
			return fmt.Errorf("notification provider activate: %w", err)
		}
		defer resp.Body.Close()
		if err := handleAPIError(cmd, resp); err != nil {
			return err
		}
		fmt.Fprintf(cmd.OutOrStdout(), "Provider %s activated\n", args[0])
		return nil
	},
}

var notifProviderDeactivateCmd = &cobra.Command{
	Use:  "deactivate <id>",
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		resp, err := apiPost(baseURL+"/api/notification/providers/"+url.PathEscape(args[0])+"/deactivate", nil)
		if err != nil {
			return fmt.Errorf("notification provider deactivate: %w", err)
		}
		defer resp.Body.Close()
		if err := handleAPIError(cmd, resp); err != nil {
			return err
		}
		fmt.Fprintf(cmd.OutOrStdout(), "Provider %s deactivated\n", args[0])
		return nil
	},
}

// ── notification channels ─────────────────────────────────────────────────────

var notifChannelCmd = &cobra.Command{
	Use:   "channels",
	Short: "Manage notification channels (delivery config + template)",
}

var notifChannelListCmd = &cobra.Command{
	Use:   "list",
	Short: "List notification channels",
	RunE: func(cmd *cobra.Command, args []string) error {
		u, _ := url.Parse(baseURL + "/api/notification/channels")
		q := u.Query()
		if notifChannelProviderID != "" {
			q.Set("provider_id", notifChannelProviderID)
		}
		u.RawQuery = q.Encode()
		resp, err := apiGet(u.String())
		if err != nil {
			return fmt.Errorf("notification channels list: %w", err)
		}
		defer resp.Body.Close()
		if err := handleAPIError(cmd, resp); err != nil {
			return err
		}
		var channels []map[string]interface{}
		if err := json.NewDecoder(resp.Body).Decode(&channels); err != nil {
			return fmt.Errorf("notification channels list: decode: %w", err)
		}
		w := tabwriter.NewWriter(cmd.OutOrStdout(), 0, 0, 2, ' ', 0)
		fmt.Fprintln(w, "ID\tNAME\tPROVIDER_ID\tACTIVE\tTEMPLATE")
		for _, c := range channels {
			fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\n",
				formatField(c["id"]), formatField(c["name"]),
				formatField(c["provider_id"]), formatField(c["active"]),
				formatField(c["message_template"]))
		}
		return w.Flush()
	},
}

var (
	notifChannelName       string
	notifChannelProviderID string
	notifChannelTemplate   string
	notifChannelConfig     string
)

var notifChannelCreateCmd = &cobra.Command{
	Use:   "create",
	Short: "Create a notification channel",
	RunE: func(cmd *cobra.Command, args []string) error {
		var cfg interface{}
		if notifChannelConfig != "" {
			if err := json.Unmarshal([]byte(notifChannelConfig), &cfg); err != nil {
				return fmt.Errorf("notification channel create: parse config: %w", err)
			}
		}
		body := map[string]interface{}{
			"name":             notifChannelName,
			"provider_id":      notifChannelProviderID,
			"message_template": notifChannelTemplate,
			"config":           cfg,
		}
		r, err := jsonBody(body)
		if err != nil {
			return fmt.Errorf("notification channel create: %w", err)
		}
		resp, err := apiPost(baseURL+"/api/notification/channels", r)
		if err != nil {
			return fmt.Errorf("notification channel create: %w", err)
		}
		defer resp.Body.Close()
		if err := handleAPIError(cmd, resp); err != nil {
			return err
		}
		var created map[string]interface{}
		_ = json.NewDecoder(resp.Body).Decode(&created)
		fmt.Fprintf(cmd.OutOrStdout(), "Channel created: id=%s name=%s provider_id=%s\n",
			formatField(created["id"]), formatField(created["name"]),
			formatField(created["provider_id"]))
		return nil
	},
}

var notifChannelGetCmd = &cobra.Command{
	Use:  "get <id>",
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		resp, err := apiGet(baseURL + "/api/notification/channels/" + url.PathEscape(args[0]))
		if err != nil {
			return fmt.Errorf("notification channel get: %w", err)
		}
		defer resp.Body.Close()
		if err := handleAPIError(cmd, resp); err != nil {
			return err
		}
		var result interface{}
		_ = json.NewDecoder(resp.Body).Decode(&result)
		out, _ := json.MarshalIndent(result, "", "  ")
		fmt.Fprintln(cmd.OutOrStdout(), string(out))
		return nil
	},
}

var notifChannelDeleteCmd = &cobra.Command{
	Use:  "delete <id>",
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		resp, err := apiDelete(baseURL + "/api/notification/channels/" + url.PathEscape(args[0]))
		if err != nil {
			return fmt.Errorf("notification channel delete: %w", err)
		}
		defer resp.Body.Close()
		if err := handleAPIError(cmd, resp); err != nil {
			return err
		}
		fmt.Fprintf(cmd.OutOrStdout(), "Channel %s deleted\n", args[0])
		return nil
	},
}

var notifChannelActivateCmd = &cobra.Command{
	Use:  "activate <id>",
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		resp, err := apiPost(baseURL+"/api/notification/channels/"+url.PathEscape(args[0])+"/activate", nil)
		if err != nil {
			return fmt.Errorf("notification channel activate: %w", err)
		}
		defer resp.Body.Close()
		if err := handleAPIError(cmd, resp); err != nil {
			return err
		}
		fmt.Fprintf(cmd.OutOrStdout(), "Channel %s activated\n", args[0])
		return nil
	},
}

var notifChannelDeactivateCmd = &cobra.Command{
	Use:  "deactivate <id>",
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		resp, err := apiPost(baseURL+"/api/notification/channels/"+url.PathEscape(args[0])+"/deactivate", nil)
		if err != nil {
			return fmt.Errorf("notification channel deactivate: %w", err)
		}
		defer resp.Body.Close()
		if err := handleAPIError(cmd, resp); err != nil {
			return err
		}
		fmt.Fprintf(cmd.OutOrStdout(), "Channel %s deactivated\n", args[0])
		return nil
	},
}

func init() {
	// Provider flags
	notifProviderCreateCmd.Flags().StringVar(&notifProviderName, "name", "", "Provider name")
	notifProviderCreateCmd.Flags().StringVar(&notifProviderType, "type", "", "Provider type: pushover or telegram")
	notifProviderCreateCmd.Flags().StringVar(&notifProviderConfig, "config", "", "JSON credentials (e.g. '{\"app_token\":\"...\",\"user_key\":\"...\"}')")
	_ = notifProviderCreateCmd.MarkFlagRequired("name")
	_ = notifProviderCreateCmd.MarkFlagRequired("type")

	notifProviderCmd.AddCommand(notifProviderListCmd)
	notifProviderCmd.AddCommand(notifProviderCreateCmd)
	notifProviderCmd.AddCommand(notifProviderGetCmd)
	notifProviderCmd.AddCommand(notifProviderDeleteCmd)
	notifProviderCmd.AddCommand(notifProviderActivateCmd)
	notifProviderCmd.AddCommand(notifProviderDeactivateCmd)

	// Channel flags
	notifChannelListCmd.Flags().StringVar(&notifChannelProviderID, "provider-id", "", "Filter by provider ID")
	notifChannelCreateCmd.Flags().StringVar(&notifChannelName, "name", "", "Channel name")
	notifChannelCreateCmd.Flags().StringVar(&notifChannelProviderID, "provider-id", "", "Provider ID")
	notifChannelCreateCmd.Flags().StringVar(&notifChannelTemplate, "template", "", "Message template with ${variable} placeholders")
	notifChannelCreateCmd.Flags().StringVar(&notifChannelConfig, "config", "", "JSON delivery config (e.g. '{\"chat_id\":\"...\"}' or '{\"priority\":1}')")
	_ = notifChannelCreateCmd.MarkFlagRequired("name")
	_ = notifChannelCreateCmd.MarkFlagRequired("provider-id")

	notifChannelCmd.AddCommand(notifChannelListCmd)
	notifChannelCmd.AddCommand(notifChannelCreateCmd)
	notifChannelCmd.AddCommand(notifChannelGetCmd)
	notifChannelCmd.AddCommand(notifChannelDeleteCmd)
	notifChannelCmd.AddCommand(notifChannelActivateCmd)
	notifChannelCmd.AddCommand(notifChannelDeactivateCmd)

	notificationCmd.AddCommand(notifProviderCmd)
	notificationCmd.AddCommand(notifChannelCmd)
	rootCmd.AddCommand(notificationCmd)
}
```

- [ ] **Step 2: Build CLI**

```bash
go build ./cli/...
```
Expected: no errors.

- [ ] **Step 3: Commit**

```bash
git add cli/cmd/notification.go
git commit -m "feat: update CLI notification commands for providers and channels"
```

---

## Task 10: Update CLAUDE.md + Design Spec

**Files:**
- Modify: `CLAUDE.md`
- Modify: `docs/superpowers/specs/2026-03-28-notification-pipeline-design.md`

- [ ] **Step 1: Update CLAUDE.md**

In the **Key Design Decisions** section, find the `**Notifications**` bullet and replace it with:

```
- **Notification pipeline** — notifications are delivered through the action/chain system as a `notification` pipeline type; `notification_providers` table stores credentials (type + JSON config: `app_token`/`user_key` for Pushover, `bot_token` for Telegram); `notification_channels` table stores delivery config (provider FK, `message_template` with `${variable}` substitution from mergedCtx, JSON `config` for provider-specific fields); a notification pipeline stores `{"channel_id": N}` in `pipelines.config`; `ActionCache` builds `NotificationPipeline` at load time by resolving channel → provider → instantiating client; REST API: `GET|POST /api/notification/providers`, `GET|PUT|DELETE /api/notification/providers/{id}`, `POST /api/notification/providers/{id}/{activate,deactivate}`, same pattern for `/api/notification/channels`; `notifier.Dispatcher` and all event-emission code are retired
```

Also update the **Project Structure** `internal/notifier/` line to remove mention of dispatcher/event/template, and update `internal/pipeline/` to mention `NotificationPipeline`.

- [ ] **Step 2: Commit**

```bash
git add CLAUDE.md
git commit -m "docs: update CLAUDE.md for notification pipeline system"
```

---

## Task 11: Build Verification

- [ ] **Step 1: Run all tests**

```bash
go test ./...
```
Expected: all PASS, no compile errors.

- [ ] **Step 2: Build container image**

```bash
docker compose build iran-watcher
```
Expected: build succeeds with exit code 0.

- [ ] **Step 3: Confirm if build fails**

If the build fails due to CGO/ONNX in the Docker environment, check `docker compose logs iran-watcher` and address any linker or import issues before completing this task.

---

## Self-Review

**Spec coverage check:**
- ✅ `notification_providers` table with credentials JSON — Task 1
- ✅ `notification_channels` table with template + config JSON — Task 1
- ✅ `${variable}` substitution from mergedCtx — Task 5 (`substituteVars`)
- ✅ `${tweet_url}` built-in — the worker already sets `tweet_id` in mergedCtx; `tweet_url` can be derived in a template as `https://twitter.com/i/web/status/${tweet_id}` without extra wiring, OR the worker must inject it. **Gap found:** The spec says `${tweet_url}` is injected by the worker. Currently `manager.go` sets `tweet_id` and `tweet_author` but NOT `tweet_url`. Add to Task 8 Step 7 (serve.go edits):

  In `internal/worker/manager.go`, in `processTask`, after the existing mergedCtx assignments:
  ```go
  mergedCtx["tweet_id"] = ev.TweetID
  mergedCtx["tweet_author"] = ev.AuthorHandle
  // Add:
  mergedCtx["tweet"] = ev.Text
  mergedCtx["author"] = ev.AuthorHandle
  mergedCtx["tweet_url"] = "https://twitter.com/i/web/status/" + ev.TweetID
  ```
  Note: `tweet` and `author` are user-facing friendly names; `tweet_author`/`tweet_id` are the existing internal keys. Both sets should be present so templates can use either convention.

  Add this as a sub-step in Task 8.

- ✅ `NotificationPipeline` implements `inference.Pipeline` — Task 5
- ✅ `PipelineTypeNotification` in both model and inference — Task 2
- ✅ `ActionCache` handles notification type — Task 6
- ✅ `Provider` interface with `Deliver` — Task 4
- ✅ `notifier.Dispatcher` retired — Task 8
- ✅ New REST API routes — Task 7 + Task 8
- ✅ Data migration from old `notifications` — Task 1
- ✅ CLI updated — Task 9
- ✅ `configJSON` helper note — Task 3 Step 4

**Placeholder scan:** None found.

**Type consistency:**
- `NewNotificationPipeline(name string, channel *model.NotificationChannel, provider notifier.Provider)` — used consistently in Task 5 and Task 6. ✅
- `buildNotificationProvider(prov *model.NotificationProvider, logger *slog.Logger) (notifier.Provider, error)` — defined in Task 6, referenced only there. ✅
- `Store.GetNotificationChannel(ctx, int64)` — defined in Task 3, used in Task 6. ✅
- `Store.GetNotificationProvider(ctx, int64)` — defined in Task 3, used in Task 6. ✅
