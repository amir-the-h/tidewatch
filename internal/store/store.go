package store

import (
	"context"
	"time"

	"github.com/amir-the-h/tidewatch/internal/model"
)

// SignalFilter defines optional filters for listing signals.
type SignalFilter struct {
	RuleID         *int64
	SourceID       *int64 // signals seen by this source (signal_sources)
	SourceType     string // signals seen by any source of this connector type
	Kind           *model.SignalKind
	EntityID       *int64
	HasEntityTrack bool
	From           *time.Time
	To             *time.Time
	Page           int
	PerPage        int
	OrderAsc       bool
}

// SummaryFilter defines optional filters for listing signal+summary pairs.
type SummaryFilter struct {
	RuleID   *int64
	EntityID *int64
	From     *time.Time
	To       *time.Time
	Page     int
	PerPage  int
}

// SourceFilter defines optional filters for listing generic sources.
type SourceFilter struct {
	Type   string
	Active *bool
}

// BulkDeleteSourcesFilter defines filters for bulk-deleting generic sources.
type BulkDeleteSourcesFilter struct {
	IDs    []int64
	Active *bool
	Type   string
}

// XTermFilter defines optional filters for listing X terms.
type XTermFilter struct {
	Type   *model.XTermType
	Class  *model.XTermClass
	Active *bool
}

// XRuleFilter defines optional filters for listing X rules.
type XRuleFilter struct {
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
	SignalID *int64
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
	SignalID          int64                `json:"signal_id"`
	SignalPublishedAt string               `json:"signal_published_at"`
	SignalText        string               `json:"signal_text"`
	ModelName         string               `json:"model_name"`
	CoordinatesJSON   string               `json:"-"` // raw JSON, format indicated by CoordinateType
	CoordinateType    model.CoordinateType `json:"-"`
	MetadataJSON      string               `json:"-"` // JSON object of all execution metadata keys
	PopupTemplate     string               `json:"-"` // from map_config.popup_template, may be empty
}

// SignalActionSummary is the aggregated result of all successful executions of one
// action on a single signal. Metadata holds the latest value per key (parsed from
// JSON) across all executions. Empty Metadata means the action ran but produced no
// persistent metadata (e.g. a twitter_retweet action).
type SignalActionSummary struct {
	ActionID   int64
	ActionName string
	Metadata   map[string]any
}

// ExecutionFilter defines optional filters for listing action executions.
type ExecutionFilter struct {
	ActionID *int64
	SignalID *int64
	Status   *string
	From     *time.Time
	To       *time.Time
	Page     int
	PerPage  int
}

// BulkDeleteXRulesFilter defines filters for bulk-deleting rules.
type BulkDeleteXRulesFilter struct {
	IDs    []int64
	Active *bool
	Local  *bool
}

