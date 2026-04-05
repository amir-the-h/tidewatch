package api

import (
	"encoding/json"
	"net/http"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/amir-the-h/iran-watcher/internal/model"
	"github.com/amir-the-h/iran-watcher/internal/queue"
	"github.com/amir-the-h/iran-watcher/internal/store"
	"github.com/amir-the-h/iran-watcher/internal/worker"
)

func (s *Server) handleListTweets(w http.ResponseWriter, r *http.Request) {
	filter := store.TweetFilter{
		Page:    1,
		PerPage: 20,
	}

	q := r.URL.Query()

	if v := q.Get("rule_id"); v != "" {
		id, err := strconv.ParseInt(v, 10, 64)
		if err != nil {
			writeError(w, http.StatusBadRequest, "invalid rule_id")
			return
		}
		filter.RuleID = &id
	}
	if v := q.Get("from"); v != "" {
		t, err := time.Parse(time.RFC3339, v)
		if err != nil {
			writeError(w, http.StatusBadRequest, "invalid from (use RFC3339)")
			return
		}
		filter.From = &t
	}
	if v := q.Get("to"); v != "" {
		t, err := time.Parse(time.RFC3339, v)
		if err != nil {
			writeError(w, http.StatusBadRequest, "invalid to (use RFC3339)")
			return
		}
		filter.To = &t
	}
	if v := q.Get("page"); v != "" {
		page, err := strconv.Atoi(v)
		if err != nil || page < 1 {
			writeError(w, http.StatusBadRequest, "invalid page")
			return
		}
		filter.Page = page
	}
	if v := q.Get("per_page"); v != "" {
		pp, err := strconv.Atoi(v)
		if err != nil || pp < 1 {
			writeError(w, http.StatusBadRequest, "invalid per_page")
			return
		}
		filter.PerPage = pp
	}

	tweets, err := s.store.ListTweets(r.Context(), filter)
	if err != nil {
		s.logger.Error("api: list tweets", "error", err)
		writeError(w, http.StatusInternalServerError, "failed to list tweets")
		return
	}
	total, err := s.store.CountTweets(r.Context(), filter)
	if err != nil {
		s.logger.Error("api: count tweets", "error", err)
		writeError(w, http.StatusInternalServerError, "failed to count tweets")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"tweets": tweets, "total": total})
}

func (s *Server) handleGetTweet(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")

	tweet, err := s.store.GetTweet(r.Context(), id)
	if err != nil {
		writeError(w, http.StatusNotFound, "tweet not found")
		return
	}

	tweetID := tweet.ID

	// Load action executions for this tweet, with latest-per-action deduplication.
	execs, err := s.store.ListActionExecutions(r.Context(), store.ExecutionFilter{TweetID: &tweetID})
	if err != nil {
		s.logger.Error("api: list executions for tweet", "tweet_id", id, "error", err)
		writeError(w, http.StatusInternalServerError, "failed to list executions")
		return
	}

	type execResult struct {
		ID         int64          `json:"id"`
		Status     string         `json:"status"`
		Error      string         `json:"error,omitempty"`
		ExecutedAt string         `json:"executed_at"`
		Metadata   map[string]any `json:"metadata"`
	}
	type actionResult struct {
		ActionID   int64        `json:"action_id"`
		ActionName string       `json:"action_name"`
		Executions []execResult `json:"executions"`
	}

	// Group all executions under their action, preserving insertion order.
	actionOrder := []int64{}
	actionMap := map[int64]*actionResult{}

	for _, e := range execs {
		if _, known := actionMap[e.ActionID]; !known {
			name := ""
			if a, err := s.store.GetAction(r.Context(), e.ActionID); err == nil {
				name = a.Name
			}
			actionOrder = append(actionOrder, e.ActionID)
			actionMap[e.ActionID] = &actionResult{
				ActionID:   e.ActionID,
				ActionName: name,
				Executions: []execResult{},
			}
		}

		rawMeta, _ := s.store.ListActionExecutionMetadata(r.Context(), e.ID)
		meta := make(map[string]any, len(rawMeta))
		for k, v := range rawMeta {
			var parsed any
			if json.Unmarshal([]byte(v), &parsed) == nil {
				meta[k] = parsed
			} else {
				meta[k] = v
			}
		}

		actionMap[e.ActionID].Executions = append(actionMap[e.ActionID].Executions, execResult{
			ID:         e.ID,
			Status:     e.Status,
			Error:      e.Error,
			ExecutedAt: e.ExecutedAt.UTC().Format(time.RFC3339),
			Metadata:   meta,
		})
	}

	actions := make([]actionResult, 0, len(actionOrder))
	for _, id := range actionOrder {
		actions = append(actions, *actionMap[id])
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"tweet":   tweet,
		"actions": actions,
	})
}

