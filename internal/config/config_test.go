package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadEmpty_ReturnsDefaults(t *testing.T) {
	t.Setenv("TIDEWATCH_X_BEARER_TOKEN", "")

	cfg, err := Load("")
	if err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}
	if cfg.Redis.Addr != "localhost:6379" {
		t.Errorf("expected Redis.Addr=localhost:6379, got %q", cfg.Redis.Addr)
	}
	if cfg.API.Bind != "127.0.0.1:8080" {
		t.Errorf("expected API.Bind=127.0.0.1:8080, got %q", cfg.API.Bind)
	}
}

func TestLoadYAML_OverridesDefaults(t *testing.T) {
	// Clear env vars so they don't override YAML values under test.
	t.Setenv("TIDEWATCH_X_BEARER_TOKEN", "")

	content := `
x:
  bearer_token: "my-token"
redis:
  addr: "redis:6380"
`
	tmp := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(tmp, []byte(content), 0o600); err != nil {
		t.Fatalf("failed to write temp file: %v", err)
	}

	cfg, err := Load(tmp)
	if err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}
	if cfg.X.BearerToken != "my-token" {
		t.Errorf("expected X.BearerToken=my-token, got %q", cfg.X.BearerToken)
	}
	if cfg.Redis.Addr != "redis:6380" {
		t.Errorf("expected Redis.Addr=redis:6380, got %q", cfg.Redis.Addr)
	}
	// Non-overridden fields stay at defaults.
	if cfg.API.Bind != "127.0.0.1:8080" {
		t.Errorf("expected API.Bind=127.0.0.1:8080, got %q", cfg.API.Bind)
	}
	if cfg.Log.Level != "info" {
		t.Errorf("expected Log.Level=info, got %q", cfg.Log.Level)
	}
}

func TestLoadNonExistentPath_ReturnsDefaults(t *testing.T) {
	t.Setenv("TIDEWATCH_X_BEARER_TOKEN", "")

	cfg, err := Load("nonexistent-path.yaml")
	if err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}
	if cfg.Redis.Addr != "localhost:6379" {
		t.Errorf("expected Redis.Addr=localhost:6379, got %q", cfg.Redis.Addr)
	}
	if cfg.API.Bind != "127.0.0.1:8080" {
		t.Errorf("expected API.Bind=127.0.0.1:8080, got %q", cfg.API.Bind)
	}
}

func TestEnvVar_BearerToken(t *testing.T) {
	t.Setenv("TIDEWATCH_X_BEARER_TOKEN", "env-token")

	cfg, err := Load("")
	if err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}
	if cfg.X.BearerToken != "env-token" {
		t.Errorf("expected X.BearerToken=env-token, got %q", cfg.X.BearerToken)
	}
}

func TestEnvVar_RedisDB_Valid(t *testing.T) {
	t.Setenv("TIDEWATCH_REDIS_DB", "5")

	cfg, err := Load("")
	if err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}
	if cfg.Redis.DB != 5 {
		t.Errorf("expected Redis.DB=5, got %d", cfg.Redis.DB)
	}
}

func TestEnvVar_RedisDB_Invalid(t *testing.T) {
	t.Setenv("TIDEWATCH_REDIS_DB", "notanumber")

	_, err := Load("")
	if err == nil {
		t.Fatal("expected error for invalid TIDEWATCH_REDIS_DB, got nil")
	}
}

func TestEnvVar_WorkerCount_Invalid(t *testing.T) {
	t.Setenv("TIDEWATCH_WORKER_COUNT", "notanumber")

	_, err := Load("")
	if err == nil {
		t.Fatal("expected error for invalid TIDEWATCH_WORKER_COUNT, got nil")
	}
}

func TestSourcesMaxConcurrentPolls(t *testing.T) {
	cfg, err := Load("")
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Sources.MaxConcurrentPolls != 8 {
		t.Errorf("default: %d, want 8", cfg.Sources.MaxConcurrentPolls)
	}

	t.Setenv("TIDEWATCH_SOURCES_MAX_CONCURRENT_POLLS", "3")
	if cfg, _ = Load(""); cfg.Sources.MaxConcurrentPolls != 3 {
		t.Errorf("env: %d, want 3", cfg.Sources.MaxConcurrentPolls)
	}

	t.Setenv("TIDEWATCH_SOURCES_MAX_CONCURRENT_POLLS", "many")
	if _, err := Load(""); err == nil {
		t.Error("invalid TIDEWATCH_SOURCES_MAX_CONCURRENT_POLLS: want error")
	}
}
