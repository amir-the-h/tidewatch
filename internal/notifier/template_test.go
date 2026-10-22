package notifier

import (
	"strings"
	"testing"

	"github.com/amir-the-h/tidewatch/internal/model"
)

func sampleSignal() *model.Signal {
	return &model.Signal{
		ID:         42,
		ExternalID: "1234567890",
		Author:     "testuser",
		Text:       "This is a test post about Iran.",
	}
}

func TestRenderMessage_DefaultTemplates(t *testing.T) {
	sig := sampleSignal()

	tests := []struct {
		name     string
		event    NotificationEvent
		contains string
	}{
		{
			name: EventSignalIngested,
			event: NotificationEvent{
				Type:   EventSignalIngested,
				Signal: sig,
			},
			contains: "testuser",
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
	sig := sampleSignal()
	event := NotificationEvent{
		Type:   EventSignalIngested,
		Signal: sig,
	}
	custom := "Custom: {{.Signal.ExternalID}}"
	msg, err := RenderMessage(event, &custom)
	if err != nil {
		t.Fatalf("RenderMessage returned error: %v", err)
	}
	if !strings.Contains(msg, "Custom:") {
		t.Errorf("expected custom template prefix, got %q", msg)
	}
	if !strings.Contains(msg, sig.ExternalID) {
		t.Errorf("expected external ID %q in message, got %q", sig.ExternalID, msg)
	}
	// Ensure default was overridden (default would contain testuser)
	if strings.Contains(msg, "testuser") {
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

func TestExpandURL_WithSignal(t *testing.T) {
	sig := sampleSignal()
	event := NotificationEvent{
		Type:   EventSignalIngested,
		Signal: sig,
	}
	urlTmpl := "https://x.com/{{.Signal.Author}}/status/{{.Signal.ExternalID}}"
	result := ExpandURL(urlTmpl, event)
	expected := "https://x.com/testuser/status/1234567890"
	if result != expected {
		t.Errorf("expected %q, got %q", expected, result)
	}
}

func TestExpandURL_NilSignal(t *testing.T) {
	event := NotificationEvent{
		Type:   EventStreamConnected,
		Signal: nil,
	}
	urlTmpl := "https://example.com/{{.Signal.Author}}/{{.Signal.ExternalID}}"
	result := ExpandURL(urlTmpl, event)
	expected := urlTmpl // returned as-is on template execute error (nil Signal dereference)
	if result != expected {
		t.Errorf("expected %q, got %q", expected, result)
	}
}
