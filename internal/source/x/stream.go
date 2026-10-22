package x

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/amir-the-h/tidewatch/internal/model"
	"github.com/amir-the-h/tidewatch/internal/source"
	"github.com/amir-the-h/tidewatch/internal/store"
)

const (
	defaultAPIBase = "https://api.twitter.com"
	streamPath     = "/2/tweets/search/stream?tweet.fields=created_at,author_id,referenced_tweets,note_tweet&expansions=author_id,referenced_tweets.id&user.fields=username"
	// X sends a keep-alive newline every 20 s; a connection silent for this
	// long is stalled and is dropped so the manager reconnects.
	defaultIdleTimeout = 90 * time.Second
	maxEventBytes      = 1 << 20
)

var errStreamIdle = errors.New("x_stream: no data or keep-alive for too long")

// credentials is the part of the config both X connectors share.
type credentials struct {
	BearerToken string `json:"bearer_token"`
	Tier        string `json:"tier"`
}

func credentialFields() []source.Field {
	return []source.Field{
		{Key: "bearer_token", Label: "Bearer token", Type: source.FieldString, Required: true, Secret: true,
			Help: "App-only bearer token of the X developer app"},
		{Key: "tier", Label: "X API tier", Type: source.FieldSelect, Options: tiers, Default: "basic",
			Help: "The X developer plan, not the source's reliability tier"},
	}
}

func validateTier(tier string) error {
	if tier != "" && !slices.Contains(tiers, tier) {
		return fmt.Errorf("tier must be one of %v", tiers)
	}
	return nil
}

// Stream is the x_stream connector: the X Filtered Stream. X allows one
// stream connection per app, so at most one x_stream source can be active.
//
// Rules (x_rules) are global: Run reconciles them with X before connecting,
// and the x_rules API changes them through WithRules while the source runs.
type Stream struct {
	store       store.Store
	apiBase     string
	idleTimeout time.Duration
	httpClient  *http.Client

	// mu serialises rule changes on X (Run's reconcile and WithRules), so a
	// rule added by the API is never deleted as unclaimed by a reconcile.
	mu sync.Mutex
}

// NewStream returns the x_stream connector.
func NewStream(s store.Store) *Stream {
	return &Stream{
		store:       s,
		apiBase:     defaultAPIBase,
		idleTimeout: defaultIdleTimeout,
		httpClient: &http.Client{
			Transport: &http.Transport{
				Proxy:                 http.ProxyFromEnvironment,
				ResponseHeaderTimeout: 60 * time.Second,
			},
		},
	}
}

// Descriptor implements source.Connector.
func (c *Stream) Descriptor() source.Descriptor {
	return source.Descriptor{
		Type:         TypeStream,
		Label:        "X Filtered Stream",
		Mode:         source.ModeStream,
		ExtNS:        ExtNS,
		Kinds:        []model.SignalKind{model.SignalKindPost},
		Fields:       credentialFields(),
		MaxInstances: 1,
	}
}

// Validate implements source.Connector.
func (c *Stream) Validate(cfg json.RawMessage) error {
	var cr credentials
	if err := json.Unmarshal(cfg, &cr); err != nil {
		return err
	}
	return validateTier(cr.Tier)
}

// Run implements source.StreamConnector: reconcile the rules, then read the
// stream until it fails or ctx is cancelled.
func (c *Stream) Run(ctx context.Context, src *model.Source, rt source.Runtime) error {
	var cr credentials
	if err := json.Unmarshal(src.Config, &cr); err != nil {
		return fmt.Errorf("x_stream: config: %w", err)
	}
	c.mu.Lock()
	err := c.reconcile(ctx, c.ruleAPI(cr.BearerToken), rt.Logger())
	c.mu.Unlock()
	if err != nil {
		return fmt.Errorf("x_stream: sync rules: %w", err)
	}
	return c.read(ctx, cr.BearerToken, rt)
}

func (c *Stream) ruleAPI(token string) *RuleManager {
	return &RuleManager{bearerToken: token, httpClient: source.NewHTTPClient(), apiBase: c.apiBase}
}

// WithRules runs fn with the rules API of the active x_stream source and its
// tier, holding the rule-sync lock. With no active x_stream source, api is nil
// and tier is "basic": rule changes are then only stored, and the next stream
// start syncs them.
func (c *Stream) WithRules(ctx context.Context, fn func(api RuleAPI, tier string) error) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	active := true
	srcs, err := c.store.ListSources(ctx, store.SourceFilter{Type: TypeStream, Active: &active})
	if err != nil {
		return fmt.Errorf("x_stream: find active source: %w", err)
	}
	if len(srcs) == 0 {
		return fn(nil, "basic")
	}
	var cr credentials
	if err := json.Unmarshal(srcs[0].Config, &cr); err != nil {
		return fmt.Errorf("x_stream: config: %w", err)
	}
	if cr.Tier == "" {
		cr.Tier = "basic"
	}
	return fn(c.ruleAPI(cr.BearerToken), cr.Tier)
}

