package notifier

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"time"
)

// EscapeMarkdownV2 escapes all MarkdownV2-reserved characters in s so it can
// be safely embedded as literal text inside a MarkdownV2-formatted message.
// The template's intentional formatting is left untouched; only substituted
// variable values should be passed through this function.
// Reserved characters: _ * [ ] ( ) ~ ` > # + - = | { } . ! \
func EscapeMarkdownV2(s string) string {
	var b strings.Builder
	b.Grow(len(s))
	for _, r := range s {
		switch r {
		case '_', '*', '[', ']', '(', ')', '~', '`', '>', '#', '+', '-', '=', '|', '{', '}', '.', '!', '\\':
			b.WriteByte('\\')
		}
		b.WriteRune(r)
	}
	return b.String()
}

// EscapeHTML escapes HTML special characters in s so it can be safely embedded
// as plain text inside an HTML-formatted Telegram message.
// Escapes: & → &amp;   < → &lt;   > → &gt;
func EscapeHTML(s string) string {
	s = strings.ReplaceAll(s, "&", "&amp;")
	s = strings.ReplaceAll(s, "<", "&lt;")
	s = strings.ReplaceAll(s, ">", "&gt;")
	return s
}

// EscapeMarkdown escapes reserved characters for Telegram's legacy Markdown
// parse mode: _ * ` [
func EscapeMarkdown(s string) string {
	var b strings.Builder
	b.Grow(len(s))
	for _, r := range s {
		switch r {
		case '_', '*', '`', '[':
			b.WriteByte('\\')
		}
		b.WriteRune(r)
	}
	return b.String()
}

// ParseModeEscaper returns the value-escaping function for the given Telegram
// parse_mode string. Returns nil when no escaping is needed (empty / unknown mode).
func ParseModeEscaper(parseMode string) func(string) string {
	switch parseMode {
	case "MarkdownV2":
		return EscapeMarkdownV2
	case "HTML":
		return EscapeHTML
	case "Markdown":
		return EscapeMarkdown
	default:
		return nil
	}
}

// TelegramMessage holds the parameters for a Telegram sendMessage call.
type TelegramMessage struct {
	ChatID                string
	Text                  string
	ParseMode             string // "HTML", "Markdown", "MarkdownV2", or ""
	DisableWebPagePreview bool
	DisableNotification   bool
	MessageThreadID       int64 // 0 means not set
	ReplyToMessageID      int64 // 0 means not set; sets reply_to_message_id on sendMessage
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

// Send posts a message to a Telegram chat and returns the assigned message_id.
func (c *TelegramClient) Send(ctx context.Context, msg TelegramMessage) (int64, error) {
	if msg.ChatID == "" {
		return 0, fmt.Errorf("notifier: telegram: chat_id is required")
	}
	if msg.Text == "" {
		return 0, fmt.Errorf("notifier: telegram: message text is empty")
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
	if msg.ReplyToMessageID != 0 {
		body["reply_to_message_id"] = msg.ReplyToMessageID
	}

	payload, err := json.Marshal(body)
	if err != nil {
		return 0, fmt.Errorf("notifier: telegram: marshal body: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost,
		c.apiBase+"/sendMessage", bytes.NewReader(payload))
	if err != nil {
		return 0, fmt.Errorf("notifier: telegram: build request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return 0, fmt.Errorf("notifier: telegram: http: %w", err)
	}
	defer resp.Body.Close()

	var result struct {
		OK          bool   `json:"ok"`
		Description string `json:"description"`
		Result      struct {
			MessageID int64 `json:"message_id"`
		} `json:"result"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return 0, fmt.Errorf("notifier: telegram: decode response: %w", err)
	}
	if !result.OK {
		return 0, fmt.Errorf("notifier: telegram: API error: %s", result.Description)
	}

	return result.Result.MessageID, nil
}

// EditMessage updates the text of an existing Telegram message via editMessageText.
func (c *TelegramClient) EditMessage(ctx context.Context, msg TelegramMessage, messageID int64) error {
	if msg.ChatID == "" {
		return fmt.Errorf("notifier: telegram: chat_id is required")
	}
	if msg.Text == "" {
		return fmt.Errorf("notifier: telegram: message text is empty")
	}

	body := map[string]any{
		"chat_id":    msg.ChatID,
		"message_id": messageID,
		"text":       msg.Text,
	}
	if msg.ParseMode != "" {
		body["parse_mode"] = msg.ParseMode
	}
	if msg.DisableWebPagePreview {
		body["disable_web_page_preview"] = true
	}

	payload, err := json.Marshal(body)
	if err != nil {
		return fmt.Errorf("notifier: telegram: marshal body: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost,
		c.apiBase+"/editMessageText", bytes.NewReader(payload))
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
// config keys: "chat_id", "parse_mode", "disable_web_page_preview", "disable_notification",
// "message_thread_id", "telegram_message_id" (if set, edits that message instead of sending a new one),
// "reply_to_message_id" (if set and no telegram_message_id, sends as a reply to that message).
// Returns {"telegram_message_id": <id>} so the caller can persist it for future edit runs.
func (c *TelegramClient) Deliver(ctx context.Context, text string, config map[string]any) (map[string]any, error) {
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

	// If a prior execution already sent this message, edit it instead.
	if existingID := parseTelegramMessageID(config["telegram_message_id"]); existingID != 0 {
		if err := c.EditMessage(ctx, msg, existingID); err != nil {
			return nil, fmt.Errorf("notifier: telegram: %w", err)
		}
		return map[string]any{"telegram_message_id": existingID}, nil
	}

	// Reply to a prior notification for this signal if one exists, forming a thread.
	if replyTo := parseTelegramMessageID(config["reply_to_message_id"]); replyTo != 0 {
		msg.ReplyToMessageID = replyTo
	}

	messageID, err := c.Send(ctx, msg)
	if err != nil {
		return nil, fmt.Errorf("notifier: telegram: %w", err)
	}
	return map[string]any{"telegram_message_id": messageID}, nil
}

// parseTelegramMessageID coerces the stored telegram_message_id value (which may
// be an int64, float64, or JSON-serialised string like "12345") into an int64.
func parseTelegramMessageID(v any) int64 {
	switch id := v.(type) {
	case int64:
		return id
	case float64:
		return int64(id)
	case string:
		n, _ := strconv.ParseInt(id, 10, 64)
		return n
	}
	return 0
}
