package api

import (
	"net/http"
	"time"

	"github.com/amir-the-h/iran-watcher/internal/store"
)

func (s *Server) handleListCategorizations(w http.ResponseWriter, r *http.Request) {
	filter := store.CategorizationFilter{}

	q := r.URL.Query()

	if v := q.Get("model_name"); v != "" {
		filter.ModelName = &v
	}
	if v := q.Get("tweet_id"); v != "" {
		filter.TweetID = &v
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

	cats, err := s.store.ListCategorizations(r.Context(), filter)
	if err != nil {
		s.logger.Error("api: list categorizations", "error", err)
		writeError(w, http.StatusInternalServerError, "failed to list categorizations")
		return
	}

	writeJSON(w, http.StatusOK, cats)
}
