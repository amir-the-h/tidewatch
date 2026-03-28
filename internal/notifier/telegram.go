package notifier

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"time"
)

// TelegramMessage holds the parameters for a Telegram sendMessage call.
type TelegramMessage struct {
	ChatID                string
	Text                  string
	ParseMode             string // "HTML", "Markdown", "MarkdownV2", or ""
	DisableWebPagePreview bool
	DisableNotification   bool
	MessageThreadID       int64 // 0 means not set
}

// TelegramClient sends messages via the Telegram Bot API.
type TelegramClient struct {
	botToken   string
	apiBase    string
	httpClient *http.Client
	logger     *slog.Logger
}

// NewTelegramClient creates a TelegramClient using the given bot token.
func NewTelegramClient(botToken string, logger *slog.Logger) *TelegramClient {
	return &TelegramClient{
		botToken: botToken,
		apiBase:  "https://api.telegram.org/bot" + botToken,
		httpClient: &http.Client{
			Timeout: 10 * time.Second,
		},
		logger: logger,
	}
}

// Send posts a message to a Telegram chat.
func (c *TelegramClient) Send(ctx context.Context, msg TelegramMessage) error {
	if msg.ChatID == "" {
		return fmt.Errorf("notifier: telegram: chat_id is required")
	}
	if msg.Text == "" {
		return fmt.Errorf("notifier: telegram: message text is empty")
	}

	body := map[string]any{
		"chat_id": msg.ChatID,
		"text":    msg.Text,
	}
	if msg.ParseMode != "" {
		body["parse_mode"] = msg.ParseMode
	}
	if msg.DisableWebPagePreview {
		body["disable_web_page_preview"] = true
	}
	if msg.DisableNotification {
		body["disable_notification"] = true
	}
	if msg.MessageThreadID != 0 {
		body["message_thread_id"] = msg.MessageThreadID
	}

	payload, err := json.Marshal(body)
	if err != nil {
		return fmt.Errorf("notifier: telegram: marshal body: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost,
		c.apiBase+"/sendMessage", bytes.NewReader(payload))
	if err != nil {
		return fmt.Errorf("notifier: telegram: build request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("notifier: telegram: http: %w", err)
	}
	defer resp.Body.Close()

	var result struct {
		OK          bool   `json:"ok"`
		Description string `json:"description"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return fmt.Errorf("notifier: telegram: decode response: %w", err)
	}
	if !result.OK {
		return fmt.Errorf("notifier: telegram: API error: %s", result.Description)
	}

	return nil
}

// Deliver implements notifier.Provider for TelegramClient.
// config keys: "chat_id", "parse_mode", "disable_web_page_preview", "disable_notification", "message_thread_id"
func (c *TelegramClient) Deliver(ctx context.Context, text string, config map[string]any) error {
	msg := TelegramMessage{
		Text: text,
	}
	if v, ok := config["chat_id"].(string); ok {
		msg.ChatID = v
	}
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
