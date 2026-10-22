package x

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/amir-the-h/tidewatch/internal/model"
	"github.com/amir-the-h/tidewatch/internal/source"
	"github.com/amir-the-h/tidewatch/internal/store"
)

const (
	searchRecentPath = "/2/tweets/search/recent"
	searchAllPath    = "/2/tweets/search/all"

	defaultMaxResults = 100
)

var (
	defaultTweetFields = []string{"author_id", "created_at", "referenced_tweets", "text", "note_tweet"}
	defaultUserFields  = []string{"username"}
	defaultExpansions  = []string{"author_id", "referenced_tweets.id"}
)

// SearchParams controls pagination and field selection for a search request.
type SearchParams struct {
	MaxResults  int       // per page; defaults to 100 when 0
	Limit       int       // total tweets per query (0 = unlimited)
	StartTime   time.Time // zero = API default
	EndTime     time.Time
	SinceID     string
	TweetFields []string
	UserFields  []string
	Expansions  []string
}

// searchConfig is an x_search source's config.
type searchConfig struct {
	credentials
	Query       string   `json:"query"`
	RuleIDs     []int64  `json:"rule_ids"`
	MaxResults  int      `json:"max_results"`
	Limit       int      `json:"limit"`
	StartTime   string   `json:"start_time"`
	TweetFields []string `json:"tweet_fields"`
	UserFields  []string `json:"user_fields"`
	Expansions  []string `json:"expansions"`
}

func parseSearchConfig(raw json.RawMessage) (*searchConfig, error) {
	var cfg searchConfig
	if err := json.Unmarshal(raw, &cfg); err != nil {
		return nil, err
	}
	return &cfg, nil
}

// params returns the base search params of a config.
func (cfg *searchConfig) params() SearchParams {
	p := SearchParams{
		MaxResults:  cfg.MaxResults,
		Limit:       cfg.Limit,
		TweetFields: cfg.TweetFields,
		UserFields:  cfg.UserFields,
		Expansions:  cfg.Expansions,
	}
	if p.TweetFields == nil {
		p.TweetFields = defaultTweetFields
	}
	if p.UserFields == nil {
		p.UserFields = defaultUserFields
	}
	if p.Expansions == nil {
		p.Expansions = defaultExpansions
	}
	if cfg.StartTime != "" {
		p.StartTime, _ = time.Parse(time.RFC3339, cfg.StartTime) // checked by Validate
	}
	return p
}

// searchCursor is an x_search source's cursor: the newest tweet ID seen per
// query, so each poll only fetches what is new.
type searchCursor struct {
	SinceIDs map[string]string `json:"since_ids"`
}

// target is one query a poll runs: the source's own query, or a compiled
// x_rule (whose matches are recorded in x_rule_matches).
type target struct {
	key    string // cursor key: "query" or "rule:<id>"
	query  string
	ruleID int64 // 0 for a raw query
}

// Search is the x_search connector: X recent search (full-archive search on
// the pro and enterprise tiers), polled on an interval and backfillable.
type Search struct {
	store   store.Store
	apiBase string
}

// NewSearch returns the x_search connector.
func NewSearch(s store.Store) *Search {
	return &Search{store: s, apiBase: defaultAPIBase}
}

// Descriptor implements source.Connector.
func (c *Search) Descriptor() source.Descriptor {
	fields := append(credentialFields(),
		source.Field{Key: "query", Label: "Query", Type: source.FieldString,
			Help: "An X search query. Leave empty to search with X rules instead"},
		source.Field{Key: "rule_ids", Label: "Rules", Type: source.FieldIntList,
			Help: "X rule IDs to search with; empty (and no query) = every active non-local rule"},
		source.Field{Key: "max_results", Label: "Results per page", Type: source.FieldInt, Default: defaultMaxResults,
			Help: "10–100 on recent search, up to 500 on full-archive search"},
		source.Field{Key: "limit", Label: "Limit per query", Type: source.FieldInt, Default: 0,
			Help: "Most tweets one poll fetches per query; 0 = no limit"},
		source.Field{Key: "start_time", Label: "Start time", Type: source.FieldString,
			Help: "RFC 3339; only used until a query has a since_id. Empty = X's default (7 days on recent search)"},
		source.Field{Key: "tweet_fields", Label: "Tweet fields", Type: source.FieldStringList, Default: defaultTweetFields},
		source.Field{Key: "user_fields", Label: "User fields", Type: source.FieldStringList, Default: defaultUserFields},
		source.Field{Key: "expansions", Label: "Expansions", Type: source.FieldStringList, Default: defaultExpansions},
	)
	return source.Descriptor{
		Type:            TypeSearch,
		Label:           "X search",
		Mode:            source.ModePoll,
		ExtNS:           ExtNS,
		Kinds:           []model.SignalKind{model.SignalKindPost},
		Fields:          fields,
		DefaultInterval: 15 * time.Minute,
		MinInterval:     5 * time.Minute,
		Backfill:        true,
	}
}

