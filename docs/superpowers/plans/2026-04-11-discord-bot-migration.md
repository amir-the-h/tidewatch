# Discord Bot Migration + Thread Tagging Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Replace the webhook-based `DiscordClient` with a bot-API `DiscordBotClient` and add a `discord_tag_thread` internal pipeline handler that resolves generated tag names to IDs and patches the Forum thread.

**Architecture:** `DiscordBotClient` authenticates with `Authorization: Bot <token>` and calls `POST /channels/{forum_channel_id}/threads` (create), `POST /channels/{thread_id}/messages` (reply), `GET /channels/{forum_channel_id}` (list tags), and `PATCH /channels/{thread_id}` (apply tags). The new `discord_tag_thread` handler reads `discord_thread_id` + tag names from pipeline context, resolves names to IDs via `GetAvailableTags`, then calls `PatchThreadTags`. The `NotificationProvider` config for Discord changes from `{"webhook_url":"..."}` to `{"bot_token":"...","forum_channel_id":"..."}`.

**Tech Stack:** Go stdlib `net/http`, `net/http/httptest` for tests, Discord Bot API v10.

---

## File Map

| File | Action | Responsibility |
|------|--------|----------------|
| `internal/notifier/discord.go` | **Replace** | `DiscordBotClient` + `ForumTag` + all bot API methods |
| `internal/notifier/discord_test.go` | **Replace** | Tests for `DiscordBotClient` via `httptest.Server` |
| `internal/pipeline/discord_tag_thread.go` | **Create** | `DiscordTagThreadPipeline` + `discordTagger` interface + factory |
| `internal/pipeline/discord_tag_thread_test.go` | **Create** | Unit tests using mock `discordTagger` |
| `internal/worker/action_cache.go` | **Modify** | Discord provider case: parse `bot_token`+`forum_channel_id`, construct `DiscordBotClient` |
| `cli/cmd/serve.go` | **Modify** | Register `discord_tag_thread` handler |

---

## Task 1: DiscordBotClient — struct, constructor, and deliverRoot

**Files:**
- Replace: `internal/notifier/discord.go`
- Replace: `internal/notifier/discord_test.go`

- [ ] **Step 1: Replace discord_test.go with failing tests for the bot client**

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

func TestDiscordBotClient_CreateThread_ReturnsThreadID(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("expected POST, got %s", r.Method)
		}
		if !strings.HasSuffix(r.URL.Path, "/threads") {
			t.Errorf("expected path ending in /threads, got %s", r.URL.Path)
		}
		if r.Header.Get("Authorization") != "Bot mytoken" {
			t.Errorf("expected Bot auth header, got %q", r.Header.Get("Authorization"))
		}
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatalf("decode body: %v", err)
		}
		if body["name"] == nil || body["name"] == "" {
			t.Errorf("expected thread name in body, got %v", body)
		}
		if body["message"] == nil {
			t.Errorf("expected message object in body, got %v", body)
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"id": "thread123"})
	}))
	defer srv.Close()

	c := NewDiscordBotClient("mytoken", "forum456")
	c.baseURL = srv.URL
	out, err := c.Deliver(context.Background(), "hello world", map[string]any{})
	if err != nil {
		t.Fatalf("Deliver: %v", err)
	}
	if out["discord_thread_id"] != "thread123" {
		t.Errorf("discord_thread_id: got %v, want thread123", out["discord_thread_id"])
	}
}

func TestDiscordBotClient_CreateThread_EmptyText_Errors(t *testing.T) {
	c := NewDiscordBotClient("mytoken", "forum456")
	_, err := c.Deliver(context.Background(), "", map[string]any{})
	if err == nil {
		t.Fatal("expected error for empty text")
	}
}

func TestDiscordBotClient_CreateThread_MissingIDInResponse_Errors(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"type": 11}) // no id
	}))
	defer srv.Close()

	c := NewDiscordBotClient("mytoken", "forum456")
	c.baseURL = srv.URL
	_, err := c.Deliver(context.Background(), "msg", map[string]any{})
	if err == nil {
		t.Fatal("expected error when id missing from response")
	}
	if !strings.Contains(err.Error(), "thread id missing") {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestDiscordBotClient_CreateThread_WithAppliedTags(t *testing.T) {
	var gotTags []any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		if tags, ok := body["applied_tags"]; ok {
			gotTags, _ = tags.([]any)
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"id": "thread999"})
	}))
	defer srv.Close()

	c := NewDiscordBotClient("mytoken", "forum456")
	c.baseURL = srv.URL
	_, err := c.Deliver(context.Background(), "msg", map[string]any{
		"applied_tags": []string{"tag1", "tag2"},
	})
	if err != nil {
		t.Fatalf("Deliver: %v", err)
	}
	if len(gotTags) != 2 {
		t.Errorf("expected 2 applied_tags in request body, got %v", gotTags)
	}
}

