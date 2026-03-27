package api

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/amir-the-h/iran-watcher/internal/inference"
	"github.com/amir-the-h/iran-watcher/internal/model"
	"github.com/amir-the-h/iran-watcher/internal/notifier"
	"github.com/amir-the-h/iran-watcher/internal/queue"
	"github.com/amir-the-h/iran-watcher/internal/store"
)

// handleListSummaries returns paginated tweet+summary pairs for tweets that
// have been processed by an active summarizer model.
//
// Query params: from, to (RFC3339), rule_id, source_id, entity_id, page, per_page.
func (s *Server) handleListSummaries(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()

	filter := store.SummaryFilter{
		Page:    parseIntParam(q.Get("page"), 1),
		PerPage: parseIntParam(q.Get("per_page"), 50),
	}

	if v := q.Get("rule_id"); v != "" {
		if id, err := strconv.ParseInt(v, 10, 64); err == nil {
			filter.RuleID = &id
		}
	}
	if v := q.Get("source_id"); v != "" {
		if id, err := strconv.ParseInt(v, 10, 64); err == nil {
			filter.SourceID = &id
		}
	}
	if v := q.Get("entity_id"); v != "" {
		if id, err := strconv.ParseInt(v, 10, 64); err == nil {
			filter.EntityID = &id
		}
	}
	if v := q.Get("from"); v != "" {
		if t, err := time.Parse(time.RFC3339, v); err == nil {
			filter.From = &t
		}
	}
	if v := q.Get("to"); v != "" {
		if t, err := time.Parse(time.RFC3339, v); err == nil {
			filter.To = &t
		}
	}

	items, err := s.store.ListSummaries(r.Context(), filter)
	if err != nil {
		s.logger.Error("api: list summaries", "error", err)
		writeError(w, http.StatusInternalServerError, "failed to list summaries")
		return
	}
	total, err := s.store.CountSummaries(r.Context(), filter)
	if err != nil {
		s.logger.Error("api: count summaries", "error", err)
		writeError(w, http.StatusInternalServerError, "failed to count summaries")
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"summaries": items,
		"total":     total,
	})
}

