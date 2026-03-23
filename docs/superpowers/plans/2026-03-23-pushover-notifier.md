# Pushover Notifier Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add a dynamic Pushover push notification system that dispatches alerts asynchronously when tweets are matched/categorized and when stream lifecycle events occur.

**Architecture:** Async channel-based dispatcher. Components (stream.Client, worker.Pool) emit lightweight events onto a buffered channel. A single dispatcher goroutine consumes events, matches them against notification configs stored in SQLite, and sends Pushover messages via HTTP POST. Fire-and-forget on failure.

**Tech Stack:** Go 1.25.5, SQLite (modernc.org/sqlite), chi router, cobra CLI, Pushover REST API

**Spec:** `docs/superpowers/specs/2026-03-23-pushover-notifier-design.md`

---

## File Structure

### New Files
| File | Responsibility |
|------|---------------|
| `internal/notifier/event.go` | `NotificationEvent` struct, `EventEmitter` interface, event type constants |
| `internal/notifier/pushover.go` | `PushoverClient` HTTP client, `PushoverMessage`, `Send()` |
| `internal/notifier/template.go` | Default message templates, `RenderMessage()`, URL placeholder expansion |
| `internal/notifier/dispatcher.go` | `Dispatcher` goroutine, channel management, filter matching logic |
| `internal/model/notification.go` | `Notification` domain struct |
| `migrations/004_notifications.sql` | CREATE TABLE notifications |
| `internal/api/handlers_notifications.go` | CRUD + activate/deactivate + test HTTP handlers |
| `cli/cmd/notification.go` | Cobra CLI commands for notification management |

### Modified Files
| File | Change |
|------|--------|
| `internal/store/store.go` | Add `NotificationFilter`, 6 notification methods to `Store` interface |
| `internal/store/sqlite.go` | Implement notification CRUD + `scanNotification()` helper |
| `internal/config/config.go` | Add `Pushover` config struct with env var overrides |
| `configs/iran-watcher.yaml` | Add `pushover` section |
| `internal/stream/client.go` | Add `emitter` field, emit `tweet_matched` + stream lifecycle events |
| `internal/worker/pool.go` | Add `emitter` field to Pool, thread through to workers |
| `internal/worker/worker.go` | Add `emitter` field, emit `tweet_categorized` + `categorization_failed` |
| `internal/api/server.go` | Add `dispatcher` field, register `/api/notifications` routes |
| `cli/cmd/serve.go` | Create PushoverClient + Dispatcher, wire into components |

---

## Task 1: Event Types and Emitter Interface

**Files:**
- Create: `internal/notifier/event.go`

- [ ] **Step 1: Create event.go with types and interface**

```go
package notifier

import (
	"encoding/json"
	"time"

	"github.com/amir-the-h/iran-watcher/internal/model"
)

const (
	EventTweetMatched         = "tweet_matched"
	EventTweetCategorized     = "tweet_categorized"
	EventCategorizationFailed = "categorization_failed"
	EventStreamConnected      = "stream_connected"
	EventStreamDisconnected   = "stream_disconnected"
	EventStreamError          = "stream_error"
)

// ValidEventTypes is the set of all valid event type values.
var ValidEventTypes = map[string]bool{
	EventTweetMatched:         true,
	EventTweetCategorized:     true,
	EventCategorizationFailed: true,
	EventStreamConnected:      true,
	EventStreamDisconnected:   true,
	EventStreamError:          true,
}

// NotificationEvent is the data emitted by components for the dispatcher.
type NotificationEvent struct {
	Type      string
	Tweet     *model.Tweet
	RuleIDs   []int64
	ModelName string
	Results   json.RawMessage
	Error     string
	Timestamp time.Time
}

// EventEmitter is the interface accepted by stream.Client and worker.Pool.
// Implementations must be safe for concurrent use. A nil EventEmitter is
// valid — callers should check for nil before calling Emit.
type EventEmitter interface {
	Emit(event NotificationEvent)
}
```

- [ ] **Step 2: Verify it compiles**

Run: `cd /mnt/shared/projects/personal/iran-watcher && go build ./internal/notifier/`
Expected: success (no errors)

- [ ] **Step 3: Commit**

```bash
git add internal/notifier/event.go
git commit -m "feat(notifier): add event types and EventEmitter interface"
```

---

## Task 2: Notification Domain Model

**Files:**
- Create: `internal/model/notification.go`

- [ ] **Step 1: Create notification.go**

```go
package model

import (
	"encoding/json"
	"time"
)

// Notification defines a push notification configuration.
type Notification struct {
	ID                int64            `json:"id"`
	Name              string           `json:"name"`
	EventType         string           `json:"event_type"`
	Active            bool             `json:"active"`
	FilterRuleID      *int64           `json:"filter_rule_id,omitempty"`
	FilterModelName   *string          `json:"filter_model_name,omitempty"`
	FilterModelResult *json.RawMessage `json:"filter_model_result,omitempty"`
	PushoverPriority  int              `json:"pushover_priority"`
	PushoverSound     *string          `json:"pushover_sound,omitempty"`
	PushoverDevice    *string          `json:"pushover_device,omitempty"`
	PushoverTitle     *string          `json:"pushover_title,omitempty"`
	PushoverURL       *string          `json:"pushover_url,omitempty"`
	PushoverURLTitle  *string          `json:"pushover_url_title,omitempty"`
	PushoverTTL       *int             `json:"pushover_ttl,omitempty"`
	PushoverHTML      bool             `json:"pushover_html"`
	PushoverRetry     *int             `json:"pushover_retry,omitempty"`
	PushoverExpire    *int             `json:"pushover_expire,omitempty"`
	MessageTemplate   *string          `json:"message_template,omitempty"`
	CreatedAt         time.Time        `json:"created_at"`
	UpdatedAt         time.Time        `json:"updated_at"`
}
```

- [ ] **Step 2: Verify it compiles**

Run: `go build ./internal/model/`
Expected: success

- [ ] **Step 3: Commit**

```bash
git add internal/model/notification.go
git commit -m "feat(model): add Notification domain struct"
```

---

## Task 3: Migration and Store Interface

**Files:**
- Create: `migrations/004_notifications.sql`
- Modify: `internal/store/store.go`

- [ ] **Step 1: Create migration file**

> **Note:** Verify existing migration files in `migrations/` before creating. Current state: `001_initial.sql`, `003_rule_builder.sql`. The next number is `004`. If another branch has landed a new migration, adjust accordingly.

```sql
CREATE TABLE IF NOT EXISTS notifications (
    id                   INTEGER PRIMARY KEY AUTOINCREMENT,
    name                 TEXT    NOT NULL,
    event_type           TEXT    NOT NULL,
    active               INTEGER NOT NULL DEFAULT 1,
    filter_rule_id       INTEGER NULL REFERENCES rules(id),
    filter_model_name    TEXT NULL,
    filter_model_result  TEXT NULL,
    pushover_priority    INTEGER NOT NULL DEFAULT 0,
    pushover_sound       TEXT NULL,
    pushover_device      TEXT NULL,
    pushover_title       TEXT NULL,
    pushover_url         TEXT NULL,
    pushover_url_title   TEXT NULL,
    pushover_ttl         INTEGER NULL,
    pushover_html        INTEGER NOT NULL DEFAULT 0,
    pushover_retry       INTEGER NULL,
    pushover_expire      INTEGER NULL,
    message_template     TEXT NULL,
    created_at           DATETIME NOT NULL DEFAULT (datetime('now')),
    updated_at           DATETIME NOT NULL DEFAULT (datetime('now'))
);

CREATE INDEX IF NOT EXISTS idx_notifications_event_type ON notifications(event_type);
CREATE INDEX IF NOT EXISTS idx_notifications_active ON notifications(active);
```

