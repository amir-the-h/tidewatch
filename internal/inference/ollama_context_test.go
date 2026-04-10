package inference

import (
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
