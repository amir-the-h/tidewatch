package pipeline

import (
	"context"
	"errors"
	"testing"

	"github.com/amir-the-h/tidewatch/internal/inference"
	"github.com/amir-the-h/tidewatch/internal/notifier"
)

// mockDiscordTagger implements discordTagger for tests.
type mockDiscordTagger struct {
	tags            []notifier.ForumTag
	tagsErr         error
	addForumTagErr  error
	addedTagNames   []string
	patchedThreadID string
	patchedTagIDs   []string
	patchErr        error
}

func (m *mockDiscordTagger) GetAvailableTags(_ context.Context) ([]notifier.ForumTag, error) {
	return m.tags, m.tagsErr
}

func (m *mockDiscordTagger) AddForumTag(_ context.Context, name string) (notifier.ForumTag, error) {
	if m.addForumTagErr != nil {
		return notifier.ForumTag{}, m.addForumTagErr
	}
	m.addedTagNames = append(m.addedTagNames, name)
	tag := notifier.ForumTag{ID: "new-" + name, Name: name}
	m.tags = append(m.tags, tag)
	return tag, nil
}

func (m *mockDiscordTagger) PatchThreadTags(_ context.Context, threadID string, tagIDs []string) error {
	m.patchedThreadID = threadID
	m.patchedTagIDs = tagIDs
	return m.patchErr
}

func TestDiscordTagThreadPipeline_AppliesTags(t *testing.T) {
	mock := &mockDiscordTagger{
		tags: []notifier.ForumTag{
			{ID: "111", Name: "Military"},
			{ID: "222", Name: "Diplomacy"},
		},
	}
	p := &DiscordTagThreadPipeline{name: "test", client: mock, tagNamesKey: "discord_tag_names"}

	out, err := p.Run(context.Background(), inference.PipelineInput{
		Context: map[string]any{
			"discord_thread_id": "thread123",
			"discord_tag_names": []any{"military", "diplomacy"},
		},
	})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if out["tag_count"] != 2 {
		t.Errorf("tag_count: got %v, want 2", out["tag_count"])
	}
	if mock.patchedThreadID != "thread123" {
		t.Errorf("patched thread ID: got %q, want thread123", mock.patchedThreadID)
	}
	if len(mock.patchedTagIDs) != 2 || mock.patchedTagIDs[0] != "111" || mock.patchedTagIDs[1] != "222" {
		t.Errorf("patched tag IDs: got %v, want [111 222]", mock.patchedTagIDs)
	}
}

func TestDiscordTagThreadPipeline_AppliesTags_StringSlice(t *testing.T) {
	mock := &mockDiscordTagger{
		tags: []notifier.ForumTag{
			{ID: "111", Name: "Military"},
			{ID: "222", Name: "Diplomacy"},
		},
	}
	p := &DiscordTagThreadPipeline{name: "test", client: mock, tagNamesKey: "discord_tag_names"}

	out, err := p.Run(context.Background(), inference.PipelineInput{
		Context: map[string]any{
			"discord_thread_id": "thread123",
			"discord_tag_names": []string{"military", "diplomacy"},
		},
	})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if out["tag_count"] != 2 {
		t.Errorf("tag_count: got %v, want 2", out["tag_count"])
	}
	if len(mock.patchedTagIDs) != 2 || mock.patchedTagIDs[0] != "111" || mock.patchedTagIDs[1] != "222" {
		t.Errorf("patched tag IDs: got %v, want [111 222]", mock.patchedTagIDs)
	}
}

func TestDiscordTagThreadPipeline_CaseInsensitiveMatch(t *testing.T) {
	mock := &mockDiscordTagger{
		tags: []notifier.ForumTag{
			{ID: "111", Name: "Military"},
		},
	}
	p := &DiscordTagThreadPipeline{name: "test", client: mock, tagNamesKey: "discord_tag_names"}

	out, err := p.Run(context.Background(), inference.PipelineInput{
		Context: map[string]any{
			"discord_thread_id": "t1",
			"discord_tag_names": []any{"MILITARY"},
		},
	})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if out["tag_count"] != 1 {
		t.Errorf("tag_count: got %v, want 1", out["tag_count"])
	}
}

func TestDiscordTagThreadPipeline_UnknownTagsSkipped(t *testing.T) {
	mock := &mockDiscordTagger{
		tags: []notifier.ForumTag{
			{ID: "111", Name: "military"},
		},
		// Simulate a forum channel that refuses tag creation (e.g. 403 or tag limit reached).
		// AddForumTag errors are non-fatal; the unknown tag should be silently skipped.
		addForumTagErr: errors.New("tag creation refused"),
	}
	p := &DiscordTagThreadPipeline{name: "test", client: mock, tagNamesKey: "discord_tag_names"}

	out, err := p.Run(context.Background(), inference.PipelineInput{
		Context: map[string]any{
			"discord_thread_id": "t1",
			"discord_tag_names": []any{"military", "unknown-tag"},
		},
	})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if out["tag_count"] != 1 {
		t.Errorf("tag_count: got %v, want 1 (unknown skipped)", out["tag_count"])
	}
	if len(mock.patchedTagIDs) != 1 || mock.patchedTagIDs[0] != "111" {
		t.Errorf("patched tag IDs: got %v, want [111]", mock.patchedTagIDs)
	}
}

