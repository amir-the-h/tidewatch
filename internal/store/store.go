package store

import (
	"context"
	"time"

	"github.com/amir-the-h/iran-watcher/internal/model"
)

// TweetFilter defines optional filters for listing tweets.
type TweetFilter struct {
	RuleID   *int64
	SourceID *int64
	EntityID *int64
	From     *time.Time
	To       *time.Time
	Page     int
	PerPage  int
}

// SummaryFilter defines optional filters for listing tweet+summary pairs.
type SummaryFilter struct {
	RuleID   *int64
	SourceID *int64
	EntityID *int64
	From     *time.Time
	To       *time.Time
	Page     int
	PerPage  int
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
	Local  *bool
}

// CategorizationFilter defines optional filters for listing categorizations.
type CategorizationFilter struct {
	TweetID   *string
	ModelName *string
	From      *time.Time
	To        *time.Time
}

// NotificationFilter defines optional filters for listing notifications.
type NotificationFilter struct {
	EventType *string
	Active    *bool
	RuleID    *int64
}

// TrackedEntityFilter defines optional filters for listing tracked entities.
type TrackedEntityFilter struct {
	Type   *string
	Active *bool
}

// EntityTrackFilter defines optional filters for listing entity tracks.
type EntityTrackFilter struct {
	EntityID *int64
	TweetID  *string
	From     *time.Time
	To       *time.Time
	Page     int
	PerPage  int
}

// Store is the repository interface for all persistent data.
type Store interface {
	// Tweets
	SaveTweet(ctx context.Context, tweet *model.Tweet) error
	GetTweet(ctx context.Context, id string) (*model.Tweet, error)
	ListTweets(ctx context.Context, filter TweetFilter) ([]*model.Tweet, error)
	CountTweets(ctx context.Context, filter TweetFilter) (int, error)
	DeleteTweets(ctx context.Context, ids []string) (int64, error)
	// DeleteNoiseTweets deletes tweets that either match no rules at all, or
	// whose every matching rule is marked local=true. Returns the count deleted.
	DeleteNoiseTweets(ctx context.Context) (int64, error)

	// Sources
	SaveSource(ctx context.Context, source *model.Source) error
	UpdateSource(ctx context.Context, source *model.Source) error
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
	ListRulesDependingOn(ctx context.Context, ruleID int64) ([]*model.Rule, error)
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
	ListModelLabels(ctx context.Context, modelName string) ([]string, error)
	AddModelLabel(ctx context.Context, modelName, label string) error
	RemoveModelLabel(ctx context.Context, modelName, label string) error

	// Model rules (many-to-many: which rules trigger which models)
	AddModelRule(ctx context.Context, modelID, ruleID int64) error
	RemoveModelRule(ctx context.Context, modelID, ruleID int64) error
	ListTweetRuleIDs(ctx context.Context, tweetID string) ([]int64, error)

	// Categorizations
	SaveCategorization(ctx context.Context, c *model.Categorization) error
	ListCategorizations(ctx context.Context, filter CategorizationFilter) ([]*model.Categorization, error)
	ListUncategorizedTweets(ctx context.Context, modelNames []string) ([]*model.Tweet, error)

	// Notifications
	SaveNotification(ctx context.Context, n *model.Notification) error
	GetNotification(ctx context.Context, id int64) (*model.Notification, error)
	ListNotifications(ctx context.Context, filter NotificationFilter) ([]*model.Notification, error)
	UpdateNotification(ctx context.Context, n *model.Notification) error
	DeleteNotification(ctx context.Context, id int64) error
	SetNotificationActive(ctx context.Context, id int64, active bool) error

	// TrackedEntities
	SaveTrackedEntity(ctx context.Context, e *model.TrackedEntity) error
	GetTrackedEntity(ctx context.Context, id int64) (*model.TrackedEntity, error)
	ListTrackedEntities(ctx context.Context, filter TrackedEntityFilter) ([]*model.TrackedEntity, error)
	UpdateTrackedEntity(ctx context.Context, e *model.TrackedEntity) error
	DeleteTrackedEntity(ctx context.Context, id int64) error
	SetTrackedEntityActive(ctx context.Context, id int64, active bool) error

	// EntityTracks
	ListActiveTrackedEntities(ctx context.Context) ([]*model.TrackedEntity, error)
	SaveEntityTrack(ctx context.Context, t *model.EntityTrack) error
	ListEntityTracks(ctx context.Context, filter EntityTrackFilter) ([]*model.EntityTrack, error)
	CountEntityTracks(ctx context.Context, filter EntityTrackFilter) (int, error)

	// Summaries
	ListSummaries(ctx context.Context, filter SummaryFilter) ([]*model.SummaryItem, error)
	CountSummaries(ctx context.Context, filter SummaryFilter) (int, error)

	// DigestJobs
	SaveDigestJob(ctx context.Context, job *model.DigestJob) error
	GetDigestJob(ctx context.Context, id string) (*model.DigestJob, error)
	UpdateDigestJob(ctx context.Context, job *model.DigestJob) error
	ListDigestJobs(ctx context.Context, limit int) ([]*model.DigestJob, error)
	DeleteDigestJob(ctx context.Context, id string) error

	// SearchJobs
	SaveSearchJob(ctx context.Context, job *model.SearchJob) error
	GetSearchJob(ctx context.Context, id string) (*model.SearchJob, error)
	UpdateSearchJob(ctx context.Context, job *model.SearchJob) error
	ListSearchJobs(ctx context.Context, limit int) ([]*model.SearchJob, error)
	DeleteSearchJob(ctx context.Context, id string) error

	Close() error
}
