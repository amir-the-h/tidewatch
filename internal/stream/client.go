package stream

import (
	"bufio"
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/amir-the-h/iran-watcher/internal/model"
	"github.com/amir-the-h/iran-watcher/internal/queue"
	"github.com/amir-the-h/iran-watcher/internal/store"
)

const (
	defaultStreamURL  = "https://api.twitter.com/2/tweets/search/stream?tweet.fields=created_at,author_id&expansions=author_id&user.fields=username"
	backoffMin        = 1 * time.Second
	backoffMax        = 5 * time.Minute
)

// Client maintains a persistent connection to the X Filtered Stream.
type Client struct {
	bearerToken string
	store       store.Store
	producer    queue.Producer
	rules       *RuleManager
	httpClient  *http.Client
	logger      *slog.Logger
	streamURL   string // overridable for tests
}

// NewClient creates a new stream Client.
func NewClient(bearerToken string, s store.Store, p queue.Producer, rules *RuleManager, logger *slog.Logger) *Client {
	return &Client{
		bearerToken: bearerToken,
		store:       s,
		producer:    p,
		rules:       rules,
		httpClient:  http.DefaultClient,
		logger:      logger,
		streamURL:   defaultStreamURL,
	}
}

// Start begins the stream connection loop. Blocks until ctx is cancelled.
// For each SSE event received:
//  1. Normalize to StreamTweet
//  2. SaveTweet to store (persists tweet row)
//  3. For each MatchingRule: look up Source by x_rule_id, call SaveTweetSource
//  4. Push StreamTweet to queue.QueueRaw via producer
//
// On any read/connection error: log at warn level, wait with exponential backoff
// (start 1s, double each retry, cap at 5min), retry indefinitely.
// Respect ctx cancellation — return nil when ctx is done.
func (c *Client) Start(ctx context.Context) error {
	backoff := backoffMin

	for {
		// Check for cancellation before attempting a connection.
		if ctx.Err() != nil {
			return nil
		}

		gotEvent, err := c.connect(ctx)
		if err != nil {
			if ctx.Err() != nil {
				return nil
			}
			c.logger.Warn("stream: connection error, will retry",
				"component", "stream",
				"error", err,
				"backoff", backoff,
			)
		} else if ctx.Err() != nil {
			// Stream ended because context was cancelled.
			return nil
		} else {
			// Stream ended cleanly (EOF / server closed).
			c.logger.Warn("stream: connection closed, will retry",
				"component", "stream",
				"backoff", backoff,
			)
		}

		// Reset backoff if we successfully read at least one event.
		if gotEvent {
			backoff = backoffMin
		}

		select {
		case <-ctx.Done():
			return nil
		case <-time.After(backoff):
		}

		// Advance backoff for next failure.
		if !gotEvent {
			backoff *= 2
			if backoff > backoffMax {
				backoff = backoffMax
			}
		} else {
			backoff = backoffMin
		}
	}
}

// connect opens a single stream connection and reads events until the connection
// closes or ctx is cancelled. Returns (true, nil) if at least one event was
// processed, (false, err) on connection/read error.
func (c *Client) connect(ctx context.Context) (gotEvent bool, err error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.streamURL, nil)
	if err != nil {
		return false, fmt.Errorf("stream: build request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+c.bearerToken)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return false, fmt.Errorf("stream: http: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return false, fmt.Errorf("stream: http %d", resp.StatusCode)
	}

	c.logger.Info("stream: connected", "component", "stream")

	scanner := bufio.NewScanner(resp.Body)
	for scanner.Scan() {
		if ctx.Err() != nil {
			return gotEvent, nil
		}

		line := scanner.Text()

		// Empty lines are heartbeats — skip.
		if strings.TrimSpace(line) == "" {
			continue
		}

		// SSE format: lines starting with "data:" carry the event payload.
		if !strings.HasPrefix(line, "data:") {
			continue
		}

		payload := strings.TrimSpace(strings.TrimPrefix(line, "data:"))
		if payload == "" {
			continue
		}

		if err := c.processEvent(ctx, []byte(payload)); err != nil {
			c.logger.Warn("stream: failed to process event",
				"component", "stream",
				"error", err,
			)
			// Don't abort the stream for a single bad event.
			continue
		}
		gotEvent = true
	}

	if scanErr := scanner.Err(); scanErr != nil {
		if ctx.Err() != nil {
			return gotEvent, nil
		}
		return gotEvent, fmt.Errorf("stream: scan: %w", scanErr)
	}

	return gotEvent, nil
}

// processEvent normalizes a raw SSE payload and persists/queues the result.
func (c *Client) processEvent(ctx context.Context, data []byte) error {
	st, err := Normalize(data)
	if err != nil {
		return fmt.Errorf("stream: normalize: %w", err)
	}

	// 1. Persist the tweet.
	if err := c.store.SaveTweet(ctx, st.Tweet); err != nil {
		return fmt.Errorf("stream: save tweet %s: %w", st.Tweet.ID, err)
	}

	// 2. For each matching rule, look up the source and write tweet_sources.
	for _, rule := range st.MatchingRules {
		sources, err := c.store.ListSources(ctx, store.SourceFilter{})
		if err != nil {
			c.logger.Warn("stream: list sources failed",
				"component", "stream",
				"rule_id", rule.ID,
				"error", err,
			)
			continue
		}
		for _, src := range sources {
			if src.XRuleID == rule.ID {
				ts := &model.TweetSource{
					TweetID:  st.Tweet.ID,
					SourceID: src.ID,
				}
				if err := c.store.SaveTweetSource(ctx, ts); err != nil {
					c.logger.Warn("stream: save tweet_source failed",
						"component", "stream",
						"tweet_id", st.Tweet.ID,
						"source_id", src.ID,
						"error", err,
					)
				}
			}
		}
	}

	// 3. Push to the raw queue.
	if err := c.producer.Push(ctx, queue.QueueRaw, st); err != nil {
		return fmt.Errorf("stream: push tweet %s: %w", st.Tweet.ID, err)
	}

	c.logger.Info("stream: processed tweet",
		"component", "stream",
		"tweet_id", st.Tweet.ID,
		"author", st.Tweet.AuthorHandle,
	)

	return nil
}
