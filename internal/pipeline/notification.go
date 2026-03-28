package pipeline

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/amir-the-h/iran-watcher/internal/inference"
	"github.com/amir-the-h/iran-watcher/internal/model"
	"github.com/amir-the-h/iran-watcher/internal/notifier"
)

// NotificationPipeline delivers a rendered notification message via a Provider.
// It performs ${key} substitution on channel.MessageTemplate using the input context map.
type NotificationPipeline struct {
	name     string
	channel  *model.NotificationChannel
	provider notifier.Provider
}

// NewNotificationPipeline constructs a NotificationPipeline.
func NewNotificationPipeline(name string, channel *model.NotificationChannel, provider notifier.Provider) *NotificationPipeline {
	return &NotificationPipeline{
		name:     name,
		channel:  channel,
		provider: provider,
	}
}

// Name implements inference.Pipeline.
func (p *NotificationPipeline) Name() string { return p.name }

// Type implements inference.Pipeline.
func (p *NotificationPipeline) Type() inference.PipelineType { return inference.PipelineTypeNotification }

// Close implements inference.Pipeline.
func (p *NotificationPipeline) Close() error { return nil }

// Run substitutes template variables, parses channel config, and delivers the message.
// Returns {"sent": true, "provider": name} on success.
func (p *NotificationPipeline) Run(ctx context.Context, input inference.PipelineInput) (map[string]any, error) {
	text := substituteVars(p.channel.MessageTemplate, input.Context)

	var cfg map[string]any
	if len(p.channel.Config) > 0 && string(p.channel.Config) != "null" {
		if err := json.Unmarshal(p.channel.Config, &cfg); err != nil {
			return nil, fmt.Errorf("notification pipeline: parse channel config: %w", err)
		}
	}

	if err := p.provider.Deliver(ctx, text, cfg); err != nil {
		return nil, fmt.Errorf("notification pipeline: deliver: %w", err)
	}

	return map[string]any{
		"sent":     true,
		"provider": p.name,
	}, nil
}

// substituteVars replaces ${key} placeholders in tmpl with values from ctx.
// Unknown keys are left as-is.
func substituteVars(tmpl string, ctx map[string]any) string {
	for k, v := range ctx {
		tmpl = strings.ReplaceAll(tmpl, "${"+k+"}", fmt.Sprintf("%v", v))
	}
	return tmpl
}

// Ensure NotificationPipeline satisfies inference.Pipeline at compile time.
var _ inference.Pipeline = (*NotificationPipeline)(nil)

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

	if err := p.client.Send(ctx, msg); err != nil {
		return nil, fmt.Errorf("telegram: send: %w", err)
	}

	return map[string]any{
		"sent": true,
	}, nil
}