func TestDiscordBotClient_ReplyToThread_PostsToMessages(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("expected POST, got %s", r.Method)
		}
		if !strings.HasSuffix(r.URL.Path, "/thread456/messages") {
			t.Errorf("expected path ending in /thread456/messages, got %s", r.URL.Path)
		}
		if r.Header.Get("Authorization") != "Bot mytoken" {
			t.Errorf("expected Bot auth header, got %q", r.Header.Get("Authorization"))
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"id": "msg789"})
	}))
	defer srv.Close()

	c := NewDiscordBotClient("mytoken", "forum456")
	c.baseURL = srv.URL
	out, err := c.Deliver(context.Background(), "reply text", map[string]any{"discord_thread_id": "thread456"})
	if err != nil {
		t.Fatalf("Deliver: %v", err)
	}
	if out["sent"] != true {
		t.Errorf("expected sent=true, got %v", out)
	}
}

func TestDiscordBotClient_ReplyToThread_EmptyThreadID_Errors(t *testing.T) {
	c := NewDiscordBotClient("mytoken", "forum456")
	_, err := c.Deliver(context.Background(), "msg", map[string]any{"discord_thread_id": ""})
	if err == nil {
		t.Fatal("expected error for empty discord_thread_id")
	}
	if !strings.Contains(err.Error(), "discord_thread_id missing from context") {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestDiscordBotClient_GetAvailableTags_ReturnsTags(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			t.Errorf("expected GET, got %s", r.Method)
		}
		if !strings.HasSuffix(r.URL.Path, "/forum456") {
			t.Errorf("expected path ending in /forum456, got %s", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"id":   "forum456",
			"type": 15,
			"available_tags": []any{
				map[string]any{"id": "111", "name": "military"},
				map[string]any{"id": "222", "name": "diplomacy"},
			},
		})
	}))
	defer srv.Close()

	c := NewDiscordBotClient("mytoken", "forum456")
	c.baseURL = srv.URL
	tags, err := c.GetAvailableTags(context.Background())
	if err != nil {
		t.Fatalf("GetAvailableTags: %v", err)
	}
	if len(tags) != 2 {
		t.Fatalf("expected 2 tags, got %d", len(tags))
	}
	if tags[0].ID != "111" || tags[0].Name != "military" {
		t.Errorf("tag[0]: got {%q, %q}, want {111, military}", tags[0].ID, tags[0].Name)
	}
	if tags[1].ID != "222" || tags[1].Name != "diplomacy" {
		t.Errorf("tag[1]: got {%q, %q}, want {222, diplomacy}", tags[1].ID, tags[1].Name)
	}
}

func TestDiscordBotClient_GetAvailableTags_APIError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusForbidden)
	}))
	defer srv.Close()

	c := NewDiscordBotClient("mytoken", "forum456")
	c.baseURL = srv.URL
	_, err := c.GetAvailableTags(context.Background())
	if err == nil {
		t.Fatal("expected error for non-200 response")
	}
}

func TestDiscordBotClient_PatchThreadTags_SendsPatch(t *testing.T) {
	var gotTagIDs []any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPatch {
			t.Errorf("expected PATCH, got %s", r.Method)
		}
		if !strings.HasSuffix(r.URL.Path, "/thread456") {
			t.Errorf("expected path ending in /thread456, got %s", r.URL.Path)
		}
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		gotTagIDs, _ = body["applied_tags"].([]any)
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"id": "thread456"})
	}))
	defer srv.Close()

	c := NewDiscordBotClient("mytoken", "forum456")
	c.baseURL = srv.URL
	err := c.PatchThreadTags(context.Background(), "thread456", []string{"111", "222"})
	if err != nil {
		t.Fatalf("PatchThreadTags: %v", err)
	}
	if len(gotTagIDs) != 2 {
		t.Errorf("expected 2 tag IDs in request, got %v", gotTagIDs)
	}
}

