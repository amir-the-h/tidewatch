package api

import (
	"encoding/json"
	"io"
	"net/http"
	"strconv"
	"time"

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

	count := 0
	for _, tweet := range tweets {
		st := &model.StreamTweet{
			Tweet:        tweet,
			TargetModels: []string{summarizerName},
		}
		if err := s.producer.Push(r.Context(), queue.QueueRaw, st); err != nil {
			s.logger.Error("api: run summaries: push", "tweet_id", tweet.ID, "error", err)
			continue
		}
		count++
	}

	writeJSON(w, http.StatusAccepted, map[string]any{"queued": count})
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
