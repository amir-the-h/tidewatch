package pipeline

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strconv"
	"strings"

	"github.com/amir-the-h/iran-watcher/internal/inference"
	"github.com/amir-the-h/iran-watcher/internal/notifier"
)

// ActionSummary holds the aggregated execution metadata for a single action on a tweet.
// It is used to build the ${actions} template variable.
type ActionSummary struct {
	ActionName string
	Metadata   map[string]any
}

// ActionSummaryLoader is a callback that fetches all ActionSummary records for a tweet.
// Injected at construction time so the pipeline does not depend directly on the store.
// A nil loader disables ${actions} expansion.
type ActionSummaryLoader func(ctx context.Context, tweetID string) ([]ActionSummary, error)

// internalMetaKeys are metadata keys produced by pipeline infrastructure (not domain data)
// and are excluded from the ${actions} display block.
var internalMetaKeys = map[string]bool{
	"sent":                true,
	"provider":            true,
	"telegram_message_id": true,
	"raw":                 true,
	"truncated":           true,
}

// NotificationPipeline delivers a rendered notification message via a Provider.
// It performs ${key} substitution on messageTemplate using the input context map,
// then calls provider.Deliver with the merged deliveryConfig (channel routing + pipeline options).
type NotificationPipeline struct {
	name            string
	messageTemplate string
	deliveryConfig  map[string]any
	provider        notifier.Provider
	loadActions     ActionSummaryLoader // nil = ${actions} expansion disabled
}

// NewNotificationPipeline constructs a NotificationPipeline.
// deliveryConfig is the merged map of channel routing keys and pipeline delivery options.
// loadActions may be nil; if non-nil it is called to expand ${actions} in the template.
func NewNotificationPipeline(name, messageTemplate string, deliveryConfig map[string]any, provider notifier.Provider, loadActions ActionSummaryLoader) *NotificationPipeline {
	return &NotificationPipeline{
		name:            name,
		messageTemplate: messageTemplate,
		deliveryConfig:  deliveryConfig,
		provider:        provider,
		loadActions:     loadActions,
	}
}

// Name implements inference.Pipeline.
func (p *NotificationPipeline) Name() string { return p.name }

// Type implements inference.Pipeline.
func (p *NotificationPipeline) Type() inference.PipelineType { return inference.PipelineTypeNotification }

// Close implements inference.Pipeline.
func (p *NotificationPipeline) Close() error { return nil }

// Run substitutes template variables and delivers the message.
// Variable values are escaped according to the delivery config's parse_mode
// (MarkdownV2, HTML, or Markdown) so template markup is preserved while
// user-supplied content cannot break the formatting.
//
// If input.Context contains a "telegram_message_id" key from a prior execution,
// it is forwarded to the provider so it can edit the existing message instead
// of sending a new one.
//
// If the template contains ${actions} and a loadActions callback was provided,
// that variable is replaced with a Telegram expandable-block summary of every
// action that ran on the tweet, formatted for the configured parse_mode.
//
// Returns the base output {"sent": true, "provider": name} merged with any
// provider-assigned metadata (e.g. {"telegram_message_id": 12345}).
func (p *NotificationPipeline) Run(ctx context.Context, input inference.PipelineInput) (map[string]any, error) {
	pm, _ := p.deliveryConfig["parse_mode"].(string)
	var escaper func(string) string
	if pm != "" {
		escaper = notifier.ParseModeEscaper(pm)
	}

	// Expand ${actions} directly into the template string before substituteVars runs.
	// The block already contains parse-mode markup (e.g. **>*bold*) that must not
	// be passed through the escaper — raw string replacement bypasses it entirely.
	tmpl := p.messageTemplate
	if p.loadActions != nil && strings.Contains(tmpl, "${actions}") {
		tweetID, _ := input.Context["tweet_id"].(string)
		block := ""
		if tweetID != "" {
			summaries, err := p.loadActions(ctx, tweetID)
			if err == nil {
				block = formatActionsBlock(summaries, pm)
			}
		}
		tmpl = strings.ReplaceAll(tmpl, "${actions}", block)
	}

	text := substituteVars(tmpl, input.Context, escaper)

	// Build a per-call delivery config so we never mutate the stored map.
	cfg := make(map[string]any, len(p.deliveryConfig)+1)
	for k, v := range p.deliveryConfig {
		cfg[k] = v
	}
	// Forward the previous message_id (if any) so the provider can edit instead of resend.
	if v, ok := input.Context["telegram_message_id"]; ok {
		cfg["telegram_message_id"] = v
	}

	providerMeta, err := p.provider.Deliver(ctx, text, cfg)
	if err != nil {
		return nil, fmt.Errorf("notification pipeline: deliver: %w", err)
	}

	out := map[string]any{
		"sent":     true,
		"provider": p.name,
	}
	for k, v := range providerMeta {
		out[k] = v
	}
	return out, nil
}