// Validate implements source.Connector.
func (c *Search) Validate(raw json.RawMessage) error {
	cfg, err := parseSearchConfig(raw)
	if err != nil {
		return err
	}
	if err := validateTier(cfg.Tier); err != nil {
		return err
	}
	if cfg.Query != "" && len(cfg.RuleIDs) > 0 {
		return errors.New("set query or rule_ids, not both")
	}
	if cfg.MaxResults != 0 && (cfg.MaxResults < 10 || cfg.MaxResults > 500) {
		return errors.New("max_results must be between 10 and 500")
	}
	if cfg.Limit < 0 {
		return errors.New("limit can't be negative")
	}
	if cfg.StartTime != "" {
		if _, err := time.Parse(time.RFC3339, cfg.StartTime); err != nil {
			return errors.New("start_time must be an RFC 3339 time")
		}
	}
	return nil
}

// Poll implements source.PollConnector: run every target from its since_id
// and advance the cursor. A target that fails is logged and keeps its old
// since_id; the poll fails only when every target does.
func (c *Search) Poll(ctx context.Context, src *model.Source, rt source.Runtime) error {
	cfg, err := parseSearchConfig(src.Config)
	if err != nil {
		return fmt.Errorf("x_search: config: %w", err)
	}
	targets, err := c.targets(ctx, cfg)
	if err != nil {
		return err
	}
	var cur searchCursor
	_ = json.Unmarshal(rt.Cursor(), &cur)
	next := searchCursor{SinceIDs: map[string]string{}}

	var errs []error
	for _, t := range targets {
		p := cfg.params()
		if id := cur.SinceIDs[t.key]; id != "" {
			p.SinceID = id
			p.StartTime = time.Time{} // since_id bounds the window from here on
			next.SinceIDs[t.key] = id
		}
		n, newest, err := c.run(ctx, cfg, t, p, rt)
		if err != nil {
			rt.Logger().Warn("x_search: query failed", "query", t.key, "error", err)
			errs = append(errs, fmt.Errorf("%s: %w", t.key, err))
			continue
		}
		if newest != "" {
			next.SinceIDs[t.key] = newest
		}
		rt.Logger().Info("x_search: fetched", "query", t.key, "tweets", n)
	}
	if len(targets) > 0 && len(errs) == len(targets) {
		return errors.Join(errs...)
	}
	b, err := json.Marshal(next)
	if err != nil {
		return err
	}
	rt.SetCursor(b)
	return nil
}

// backfillParams are the params of an x_search backfill run.
type backfillParams struct {
	StartTime string `json:"start_time"`
	EndTime   string `json:"end_time"`
	Limit     int    `json:"limit"`
}

// Backfill implements source.Backfiller: run every target over a time window
// without touching the cursor.
func (c *Search) Backfill(ctx context.Context, src *model.Source, raw json.RawMessage, rt source.Runtime) error {
	cfg, err := parseSearchConfig(src.Config)
	if err != nil {
		return fmt.Errorf("x_search: config: %w", err)
	}
	var bp backfillParams
	if len(raw) > 0 {
		if err := json.Unmarshal(raw, &bp); err != nil {
			return fmt.Errorf("x_search: backfill params: %w", err)
		}
	}
	p := cfg.params()
	p.StartTime = time.Time{}
	for _, f := range []struct {
		v   string
		dst *time.Time
	}{{bp.StartTime, &p.StartTime}, {bp.EndTime, &p.EndTime}} {
		if f.v == "" {
			continue
		}
		if *f.dst, err = time.Parse(time.RFC3339, f.v); err != nil {
			return fmt.Errorf("x_search: backfill: %q is not an RFC 3339 time", f.v)
		}
	}
	if bp.Limit < 0 {
		return errors.New("x_search: backfill: limit can't be negative")
	}
	if bp.Limit > 0 {
		p.Limit = bp.Limit
	}

	targets, err := c.targets(ctx, cfg)
	if err != nil {
		return err
	}
	var errs []error
	for _, t := range targets {
		n, _, err := c.run(ctx, cfg, t, p, rt)
		if err != nil {
			errs = append(errs, fmt.Errorf("%s: %w", t.key, err))
			continue
		}
		rt.Logger().Info("x_search: backfilled", "query", t.key, "tweets", n)
	}
	return errors.Join(errs...)
}