func TestDiscordTagThreadPipeline_MissingThreadID_Errors(t *testing.T) {
	p := &DiscordTagThreadPipeline{name: "test", client: &mockDiscordTagger{}, tagNamesKey: "discord_tag_names"}

	_, err := p.Run(context.Background(), inference.PipelineInput{
		Context: map[string]any{"discord_tag_names": []any{"military"}},
	})
	if err == nil {
		t.Fatal("expected error when discord_thread_id missing")
	}
}

func TestDiscordTagThreadPipeline_EmptyTagNames_SkipsPatch(t *testing.T) {
	mock := &mockDiscordTagger{}
	p := &DiscordTagThreadPipeline{name: "test", client: mock, tagNamesKey: "discord_tag_names"}

	out, err := p.Run(context.Background(), inference.PipelineInput{
		Context: map[string]any{
			"discord_thread_id": "t1",
			// discord_tag_names absent
		},
	})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if out["tag_count"] != 0 {
		t.Errorf("tag_count: got %v, want 0", out["tag_count"])
	}
	if mock.patchedThreadID != "" {
		t.Error("PatchThreadTags should not be called when tag list is empty")
	}
}

func TestDiscordTagThreadPipeline_GetTagsError_Propagates(t *testing.T) {
	mock := &mockDiscordTagger{tagsErr: errors.New("network error")}
	// Unique forum ID so a tag cache populated by earlier tests can't satisfy the lookup.
	p := &DiscordTagThreadPipeline{name: "test", client: mock, tagNamesKey: "discord_tag_names", forumChannelID: t.Name()}

	_, err := p.Run(context.Background(), inference.PipelineInput{
		Context: map[string]any{
			"discord_thread_id": "t1",
			"discord_tag_names": []any{"military"},
		},
	})
	if err == nil {
		t.Fatal("expected error when GetAvailableTags fails")
	}
}

func TestDiscordTagThreadPipeline_PatchError_Propagates(t *testing.T) {
	mock := &mockDiscordTagger{
		tags:     []notifier.ForumTag{{ID: "111", Name: "military"}},
		patchErr: errors.New("forbidden"),
	}
	p := &DiscordTagThreadPipeline{name: "test", client: mock, tagNamesKey: "discord_tag_names"}

	_, err := p.Run(context.Background(), inference.PipelineInput{
		Context: map[string]any{
			"discord_thread_id": "t1",
			"discord_tag_names": []any{"military"},
		},
	})
	if err == nil {
		t.Fatal("expected error when PatchThreadTags fails")
	}
}

func TestDiscordTagThreadPipeline_TypeAndName(t *testing.T) {
	p := &DiscordTagThreadPipeline{name: "my-tagger", client: &mockDiscordTagger{}, tagNamesKey: "discord_tag_names"}
	if p.Name() != "my-tagger" {
		t.Errorf("Name: got %q, want my-tagger", p.Name())
	}
	if p.Type() != inference.PipelineTypeInternal {
		t.Errorf("Type: got %q, want internal", p.Type())
	}
	if err := p.Close(); err != nil {
		t.Errorf("Close: got %v, want nil", err)
	}
}

// The factory receives pre-injected credentials (resolved by action_cache from
// the linked notification channel + provider). These tests simulate that injection.

func TestNewDiscordTagThreadFactory_MissingBotToken_Errors(t *testing.T) {
	f := NewDiscordTagThreadFactory()
	// Simulate action_cache failing to inject bot_token (channel missing bot_token).
	_, err := f("test", map[string]any{"forum_channel_id": "123"})
	if err == nil {
		t.Fatal("expected error for missing bot_token")
	}
}

func TestNewDiscordTagThreadFactory_MissingForumChannelID_Errors(t *testing.T) {
	f := NewDiscordTagThreadFactory()
	// Simulate action_cache failing to inject forum_channel_id (channel missing it).
	_, err := f("test", map[string]any{"bot_token": "Bot abc"})
	if err == nil {
		t.Fatal("expected error for missing forum_channel_id")
	}
}

func TestNewDiscordTagThreadFactory_DefaultTagNamesKey(t *testing.T) {
	f := NewDiscordTagThreadFactory()
	// Simulate pre-injected credentials from action_cache.
	p, err := f("test", map[string]any{"bot_token": "Bot abc", "forum_channel_id": "123"})
	if err != nil {
		t.Fatalf("factory: %v", err)
	}
	dp, ok := p.(*DiscordTagThreadPipeline)
	if !ok {
		t.Fatal("expected *DiscordTagThreadPipeline")
	}
	if dp.tagNamesKey != "discord_tag_names" {
		t.Errorf("default tagNamesKey: got %q, want discord_tag_names", dp.tagNamesKey)
	}
}
