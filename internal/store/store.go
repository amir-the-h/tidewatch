package store

import (
	"context"
	"time"

	"github.com/amir-the-h/iran-watcher/internal/model"
)

// TweetFilter defines optional filters for listing tweets.
type TweetFilter struct {
	RuleID  *int64
	From    *time.Time
	To      *time.Time
	Page    int
	PerPage int
}

// SourceFilter defines optional filters for listing sources.
type SourceFilter struct {
	Type   *model.SourceType
	Class  *model.SourceClass
	Active *bool
}

// RuleFilter defines optional filters for listing rules.
type RuleFilter struct {
	Active *bool
}

// CategorizationFilter defines optional filters for listing categorizations.
type CategorizationFilter struct {
	TweetID   *string
	ModelName *string
	From      *time.Time
	To        *time.Time
}

// Store is the repository interface for all persistent data.
type Store interface {
	// Tweets
	SaveTweet(ctx context.Context, tweet *model.Tweet) error
	GetTweet(ctx context.Context, id string) (*model.Tweet, error)
	ListTweets(ctx context.Context, filter TweetFilter) ([]*model.Tweet, error)

	// Sources
	SaveSource(ctx context.Context, source *model.Source) error
	GetSource(ctx context.Context, id int64) (*model.Source, error)
	ListSources(ctx context.Context, filter SourceFilter) ([]*model.Source, error)
	DeleteSource(ctx context.Context, id int64) error
	SetSourceActive(ctx context.Context, id int64, active bool) error
	ActiveRuleRefCount(ctx context.Context, sourceID int64) (int, error)
	RuleRefCount(ctx context.Context, sourceID int64) (int, error)

	// Rules
	SaveRule(ctx context.Context, rule *model.Rule) error
	GetRule(ctx context.Context, id int64) (*model.Rule, error)
	GetRuleByXRuleID(ctx context.Context, xRuleID string) (*model.Rule, error)
	ListRules(ctx context.Context, filter RuleFilter) ([]*model.Rule, error)
	UpdateRule(ctx context.Context, rule *model.Rule) error
	DeleteRule(ctx context.Context, id int64) error
	SetRuleActive(ctx context.Context, id int64, active bool, xRuleID string) error
	TweetRuleCount(ctx context.Context, ruleID int64) (int, error)

	// TweetRules
	SaveTweetRule(ctx context.Context, tr *model.TweetRule) error

	// Models
	SaveModel(ctx context.Context, m *model.Model) error
	GetModel(ctx context.Context, id int64) (*model.Model, error)
	ListModels(ctx context.Context) ([]*model.Model, error)
	DeleteModel(ctx context.Context, id int64) error
	SetModelActive(ctx context.Context, id int64, active bool) error

	// Categorizations
	SaveCategorization(ctx context.Context, c *model.Categorization) error
	ListCategorizations(ctx context.Context, filter CategorizationFilter) ([]*model.Categorization, error)
	ListUncategorizedTweets(ctx context.Context, modelNames []string) ([]*model.Tweet, error)

	Close() error
}