// targets resolves what a config searches for: its query, its rule_ids, or
// every active non-local rule. Missing, inactive and local rules are skipped.
func (c *Search) targets(ctx context.Context, cfg *searchConfig) ([]target, error) {
	if cfg.Query != "" {
		return []target{{key: "query", query: cfg.Query}}, nil
	}
	var rules []*model.XRule
	if len(cfg.RuleIDs) > 0 {
		for _, id := range cfg.RuleIDs {
			r, err := c.store.GetXRule(ctx, id)
			if err != nil {
				continue
			}
			if r.Active && !r.Local {
				rules = append(rules, r)
			}
		}
	} else {
		active, local := true, false
		all, err := c.store.ListXRules(ctx, store.XRuleFilter{Active: &active, Local: &local})
		if err != nil {
			return nil, fmt.Errorf("x_search: list rules: %w", err)
		}
		rules = all
	}
	out := make([]target, 0, len(rules))
	for _, r := range rules {
		if r.CompiledValue != "" {
			out = append(out, target{key: "rule:" + strconv.FormatInt(r.ID, 10), query: r.CompiledValue, ruleID: r.ID})
		}
	}
	return out, nil
}

// endpoint returns the search endpoint for the account tier.
func (c *Search) endpoint(tier string) string {
	if tier == "pro" || tier == "enterprise" {
		return c.apiBase + searchAllPath
	}
	return c.apiBase + searchRecentPath
}

// buildQuery constructs the request query for one page.
func buildQuery(query string, params SearchParams, nextToken string) url.Values {
	maxResults := params.MaxResults
	if maxResults <= 0 {
		maxResults = defaultMaxResults
	}

	q := url.Values{}
	q.Set("query", query)
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
	if !params.StartTime.IsZero() {
		q.Set("start_time", params.StartTime.UTC().Format(time.RFC3339))
	}
	if !params.EndTime.IsZero() {
		q.Set("end_time", params.EndTime.UTC().Format(time.RFC3339))
	}
	if params.SinceID != "" {
		q.Set("since_id", params.SinceID)
	}
	if nextToken != "" {
		q.Set("pagination_token", nextToken)
	}
	return q
}

// fetchPage makes one GET to the search endpoint. On a 429 it sleeps until
// x-rate-limit-reset and retries once.
func fetchPage(ctx context.Context, hc *http.Client, token, endpoint string, params url.Values, logger *slog.Logger) ([]byte, error) {
	do := func() (*http.Response, error) {
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint+"?"+params.Encode(), nil)
		if err != nil {
			return nil, fmt.Errorf("build request: %w", err)
		}
		req.Header.Set("Authorization", "Bearer "+token)
		return hc.Do(req)
	}
	read := func(resp *http.Response) ([]byte, error) {
		defer resp.Body.Close()
		body, err := io.ReadAll(resp.Body)
		if err != nil {
			return nil, fmt.Errorf("read response body: %w", err)
		}
		if resp.StatusCode != http.StatusOK {
			return nil, fmt.Errorf("http %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
		}
		return body, nil
	}

	resp, err := do()
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusTooManyRequests {
		return read(resp)
	}
	resp.Body.Close()

	sleepUntil := time.Now().Add(60 * time.Second)
	if ts, err := strconv.ParseInt(resp.Header.Get("x-rate-limit-reset"), 10, 64); err == nil {
		sleepUntil = time.Unix(ts+1, 0)
	}
	wait := time.Until(sleepUntil)
	logger.Info("x_search: rate limited; sleeping until reset", "sleep_seconds", wait.Seconds())
	select {
	case <-ctx.Done():
		return nil, fmt.Errorf("rate-limit sleep: %w", ctx.Err())
	case <-time.After(wait):
	}
	resp, err = do()
	if err != nil {
		return nil, err
	}
	return read(resp)
}

// run fetches every page of one target and emits its tweets. It returns the
// number of tweets fetched and the newest tweet ID (meta.newest_id of the
// first page), the next since_id for this target.
func (c *Search) run(ctx context.Context, cfg *searchConfig, t target, params SearchParams, rt source.Runtime) (int, string, error) {
	endpoint := c.endpoint(cfg.Tier)
	var ruleIDs []int64
	if t.ruleID != 0 {
		ruleIDs = []int64{t.ruleID}
	}
	fetched := 0
	nextToken := ""
	newestID := ""

	for {
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
				// X rejects max_results below 10.
				pageParams.MaxResults = max(remaining, 10)
			}
		}

		body, err := fetchPage(ctx, rt.HTTPClient(), cfg.BearerToken, endpoint, buildQuery(t.query, pageParams, nextToken), rt.Logger())
		if err != nil {
			return fetched, newestID, err
		}
		page, err := NormalizeSearchResponse(body)
		if err != nil {
			return fetched, newestID, err
		}
		// The first page holds the newest tweets; later pages are older.
		if newestID == "" && page.NewestID != "" {
			newestID = page.NewestID
		}

		for _, sig := range page.Signals {
			if params.Limit > 0 && fetched >= params.Limit {
				break
			}
			if _, err := rt.Emit(ctx, sig, source.WithXRuleMatches(ruleIDs)); err != nil {
				return fetched, newestID, err
			}
			fetched++
		}

		if page.NextToken == "" || (params.Limit > 0 && fetched >= params.Limit) {
			break
		}
		nextToken = page.NextToken
	}
	return fetched, newestID, nil
}
