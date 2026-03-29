package pipeline

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/amir-the-h/iran-watcher/internal/inference"
	"github.com/amir-the-h/iran-watcher/internal/notifier"
)

// mockProvider records what was delivered and returns optional metadata.
type mockProvider struct {
	lastText   string
	lastConfig map[string]any
	meta       map[string]any
	err        error
}

func (m *mockProvider) Deliver(_ context.Context, text string, config map[string]any) (map[string]any, error) {
	m.lastText = text
	m.lastConfig = config
	return m.meta, m.err
}

func TestNotificationPipeline_SubstitutesVars(t *testing.T) {
	mp := &mockProvider{}
	p := NewNotificationPipeline("test", "Hello ${author}, tweet: ${tweet}", map[string]any{"priority": 1}, mp, nil)

	input := inference.PipelineInput{
		Context: map[string]any{
			"tweet":  "Iran protests continue",
			"author": "reporter1",
		},
	}
	out, err := p.Run(context.Background(), input)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	wantText := "Hello reporter1, tweet: Iran protests continue"
	if mp.lastText != wantText {
		t.Errorf("text: got %q, want %q", mp.lastText, wantText)
	}
	if out["sent"] != true {
		t.Errorf("expected sent=true in output")
	}
}

func TestNotificationPipeline_UnknownVarsLeftAsIs(t *testing.T) {
	mp := &mockProvider{}
	p := NewNotificationPipeline("test", "${unknown} and ${author}", nil, mp, nil)

	input := inference.PipelineInput{
		Context: map[string]any{"author": "me"},
	}
	_, err := p.Run(context.Background(), input)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if mp.lastText != "${unknown} and me" {
		t.Errorf("text: got %q", mp.lastText)
	}
}

func TestNotificationPipeline_OutputContainsProviderType(t *testing.T) {
	mp := &mockProvider{}
	p := NewNotificationPipeline("pushover-test", "msg", nil, mp, nil)

	out, err := p.Run(context.Background(), inference.PipelineInput{})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if out["provider"] != "pushover-test" {
		t.Errorf("provider: got %v, want pushover-test", out["provider"])
	}
}

func TestNotificationPipeline_ProviderErrorPropagates(t *testing.T) {
	mp := &mockProvider{err: errors.New("network timeout")}
	p := NewNotificationPipeline("test", "msg", nil, mp, nil)

	_, err := p.Run(context.Background(), inference.PipelineInput{})
	if err == nil {
		t.Fatal("expected error from provider, got nil")
	}
}

func TestNotificationPipeline_DeliveryConfigPassedThrough(t *testing.T) {
	mp := &mockProvider{}
	cfg := map[string]any{"chat_id": "-1001234", "parse_mode": "HTML"}
	p := NewNotificationPipeline("test", "msg", cfg, mp, nil)

	_, err := p.Run(context.Background(), inference.PipelineInput{})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if mp.lastConfig["chat_id"] != "-1001234" {
		t.Errorf("chat_id: got %v", mp.lastConfig["chat_id"])
	}
}

func TestNotificationPipeline_TypeReturnsNotification(t *testing.T) {
	mp := &mockProvider{}
	p := NewNotificationPipeline("test", "msg", nil, mp, nil)
	if p.Type() != inference.PipelineTypeNotification {
		t.Errorf("Type: got %q, want %q", p.Type(), inference.PipelineTypeNotification)
	}
}

func TestNotificationPipeline_Close(t *testing.T) {
	mp := &mockProvider{}
	p := NewNotificationPipeline("test", "msg", nil, mp, nil)
	if err := p.Close(); err != nil {
		t.Errorf("Close: expected nil, got %v", err)
	}
}

func TestNotificationPipeline_MarkdownV2EscapesValues(t *testing.T) {
	mp := &mockProvider{}
	cfg := map[string]any{"parse_mode": "MarkdownV2"}
	// Template has intentional bold; the tweet value contains special chars.
	p := NewNotificationPipeline("test", "*Tweet:* ${tweet}", cfg, mp, nil)

	_, err := p.Run(context.Background(), inference.PipelineInput{
		Context: map[string]any{"tweet": "Hello_World! (breaking.news)"},
	})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	// Template markup (*) is preserved; value chars are escaped.
	want := `*Tweet:* Hello\_World\! \(breaking\.news\)`
	if mp.lastText != want {
		t.Errorf("text: got %q, want %q", mp.lastText, want)
	}
}

