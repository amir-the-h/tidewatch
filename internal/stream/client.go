package stream

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/amir-the-h/iran-watcher/internal/model"
	"github.com/amir-the-h/iran-watcher/internal/notifier"
	"github.com/amir-the-h/iran-watcher/internal/queue"
	"github.com/amir-the-h/iran-watcher/internal/store"
	"github.com/amir-the-h/iran-watcher/internal/worker"
)

const (
	defaultStreamURL = "https://api.twitter.com/2/tweets/search/stream?tweet.fields=created_at,author_id,referenced_tweets,note_tweet&expansions=author_id,referenced_tweets.id&user.fields=username"
	backoffMin       = 1 * time.Second
	backoffMax       = 5 * time.Minute
)

// Client maintains a persistent connection to the X Filtered Stream.
// The stream does not start automatically; call Connect to begin streaming.
type Client struct {
	bearerToken string
	store       store.Store
	producer    queue.Producer
	cache       *worker.ActionCache
	rules       *RuleManager
	httpClient  *http.Client
	logger      *slog.Logger
	streamURL   string                // overridable for tests
	emitter     notifier.EventEmitter // may be nil

	mu        sync.Mutex
	connected bool
	cancel    context.CancelFunc
	done      chan struct{}
}

// NewClient creates a new stream Client. The stream is disconnected by default.
func NewClient(bearerToken string, s store.Store, p queue.Producer, cache *worker.ActionCache, rules *RuleManager, emitter notifier.EventEmitter, logger *slog.Logger) *Client {
	return &Client{
		bearerToken: bearerToken,
		store:       s,
		producer:    p,
		cache:       cache,
		rules:       rules,
		httpClient: &http.Client{
			Transport: &http.Transport{
				ResponseHeaderTimeout: 60 * time.Second,
			},
		},
		logger:    logger,
		streamURL: defaultStreamURL,
		emitter:   emitter,
	}
}

// Connected reports whether the stream is currently active.
func (c *Client) Connected() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.connected
}

// Connect starts the stream connection loop in a background goroutine.
// Returns an error if already connected. The stream runs until Disconnect
// is called or parentCtx is cancelled.
func (c *Client) Connect(parentCtx context.Context) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.connected {
		return fmt.Errorf("stream: already connected")
	}

	ctx, cancel := context.WithCancel(parentCtx)
	c.cancel = cancel
	c.connected = true
	c.done = make(chan struct{})

	go func() {
		defer func() {
			c.mu.Lock()
			c.connected = false
			c.mu.Unlock()
			close(c.done)
		}()

		if err := c.runLoop(ctx); err != nil {
			c.logger.Error("stream: loop exited with error", "component", "stream", "error", err)
		}
	}()

	c.logger.Info("stream: connect requested", "component", "stream")
	return nil
}

// Disconnect stops the stream connection. No-op if already disconnected.
func (c *Client) Disconnect() {
	c.mu.Lock()
	if !c.connected {
		c.mu.Unlock()
		return
	}
	cancel := c.cancel
	done := c.done
	c.mu.Unlock()

	cancel()
	<-done
	c.logger.Info("stream: disconnected", "component", "stream")
	if c.emitter != nil {
		c.emitter.Emit(notifier.NotificationEvent{
			Type:      notifier.EventStreamDisconnected,
			Timestamp: time.Now(),
		})
	}
}

// Start begins the stream connection loop. Blocks until ctx is cancelled.
// Deprecated: Use Connect/Disconnect for non-blocking control.
func (c *Client) Start(ctx context.Context) error {
	return c.runLoop(ctx)
}

// runLoop is the internal reconnect loop with exponential backoff.
func (c *Client) runLoop(ctx context.Context) error {
	backoff := backoffMin

	for {
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
			if c.emitter != nil {
				c.emitter.Emit(notifier.NotificationEvent{
					Type:      notifier.EventStreamError,
					Error:     err.Error(),
					Timestamp: time.Now(),
				})
			}
		} else if ctx.Err() != nil {
			return nil
		} else {
			c.logger.Warn("stream: connection closed, will retry",
				"component", "stream",
				"backoff", backoff,
			)
		}

		if gotEvent {
			backoff = backoffMin
		}

		select {
		case <-ctx.Done():
			return nil
		case <-time.After(backoff):
		}

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
// closes or ctx is cancelled.
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
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		c.logger.Error("stream: unexpected status",
			"component", "stream",
			"status", resp.StatusCode,
			"body", string(body),
		)
		return false, fmt.Errorf("stream: http %d: %s", resp.StatusCode, string(body))
	}

	c.logger.Info("stream: connected", "component", "stream")
	if c.emitter != nil {
		c.emitter.Emit(notifier.NotificationEvent{
			Type:      notifier.EventStreamConnected,
			Timestamp: time.Now(),
		})
	}

	scanner := bufio.NewScanner(resp.Body)
	for scanner.Scan() {
		if ctx.Err() != nil {
			return gotEvent, nil
		}

		line := scanner.Text()

		if strings.TrimSpace(line) == "" {
			continue
		}

		// Strip SSE "data: " prefix if present.
		if strings.HasPrefix(line, "data: ") {
			line = strings.TrimPrefix(line, "data: ")
		} else if strings.HasPrefix(line, ":") {
			// SSE comment / keep-alive, skip.
			continue
		}

		if err := c.processEvent(ctx, []byte(line)); err != nil {
			c.logger.Warn("stream: failed to process event",
				"component", "stream",
				"error", err,
			)
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

	// 2. For each matching rule, look up the Rule by x_rule_id and write tweet_rules.
	var resolvedRuleIDs []int64
	for _, mr := range st.MatchingRules {
		rule, err := c.store.GetRuleByXRuleID(ctx, mr.ID)
		if err != nil {
			c.logger.Warn("stream: rule lookup failed",
				"component", "stream",
				"x_rule_id", mr.ID,
				"error", err,
			)
			continue
		}
		resolvedRuleIDs = append(resolvedRuleIDs, rule.ID)
		tr := &model.TweetRule{
			TweetID: st.Tweet.ID,
			RuleID:  rule.ID,
		}
		if err := c.store.SaveTweetRule(ctx, tr); err != nil {
			c.logger.Warn("stream: save tweet_rule failed",
				"component", "stream",
				"tweet_id", st.Tweet.ID,
				"rule_id", rule.ID,
				"error", err,
			)
		}
	}

	// 2b. Emit tweet_matched event with resolved local rule IDs.
	if c.emitter != nil {
		c.emitter.Emit(notifier.NotificationEvent{
			Type:      notifier.EventTweetMatched,
			Tweet:     st.Tweet,
			RuleIDs:   resolvedRuleIDs,
			Timestamp: time.Now(),
		})
	}

	// TODO(chains): chain execution — deferred to worker execution spec.
	// When implemented, look up trigger_chain_links for "new_tweet" and
	// walk the chain DAG for each matched tweet.

	c.logger.Info("stream: processed tweet",
		"component", "stream",
		"tweet_id", st.Tweet.ID,
		"author", st.Tweet.AuthorHandle,
	)

	return nil
}
