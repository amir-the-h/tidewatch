package api

import (
	"encoding/json"
	"net/http"
	"time"
)

type searchRunRequest struct {
	RuleIDs    []int64 `json:"rule_ids"`
	StartTime  string  `json:"start_time"`
	EndTime    string  `json:"end_time"`
	SinceID    string  `json:"since_id"`
	UntilID    string  `json:"until_id"`
	MaxResults int     `json:"max_results"`
	Limit      int     `json:"limit"`
}

type searchRunResponse struct {
	TweetsFetched int `json:"tweets_fetched"`
}

func (s *Server) handleSearchRun(w http.ResponseWriter, r *http.Request) {
	var req searchRunRequest
	if r.ContentLength != 0 {
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeError(w, http.StatusBadRequest, "invalid request body")
			return
		}
	}

	if s.searchScheduler == nil {
		writeError(w, http.StatusServiceUnavailable, "search not configured")
		return
	}

	// Start from the scheduler's defaults (includes TweetFields, UserFields,
	// Expansions, etc.), then apply request overrides on top.
	params := s.searchScheduler.DefaultParams()
	if req.MaxResults > 0 {
		params.MaxResults = req.MaxResults
	}
	if req.Limit > 0 {
		params.Limit = req.Limit
	}
	if req.SinceID != "" {
		params.SinceID = req.SinceID
	}
	if req.UntilID != "" {
		params.UntilID = req.UntilID
	}
	if req.StartTime != "" {
		if t, err := time.Parse(time.RFC3339, req.StartTime); err == nil {
			params.StartTime = t
		}
	}
	if req.EndTime != "" {
		if t, err := time.Parse(time.RFC3339, req.EndTime); err == nil {
			params.EndTime = t
		}
	}

	n, err := s.searchScheduler.RunWithParams(r.Context(), req.RuleIDs, params)
	if err != nil {
		s.logger.Error("api: search run", "error", err)
		writeError(w, http.StatusInternalServerError, "search run failed")
		return
	}

	writeJSON(w, http.StatusOK, searchRunResponse{TweetsFetched: n})
}