- [ ] **Step 2: Add NotificationFilter and Store methods to store.go**

Add after line 37 (after `CategorizationFilter`):

```go
// NotificationFilter defines optional filters for listing notifications.
type NotificationFilter struct {
	EventType *string
	Active    *bool
	RuleID    *int64
}
```

Add to the `Store` interface before `Close() error` (after line 78):

```go
	// Notifications
	SaveNotification(ctx context.Context, n *model.Notification) error
	GetNotification(ctx context.Context, id int64) (*model.Notification, error)
	ListNotifications(ctx context.Context, filter NotificationFilter) ([]*model.Notification, error)
	UpdateNotification(ctx context.Context, n *model.Notification) error
	DeleteNotification(ctx context.Context, id int64) error
	SetNotificationActive(ctx context.Context, id int64, active bool) error
```

- [ ] **Step 3: Verify the expected compile error** (checkpoint — sqlite.go doesn't implement the new methods yet)

Run: `go build ./internal/store/`
Expected: compile error about `sqliteStore` not implementing `Store` — this is correct and will be resolved in Task 4.

- [ ] **Step 4: Commit**

```bash
git add migrations/004_notifications.sql internal/store/store.go
git commit -m "feat(store): add notifications table migration and Store interface methods"
```

---

## Task 4: SQLite Notification Implementation

**Files:**
- Modify: `internal/store/sqlite.go`

- [ ] **Step 1: Add scanNotification helper**

Add after `scanCategorization` (after line 691):

```go
func scanNotification(s scanner) (*model.Notification, error) {
	var n model.Notification
	var active, html int
	var filterRuleID sql.NullInt64
	var filterModelName, filterModelResult sql.NullString
	var pushSound, pushDevice, pushTitle, pushURL, pushURLTitle sql.NullString
	var pushTTL, pushRetry, pushExpire sql.NullInt64
	var msgTemplate sql.NullString
	var createdAt, updatedAt string

	if err := s.Scan(
		&n.ID, &n.Name, &n.EventType, &active,
		&filterRuleID, &filterModelName, &filterModelResult,
		&n.PushoverPriority, &pushSound, &pushDevice, &pushTitle,
		&pushURL, &pushURLTitle, &pushTTL, &html,
		&pushRetry, &pushExpire, &msgTemplate,
		&createdAt, &updatedAt,
	); err != nil {
		return nil, err
	}

	n.Active = active != 0
	n.PushoverHTML = html != 0

	if filterRuleID.Valid {
		n.FilterRuleID = &filterRuleID.Int64
	}
	if filterModelName.Valid {
		n.FilterModelName = &filterModelName.String
	}
	if filterModelResult.Valid {
		raw := json.RawMessage(filterModelResult.String)
		n.FilterModelResult = &raw
	}
	if pushSound.Valid {
		n.PushoverSound = &pushSound.String
	}
	if pushDevice.Valid {
		n.PushoverDevice = &pushDevice.String
	}
	if pushTitle.Valid {
		n.PushoverTitle = &pushTitle.String
	}
	if pushURL.Valid {
		n.PushoverURL = &pushURL.String
	}
	if pushURLTitle.Valid {
		n.PushoverURLTitle = &pushURLTitle.String
	}
	if pushTTL.Valid {
		v := int(pushTTL.Int64)
		n.PushoverTTL = &v
	}
	if pushRetry.Valid {
		v := int(pushRetry.Int64)
		n.PushoverRetry = &v
	}
	if pushExpire.Valid {
		v := int(pushExpire.Int64)
		n.PushoverExpire = &v
	}
	if msgTemplate.Valid {
		n.MessageTemplate = &msgTemplate.String
	}

	var err error
	n.CreatedAt, err = time.Parse(time.RFC3339, createdAt)
	if err != nil {
		return nil, fmt.Errorf("parse notification created_at: %w", err)
	}
	n.UpdatedAt, err = time.Parse(time.RFC3339, updatedAt)
	if err != nil {
		return nil, fmt.Errorf("parse notification updated_at: %w", err)
	}

	return &n, nil
}
```

- [ ] **Step 2: Add CRUD methods**

Add after the scanNotification helper, before the `boolToInt` helper:

```go
// ── Notifications ─────────────────────────────────────────────────────────────

const notificationColumns = `id, name, event_type, active,
	filter_rule_id, filter_model_name, filter_model_result,
	pushover_priority, pushover_sound, pushover_device, pushover_title,
	pushover_url, pushover_url_title, pushover_ttl, pushover_html,
	pushover_retry, pushover_expire, message_template,
	created_at, updated_at`

func (s *sqliteStore) SaveNotification(ctx context.Context, n *model.Notification) error {
	var filterResult *string
	if n.FilterModelResult != nil {
		v := string(*n.FilterModelResult)
		filterResult = &v
	}

	res, err := s.db.ExecContext(ctx,
		`INSERT INTO notifications (name, event_type, active,
			filter_rule_id, filter_model_name, filter_model_result,
			pushover_priority, pushover_sound, pushover_device, pushover_title,
			pushover_url, pushover_url_title, pushover_ttl, pushover_html,
			pushover_retry, pushover_expire, message_template,
			created_at, updated_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		n.Name, n.EventType, boolToInt(n.Active),
		n.FilterRuleID, n.FilterModelName, filterResult,
		n.PushoverPriority, n.PushoverSound, n.PushoverDevice, n.PushoverTitle,
		n.PushoverURL, n.PushoverURLTitle, n.PushoverTTL, boolToInt(n.PushoverHTML),
		n.PushoverRetry, n.PushoverExpire, n.MessageTemplate,
		n.CreatedAt.UTC().Format(time.RFC3339),
		n.UpdatedAt.UTC().Format(time.RFC3339),
	)
	if err != nil {
		return fmt.Errorf("store: save notification: %w", err)
	}
	id, err := res.LastInsertId()
	if err != nil {
		return fmt.Errorf("store: save notification: last insert id: %w", err)
	}
	n.ID = id
	return nil
}

func (s *sqliteStore) GetNotification(ctx context.Context, id int64) (*model.Notification, error) {
	row := s.db.QueryRowContext(ctx,
		`SELECT `+notificationColumns+` FROM notifications WHERE id = ?`, id)
	n, err := scanNotification(row)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, fmt.Errorf("store: get notification: not found: %w", err)
		}
		return nil, fmt.Errorf("store: get notification: %w", err)
	}
	return n, nil
}

func (s *sqliteStore) ListNotifications(ctx context.Context, filter NotificationFilter) ([]*model.Notification, error) {
	query := `SELECT ` + notificationColumns + ` FROM notifications`
	var args []any
	var where []string

	if filter.EventType != nil {
		where = append(where, "event_type = ?")
		args = append(args, *filter.EventType)
	}
	if filter.Active != nil {
		where = append(where, "active = ?")
		args = append(args, boolToInt(*filter.Active))
	}
	if filter.RuleID != nil {
		where = append(where, "filter_rule_id = ?")
		args = append(args, *filter.RuleID)
	}
	if len(where) > 0 {
		query += " WHERE " + strings.Join(where, " AND ")
	}
	query += " ORDER BY id ASC"

	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("store: list notifications: %w", err)
	}
	defer rows.Close()

	var notifications []*model.Notification
	for rows.Next() {
		n, err := scanNotification(rows)
		if err != nil {
			return nil, fmt.Errorf("store: list notifications: scan: %w", err)
		}
		notifications = append(notifications, n)
	}
	return notifications, rows.Err()
}

