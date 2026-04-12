package notifier

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestDiscordBotClient_CreateThread_ReturnsThreadID(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("expected POST, got %s", r.Method)
		}
		if !strings.HasSuffix(r.URL.Path, "/threads") {
			t.Errorf("expected path ending in /threads, got %s", r.URL.Path)
		}
		if r.Header.Get("Authorization") != "Bot mytoken" {
			t.Errorf("expected Bot auth header, got %q", r.Header.Get("Authorization"))
		}
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatalf("decode body: %v", err)
		}
		if body["name"] != "My Thread" {
			t.Errorf("expected thread name 'My Thread', got %v", body["name"])
		}
		if body["message"] == nil {
			t.Errorf("expected message object in body, got %v", body)
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"id": "thread123"})
	}))
	defer srv.Close()

	c := NewDiscordBotClient("mytoken", "forum456")
	c.baseURL = srv.URL
	out, err := c.Deliver(context.Background(), "hello world", map[string]any{
		"thread_name": "My Thread",
	})
	if err != nil {
		t.Fatalf("Deliver: %v", err)
	}
	if out["discord_thread_id"] != "thread123" {
		t.Errorf("discord_thread_id: got %v, want thread123", out["discord_thread_id"])
	}
}

func TestDiscordBotClient_CreateThread_EmptyText_Errors(t *testing.T) {
	c := NewDiscordBotClient("mytoken", "forum456")
	_, err := c.Deliver(context.Background(), "", map[string]any{})
	if err == nil {
		t.Fatal("expected error for empty text")
	}
}

func TestDiscordBotClient_CreateThread_MissingIDInResponse_Errors(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"type": 11}) // no id
	}))
	defer srv.Close()

	c := NewDiscordBotClient("mytoken", "forum456")
	c.baseURL = srv.URL
	_, err := c.Deliver(context.Background(), "msg", map[string]any{})
	if err == nil {
		t.Fatal("expected error when id missing from response")
	}
	if !strings.Contains(err.Error(), "thread id missing") {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestDiscordBotClient_CreateThread_WithAppliedTags(t *testing.T) {
	var gotTags []any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		if tags, ok := body["applied_tags"]; ok {
			gotTags, _ = tags.([]any)
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"id": "thread999"})
	}))
	defer srv.Close()

	c := NewDiscordBotClient("mytoken", "forum456")
	c.baseURL = srv.URL
	_, err := c.Deliver(context.Background(), "msg", map[string]any{
		"applied_tags": []string{"tag1", "tag2"},
	})
	if err != nil {
		t.Fatalf("Deliver: %v", err)
	}
	if len(gotTags) != 2 {
		t.Errorf("expected 2 applied_tags in request body, got %v", gotTags)
	}
}

func TestDiscordBotClient_ReplyToThread_PostsToMessages(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("expected POST, got %s", r.Method)
		}
		if !strings.HasSuffix(r.URL.Path, "/thread456/messages") {
			t.Errorf("expected path ending in /thread456/messages, got %s", r.URL.Path)
		}
		if r.Header.Get("Authorization") != "Bot mytoken" {
			t.Errorf("expected Bot auth header, got %q", r.Header.Get("Authorization"))
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"id": "msg789"})
	}))
	defer srv.Close()

	c := NewDiscordBotClient("mytoken", "forum456")
	c.baseURL = srv.URL
	out, err := c.Deliver(context.Background(), "reply text", map[string]any{"discord_thread_id": "thread456"})
	if err != nil {
		t.Fatalf("Deliver: %v", err)
	}
	if out["sent"] != true {
		t.Errorf("expected sent=true, got %v", out)
	}
}

func TestDiscordBotClient_ReplyToThread_EmptyThreadID_Errors(t *testing.T) {
	c := NewDiscordBotClient("mytoken", "forum456")
	_, err := c.Deliver(context.Background(), "msg", map[string]any{"discord_thread_id": ""})
	if err == nil {
		t.Fatal("expected error for empty discord_thread_id")
	}
	if !strings.Contains(err.Error(), "discord_thread_id missing from context") {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestDiscordBotClient_GetAvailableTags_ReturnsTags(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			t.Errorf("expected GET, got %s", r.Method)
		}
		if !strings.HasSuffix(r.URL.Path, "/forum456") {
			t.Errorf("expected path ending in /forum456, got %s", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"id":   "forum456",
			"type": 15,
			"available_tags": []any{
				map[string]any{"id": "111", "name": "military"},
				map[string]any{"id": "222", "name": "diplomacy"},
			},
		})
	}))
	defer srv.Close()

	c := NewDiscordBotClient("mytoken", "forum456")
	c.baseURL = srv.URL
	tags, err := c.GetAvailableTags(context.Background())
	if err != nil {
		t.Fatalf("GetAvailableTags: %v", err)
	}
	if len(tags) != 2 {
		t.Fatalf("expected 2 tags, got %d", len(tags))
	}
	if tags[0].ID != "111" || tags[0].Name != "military" {
		t.Errorf("tag[0]: got {%q, %q}, want {111, military}", tags[0].ID, tags[0].Name)
	}
	if tags[1].ID != "222" || tags[1].Name != "diplomacy" {
		t.Errorf("tag[1]: got {%q, %q}, want {222, diplomacy}", tags[1].ID, tags[1].Name)
	}
}

func TestDiscordBotClient_GetAvailableTags_APIError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusForbidden)
	}))
	defer srv.Close()

	c := NewDiscordBotClient("mytoken", "forum456")
	c.baseURL = srv.URL
	_, err := c.GetAvailableTags(context.Background())
	if err == nil {
		t.Fatal("expected error for non-200 response")
	}
}

func TestDiscordBotClient_PatchThreadTags_SendsPatch(t *testing.T) {
	var gotTagIDs []any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPatch {
			t.Errorf("expected PATCH, got %s", r.Method)
		}
		if !strings.HasSuffix(r.URL.Path, "/thread456") {
			t.Errorf("expected path ending in /thread456, got %s", r.URL.Path)
		}
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		gotTagIDs, _ = body["applied_tags"].([]any)
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"id": "thread456"})
	}))
	defer srv.Close()

	c := NewDiscordBotClient("mytoken", "forum456")
	c.baseURL = srv.URL
	err := c.PatchThreadTags(context.Background(), "thread456", []string{"111", "222"})
	if err != nil {
		t.Fatalf("PatchThreadTags: %v", err)
	}
	if len(gotTagIDs) != 2 {
		t.Errorf("expected 2 tag IDs in request, got %v", gotTagIDs)
	}
}

func TestDiscordBotClient_PatchThreadTags_APIError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusForbidden)
	}))
	defer srv.Close()

	c := NewDiscordBotClient("mytoken", "forum456")
	c.baseURL = srv.URL
	err := c.PatchThreadTags(context.Background(), "thread456", []string{"111"})
	if err == nil {
		t.Fatal("expected error for non-200 response")
	}
}
