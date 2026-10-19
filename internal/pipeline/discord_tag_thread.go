package pipeline

import (
	"context"
	"fmt"
	"strings"
	"sync"

	"github.com/amir-the-h/tidewatch/internal/inference"
	"github.com/amir-the-h/tidewatch/internal/notifier"
)

// discordTagger abstracts the Discord Bot API calls needed by DiscordTagThreadPipeline.
// Satisfied by *notifier.DiscordBotClient; extracted for testability.
type discordTagger interface {
	GetAvailableTags(ctx context.Context) ([]notifier.ForumTag, error)
	AddForumTag(ctx context.Context, name string) (notifier.ForumTag, error)
	PatchThreadTags(ctx context.Context, threadID string, tagIDs []string) error
}

// tagCacheEntry is an in-process cache of Discord Forum tags keyed by forum channel ID.
// The inner map is lowercase(name) → snowflake ID.
var (
	tagCacheMap = map[string]map[string]string{}
	tagCacheMu  sync.RWMutex
)

func cachedTags(channelID string) (map[string]string, bool) {
	tagCacheMu.RLock()
	defer tagCacheMu.RUnlock()
	m, ok := tagCacheMap[channelID]
	return m, ok
}

func updateTagCache(channelID string, nameToID map[string]string) {
	tagCacheMu.Lock()
	defer tagCacheMu.Unlock()
	tagCacheMap[channelID] = nameToID
}

func addToTagCache(channelID, lname, id string) {
	tagCacheMu.Lock()
	defer tagCacheMu.Unlock()
	if tagCacheMap[channelID] == nil {
		tagCacheMap[channelID] = map[string]string{}
	}
	tagCacheMap[channelID][lname] = id
}

// DiscordTagThreadPipeline reads generated tag names from pipeline context,
// resolves them to Discord Forum tag IDs (creating tags that do not yet exist),
// caches the name→ID mapping in-process, and patches the thread.
type DiscordTagThreadPipeline struct {
	name           string
	client         discordTagger
	forumChannelID string
	tagNamesKey    string // context key holding []string of tag names
}

// Name implements inference.Pipeline.
func (p *DiscordTagThreadPipeline) Name() string { return p.name }

// Type implements inference.Pipeline.
func (p *DiscordTagThreadPipeline) Type() inference.PipelineType { return inference.PipelineTypeInternal }

// Close implements inference.Pipeline.
func (p *DiscordTagThreadPipeline) Close() error { return nil }

// Run resolves tag names to IDs, creating any that do not exist in the forum channel,
// then patches the Discord thread.
//
// Reads "discord_thread_id" from input.Context (set by the upstream Discord
// notification action). Reads tag names from input.Context[tagNamesKey]
// (accepts []string or []any of strings).
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

	// Resolve names → IDs, using the in-process cache and creating missing tags.
	nameToID, err := p.resolveNames(ctx, tagNames)
	if err != nil {
		return nil, err
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

// resolveNames returns a lowercase(name)→id map covering all requested names.
// It checks the in-process cache first; on any miss it refreshes from Discord;
// tags still absent after the refresh are created via AddForumTag.
func (p *DiscordTagThreadPipeline) resolveNames(ctx context.Context, names []string) (map[string]string, error) {
	// 1. Check cache.
	nameToID, cached := cachedTags(p.forumChannelID)

	// Determine which names are missing.
	missing := p.missing(names, nameToID)

	// 2. On any cache miss, refresh from Discord.
	if !cached || len(missing) > 0 {
		available, err := p.client.GetAvailableTags(ctx)
		if err != nil {
			return nil, fmt.Errorf("discord_tag_thread: get available tags: %w", err)
		}
		nameToID = make(map[string]string, len(available))
		for _, t := range available {
			nameToID[strings.ToLower(t.Name)] = t.ID
		}
		updateTagCache(p.forumChannelID, nameToID)
		missing = p.missing(names, nameToID)
	}

	// 3. Create any tags that still don't exist.
	for _, name := range missing {
		tag, err := p.client.AddForumTag(ctx, name)
		if err != nil {
			// Non-fatal: log the name and continue — the thread still gets other tags.
			// Returning an error here would abort the entire patch for one bad tag.
			continue
		}
		nameToID[strings.ToLower(tag.Name)] = tag.ID
		addToTagCache(p.forumChannelID, strings.ToLower(tag.Name), tag.ID)
	}

	return nameToID, nil
}

// missing returns the subset of names not present in nameToID (case-insensitive).
func (p *DiscordTagThreadPipeline) missing(names []string, nameToID map[string]string) []string {
	var out []string
	for _, name := range names {
		if _, ok := nameToID[strings.ToLower(name)]; !ok {
			out = append(out, name)
		}
	}
	return out
}

// NewDiscordTagThreadFactory returns a Factory for DiscordTagThreadPipeline.
// Each action gets its own DiscordBotClient constructed from its config.
//
// In production, the action config should contain "channel_id" pointing to a
// Discord notification channel. The action_cache resolves bot_token (from the
// linked provider) and forum_channel_id (from the channel) before calling this
// factory, so those keys are already present in config by the time the factory runs.
//
// Required resolved config keys: "bot_token", "forum_channel_id".
// User-facing config keys: "channel_id", "tag_names_key" (optional, defaults to "discord_tag_names").
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
		return &DiscordTagThreadPipeline{
			name:           name,
			client:         client,
			forumChannelID: forumChannelID,
			tagNamesKey:    tagNamesKey,
		}, nil
	}
}

// Ensure DiscordTagThreadPipeline satisfies inference.Pipeline at compile time.
var _ inference.Pipeline = (*DiscordTagThreadPipeline)(nil)

// Ensure *notifier.DiscordBotClient satisfies discordTagger at compile time.
var _ discordTagger = (*notifier.DiscordBotClient)(nil)