func TestNotificationPipeline_HTMLEscapesValues(t *testing.T) {
	mp := &mockProvider{}
	cfg := map[string]any{"parse_mode": "HTML"}
	p := NewNotificationPipeline("test", "<b>Tweet:</b> ${tweet}", cfg, mp, nil)

	_, err := p.Run(context.Background(), inference.PipelineInput{
		Context: map[string]any{"tweet": "<script>alert('xss')</script> & more"},
	})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	want := "<b>Tweet:</b> &lt;script&gt;alert('xss')&lt;/script&gt; &amp; more"
	if mp.lastText != want {
		t.Errorf("text: got %q, want %q", mp.lastText, want)
	}
}

func TestNotificationPipeline_NoParseModeLeavesValuesRaw(t *testing.T) {
	mp := &mockProvider{}
	p := NewNotificationPipeline("test", "${tweet}", nil, mp, nil)

	_, err := p.Run(context.Background(), inference.PipelineInput{
		Context: map[string]any{"tweet": "raw_text*here"},
	})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if mp.lastText != "raw_text*here" {
		t.Errorf("text: got %q", mp.lastText)
	}
}

func TestRenderValue_String(t *testing.T) {
	if got := renderValue("hello world"); got != "hello world" {
		t.Errorf("got %q, want %q", got, "hello world")
	}
}

func TestRenderValue_Float(t *testing.T) {
	if got := renderValue(float64(0.95)); got != "0.95" {
		t.Errorf("got %q, want 0.95", got)
	}
}