// substituteVars replaces ${key} placeholders in tmpl with values from ctx.
// Strings are used as-is; complex types (slices, maps) are rendered as compact
// JSON so they appear human-readable rather than as Go's default %v output.
// If escaper is non-nil, each rendered value is passed through it before
// substitution so user-supplied content cannot break formatted messages.
// Unknown keys are left as-is. After substitution, inline empty parentheticals
// like " ()" are removed and lines whose only value resolved to empty are dropped.
func substituteVars(tmpl string, ctx map[string]any, escaper func(string) string) string {
	for k, v := range ctx {
		val := renderValue(v)
		if escaper != nil {
			val = escaper(val)
		}
		tmpl = strings.ReplaceAll(tmpl, "${"+k+"}", val)
	}

	// Remove inline empty parentheticals: "foo ()" → "foo".
	tmpl = strings.ReplaceAll(tmpl, " ()", "")

	// Drop lines where every substituted value resolved to empty, leaving
	// only a trailing colon (e.g. "🎯 Perpetrator:").
	lines := strings.Split(tmpl, "\n")
	kept := lines[:0]
	for _, line := range lines {
		if strings.HasSuffix(strings.TrimRight(line, " \t"), ":") {
			continue
		}
		kept = append(kept, line)
	}
	return strings.Join(kept, "\n")
}

// renderValue converts a context value to a display string.
// nil (JSON null) becomes an empty string so null fields don't print literally.
// Strings pass through unchanged; everything else is JSON-encoded.
func renderValue(v any) string {
	if v == nil {
		return ""
	}
	if s, ok := v.(string); ok {
		return s
	}
	b, err := json.Marshal(v)
	if err != nil {
		return fmt.Sprintf("%v", v)
	}
	return string(b)
}

// Ensure NotificationPipeline satisfies inference.Pipeline at compile time.
var _ inference.Pipeline = (*NotificationPipeline)(nil)

// ── ${actions} block formatting ───────────────────────────────────────────────

// formatActionsBlock produces a string that lists each action summary as a
// Telegram expandable blockquote. The output is formatted for parseMode
// ("MarkdownV2", "HTML", or plain text when empty).
// Values inside the block are NOT re-escaped by the caller's escaper — they are
// escaped here using the parse-mode-specific function so the block markup itself
// is preserved correctly.
func formatActionsBlock(summaries []ActionSummary, parseMode string) string {
	escaper := notifier.ParseModeEscaper(parseMode)
	var blocks []string
	for _, s := range summaries {
		block := formatOneActionBlock(s, parseMode, escaper)
		if block != "" {
			blocks = append(blocks, block)
		}
	}
	return strings.Join(blocks, "\n\n")
}

func formatOneActionBlock(s ActionSummary, parseMode string, escaper func(string) string) string {
	// Collect displayable (key, renderedValue) pairs, sorted by key.
	keys := make([]string, 0, len(s.Metadata))
	for k := range s.Metadata {
		if !internalMetaKeys[k] {
			keys = append(keys, k)
		}
	}
	sort.Strings(keys)

	var lines []string
	for _, k := range keys {
		val := renderMetaDisplayValue(s.Metadata[k])
		if val == "" {
			continue
		}
		displayKey := k
		if escaper != nil {
			displayKey = escaper(k)
			val = escaper(val)
		}
		lines = append(lines, "• "+displayKey+": "+val)
	}

	if len(lines) == 0 {
		return ""
	}

	switch parseMode {
	case "MarkdownV2":
		escapedName := notifier.EscapeMarkdownV2(s.ActionName)
		var sb strings.Builder
		sb.WriteString("**>*" + escapedName + "*")
		for _, line := range lines {
			sb.WriteString("\n**>" + line)
		}
		return sb.String()

	case "HTML":
		escapedName := notifier.EscapeHTML(s.ActionName)
		var sb strings.Builder
		sb.WriteString("<blockquote expandable><b>")
		sb.WriteString(escapedName)
		sb.WriteString("</b>")
		for _, line := range lines {
			sb.WriteString("\n" + line)
		}
		sb.WriteString("</blockquote>")
		return sb.String()

	default:
		var sb strings.Builder
		sb.WriteString("[" + s.ActionName + "]")
		for _, line := range lines {
			sb.WriteString("\n" + line)
		}
		return sb.String()
	}
}

