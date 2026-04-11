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
