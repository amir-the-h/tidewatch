package api

import (
	"encoding/json"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/amir-the-h/iran-watcher/internal/model"
)

// handleSearchRun creates a background search job and returns immediately.
// Poll GET /api/search/{id} for status and result.
func (s *Server) handleSearchRun(w http.ResponseWriter, r *http.Request) {
	if s.searchScheduler == nil {
		writeError(w, http.StatusServiceUnavailable, "search not configured")
		return
	}

	var req model.SearchJobParams
	if r.ContentLength != 0 {
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeError(w, http.StatusBadRequest, "invalid request body")
			return
		}
	}

	job := &model.SearchJob{
		ID:        uuid.New().String(),
		Status:    model.SearchJobStatusPending,
		Params:    req,
		CreatedAt: time.Now().UTC(),
	}
	if err := s.store.SaveSearchJob(r.Context(), job); err != nil {
		s.logger.Error("api: search: save job", "error", err)
		writeError(w, http.StatusInternalServerError, "failed to create search job")
		return
	}

	go s.runSearchJob(job)

	writeJSON(w, http.StatusAccepted, job)
}

// runSearchJob executes the search in the background and persists the result.
func (s *Server) runSearchJob(job *model.SearchJob) {
	ctx := s.ctx

	job.Status = model.SearchJobStatusRunning
	if err := s.store.UpdateSearchJob(ctx, job); err != nil {
		s.logger.Error("api: search job: mark running", "job_id", job.ID, "error", err)
	}

	params := s.searchScheduler.DefaultParams()
	req := job.Params
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

	n, err := s.searchScheduler.RunWithParams(ctx, req.RuleIDs, params)

	now := time.Now().UTC()
	job.CompletedAt = &now

	if err != nil {
		s.logger.Error("api: search job: run", "job_id", job.ID, "error", err)
		job.Status = model.SearchJobStatusError
		job.Error = "search failed: " + err.Error()
	} else {
		job.Status = model.SearchJobStatusDone
		job.TweetsFetched = n
	}

	if err := s.store.UpdateSearchJob(ctx, job); err != nil {
		s.logger.Error("api: search job: save result", "job_id", job.ID, "error", err)
	}
}

// handleGetSearchJob returns the status and result of a search job.
func (s *Server) handleGetSearchJob(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	job, err := s.store.GetSearchJob(r.Context(), id)
	if err != nil {
		writeError(w, http.StatusNotFound, "search job not found")
		return
	}
	writeJSON(w, http.StatusOK, job)
}

// handleListSearchJobs returns recent search jobs.
func (s *Server) handleListSearchJobs(w http.ResponseWriter, r *http.Request) {
	limit := parseIntParam(r.URL.Query().Get("limit"), 20)
	jobs, err := s.store.ListSearchJobs(r.Context(), limit)
	if err != nil {
		s.logger.Error("api: list search jobs", "error", err)
		writeError(w, http.StatusInternalServerError, "failed to list search jobs")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"jobs": jobs})
}

// handleDeleteSearchJob deletes a search job by ID.
func (s *Server) handleDeleteSearchJob(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	if err := s.store.DeleteSearchJob(r.Context(), id); err != nil {
		writeError(w, http.StatusNotFound, "search job not found")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
