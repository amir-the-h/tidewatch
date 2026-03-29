package api

import (
	"encoding/json"
	"io"
	"net/http"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/amir-the-h/iran-watcher/internal/inference"
	"github.com/amir-the-h/iran-watcher/internal/model"
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

	// Find active actions backed by the summarizer pipeline via the action cache.
	var actionIDs []int64
	if s.actionCache != nil {
		entries := s.actionCache.AllForRules(nil)
		for _, e := range entries {
			if e.Pipeline != nil && e.Pipeline.Name() == summarizerName {
				actionIDs = append(actionIDs, e.Action.ID)
			}
		}
	}

	count := 0
	for _, tweet := range tweets {
		ev, _ := json.Marshal(queue.TweetEvent{
			TweetID:      tweet.ID,
			Text:         tweet.Text,
			AuthorHandle: tweet.AuthorHandle,
		})
		pushed := false
		for _, actionID := range actionIDs {
			task := &queue.Task{
				TriggerType: string(model.TriggerTypeNewTweet),
				Event:       ev,
				ActionID:    actionID,
				Context:     map[string]any{},
			}
			if err := s.producer.PushTask(r.Context(), task); err != nil {
				s.logger.Error("api: run summaries: push task", "tweet_id", tweet.ID, "action_id", actionID, "error", err)
			} else {
				pushed = true
			}
		}
		if pushed {
			count++
		}
	}

	writeJSON(w, http.StatusAccepted, map[string]any{"queued": count})
}

// handleDigest accepts digest parameters, creates a digest job, and enqueues a
// task for each active action with trigger_type=digest_requested. Poll GET
// /digest/{id} for status and result.
func (s *Server) handleDigest(w http.ResponseWriter, r *http.Request) {
	if s.actionCache == nil {
		writeError(w, http.StatusServiceUnavailable, "action cache not available")
		return
	}

	entries := s.actionCache.AllForTrigger(model.TriggerTypeDigestRequested, nil)
	if len(entries) == 0 {
		writeError(w, http.StatusUnprocessableEntity, "no active digest action configured (create an action with trigger_type 'digest_requested' backed by an Ollama pipeline)")
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

	ev, _ := json.Marshal(queue.DigestEvent{DigestJobID: job.ID})
	for _, entry := range entries {
		task := &queue.Task{
			TriggerType: string(model.TriggerTypeDigestRequested),
			Event:       ev,
			ActionID:    entry.Action.ID,
			Context:     map[string]any{"digest_job_id": job.ID},
		}
		if err := s.producer.PushTask(r.Context(), task); err != nil {
			s.logger.Error("api: digest: push task", "action_id", entry.Action.ID, "job_id", job.ID, "error", err)
		}
	}

	writeJSON(w, http.StatusAccepted, job)
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