func TestRenderValue_Slice(t *testing.T) {
	got := renderValue([]any{"political", "news"})
	want := `["political","news"]`
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestRenderValue_Map(t *testing.T) {
	got := renderValue(map[string]any{"word": "Iran", "label": "GPE"})
	// JSON object — just check it's valid JSON, not Go %v format
	if !strings.HasPrefix(got, "{") {
		t.Errorf("expected JSON object, got %q", got)
	}
}

func TestTelegramEscapeMarkdownV2(t *testing.T) {
	cases := []struct{ in, want string }{
		{"hello", "hello"},
		{"hello_world", `hello\_world`},
		{"1+1=2", `1\+1\=2`},
		{"(test)", `\(test\)`},
		{"foo.bar", `foo\.bar`},
		{`back\slash`, `back\\slash`},
	}
	for _, c := range cases {
		got := notifier.EscapeMarkdownV2(c.in)
		if got != c.want {
			t.Errorf("EscapeMarkdownV2(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestTelegramEscapeHTML(t *testing.T) {
	cases := []struct{ in, want string }{
		{"hello", "hello"},
		{"<b>bold</b>", "&lt;b&gt;bold&lt;/b&gt;"},
		{"a & b", "a &amp; b"},
		{"<a & b>", "&lt;a &amp; b&gt;"},
	}
	for _, c := range cases {
		got := notifier.EscapeHTML(c.in)
		if got != c.want {
			t.Errorf("EscapeHTML(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestNotificationPipeline_ForwardsMessageIDToProvider(t *testing.T) {
	mp := &mockProvider{meta: map[string]any{"telegram_message_id": int64(42)}}
	p := NewNotificationPipeline("test", "msg", map[string]any{"chat_id": "-100"}, mp, nil)

	// Simulate a second run where the previous execution stored a message_id.
	_, err := p.Run(context.Background(), inference.PipelineInput{
		Context: map[string]any{"telegram_message_id": "99"},
	})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if mp.lastConfig["telegram_message_id"] != "99" {
		t.Errorf("expected telegram_message_id forwarded to provider, got %v", mp.lastConfig["telegram_message_id"])
	}
}

func TestNotificationPipeline_ProviderMetaMergedIntoOutput(t *testing.T) {
	mp := &mockProvider{meta: map[string]any{"telegram_message_id": int64(123)}}
	p := NewNotificationPipeline("test", "msg", nil, mp, nil)

	out, err := p.Run(context.Background(), inference.PipelineInput{})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if out["telegram_message_id"] != int64(123) {
		t.Errorf("telegram_message_id: got %v, want 123", out["telegram_message_id"])
	}
	if out["sent"] != true {
		t.Errorf("expected sent=true")
	}
}

func TestNotificationPipeline_NoMessageIDForwardedWhenAbsent(t *testing.T) {
	mp := &mockProvider{}
	p := NewNotificationPipeline("test", "msg", map[string]any{"chat_id": "-100"}, mp, nil)

	_, err := p.Run(context.Background(), inference.PipelineInput{})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if _, ok := mp.lastConfig["telegram_message_id"]; ok {
		t.Errorf("telegram_message_id should not be in config when not in context")
	}
}

func TestTelegramEscapeMarkdown(t *testing.T) {
	cases := []struct{ in, want string }{
		{"hello", "hello"},
		{"_italic_", `\_italic\_`},
		{"*bold*", `\*bold\*`},
		{"[link]", `\[link]`},
		{"`code`", "\\`code\\`"},
	}
	for _, c := range cases {
		got := notifier.EscapeMarkdown(c.in)
		if got != c.want {
			t.Errorf("EscapeMarkdown(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

// ── ${actions} block tests ────────────────────────────────────────────────────

func TestNotificationPipeline_ActionsExpanded(t *testing.T) {
	mp := &mockProvider{}
	loader := func(_ context.Context, tweetID string) ([]ActionSummary, error) {
		return []ActionSummary{
			{ActionName: "Classify", Metadata: map[string]any{"label": "political", "score": float64(0.95)}},
		}, nil
	}
	p := NewNotificationPipeline("test", "Tweet: ${tweet}\n${actions}", nil, mp, loader)

	_, err := p.Run(context.Background(), inference.PipelineInput{
		Context: map[string]any{"tweet": "hello", "tweet_id": "123"},
	})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if !strings.Contains(mp.lastText, "Classify") {
		t.Errorf("expected action name in output, got: %q", mp.lastText)
	}
	if !strings.Contains(mp.lastText, "political") {
		t.Errorf("expected label value in output, got: %q", mp.lastText)
	}
}

func TestNotificationPipeline_ActionsSkippedWithoutLoader(t *testing.T) {
	mp := &mockProvider{}
	p := NewNotificationPipeline("test", "${actions}", nil, mp, nil)

	_, err := p.Run(context.Background(), inference.PipelineInput{
		Context: map[string]any{"tweet_id": "123"},
	})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	// Without a loader the placeholder is left as-is.
	if mp.lastText != "${actions}" {
		t.Errorf("expected placeholder unchanged, got: %q", mp.lastText)
	}
}

func TestNotificationPipeline_ActionsEmptyWhenNoSummaries(t *testing.T) {
	mp := &mockProvider{}
	loader := func(_ context.Context, _ string) ([]ActionSummary, error) {
		return nil, nil // no actions ran yet
	}
	p := NewNotificationPipeline("test", "tweet\n${actions}", nil, mp, loader)

	_, err := p.Run(context.Background(), inference.PipelineInput{
		Context: map[string]any{"tweet_id": "123"},
	})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	// ${actions} must be replaced with "" — not left as the literal placeholder.
	if strings.Contains(mp.lastText, "${actions}") {
		t.Errorf("placeholder should be removed when summaries are empty, got: %q", mp.lastText)
	}
}

func TestFormatOneActionBlock_MarkdownV2(t *testing.T) {
	s := ActionSummary{
		ActionName: "NER (Test)",
		Metadata: map[string]any{
			"entity_list": "Iran (GPE)",
			"sent":        true, // internal — should be skipped
		},
	}
	got := formatOneActionBlock(s, "MarkdownV2", notifier.EscapeMarkdownV2)
	// Header must be bold and expandable; action name parentheses escaped.
	if !strings.HasPrefix(got, "**>*NER \\(Test\\)*") {
		t.Errorf("unexpected header: %q", got)
	}
	// Key name underscores must be escaped: entity_list → entity\_list
	if !strings.Contains(got, `entity\_list`) {
		t.Errorf("expected escaped key name entity\\_list in block, got: %q", got)
	}
	// Value parentheses must be escaped: Iran (GPE) → Iran \(GPE\)
	if !strings.Contains(got, `Iran \(GPE\)`) {
		t.Errorf("expected escaped value in block, got: %q", got)
	}
	if strings.Contains(got, "sent") {
		t.Errorf("internal key 'sent' should be excluded, got: %q", got)
	}
}

func TestFormatOneActionBlock_HTML(t *testing.T) {
	s := ActionSummary{
		ActionName: "Classify <html>",
		Metadata:   map[string]any{"label": "political"},
	}
	got := formatOneActionBlock(s, "HTML", notifier.EscapeHTML)
	if !strings.HasPrefix(got, "<blockquote expandable>") {
		t.Errorf("expected HTML expandable blockquote, got: %q", got)
	}
	if !strings.Contains(got, "Classify &lt;html&gt;") {
		t.Errorf("action name should be HTML-escaped in header, got: %q", got)
	}
	if !strings.HasSuffix(got, "</blockquote>") {
		t.Errorf("expected closing blockquote tag, got: %q", got)
	}
}

func TestFormatOneActionBlock_NoMetadata(t *testing.T) {
	s := ActionSummary{ActionName: "Retweet", Metadata: map[string]any{}}
	got := formatOneActionBlock(s, "HTML", notifier.EscapeHTML)
	if got != "" {
		t.Errorf("expected empty string for action with no displayable metadata, got: %q", got)
	}
}

func TestRenderMetaDisplayValue_Bool(t *testing.T) {
	if got := renderMetaDisplayValue(true); got != "yes" {
		t.Errorf("true: got %q, want yes", got)
	}
	if got := renderMetaDisplayValue(false); got != "" {
		t.Errorf("false: got %q, want empty (skip)", got)
	}
}

func TestRenderMetaDisplayValue_Array(t *testing.T) {
	got := renderMetaDisplayValue([]any{"Iran", "Iraq"})
	if got != "Iran, Iraq" {
		t.Errorf("got %q, want %q", got, "Iran, Iraq")
	}
}

func TestRenderMetaDisplayValue_LongString(t *testing.T) {
	long := strings.Repeat("a", 250)
	got := renderMetaDisplayValue(long)
	if len(got) != 200 || !strings.HasSuffix(got, "...") {
		t.Errorf("expected truncation to 200 chars with ellipsis, got len=%d", len(got))
	}
}

// ── filter tests ──────────────────────────────────────────────────────────────

func TestApplyFilter_YesNo_Bool(t *testing.T) {
	if got := applyFilter(true, "yesno"); got != "yes" {
		t.Errorf("true|yesno: got %q, want yes", got)
	}
	if got := applyFilter(false, "yesno"); got != "no" {
		t.Errorf("false|yesno: got %q, want no", got)
	}
}

func TestApplyFilter_YesNo_String(t *testing.T) {
	if got := applyFilter("true", "yesno"); got != "yes" {
		t.Errorf("\"true\"|yesno: got %q, want yes", got)
	}
	if got := applyFilter("false", "yesno"); got != "no" {
		t.Errorf("\"false\"|yesno: got %q, want no", got)
	}
}

func TestApplyFilter_Join_Slice(t *testing.T) {
	got := applyFilter([]any{"Iran", "Iraq"}, "join")
	if got != "Iran, Iraq" {
		t.Errorf("[]any|join: got %q, want %q", got, "Iran, Iraq")
	}
}

func TestApplyFilter_Join_EmptySlice(t *testing.T) {
	got := applyFilter([]any{}, "join")
	if got != "" {
		t.Errorf("empty []any|join: got %q, want empty string", got)
	}
}

func TestApplyFilter_Join_JSONString(t *testing.T) {
	got := applyFilter(`["second strike","capital targeted"]`, "join")
	if got != "second strike, capital targeted" {
		t.Errorf("JSON string|join: got %q", got)
	}
}

func TestApplyFilter_Join_EmptyJSONArray(t *testing.T) {
	got := applyFilter("[]", "join")
	if got != "" {
		t.Errorf("empty JSON array string|join: got %q, want empty string", got)
	}
}

func TestSubstituteVars_FilterYesNo(t *testing.T) {
	ctx := map[string]any{"response": "true"}
	got := substituteVars("Response: ${response | yesno}", ctx, nil)
	if got != "Response: yes" {
		t.Errorf("got %q, want %q", got, "Response: yes")
	}
}

func TestSubstituteVars_FilterJoin_EmptyDropsLine(t *testing.T) {
	ctx := map[string]any{"tags": []any{}}
	got := substituteVars("Header\nEscalation: ${tags | join}\nFooter", ctx, nil)
	if strings.Contains(got, "Escalation") {
		t.Errorf("empty join should drop the line, got: %q", got)
	}
	if !strings.Contains(got, "Header") || !strings.Contains(got, "Footer") {
		t.Errorf("surrounding lines should be preserved, got: %q", got)
	}
}

func TestSubstituteVars_HTMLWrappedLabelDroppedWhenEmpty(t *testing.T) {
	ctx := map[string]any{"casualties": nil, "tags": []any{}}
	tmpl := "Header\n<b>Casualties:</b> ${casualties}\n<b>Escalation:</b> ${tags | join}\nFooter"
	got := substituteVars(tmpl, ctx, nil)
	if strings.Contains(got, "Casualties") {
		t.Errorf("nil casualties should drop the HTML-wrapped line, got: %q", got)
	}
	if strings.Contains(got, "Escalation") {
		t.Errorf("empty join should drop the HTML-wrapped line, got: %q", got)
	}
	if !strings.Contains(got, "Header") || !strings.Contains(got, "Footer") {
		t.Errorf("surrounding lines should be preserved, got: %q", got)
	}
}
