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
// Returns {"tags_applied": []string of tag IDs, "tag_count": int}.
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
