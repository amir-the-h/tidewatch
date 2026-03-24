package notifier

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/amir-the-h/iran-watcher/internal/model"
)

func sampleTweet() *model.Tweet {
	return &model.Tweet{
		ID:           "1234567890",
		AuthorHandle: "testuser",
		Text:         "This is a test tweet about Iran.",
	}
}

func TestRenderMessage_DefaultTemplates(t *testing.T) {
	tweet := sampleTweet()
	results, _ := json.Marshal(map[string]float32{"relevant": 0.95})

	tests := []struct {
		name     string
		event    NotificationEvent
		contains string
	}{
		{
			name: EventTweetMatched,
			event: NotificationEvent{
				Type:  EventTweetMatched,
				Tweet: tweet,
			},
			contains: "@testuser",
		},
		{
			name: EventTweetCategorized,
			event: NotificationEvent{
				Type:      EventTweetCategorized,
				Tweet:     tweet,
				ModelName: "relevance",
				Results:   json.RawMessage(results),
			},
			contains: "relevance",
		},
		{
			name: EventCategorizationFailed,
			event: NotificationEvent{
				Type:      EventCategorizationFailed,
				Tweet:     tweet,
				ModelName: "relevance",
				Error:     "model unavailable",
			},
			contains: "1234567890",
		},
		{
			name: EventStreamConnected,
			event: NotificationEvent{
				Type: EventStreamConnected,
			},
			contains: "Stream connected",
		},
		{
			name: EventStreamDisconnected,
			event: NotificationEvent{
				Type: EventStreamDisconnected,
			},
			contains: "Stream disconnected",
		},
		{
			name: EventStreamError,
			event: NotificationEvent{
				Type:  EventStreamError,
				Error: "connection reset",
			},
			contains: "connection reset",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			msg, err := RenderMessage(tc.event, nil)
			if err != nil {
				t.Fatalf("RenderMessage returned error: %v", err)
			}
			if !strings.Contains(msg, tc.contains) {
				t.Errorf("expected message to contain %q, got %q", tc.contains, msg)
			}
		})
	}
}

func TestRenderMessage_CustomTemplate(t *testing.T) {
	tweet := sampleTweet()
	event := NotificationEvent{
		Type:  EventTweetMatched,
		Tweet: tweet,
	}
	custom := "Custom: {{.Tweet.ID}}"
	msg, err := RenderMessage(event, &custom)
	if err != nil {
		t.Fatalf("RenderMessage returned error: %v", err)
	}
	if !strings.Contains(msg, "Custom:") {
		t.Errorf("expected custom template prefix, got %q", msg)
	}
	if !strings.Contains(msg, tweet.ID) {
		t.Errorf("expected tweet ID %q in message, got %q", tweet.ID, msg)
	}
	// Ensure default was overridden (default would contain @testuser)
	if strings.Contains(msg, "@testuser") {
		t.Errorf("expected custom template to override default, but got %q", msg)
	}
}

func TestRenderMessage_StreamConnected(t *testing.T) {
	event := NotificationEvent{
		Type: EventStreamConnected,
	}
	msg, err := RenderMessage(event, nil)
	if err != nil {
		t.Fatalf("RenderMessage returned error: %v", err)
	}
	if msg != "Stream connected" {
		t.Errorf("expected %q, got %q", "Stream connected", msg)
	}
}

func TestValidateTemplate_Valid(t *testing.T) {
	err := ValidateTemplate("Hello {{.Name}}, event: {{.Type}}")
	if err != nil {
		t.Errorf("expected nil error for valid template, got %v", err)
	}
}

func TestValidateTemplate_Invalid(t *testing.T) {
	err := ValidateTemplate("Hello {{.Name")
	if err == nil {
		t.Error("expected error for invalid template, got nil")
	}
}

func TestExpandURL_WithTweet(t *testing.T) {
	tweet := sampleTweet()
	event := NotificationEvent{
		Type:  EventTweetMatched,
		Tweet: tweet,
	}
	urlTmpl := "https://x.com/{{.Tweet.AuthorHandle}}/status/{{.Tweet.ID}}"
	result := ExpandURL(urlTmpl, event)
	expected := "https://x.com/testuser/status/1234567890"
	if result != expected {
		t.Errorf("expected %q, got %q", expected, result)
	}
}

func TestExpandURL_NilTweet(t *testing.T) {
	event := NotificationEvent{
		Type:  EventStreamConnected,
		Tweet: nil,
	}
	urlTmpl := "https://example.com/{{.Tweet.AuthorHandle}}/{{.Tweet.ID}}"
	result := ExpandURL(urlTmpl, event)
	expected := urlTmpl // returned as-is on template execute error (nil Tweet dereference)
	if result != expected {
		t.Errorf("expected %q, got %q", expected, result)
	}
}
