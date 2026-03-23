package notifier

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

func newTestPushoverClient(serverURL string) *PushoverClient {
	c := NewPushoverClient("test-token", "test-user", slog.New(slog.NewTextHandler(io.Discard, nil)))
	c.apiURL = serverURL
	return c
}

func TestPushoverSend_Success(t *testing.T) {
	var gotForm url.Values
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := r.ParseForm(); err != nil {
			t.Fatalf("parse form: %v", err)
		}
		gotForm = r.PostForm
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"status": 1})
	}))
	defer srv.Close()

	client := newTestPushoverClient(srv.URL)
	err := client.Send(context.Background(), PushoverMessage{
		Message: "hello",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if gotForm.Get("token") != "test-token" {
		t.Errorf("token: got %q, want %q", gotForm.Get("token"), "test-token")
	}
	if gotForm.Get("user") != "test-user" {
		t.Errorf("user: got %q, want %q", gotForm.Get("user"), "test-user")
	}
	if gotForm.Get("message") != "hello" {
		t.Errorf("message: got %q, want %q", gotForm.Get("message"), "hello")
	}
}

func TestPushoverSend_Error(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"status": 0,
			"errors": []string{"invalid token"},
		})
	}))
	defer srv.Close()

	client := newTestPushoverClient(srv.URL)
	err := client.Send(context.Background(), PushoverMessage{
		Message: "hello",
	})
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !strings.Contains(err.Error(), "API error") {
		t.Errorf("error should mention API error, got: %v", err)
	}
	if !strings.Contains(err.Error(), "invalid token") {
		t.Errorf("error should mention 'invalid token', got: %v", err)
	}
}

func TestPushoverSend_EmptyMessage(t *testing.T) {
	called := false
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
	}))
	defer srv.Close()

	client := newTestPushoverClient(srv.URL)
	err := client.Send(context.Background(), PushoverMessage{})
	if err == nil {
		t.Fatal("expected validation error, got nil")
	}
	if !strings.Contains(err.Error(), "message is empty") {
		t.Errorf("error should mention empty message, got: %v", err)
	}
	if called {
		t.Error("HTTP server should not have been called for empty message")
	}
}

func TestPushoverSend_EmergencyValidation(t *testing.T) {
	// priority=2 with retry < 30 should fail before any HTTP call
	calledCount := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calledCount++
		if err := r.ParseForm(); err != nil {
			t.Fatalf("parse form: %v", err)
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"status": 1})
	}))
	defer srv.Close()

	client := newTestPushoverClient(srv.URL)

	// retry too low
	err := client.Send(context.Background(), PushoverMessage{
		Message:  "urgent",
		Priority: 2,
		Retry:    10,
		Expire:   3600,
	})
	if err == nil {
		t.Fatal("expected error for retry < 30, got nil")
	}
	if !strings.Contains(err.Error(), "retry >= 30") {
		t.Errorf("error should mention retry, got: %v", err)
	}
	if calledCount != 0 {
		t.Error("HTTP server should not have been called")
	}

	// valid emergency — verify retry and expire are sent
	var gotForm url.Values
	srv2 := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := r.ParseForm(); err != nil {
			t.Fatalf("parse form: %v", err)
		}
		gotForm = r.PostForm
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"status": 1})
	}))
	defer srv2.Close()

	client2 := newTestPushoverClient(srv2.URL)
	err = client2.Send(context.Background(), PushoverMessage{
		Message:  "urgent",
		Priority: 2,
		Retry:    30,
		Expire:   3600,
	})
	if err != nil {
		t.Fatalf("unexpected error for valid emergency: %v", err)
	}
	if gotForm.Get("retry") != "30" {
		t.Errorf("retry: got %q, want %q", gotForm.Get("retry"), "30")
	}
	if gotForm.Get("expire") != "3600" {
		t.Errorf("expire: got %q, want %q", gotForm.Get("expire"), "3600")
	}
}

func TestPushoverSend_OptionalFields(t *testing.T) {
	var gotForm url.Values
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := r.ParseForm(); err != nil {
			t.Fatalf("parse form: %v", err)
		}
		gotForm = r.PostForm
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"status": 1})
	}))
	defer srv.Close()

	client := newTestPushoverClient(srv.URL)
	err := client.Send(context.Background(), PushoverMessage{
		Message: "test message",
		Title:   "Test Title",
		Sound:   "pushover",
		Device:  "myphone",
		URL:     "https://example.com",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	checks := map[string]string{
		"title":  "Test Title",
		"sound":  "pushover",
		"device": "myphone",
		"url":    "https://example.com",
	}
	for field, want := range checks {
		if got := gotForm.Get(field); got != want {
			t.Errorf("field %q: got %q, want %q", field, got, want)
		}
	}
}
