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

// PipelineFilter defines optional filters for listing pipelines.
type PipelineFilter struct {
	Type    *model.PipelineType
	Active  *bool
	Usecase *string // filters by config->$.usecase (ollama pipelines)
}

// ActionFilter defines optional filters for listing actions.
type ActionFilter struct {
	PipelineID *int64
	Active     *bool
}

// MapPin is a geo-event pin derived from action_execution_metadata.
type MapPin struct {
	TweetID          string `json:"tweet_id"`
	TweetCreatedAt   string `json:"tweet_created_at"`
	ModelName        string `json:"model_name"`
	Description      string `json:"description,omitempty"`
	CoordinatesJSON  string `json:"-"` // raw JSON [[lat,lon],...], parsed by handler
}

// TweetActionSummary is the aggregated result of all successful executions of one
// action on a single tweet. Metadata holds the latest value per key (parsed from
// JSON) across all executions. Empty Metadata means the action ran but produced no
// persistent metadata (e.g. a twitter_retweet action).
type TweetActionSummary struct {
	ActionID   int64
	ActionName string
	Metadata   map[string]any
}

// ExecutionFilter defines optional filters for listing action executions.
type ExecutionFilter struct {
	ActionID *int64
	TweetID  *string
	Status   *string
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

	// Pipelines
	SavePipeline(ctx context.Context, p *model.Pipeline) error
	GetPipeline(ctx context.Context, id int64) (*model.Pipeline, error)
	ListPipelines(ctx context.Context, filter PipelineFilter) ([]*model.Pipeline, error)
	UpdatePipeline(ctx context.Context, p *model.Pipeline) error
	DeletePipeline(ctx context.Context, id int64) error
	SetPipelineActive(ctx context.Context, id int64, active bool) error
	ListPipelineLabels(ctx context.Context, pipelineID int64) ([]string, error)
	AddPipelineLabel(ctx context.Context, pipelineID int64, label string) error
	RemovePipelineLabel(ctx context.Context, pipelineID int64, label string) error

	// TweetRuleIDs (kept — used by stream listener rule matching)
	ListTweetRuleIDs(ctx context.Context, tweetID string) ([]int64, error)

	// NotificationProviders
	SaveNotificationProvider(ctx context.Context, p *model.NotificationProvider) error
	GetNotificationProvider(ctx context.Context, id int64) (*model.NotificationProvider, error)
	ListNotificationProviders(ctx context.Context) ([]*model.NotificationProvider, error)
	UpdateNotificationProvider(ctx context.Context, p *model.NotificationProvider) error
	DeleteNotificationProvider(ctx context.Context, id int64) error
	SetNotificationProviderActive(ctx context.Context, id int64, active bool) error

	// NotificationChannels
	SaveNotificationChannel(ctx context.Context, c *model.NotificationChannel) error
	GetNotificationChannel(ctx context.Context, id int64) (*model.NotificationChannel, error)
	ListNotificationChannels(ctx context.Context, providerID *int64) ([]*model.NotificationChannel, error)
	UpdateNotificationChannel(ctx context.Context, c *model.NotificationChannel) error
	DeleteNotificationChannel(ctx context.Context, id int64) error
	SetNotificationChannelActive(ctx context.Context, id int64, active bool) error

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
	ListNERSuggestionsForEntity(ctx context.Context, entityID int64) ([]string, error)
	SearchNERSuggestions(ctx context.Context, name string, entityType string) ([]string, error)

	// Summaries
	ListSummaries(ctx context.Context, filter SummaryFilter) ([]*model.SummaryItem, error)
	CountSummaries(ctx context.Context, filter SummaryFilter) (int, error)

	// Map
	ListMapPins(ctx context.Context) ([]*MapPin, error)

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

	// Users
	SaveUser(ctx context.Context, u *model.User) error
	GetUser(ctx context.Context, id int64) (*model.User, error)
	GetUserByUsername(ctx context.Context, username string) (*model.User, error)
	ListUsers(ctx context.Context) ([]*model.User, error)
	UpdateUser(ctx context.Context, u *model.User) error
	DeleteUser(ctx context.Context, id int64) error
	CountAdmins(ctx context.Context) (int, error)

	// Actions
	SaveAction(ctx context.Context, a *model.Action) error
	GetAction(ctx context.Context, id int64) (*model.Action, error)
	ListActions(ctx context.Context, filter ActionFilter) ([]*model.Action, error)
	UpdateAction(ctx context.Context, a *model.Action) error
	DeleteAction(ctx context.Context, id int64) error
	SetActionActive(ctx context.Context, id int64, active bool) error

	// Chains
	SaveChain(ctx context.Context, c *model.Chain) error
	GetChain(ctx context.Context, id int64) (*model.Chain, error)
	ListChains(ctx context.Context) ([]*model.Chain, error)
	UpdateChain(ctx context.Context, c *model.Chain) error
	DeleteChain(ctx context.Context, id int64) error
	SetChainActive(ctx context.Context, id int64, active bool) error
	GetChainGraph(ctx context.Context, chainID int64) (*model.ChainGraph, error)
	SaveChainGraph(ctx context.Context, chainID int64, nodes []*model.ChainNodeRequest, edges []*model.ChainEdgeRequest) (*model.ChainGraph, error)

	// TriggerChainLinks
	SaveTriggerChainLink(ctx context.Context, l *model.TriggerChainLink) error
	ListTriggerChainLinks(ctx context.Context) ([]*model.TriggerChainLink, error)
	DeleteTriggerChainLink(ctx context.Context, id int64) error
	SetTriggerChainLinkActive(ctx context.Context, id int64, active bool) error

	// ActionExecutions
	SaveActionExecution(ctx context.Context, e *model.ActionExecution) error
	ListActionExecutions(ctx context.Context, filter ExecutionFilter) ([]*model.ActionExecution, error)
	ListActionExecutionMetadata(ctx context.Context, execID int64) (map[string]string, error)
	MergeActionExecutionMetadata(ctx context.Context, actionID int64, tweetID string) (map[string]string, error)
	// MergeAllTweetExecutionMetadata returns the latest value per key across all
	// successful executions for tweetID regardless of action. This gives notification
	// pipelines access to upstream ONNX/Ollama outputs even when not triggered via chain.
	MergeAllTweetExecutionMetadata(ctx context.Context, tweetID string) (map[string]string, error)
	// ListTweetActionSummaries returns one TweetActionSummary per action that had at
	// least one successful execution on tweetID. Each summary carries the latest value
	// per metadata key (same merge logic as MergeActionExecutionMetadata). Actions with
	// no metadata rows are included with an empty Metadata map.
	ListTweetActionSummaries(ctx context.Context, tweetID string) ([]TweetActionSummary, error)
	SaveActionExecutionMetadata(ctx context.Context, execID int64, key, value string) error
	// DeleteActionExecutions deletes all execution records and their metadata for
	// the given tweets. If actionIDs is non-empty, only executions for those actions
	// are deleted. Returns the count of deleted execution rows.
	DeleteActionExecutions(ctx context.Context, tweetIDs []string, actionIDs []int64) (int64, error)

	// TwitterCredentials
	SaveTwitterCredentials(ctx context.Context, c *model.TwitterCredentials) error
	GetTwitterCredentials(ctx context.Context) (*model.TwitterCredentials, error)
	DeleteTwitterCredentials(ctx context.Context) error

	// OllamaBackends
	ListOllamaBackends(ctx context.Context) ([]model.OllamaBackend, error)
	GetOllamaBackend(ctx context.Context, id int64) (model.OllamaBackend, error)
	CreateOllamaBackend(ctx context.Context, name, url string) (model.OllamaBackend, error)
	UpdateOllamaBackend(ctx context.Context, id int64, name, url string) error
	DeleteOllamaBackend(ctx context.Context, id int64) error
	SetOllamaBackendEnabled(ctx context.Context, id int64, enabled bool) error

	// PipelineBackends
	ListPipelineBackends(ctx context.Context, pipelineID int64) ([]model.PipelineBackend, error)
	SetPipelineBackends(ctx context.Context, pipelineID int64, assignments []model.PipelineBackend) error
	RemovePipelineBackend(ctx context.Context, pipelineID, backendID int64) error
	// ListAllPipelineBackendIDs returns pipelineID → []backendID in priority order (for ActionCache.Load).
	ListAllPipelineBackendIDs(ctx context.Context) (map[int64][]int64, error)

	Close() error
}
