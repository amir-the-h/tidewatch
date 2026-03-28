package action

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/amir-the-h/iran-watcher/internal/inference"
	"github.com/amir-the-h/iran-watcher/internal/model"
	"github.com/amir-the-h/iran-watcher/internal/notifier"
	"github.com/amir-the-h/iran-watcher/internal/queue"
	"github.com/amir-the-h/iran-watcher/internal/store"
)

// PipelineAction wraps an inference.Pipeline and handles the full lifecycle:
// run the model, save the categorization, match entities, dead-letter on failure.
type PipelineAction struct {
	id       int64
	name     string
	ruleIDs  []int64
	pipeline inference.Pipeline
	st       store.Store
	producer queue.Producer
	emitter  notifier.EventEmitter
	reg      *inference.Registry // used to find a SummarizationPipeline for entity matching
	logger   *slog.Logger
}

// NewPipelineAction constructs a PipelineAction.
func NewPipelineAction(
	id int64,
	name string,
	ruleIDs []int64,
	pipeline inference.Pipeline,
	st store.Store,
	producer queue.Producer,
	emitter notifier.EventEmitter,
	reg *inference.Registry,
	logger *slog.Logger,
) *PipelineAction {
	return &PipelineAction{
		id:       id,
		name:     name,
		ruleIDs:  ruleIDs,
		pipeline: pipeline,
		st:       st,
		producer: producer,
		emitter:  emitter,
		reg:      reg,
		logger:   logger,
	}
}

func (a *PipelineAction) ID() int64              { return a.id }
func (a *PipelineAction) Name() string           { return a.name }
func (a *PipelineAction) Type() model.ActionType { return model.ActionTypePipeline }
func (a *PipelineAction) RuleIDs() []int64       { return a.ruleIDs }

// MaxConcurrency delegates to the underlying pipeline if it implements ConcurrencyLimiter.
func (a *PipelineAction) MaxConcurrency() int {
	if cl, ok := a.pipeline.(inference.ConcurrencyLimiter); ok {
		return cl.MaxConcurrency()
	}
	return 0
}

// Execute runs the pipeline, saves the result, matches entities, and logs
// failures to the dead-letter queue. Each failure is logged independently —
// no coordination with other actions needed.
func (a *PipelineAction) Execute(ctx context.Context, tweet model.Tweet) error {
	result, runErr := a.pipeline.Run([]byte(tweet.Text))
	if runErr != nil {
		if a.logger != nil {
			a.logger.Warn("action: pipeline failed",
				"action_id", a.id,
				"tweet_id", tweet.ID,
				"pipeline", a.pipeline.Name(),
				"error", fmt.Errorf("action: %w", runErr))
		}
		if a.emitter != nil {
			a.emitter.Emit(notifier.NotificationEvent{
				Type:      notifier.EventCategorizationFailed,
				Tweet:     &tweet,
				ModelName: a.pipeline.Name(),
				Error:     runErr.Error(),
				Timestamp: time.Now(),
			})
		}
		a.pushDeadLetter(ctx, &tweet, runErr.Error())
		return runErr
	}

	// Extract NER entities if the pipeline produced them.
	var nerEnts []model.NEREntity
	var nerOut struct {
		Entities []model.NEREntity `json:"entities"`
	}
	if json.Unmarshal(result, &nerOut) == nil {
		nerEnts = nerOut.Entities
	}

	c := &model.Categorization{
		TweetID:   tweet.ID,
		ModelName: a.pipeline.Name(),
		Results:   result,
		CreatedAt: time.Now(),
	}
	if saveErr := a.st.SaveCategorization(ctx, c); saveErr != nil {
		if a.logger != nil {
			a.logger.Warn("action: save categorization",
				"action_id", a.id,
				"tweet_id", tweet.ID,
				"pipeline", a.pipeline.Name(),
				"error", fmt.Errorf("action: %w", saveErr))
		}
		a.pushDeadLetter(ctx, &tweet, saveErr.Error())
		return saveErr
	}

	if a.emitter != nil {
		a.emitter.Emit(notifier.NotificationEvent{
			Type:      notifier.EventTweetCategorized,
			Tweet:     &tweet,
			ModelName: a.pipeline.Name(),
			Results:   result,
			Timestamp: time.Now(),
		})
	}

	// Entity matching — run after the categorization is saved.
	a.matchEntities(ctx, &tweet, nerEnts)

	return nil
}

func (a *PipelineAction) pushDeadLetter(ctx context.Context, tweet *model.Tweet, errMsg string) {
	if a.producer == nil {
		return
	}
	ft := &queue.FailedTweet{
		Tweet: &model.StreamTweet{Tweet: tweet},
		FailedModels: []queue.FailedModel{{
			Name:      a.pipeline.Name(),
			Error:     errMsg,
			Timestamp: time.Now(),
		}},
	}
	if err := a.producer.PushFailed(ctx, ft); err != nil && a.logger != nil {
		a.logger.Error("action: push dead letter",
			"action_id", a.id,
			"tweet_id", tweet.ID,
			"error", err)
	}
}

// matchEntities loads active tracked entities and records any matches.
// Mirrors the logic in internal/worker/entity_matcher.go.
func (a *PipelineAction) matchEntities(ctx context.Context, tweet *model.Tweet, nerEntities []model.NEREntity) {
	entities, err := a.st.ListActiveTrackedEntities(ctx)
	if err != nil {
		if a.logger != nil {
			a.logger.Warn("action: match entities: list active", "error", err)
		}
		return
	}
	if len(entities) == 0 {
		return
	}

	// Generate summary once per tweet using the first active summarizer pipeline.
	var summary string
	if a.reg != nil {
		for _, p := range a.reg.All() {
			if sp, ok := p.(inference.SummarizationPipeline); ok {
				s, sumErr := sp.Summarize(ctx, tweet.Text)
				if sumErr != nil {
					if a.logger != nil {
						a.logger.Warn("action: match entities: summarize",
							"tweet_id", tweet.ID,
							"pipeline", p.Name(),
							"error", sumErr)
					}
				} else {
					summary = s
				}
				break
			}
		}
	}

	tweetTextLower := strings.ToLower(tweet.Text)
	for _, entity := range entities {
		candidates := make([]string, 0, 1+len(entity.Aliases))
		candidates = append(candidates, entity.Name)
		candidates = append(candidates, entity.Aliases...)

		matchSource, context := matchCandidate(tweetTextLower, candidates, nerEntities)
		if matchSource == "" {
			continue
		}

		track := &model.EntityTrack{
			EntityID:    entity.ID,
			TweetID:     tweet.ID,
			MatchSource: matchSource,
			Context:     context,
			Summary:     summary,
		}
		if saveErr := a.st.SaveEntityTrack(ctx, track); saveErr != nil {
			if a.logger != nil {
				a.logger.Warn("action: match entities: save track",
					"entity_id", entity.ID,
					"tweet_id", tweet.ID,
					"error", saveErr)
			}
		}
	}
}

func matchCandidate(tweetTextLower string, candidates []string, nerEntities []model.NEREntity) (matchSource, context string) {
	for _, c := range candidates {
		cLower := strings.ToLower(c)
		for _, ne := range nerEntities {
			if strings.ToLower(ne.Word) == cLower {
				return "ner", ne.Word
			}
		}
		if strings.Contains(tweetTextLower, cLower) {
			return "text", c
		}
	}
	return "", ""
}