func TestDiscordBotClient_PatchThreadTags_APIError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusForbidden)
	}))
	defer srv.Close()

	c := NewDiscordBotClient("mytoken", "forum456")
	c.baseURL = srv.URL
	err := c.PatchThreadTags(context.Background(), "thread456", []string{"111"})
	if err == nil {
		t.Fatal("expected error for non-200 response")
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

```bash
cd /mnt/storage/shared/projects/personal/iran-watcher
go test ./internal/notifier/... -run TestDiscordBot -v 2>&1 | head -30
```

Expected: compile error referencing `NewDiscordBotClient` undefined, or test failures.

- [ ] **Step 3: Replace discord.go with DiscordBotClient implementation**

```go
// internal/notifier/discord.go
package notifier

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"
)

// ForumTag represents a Discord Forum channel tag.
type ForumTag struct {
	ID   string
	Name string
}

// DiscordBotClient sends messages to a Discord Forum channel using the Discord Bot API.
// It authenticates with Authorization: Bot <token>.
type DiscordBotClient struct {
	botToken       string
	forumChannelID string
	baseURL        string // defaults to "https://discord.com/api/v10"; overridable in tests
	httpClient     *http.Client
}

// NewDiscordBotClient creates a DiscordBotClient for the given bot token and Forum channel ID.
func NewDiscordBotClient(botToken, forumChannelID string) *DiscordBotClient {
	return &DiscordBotClient{
		botToken:       botToken,
		forumChannelID: forumChannelID,
		baseURL:        "https://discord.com/api/v10",
		httpClient:     &http.Client{Timeout: 10 * time.Second},
	}
}

// Deliver implements notifier.Provider for DiscordBotClient.
//
// If config contains a non-empty "discord_thread_id" key, the message is posted
// to the existing Forum thread via POST /channels/{thread_id}/messages.
// Returns {"sent": true} on success.
//
// If "discord_thread_id" is absent, a new Forum thread is created via
// POST /channels/{forum_channel_id}/threads. If config contains "applied_tags"
// ([]string or []any of strings), they are included at creation time.
// Returns {"discord_thread_id": "<id>"} on success.
//
// If "discord_thread_id" is present but empty, an error is returned.
func (c *DiscordBotClient) Deliver(ctx context.Context, text string, config map[string]any) (map[string]any, error) {
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

	var appliedTagIDs []string
	if rawTags, ok := config["applied_tags"]; ok {
		switch v := rawTags.(type) {
		case []string:
			appliedTagIDs = v
		case []any:
			for _, item := range v {
				if s, ok := item.(string); ok {
					appliedTagIDs = append(appliedTagIDs, s)
				}
			}
		}
	}

	return c.deliverRoot(ctx, text, appliedTagIDs)
}

// deliverRoot creates a new Forum thread via POST /channels/{forum_channel_id}/threads.
// Returns {"discord_thread_id": "<id>"} from the Discord response.
func (c *DiscordBotClient) deliverRoot(ctx context.Context, text string, appliedTagIDs []string) (map[string]any, error) {
	msgBody := extractEmbedBody(text)

	reqBody := map[string]any{
		"name":    threadNameFromText(text),
		"message": msgBody,
	}
	if len(appliedTagIDs) > 0 {
		reqBody["applied_tags"] = appliedTagIDs
	}

	payload, err := json.Marshal(reqBody)
	if err != nil {
		return nil, fmt.Errorf("notifier: discord: marshal body: %w", err)
	}

	url := c.baseURL + "/channels/" + c.forumChannelID + "/threads"
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(payload))
	if err != nil {
		return nil, fmt.Errorf("notifier: discord: build request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bot "+c.botToken)

	resp, err := c.doRequest(ctx, req, payload)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusCreated {
		respBody, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("notifier: discord: unexpected status %d: %s", resp.StatusCode, bytes.TrimSpace(respBody))
	}

	var threadResp struct {
		ID string `json:"id"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&threadResp); err != nil {
		return nil, fmt.Errorf("notifier: discord: decode thread response: %w", err)
	}
	if threadResp.ID == "" {
		return nil, fmt.Errorf("notifier: discord: thread id missing from response")
	}

	return map[string]any{"discord_thread_id": threadResp.ID}, nil
}

// deliverToThread posts a message into an existing Forum thread via
// POST /channels/{thread_id}/messages. Returns {"sent": true} on success.
func (c *DiscordBotClient) deliverToThread(ctx context.Context, text string, threadID string) (map[string]any, error) {
	msgBody := extractEmbedBody(text)

	payload, err := json.Marshal(msgBody)
	if err != nil {
		return nil, fmt.Errorf("notifier: discord: marshal body: %w", err)
	}

	url := c.baseURL + "/channels/" + threadID + "/messages"
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(payload))
	if err != nil {
		return nil, fmt.Errorf("notifier: discord: build request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bot "+c.botToken)

	resp, err := c.doRequest(ctx, req, payload)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusCreated {
		respBody, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("notifier: discord: unexpected status %d: %s", resp.StatusCode, bytes.TrimSpace(respBody))
	}

	return map[string]any{"sent": true}, nil
}