func (s *sqliteStore) UpdateNotification(ctx context.Context, n *model.Notification) error {
	var filterResult *string
	if n.FilterModelResult != nil {
		v := string(*n.FilterModelResult)
		filterResult = &v
	}

	_, err := s.db.ExecContext(ctx,
		`UPDATE notifications SET
			name = ?, event_type = ?, active = ?,
			filter_rule_id = ?, filter_model_name = ?, filter_model_result = ?,
			pushover_priority = ?, pushover_sound = ?, pushover_device = ?, pushover_title = ?,
			pushover_url = ?, pushover_url_title = ?, pushover_ttl = ?, pushover_html = ?,
			pushover_retry = ?, pushover_expire = ?, message_template = ?,
			updated_at = ?
		 WHERE id = ?`,
		n.Name, n.EventType, boolToInt(n.Active),
		n.FilterRuleID, n.FilterModelName, filterResult,
		n.PushoverPriority, n.PushoverSound, n.PushoverDevice, n.PushoverTitle,
		n.PushoverURL, n.PushoverURLTitle, n.PushoverTTL, boolToInt(n.PushoverHTML),
		n.PushoverRetry, n.PushoverExpire, n.MessageTemplate,
		n.UpdatedAt.UTC().Format(time.RFC3339),
		n.ID,
	)
	if err != nil {
		return fmt.Errorf("store: update notification: %w", err)
	}
	return nil
}

func (s *sqliteStore) DeleteNotification(ctx context.Context, id int64) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM notifications WHERE id = ?`, id)
	if err != nil {
		return fmt.Errorf("store: delete notification: %w", err)
	}
	return nil
}

func (s *sqliteStore) SetNotificationActive(ctx context.Context, id int64, active bool) error {
	_, err := s.db.ExecContext(ctx,
		`UPDATE notifications SET active = ?, updated_at = ? WHERE id = ?`,
		boolToInt(active), time.Now().UTC().Format(time.RFC3339), id)
	if err != nil {
		return fmt.Errorf("store: set notification active: %w", err)
	}
	return nil
}
```

- [ ] **Step 3: Verify it compiles**

Run: `go build ./internal/store/`
Expected: success

- [ ] **Step 4: Write unit tests for notification CRUD**

Create `internal/store/sqlite_notification_test.go` — test SaveNotification, GetNotification, ListNotifications (with filters), UpdateNotification, DeleteNotification, SetNotificationActive. Use in-memory SQLite (`:memory:`) with embedded migrations, following the existing test patterns.

- [ ] **Step 5: Run tests**

Run: `go test ./internal/store/ -v -run Notification`
Expected: all pass

- [ ] **Step 6: Commit**

```bash
git add internal/store/sqlite.go internal/store/sqlite_notification_test.go
git commit -m "feat(store): implement SQLite notification CRUD"
```

---

## Task 5: Pushover Client

**Files:**
- Create: `internal/notifier/pushover.go`

- [ ] **Step 1: Write pushover_test.go with a test server**

Create `internal/notifier/pushover_test.go`. Use `httptest.NewServer` to mock the Pushover API. Test:
- Successful send (status 1 response) — verify form fields are correct
- Pushover error response (status 0 with errors array)
- Empty message validation error
- Emergency priority validation (retry < 30, missing expire)
- Optional fields omitted when zero/empty

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/notifier/ -v -run TestPushover`
Expected: FAIL (PushoverClient not defined)

- [ ] **Step 3: Implement pushover.go**

```go
package notifier

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

const defaultPushoverURL = "https://api.pushover.net/1/messages.json"

// PushoverMessage holds all parameters for a single Pushover notification.
type PushoverMessage struct {
	Message  string
	Title    string
	Priority int
	Sound    string
	Device   string
	URL      string
	URLTitle string
	TTL      int
	HTML     bool
	Retry    int
	Expire   int
}

// PushoverClient sends notifications via the Pushover API.
type PushoverClient struct {
	appToken   string
	userKey    string
	apiURL     string // overridable for tests
	httpClient *http.Client
	logger     *slog.Logger
}

// NewPushoverClient creates a new client with the given credentials.
func NewPushoverClient(appToken, userKey string, logger *slog.Logger) *PushoverClient {
	return &PushoverClient{
		appToken: appToken,
		userKey:  userKey,
		apiURL:   defaultPushoverURL,
		httpClient: &http.Client{
			Timeout: 10 * time.Second,
		},
		logger: logger,
	}
}

