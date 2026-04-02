package search

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/amir-the-h/iran-watcher/internal/model"
	"github.com/amir-the-h/iran-watcher/internal/queue"
	"github.com/amir-the-h/iran-watcher/internal/store"
	"github.com/amir-the-h/iran-watcher/internal/worker"
)

const (
	searchRecentURL = "https://api.twitter.com/2/tweets/search/recent"
	searchAllURL    = "https://api.twitter.com/2/tweets/search/all"

	defaultMaxResults = 100
)

// SearchParams controls pagination and field selection for a search request.
type SearchParams struct {
	MaxResults  int       // per page, 1–100; defaults to 100 when 0
	Limit       int       // total tweets per rule (0 = unlimited)
	StartTime   time.Time // zero = API default
	EndTime     time.Time
	SinceID     string
	UntilID     string
	TweetFields []string
	UserFields  []string
	Expansions  []string
	MediaFields []string
	PlaceFields []string
	PollFields  []string
}

// Client fetches tweets from the X API v2 search endpoints and pushes them
// into the existing worker pipeline via Redis.
type Client struct {
	httpClient  *http.Client
	bearerToken string
	store       store.Store
	producer    queue.Producer
	cache       *worker.ActionCache // may be nil; if nil, no tasks are dispatched
	tier        string              // "basic" | "pro"
	logger      *slog.Logger
}

// NewClient creates a new search Client.
func NewClient(bearerToken, tier string, st store.Store, producer queue.Producer, logger *slog.Logger) *Client {
	return &Client{
		httpClient:  &http.Client{Timeout: 30 * time.Second},
		bearerToken: bearerToken,
		store:       st,
		producer:    producer,
		tier:        tier,
		logger:      logger.With("component", "search"),
	}
}

// WithActionCache sets the ActionCache used to dispatch tasks for ingested
// tweets. Call this before starting the scheduler.
func (c *Client) WithActionCache(cache *worker.ActionCache) {
	c.cache = cache
}

// searchURL returns the appropriate endpoint based on the account tier.
func (c *Client) searchURL() string {
	if c.tier == "pro" {
		return searchAllURL
	}
	return searchRecentURL
}

// buildQuery constructs url.Values from SearchParams plus the rule query string.
// nextToken is appended as pagination_token when non-empty.
func (c *Client) buildQuery(rule *model.Rule, params SearchParams, nextToken string) url.Values {
	maxResults := params.MaxResults
	if maxResults <= 0 {
		maxResults = defaultMaxResults
	}

	q := url.Values{}
	q.Set("query", rule.CompiledValue)
	q.Set("max_results", strconv.Itoa(maxResults))

	if len(params.TweetFields) > 0 {
		q.Set("tweet.fields", strings.Join(params.TweetFields, ","))
	}
	if len(params.UserFields) > 0 {
		q.Set("user.fields", strings.Join(params.UserFields, ","))
	}
	if len(params.Expansions) > 0 {
		q.Set("expansions", strings.Join(params.Expansions, ","))
	}
	if len(params.MediaFields) > 0 {
		q.Set("media.fields", strings.Join(params.MediaFields, ","))
	}
	if len(params.PlaceFields) > 0 {
		q.Set("place.fields", strings.Join(params.PlaceFields, ","))
	}
	if len(params.PollFields) > 0 {
		q.Set("poll.fields", strings.Join(params.PollFields, ","))
	}
	if !params.StartTime.IsZero() {
		q.Set("start_time", params.StartTime.UTC().Format(time.RFC3339))
	}
	if !params.EndTime.IsZero() {
		q.Set("end_time", params.EndTime.UTC().Format(time.RFC3339))
	}
	if params.SinceID != "" {
		q.Set("since_id", params.SinceID)
	}
	if params.UntilID != "" {
		q.Set("until_id", params.UntilID)
	}
	if nextToken != "" {
		q.Set("pagination_token", nextToken)
	}

	return q
}