// handleRunSummaries queues filtered tweets for summarization by the active
// summarizer pipeline. Follows the same pattern as handleRunModelPipeline.
func (s *Server) handleRunSummaries(w http.ResponseWriter, r *http.Request) {
	if s.registry == nil {
		writeError(w, http.StatusServiceUnavailable, "registry not available")
		return
	}

	// Find the first active summarizer pipeline.
	var summarizerName string
	for _, p := range s.registry.All() {
		if _, ok := p.(inference.SummarizationPipeline); ok {
			summarizerName = p.Name()
			break
		}
	}
	if summarizerName == "" {
		writeError(w, http.StatusUnprocessableEntity, "no active summarizer model in registry")
		return
	}

	var req struct {
		From     *time.Time `json:"from"`
		To       *time.Time `json:"to"`
		RuleID   *int64     `json:"rule_id"`
		SourceID *int64     `json:"source_id"`
		EntityID *int64     `json:"entity_id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil && err != io.EOF {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	const pageSize = 5000
	tweetFilter := store.TweetFilter{
		RuleID:   req.RuleID,
		SourceID: req.SourceID,
		EntityID: req.EntityID,
		From:     req.From,
		To:       req.To,
		Page:     1,
		PerPage:  pageSize,
	}

	tweets, err := s.store.ListTweets(r.Context(), tweetFilter)
	if err != nil {
		s.logger.Error("api: run summaries: fetch tweets", "error", err)
		writeError(w, http.StatusInternalServerError, "failed to fetch tweets")
		return
	}

	// Find the pipeline action backed by the summarizer model.
	active := true
	pipelineType := model.ActionTypePipeline
	dbActions, _ := s.store.ListActions(r.Context(), store.ActionFilter{Active: &active, Type: &pipelineType})
	var actionIDs []int64
	for _, a := range dbActions {
		var cfg struct {
			ModelID int64 `json:"model_id"`
		}
		if err := json.Unmarshal(a.Config, &cfg); err == nil {
			mdl, mErr := s.store.GetModel(r.Context(), cfg.ModelID)
			if mErr == nil && mdl.Name == summarizerName {
				actionIDs = append(actionIDs, a.ID)
			}
		}
	}

	count := 0
	for _, tweet := range tweets {
		st := &model.StreamTweet{
			Tweet:         tweet,
			TargetActions: actionIDs,
		}
		if err := s.producer.Push(r.Context(), queue.QueueRaw, st); err != nil {
			s.logger.Error("api: run summaries: push", "tweet_id", tweet.ID, "error", err)
			continue
		}
		count++
	}

	writeJSON(w, http.StatusAccepted, map[string]any{"queued": count})
}

// handleDigest accepts digest parameters, creates a background job, and returns
// the job ID immediately. Poll GET /digest/{id} for status and result.
func (s *Server) handleDigest(w http.ResponseWriter, r *http.Request) {
	if s.registry == nil {
		writeError(w, http.StatusServiceUnavailable, "registry not available")
		return
	}

	// Prefer a pipeline explicitly named "digest"; fall back to first available.
	var digestPipeline inference.DigestPipeline
	for _, p := range s.registry.All() {
		if dp, ok := p.(inference.DigestPipeline); ok {
			if digestPipeline == nil {
				digestPipeline = dp
			}
			if p.Name() == "digest" {
				digestPipeline = dp
				break
			}
		}
	}
	if digestPipeline == nil {
		writeError(w, http.StatusUnprocessableEntity, "no active digest-capable model (requires an Ollama extraction model)")
		return
	}

	var params model.DigestParams
	if err := json.NewDecoder(r.Body).Decode(&params); err != nil && err != io.EOF {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if params.Limit <= 0 || params.Limit > 1000 {
		params.Limit = 200
	}

	job := &model.DigestJob{
		ID:        uuid.New().String(),
		Status:    model.DigestStatusPending,
		Params:    params,
		CreatedAt: time.Now().UTC(),
	}
	if err := s.store.SaveDigestJob(r.Context(), job); err != nil {
		s.logger.Error("api: digest: save job", "error", err)
		writeError(w, http.StatusInternalServerError, "failed to create digest job")
		return
	}

	go s.runDigestJob(job, digestPipeline, digestPipeline.Name())

	writeJSON(w, http.StatusAccepted, job)
}

// runDigestJob executes the digest in the background and persists the result.
func (s *Server) runDigestJob(job *model.DigestJob, pipeline inference.DigestPipeline, pipelineName string) {
	ctx := s.ctx

	job.Status = model.DigestStatusRunning
	if err := s.store.UpdateDigestJob(ctx, job); err != nil {
		s.logger.Error("api: digest job: mark running", "job_id", job.ID, "error", err)
	}

	p := job.Params
	tweets, err := s.store.ListTweets(ctx, store.TweetFilter{
		RuleID:   p.RuleID,
		SourceID: p.SourceID,
		EntityID: p.EntityID,
		From:     p.From,
		To:       p.To,
		Page:     1,
		PerPage:  p.Limit,
	})

	now := time.Now().UTC()
	job.CompletedAt = &now

	if err != nil {
		s.logger.Error("api: digest job: list tweets", "job_id", job.ID, "error", err)
		job.Status = model.DigestStatusError
		job.Error = "failed to fetch tweets: " + err.Error()
		_ = s.store.UpdateDigestJob(ctx, job)
		s.emitDigestEvent(job, pipelineName)
		return
	}

	job.TweetCount = len(tweets)

	if len(tweets) == 0 {
		job.Status = model.DigestStatusDone
		job.Result = "No tweets found matching the selected filters."
		_ = s.store.UpdateDigestJob(ctx, job)
		s.emitDigestEvent(job, pipelineName)
		return
	}

	formatted := make([]string, len(tweets))
	for i, t := range tweets {
		if t.AuthorHandle != "" {
			formatted[i] = fmt.Sprintf("@%s: %s", t.AuthorHandle, t.Text)
		} else {
			formatted[i] = t.Text
		}
	}

	digest, err := pipeline.Digest(formatted)
	if err != nil {
		s.logger.Error("api: digest job: run", "job_id", job.ID, "error", err)
		job.Status = model.DigestStatusError
		job.Error = "digest generation failed: " + err.Error()
		_ = s.store.UpdateDigestJob(ctx, job)
		s.emitDigestEvent(job, pipelineName)
		return
	}

	job.Status = model.DigestStatusDone
	job.Result = digest
	if err := s.store.UpdateDigestJob(ctx, job); err != nil {
		s.logger.Error("api: digest job: save result", "job_id", job.ID, "error", err)
	}
	s.emitDigestEvent(job, pipelineName)
}

// emitDigestEvent fires a digest_done or digest_failed notification event.
func (s *Server) emitDigestEvent(job *model.DigestJob, pipelineName string) {
	if s.dispatcher == nil {
		return
	}
	eventType := notifier.EventDigestDone
	if job.Status == model.DigestStatusError {
		eventType = notifier.EventDigestFailed
	}
	s.dispatcher.Emit(notifier.NotificationEvent{
		Type:      eventType,
		DigestJob: job,
		ModelName: pipelineName,
		Error:     job.Error,
		Timestamp: time.Now().UTC(),
	})
}

// handleGetDigest returns the status and result of a digest job.
func (s *Server) handleGetDigest(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	job, err := s.store.GetDigestJob(r.Context(), id)
	if err != nil {
		writeError(w, http.StatusNotFound, "digest job not found")
		return
	}
	writeJSON(w, http.StatusOK, job)
}

// handleListDigests returns recent digest jobs.
func (s *Server) handleListDigests(w http.ResponseWriter, r *http.Request) {
	limit := parseIntParam(r.URL.Query().Get("limit"), 20)
	jobs, err := s.store.ListDigestJobs(r.Context(), limit)
	if err != nil {
		s.logger.Error("api: list digests", "error", err)
		writeError(w, http.StatusInternalServerError, "failed to list digest jobs")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"jobs": jobs})
}

// handleDeleteDigest deletes a digest job by ID.
func (s *Server) handleDeleteDigest(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	if err := s.store.DeleteDigestJob(r.Context(), id); err != nil {
		writeError(w, http.StatusNotFound, "digest job not found")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// parseIntParam parses a string as int, returning def on failure.
func parseIntParam(s string, def int) int {
	if s == "" {
		return def
	}
	v, err := strconv.Atoi(s)
	if err != nil || v < 1 {
		return def
	}
	return v
}
