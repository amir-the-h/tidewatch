# Discord Threaded Notifications Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Extend `DiscordClient.Deliver` and `NotificationPipeline.Run` so that a root Discord notification action creates a Forum channel thread and saves its ID, and downstream reply actions post into that thread.

**Architecture:** Mirror the existing Telegram threading pattern — `DiscordClient.Deliver` checks config for `discord_thread_id`; absent = root send (POST `?wait=true`, returns thread ID); present = reply send (POST `?thread_id=<id>`). `NotificationPipeline.Run` forwards `discord_thread_id` from context to the delivery config the same way it already forwards `telegram_message_id`.

**Tech Stack:** Go stdlib (`net/http`, `encoding/json`), `net/http/httptest` for tests, existing `notifier.Provider` interface.

---

## File Map

| File | Change |
|------|--------|
| `internal/notifier/discord.go` | Modify `Deliver`; split into `deliverRoot` + `deliverToThread`; add `threadNameFromText` helper |
| `internal/notifier/discord_test.go` | **Create** — 4 new tests for `DiscordClient` threading |
| `internal/pipeline/notification.go` | Add `discord_thread_id` to `internalMetaKeys`; add forwarding block in `Run` |
| `internal/pipeline/notification_test.go` | Add 1 new test for `discord_thread_id` forwarding |

---

## Task 1: Write failing tests for `DiscordClient` threading

**Files:**
- Create: `internal/notifier/discord_test.go`

- [ ] **Step 1: Create the test file**

```go
// internal/notifier/discord_test.go
package notifier

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestDiscordClient_RootSend_ReturnsThreadID(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("wait") != "true" {
			t.Errorf("expected wait=true query param, got %q", r.URL.RawQuery)
		}
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatalf("decode body: %v", err)
		}
		if _, ok := body["thread_name"]; !ok {
			t.Errorf("expected thread_name in body, got %v", body)
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"id":         "msg123",
			"channel_id": "thread456",
		})
	}))
	defer srv.Close()

	c := NewDiscordClient(srv.URL)
	out, err := c.Deliver(context.Background(), "hello world", map[string]any{})
	if err != nil {
		t.Fatalf("Deliver: %v", err)
	}
	if out["discord_thread_id"] != "thread456" {
		t.Errorf("discord_thread_id: got %v, want thread456", out["discord_thread_id"])
	}
}

func TestDiscordClient_ReplySend_UsesThreadID(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("thread_id") != "thread456" {
			t.Errorf("expected thread_id=thread456, got %q", r.URL.RawQuery)
		}
		w.WriteHeader(http.StatusNoContent)
	}))
	defer srv.Close()

	c := NewDiscordClient(srv.URL)
	out, err := c.Deliver(context.Background(), "reply message", map[string]any{"discord_thread_id": "thread456"})
	if err != nil {
		t.Fatalf("Deliver: %v", err)
	}
	if out["sent"] != true {
		t.Errorf("expected sent=true, got %v", out)
	}
}

func TestDiscordClient_ReplySend_MissingThreadID_Errors(t *testing.T) {
	c := NewDiscordClient("http://unused")
	_, err := c.Deliver(context.Background(), "msg", map[string]any{"discord_thread_id": ""})
	if err == nil {
		t.Fatal("expected error for empty discord_thread_id")
	}
	if !strings.Contains(err.Error(), "discord_thread_id missing from context") {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestDiscordClient_RootSend_MissingChannelID_Errors(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"id": "msg123"}) // no channel_id
	}))
	defer srv.Close()

	c := NewDiscordClient(srv.URL)
	_, err := c.Deliver(context.Background(), "msg", map[string]any{})
	if err == nil {
		t.Fatal("expected error when channel_id missing from response")
	}
	if !strings.Contains(err.Error(), "thread_id missing from wait response") {
		t.Errorf("unexpected error: %v", err)
	}
}
```

- [ ] **Step 2: Run tests to confirm they fail**

```bash
go test ./internal/notifier/... -run "TestDiscordClient" -v
```

Expected: FAIL — `TestDiscordClient_RootSend_ReturnsThreadID` will fail because `Deliver` currently ignores config and never returns `discord_thread_id`; the others will fail for similar reasons.

---

## Task 2: Implement `DiscordClient` threading

**Files:**
- Modify: `internal/notifier/discord.go`

- [ ] **Step 1: Replace `discord.go` with the updated implementation**

