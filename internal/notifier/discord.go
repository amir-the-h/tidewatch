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

// DiscordClient sends messages to a Discord channel via an incoming webhook URL.
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
// The message text may contain an embedded JSON object anywhere in it.
// If a JSON object is found, it is sent as an embed; any surrounding text
// is sent as the "content" field. Both can appear in the same webhook payload.
// If there is no JSON object the entire text is sent as plain content.
// Discord returns 204 No Content on success.
// Returns {"sent": true} on success.
func (c *DiscordClient) Deliver(ctx context.Context, text string, _ map[string]any) (map[string]any, error) {
	if text == "" {
		return nil, fmt.Errorf("notifier: discord: message text is empty")
	}

	body := extractEmbedBody(text)

	payload, err := json.Marshal(body)
	if err != nil {
		return nil, fmt.Errorf("notifier: discord: marshal body: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.webhookURL, bytes.NewReader(payload))
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
