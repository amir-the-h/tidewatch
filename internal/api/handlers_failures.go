package api

import (
	"net/http"
	"strconv"

	"github.com/amir-the-h/iran-watcher/internal/queue"
)

func (s *Server) handleListFailures(w http.ResponseWriter, r *http.Request) {
	if s.failureLister == nil {
		writeError(w, http.StatusServiceUnavailable, "failure lister not available")
		return
	}

	offset, _ := strconv.ParseInt(r.URL.Query().Get("offset"), 10, 64)
	limit, _ := strconv.ParseInt(r.URL.Query().Get("limit"), 10, 64)
	if limit <= 0 {
		limit = 50
	}

	var total int64
	if s.sizer != nil {
		total, _ = s.sizer.Len(r.Context(), queue.QueueFailed)
	}

	items, err := s.failureLister.ListFailed(r.Context(), offset, limit)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"failures": items,
		"total":    total,
	})
}

func (s *Server) handleClearFailures(w http.ResponseWriter, r *http.Request) {
	if s.failureLister == nil {
		writeError(w, http.StatusServiceUnavailable, "failure lister not available")
		return
	}

	if err := s.failureLister.ClearFailed(r.Context()); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{"cleared": true})
}

func (s *Server) handleFlushQueue(w http.ResponseWriter, r *http.Request) {
	if s.failureLister == nil {
		writeError(w, http.StatusServiceUnavailable, "failure lister not available")
		return
	}

	if err := s.failureLister.FlushQueue(r.Context()); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{"flushed": true})
}