// renderMetaDisplayValue converts a metadata value to a human-readable string
// for use inside the ${actions} block. Returns "" for values that should be skipped
// (false booleans, nil, empty strings, complex nested objects).
func renderMetaDisplayValue(v any) string {
	switch val := v.(type) {
	case string:
		if len(val) > 200 {
			return val[:197] + "..."
		}
		return val
	case float64:
		return strconv.FormatFloat(val, 'f', -1, 64)
	case bool:
		if val {
			return "yes"
		}
		return "" // skip false booleans — their absence is the message
	case []any:
		var parts []string
		for _, item := range val {
			s := renderMetaDisplayValue(item)
			if s != "" {
				parts = append(parts, s)
			}
		}
		joined := strings.Join(parts, ", ")
		if len(joined) > 300 {
			return joined[:297] + "..."
		}
		return joined
	case map[string]any:
		b, err := json.Marshal(val)
		if err != nil || len(b) > 200 {
			return "" // skip large/unserializable objects
		}
		return string(b)
	case nil:
		return ""
	default:
		s := fmt.Sprintf("%v", val)
		if len(s) > 200 {
			return s[:197] + "..."
		}
		return s
	}
}

// Legacy pipeline factories for backward compatibility with existing infrastructure.

// PushoverPipeline sends a Pushover push notification when run.
// The notification message is taken from input.Text.
// Optional config keys: "title", "priority", "sound", "url", "url_title".
type PushoverPipeline struct {
	name   string
	client *notifier.PushoverClient
}

// NewPushoverFactory returns a Factory that creates a PushoverPipeline.
func NewPushoverFactory(client *notifier.PushoverClient) Factory {
	return func(name string, _ map[string]any) (inference.Pipeline, error) {
		return &PushoverPipeline{name: name, client: client}, nil
	}
}

func (p *PushoverPipeline) Name() string                 { return p.name }
func (p *PushoverPipeline) Type() inference.PipelineType { return inference.PipelineTypeInternal }
func (p *PushoverPipeline) Close() error                 { return nil }

func (p *PushoverPipeline) Run(ctx context.Context, input inference.PipelineInput) (map[string]any, error) {
	msg := notifier.PushoverMessage{
		Message: input.Text,
	}
	if v, ok := input.Config["title"].(string); ok {
		msg.Title = v
	}
	if v, ok := input.Config["priority"].(int); ok {
		msg.Priority = v
	} else if v, ok := input.Config["priority"].(float64); ok {
		msg.Priority = int(v)
	}
	if v, ok := input.Config["sound"].(string); ok {
		msg.Sound = v
	}
	if v, ok := input.Config["url"].(string); ok {
		msg.URL = v
	}
	if v, ok := input.Config["url_title"].(string); ok {
		msg.URLTitle = v
	}

	if err := p.client.Send(ctx, msg); err != nil {
		return nil, fmt.Errorf("pushover: send: %w", err)
	}

	return map[string]any{
		"sent": true,
	}, nil
}

// TelegramPipeline sends a Telegram message when run.
// The message text is taken from input.Text.
// Required config key: "chat_id".
// Optional config keys: "parse_mode", "disable_web_page_preview", "disable_notification", "message_thread_id".
type TelegramPipeline struct {
	name   string
	client *notifier.TelegramClient
}

// NewTelegramFactory returns a Factory that creates a TelegramPipeline.
func NewTelegramFactory(client *notifier.TelegramClient) Factory {
	return func(name string, _ map[string]any) (inference.Pipeline, error) {
		return &TelegramPipeline{name: name, client: client}, nil
	}
}

func (p *TelegramPipeline) Name() string                 { return p.name }
func (p *TelegramPipeline) Type() inference.PipelineType { return inference.PipelineTypeInternal }
func (p *TelegramPipeline) Close() error                 { return nil }

func (p *TelegramPipeline) Run(ctx context.Context, input inference.PipelineInput) (map[string]any, error) {
	chatID, _ := input.Config["chat_id"].(string)
	if chatID == "" {
		return nil, fmt.Errorf("telegram: chat_id missing from config")
	}

	msg := notifier.TelegramMessage{
		ChatID: chatID,
		Text:   input.Text,
	}
	if v, ok := input.Config["parse_mode"].(string); ok {
		msg.ParseMode = v
	}
	if v, ok := input.Config["disable_web_page_preview"].(bool); ok {
		msg.DisableWebPagePreview = v
	}
	if v, ok := input.Config["disable_notification"].(bool); ok {
		msg.DisableNotification = v
	}
	if v, ok := input.Config["message_thread_id"].(int64); ok {
		msg.MessageThreadID = v
	} else if v, ok := input.Config["message_thread_id"].(float64); ok {
		msg.MessageThreadID = int64(v)
	}

	if _, err := p.client.Send(ctx, msg); err != nil {
		return nil, fmt.Errorf("telegram: send: %w", err)
	}

	return map[string]any{
		"sent": true,
	}, nil
}