```go
package notifier

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"
)

// DiscordClient sends messages to a Discord Forum channel via an incoming webhook URL.
type DiscordClient struct {
	webhookURL string
	httpClient *http.Client
}

// NewDiscordClient creates a DiscordClient that delivers to the given webhook URL.
func NewDiscordClient(webhookURL string) *DiscordClient {
	return &DiscordClient{
		webhookURL: webhookURL,
		httpClient: &http.Client{
			Timeout: 10 * time.Second,
		},
	}
}

// Deliver implements notifier.Provider for DiscordClient.
//
// If config contains a non-empty "discord_thread_id" key, this is a reply send:
// the message is posted to the existing Forum thread via ?thread_id=<id>.
// Returns {"sent": true} on success.
//
// If "discord_thread_id" is absent from config, this is a root send:
// the message is posted with ?wait=true and a thread_name derived from the
// first 100 characters of the rendered text. The Forum channel creates a new
// thread automatically. Returns {"discord_thread_id": <thread_id>} on success.
//
// If "discord_thread_id" is present but empty, an error is returned.
func (c *DiscordClient) Deliver(ctx context.Context, text string, config map[string]any) (map[string]any, error) {
	if text == "" {
		return nil, fmt.Errorf("notifier: discord: message text is empty")
	}

	if rawID, ok := config["discord_thread_id"]; ok {
		threadID, _ := rawID.(string)
		if threadID == "" {
			return nil, fmt.Errorf("notifier: discord: discord_thread_id missing from context")
		}
		return c.deliverToThread(ctx, text, threadID)
	}

	return c.deliverRoot(ctx, text)
}

// deliverRoot creates a new Forum thread by posting with ?wait=true.
// Returns {"discord_thread_id": <channel_id>} from the Discord response.
func (c *DiscordClient) deliverRoot(ctx context.Context, text string) (map[string]any, error) {
	body := extractEmbedBody(text)
	body["thread_name"] = threadNameFromText(text)

	payload, err := json.Marshal(body)
	if err != nil {
		return nil, fmt.Errorf("notifier: discord: marshal body: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.webhookURL+"?wait=true", bytes.NewReader(payload))
	if err != nil {
		return nil, fmt.Errorf("notifier: discord: build request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("notifier: discord: http: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusNoContent {
		return nil, fmt.Errorf("notifier: discord: unexpected status %d", resp.StatusCode)
	}

	var waitResp struct {
		ChannelID string `json:"channel_id"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&waitResp); err != nil {
		return nil, fmt.Errorf("notifier: discord: decode wait response: %w", err)
	}
	if waitResp.ChannelID == "" {
		return nil, fmt.Errorf("notifier: discord: thread_id missing from wait response")
	}

	return map[string]any{"discord_thread_id": waitResp.ChannelID}, nil
}

