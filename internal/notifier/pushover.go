package notifier

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

const defaultPushoverURL = "https://api.pushover.net/1/messages.json"

type PushoverMessage struct {
	Message  string
	Title    string
	Priority int
	Sound    string
	Device   string
	URL      string
	URLTitle string
	TTL      int
	HTML     bool
	Retry    int
	Expire   int
}

type PushoverClient struct {
	appToken   string
	userKey    string
	apiURL     string
	httpClient *http.Client
	logger     *slog.Logger
}

func NewPushoverClient(appToken, userKey string, logger *slog.Logger) *PushoverClient {
	return &PushoverClient{
		appToken: appToken,
		userKey:  userKey,
		apiURL:   defaultPushoverURL,
		httpClient: &http.Client{
			Timeout: 10 * time.Second,
		},
		logger: logger,
	}
}

func (c *PushoverClient) Send(ctx context.Context, msg PushoverMessage) error {
	if msg.Message == "" {
		return fmt.Errorf("notifier: pushover: message is empty")
	}
	if msg.Priority == 2 {
		if msg.Retry < 30 {
			return fmt.Errorf("notifier: pushover: emergency priority requires retry >= 30")
		}
		if msg.Expire <= 0 || msg.Expire > 10800 {
			return fmt.Errorf("notifier: pushover: emergency priority requires 0 < expire <= 10800")
		}
	}

	form := url.Values{
		"token":   {c.appToken},
		"user":    {c.userKey},
		"message": {msg.Message},
	}
	if msg.Title != "" {
		form.Set("title", msg.Title)
	}
	if msg.Priority != 0 {
		form.Set("priority", strconv.Itoa(msg.Priority))
	}
	if msg.Sound != "" {
		form.Set("sound", msg.Sound)
	}
	if msg.Device != "" {
		form.Set("device", msg.Device)
	}
	if msg.URL != "" {
		form.Set("url", msg.URL)
	}
	if msg.URLTitle != "" {
		form.Set("url_title", msg.URLTitle)
	}
	if msg.TTL > 0 {
		form.Set("ttl", strconv.Itoa(msg.TTL))
	}
	if msg.HTML {
		form.Set("html", "1")
	}
	if msg.Priority == 2 {
		form.Set("retry", strconv.Itoa(msg.Retry))
		form.Set("expire", strconv.Itoa(msg.Expire))
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.apiURL, strings.NewReader(form.Encode()))
	if err != nil {
		return fmt.Errorf("notifier: pushover: build request: %w", err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("notifier: pushover: http: %w", err)
	}
	defer resp.Body.Close()

	var result struct {
		Status int      `json:"status"`
		Errors []string `json:"errors"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return fmt.Errorf("notifier: pushover: decode response: %w", err)
	}
	if result.Status != 1 {
		return fmt.Errorf("notifier: pushover: API error: %v", result.Errors)
	}

	return nil
}

// Deliver implements notifier.Provider for PushoverClient.
// config keys: "title", "priority", "sound", "device", "url", "url_title", "ttl", "html", "retry", "expire"
func (c *PushoverClient) Deliver(ctx context.Context, text string, config map[string]any) error {
	msg := PushoverMessage{
		Message: text,
	}
	if v, ok := config["title"].(string); ok {
		msg.Title = v
	}
	if v, ok := config["priority"].(float64); ok {
		msg.Priority = int(v)
	}
	if v, ok := config["sound"].(string); ok {
		msg.Sound = v
	}
	if v, ok := config["device"].(string); ok {
		msg.Device = v
	}
	if v, ok := config["url"].(string); ok {
		msg.URL = v
	}
	if v, ok := config["url_title"].(string); ok {
		msg.URLTitle = v
	}
	if v, ok := config["ttl"].(float64); ok {
		msg.TTL = int(v)
	}
	if v, ok := config["html"].(bool); ok {
		msg.HTML = v
	}
	if v, ok := config["retry"].(float64); ok {
		msg.Retry = int(v)
	}
	if v, ok := config["expire"].(float64); ok {
		msg.Expire = int(v)
	}
	return c.Send(ctx, msg)
}
