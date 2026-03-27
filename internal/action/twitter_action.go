package action

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/amir-the-h/iran-watcher/internal/inference"
	"github.com/amir-the-h/iran-watcher/internal/model"
	"github.com/amir-the-h/iran-watcher/internal/store"
	"github.com/amir-the-h/iran-watcher/internal/twitter"
)

// TwitterConfig is the type-specific config stored in actions.config for
// retweet/like/quote action types.
type TwitterConfig struct {
	// Template is the quote text template (only for quote type).
	// Supports {text} and {author} placeholders.
	Template string `json:"template,omitempty"`
	// PipelineModelID is optional: if set, the pipeline output is used as
	// quote text instead of the static template (only for quote type).
	PipelineModelID *int64 `json:"pipeline_model_id,omitempty"`
}

// twitterClient is the subset of twitter.Client used by TwitterAction.
type twitterClient interface {
	Retweet(ctx context.Context, tweetID string) (string, error)
	Like(ctx context.Context, tweetID string) error
	QuoteTweet(ctx context.Context, tweetID, text string) (string, error)
}

// TwitterAction executes a Twitter interaction (retweet, like, or quote tweet).
type TwitterAction struct {
	id      int64
	name    string
	typ     model.ActionType
	ruleIDs []int64
	cfg     TwitterConfig
	client  twitterClient
	reg     *inference.Registry // for optional pipeline-based quote text
	st      store.Store
	logger  *slog.Logger
}

// NewTwitterAction constructs a TwitterAction.
func NewTwitterAction(
	id int64,
	name string,
	typ model.ActionType,
	ruleIDs []int64,
	cfg TwitterConfig,
	client *twitter.Client,
	reg *inference.Registry,
	st store.Store,
	logger *slog.Logger,
) *TwitterAction {
	return &TwitterAction{
		id:      id,
		name:    name,
		typ:     typ,
		ruleIDs: ruleIDs,
		cfg:     cfg,
		client:  client,
		reg:     reg,
		st:      st,
		logger:  logger,
	}
}

func (a *TwitterAction) ID() int64              { return a.id }
func (a *TwitterAction) Name() string           { return a.name }
func (a *TwitterAction) Type() model.ActionType { return a.typ }
func (a *TwitterAction) RuleIDs() []int64       { return a.ruleIDs }

// Execute performs the configured Twitter action for the given tweet.
// HTTP 429 rate limit errors are logged and not propagated — the action
// moves on without retrying so the worker is not stalled.
func (a *TwitterAction) Execute(ctx context.Context, tweet model.Tweet) error {
	var execErr error

	switch a.typ {
	case model.ActionTypeRetweet:
		_, execErr = a.client.Retweet(ctx, tweet.ID)

	case model.ActionTypeLike:
		execErr = a.client.Like(ctx, tweet.ID)

	case model.ActionTypeQuote:
		text, err := a.quoteText(ctx, tweet)
		if err != nil {
			if a.logger != nil {
				a.logger.Warn("action: twitter quote: build text",
					"action_id", a.id, "tweet_id", tweet.ID, "error", err)
			}
			return err
		}
		_, execErr = a.client.QuoteTweet(ctx, tweet.ID, text)

	default:
		return fmt.Errorf("action: unknown twitter action type: %s", a.typ)
	}

	if execErr != nil {
		var rateLimitErr *twitter.RateLimitError
		if errors.As(execErr, &rateLimitErr) {
			if a.logger != nil {
				a.logger.Warn("action: twitter rate limited",
					"action_id", a.id,
					"type", a.typ,
					"tweet_id", tweet.ID,
					"rate_limit_reset", rateLimitErr.ResetAt.Format(time.RFC3339))
			}
			// Rate limit: log only, do not propagate (per spec).
			return nil
		}
		if a.logger != nil {
			a.logger.Warn("action: twitter execute failed",
				"action_id", a.id,
				"type", a.typ,
				"tweet_id", tweet.ID,
				"error", execErr)
		}
		return execErr
	}

	return nil
}

// quoteText returns the text to use for a quote tweet.
// If pipeline_model_id is set and the pipeline is active, runs the pipeline.
// Otherwise renders the static template.
func (a *TwitterAction) quoteText(ctx context.Context, tweet model.Tweet) (string, error) {
	if a.cfg.PipelineModelID != nil && a.reg != nil {
		mdl, err := a.st.GetModel(ctx, *a.cfg.PipelineModelID)
		if err == nil {
			p := a.reg.Get(mdl.Name)
			if p != nil {
				result, runErr := p.Run([]byte(tweet.Text))
				if runErr == nil {
					return string(result), nil
				}
				if a.logger != nil {
					a.logger.Warn("action: quote pipeline failed, falling back to template",
						"action_id", a.id, "model", mdl.Name, "error", runErr)
				}
			}
		}
	}

	// Static template with {text} and {author} placeholders.
	text := a.cfg.Template
	text = strings.ReplaceAll(text, "{text}", tweet.Text)
	text = strings.ReplaceAll(text, "{author}", tweet.AuthorHandle)
	return text, nil
}
