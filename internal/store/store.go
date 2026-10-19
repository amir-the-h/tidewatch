package store

import (
	"context"
	"time"

	"github.com/amir-the-h/tidewatch/internal/model"
)

// TweetFilter defines optional filters for listing tweets.
type TweetFilter struct {
	RuleID         *int64
	SourceID       *int64
	EntityID       *int64
	HasEntityTrack bool
	From           *time.Time
	To             *time.Time
	Page           int
	PerPage        int
	OrderAsc       bool
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

// EntityCandidateFilter filters entity candidate queries.
type EntityCandidateFilter struct {
	Status  *string
	Type    *string
	Page    int
	PerPage int
}

// ActionFilter defines optional filters for listing actions.
type ActionFilter struct {
	Type   *model.ActionType
	Active *bool
}

// MapPin is a geo-event pin derived from action_execution_metadata.
type MapPin struct {
	TweetID         string               `json:"tweet_id"`
	TweetCreatedAt  string               `json:"tweet_created_at"`
	TweetText       string               `json:"tweet_text"`
	ModelName       string               `json:"model_name"`
	CoordinatesJSON string               `json:"-"` // raw JSON, format indicated by CoordinateType
	CoordinateType  model.CoordinateType `json:"-"`
	MetadataJSON    string               `json:"-"` // JSON object of all execution metadata keys
	PopupTemplate   string               `json:"-"` // from map_config.popup_template, may be empty
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

// BulkDeleteRulesFilter defines filters for bulk-deleting rules.
type BulkDeleteRulesFilter struct {
	IDs    []int64
	Active *bool
	Local  *bool
}

// BulkDeleteSourcesFilter defines filters for bulk-deleting sources.
type BulkDeleteSourcesFilter struct {
	IDs    []int64
	Active *bool
	Type   model.SourceType
}

// BulkDeletePipelinesFilter defines filters for bulk-deleting pipelines.
type BulkDeletePipelinesFilter struct {
	IDs    []int64
	Active *bool
}

// BulkDeleteTriggerPipelineLinksFilter defines filters for bulk-deleting trigger-pipeline links.
type BulkDeleteTriggerPipelineLinksFilter struct {
	IDs         []int64
	PipelineID  *int64
	TriggerType string
}

// BulkDeleteTrackedEntitiesFilter defines filters for bulk-deleting tracked entities.
type BulkDeleteTrackedEntitiesFilter struct {
	IDs    []int64
	Active *bool
	Type   string
}

// BulkDeleteEntityCandidatesFilter defines filters for bulk-deleting entity candidates.
type BulkDeleteEntityCandidatesFilter struct {
	IDs            []int64
	Status         string
	Type           string
	AutoPromoted   *bool
	MinConfidence  *float64
	MaxConfidence  *float64
	MinOccurrences *int
	MaxOccurrences *int
}

// BulkDeleteActionsFilter defines filters for bulk-deleting actions.
type BulkDeleteActionsFilter struct {
	IDs    []int64
	Active *bool
	Type   model.ActionType
}

// BulkDeleteOllamaBackendsFilter defines filters for bulk-deleting Ollama backends.
type BulkDeleteOllamaBackendsFilter struct {
	IDs     []int64
	Enabled *bool
}

// BulkDeleteNotificationProvidersFilter defines filters for bulk-deleting notification providers.
type BulkDeleteNotificationProvidersFilter struct {
	IDs    []int64
	Active *bool
	Type   string
}

// BulkDeleteNotificationChannelsFilter defines filters for bulk-deleting notification channels.
type BulkDeleteNotificationChannelsFilter struct {
	IDs        []int64
	Active     *bool
	ProviderID *int64
}

// BulkDeleteSearchJobsFilter defines filters for bulk-deleting search jobs.
type BulkDeleteSearchJobsFilter struct {
	IDs    []string
	Status model.SearchJobStatus
}

// BulkDeleteDigestJobsFilter defines filters for bulk-deleting digest jobs.
type BulkDeleteDigestJobsFilter struct {
	IDs    []string
	Status model.DigestStatus
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
	BulkDeleteSources(ctx context.Context, f BulkDeleteSourcesFilter) (int64, error)
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
	BulkDeleteRules(ctx context.Context, f BulkDeleteRulesFilter) (int64, error)
	SetRuleActive(ctx context.Context, id int64, active bool, xRuleID string) error
	TweetRuleCount(ctx context.Context, ruleID int64) (int, error)

	// TweetRules
	SaveTweetRule(ctx context.Context, tr *model.TweetRule) error

	// TweetRuleIDs (kept — used by stream listener rule matching)
	ListTweetRuleIDs(ctx context.Context, tweetID string) ([]int64, error)

	// NotificationProviders
	SaveNotificationProvider(ctx context.Context, p *model.NotificationProvider) error
	GetNotificationProvider(ctx context.Context, id int64) (*model.NotificationProvider, error)
	ListNotificationProviders(ctx context.Context) ([]*model.NotificationProvider, error)
	UpdateNotificationProvider(ctx context.Context, p *model.NotificationProvider) error
	DeleteNotificationProvider(ctx context.Context, id int64) error
	BulkDeleteNotificationProviders(ctx context.Context, f BulkDeleteNotificationProvidersFilter) (int64, error)
	SetNotificationProviderActive(ctx context.Context, id int64, active bool) error

	// NotificationChannels
	SaveNotificationChannel(ctx context.Context, c *model.NotificationChannel) error
	GetNotificationChannel(ctx context.Context, id int64) (*model.NotificationChannel, error)
	ListNotificationChannels(ctx context.Context, providerID *int64) ([]*model.NotificationChannel, error)
	UpdateNotificationChannel(ctx context.Context, c *model.NotificationChannel) error
	DeleteNotificationChannel(ctx context.Context, id int64) error
	BulkDeleteNotificationChannels(ctx context.Context, f BulkDeleteNotificationChannelsFilter) (int64, error)
	SetNotificationChannelActive(ctx context.Context, id int64, active bool) error

	// TrackedEntities
	SaveTrackedEntity(ctx context.Context, e *model.TrackedEntity) error
	GetTrackedEntity(ctx context.Context, id int64) (*model.TrackedEntity, error)
	ListTrackedEntities(ctx context.Context, filter TrackedEntityFilter) ([]*model.TrackedEntity, error)
	UpdateTrackedEntity(ctx context.Context, e *model.TrackedEntity) error
	DeleteTrackedEntity(ctx context.Context, id int64) error
	BulkDeleteTrackedEntities(ctx context.Context, f BulkDeleteTrackedEntitiesFilter) (int64, error)
	SetTrackedEntityActive(ctx context.Context, id int64, active bool) error

	// EntityTracks
	ListActiveTrackedEntities(ctx context.Context) ([]*model.TrackedEntity, error)
	SaveEntityTrack(ctx context.Context, t *model.EntityTrack) error
	ListEntityTracks(ctx context.Context, filter EntityTrackFilter) ([]*model.EntityTrack, error)
	CountEntityTracks(ctx context.Context, filter EntityTrackFilter) (int, error)
	ListNERSuggestionsForEntity(ctx context.Context, entityID int64) ([]string, error)
	SearchNERSuggestions(ctx context.Context, name string, entityType string) ([]string, error)

	// EntityCandidates
	UpsertEntityCandidate(ctx context.Context, c *model.EntityCandidate) error
	GetEntityCandidate(ctx context.Context, id int64) (*model.EntityCandidate, error)
	ListEntityCandidates(ctx context.Context, filter EntityCandidateFilter) ([]*model.EntityCandidate, error)
	CountEntityCandidates(ctx context.Context, filter EntityCandidateFilter) (int, error)
	UpdateEntityCandidateStatus(ctx context.Context, id int64, status string, promotedEntityID *int64, autoPromoted bool) error
	DeleteEntityCandidate(ctx context.Context, id int64) error
	BulkDeleteEntityCandidates(ctx context.Context, f BulkDeleteEntityCandidatesFilter) (int64, error)

	// EntityConnections
	UpsertEntityConnection(ctx context.Context, fromID, toID int64) error
	ListEntityConnections(ctx context.Context) ([]*model.EntityConnection, error)
	GetEntityConnectionSubgraph(ctx context.Context, entityID int64, depth, minWeight int) ([]*model.EntityConnection, error)
	DeleteAllEntityConnections(ctx context.Context) error

	// Summaries
	ListSummaries(ctx context.Context, filter SummaryFilter) ([]*model.SummaryItem, error)
	CountSummaries(ctx context.Context, filter SummaryFilter) (int, error)

	// Map
	ListMapPins(ctx context.Context, limit, offset int) ([]*MapPin, error)

	// GeocodeCache
	// GetGeocodeCache returns the cached location name for a pre-rounded coordinate.
	// found=false means no cache entry exists (distinct from found=true, name="" which
	// means Nominatim confirmed no result for that coordinate).
	GetGeocodeCache(ctx context.Context, lat, lon float64) (name string, found bool, err error)
	// SetGeocodeCache stores a location name for a pre-rounded coordinate.
	// name="" is a valid cached value meaning "Nominatim returned no result here".
	SetGeocodeCache(ctx context.Context, lat, lon float64, name string) error

	// DigestJobs
	SaveDigestJob(ctx context.Context, job *model.DigestJob) error
	GetDigestJob(ctx context.Context, id string) (*model.DigestJob, error)
	UpdateDigestJob(ctx context.Context, job *model.DigestJob) error
	ListDigestJobs(ctx context.Context, limit int) ([]*model.DigestJob, error)
	DeleteDigestJob(ctx context.Context, id string) error
	BulkDeleteDigestJobs(ctx context.Context, f BulkDeleteDigestJobsFilter) (int64, error)

	// SearchJobs
	SaveSearchJob(ctx context.Context, job *model.SearchJob) error
	GetSearchJob(ctx context.Context, id string) (*model.SearchJob, error)
	UpdateSearchJob(ctx context.Context, job *model.SearchJob) error
	ListSearchJobs(ctx context.Context, limit int) ([]*model.SearchJob, error)
	DeleteSearchJob(ctx context.Context, id string) error
	BulkDeleteSearchJobs(ctx context.Context, f BulkDeleteSearchJobsFilter) (int64, error)

	// Users
	SaveUser(ctx context.Context, u *model.User) error
	GetUser(ctx context.Context, id int64) (*model.User, error)
	GetUserByUsername(ctx context.Context, username string) (*model.User, error)
	ListUsers(ctx context.Context) ([]*model.User, error)
	UpdateUser(ctx context.Context, u *model.User) error
	DeleteUser(ctx context.Context, id int64) error
	CountAdmins(ctx context.Context) (int, error)

	// Actions (absorbs old Pipelines + Actions)
	SaveAction(ctx context.Context, a *model.Action) error
	GetAction(ctx context.Context, id int64) (*model.Action, error)
	ListActions(ctx context.Context, filter ActionFilter) ([]*model.Action, error)
	UpdateAction(ctx context.Context, a *model.Action) error
	DeleteAction(ctx context.Context, id int64) error
	BulkDeleteActions(ctx context.Context, f BulkDeleteActionsFilter) (int64, error)
	SetActionActive(ctx context.Context, id int64, active bool) error
	ListActionLabels(ctx context.Context, actionID int64) ([]string, error)
	AddActionLabel(ctx context.Context, actionID int64, label string) error
	RemoveActionLabel(ctx context.Context, actionID int64, label string) error

	// Pipelines (replaces Chains)
	SavePipeline(ctx context.Context, p *model.Pipeline) error
	GetPipeline(ctx context.Context, id int64) (*model.Pipeline, error)
	ListPipelines(ctx context.Context) ([]*model.Pipeline, error)
	UpdatePipeline(ctx context.Context, p *model.Pipeline) error
	DeletePipeline(ctx context.Context, id int64) error
	BulkDeletePipelines(ctx context.Context, f BulkDeletePipelinesFilter) (int64, error)
	SetPipelineActive(ctx context.Context, id int64, active bool) error
	GetPipelineGraph(ctx context.Context, pipelineID int64) (*model.PipelineGraph, error)
	SavePipelineGraph(ctx context.Context, pipelineID int64, nodes []*model.PipelineNodeRequest, edges []*model.PipelineEdgeRequest) (*model.PipelineGraph, error)
	GetPipelineIDForNode(ctx context.Context, nodeID int64) (int64, error)

	// TriggerPipelineLinks (replaces TriggerChainLinks)
	SaveTriggerPipelineLink(ctx context.Context, l *model.TriggerPipelineLink) error
	ListTriggerPipelineLinks(ctx context.Context) ([]*model.TriggerPipelineLink, error)
	ListActiveTriggerPipelineLinks(ctx context.Context, triggerType string, sourcePipelineID int64) ([]*model.TriggerPipelineLink, error)
	DeleteTriggerPipelineLink(ctx context.Context, id int64) error
	BulkDeleteTriggerPipelineLinks(ctx context.Context, f BulkDeleteTriggerPipelineLinksFilter) (int64, error)
	SetTriggerPipelineLinkActive(ctx context.Context, id int64, active bool) error

	// Pipeline node execution tracking (for sub-pipeline fan-in)
	InsertPipelineNodeExecution(ctx context.Context, parentNodeID int64, tweetID string, leafNodeID int64) error
	MarkPipelineNodeLeafComplete(ctx context.Context, parentNodeID int64, tweetID string, leafNodeID int64) (allDone bool, err error)

	// Pipeline run tracking (for pipeline_finished trigger)
	// InitPipelineRunTracking seeds a tracking row for (pipelineID, tweetID) with initialCount
	// active tasks. Upserts — resets an existing row if the pipeline is re-run.
	InitPipelineRunTracking(ctx context.Context, pipelineID int64, tweetID string, initialCount int, eventPayload []byte) error
	// IncrementPipelineRunTracking atomically adds 1 to active_tasks for (pipelineID, tweetID).
	// No-op if no tracking row exists (untracked pipelines, e.g. backfill start-from-node runs).
	IncrementPipelineRunTracking(ctx context.Context, pipelineID int64, tweetID string) error
	// DecrementPipelineRunTracking atomically subtracts 1 from active_tasks.
	// Returns done=true when active_tasks reaches 0. Returns done=false with no error
	// if no tracking row exists (untracked pipelines). Sets completed_at when done.
	DecrementPipelineRunTracking(ctx context.Context, pipelineID int64, tweetID string) (done bool, err error)
	// GetPipelineRunEventPayload returns the original TweetEvent JSON stored at tracking time.
	GetPipelineRunEventPayload(ctx context.Context, pipelineID int64, tweetID string) ([]byte, error)

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
	CreateOllamaBackend(ctx context.Context, name, url string, maxConcurrent int) (model.OllamaBackend, error)
	UpdateOllamaBackend(ctx context.Context, id int64, name, url string, maxConcurrent int) error
	DeleteOllamaBackend(ctx context.Context, id int64) error
	BulkDeleteOllamaBackends(ctx context.Context, f BulkDeleteOllamaBackendsFilter) (int64, error)
	SetOllamaBackendEnabled(ctx context.Context, id int64, enabled bool) error

	// ActionBackends (replaces PipelineBackends)
	ListActionBackends(ctx context.Context, actionID int64) ([]model.ActionBackend, error)
	SetActionBackends(ctx context.Context, actionID int64, assignments []model.ActionBackend) error
	RemoveActionBackend(ctx context.Context, actionID, backendID int64) error
	// ListAllActionBackendIDs returns actionID → []backendID in priority order (for ActionCache.Load).
	ListAllActionBackendIDs(ctx context.Context) (map[int64][]int64, error)

	Close() error
}
