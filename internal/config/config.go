package config

import (
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

// OllamaBackendConfig holds the URL and optional display name for a single Ollama backend.
type OllamaBackendConfig struct {
	URL  string `yaml:"url"`
	Name string `yaml:"name"` // optional display name; defaults to URL if empty
}

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
	Ollama struct {
		BaseURL         string                `yaml:"base_url"`          // single URL — kept for backward compatibility; use base_urls for multiple backends
		BaseURLs        []string              `yaml:"base_urls"`         // pool of Ollama server addresses; takes precedence over base_url when set
		Backends        []OllamaBackendConfig `yaml:"backends"`          // named backends (takes precedence over base_urls/base_url); supports display names
		FastWaitTimeout time.Duration         `yaml:"fast_wait_timeout"` // how long acquireFastest waits for the best backend before falling back; default 5s
	} `yaml:"ollama"`
	Log struct {
		Level  string `yaml:"level"`
		Format string `yaml:"format"`
	} `yaml:"log"`
	Pushover struct {
		AppToken string `yaml:"app_token"`
		UserKey  string `yaml:"user_key"`
	} `yaml:"pushover"`
	Telegram struct {
		BotToken string `yaml:"bot_token"`
	} `yaml:"telegram"`
	Stream struct {
		AutoConnect bool `yaml:"auto_connect"`
	} `yaml:"stream"`
	Search struct {
		Enabled     bool          `yaml:"enabled"`
		Interval    time.Duration `yaml:"interval"`
		MaxResults  int           `yaml:"max_results"`
		Limit       int           `yaml:"limit"`
		TweetFields []string      `yaml:"tweet_fields"`
		UserFields  []string      `yaml:"user_fields"`
		Expansions  []string      `yaml:"expansions"`
		StartTime   string        `yaml:"start_time"`
	} `yaml:"search"`
	Auth struct {
		JWTSecret     string `yaml:"jwt_secret"`
		AdminUsername string `yaml:"admin_username"`
		AdminPassword string `yaml:"admin_password"`
	} `yaml:"auth"`
	Twitter struct {
		ClientID     string `yaml:"client_id"`
		ClientSecret string `yaml:"client_secret"`
		CallbackURL  string `yaml:"callback_url"`
	} `yaml:"twitter"`
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
	cfg.Ollama.BaseURL = "http://localhost:11434" // default used when base_urls is not set
	cfg.Ollama.FastWaitTimeout = 5 * time.Second
	cfg.Log.Level = "info"
	cfg.Log.Format = "text"
	cfg.Search.Enabled = false
	cfg.Search.Interval = 15 * time.Minute
	cfg.Search.MaxResults = 100
	cfg.Search.Limit = 0
	cfg.Search.TweetFields = []string{"author_id", "created_at", "referenced_tweets", "text"}
	cfg.Search.UserFields = []string{"username"}
	cfg.Search.Expansions = []string{"author_id", "referenced_tweets.id"}
	cfg.Auth.AdminUsername = "admin"
	cfg.Auth.AdminPassword = "changeme"
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

// OllamaBackends returns the effective list of Ollama backends with optional
// display names. Precedence: backends > base_urls > base_url > default.
func (cfg *Config) OllamaBackends() []OllamaBackendConfig {
	if len(cfg.Ollama.Backends) > 0 {
		return cfg.Ollama.Backends
	}
	if len(cfg.Ollama.BaseURLs) > 0 {
		out := make([]OllamaBackendConfig, len(cfg.Ollama.BaseURLs))
		for i, u := range cfg.Ollama.BaseURLs {
			out[i] = OllamaBackendConfig{URL: u}
		}
		return out
	}
	if cfg.Ollama.BaseURL != "" {
		return []OllamaBackendConfig{{URL: cfg.Ollama.BaseURL}}
	}
	return []OllamaBackendConfig{{URL: "http://localhost:11434"}}
}

// OllamaBaseURLs returns the effective list of Ollama backend URLs.
// Delegates to OllamaBackends for consistent precedence logic.
// Kept for backward compatibility; prefer OllamaBackends() for new code.
func (cfg *Config) OllamaBaseURLs() []string {
	backends := cfg.OllamaBackends()
	urls := make([]string, len(backends))
	for i, b := range backends {
		urls[i] = b.URL
	}
	return urls
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
	if v := os.Getenv("IRAN_WATCHER_OLLAMA_BASE_URL"); v != "" {
		cfg.Ollama.BaseURL = v
	}
	if v := os.Getenv("IRAN_WATCHER_OLLAMA_BASE_URLS"); v != "" {
		urls := strings.Split(v, ",")
		for i, u := range urls {
			urls[i] = strings.TrimSpace(u)
		}
		cfg.Ollama.BaseURLs = urls
	}
	if v := os.Getenv("IRAN_WATCHER_OLLAMA_FAST_WAIT_TIMEOUT"); v != "" {
		d, err := time.ParseDuration(v)
		if err != nil {
			return fmt.Errorf("config: IRAN_WATCHER_OLLAMA_FAST_WAIT_TIMEOUT: %w", err)
		}
		cfg.Ollama.FastWaitTimeout = d
	}
	if v := os.Getenv("IRAN_WATCHER_PUSHOVER_APP_TOKEN"); v != "" {
		cfg.Pushover.AppToken = v
	}
	if v := os.Getenv("IRAN_WATCHER_PUSHOVER_USER_KEY"); v != "" {
		cfg.Pushover.UserKey = v
	}
	if v := os.Getenv("IRAN_WATCHER_TELEGRAM_BOT_TOKEN"); v != "" {
		cfg.Telegram.BotToken = v
	}
	if v := os.Getenv("IRAN_WATCHER_STREAM_AUTO_CONNECT"); v != "" {
		cfg.Stream.AutoConnect = v == "true" || v == "1"
	}
	if v := os.Getenv("IRAN_WATCHER_AUTH_JWT_SECRET"); v != "" {
		cfg.Auth.JWTSecret = v
	}
	if v := os.Getenv("IRAN_WATCHER_AUTH_ADMIN_USERNAME"); v != "" {
		cfg.Auth.AdminUsername = v
	}
	if v := os.Getenv("IRAN_WATCHER_AUTH_ADMIN_PASSWORD"); v != "" {
		cfg.Auth.AdminPassword = v
	}
	if v := os.Getenv("IRAN_WATCHER_TWITTER_CLIENT_ID"); v != "" {
		cfg.Twitter.ClientID = v
	}
	if v := os.Getenv("IRAN_WATCHER_TWITTER_CLIENT_SECRET"); v != "" {
		cfg.Twitter.ClientSecret = v
	}
	if v := os.Getenv("IRAN_WATCHER_TWITTER_CALLBACK_URL"); v != "" {
		cfg.Twitter.CallbackURL = v
	}
	return nil
}
