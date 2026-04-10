package inference

import (
	"context"
	"fmt"
	"strings"
	"testing"
)

func TestBuildContextBlock_TweetVarsOnly(t *testing.T) {
	tweetVars := map[string]any{
		"tweet_id":     "123",
		"tweet_url":    "https://x.com/user/status/123",
		"tweet_author": "user",
		"tweet_short":  "hello world",
	}
	got := buildContextBlock(tweetVars, nil)
	if !strings.HasPrefix(got, "<context>\n") {
		t.Errorf("expected <context> prefix, got: %s", got)
	}
	if !strings.HasSuffix(got, "</context>") {
		t.Errorf("expected </context> suffix, got: %s", got)
	}
	for _, want := range []string{"tweet_id: 123", "tweet_url: https://x.com/user/status/123", "tweet_author: user", "tweet_short: hello world"} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %q in output:\n%s", want, got)
		}
	}
}

func TestBuildContextBlock_ScalarMetadata(t *testing.T) {
	summaries := []ContextSummary{
		{ActionID: 1, ActionName: "Classifier", Metadata: map[string]any{
			"top_label":  "violence",
			"confidence": 0.97,
			"flagged":    true,
		}},
	}
	got := buildContextBlock(map[string]any{}, summaries)
	if !strings.Contains(got, "Classifier:") {
		t.Errorf("expected action section header")
	}
	if !strings.Contains(got, "  top_label: violence") {
		t.Errorf("expected top_label line")
	}
	if !strings.Contains(got, "  confidence: 0.97") {
		t.Errorf("expected confidence line")
	}
	if !strings.Contains(got, "  flagged: true") {
		t.Errorf("expected flagged line")
	}
}

func TestBuildContextBlock_NestedObject(t *testing.T) {
	summaries := []ContextSummary{
		{ActionID: 2, ActionName: "EmbedGen", Metadata: map[string]any{
			"discord_embed": map[string]any{"color": float64(123), "title": "Test"},
		}},
	}
	got := buildContextBlock(map[string]any{}, summaries)
	if !strings.Contains(got, "discord_embed: {") {
		t.Errorf("expected JSON-serialised object, got:\n%s", got)
	}
}

func TestBuildContextBlock_ArrayValue(t *testing.T) {
	summaries := []ContextSummary{
		{ActionID: 3, ActionName: "NER", Metadata: map[string]any{
			"entities": []any{"Iran", "Khamenei"},
		}},
	}
	got := buildContextBlock(map[string]any{}, summaries)
	if !strings.Contains(got, `entities: ["Iran","Khamenei"]`) {
		t.Errorf("expected JSON array, got:\n%s", got)
	}
}

func TestBuildContextBlock_EmptyMetadataOmitted(t *testing.T) {
	summaries := []ContextSummary{
		{ActionID: 4, ActionName: "NoOutput", Metadata: map[string]any{}},
	}
	got := buildContextBlock(map[string]any{}, summaries)
	if strings.Contains(got, "NoOutput") {
		t.Errorf("action with empty metadata should be omitted, got:\n%s", got)
	}
}

func TestBuildContextBlock_OrderPreserved(t *testing.T) {
	summaries := []ContextSummary{
		{ActionID: 1, ActionName: "First", Metadata: map[string]any{"x": "1"}},
		{ActionID: 2, ActionName: "Second", Metadata: map[string]any{"y": "2"}},
	}
	got := buildContextBlock(map[string]any{}, summaries)
	firstIdx := strings.Index(got, "First:")
	secondIdx := strings.Index(got, "Second:")
	if firstIdx == -1 || secondIdx == -1 || firstIdx > secondIdx {
		t.Errorf("expected First before Second, got:\n%s", got)
	}
}

func TestBuildContextBlock_NativeIntValue(t *testing.T) {
	summaries := []ContextSummary{
		{ActionID: 1, ActionName: "Test", Metadata: map[string]any{
			"count": int(42),
		}},
	}
	got := buildContextBlock(map[string]any{}, summaries)
	if !strings.Contains(got, "  count: 42") {
		t.Errorf("expected native int rendered as plain text, got:\n%s", got)
	}
}

