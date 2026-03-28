package pipeline

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/amir-the-h/iran-watcher/internal/inference"
	"github.com/amir-the-h/iran-watcher/internal/model"
)

// mockProvider records what was delivered.
type mockProvider struct {
	lastText   string
	lastConfig map[string]any
	err        error
}

func (m *mockProvider) Deliver(_ context.Context, text string, config map[string]any) error {
	m.lastText = text
	m.lastConfig = config
	return m.err
}

func testChannel(template string, cfg string) *model.NotificationChannel {
	return &model.NotificationChannel{
		ID:              1,
		Name:            "test-channel",
		ProviderID:      1,
		MessageTemplate: template,
		Config:          json.RawMessage(cfg),
		Active:          true,
	}
}

func TestNotificationPipeline_SubstitutesVars(t *testing.T) {
	mp := &mockProvider{}
	ch := testChannel("Hello ${author}, tweet: ${tweet}", `{"priority":1}`)
	p := NewNotificationPipeline("test", ch, mp)

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
	ch := testChannel("${unknown} and ${author}", `{}`)
	p := NewNotificationPipeline("test", ch, mp)

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
	ch := testChannel("msg", `{}`)
	p := NewNotificationPipeline("pushover-test", ch, mp)

	input := inference.PipelineInput{}
	out, err := p.Run(context.Background(), input)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if out["provider"] != "pushover-test" {
		t.Errorf("provider: got %v, want pushover-test", out["provider"])
	}
}

func TestNotificationPipeline_ProviderErrorPropagates(t *testing.T) {
	mp := &mockProvider{err: errors.New("network timeout")}
	ch := testChannel("msg", `{}`)
	p := NewNotificationPipeline("test", ch, mp)

	input := inference.PipelineInput{}
	_, err := p.Run(context.Background(), input)
	if err == nil {
		t.Fatal("expected error from provider, got nil")
	}
}

func TestNotificationPipeline_NilConfigDoesNotPanic(t *testing.T) {
	mp := &mockProvider{}
	ch := testChannel("msg", `null`)
	p := NewNotificationPipeline("test", ch, mp)

	input := inference.PipelineInput{}
	_, err := p.Run(context.Background(), input)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestNotificationPipeline_TypeReturnsNotification(t *testing.T) {
	mp := &mockProvider{}
	ch := testChannel("msg", `{}`)
	p := NewNotificationPipeline("test", ch, mp)
	if p.Type() != inference.PipelineTypeNotification {
		t.Errorf("Type: got %q, want %q", p.Type(), inference.PipelineTypeNotification)
	}
}

func TestNotificationPipeline_Close(t *testing.T) {
	mp := &mockProvider{}
	ch := testChannel("msg", `{}`)
	p := NewNotificationPipeline("test", ch, mp)
	err := p.Close()
	if err != nil {
		t.Errorf("Close: expected nil, got %v", err)
	}
}