// reconcile makes X's stream rules match the active non-local x_rules: a rule
// whose x_rule_id is on X with the same value is kept, one whose value is on X
// under another ID adopts that ID, the rest are added; X rules no active rule
// claims are deleted. A rule X rejects is logged and left without an
// x_rule_id. Callers hold c.mu.
func (c *Stream) reconcile(ctx context.Context, api RuleAPI, logger *slog.Logger) error {
	remote, err := api.ListRules(ctx)
	if err != nil {
		return err
	}
	active, local := true, false
	rules, err := c.store.ListXRules(ctx, store.XRuleFilter{Active: &active, Local: &local})
	if err != nil {
		return fmt.Errorf("list rules: %w", err)
	}

	byID := make(map[string]XRule, len(remote))
	for _, r := range remote {
		byID[r.ID] = r
	}
	claimed := map[string]bool{}
	adopt := func(value string) string {
		for _, r := range remote {
			if r.Value == value && !claimed[r.ID] {
				return r.ID
			}
		}
		return ""
	}

	added, kept := 0, 0
	for _, r := range rules {
		if r.CompiledValue == "" {
			continue
		}
		if rr, ok := byID[r.XRuleID]; ok && rr.Value == r.CompiledValue && !claimed[r.XRuleID] {
			claimed[r.XRuleID] = true
			kept++
			continue
		}
		id := adopt(r.CompiledValue)
		if id == "" {
			id, err = api.AddRule(ctx, r.Name, r.CompiledValue)
			if err != nil {
				logger.Warn("x_stream: X rejected rule", "rule_id", r.ID, "rule", r.Name, "error", err)
				if err := c.store.SetXRuleActive(ctx, r.ID, true, ""); err != nil {
					return fmt.Errorf("clear x rule id %d: %w", r.ID, err)
				}
				continue
			}
			added++
		} else {
			kept++
		}
		claimed[id] = true
		if err := c.store.SetXRuleActive(ctx, r.ID, true, id); err != nil {
			return fmt.Errorf("store x rule id %d: %w", r.ID, err)
		}
	}

	deleted := 0
	for _, r := range remote {
		if claimed[r.ID] {
			continue
		}
		if err := api.DeleteRule(ctx, r.ID); err != nil {
			return fmt.Errorf("delete unclaimed rule %s: %w", r.ID, err)
		}
		deleted++
	}
	logger.Info("x_stream: rules synced", "kept", kept, "added", added, "deleted", deleted)
	return nil
}

// read opens one stream connection and ingests events until the connection
// ends (an error) or ctx is cancelled (nil).
func (c *Stream) read(parent context.Context, token string, rt source.Runtime) error {
	ctx, cancel := context.WithCancelCause(parent)
	defer cancel(nil)

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.apiBase+streamPath, nil)
	if err != nil {
		return fmt.Errorf("x_stream: build request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("User-Agent", source.UserAgent)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		if parent.Err() != nil {
			return nil
		}
		return fmt.Errorf("x_stream: http: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return fmt.Errorf("x_stream: http %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}
	rt.Logger().Info("x_stream: connected")

	idle := time.AfterFunc(c.idleTimeout, func() { cancel(errStreamIdle) })
	defer idle.Stop()

	scanner := bufio.NewScanner(resp.Body)
	scanner.Buffer(make([]byte, 64*1024), maxEventBytes)
	for scanner.Scan() {
		idle.Reset(c.idleTimeout)
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, ":") {
			continue // keep-alive or SSE comment
		}
		line = strings.TrimPrefix(line, "data: ")
		if err := c.processEvent(ctx, []byte(line), rt); err != nil {
			rt.Logger().Warn("x_stream: failed to process event", "error", err)
		}
	}

	switch {
	case parent.Err() != nil:
		return nil
	case errors.Is(context.Cause(ctx), errStreamIdle):
		return errStreamIdle
	case scanner.Err() != nil:
		return fmt.Errorf("x_stream: read: %w", scanner.Err())
	default:
		return errors.New("x_stream: connection closed by X")
	}
}

// processEvent normalizes one stream event, resolves its matching rules to
// local rule IDs and emits the signal.
func (c *Stream) processEvent(ctx context.Context, data []byte, rt source.Runtime) error {
	evt, err := Normalize(data)
	if err != nil {
		return err
	}

	ruleIDs := []int64{}
	for _, mr := range evt.MatchingRules {
		rule, err := c.store.GetXRuleByXRuleID(ctx, mr.ID)
		if err != nil {
			rt.Logger().Warn("x_stream: rule lookup failed", "x_rule_id", mr.ID, "error", err)
			continue
		}
		ruleIDs = append(ruleIDs, rule.ID)
	}

	isNew, err := rt.Emit(ctx, evt.Signal, source.WithXRuleMatches(ruleIDs))
	if err != nil {
		return err
	}
	rt.Logger().Info("x_stream: processed tweet",
		"signal_id", evt.Signal.ID,
		"x_tweet_id", evt.Signal.ExternalID,
		"author", evt.Signal.Author,
		"new", isNew,
	)
	return nil
}
