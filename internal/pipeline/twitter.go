package pipeline

import (
	"context"
	"errors"
	"fmt"

	"github.com/amir-the-h/tidewatch/internal/inference"
	"github.com/amir-the-h/tidewatch/internal/twitter"
)

// RetweetPipeline retweets the tweet identified by tweet_id in the input context.
// Outputs: {"retweet_id": "<new tweet id>"} on success.
type RetweetPipeline struct {
	name   string
	client *twitter.Client
}

// NewRetweetFactory returns a Factory that creates a RetweetPipeline.
func NewRetweetFactory(client *twitter.Client) Factory {
	return func(name string, _ map[string]any) (inference.Pipeline, error) {
		return &RetweetPipeline{name: name, client: client}, nil
	}
}

func (p *RetweetPipeline) Name() string                 { return p.name }
func (p *RetweetPipeline) Type() inference.PipelineType { return inference.PipelineTypeInternal }
func (p *RetweetPipeline) Close() error                 { return nil }

func (p *RetweetPipeline) Run(ctx context.Context, input inference.PipelineInput) (map[string]any, error) {
	tweetID, _ := input.Context["tweet_id"].(string)
	if tweetID == "" {
		return nil, fmt.Errorf("retweet: tweet_id missing from context")
	}

	retweetID, err := p.client.Retweet(ctx, tweetID)
	if err != nil {
		var rle *twitter.RateLimitError
		if errors.As(err, &rle) {
			return nil, fmt.Errorf("retweet: rate limited, resets at %s: %w", rle.ResetAt, err)
		}
		return nil, fmt.Errorf("retweet: %w", err)
	}

	return map[string]any{
		"retweet_id": retweetID,
	}, nil
}

// LikePipeline likes the tweet identified by tweet_id in the input context.
// Outputs: {"liked": true} on success.
type LikePipeline struct {
	name   string
	client *twitter.Client
}

// NewLikeFactory returns a Factory that creates a LikePipeline.
func NewLikeFactory(client *twitter.Client) Factory {
	return func(name string, _ map[string]any) (inference.Pipeline, error) {
		return &LikePipeline{name: name, client: client}, nil
	}
}

func (p *LikePipeline) Name() string                 { return p.name }
func (p *LikePipeline) Type() inference.PipelineType { return inference.PipelineTypeInternal }
func (p *LikePipeline) Close() error                 { return nil }

func (p *LikePipeline) Run(ctx context.Context, input inference.PipelineInput) (map[string]any, error) {
	tweetID, _ := input.Context["tweet_id"].(string)
	if tweetID == "" {
		return nil, fmt.Errorf("like: tweet_id missing from context")
	}

	if err := p.client.Like(ctx, tweetID); err != nil {
		var rle *twitter.RateLimitError
		if errors.As(err, &rle) {
			return nil, fmt.Errorf("like: rate limited, resets at %s: %w", rle.ResetAt, err)
		}
		return nil, fmt.Errorf("like: %w", err)
	}

	return map[string]any{
		"liked": true,
	}, nil
}

// QuotePipeline quotes the tweet identified by tweet_id in the input context.
// The quote text is taken from input.Text.
// Outputs: {"quote_id": "<new tweet id>"} on success.
type QuotePipeline struct {
	name   string
	client *twitter.Client
}

// NewQuoteFactory returns a Factory that creates a QuotePipeline.
func NewQuoteFactory(client *twitter.Client) Factory {
	return func(name string, _ map[string]any) (inference.Pipeline, error) {
		return &QuotePipeline{name: name, client: client}, nil
	}
}

func (p *QuotePipeline) Name() string                 { return p.name }
func (p *QuotePipeline) Type() inference.PipelineType { return inference.PipelineTypeInternal }
func (p *QuotePipeline) Close() error                 { return nil }

func (p *QuotePipeline) Run(ctx context.Context, input inference.PipelineInput) (map[string]any, error) {
	tweetID, _ := input.Context["tweet_id"].(string)
	if tweetID == "" {
		return nil, fmt.Errorf("quote: tweet_id missing from context")
	}

	// Use the pipeline's input text as the quote body; fall back to tweet_author for attribution.
	text := input.Text
	if text == "" {
		author, _ := input.Context["tweet_author"].(string)
		if author != "" {
			text = fmt.Sprintf("via @%s", author)
		}
	}
	if text == "" {
		return nil, fmt.Errorf("quote: no text provided for quote tweet")
	}

	quoteID, err := p.client.QuoteTweet(ctx, tweetID, text)
	if err != nil {
		var rle *twitter.RateLimitError
		if errors.As(err, &rle) {
			return nil, fmt.Errorf("quote: rate limited, resets at %s: %w", rle.ResetAt, err)
		}
		return nil, fmt.Errorf("quote: %w", err)
	}

	return map[string]any{
		"quote_id": quoteID,
	}, nil
}
