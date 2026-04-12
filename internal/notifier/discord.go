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
	"sync"
	"time"
)

// discordTokenBucket is a package-level rate limiter shared across all DiscordBotClient
// instances. It allows at most 1 Discord API call per second, preventing 429 floods
// when many workers fire simultaneously during bulk backfills.
var (
	discordTokenBucket     = make(chan struct{}, 1)
	discordTokenBucketOnce sync.Once
)

func initDiscordRateLimiter() {
	discordTokenBucketOnce.Do(func() {
		discordTokenBucket <- struct{}{} // seed one initial token
		go func() {
			ticker := time.NewTicker(time.Second)
			for range ticker.C {
				select {
				case discordTokenBucket <- struct{}{}:
				default: // bucket full, discard
				}
			}
		}()
	})
}

// acquireDiscordToken blocks until a rate-limit token is available or ctx is cancelled.
func acquireDiscordToken(ctx context.Context) error {
	initDiscordRateLimiter()
	select {
	case <-discordTokenBucket:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

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
		httpClient:     &http.Client{Timeout: 30 * time.Second},
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

	return c.deliverRoot(ctx, text, appliedTagIDs, config)
}

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

	payload, err := json.Marshal(reqBody)
	if err != nil {
		return nil, fmt.Errorf("notifier: discord: marshal body: %w", err)
	}

	url := c.baseURL + "/channels/" + c.forumChannelID + "/threads"
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, http.NoBody)
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
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, http.NoBody)
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

// AddForumTag creates a new tag in the Forum channel by appending it to the
// channel's available_tags via PATCH /channels/{forum_channel_id}. Returns the
// newly created ForumTag with its Discord-assigned snowflake ID.
// Existing tags are preserved; only the new one is appended.
func (c *DiscordBotClient) AddForumTag(ctx context.Context, name string) (ForumTag, error) {
	existing, err := c.GetAvailableTags(ctx)
	if err != nil {
		return ForumTag{}, fmt.Errorf("notifier: discord: add forum tag: fetch existing: %w", err)
	}

	// Build the updated tag list: keep all existing (by ID) then append the new one.
	tags := make([]map[string]any, len(existing)+1)
	for i, t := range existing {
		tags[i] = map[string]any{"id": t.ID, "name": t.Name, "moderated": false}
	}
	tags[len(existing)] = map[string]any{"name": name, "moderated": false}

	payload, err := json.Marshal(map[string]any{"available_tags": tags})
	if err != nil {
		return ForumTag{}, fmt.Errorf("notifier: discord: add forum tag: marshal: %w", err)
	}

	url := c.baseURL + "/channels/" + c.forumChannelID
	req, err := http.NewRequestWithContext(ctx, http.MethodPatch, url, http.NoBody)
	if err != nil {
		return ForumTag{}, fmt.Errorf("notifier: discord: add forum tag: build request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bot "+c.botToken)

	resp, err := c.doRequest(ctx, req, payload)
	if err != nil {
		return ForumTag{}, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		respBody, _ := io.ReadAll(resp.Body)
		return ForumTag{}, fmt.Errorf("notifier: discord: add forum tag: status %d: %s", resp.StatusCode, bytes.TrimSpace(respBody))
	}

	var channelResp struct {
		AvailableTags []struct {
			ID   string `json:"id"`
			Name string `json:"name"`
		} `json:"available_tags"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&channelResp); err != nil {
		return ForumTag{}, fmt.Errorf("notifier: discord: add forum tag: decode: %w", err)
	}

	lname := strings.ToLower(name)
	for _, t := range channelResp.AvailableTags {
		if strings.ToLower(t.Name) == lname {
			return ForumTag{ID: t.ID, Name: t.Name}, nil
		}
	}
	return ForumTag{}, fmt.Errorf("notifier: discord: add forum tag: newly created tag %q not found in response", name)
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
	req, err := http.NewRequestWithContext(ctx, http.MethodPatch, url, http.NoBody)
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

// doRequest executes an HTTP request against the Discord API. It first acquires a
// proactive rate-limit token (1 req/sec globally) before sending. If the response is
// 429 Too Many Requests, it honours the Retry-After value and retries up to 9 more
// times (10 total attempts). For nil payload (GET requests), the body is set to
// http.NoBody. Returns the final *http.Response on success or an error.
func (c *DiscordBotClient) doRequest(ctx context.Context, req *http.Request, payload []byte) (*http.Response, error) {
	const maxRetries = 10
	for attempt := 0; attempt < maxRetries; attempt++ {
		// Proactively throttle: wait for a rate-limit token before each attempt.
		// The package-level token bucket allows at most 1 Discord API call per second
		// across all client instances, preventing 429 floods during bulk backfills.
		if err := acquireDiscordToken(ctx); err != nil {
			return nil, err
		}

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
		if ra := resp.Header.Get("Retry-After"); ra != "" {
			if secs, err := strconv.ParseFloat(ra, 64); err == nil {
				delay = time.Duration(secs*1000) * time.Millisecond
			}
		}
		if delay <= 0 {
			delay = 5 * time.Second
		}
		// Discord global rate limits can reach several minutes; cap at 10 minutes
		// before giving up so workers are not blocked indefinitely.
		if delay > 10*time.Minute {
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

// Ensure DiscordBotClient satisfies notifier.Provider at compile time.
var _ Provider = (*DiscordBotClient)(nil)