// deliverToThread posts a message into an existing Forum thread via ?thread_id=<id>.
// Returns {"sent": true} on success.
func (c *DiscordClient) deliverToThread(ctx context.Context, text string, threadID string) (map[string]any, error) {
	body := extractEmbedBody(text)

	payload, err := json.Marshal(body)
	if err != nil {
		return nil, fmt.Errorf("notifier: discord: marshal body: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.webhookURL+"?thread_id="+threadID, bytes.NewReader(payload))
	if err != nil {
		return nil, fmt.Errorf("notifier: discord: build request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("notifier: discord: http: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusNoContent && resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("notifier: discord: unexpected status %d", resp.StatusCode)
	}

	return map[string]any{"sent": true}, nil
}

// extractEmbedBody scans text for the first top-level JSON object. If found,
// it builds a Discord webhook body with the JSON as an embed and any surrounding
// non-blank text as content. If no JSON object is found the full text is content.
func extractEmbedBody(text string) map[string]any {
	start := -1
	depth := 0
	for i, ch := range text {
		if ch == '{' {
			if depth == 0 {
				start = i
			}
			depth++
		} else if ch == '}' && depth > 0 {
			depth--
			if depth == 0 {
				candidate := text[start : i+1]
				var embed map[string]any
				if err := json.Unmarshal([]byte(candidate), &embed); err == nil && len(embed) > 0 {
					surrounding := (text[:start] + text[i+1:])
					content := strings.TrimSpace(surrounding)
					body := map[string]any{"embeds": []any{embed}}
					if content != "" {
						body["content"] = content
					}
					return body
				}
				// Not valid JSON — reset and keep scanning.
				start = -1
				depth = 0
			}
		}
	}
	return map[string]any{"content": text}
}

// threadNameFromText derives a Forum thread name from the rendered message text.
// It strips any embedded JSON object first, trims whitespace, then returns the
// first 100 runes. Falls back to "notification" when the result is empty.
func threadNameFromText(text string) string {
	stripped := text
	start := -1
	depth := 0
	for i, ch := range text {
		if ch == '{' {
			if depth == 0 {
				start = i
			}
			depth++
		} else if ch == '}' && depth > 0 {
			depth--
			if depth == 0 {
				candidate := text[start : i+1]
				var obj map[string]any
				if err := json.Unmarshal([]byte(candidate), &obj); err == nil && len(obj) > 0 {
					stripped = text[:start] + text[i+1:]
					break
				}
				start = -1
				depth = 0
			}
		}
	}
	stripped = strings.TrimSpace(stripped)
	if stripped == "" {
		return "notification"
	}
	runes := []rune(stripped)
	if len(runes) > 100 {
		runes = runes[:100]
	}
	return string(runes)
}
```

- [ ] **Step 2: Run tests to confirm they pass**

```bash
go test ./internal/notifier/... -run "TestDiscordClient" -v
```

Expected: all 4 `TestDiscordClient_*` tests PASS.

- [ ] **Step 3: Run the full notifier test suite to check for regressions**

```bash
go test ./internal/notifier/... -v
```

Expected: all tests PASS.

- [ ] **Step 4: Commit**

```bash
git add internal/notifier/discord.go internal/notifier/discord_test.go
git commit -m "feat(notifier): add Discord Forum channel threading support"
```

---

## Task 3: Write failing test for `NotificationPipeline` discord forwarding

**Files:**
- Modify: `internal/pipeline/notification_test.go`

- [ ] **Step 1: Add the test at the end of the file, after the existing `TestNotificationPipeline_ForwardsMessageIDToProvider` test**

```go
func TestNotificationPipeline_ForwardsDiscordThreadID(t *testing.T) {
	mp := &mockProvider{}
	p := NewNotificationPipeline("test", "msg", map[string]any{}, mp, nil)

	_, err := p.Run(context.Background(), inference.PipelineInput{
		Context: map[string]any{"discord_thread_id": "thread456"},
	})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if mp.lastConfig["discord_thread_id"] != "thread456" {
		t.Errorf("expected discord_thread_id forwarded to provider config, got %v", mp.lastConfig["discord_thread_id"])
	}
}
```

- [ ] **Step 2: Run the test to confirm it fails**

```bash
go test ./internal/pipeline/... -run "TestNotificationPipeline_ForwardsDiscordThreadID" -v
```

Expected: FAIL — `discord_thread_id` is not yet forwarded in `Run`.

---

## Task 4: Implement `NotificationPipeline` discord forwarding

**Files:**
- Modify: `internal/pipeline/notification.go`

- [ ] **Step 1: Add `discord_thread_id` to `internalMetaKeys`**

In `internal/pipeline/notification.go`, find the `internalMetaKeys` map (around line 30) and add the new key:

```go
var internalMetaKeys = map[string]bool{
	"sent":                true,
	"provider":            true,
	"telegram_message_id": true,
	"discord_thread_id":   true,
	"raw":                 true,
	"truncated":           true,
}
```

- [ ] **Step 2: Add the discord forwarding block in `Run`**

In `NotificationPipeline.Run` (around line 119), find the existing Telegram forwarding block:

```go
if v, ok := input.Context["telegram_message_id"]; ok {
    cfg["telegram_message_id"] = v
} else if v, ok := input.Context["last_telegram_message_id"]; ok {
    cfg["reply_to_message_id"] = v
}
```

Add the Discord block immediately after it:

```go
if v, ok := input.Context["discord_thread_id"]; ok {
    cfg["discord_thread_id"] = v
}
```

- [ ] **Step 3: Run the new test to confirm it passes**

```bash
go test ./internal/pipeline/... -run "TestNotificationPipeline_ForwardsDiscordThreadID" -v
```

Expected: PASS.

- [ ] **Step 4: Run the full pipeline test suite to check for regressions**

```bash
go test ./internal/pipeline/... -v
```

Expected: all tests PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/pipeline/notification.go internal/pipeline/notification_test.go
git commit -m "feat(pipeline): forward discord_thread_id to notification provider"
```

---

## Task 5: Build verification

- [ ] **Step 1: Run the full unit test suite**

```bash
go test ./...
```

Expected: all tests PASS with no compilation errors.

- [ ] **Step 2: Build the Docker image**

```bash
docker compose build --build-arg BUILDKIT_INLINE_CACHE=1 iran-watcher
```

Expected: build succeeds with no errors.
