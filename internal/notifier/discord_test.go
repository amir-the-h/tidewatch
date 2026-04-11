package notifier

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestDiscordClient_RootSend_ReturnsThreadID(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("wait") != "true" {
			t.Errorf("expected wait=true query param, got %q", r.URL.RawQuery)
		}
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatalf("decode body: %v", err)
		}
		if _, ok := body["thread_name"]; !ok {
			t.Errorf("expected thread_name in body, got %v", body)
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"id":         "msg123",
			"channel_id": "thread456",
		})
	}))
	defer srv.Close()

	c := NewDiscordClient(srv.URL)
	out, err := c.Deliver(context.Background(), "hello world", map[string]any{})
	if err != nil {
		t.Fatalf("Deliver: %v", err)
	}
	if out["discord_thread_id"] != "thread456" {
		t.Errorf("discord_thread_id: got %v, want thread456", out["discord_thread_id"])
	}
}

func TestDiscordClient_ReplySend_UsesThreadID(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("thread_id") != "thread456" {
			t.Errorf("expected thread_id=thread456, got %q", r.URL.RawQuery)
		}
		if r.URL.Query().Get("wait") != "" {
			t.Errorf("reply send must not include wait=true, got %q", r.URL.RawQuery)
		}
		w.WriteHeader(http.StatusNoContent)
	}))
	defer srv.Close()

	c := NewDiscordClient(srv.URL)
	out, err := c.Deliver(context.Background(), "reply message", map[string]any{"discord_thread_id": "thread456"})
	if err != nil {
		t.Fatalf("Deliver: %v", err)
	}
	if out["sent"] != true {
		t.Errorf("expected sent=true, got %v", out)
	}
}

func TestDiscordClient_ReplySend_MissingThreadID_Errors(t *testing.T) {
	c := NewDiscordClient("http://unused")
	_, err := c.Deliver(context.Background(), "msg", map[string]any{"discord_thread_id": ""})
	if err == nil {
		t.Fatal("expected error for empty discord_thread_id")
	}
	if !strings.Contains(err.Error(), "discord_thread_id missing from context") {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestDiscordClient_RootSend_MissingChannelID_Errors(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("wait") != "true" {
			t.Errorf("expected wait=true query param, got %q", r.URL.RawQuery)
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"id": "msg123"}) // no channel_id
	}))
	defer srv.Close()

	c := NewDiscordClient(srv.URL)
	_, err := c.Deliver(context.Background(), "msg", map[string]any{})
	if err == nil {
		t.Fatal("expected error when channel_id missing from response")
	}
	if !strings.Contains(err.Error(), "thread_id missing from wait response") {
		t.Errorf("unexpected error: %v", err)
	}
}
