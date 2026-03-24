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
	cats, err := s.store.ListCategorizations(r.Context(), store.CategorizationFilter{
		TweetID: &tweetID,
	})
	if err != nil {
		s.logger.Error("api: list categorizations for tweet", "tweet_id", id, "error", err)
		writeError(w, http.StatusInternalServerError, "failed to list categorizations")
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"tweet":           tweet,
		"categorizations": cats,
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
		TweetIDs   []string `json:"tweet_ids"`
		ModelNames []string `json:"model_names"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	var tweets []*model.Tweet

	if len(req.TweetIDs) > 0 {
		// Explicit list — fetch each and requeue regardless of categorization state.
		for _, id := range req.TweetIDs {
			tweet, err := s.store.GetTweet(r.Context(), id)
			if err != nil {
				s.logger.Warn("api: reprocess: tweet not found", "tweet_id", id, "error", err)
				continue
			}
			tweets = append(tweets, tweet)
		}
	} else {
		// Fallback: find tweets missing categorizations for the given models.
		var err error
		tweets, err = s.store.ListUncategorizedTweets(r.Context(), req.ModelNames)
		if err != nil {
			s.logger.Error("api: list uncategorized tweets", "error", err)
			writeError(w, http.StatusInternalServerError, "failed to list uncategorized tweets")
			return
		}
	}

	count := 0
	for _, tweet := range tweets {
		st := &model.StreamTweet{
			Tweet:         tweet,
			MatchingRules: nil,
		}
		if err := s.producer.Push(r.Context(), queue.QueueRaw, st); err != nil {
			s.logger.Error("api: reprocess push", "tweet_id", tweet.ID, "error", err)
			continue
		}
		count++
	}

	writeJSON(w, http.StatusOK, map[string]int{"requeued": count})
}