func (s *Server) handleDeleteTweets(w http.ResponseWriter, r *http.Request) {
	var req struct {
		IDs []string `json:"ids"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if len(req.IDs) == 0 {
		writeError(w, http.StatusBadRequest, "ids is required")
		return
	}
	n, err := s.store.DeleteTweets(r.Context(), req.IDs)
	if err != nil {
		s.logger.Error("api: delete tweets", "error", err)
		writeError(w, http.StatusInternalServerError, "failed to delete tweets")
		return
	}
	writeJSON(w, http.StatusOK, map[string]int64{"deleted": n})
}

func (s *Server) handleReprocessTweets(w http.ResponseWriter, r *http.Request) {
	var req struct {
		TweetIDs  []string `json:"tweet_ids"`
		ActionIDs []int64  `json:"action_ids"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	if len(req.TweetIDs) == 0 {
		writeError(w, http.StatusBadRequest, "tweet_ids is required")
		return
	}

	var tweets []*model.Tweet
	for _, id := range req.TweetIDs {
		tweet, err := s.store.GetTweet(r.Context(), id)
		if err != nil {
			s.logger.Warn("api: reprocess: tweet not found", "tweet_id", id, "error", err)
			continue
		}
		tweets = append(tweets, tweet)
	}

	if len(tweets) == 0 {
		writeError(w, http.StatusNotFound, "tweet not found")
		return
	}

	count := 0
	for _, tweet := range tweets {
		ruleIDs, _ := s.store.ListTweetRuleIDs(r.Context(), tweet.ID)
		ev := queue.TweetEvent{
			TweetID:      tweet.ID,
			Text:         tweet.Text,
			AuthorHandle: tweet.AuthorHandle,
			RuleIDs:      ruleIDs,
		}

		if len(req.ActionIDs) == 0 {
			// Chain-based dispatch — mirrors what the stream listener does.
			if err := worker.PushInitialPipelineTasks(r.Context(), s.store, s.producer, s.actionCache, "new_tweet", ev, s.logger); err != nil {
				s.logger.Error("api: reprocess chain dispatch", "tweet_id", tweet.ID, "error", err)
				continue
			}
			count++
		} else {
			// Targeted dispatch: caller explicitly named which actions to run
			// (bypass chain routing — useful for debugging a specific action).
			evBytes, _ := json.Marshal(ev)
			pushed := false
			for _, actionID := range req.ActionIDs {
				task := &queue.Task{
					TriggerType: "new_tweet",
					Event:       evBytes,
					ActionID:    actionID,
					Context:     map[string]any{},
				}
				if err := s.producer.PushTask(r.Context(), task); err != nil {
					s.logger.Error("api: reprocess push", "tweet_id", tweet.ID, "action_id", actionID, "error", err)
				} else {
					pushed = true
				}
			}
			if pushed {
				count++
			}
		}
	}

	writeJSON(w, http.StatusOK, map[string]int{"requeued": count})
}

func (s *Server) handleDeleteNoiseTweets(w http.ResponseWriter, r *http.Request) {
	n, err := s.store.DeleteNoiseTweets(r.Context())
	if err != nil {
		s.logger.Error("api: delete noise tweets", "error", err)
		writeError(w, http.StatusInternalServerError, "failed to delete noise tweets")
		return
	}
	writeJSON(w, http.StatusOK, map[string]int64{"deleted": n})
}

func (s *Server) handleDeleteExecutions(w http.ResponseWriter, r *http.Request) {
	var req struct {
		TweetIDs   []string `json:"tweet_ids"`
		ActionIDs  []int64  `json:"action_ids"`
		PipelineID *int64   `json:"pipeline_id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	actionIDs := req.ActionIDs
	if req.PipelineID != nil {
		graph, err := s.store.GetPipelineGraph(r.Context(), *req.PipelineID)
		if err != nil {
			s.logger.Error("api: delete executions: resolve pipeline graph", "pipeline_id", *req.PipelineID, "error", err)
			writeError(w, http.StatusInternalServerError, "failed to resolve pipeline actions")
			return
		}
		for _, n := range graph.Nodes {
			if n.ActionID != nil {
				actionIDs = append(actionIDs, *n.ActionID)
			}
		}
	}

	n, err := s.store.DeleteActionExecutions(r.Context(), req.TweetIDs, actionIDs)
	if err != nil {
		s.logger.Error("api: delete executions", "error", err)
		writeError(w, http.StatusInternalServerError, "failed to delete executions")
		return
	}
	writeJSON(w, http.StatusOK, map[string]int64{"deleted": n})
}