// GetAvailableTags fetches the Forum channel's available tags via
// GET /channels/{forum_channel_id}. Returns a slice of ForumTag.
func (c *DiscordBotClient) GetAvailableTags(ctx context.Context) ([]ForumTag, error) {
	url := c.baseURL + "/channels/" + c.forumChannelID
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("notifier: discord: build request: %w", err)
	}
	req.Header.Set("Authorization", "Bot "+c.botToken)

	resp, err := c.doRequest(ctx, req, nil)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		respBody, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("notifier: discord: get channel status %d: %s", resp.StatusCode, bytes.TrimSpace(respBody))
	}

	var channelResp struct {
		AvailableTags []struct {
			ID   string `json:"id"`
			Name string `json:"name"`
		} `json:"available_tags"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&channelResp); err != nil {
		return nil, fmt.Errorf("notifier: discord: decode channel response: %w", err)
	}

	tags := make([]ForumTag, len(channelResp.AvailableTags))
	for i, t := range channelResp.AvailableTags {
		tags[i] = ForumTag{ID: t.ID, Name: t.Name}
	}
	return tags, nil
}

// PatchThreadTags applies the given tag IDs to the Forum thread via
// PATCH /channels/{thread_id}.
func (c *DiscordBotClient) PatchThreadTags(ctx context.Context, threadID string, tagIDs []string) error {
	reqBody := map[string]any{"applied_tags": tagIDs}
	payload, err := json.Marshal(reqBody)
	if err != nil {
		return fmt.Errorf("notifier: discord: marshal patch body: %w", err)
	}

	url := c.baseURL + "/channels/" + threadID
	req, err := http.NewRequestWithContext(ctx, http.MethodPatch, url, bytes.NewReader(payload))
	if err != nil {
		return fmt.Errorf("notifier: discord: build request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bot "+c.botToken)

	resp, err := c.doRequest(ctx, req, payload)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		respBody, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("notifier: discord: patch thread status %d: %s", resp.StatusCode, bytes.TrimSpace(respBody))
	}
	return nil
}

// doRequest executes an HTTP request against the Discord API. If the response is
// 429 Too Many Requests, it honours the retry_after value and retries once after
// that delay (up to 3 attempts). For nil payload (GET requests), the body is left
// as-is on the request. Returns the final *http.Response on success or an error.
func (c *DiscordBotClient) doRequest(ctx context.Context, req *http.Request, payload []byte) (*http.Response, error) {
	const maxRetries = 3
	for attempt := 0; attempt < maxRetries; attempt++ {
		if payload != nil {
			req.Body = io.NopCloser(bytes.NewReader(payload))
			req.ContentLength = int64(len(payload))
		} else {
			req.Body = http.NoBody
			req.ContentLength = 0
		}

		resp, err := c.httpClient.Do(req)
		if err != nil {
			return nil, fmt.Errorf("notifier: discord: http: %w", err)
		}

		if resp.StatusCode != http.StatusTooManyRequests {
			return resp, nil
		}
		resp.Body.Close()

		var delay time.Duration
		if ra := resp.Header.Get("X-RateLimit-Reset-After"); ra != "" {
			if secs, err := strconv.ParseFloat(ra, 64); err == nil {
				delay = time.Duration(secs*1000) * time.Millisecond
			}
		}
		if delay <= 0 {
			delay = 5 * time.Second
		}
		if delay > 30*time.Second {
			return nil, fmt.Errorf("notifier: discord: rate limited, retry_after %s exceeds cap", delay)
		}

		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(delay):
		}
	}
	return nil, fmt.Errorf("notifier: discord: rate limited after %d retries", maxRetries)
}

// extractEmbedBody scans text for the first top-level JSON object. If found,
// it builds a Discord message body with the JSON as an embed and any surrounding
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
					surrounding := stripFirstJSONObject(text)
					content := strings.TrimSpace(surrounding)
					body := map[string]any{"embeds": []any{embed}}
					if content != "" {
						body["content"] = content
					}
					return body
				}
				start = -1
				depth = 0
			}
		}
	}
	return map[string]any{"content": text}
}

// stripFirstJSONObject scans text for the first top-level JSON object and removes it.
func stripFirstJSONObject(text string) string {
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
					return text[:start] + text[i+1:]
				}
				start = -1
				depth = 0
			}
		}
	}
	return text
}

// threadNameFromText derives a Forum thread name from the rendered message text.
// Strips any embedded JSON object first, trims whitespace, returns the first 100 runes.
// Falls back to "notification" when the result is empty.
func threadNameFromText(text string) string {
	stripped := stripFirstJSONObject(text)
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

// Ensure DiscordBotClient satisfies notifier.Provider at compile time.
var _ Provider = (*DiscordBotClient)(nil)
```

- [ ] **Step 4: Run tests to verify they pass**

```bash
go test ./internal/notifier/... -run TestDiscordBot -v
```

Expected: all `TestDiscordBot*` tests PASS.

- [ ] **Step 5: Run the full notifier test suite to catch regressions**

```bash
go test ./internal/notifier/... -v 2>&1 | tail -20
```

Expected: all tests PASS. (Old `TestDiscordClient_*` tests are gone; replaced by the new ones above.)

- [ ] **Step 6: Commit**

```bash
git add internal/notifier/discord.go internal/notifier/discord_test.go
git commit -m "feat(notifier): replace DiscordClient with DiscordBotClient using bot API"
```

---

## Task 2: Update action_cache.go — discord provider uses bot credentials

**Files:**
- Modify: `internal/worker/action_cache.go:254-261`

- [ ] **Step 1: Update the discord case in buildNotificationPipeline**

In `internal/worker/action_cache.go`, find the `case "discord":` block (around line 254) and replace it:

```go
	case "discord":
		var creds struct {
			BotToken       string `json:"bot_token"`
			ForumChannelID string `json:"forum_channel_id"`
		}
		if err := json.Unmarshal(provider.Config, &creds); err != nil {
			return nil, fmt.Errorf("action cache: parse discord config: %w", err)
		}
		if creds.BotToken == "" {
			return nil, fmt.Errorf("action cache: discord provider missing bot_token")
		}
		if creds.ForumChannelID == "" {
			return nil, fmt.Errorf("action cache: discord provider missing forum_channel_id")
		}
		notifProvider = notifier.NewDiscordBotClient(creds.BotToken, creds.ForumChannelID)
```

- [ ] **Step 2: Run the full worker test suite**

```bash
go test ./internal/worker/... -v 2>&1 | tail -20
```

Expected: all tests PASS (no test directly exercises `buildNotificationPipeline`; the change is verified by compile success and the integration path).

- [ ] **Step 3: Commit**

```bash
git add internal/worker/action_cache.go
git commit -m "feat(worker): discord provider reads bot_token+forum_channel_id instead of webhook_url"
```

---

## Task 3: discord_tag_thread handler — interface, pipeline, factory

**Files:**
- Create: `internal/pipeline/discord_tag_thread.go`
- Create: `internal/pipeline/discord_tag_thread_test.go`

- [ ] **Step 1: Write the failing tests**

```go
// internal/pipeline/discord_tag_thread_test.go
package pipeline

import (
	"context"
	"errors"
	"testing"

	"github.com/amir-the-h/iran-watcher/internal/inference"
	"github.com/amir-the-h/iran-watcher/internal/notifier"
)

// mockDiscordTagger implements discordTagger for tests.
type mockDiscordTagger struct {
	tags            []notifier.ForumTag
	tagsErr         error
	patchedThreadID string
	patchedTagIDs   []string
	patchErr        error
}

func (m *mockDiscordTagger) GetAvailableTags(_ context.Context) ([]notifier.ForumTag, error) {
	return m.tags, m.tagsErr
}

func (m *mockDiscordTagger) PatchThreadTags(_ context.Context, threadID string, tagIDs []string) error {
	m.patchedThreadID = threadID
	m.patchedTagIDs = tagIDs
	return m.patchErr
}

func TestDiscordTagThreadPipeline_AppliesTags(t *testing.T) {
	mock := &mockDiscordTagger{
		tags: []notifier.ForumTag{
			{ID: "111", Name: "Military"},
			{ID: "222", Name: "Diplomacy"},
		},
	}
	p := &DiscordTagThreadPipeline{name: "test", client: mock, tagNamesKey: "discord_tag_names"}

	out, err := p.Run(context.Background(), inference.PipelineInput{
		Context: map[string]any{
			"discord_thread_id":  "thread123",
			"discord_tag_names":  []any{"military", "diplomacy"},
		},
	})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if out["tag_count"] != 2 {
		t.Errorf("tag_count: got %v, want 2", out["tag_count"])
	}
	if mock.patchedThreadID != "thread123" {
		t.Errorf("patched thread ID: got %q, want thread123", mock.patchedThreadID)
	}
	if len(mock.patchedTagIDs) != 2 {
		t.Errorf("patched tag IDs: got %v, want [111 222]", mock.patchedTagIDs)
	}
}

func TestDiscordTagThreadPipeline_CaseInsensitiveMatch(t *testing.T) {
	mock := &mockDiscordTagger{
		tags: []notifier.ForumTag{
			{ID: "111", Name: "Military"},
		},
	}
	p := &DiscordTagThreadPipeline{name: "test", client: mock, tagNamesKey: "discord_tag_names"}

	out, err := p.Run(context.Background(), inference.PipelineInput{
		Context: map[string]any{
			"discord_thread_id": "t1",
			"discord_tag_names": []any{"MILITARY"},
		},
	})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if out["tag_count"] != 1 {
		t.Errorf("tag_count: got %v, want 1", out["tag_count"])
	}
}

func TestDiscordTagThreadPipeline_UnknownTagsSkipped(t *testing.T) {
	mock := &mockDiscordTagger{
		tags: []notifier.ForumTag{
			{ID: "111", Name: "military"},
		},
	}
	p := &DiscordTagThreadPipeline{name: "test", client: mock, tagNamesKey: "discord_tag_names"}

	out, err := p.Run(context.Background(), inference.PipelineInput{
		Context: map[string]any{
			"discord_thread_id": "t1",
			"discord_tag_names": []any{"military", "unknown-tag"},
		},
	})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if out["tag_count"] != 1 {
		t.Errorf("tag_count: got %v, want 1 (unknown skipped)", out["tag_count"])
	}
	if len(mock.patchedTagIDs) != 1 || mock.patchedTagIDs[0] != "111" {
		t.Errorf("patched tag IDs: got %v, want [111]", mock.patchedTagIDs)
	}
}

func TestDiscordTagThreadPipeline_MissingThreadID_Errors(t *testing.T) {
	p := &DiscordTagThreadPipeline{name: "test", client: &mockDiscordTagger{}, tagNamesKey: "discord_tag_names"}

	_, err := p.Run(context.Background(), inference.PipelineInput{
		Context: map[string]any{"discord_tag_names": []any{"military"}},
	})
	if err == nil {
		t.Fatal("expected error when discord_thread_id missing")
	}
}

func TestDiscordTagThreadPipeline_EmptyTagNames_SkipsPatch(t *testing.T) {
	mock := &mockDiscordTagger{}
	p := &DiscordTagThreadPipeline{name: "test", client: mock, tagNamesKey: "discord_tag_names"}

	out, err := p.Run(context.Background(), inference.PipelineInput{
		Context: map[string]any{
			"discord_thread_id": "t1",
			// discord_tag_names absent
		},
	})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if out["tag_count"] != 0 {
		t.Errorf("tag_count: got %v, want 0", out["tag_count"])
	}
	if mock.patchedThreadID != "" {
		t.Error("PatchThreadTags should not be called when tag list is empty")
	}
}

func TestDiscordTagThreadPipeline_GetTagsError_Propagates(t *testing.T) {
	mock := &mockDiscordTagger{tagsErr: errors.New("network error")}
	p := &DiscordTagThreadPipeline{name: "test", client: mock, tagNamesKey: "discord_tag_names"}

	_, err := p.Run(context.Background(), inference.PipelineInput{
		Context: map[string]any{
			"discord_thread_id": "t1",
			"discord_tag_names": []any{"military"},
		},
	})
	if err == nil {
		t.Fatal("expected error when GetAvailableTags fails")
	}
}

func TestDiscordTagThreadPipeline_PatchError_Propagates(t *testing.T) {
	mock := &mockDiscordTagger{
		tags:     []notifier.ForumTag{{ID: "111", Name: "military"}},
		patchErr: errors.New("forbidden"),
	}
	p := &DiscordTagThreadPipeline{name: "test", client: mock, tagNamesKey: "discord_tag_names"}

	_, err := p.Run(context.Background(), inference.PipelineInput{
		Context: map[string]any{
			"discord_thread_id": "t1",
			"discord_tag_names": []any{"military"},
		},
	})
	if err == nil {
		t.Fatal("expected error when PatchThreadTags fails")
	}
}

func TestDiscordTagThreadPipeline_TypeAndName(t *testing.T) {
	p := &DiscordTagThreadPipeline{name: "my-tagger", client: &mockDiscordTagger{}, tagNamesKey: "discord_tag_names"}
	if p.Name() != "my-tagger" {
		t.Errorf("Name: got %q, want my-tagger", p.Name())
	}
	if p.Type() != inference.PipelineTypeInternal {
		t.Errorf("Type: got %q, want internal", p.Type())
	}
	if err := p.Close(); err != nil {
		t.Errorf("Close: got %v, want nil", err)
	}
}

func TestNewDiscordTagThreadFactory_MissingBotToken_Errors(t *testing.T) {
	f := NewDiscordTagThreadFactory()
	_, err := f("test", map[string]any{"forum_channel_id": "123"})
	if err == nil {
		t.Fatal("expected error for missing bot_token")
	}
}

func TestNewDiscordTagThreadFactory_MissingForumChannelID_Errors(t *testing.T) {
	f := NewDiscordTagThreadFactory()
	_, err := f("test", map[string]any{"bot_token": "Bot abc"})
	if err == nil {
		t.Fatal("expected error for missing forum_channel_id")
	}
}

func TestNewDiscordTagThreadFactory_DefaultTagNamesKey(t *testing.T) {
	f := NewDiscordTagThreadFactory()
	p, err := f("test", map[string]any{"bot_token": "Bot abc", "forum_channel_id": "123"})
	if err != nil {
		t.Fatalf("factory: %v", err)
	}
	dp, ok := p.(*DiscordTagThreadPipeline)
	if !ok {
		t.Fatal("expected *DiscordTagThreadPipeline")
	}
	if dp.tagNamesKey != "discord_tag_names" {
		t.Errorf("default tagNamesKey: got %q, want discord_tag_names", dp.tagNamesKey)
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

```bash
go test ./internal/pipeline/... -run TestDiscordTagThread -v 2>&1 | head -20
```

Expected: compile errors referencing `DiscordTagThreadPipeline` and `discordTagger` undefined.

- [ ] **Step 3: Create the handler implementation**

```go
// internal/pipeline/discord_tag_thread.go
package pipeline

import (
	"context"
	"fmt"
	"strings"

	"github.com/amir-the-h/iran-watcher/internal/inference"
	"github.com/amir-the-h/iran-watcher/internal/notifier"
)

// discordTagger abstracts the Discord Bot API calls needed by DiscordTagThreadPipeline.
// Satisfied by *notifier.DiscordBotClient; extracted for testability.
type discordTagger interface {
	GetAvailableTags(ctx context.Context) ([]notifier.ForumTag, error)
	PatchThreadTags(ctx context.Context, threadID string, tagIDs []string) error
}

// DiscordTagThreadPipeline reads generated tag names from pipeline context,
// resolves them to Discord Forum tag IDs via the bot API, and patches the thread.
type DiscordTagThreadPipeline struct {
	name        string
	client      discordTagger
	tagNamesKey string // context key holding []string of tag names
}

// Name implements inference.Pipeline.
func (p *DiscordTagThreadPipeline) Name() string { return p.name }

// Type implements inference.Pipeline.
func (p *DiscordTagThreadPipeline) Type() inference.PipelineType { return inference.PipelineTypeInternal }

// Close implements inference.Pipeline.
func (p *DiscordTagThreadPipeline) Close() error { return nil }

// Run resolves tag names to IDs and patches the Discord Forum thread.
//
// Reads "discord_thread_id" from input.Context (set by the upstream Discord
// notification action). Reads tag names from input.Context[tagNamesKey]
// (accepts []string or []any of strings). Unknown tag names are silently skipped.
// Returns {"tags_applied": []string, "tag_count": int}.
func (p *DiscordTagThreadPipeline) Run(ctx context.Context, input inference.PipelineInput) (map[string]any, error) {
	threadID, _ := input.Context["discord_thread_id"].(string)
	if threadID == "" {
		return nil, fmt.Errorf("discord_tag_thread: discord_thread_id not found in context")
	}

	var tagNames []string
	switch v := input.Context[p.tagNamesKey].(type) {
	case []string:
		tagNames = v
	case []any:
		for _, item := range v {
			if s, ok := item.(string); ok {
				tagNames = append(tagNames, s)
			}
		}
	}

	if len(tagNames) == 0 {
		return map[string]any{"tags_applied": []string{}, "tag_count": 0}, nil
	}

	available, err := p.client.GetAvailableTags(ctx)
	if err != nil {
		return nil, fmt.Errorf("discord_tag_thread: get available tags: %w", err)
	}

	nameToID := make(map[string]string, len(available))
	for _, tag := range available {
		nameToID[strings.ToLower(tag.Name)] = tag.ID
	}

	var tagIDs []string
	for _, name := range tagNames {
		if id, ok := nameToID[strings.ToLower(name)]; ok {
			tagIDs = append(tagIDs, id)
		}
	}

	if len(tagIDs) == 0 {
		return map[string]any{"tags_applied": []string{}, "tag_count": 0}, nil
	}

	if err := p.client.PatchThreadTags(ctx, threadID, tagIDs); err != nil {
		return nil, fmt.Errorf("discord_tag_thread: patch thread tags: %w", err)
	}

	return map[string]any{"tags_applied": tagIDs, "tag_count": len(tagIDs)}, nil
}

// NewDiscordTagThreadFactory returns a Factory for DiscordTagThreadPipeline.
// Each action gets its own DiscordBotClient constructed from its config.
// Required config keys: "bot_token", "forum_channel_id".
// Optional config key: "tag_names_key" (defaults to "discord_tag_names").
func NewDiscordTagThreadFactory() Factory {
	return func(name string, config map[string]any) (inference.Pipeline, error) {
		botToken, _ := config["bot_token"].(string)
		if botToken == "" {
			return nil, fmt.Errorf("discord_tag_thread: bot_token missing from config")
		}
		forumChannelID, _ := config["forum_channel_id"].(string)
		if forumChannelID == "" {
			return nil, fmt.Errorf("discord_tag_thread: forum_channel_id missing from config")
		}
		tagNamesKey, _ := config["tag_names_key"].(string)
		if tagNamesKey == "" {
			tagNamesKey = "discord_tag_names"
		}
		client := notifier.NewDiscordBotClient(botToken, forumChannelID)
		return &DiscordTagThreadPipeline{name: name, client: client, tagNamesKey: tagNamesKey}, nil
	}
}

// Ensure DiscordTagThreadPipeline satisfies inference.Pipeline at compile time.
var _ inference.Pipeline = (*DiscordTagThreadPipeline)(nil)

// Ensure *notifier.DiscordBotClient satisfies discordTagger at compile time.
var _ discordTagger = (*notifier.DiscordBotClient)(nil)
```

- [ ] **Step 4: Run tests to verify they pass**

```bash
go test ./internal/pipeline/... -run TestDiscordTagThread -v
```

Expected: all `TestDiscordTagThread*` and `TestNewDiscordTagThreadFactory*` tests PASS.

- [ ] **Step 5: Run full pipeline test suite**

```bash
go test ./internal/pipeline/... -v 2>&1 | tail -20
```

Expected: all tests PASS.

- [ ] **Step 6: Commit**

```bash
git add internal/pipeline/discord_tag_thread.go internal/pipeline/discord_tag_thread_test.go
git commit -m "feat(pipeline): add discord_tag_thread internal handler for bot-API tag patching"
```

---

## Task 4: Register handler in serve.go + build verification

**Files:**
- Modify: `cli/cmd/serve.go`

- [ ] **Step 1: Register the discord_tag_thread handler**

In `cli/cmd/serve.go`, find the block where internal pipeline handlers are registered (around line 134) and add after the `reverse_geocoder` line:

```go
	pipeReg.Register("discord_tag_thread", pipeline.NewDiscordTagThreadFactory())
```

The registration block should look like:

```go
	pipeReg := pipeline.NewRegistry()
	pipeReg.Register("entity_matcher", pipeline.NewEntityMatcherFactory(s))
	pipeReg.Register("entity_discovery", pipeline.NewEntityDiscoveryFactory(s))
	pipeReg.Register("entity_cooccurrence", pipeline.NewEntityCooccurrenceFactory(s))
	nominatimURL := cfg.Nominatim.URL
	if nominatimURL == "" {
		nominatimURL = "http://nominatim:8080"
	}
	pipeReg.Register("reverse_geocoder", pipeline.NewReverseGeocoderFactory(s, nominatimURL))
	pipeReg.Register("discord_tag_thread", pipeline.NewDiscordTagThreadFactory())
	if twitterClient != nil {
```

- [ ] **Step 2: Run the full test suite**

```bash
go test ./... 2>&1 | tail -30
```

Expected: all tests PASS.

- [ ] **Step 3: Build the Docker image to verify the binary compiles**

```bash
docker compose build --build-arg BUILDKIT_INLINE_CACHE=1 iran-watcher 2>&1 | tail -20
```

Expected: build completes successfully with no errors.

- [ ] **Step 4: Commit**

```bash
git add cli/cmd/serve.go
git commit -m "feat(serve): register discord_tag_thread pipeline handler"
```

---

## Task 5: Update CLAUDE.md documentation

**Files:**
- Modify: `CLAUDE.md`

- [ ] **Step 1: Update the Key Design Decisions section for Discord**

In `CLAUDE.md`, find the `**Notifications**` bullet under Key Design Decisions. Update the Discord provider description from webhook to bot, and add the new handler. The relevant part should read:

```
`NotificationProvider` stores credentials (pushover: `app_token`+`user_key`, telegram: `bot_token`; discord: `bot_token`+`forum_channel_id`); ...
```

Also add `discord_tag_thread` to the list of internal handlers:

```
internal handlers (`entity_matcher`, `entity_cooccurrence`, `reverse_geocoder`, `twitter_retweet`, `twitter_like`, `twitter_quote`, `notification_push`, `notification_tg`, `discord_tag_thread`) are registered at startup
```

- [ ] **Step 2: Commit**

```bash
git add CLAUDE.md
git commit -m "docs: update CLAUDE.md for Discord bot migration and discord_tag_thread handler"
```
