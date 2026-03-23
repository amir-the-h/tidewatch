package config

import (
	"fmt"
	"os"
	"strconv"

	"gopkg.in/yaml.v3"
)

// Config holds all application configuration.
type Config struct {
	X struct {
		BearerToken string `yaml:"bearer_token"`
		Tier        string `yaml:"tier"` // "basic" or "pro"
	} `yaml:"x"`
	Redis struct {
		Addr     string `yaml:"addr"`
		Password string `yaml:"password"`
		DB       int    `yaml:"db"`
	} `yaml:"redis"`
	SQLite struct {
		Path string `yaml:"path"`
	} `yaml:"sqlite"`
	API struct {
		Bind string `yaml:"bind"`
	} `yaml:"api"`
	Worker struct {
		Count int `yaml:"count"`
	} `yaml:"worker"`
	Inference struct {
		ONNXLibPath string `yaml:"onnx_lib_path"`
		ModelsDir   string `yaml:"models_dir"`
	} `yaml:"inference"`
	Log struct {
		Level  string `yaml:"level"`
		Format string `yaml:"format"`
	} `yaml:"log"`
	Pushover struct {
		AppToken string `yaml:"app_token"`
		UserKey  string `yaml:"user_key"`
	} `yaml:"pushover"`
}

// defaults returns a Config populated with sensible default values.
func defaults() *Config {
	cfg := &Config{}
	cfg.X.Tier = "basic"
	cfg.Redis.Addr = "localhost:6379"
	cfg.Redis.DB = 0
	cfg.SQLite.Path = "./data/iran-watcher.db"
	cfg.API.Bind = "127.0.0.1:8080"
	cfg.Inference.ModelsDir = "./models"
	cfg.Log.Level = "info"
	cfg.Log.Format = "text"
	return cfg
}

// Load reads configuration from the given YAML file path (if non-empty and
// the file exists), then applies environment variable overrides on top.
// If path is non-empty but the file does not exist, defaults are returned
// without an error.
func Load(path string) (*Config, error) {
	cfg := defaults()

	if path != "" {
		f, err := os.Open(path)
		if err == nil {
			defer f.Close()
			if err := yaml.NewDecoder(f).Decode(cfg); err != nil {
				return nil, fmt.Errorf("config: decode yaml: %w", err)
			}
		} else if !os.IsNotExist(err) {
			return nil, fmt.Errorf("config: open file: %w", err)
		}
		// If the file does not exist, fall through and use defaults.
	}

	if err := applyEnv(cfg); err != nil {
		return nil, err
	}

	return cfg, nil
}

// applyEnv overrides cfg fields from IRAN_WATCHER_* environment variables.
// Only non-empty env vars are applied.
func applyEnv(cfg *Config) error {
	if v := os.Getenv("IRAN_WATCHER_X_BEARER_TOKEN"); v != "" {
		cfg.X.BearerToken = v
	}
	if v := os.Getenv("IRAN_WATCHER_X_TIER"); v != "" {
		cfg.X.Tier = v
	}
	if v := os.Getenv("IRAN_WATCHER_REDIS_ADDR"); v != "" {
		cfg.Redis.Addr = v
	}
	if v := os.Getenv("IRAN_WATCHER_REDIS_PASSWORD"); v != "" {
		cfg.Redis.Password = v
	}
	if v := os.Getenv("IRAN_WATCHER_REDIS_DB"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil {
			return fmt.Errorf("config: IRAN_WATCHER_REDIS_DB: %w", err)
		}
		cfg.Redis.DB = n
	}
	if v := os.Getenv("IRAN_WATCHER_SQLITE_PATH"); v != "" {
		cfg.SQLite.Path = v
	}
	if v := os.Getenv("IRAN_WATCHER_API_BIND"); v != "" {
		cfg.API.Bind = v
	}
	if v := os.Getenv("IRAN_WATCHER_WORKER_COUNT"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil {
			return fmt.Errorf("config: IRAN_WATCHER_WORKER_COUNT: %w", err)
		}
		cfg.Worker.Count = n
	}
	if v := os.Getenv("IRAN_WATCHER_ONNX_LIB_PATH"); v != "" {
		cfg.Inference.ONNXLibPath = v
	}
	if v := os.Getenv("IRAN_WATCHER_MODELS_DIR"); v != "" {
		cfg.Inference.ModelsDir = v
	}
	if v := os.Getenv("IRAN_WATCHER_LOG_LEVEL"); v != "" {
		cfg.Log.Level = v
	}
	if v := os.Getenv("IRAN_WATCHER_LOG_FORMAT"); v != "" {
		cfg.Log.Format = v
	}
	if v := os.Getenv("IRAN_WATCHER_PUSHOVER_APP_TOKEN"); v != "" {
		cfg.Pushover.AppToken = v
	}
	if v := os.Getenv("IRAN_WATCHER_PUSHOVER_USER_KEY"); v != "" {
		cfg.Pushover.UserKey = v
	}
	return nil
}