// fetchPage makes one HTTP GET to the search endpoint and returns the response
// body bytes and headers. It handles 429 / rate-limit-remaining=0 by sleeping
// until x-rate-limit-reset and retrying once.
func (c *Client) fetchPage(ctx context.Context, endpoint string, params url.Values) ([]byte, http.Header, error) {
	doRequest := func() (*http.Response, error) {
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint+"?"+params.Encode(), nil)
		if err != nil {
			return nil, fmt.Errorf("search: build request: %w", err)
		}
		req.Header.Set("Authorization", "Bearer "+c.bearerToken)
		return c.httpClient.Do(req)
	}

	resp, err := doRequest()
	if err != nil {
		return nil, nil, fmt.Errorf("search: fetch page: %w", err)
	}
	defer resp.Body.Close()

	// Handle rate limiting: 429 or x-rate-limit-remaining == "0".
	if resp.StatusCode == http.StatusTooManyRequests || resp.Header.Get("x-rate-limit-remaining") == "0" {
		resetHeader := resp.Header.Get("x-rate-limit-reset")
		var sleepUntil time.Time
		if resetHeader != "" {
			if ts, parseErr := strconv.ParseInt(resetHeader, 10, 64); parseErr == nil {
				sleepUntil = time.Unix(ts+1, 0)
			}
		}
		if sleepUntil.IsZero() {
			sleepUntil = time.Now().Add(60 * time.Second)
		}

		sleepDur := time.Until(sleepUntil)
		c.logger.InfoContext(ctx, "rate limited; sleeping until reset",
			"sleep_seconds", sleepDur.Seconds(),
			"reset_at", sleepUntil,
		)

		select {
		case <-ctx.Done():
			return nil, nil, fmt.Errorf("search: fetch page: context cancelled during rate-limit sleep: %w", ctx.Err())
		case <-time.After(sleepDur):
		}

		// Retry once after sleeping.
		resp2, err2 := doRequest()
		if err2 != nil {
			return nil, nil, fmt.Errorf("search: fetch page (retry): %w", err2)
		}
		defer resp2.Body.Close()

		body, readErr := io.ReadAll(resp2.Body)
		if readErr != nil {
			return nil, nil, fmt.Errorf("search: read response body (retry): %w", readErr)
		}
		if resp2.StatusCode != http.StatusOK {
			return nil, nil, fmt.Errorf("search: fetch page (retry): unexpected status %d: %s", resp2.StatusCode, body)
		}
		return body, resp2.Header, nil
	}

	body, readErr := io.ReadAll(resp.Body)
	if readErr != nil {
		return nil, nil, fmt.Errorf("search: read response body: %w", readErr)
	}
	if resp.StatusCode != http.StatusOK {
		return nil, nil, fmt.Errorf("search: fetch page: unexpected status %d: %s", resp.StatusCode, body)
	}
	return body, resp.Header, nil
}

// RunForRule fetches all pages for a single rule's compiled query, storing
// each tweet in SQLite and pushing it onto the raw Redis queue.
// Returns the total number of tweets fetched and the newest tweet ID seen
// (meta.newest_id from the first page), which callers can use as since_id
// on subsequent runs to avoid re-fetching already-processed tweets.
func (c *Client) RunForRule(ctx context.Context, rule *model.Rule, params SearchParams) (int, string, error) {
	endpoint := c.searchURL()
	fetched := 0
	nextToken := ""
	newestID := ""

	for {
		// Respect the per-rule Limit by trimming max_results on the last page.
		pageParams := params
		if params.Limit > 0 {
			remaining := params.Limit - fetched
			if remaining <= 0 {
				break
			}
			maxR := params.MaxResults
			if maxR <= 0 {
				maxR = defaultMaxResults
			}
			if remaining < maxR {
				pageParams.MaxResults = remaining
			}
		}

		queryVals := c.buildQuery(rule, pageParams, nextToken)
		body, _, err := c.fetchPage(ctx, endpoint, queryVals)
		if err != nil {
			return fetched, newestID, fmt.Errorf("search: RunForRule rule %d: %w", rule.ID, err)
		}

		page, err := NormalizeSearchResponse(body, rule)
		if err != nil {
			return fetched, newestID, fmt.Errorf("search: RunForRule rule %d: normalize: %w", rule.ID, err)
		}

		// Capture the newest tweet ID from the first page only; subsequent pages
		// contain older tweets so the first page's value is the authoritative bound.
		if newestID == "" && page.NewestID != "" {
			newestID = page.NewestID
		}

		for _, st := range page.Tweets {
			if err := c.store.SaveTweet(ctx, st.Tweet); err != nil {
				return fetched, newestID, fmt.Errorf("search: RunForRule rule %d: save tweet %s: %w", rule.ID, st.Tweet.ID, err)
			}
			tr := &model.TweetRule{
				TweetID: st.Tweet.ID,
				RuleID:  rule.ID,
			}
			if err := c.store.SaveTweetRule(ctx, tr); err != nil {
				return fetched, newestID, fmt.Errorf("search: RunForRule rule %d: save tweet rule %s: %w", rule.ID, st.Tweet.ID, err)
			}
			ev := queue.TweetEvent{
				TweetID:      st.Tweet.ID,
				Text:         st.Tweet.Text,
				AuthorHandle: st.Tweet.AuthorHandle,
				RuleIDs:      []int64{rule.ID},
			}
			if pushErr := worker.PushInitialPipelineTasks(ctx, c.store, c.producer, c.cache, "new_tweet", ev, c.logger); pushErr != nil && c.logger != nil {
				c.logger.Warn("search: chain dispatch failed", "tweet_id", st.Tweet.ID, "error", pushErr)
			}
		}

		fetched += len(page.Tweets)

		if page.NextToken == "" {
			break
		}
		if params.Limit > 0 && fetched >= params.Limit {
			break
		}
		nextToken = page.NextToken
	}

	return fetched, newestID, nil
}
