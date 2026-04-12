# Discord Thread Name Template Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add a `thread_name_template` config field to Discord notification actions so the forum thread name is controlled by a `${key}`-substitution template instead of being derived from the first 100 characters of the message body.

**Architecture:** `thread_name_template` is extracted in `buildNotificationPipeline()` alongside `message_template`, stored in `NotificationPipeline`, rendered in `Run()` using the same substitution logic, and injected as `deliveryConfig["thread_name"]` before calling `provider.Deliver()`. `DiscordBotClient.deliverRoot()` reads `thread_name` from config directly. Validation at build time and in the UI enforces it as required for Discord.

**Tech Stack:** Go (internal/pipeline, internal/notifier, internal/worker), Vue 3 + Naive UI (web/src/components/tabs/ActionsTab.vue)

---

## File Map

| File | Change |
|------|--------|
| `internal/pipeline/notification.go` | Add `threadNameTemplate` field; render + truncate in `Run()`; inject into per-call config copy |
| `internal/worker/action_cache.go` | Extract `thread_name_template`; exclude from deliveryConfig; validate required for Discord; pass to `NewNotificationPipeline` |
| `internal/notifier/discord.go` | Read `thread_name` from config in `deliverRoot()`; delete `threadNameFromText` helper |
| `internal/pipeline/notification_test.go` | Update all `NewNotificationPipeline` call sites (new param `""`); add thread name rendering test |
| `internal/notifier/discord_test.go` | Update `TestDiscordBotClient_CreateThread_ReturnsThreadID` to pass `thread_name` in config |
| `web/src/components/tabs/ActionsTab.vue` | Add `formThreadNameTemplate` ref; render Discord-only field; validate on save |

---

### Task 1: Update `NotificationPipeline` to render `thread_name_template`

**Files:**
- Modify: `internal/pipeline/notification.go`
- Test: `internal/pipeline/notification_test.go`

- [ ] **Step 1: Add `threadNameTemplate` field and update constructor**

In `internal/pipeline/notification.go`, update the struct and constructor:

```go
type NotificationPipeline struct {
	name               string
	messageTemplate    string
	threadNameTemplate string
	deliveryConfig     map[string]any
	provider           notifier.Provider
	loadActions        ActionSummaryLoader
}

func NewNotificationPipeline(name, messageTemplate, threadNameTemplate string, deliveryConfig map[string]any, provider notifier.Provider, loadActions ActionSummaryLoader) *NotificationPipeline {
	return &NotificationPipeline{
		name:               name,
		messageTemplate:    messageTemplate,
		threadNameTemplate: threadNameTemplate,
		deliveryConfig:     deliveryConfig,
		provider:           provider,
		loadActions:        loadActions,
	}
}
```

- [ ] **Step 2: Inject rendered thread name into per-call config copy in `Run()`**

In `Run()`, after the block that builds the per-call `cfg` map (around line 114) and before the `provider.Deliver` call, add:

```go
if p.threadNameTemplate != "" {
	rendered := substituteVars(p.threadNameTemplate, input.Context, escaper)
	runes := []rune(rendered)
	if len(runes) > 100 {
		runes = runes[:100]
	}
	cfg["thread_name"] = string(runes)
}
```

- [ ] **Step 3: Fix all existing `NewNotificationPipeline` call sites in `notification_test.go`**

Every existing call in `internal/pipeline/notification_test.go` passes 5 arguments. Add `""` as the third argument (threadNameTemplate) to each call. For example:

```go
// Before:
p := NewNotificationPipeline("test", "Hello ${author}, tweet: ${tweet}", map[string]any{"priority": 1}, mp, nil)
// After:
p := NewNotificationPipeline("test", "Hello ${author}, tweet: ${tweet}", "", map[string]any{"priority": 1}, mp, nil)
```

Apply this change to every `NewNotificationPipeline(` call in the test file (there are ~15 of them).

- [ ] **Step 4: Add a test for thread name rendering**

Add this test at the end of `internal/pipeline/notification_test.go`:

```go
func TestNotificationPipeline_ThreadNameTemplate_RenderedAndInjected(t *testing.T) {
	mp := &mockProvider{}
	p := NewNotificationPipeline("test", "body ${tweet}", "[${category}] ${author}", nil, mp, nil)
	input := inference.PipelineInput{
		Context: map[string]any{
			"tweet":    "Iran protests",
			"author":   "reporter1",
			"category": "protest",
		},
	}
	_, err := p.Run(context.Background(), input)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	got, ok := mp.lastConfig["thread_name"]
	if !ok {
		t.Fatal("expected thread_name in delivery config")
	}
	if got != "[protest] reporter1" {
		t.Errorf("thread_name: got %q, want %q", got, "[protest] reporter1")
	}
}

func TestNotificationPipeline_ThreadNameTemplate_TruncatedAt100Runes(t *testing.T) {
	mp := &mockProvider{}
	long := strings.Repeat("آ", 150) // 150 runes (multi-byte)
	p := NewNotificationPipeline("test", "body", long, nil, mp, nil)
	_, err := p.Run(context.Background(), inference.PipelineInput{Context: map[string]any{}})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	got, _ := mp.lastConfig["thread_name"].(string)
	if len([]rune(got)) != 100 {
		t.Errorf("expected 100 runes, got %d", len([]rune(got)))
	}
}

func TestNotificationPipeline_ThreadNameTemplate_EmptyTemplate_NotInjected(t *testing.T) {
	mp := &mockProvider{}
	p := NewNotificationPipeline("test", "body", "", nil, mp, nil)
	_, err := p.Run(context.Background(), inference.PipelineInput{Context: map[string]any{}})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if _, ok := mp.lastConfig["thread_name"]; ok {
		t.Error("expected thread_name absent when threadNameTemplate is empty")
	}
}
```

- [ ] **Step 5: Run tests**

```bash
docker compose build iran-watcher 2>&1 | tail -5
```

Expected: build succeeds (compilation check — no easy way to run unit tests natively due to CGO).

- [ ] **Step 6: Commit**

```bash
git add internal/pipeline/notification.go internal/pipeline/notification_test.go
git commit -m "feat(pipeline): add threadNameTemplate to NotificationPipeline"
```

---

### Task 2: Update `DiscordBotClient.deliverRoot()` to read `thread_name` from config

**Files:**
- Modify: `internal/notifier/discord.go`
- Test: `internal/notifier/discord_test.go`

- [ ] **Step 1: Update `Deliver()` to pass config to `deliverRoot()`**

Currently `deliverRoot` only receives `text` and `appliedTagIDs`. Update its signature to also accept the config map, so it can read `thread_name`:

In `internal/notifier/discord.go`, change the `deliverRoot` signature:

```go
// deliverRoot creates a new Forum thread via POST /channels/{forum_channel_id}/threads.
// Returns {"discord_thread_id": "<id>"} from the Discord response.
func (c *DiscordBotClient) deliverRoot(ctx context.Context, text string, appliedTagIDs []string, config map[string]any) (map[string]any, error) {
	msgBody := extractEmbedBody(text)

	threadName, _ := config["thread_name"].(string)

	reqBody := map[string]any{
		"name":    threadName,
		"message": msgBody,
	}
	if len(appliedTagIDs) > 0 {
		reqBody["applied_tags"] = appliedTagIDs
	}
	// ... rest of function unchanged ...
```

And update the call site in `Deliver()`:

```go
return c.deliverRoot(ctx, text, appliedTagIDs, config)
```

- [ ] **Step 2: Delete `threadNameFromText` helper**

In `internal/notifier/discord.go`, delete the `threadNameFromText` function (lines ~444–458):

```go
// DELETE these lines entirely:
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
```

- [ ] **Step 3: Update `TestDiscordBotClient_CreateThread_ReturnsThreadID` to supply `thread_name`**

In `internal/notifier/discord_test.go`, the existing test calls `c.Deliver(...)` with an empty config map `map[string]any{}`. Update it to pass `thread_name` and verify it appears in the request body:

```go
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
		if body["name"] != "My Thread" {
			t.Errorf("expected thread name 'My Thread', got %v", body["name"])
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
	out, err := c.Deliver(context.Background(), "hello world", map[string]any{
		"thread_name": "My Thread",
	})
	if err != nil {
		t.Fatalf("Deliver: %v", err)
	}
	if out["discord_thread_id"] != "thread123" {
		t.Errorf("discord_thread_id: got %v, want thread123", out["discord_thread_id"])
	}
}
```

- [ ] **Step 4: Build to verify**

```bash
docker compose build iran-watcher 2>&1 | tail -5
```

Expected: build succeeds.

- [ ] **Step 5: Commit**

```bash
git add internal/notifier/discord.go internal/notifier/discord_test.go
git commit -m "feat(notifier): read thread_name from config in DiscordBotClient; remove threadNameFromText"
```

---

### Task 3: Update `buildNotificationPipeline()` to extract and validate `thread_name_template`

**Files:**
- Modify: `internal/worker/action_cache.go`

- [ ] **Step 1: Extract `thread_name_template` and validate for Discord**

In `buildNotificationPipeline()` in `internal/worker/action_cache.go`, after the line:

```go
messageTemplate, _ := pipelineCfg["message_template"].(string)
```

Add:

```go
threadNameTemplate, _ := pipelineCfg["thread_name_template"].(string)
```

In the `deliveryConfig` exclusion loop (around line 224), add `"thread_name_template"` to the skip list:

```go
if k == "channel_id" || k == "message_template" || k == "thread_name_template" {
    continue
}
```

After the Discord provider is constructed (after line 272, `notifProvider = notifier.NewDiscordBotClient(...)`), add validation:

```go
if threadNameTemplate == "" {
    return nil, fmt.Errorf("action cache: discord notification action missing thread_name_template")
}
```

- [ ] **Step 2: Pass `threadNameTemplate` to `NewNotificationPipeline`**

Update the return statement at line 294:

```go
return pipeline.NewNotificationPipeline(a.Name, messageTemplate, threadNameTemplate, deliveryConfig, notifProvider, loader), nil
```

- [ ] **Step 3: Build to verify**

```bash
docker compose build iran-watcher 2>&1 | tail -5
```

Expected: build succeeds.

- [ ] **Step 4: Commit**

```bash
git add internal/worker/action_cache.go
git commit -m "feat(worker): extract thread_name_template from Discord notification action config"
```

---

### Task 4: Update the UI to show and validate `thread_name_template`

**Files:**
- Modify: `web/src/components/tabs/ActionsTab.vue`

- [ ] **Step 1: Add the `formThreadNameTemplate` ref**

In `ActionsTab.vue`, find the block of notification-specific refs near line 147:

```typescript
const formChannelId = ref<number | null>(null)
const formMessageTemplate = ref('')
```

Add after `formMessageTemplate`:

```typescript
const formThreadNameTemplate = ref('')
```

- [ ] **Step 2: Reset the ref in `resetForm()`**

In the `resetForm()` function, after:

```typescript
formMessageTemplate.value = '${tweet}'
```

Add:

```typescript
formThreadNameTemplate.value = ''
```

- [ ] **Step 3: Populate the ref in `openEditAction()`**

In `openEditAction()`, after:

```typescript
formMessageTemplate.value = row.config?.message_template ?? '${tweet}'
```

Add:

```typescript
formThreadNameTemplate.value = row.config?.thread_name_template ?? ''
```

- [ ] **Step 4: Validate and save in `onSaveAction()`**

In `onSaveAction()`, inside the `formType.value === 'notification'` branch, after the existing Discord provider check (find the `else if (notifProviderType.value === 'telegram')` block which ends around line 414), add a Discord-specific section:

```typescript
} else if (notifProviderType.value === 'discord') {
  if (!formThreadNameTemplate.value.trim()) {
    msg.error('Thread name template is required for Discord')
    return
  }
  config.thread_name_template = formThreadNameTemplate.value.trim()
}
```

- [ ] **Step 5: Render the field in the notification form template**

In the notification template section (around line 826), after the Message Template `<n-form-item>` block and its hint text, add:

```html
<template v-if="notifProviderType === 'discord'">
  <n-form-item label="Thread Name Template">
    <n-input
      v-model:value="formThreadNameTemplate"
      placeholder="e.g. [${category}] ${author}"
      style="font-family: monospace; font-size: 12px"
    />
  </n-form-item>
  <n-text depth="3" style="font-size:12px; margin-top:-8px; display:block">
    Available vars: ${tweet}, ${author}, ${tweet_id}, plus any action execution key. Max 100 chars.
  </n-text>
</template>
```

- [ ] **Step 6: Build to verify**

```bash
docker compose build iran-watcher 2>&1 | tail -5
```

Expected: build succeeds.

- [ ] **Step 7: Commit**

```bash
git add web/src/components/tabs/ActionsTab.vue
git commit -m "feat(ui): add Discord thread name template field to notification action form"
```

---

### Task 5: Update CLAUDE.md

**Files:**
- Modify: `CLAUDE.md`

- [ ] **Step 1: Update the Discord notification bullet in Key Design Decisions**

Find the bullet point describing Discord notifications (search for `discord_tag_thread`). In the section describing `NotificationChannel` and action config keys, add a note about `thread_name_template`:

In the Discord thread tagging section or the notifications section, add:

```
`thread_name_template` (required for Discord notification actions) — a `${key}`-substitution template rendered at delivery time to set the Forum thread name; truncated to 100 runes; validated at `ActionCache` build time
```

- [ ] **Step 2: Commit**

```bash
git add CLAUDE.md
git commit -m "docs: document thread_name_template in Discord notification design decisions"
```