func TestOllamaPipelineRun_TextOnly_NoLoader(t *testing.T) {
	// text_only with nil loader — must pass raw tweet text unchanged.
	var sentText string
	p := &OllamaPipeline{
		name:      "test",
		inputMode: "text_only",
		sendChatFn: func(_ context.Context, text string) (string, error) {
			sentText = text
			return `{"result":"ok"}`, nil
		},
	}
	_, err := p.Run(context.Background(), PipelineInput{
		Text:    "raw tweet",
		TweetID: "t1",
		Context: map[string]any{},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if sentText != "raw tweet" {
		t.Errorf("expected raw tweet text, got: %s", sentText)
	}
}

func TestOllamaPipelineRun_ContextOnly(t *testing.T) {
	loader := func(_ context.Context, tweetID string, actionIDs []int64) ([]ContextSummary, error) {
		return []ContextSummary{
			{ActionID: 1, ActionName: "Clf", Metadata: map[string]any{"top_label": "violence"}},
		}, nil
	}
	var sentText string
	p := &OllamaPipeline{
		name:             "test",
		inputMode:        "context_only",
		contextActionIDs: []int64{1},
		loadSummaries:    loader,
		sendChatFn: func(_ context.Context, text string) (string, error) {
			sentText = text
			return `{"result":"ok"}`, nil
		},
	}
	_, err := p.Run(context.Background(), PipelineInput{
		Text:    "raw tweet",
		TweetID: "t1",
		Context: map[string]any{"tweet_id": "t1", "tweet_url": "https://x.com/u/status/t1", "tweet_author": "u", "tweet_short": "raw tweet"},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.HasPrefix(sentText, "<context>") {
		t.Errorf("context_only should start with <context>, got:\n%s", sentText)
	}
	if !strings.Contains(sentText, "tweet_id: t1") {
		t.Errorf("context_only should include tweet_id in context block, got:\n%s", sentText)
	}
	if !strings.Contains(sentText, "<context>") {
		t.Errorf("expected <context> block, got:\n%s", sentText)
	}
	if !strings.Contains(sentText, "top_label: violence") {
		t.Errorf("expected top_label in context, got:\n%s", sentText)
	}
}

func TestOllamaPipelineRun_TextAndContext(t *testing.T) {
	loader := func(_ context.Context, tweetID string, actionIDs []int64) ([]ContextSummary, error) {
		return []ContextSummary{
			{ActionID: 2, ActionName: "NER", Metadata: map[string]any{"entities": []any{"Iran"}}},
		}, nil
	}
	var sentText string
	p := &OllamaPipeline{
		name:             "test",
		inputMode:        "text_and_context",
		contextActionIDs: []int64{2},
		loadSummaries:    loader,
		sendChatFn: func(_ context.Context, text string) (string, error) {
			sentText = text
			return `{"result":"ok"}`, nil
		},
	}
	_, err := p.Run(context.Background(), PipelineInput{
		Text:    "raw tweet",
		TweetID: "t1",
		Context: map[string]any{"tweet_id": "t1", "tweet_url": "https://x.com/u/status/t1", "tweet_author": "u", "tweet_short": "raw tweet"},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.HasPrefix(sentText, "raw tweet") {
		t.Errorf("expected raw tweet text first, got:\n%s", sentText)
	}
	if !strings.Contains(sentText, "<context>") {
		t.Errorf("expected <context> block, got:\n%s", sentText)
	}
}

func TestOllamaPipelineRun_LoaderError_FallsBackToText(t *testing.T) {
	loader := func(_ context.Context, tweetID string, actionIDs []int64) ([]ContextSummary, error) {
		return nil, fmt.Errorf("db error")
	}
	var sentText string
	p := &OllamaPipeline{
		name:             "test",
		inputMode:        "text_and_context",
		contextActionIDs: []int64{1},
		loadSummaries:    loader,
		sendChatFn: func(_ context.Context, text string) (string, error) {
			sentText = text
			return `{"result":"ok"}`, nil
		},
	}
	_, err := p.Run(context.Background(), PipelineInput{
		Text:    "raw tweet",
		TweetID: "t1",
		Context: map[string]any{},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if sentText != "raw tweet" {
		t.Errorf("expected fallback to raw tweet, got: %s", sentText)
	}
}
