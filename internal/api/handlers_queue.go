package api

import (
	"net/http"

	"github.com/amir-the-h/iran-watcher/internal/queue"
)

func (s *Server) handleGetQueue(w http.ResponseWriter, r *http.Request) {
	var pending int64
	if s.sizer != nil {
		pending, _ = s.sizer.Len(r.Context(), queue.QueueTasksKey)
	}

	var workers any = []any{}
	if s.pool != nil {
		workers = s.pool.Stats().Workers
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"pending": pending,
		"workers": workers,
	})
}