// Send posts a message to the Pushover API. The context controls cancellation
// of the HTTP request (e.g. during graceful shutdown).
func (c *PushoverClient) Send(ctx context.Context, msg PushoverMessage) error {
	if msg.Message == "" {
		return fmt.Errorf("notifier: pushover: message is empty")
	}
	if msg.Priority == 2 {
		if msg.Retry < 30 {
			return fmt.Errorf("notifier: pushover: emergency priority requires retry >= 30")
		}
		if msg.Expire <= 0 || msg.Expire > 10800 {
			return fmt.Errorf("notifier: pushover: emergency priority requires 0 < expire <= 10800")
		}
	}

	form := url.Values{
		"token":   {c.appToken},
		"user":    {c.userKey},
		"message": {msg.Message},
	}
	if msg.Title != "" {
		form.Set("title", msg.Title)
	}
	if msg.Priority != 0 {
		form.Set("priority", strconv.Itoa(msg.Priority))
	}
	if msg.Sound != "" {
		form.Set("sound", msg.Sound)
	}
	if msg.Device != "" {
		form.Set("device", msg.Device)
	}
	if msg.URL != "" {
		form.Set("url", msg.URL)
	}
	if msg.URLTitle != "" {
		form.Set("url_title", msg.URLTitle)
	}
	if msg.TTL > 0 {
		form.Set("ttl", strconv.Itoa(msg.TTL))
	}
	if msg.HTML {
		form.Set("html", "1")
	}
	if msg.Priority == 2 {
		form.Set("retry", strconv.Itoa(msg.Retry))
		form.Set("expire", strconv.Itoa(msg.Expire))
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.apiURL, strings.NewReader(form.Encode()))
	if err != nil {
		return fmt.Errorf("notifier: pushover: build request: %w", err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("notifier: pushover: http: %w", err)
	}
	defer resp.Body.Close()

	var result struct {
		Status int      `json:"status"`
		Errors []string `json:"errors"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return fmt.Errorf("notifier: pushover: decode response: %w", err)
	}
	if result.Status != 1 {
		return fmt.Errorf("notifier: pushover: API error: %v", result.Errors)
	}

	return nil
}
```

- [ ] **Step 4: Run tests**

Run: `go test ./internal/notifier/ -v -run TestPushover`
Expected: all pass

- [ ] **Step 5: Commit**

```bash
git add internal/notifier/pushover.go internal/notifier/pushover_test.go
git commit -m "feat(notifier): add Pushover HTTP client"
```

---

## Task 6: Message Templating

**Files:**
- Create: `internal/notifier/template.go`

- [ ] **Step 1: Write template_test.go**

Test:
- Default template for each event type renders correctly with sample data
- Custom template renders with event data
- URL placeholder expansion (`{tweet_id}`, `{author_handle}`)
- URL expansion with nil Tweet (stream events) — placeholders become empty strings
- Invalid custom template returns error at parse time

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/notifier/ -v -run TestTemplate`
Expected: FAIL

- [ ] **Step 3: Implement template.go**

```go
package notifier

import (
	"bytes"
	"fmt"
	"strings"
	"text/template"
)

// defaultTemplates maps event types to their default Go templates.
var defaultTemplates = map[string]string{
	EventTweetMatched:         `New tweet from @{{.Tweet.AuthorHandle}}: {{.Tweet.Text}}`,
	EventTweetCategorized:     `Tweet by @{{.Tweet.AuthorHandle}} categorized by {{.ModelName}}: {{.Results}}`,
	EventCategorizationFailed: `Failed to categorize tweet {{.Tweet.ID}} with {{.ModelName}}: {{.Error}}`,
	EventStreamConnected:      `Stream connected`,
	EventStreamDisconnected:   `Stream disconnected`,
	EventStreamError:          `Stream error: {{.Error}}`,
}

// RenderMessage renders a notification message using the custom template if
// provided, otherwise falling back to the default for the event type.
func RenderMessage(event NotificationEvent, customTemplate *string) (string, error) {
	tmplStr := defaultTemplates[event.Type]
	if customTemplate != nil && *customTemplate != "" {
		tmplStr = *customTemplate
	}
	if tmplStr == "" {
		return fmt.Sprintf("Event: %s", event.Type), nil
	}

	t, err := template.New("msg").Parse(tmplStr)
	if err != nil {
		return "", fmt.Errorf("notifier: parse template: %w", err)
	}

	var buf bytes.Buffer
	if err := t.Execute(&buf, event); err != nil {
		return "", fmt.Errorf("notifier: execute template: %w", err)
	}
	return buf.String(), nil
}

// ValidateTemplate checks if a template string is valid Go text/template syntax.
func ValidateTemplate(tmpl string) error {
	_, err := template.New("validate").Parse(tmpl)
	if err != nil {
		return fmt.Errorf("notifier: invalid template: %w", err)
	}
	return nil
}

// ExpandURL replaces {tweet_id} and {author_handle} placeholders in a URL.
// Safe to call with nil Tweet — placeholders expand to empty strings.
func ExpandURL(urlTemplate string, event NotificationEvent) string {
	tweetID := ""
	authorHandle := ""
	if event.Tweet != nil {
		tweetID = event.Tweet.ID
		authorHandle = event.Tweet.AuthorHandle
	}
	r := strings.NewReplacer(
		"{tweet_id}", tweetID,
		"{author_handle}", authorHandle,
	)
	return r.Replace(urlTemplate)
}
```

- [ ] **Step 4: Run tests**

Run: `go test ./internal/notifier/ -v -run TestTemplate`
Expected: all pass

- [ ] **Step 5: Commit**

```bash
git add internal/notifier/template.go internal/notifier/template_test.go
git commit -m "feat(notifier): add message templating and URL expansion"
```

---

## Task 7: Dispatcher

**Files:**
- Create: `internal/notifier/dispatcher.go`

- [ ] **Step 1: Write dispatcher_test.go**

Test:
- Emit sends event, dispatcher processes and calls PushoverClient.Send
- Filter matching: event_type match, rule ID filter, model name filter, model result filter
- AND logic: multiple filters all must pass
- No matching configs → no Send call
- Channel full → event dropped (no panic, no block)
- Context cancellation → dispatcher exits gracefully
- Use a mock PushoverClient (interface or test double with overridable apiURL)

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/notifier/ -v -run TestDispatcher`
Expected: FAIL

- [ ] **Step 3: Implement dispatcher.go**

```go
package notifier

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"context"

	"github.com/amir-the-h/iran-watcher/internal/model"
	"github.com/amir-the-h/iran-watcher/internal/store"
)

// Dispatcher consumes NotificationEvents and sends matching Pushover messages.
type Dispatcher struct {
	ch     chan NotificationEvent
	store  store.Store
	client *PushoverClient
	logger *slog.Logger
}

// NewDispatcher creates a new Dispatcher with a buffered event channel.
func NewDispatcher(s store.Store, client *PushoverClient, logger *slog.Logger) *Dispatcher {
	return &Dispatcher{
		ch:     make(chan NotificationEvent, 256),
		store:  s,
		client: client,
		logger: logger,
	}
}

// Emit sends an event to the dispatcher. Non-blocking: if the channel is
// full the event is dropped with a warning log.
func (d *Dispatcher) Emit(event NotificationEvent) {
	select {
	case d.ch <- event:
	default:
		d.logger.Warn("notifier: event channel full, dropping event",
			"event_type", event.Type)
	}
}

// Start runs the dispatcher loop. Blocks until ctx is cancelled, then
// drains remaining events best-effort.
func (d *Dispatcher) Start(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			d.drain()
			return
		case event := <-d.ch:
			d.handle(ctx, event)
		}
	}
}

func (d *Dispatcher) drain() {
	for {
		select {
		case event := <-d.ch:
			// Use Background during drain — parent context is already cancelled.
			d.handle(context.Background(), event)
		default:
			return
		}
	}
}

func (d *Dispatcher) handle(ctx context.Context, event NotificationEvent) {
	active := true
	configs, err := d.store.ListNotifications(ctx, store.NotificationFilter{
		EventType: &event.Type,
		Active:    &active,
	})
	if err != nil {
		d.logger.Error("notifier: list configs failed", "error", err)
		return
	}

	for _, cfg := range configs {
		if !matchesFilters(cfg, event) {
			continue
		}

		msg, err := d.buildMessage(cfg, event)
		if err != nil {
			d.logger.Error("notifier: build message failed",
				"notification_id", cfg.ID, "error", err)
			continue
		}

		if err := d.client.Send(ctx, msg); err != nil {
			d.logger.Error("notifier: pushover send failed",
				"notification_id", cfg.ID, "error", err)
		}
	}
}

func matchesFilters(cfg *model.Notification, event NotificationEvent) bool {
	if cfg.FilterRuleID != nil {
		found := false
		for _, rid := range event.RuleIDs {
			if rid == *cfg.FilterRuleID {
				found = true
				break
			}
		}
		if !found {
			return false
		}
	}

	if cfg.FilterModelName != nil {
		if event.ModelName != *cfg.FilterModelName {
			return false
		}
	}

	if cfg.FilterModelResult != nil {
		if !matchModelResult(*cfg.FilterModelResult, event.Results) {
			return false
		}
	}

	return true
}

// matchModelResult performs a shallow JSON key-value match. Each key in filter
// must exist in results with the same value. Only works with JSON objects;
// non-object results never match.
func matchModelResult(filter json.RawMessage, results json.RawMessage) bool {
	var filterMap map[string]interface{}
	if err := json.Unmarshal(filter, &filterMap); err != nil {
		return false
	}

	var resultsMap map[string]interface{}
	if err := json.Unmarshal(results, &resultsMap); err != nil {
		return false
	}

	for k, fv := range filterMap {
		rv, ok := resultsMap[k]
		if !ok {
			return false
		}
		// Compare as JSON to handle type normalization
		fj, _ := json.Marshal(fv)
		rj, _ := json.Marshal(rv)
		if string(fj) != string(rj) {
			return false
		}
	}
	return true
}

func (d *Dispatcher) buildMessage(cfg *model.Notification, event NotificationEvent) (PushoverMessage, error) {
	text, err := RenderMessage(event, cfg.MessageTemplate)
	if err != nil {
		return PushoverMessage{}, err
	}

	msg := PushoverMessage{
		Message:  text,
		Priority: cfg.PushoverPriority,
		HTML:     cfg.PushoverHTML,
	}

	if cfg.PushoverTitle != nil {
		msg.Title = *cfg.PushoverTitle
	}
	if cfg.PushoverSound != nil {
		msg.Sound = *cfg.PushoverSound
	}
	if cfg.PushoverDevice != nil {
		msg.Device = *cfg.PushoverDevice
	}
	if cfg.PushoverURL != nil {
		msg.URL = ExpandURL(*cfg.PushoverURL, event)
	}
	if cfg.PushoverURLTitle != nil {
		msg.URLTitle = *cfg.PushoverURLTitle
	}
	if cfg.PushoverTTL != nil {
		msg.TTL = *cfg.PushoverTTL
	}
	if cfg.PushoverRetry != nil {
		msg.Retry = *cfg.PushoverRetry
	}
	if cfg.PushoverExpire != nil {
		msg.Expire = *cfg.PushoverExpire
	}

	return msg, nil
}

// SendTestNotification loads a notification config and sends a test message
// directly via PushoverClient, bypassing filters and the event channel.
func (d *Dispatcher) SendTestNotification(ctx context.Context, id int64) error {
	cfg, err := d.store.GetNotification(ctx, id)
	if err != nil {
		return fmt.Errorf("notifier: test: %w", err)
	}

	testEvent := NotificationEvent{
		Type: cfg.EventType,
		Tweet: &model.Tweet{
			ID:           "test-tweet-id",
			AuthorHandle: "test_user",
			Text:         "This is a test notification from Iran Watcher.",
		},
		ModelName: "test-model",
		Results:   json.RawMessage(`{"label":"test"}`),
		Error:     "test error",
		Timestamp: time.Now(),
	}

	msg, err := d.buildMessage(cfg, testEvent)
	if err != nil {
		return fmt.Errorf("notifier: test: build message: %w", err)
	}

	if err := d.client.Send(ctx, msg); err != nil {
		return fmt.Errorf("notifier: test: send: %w", err)
	}

	return nil
}
```

- [ ] **Step 4: Run tests**

Run: `go test ./internal/notifier/ -v -run TestDispatcher`
Expected: all pass

- [ ] **Step 5: Commit**

```bash
git add internal/notifier/dispatcher.go internal/notifier/dispatcher_test.go
git commit -m "feat(notifier): add async Dispatcher with filter matching"
```

---

## Task 8: Configuration

**Files:**
- Modify: `internal/config/config.go`
- Modify: `configs/iran-watcher.yaml`

- [ ] **Step 1: Add Pushover config struct**

In `internal/config/config.go`, add after the `Log` struct (after line 38, before the closing `}`):

```go
	Pushover struct {
		AppToken string `yaml:"app_token"`
		UserKey  string `yaml:"user_key"`
	} `yaml:"pushover"`
```

- [ ] **Step 2: Add env var overrides**

In `applyEnv()`, add before the `return nil` (before line 129):

```go
	if v := os.Getenv("IRAN_WATCHER_PUSHOVER_APP_TOKEN"); v != "" {
		cfg.Pushover.AppToken = v
	}
	if v := os.Getenv("IRAN_WATCHER_PUSHOVER_USER_KEY"); v != "" {
		cfg.Pushover.UserKey = v
	}
```

- [ ] **Step 3: Add pushover section to default config**

In `configs/iran-watcher.yaml`, add at the end:

```yaml

pushover:
  app_token: ""
  user_key: ""
```

- [ ] **Step 4: Verify it compiles**

Run: `go build ./internal/config/`
Expected: success

- [ ] **Step 5: Commit**

```bash
git add internal/config/config.go configs/iran-watcher.yaml
git commit -m "feat(config): add Pushover app_token and user_key config"
```

---

## Task 9: Wire Emitter into Stream Client

**Files:**
- Modify: `internal/stream/client.go`

- [ ] **Step 1: Add emitter field to Client struct**

Add to the `Client` struct (after line 33, the `streamURL` field):

```go
	emitter notifier.EventEmitter // may be nil
```

Add import: `"github.com/amir-the-h/iran-watcher/internal/notifier"`

- [ ] **Step 2: Update NewClient to accept emitter**

Change `NewClient` signature (line 43) to:

```go
func NewClient(bearerToken string, s store.Store, p queue.Producer, rules *RuleManager, emitter notifier.EventEmitter, logger *slog.Logger) *Client {
```

Add `emitter: emitter,` to the struct literal.

- [ ] **Step 3: Emit stream_connected in connect()**

After line 195 (`c.logger.Info("stream: connected", ...)`), add:

```go
	if c.emitter != nil {
		c.emitter.Emit(notifier.NotificationEvent{
			Type:      notifier.EventStreamConnected,
			Timestamp: time.Now(),
		})
	}
```

- [ ] **Step 4: Emit stream_error in runLoop()**

After line 135-139 (the `c.logger.Warn("stream: connection error, will retry", ...)` block), add:

```go
			if c.emitter != nil {
				c.emitter.Emit(notifier.NotificationEvent{
					Type:      notifier.EventStreamError,
					Error:     err.Error(),
					Timestamp: time.Now(),
				})
			}
```

- [ ] **Step 5: Emit stream_disconnected in Disconnect()**

After line 112 (`c.logger.Info("stream: disconnected", ...)`), add:

```go
	if c.emitter != nil {
		c.emitter.Emit(notifier.NotificationEvent{
			Type:      notifier.EventStreamDisconnected,
			Timestamp: time.Now(),
		})
	}
```

- [ ] **Step 6: Emit tweet_matched in processEvent()**

Refactor the existing rule lookup loop (lines 242-264) to collect resolved local rule IDs, then emit after the loop. Declare `var resolvedRuleIDs []int64` before line 242. Inside the loop, after the successful `GetRuleByXRuleID` call (line 243), append `rule.ID`:

```go
	// 2. For each matching rule, look up the Rule by x_rule_id and write tweet_rules.
	var resolvedRuleIDs []int64
	for _, mr := range st.MatchingRules {
		rule, err := c.store.GetRuleByXRuleID(ctx, mr.ID)
		if err != nil {
			c.logger.Warn("stream: rule lookup failed",
				"component", "stream",
				"x_rule_id", mr.ID,
				"error", err,
			)
			continue
		}
		resolvedRuleIDs = append(resolvedRuleIDs, rule.ID)
		tr := &model.TweetRule{
			TweetID: st.Tweet.ID,
			RuleID:  rule.ID,
		}
		if err := c.store.SaveTweetRule(ctx, tr); err != nil {
			c.logger.Warn("stream: save tweet_rule failed",
				"component", "stream",
				"tweet_id", st.Tweet.ID,
				"rule_id", rule.ID,
				"error", err,
			)
		}
	}

	// 2b. Emit tweet_matched event with resolved local rule IDs.
	if c.emitter != nil {
		c.emitter.Emit(notifier.NotificationEvent{
			Type:      notifier.EventTweetMatched,
			Tweet:     st.Tweet,
			RuleIDs:   resolvedRuleIDs,
			Timestamp: time.Now(),
		})
	}
```

This replaces the entire existing rule loop (lines 242-264) — no duplicate lookups.

- [ ] **Step 7: Fix test call sites in stream package**

Any test files in `internal/stream/` that call `NewClient()` need the new `emitter` parameter added (pass `nil`). Search for `NewClient(` in test files and add the parameter.

- [ ] **Step 8: Verify it compiles**

Run: `go build ./internal/stream/`
Expected: success

- [ ] **Step 9: Commit**

```bash
git add internal/stream/
git commit -m "feat(stream): emit notification events for tweet_matched and stream lifecycle"
```

---

## Task 10: Wire Emitter into Worker Pool

**Files:**
- Modify: `internal/worker/pool.go`
- Modify: `internal/worker/worker.go`

- [ ] **Step 1: Add emitter to Pool and worker**

In `pool.go`, add to `Pool` struct (after line 20):
```go
	emitter  notifier.EventEmitter
```

Update `NewPool` signature (line 24) to:
```go
func NewPool(count int, consumer queue.Consumer, producer queue.Producer, s store.Store, reg *inference.Registry, emitter notifier.EventEmitter, logger *slog.Logger) *Pool {
```

Add `emitter: emitter,` to the returned struct.

In the `Start` method, add `emitter: p.emitter,` to the worker struct literal (after line 48).

In `worker.go`, add to `worker` struct (after line 21):
```go
	emitter  notifier.EventEmitter
```

Add import: `"github.com/amir-the-h/iran-watcher/internal/notifier"`

- [ ] **Step 2: Emit tweet_categorized after SaveCategorization**

After line 93 (closing brace of `if saveErr := ...`) in the success path, add:

```go
			if w.emitter != nil {
				w.emitter.Emit(notifier.NotificationEvent{
					Type:      notifier.EventTweetCategorized,
					Tweet:     streamTweet.Tweet,
					RuleIDs:   nil, // rule IDs not available in worker context
					ModelName: pipeline.Name(),
					Results:   result,
					Timestamp: time.Now(),
				})
			}
```

> **Note:** `RuleIDs` is nil here because the worker only has the tweet, not the matching rules. If rule-based filtering is needed for `tweet_categorized` events, the `StreamTweet.MatchingRules` would need to be resolved. For now, `filter_rule_id` on `tweet_categorized` notifications will not match. This is acceptable — rule filtering is most useful for `tweet_matched` events.

- [ ] **Step 3: Emit categorization_failed per failed model**

Inside the `if runErr != nil` block (after line 71, before `continue`), add:

```go
				if w.emitter != nil {
					w.emitter.Emit(notifier.NotificationEvent{
						Type:      notifier.EventCategorizationFailed,
						Tweet:     streamTweet.Tweet,
						ModelName: pipeline.Name(),
						Error:     runErr.Error(),
						Timestamp: time.Now(),
					})
				}
```

- [ ] **Step 4: Fix test call sites in worker package**

Any test files in `internal/worker/` that call `NewPool()` need the new `emitter` parameter added (pass `nil`). Search for `NewPool(` in test files and add the parameter.

- [ ] **Step 5: Verify it compiles**

Run: `go build ./internal/worker/`
Expected: success

- [ ] **Step 6: Commit**

```bash
git add internal/worker/
git commit -m "feat(worker): emit notification events for categorization results"
```

---

## Task 11: API Handlers for Notifications

**Files:**
- Create: `internal/api/handlers_notifications.go`
- Modify: `internal/api/server.go`

- [ ] **Step 1: Add dispatcher to Server and register routes**

In `server.go`, add to `Server` struct (after line 41, the `tier` field):

```go
	dispatcher *notifier.Dispatcher
```

Add import: `"github.com/amir-the-h/iran-watcher/internal/notifier"`

Update `NewServer` signature (line 49-58) to add `dispatcher *notifier.Dispatcher` parameter (after `logger`). Add `dispatcher: dispatcher,` to struct literal.

Update `NewServerWithConcreteRules` (line 178-189) to add the same parameter and pass it through.

Add notification routes inside `r.Route("/api", ...)` (after line 111, before the closing `})`):

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

- [ ] **Step 2: Implement handlers_notifications.go**

Create `internal/api/handlers_notifications.go` following the patterns in `handlers_models.go`:
- `handleListNotifications` — parse query params (`event_type`, `active`, `rule_id`), call `store.ListNotifications`
- `handleCreateNotification` — decode JSON body, validate (name, event_type required, event_type must be valid, priority 2 checks, template validation), set timestamps, call `store.SaveNotification`
- `handleGetNotification` — parse `{id}`, call `store.GetNotification`
- `handleUpdateNotification` — parse `{id}`, get existing, decode JSON, merge fields, validate, set `updated_at`, call `store.UpdateNotification`
- `handleDeleteNotification` — parse `{id}`, call `store.DeleteNotification`, 204
- `handleActivateNotification` / `handleDeactivateNotification` — call `store.SetNotificationActive`
- `handleTestNotification` — decode `{"notification_id": N}`, call `dispatcher.SendTestNotification`

All handlers use `writeJSON` and `writeError` from `server.go`.

- [ ] **Step 3: Fix test call sites in api package**

Any test files in `internal/api/` that call `NewServer()` or `NewServerWithConcreteRules()` need the new `dispatcher` parameter added (pass `nil`). Search for these function calls in test files and add the parameter.

- [ ] **Step 4: Verify it compiles**

Run: `go build ./internal/api/`
Expected: success

- [ ] **Step 5: Commit**

```bash
git add internal/api/
git commit -m "feat(api): add notification CRUD and test endpoints"
```

---

## Task 12: Wire Everything in serve.go

**Files:**
- Modify: `cli/cmd/serve.go`

- [ ] **Step 1: Add notifier import and initialization**

Add import: `"github.com/amir-the-h/iran-watcher/internal/notifier"`

After the registry load block (after line 94), add:

```go
	// Initialize Pushover notifier (optional — skipped if credentials not configured).
	var dispatcher *notifier.Dispatcher
	var emitter notifier.EventEmitter
	if cfg.Pushover.AppToken != "" && cfg.Pushover.UserKey != "" {
		pushClient := notifier.NewPushoverClient(cfg.Pushover.AppToken, cfg.Pushover.UserKey, logger)
		dispatcher = notifier.NewDispatcher(s, pushClient, logger)
		emitter = dispatcher
		logger.Info("serve: pushover notifier enabled", "component", "notifier")
	} else {
		logger.Info("serve: pushover notifier disabled (no credentials)", "component", "notifier")
	}
```

- [ ] **Step 2: Update component constructors**

Update line 97 (NewClient):
```go
	streamClient := stream.NewClient(cfg.X.BearerToken, s, q, rules, emitter, logger)
```

Update line 107 (NewPool):
```go
	pool := worker.NewPool(workerCount, q, q, s, reg, emitter, logger)
```

Update line 108 (NewServerWithConcreteRules):
```go
	srv := api.NewServerWithConcreteRules(cfg.API.Bind, s, q, reg, rules, streamClient, cfg.X.Tier, logger, dispatcher)
```

- [ ] **Step 3: Start dispatcher goroutine**

After the worker pool goroutine launch (after line 122), add:

```go
	if dispatcher != nil {
		go func() {
			logger.Info("serve: starting notification dispatcher", "component", "notifier")
			dispatcher.Start(ctx)
		}()
	}
```

- [ ] **Step 4: Verify it compiles**

Run: `go build ./cmd/iran-watcher/`
Expected: success

- [ ] **Step 5: Run all tests**

Run: `go test ./...`
Expected: all pass (existing tests may need NewClient/NewPool/NewServer call sites updated — fix any that break)

- [ ] **Step 6: Commit**

```bash
git add cli/cmd/serve.go
git commit -m "feat(serve): wire Pushover notifier into application lifecycle"
```

---

## Task 13: CLI Commands

**Files:**
- Create: `cli/cmd/notification.go`

- [ ] **Step 1: Implement notification CLI commands**

Follow the patterns in `cli/cmd/model.go`:

```go
package cmd

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"text/tabwriter"

	"github.com/spf13/cobra"
)

var notificationCmd = &cobra.Command{
	Use:   "notification",
	Short: "Manage push notifications",
}

// -- list --

var (
	notifListEventType string
	notifListActive    string
)

var notificationListCmd = &cobra.Command{
	Use:   "list",
	Short: "List notification configs",
	RunE: func(cmd *cobra.Command, args []string) error {
		u := baseURL + "/api/notifications"
		params := url.Values{}
		if notifListEventType != "" {
			params.Set("event_type", notifListEventType)
		}
		if notifListActive != "" {
			params.Set("active", notifListActive)
		}
		if len(params) > 0 {
			u += "?" + params.Encode()
		}

		resp, err := http.Get(u)
		if err != nil {
			return fmt.Errorf("notification list: %w", err)
		}
		defer resp.Body.Close()
		if err := handleAPIError(cmd, resp); err != nil {
			return err
		}

		var items []map[string]interface{}
		if err := json.NewDecoder(resp.Body).Decode(&items); err != nil {
			return fmt.Errorf("notification list: decode: %w", err)
		}

		w := tabwriter.NewWriter(cmd.OutOrStdout(), 0, 0, 2, ' ', 0)
		fmt.Fprintln(w, "ID\tNAME\tEVENT_TYPE\tACTIVE\tPRIORITY")
		for _, item := range items {
			fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\n",
				formatField(item["id"]),
				formatField(item["name"]),
				formatField(item["event_type"]),
				formatField(item["active"]),
				formatField(item["pushover_priority"]),
			)
		}
		return w.Flush()
	},
}

// -- create --

var (
	notifName          string
	notifEventType     string
	notifFilterRuleID  string
	notifFilterModel   string
	notifFilterResult  string
	notifPriority      int
	notifSound         string
	notifDevice        string
	notifTitle         string
	notifURL           string
	notifURLTitle      string
	notifTTL           int
	notifHTML          bool
	notifRetry         int
	notifExpire        int
	notifMsgTemplate   string
)

var notificationCreateCmd = &cobra.Command{
	Use:   "create",
	Short: "Create a notification config",
	RunE: func(cmd *cobra.Command, args []string) error {
		body := map[string]interface{}{
			"name":              notifName,
			"event_type":        notifEventType,
			"pushover_priority": notifPriority,
			"pushover_html":     notifHTML,
		}
		if notifFilterRuleID != "" {
			id, err := strconv.ParseInt(notifFilterRuleID, 10, 64)
			if err != nil {
				return fmt.Errorf("notification create: invalid rule ID: %w", err)
			}
			body["filter_rule_id"] = id
		}
		if notifFilterModel != "" {
			body["filter_model_name"] = notifFilterModel
		}
		if notifFilterResult != "" {
			body["filter_model_result"] = json.RawMessage(notifFilterResult)
		}
		if notifSound != "" {
			body["pushover_sound"] = notifSound
		}
		if notifDevice != "" {
			body["pushover_device"] = notifDevice
		}
		if notifTitle != "" {
			body["pushover_title"] = notifTitle
		}
		if notifURL != "" {
			body["pushover_url"] = notifURL
		}
		if notifURLTitle != "" {
			body["pushover_url_title"] = notifURLTitle
		}
		if notifTTL > 0 {
			body["pushover_ttl"] = notifTTL
		}
		if notifRetry > 0 {
			body["pushover_retry"] = notifRetry
		}
		if notifExpire > 0 {
			body["pushover_expire"] = notifExpire
		}
		if notifMsgTemplate != "" {
			body["message_template"] = notifMsgTemplate
		}

		data, err := json.Marshal(body)
		if err != nil {
			return fmt.Errorf("notification create: marshal: %w", err)
		}

		resp, err := http.Post(baseURL+"/api/notifications", "application/json", bytes.NewReader(data))
		if err != nil {
			return fmt.Errorf("notification create: %w", err)
		}
		defer resp.Body.Close()
		if err := handleAPIError(cmd, resp); err != nil {
			return err
		}

		var created map[string]interface{}
		if err := json.NewDecoder(resp.Body).Decode(&created); err != nil {
			return fmt.Errorf("notification create: decode: %w", err)
		}
		fmt.Fprintf(cmd.OutOrStdout(), "Notification created: id=%s name=%s\n",
			formatField(created["id"]), formatField(created["name"]))
		return nil
	},
}

// -- get --

var notificationGetCmd = &cobra.Command{
	Use:   "get <id>",
	Short: "Get a notification config",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		id := url.PathEscape(args[0])
		resp, err := http.Get(baseURL + "/api/notifications/" + id)
		if err != nil {
			return fmt.Errorf("notification get: %w", err)
		}
		defer resp.Body.Close()
		if err := handleAPIError(cmd, resp); err != nil {
			return err
		}

		var n map[string]interface{}
		if err := json.NewDecoder(resp.Body).Decode(&n); err != nil {
			return fmt.Errorf("notification get: decode: %w", err)
		}

		data, _ := json.MarshalIndent(n, "", "  ")
		fmt.Fprintln(cmd.OutOrStdout(), string(data))
		return nil
	},
}

// -- delete --

var notificationDeleteCmd = &cobra.Command{
	Use:   "delete <id>",
	Short: "Delete a notification config",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		id := url.PathEscape(args[0])
		req, err := http.NewRequest(http.MethodDelete, baseURL+"/api/notifications/"+id, nil)
		if err != nil {
			return fmt.Errorf("notification delete: %w", err)
		}
		resp, err := (&http.Client{}).Do(req)
		if err != nil {
			return fmt.Errorf("notification delete: %w", err)
		}
		defer resp.Body.Close()
		if err := handleAPIError(cmd, resp); err != nil {
			return err
		}
		fmt.Fprintf(cmd.OutOrStdout(), "Notification %s deleted\n", id)
		return nil
	},
}

// -- activate / deactivate --

var notificationActivateCmd = &cobra.Command{
	Use:   "activate <id>",
	Short: "Activate a notification config",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		id := url.PathEscape(args[0])
		req, err := http.NewRequest(http.MethodPost, baseURL+"/api/notifications/"+id+"/activate", http.NoBody)
		if err != nil {
			return fmt.Errorf("notification activate: %w", err)
		}
		resp, err := (&http.Client{}).Do(req)
		if err != nil {
			return fmt.Errorf("notification activate: %w", err)
		}
		defer resp.Body.Close()
		if err := handleAPIError(cmd, resp); err != nil {
			return err
		}
		fmt.Fprintf(cmd.OutOrStdout(), "Notification %s activated\n", id)
		return nil
	},
}

var notificationDeactivateCmd = &cobra.Command{
	Use:   "deactivate <id>",
	Short: "Deactivate a notification config",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		id := url.PathEscape(args[0])
		req, err := http.NewRequest(http.MethodPost, baseURL+"/api/notifications/"+id+"/deactivate", http.NoBody)
		if err != nil {
			return fmt.Errorf("notification deactivate: %w", err)
		}
		resp, err := (&http.Client{}).Do(req)
		if err != nil {
			return fmt.Errorf("notification deactivate: %w", err)
		}
		defer resp.Body.Close()
		if err := handleAPIError(cmd, resp); err != nil {
			return err
		}
		fmt.Fprintf(cmd.OutOrStdout(), "Notification %s deactivated\n", id)
		return nil
	},
}

// -- test --

var notificationTestCmd = &cobra.Command{
	Use:   "test <id>",
	Short: "Send a test notification",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		id, err := strconv.ParseInt(args[0], 10, 64)
		if err != nil {
			return fmt.Errorf("notification test: invalid ID: %w", err)
		}
		body := map[string]interface{}{"notification_id": id}
		data, _ := json.Marshal(body)

		resp, err := http.Post(baseURL+"/api/notifications/test", "application/json", bytes.NewReader(data))
		if err != nil {
			return fmt.Errorf("notification test: %w", err)
		}
		defer resp.Body.Close()
		if err := handleAPIError(cmd, resp); err != nil {
			return err
		}
		fmt.Fprintf(cmd.OutOrStdout(), "Test notification sent for config %d\n", id)
		return nil
	},
}

// -- init --

func init() {
	notificationListCmd.Flags().StringVar(&notifListEventType, "event-type", "", "Filter by event type")
	notificationListCmd.Flags().StringVar(&notifListActive, "active", "", "Filter by active (true/false)")

	notificationCreateCmd.Flags().StringVar(&notifName, "name", "", "Notification name")
	notificationCreateCmd.Flags().StringVar(&notifEventType, "event-type", "", "Event type")
	notificationCreateCmd.Flags().StringVar(&notifFilterRuleID, "filter-rule-id", "", "Filter by rule ID")
	notificationCreateCmd.Flags().StringVar(&notifFilterModel, "filter-model-name", "", "Filter by model name")
	notificationCreateCmd.Flags().StringVar(&notifFilterResult, "filter-model-result", "", "Filter by model result (JSON)")
	notificationCreateCmd.Flags().IntVar(&notifPriority, "priority", 0, "Pushover priority (-2 to 2)")
	notificationCreateCmd.Flags().StringVar(&notifSound, "sound", "", "Pushover sound")
	notificationCreateCmd.Flags().StringVar(&notifDevice, "device", "", "Pushover device")
	notificationCreateCmd.Flags().StringVar(&notifTitle, "title", "", "Pushover title")
	notificationCreateCmd.Flags().StringVar(&notifURL, "url", "", "Pushover URL template")
	notificationCreateCmd.Flags().StringVar(&notifURLTitle, "url-title", "", "Pushover URL title")
	notificationCreateCmd.Flags().IntVar(&notifTTL, "ttl", 0, "Pushover TTL (seconds)")
	notificationCreateCmd.Flags().BoolVar(&notifHTML, "html", false, "Enable HTML in message")
	notificationCreateCmd.Flags().IntVar(&notifRetry, "retry", 0, "Emergency retry interval (seconds)")
	notificationCreateCmd.Flags().IntVar(&notifExpire, "expire", 0, "Emergency expiry (seconds)")
	notificationCreateCmd.Flags().StringVar(&notifMsgTemplate, "message-template", "", "Custom Go template for message")
	_ = notificationCreateCmd.MarkFlagRequired("name")
	_ = notificationCreateCmd.MarkFlagRequired("event-type")

	notificationCmd.AddCommand(notificationListCmd)
	notificationCmd.AddCommand(notificationCreateCmd)
	notificationCmd.AddCommand(notificationGetCmd)
	notificationCmd.AddCommand(notificationDeleteCmd)
	notificationCmd.AddCommand(notificationActivateCmd)
	notificationCmd.AddCommand(notificationDeactivateCmd)
	notificationCmd.AddCommand(notificationTestCmd)
	rootCmd.AddCommand(notificationCmd)
}
```

- [ ] **Step 2: Verify it compiles**

Run: `go build ./cmd/iran-watcher/`
Expected: success

- [ ] **Step 3: Commit**

```bash
git add cli/cmd/notification.go
git commit -m "feat(cli): add notification management commands"
```

---

## Task 14: Fix Broken Call Sites and Final Integration Test

**Files:**
- Various test files and any callers of modified constructors

- [ ] **Step 1: Find and fix all broken call sites**

Run: `go build ./...`

Any file that calls `stream.NewClient`, `worker.NewPool`, `api.NewServer`, or `api.NewServerWithConcreteRules` with the old signature needs an extra `nil` (or emitter/dispatcher) argument. Common locations:
- Test files in `internal/stream/`, `internal/worker/`, `internal/api/`
- Any test helpers that construct these types

Fix each by adding the new parameter (typically `nil` in tests).

- [ ] **Step 2: Run all tests**

Run: `go test ./...`
Expected: all pass

- [ ] **Step 3: Run full build**

Run: `go build ./cmd/iran-watcher/`
Expected: success

- [ ] **Step 4: Commit**

```bash
git add -A
git commit -m "fix: update call sites for new emitter/dispatcher parameters"
```

---

## Task 15: Update Notification Command

**Files:**
- Modify: `cli/cmd/notification.go`

- [ ] **Step 1: Implement update command**

Add a `notificationUpdateCmd` to `cli/cmd/notification.go`. It should accept the same flags as create, call `PUT /api/notifications/{id}` with a JSON body containing only the fields that were explicitly set via flags. Use `cmd.Flags().Changed("flag-name")` to detect which flags were provided.

Register with: `notificationCmd.AddCommand(notificationUpdateCmd)` in `init()`.

```go
var notificationUpdateCmd = &cobra.Command{
	Use:   "update <id>",
	Short: "Update a notification config",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		id := url.PathEscape(args[0])
		body := map[string]interface{}{}

		if cmd.Flags().Changed("name") {
			body["name"] = notifName
		}
		if cmd.Flags().Changed("event-type") {
			body["event_type"] = notifEventType
		}
		if cmd.Flags().Changed("filter-rule-id") {
			rid, err := strconv.ParseInt(notifFilterRuleID, 10, 64)
			if err != nil {
				return fmt.Errorf("notification update: invalid rule ID: %w", err)
			}
			body["filter_rule_id"] = rid
		}
		if cmd.Flags().Changed("filter-model-name") {
			body["filter_model_name"] = notifFilterModel
		}
		if cmd.Flags().Changed("filter-model-result") {
			body["filter_model_result"] = json.RawMessage(notifFilterResult)
		}
		if cmd.Flags().Changed("priority") {
			body["pushover_priority"] = notifPriority
		}
		if cmd.Flags().Changed("sound") {
			body["pushover_sound"] = notifSound
		}
		if cmd.Flags().Changed("device") {
			body["pushover_device"] = notifDevice
		}
		if cmd.Flags().Changed("title") {
			body["pushover_title"] = notifTitle
		}
		if cmd.Flags().Changed("url") {
			body["pushover_url"] = notifURL
		}
		if cmd.Flags().Changed("url-title") {
			body["pushover_url_title"] = notifURLTitle
		}
		if cmd.Flags().Changed("ttl") {
			body["pushover_ttl"] = notifTTL
		}
		if cmd.Flags().Changed("html") {
			body["pushover_html"] = notifHTML
		}
		if cmd.Flags().Changed("retry") {
			body["pushover_retry"] = notifRetry
		}
		if cmd.Flags().Changed("expire") {
			body["pushover_expire"] = notifExpire
		}
		if cmd.Flags().Changed("message-template") {
			body["message_template"] = notifMsgTemplate
		}

		data, err := json.Marshal(body)
		if err != nil {
			return fmt.Errorf("notification update: marshal: %w", err)
		}

		req, err := http.NewRequest(http.MethodPut, baseURL+"/api/notifications/"+id, bytes.NewReader(data))
		if err != nil {
			return fmt.Errorf("notification update: %w", err)
		}
		req.Header.Set("Content-Type", "application/json")

		resp, err := (&http.Client{}).Do(req)
		if err != nil {
			return fmt.Errorf("notification update: %w", err)
		}
		defer resp.Body.Close()
		if err := handleAPIError(cmd, resp); err != nil {
			return err
		}

		fmt.Fprintf(cmd.OutOrStdout(), "Notification %s updated\n", id)
		return nil
	},
}
```

The update command reuses the same flag variables as create. Register the same flags on the update command in `init()` (but without marking any as required), and add `notificationCmd.AddCommand(notificationUpdateCmd)`.

- [ ] **Step 2: Verify it compiles**

Run: `go build ./cmd/iran-watcher/`
Expected: success

- [ ] **Step 3: Commit**

```bash
git add cli/cmd/notification.go
git commit -m "feat(cli): add notification update command"
```
