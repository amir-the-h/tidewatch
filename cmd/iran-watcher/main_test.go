package main

import (
	"log/slog"
	"testing"

	"github.com/amir-the-h/iran-watcher/internal/config"
)

// TestConfig_MissingBearerToken verifies that validateConfig returns a non-nil
// error when BearerToken is empty.
func TestConfig_MissingBearerToken(t *testing.T) {
	cfg := &config.Config{}
	// BearerToken is the zero value ("") — should fail validation.
	if err := validateConfig(cfg); err == nil {
		t.Fatal("expected error for missing bearer token, got nil")
	}
}

// TestConfig_WithBearerToken verifies that validateConfig returns nil when
// BearerToken is set.
func TestConfig_WithBearerToken(t *testing.T) {
	cfg := &config.Config{}
	cfg.X.BearerToken = "test-token"
	if err := validateConfig(cfg); err != nil {
		t.Fatalf("expected no error for valid config, got: %v", err)
	}
}

// TestLogLevel_Parsing verifies that each known level string maps to the
// correct slog.Level value.
func TestLogLevel_Parsing(t *testing.T) {
	tests := []struct {
		input string
		want  slog.Level
	}{
		{"debug", slog.LevelDebug},
		{"info", slog.LevelInfo},
		{"warn", slog.LevelWarn},
		{"error", slog.LevelError},
		// Unknown strings should default to info.
		{"", slog.LevelInfo},
		{"unknown", slog.LevelInfo},
	}

	for _, tc := range tests {
		got := parseLogLevel(tc.input)
		if got != tc.want {
			t.Errorf("parseLogLevel(%q) = %v, want %v", tc.input, got, tc.want)
		}
	}
}

// TestConfig_LoadDefaults verifies that loading from a non-existent path
// returns defaults without error (as config.Load documents).
func TestConfig_LoadDefaults(t *testing.T) {
	cfg, err := config.Load("/non/existent/path/iran-watcher.yaml")
	if err != nil {
		t.Fatalf("expected no error loading defaults, got: %v", err)
	}
	if cfg.Redis.Addr != "localhost:6379" {
		t.Errorf("expected default redis addr 'localhost:6379', got %q", cfg.Redis.Addr)
	}
	if cfg.API.Bind != "127.0.0.1:8080" {
		t.Errorf("expected default api bind '127.0.0.1:8080', got %q", cfg.API.Bind)
	}
	if cfg.Log.Level != "info" {
		t.Errorf("expected default log level 'info', got %q", cfg.Log.Level)
	}
	if cfg.Log.Format != "text" {
		t.Errorf("expected default log format 'text', got %q", cfg.Log.Format)
	}
}