// BulkDeleteXTermsFilter defines filters for bulk-deleting X terms.
type BulkDeleteXTermsFilter struct {
	IDs    []int64
	Active *bool
	Type   model.XTermType
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

// BulkDeleteDigestJobsFilter defines filters for bulk-deleting digest jobs.
type BulkDeleteDigestJobsFilter struct {
	IDs    []string
	Status model.DigestStatus
}

// Store is the repository interface for all persistent data.
type Store interface {
	// Signals
	// SaveSignal inserts s unless a signal with the same (ExtNS, ExternalID)
	// exists. Either way it sets s.ID to the stored row's ID, and reports
	// whether the row is new.
	SaveSignal(ctx context.Context, s *model.Signal) (isNew bool, err error)
	GetSignal(ctx context.Context, id int64) (*model.Signal, error)
	ListSignals(ctx context.Context, filter SignalFilter) ([]*model.Signal, error)
	CountSignals(ctx context.Context, filter SignalFilter) (int, error)
	// DeleteSignals deletes the given signals; executions, metadata, entity
	// tracks and rule matches go with them through ON DELETE CASCADE.
	DeleteSignals(ctx context.Context, ids []int64) (int64, error)
	// DeleteNoiseSignals deletes X signals that either match no rules at all, or
	// whose every matching rule is marked local=true. Returns the count deleted.
	DeleteNoiseSignals(ctx context.Context) (int64, error)

	// X terms (rule atoms)
	SaveXTerm(ctx context.Context, source *model.XTerm) error
	UpdateXTerm(ctx context.Context, source *model.XTerm) error
	GetXTerm(ctx context.Context, id int64) (*model.XTerm, error)
	ListXTerms(ctx context.Context, filter XTermFilter) ([]*model.XTerm, error)
	DeleteXTerm(ctx context.Context, id int64) error
	BulkDeleteXTerms(ctx context.Context, f BulkDeleteXTermsFilter) (int64, error)
	SetXTermActive(ctx context.Context, id int64, active bool) error
	ActiveRuleRefCount(ctx context.Context, sourceID int64) (int, error)
	RuleRefCount(ctx context.Context, sourceID int64) (int, error)

	// Sources (configured connector instances)
	SaveSource(ctx context.Context, src *model.Source) error
	GetSource(ctx context.Context, id int64) (*model.Source, error)
	ListSources(ctx context.Context, filter SourceFilter) ([]*model.Source, error)
	UpdateSource(ctx context.Context, src *model.Source) error
	SetSourceActive(ctx context.Context, id int64, active bool) error
	DeleteSource(ctx context.Context, id int64) error
	BulkDeleteSources(ctx context.Context, f BulkDeleteSourcesFilter) (int64, error)
	// GetSourceState returns the source's state, or a zero state with an
	// empty cursor when none has been written yet.
	GetSourceState(ctx context.Context, sourceID int64) (*model.SourceState, error)
	ListSourceStates(ctx context.Context) ([]*model.SourceState, error)
	UpsertSourceState(ctx context.Context, st *model.SourceState) error
	SaveSourceRun(ctx context.Context, run *model.SourceRun) error
	UpdateSourceRun(ctx context.Context, run *model.SourceRun) error
	GetSourceRun(ctx context.Context, id string) (*model.SourceRun, error)
	ListSourceRuns(ctx context.Context, sourceID int64, limit int) ([]*model.SourceRun, error)
	// FailUnfinishedSourceRuns marks pending and running runs as errored (used
	// at startup: their goroutines died with the previous process).
	FailUnfinishedSourceRuns(ctx context.Context, msg string) (int64, error)
	// AddSignalSource records that a source saw a signal (idempotent).
	AddSignalSource(ctx context.Context, signalID, sourceID int64) error
	// ListSignalSources returns the sources that saw a signal, earliest first.
	ListSignalSources(ctx context.Context, signalID int64) ([]*model.SourceRef, error)

	// X rules
	SaveXRule(ctx context.Context, rule *model.XRule) error
	GetXRule(ctx context.Context, id int64) (*model.XRule, error)
	GetXRuleByXRuleID(ctx context.Context, xRuleID string) (*model.XRule, error)
	ListXRules(ctx context.Context, filter XRuleFilter) ([]*model.XRule, error)
	ListXRulesDependingOn(ctx context.Context, ruleID int64) ([]*model.XRule, error)
	UpdateXRule(ctx context.Context, rule *model.XRule) error
	DeleteXRule(ctx context.Context, id int64) error
	BulkDeleteXRules(ctx context.Context, f BulkDeleteXRulesFilter) (int64, error)
	SetXRuleActive(ctx context.Context, id int64, active bool, xRuleID string) error
	XRuleMatchCount(ctx context.Context, ruleID int64) (int, error)

	// XRuleMatches
	SaveXRuleMatch(ctx context.Context, m *model.XRuleMatch) error
	ListXRuleIDs(ctx context.Context, signalID int64) ([]int64, error)

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
	InsertPipelineNodeExecution(ctx context.Context, parentNodeID int64, signalID int64, leafNodeID int64) error
	MarkPipelineNodeLeafComplete(ctx context.Context, parentNodeID int64, signalID int64, leafNodeID int64) (allDone bool, err error)

	// Pipeline run tracking (for pipeline_finished trigger)
	// InitPipelineRunTracking seeds a tracking row for (pipelineID, signalID) with initialCount
	// active tasks. Upserts — resets an existing row if the pipeline is re-run.
	InitPipelineRunTracking(ctx context.Context, pipelineID int64, signalID int64, initialCount int, eventPayload []byte) error
	// IncrementPipelineRunTracking atomically adds 1 to active_tasks for (pipelineID, signalID).
	// No-op if no tracking row exists (untracked pipelines, e.g. backfill start-from-node runs).
	IncrementPipelineRunTracking(ctx context.Context, pipelineID int64, signalID int64) error
	// DecrementPipelineRunTracking atomically subtracts 1 from active_tasks.
	// Returns done=true when active_tasks reaches 0. Returns done=false with no error
	// if no tracking row exists (untracked pipelines). Sets completed_at when done.
	DecrementPipelineRunTracking(ctx context.Context, pipelineID int64, signalID int64) (done bool, err error)
	// GetPipelineRunEventPayload returns the original SignalEvent JSON stored at tracking time.
	GetPipelineRunEventPayload(ctx context.Context, pipelineID int64, signalID int64) ([]byte, error)

	// ActionExecutions
	SaveActionExecution(ctx context.Context, e *model.ActionExecution) error
	ListActionExecutions(ctx context.Context, filter ExecutionFilter) ([]*model.ActionExecution, error)
	ListActionExecutionMetadata(ctx context.Context, execID int64) (map[string]string, error)
	MergeActionExecutionMetadata(ctx context.Context, actionID int64, signalID int64) (map[string]string, error)
	// MergeAllSignalExecutionMetadata returns the latest value per key across all
	// successful executions for signalID regardless of action. This gives notification
	// pipelines access to upstream ONNX/Ollama outputs even when not triggered via chain.
	MergeAllSignalExecutionMetadata(ctx context.Context, signalID int64) (map[string]string, error)
	// ListSignalActionSummaries returns one SignalActionSummary per action that had at
	// least one successful execution on signalID. Each summary carries the latest value
	// per metadata key (same merge logic as MergeActionExecutionMetadata). Actions with
	// no metadata rows are included with an empty Metadata map.
	ListSignalActionSummaries(ctx context.Context, signalID int64) ([]SignalActionSummary, error)
	SaveActionExecutionMetadata(ctx context.Context, execID int64, key, value string) error
	// DeleteActionExecutions deletes all execution records and their metadata for
	// the given signals. If actionIDs is non-empty, only executions for those actions
	// are deleted. Returns the count of deleted execution rows.
	DeleteActionExecutions(ctx context.Context, signalIDs []int64, actionIDs []int64) (int64, error)

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
