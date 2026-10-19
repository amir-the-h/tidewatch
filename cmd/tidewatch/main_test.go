package main

import (
	"testing"

	"github.com/amir-the-h/tidewatch/internal/config"
)

// TestConfig_LoadDefaults verifies that loading from a non-existent path
// returns defaults without error (as config.Load documents).
func TestConfig_LoadDefaults(t *testing.T) {
	cfg, err := config.Load("/non/existent/path/tidewatch.yaml")
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
