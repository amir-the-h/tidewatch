package config

import (
	"fmt"
	"os"
	"strconv"
	"time"

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
	Sources struct {
		// MaxConcurrentPolls caps how many poll-source fetches run at once.
		MaxConcurrentPolls int `yaml:"max_concurrent_polls"`
	} `yaml:"sources"`
	Inference struct {
		ONNXLibPath string `yaml:"onnx_lib_path"`
		ModelsDir   string `yaml:"models_dir"`
	} `yaml:"inference"`
	Ollama struct {
		FastWaitTimeout time.Duration `yaml:"fast_wait_timeout"`
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
	Nominatim struct {
		URL string `yaml:"url"`
	} `yaml:"nominatim"`
}

// defaults returns a Config populated with sensible default values.
func defaults() *Config {
	cfg := &Config{}
	cfg.X.Tier = "basic"
	cfg.Redis.Addr = "localhost:6379"
	cfg.Redis.DB = 0
	cfg.SQLite.Path = "./data/tidewatch.db"
	cfg.API.Bind = "127.0.0.1:8080"
	cfg.Inference.ModelsDir = "./models"
	cfg.Ollama.FastWaitTimeout = 5 * time.Second
	cfg.Sources.MaxConcurrentPolls = 8
	cfg.Log.Level = "info"
	cfg.Log.Format = "text"
	cfg.Search.Enabled = false
	cfg.Search.Interval = 15 * time.Minute
	cfg.Search.MaxResults = 100
	cfg.Search.Limit = 0
	cfg.Search.TweetFields = []string{"author_id", "created_at", "referenced_tweets", "text", "note_tweet"}
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

// applyEnv overrides cfg fields from TIDEWATCH_* environment variables.
// Only non-empty env vars are applied.
func applyEnv(cfg *Config) error {
	if v := os.Getenv("TIDEWATCH_X_BEARER_TOKEN"); v != "" {
		cfg.X.BearerToken = v
	}
	if v := os.Getenv("TIDEWATCH_X_TIER"); v != "" {
		cfg.X.Tier = v
	}
	if v := os.Getenv("TIDEWATCH_REDIS_ADDR"); v != "" {
		cfg.Redis.Addr = v
	}
	if v := os.Getenv("TIDEWATCH_REDIS_PASSWORD"); v != "" {
		cfg.Redis.Password = v
	}
	if v := os.Getenv("TIDEWATCH_REDIS_DB"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil {
			return fmt.Errorf("config: TIDEWATCH_REDIS_DB: %w", err)
		}
		cfg.Redis.DB = n
	}
	if v := os.Getenv("TIDEWATCH_SQLITE_PATH"); v != "" {
		cfg.SQLite.Path = v
	}
	if v := os.Getenv("TIDEWATCH_API_BIND"); v != "" {
		cfg.API.Bind = v
	}
	if v := os.Getenv("TIDEWATCH_WORKER_COUNT"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil {
			return fmt.Errorf("config: TIDEWATCH_WORKER_COUNT: %w", err)
		}
		cfg.Worker.Count = n
	}
	if v := os.Getenv("TIDEWATCH_SOURCES_MAX_CONCURRENT_POLLS"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil {
			return fmt.Errorf("config: TIDEWATCH_SOURCES_MAX_CONCURRENT_POLLS: %w", err)
		}
		cfg.Sources.MaxConcurrentPolls = n
	}
	if v := os.Getenv("TIDEWATCH_ONNX_LIB_PATH"); v != "" {
		cfg.Inference.ONNXLibPath = v
	}
	if v := os.Getenv("TIDEWATCH_MODELS_DIR"); v != "" {
		cfg.Inference.ModelsDir = v
	}
	if v := os.Getenv("TIDEWATCH_LOG_LEVEL"); v != "" {
		cfg.Log.Level = v
	}
	if v := os.Getenv("TIDEWATCH_LOG_FORMAT"); v != "" {
		cfg.Log.Format = v
	}
	if v := os.Getenv("TIDEWATCH_OLLAMA_FAST_WAIT_TIMEOUT"); v != "" {
		d, err := time.ParseDuration(v)
		if err != nil {
			return fmt.Errorf("config: TIDEWATCH_OLLAMA_FAST_WAIT_TIMEOUT: %w", err)
		}
		cfg.Ollama.FastWaitTimeout = d
	}
	if v := os.Getenv("TIDEWATCH_PUSHOVER_APP_TOKEN"); v != "" {
		cfg.Pushover.AppToken = v
	}
	if v := os.Getenv("TIDEWATCH_PUSHOVER_USER_KEY"); v != "" {
		cfg.Pushover.UserKey = v
	}
	if v := os.Getenv("TIDEWATCH_TELEGRAM_BOT_TOKEN"); v != "" {
		cfg.Telegram.BotToken = v
	}
	if v := os.Getenv("TIDEWATCH_STREAM_AUTO_CONNECT"); v != "" {
		cfg.Stream.AutoConnect = v == "true" || v == "1"
	}
	if v := os.Getenv("TIDEWATCH_AUTH_JWT_SECRET"); v != "" {
		cfg.Auth.JWTSecret = v
	}
	if v := os.Getenv("TIDEWATCH_AUTH_ADMIN_USERNAME"); v != "" {
		cfg.Auth.AdminUsername = v
	}
	if v := os.Getenv("TIDEWATCH_AUTH_ADMIN_PASSWORD"); v != "" {
		cfg.Auth.AdminPassword = v
	}
	if v := os.Getenv("TIDEWATCH_TWITTER_CLIENT_ID"); v != "" {
		cfg.Twitter.ClientID = v
	}
	if v := os.Getenv("TIDEWATCH_TWITTER_CLIENT_SECRET"); v != "" {
		cfg.Twitter.ClientSecret = v
	}
	if v := os.Getenv("TIDEWATCH_TWITTER_CALLBACK_URL"); v != "" {
		cfg.Twitter.CallbackURL = v
	}
	if v := os.Getenv("TIDEWATCH_NOMINATIM_URL"); v != "" {
		cfg.Nominatim.URL = v
	}
	return nil
}
