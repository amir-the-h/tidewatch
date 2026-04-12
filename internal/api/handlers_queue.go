package api

import (
	"net/http"

	"github.com/amir-the-h/iran-watcher/internal/queue"
)

func (s *Server) handleGetQueue(w http.ResponseWriter, r *http.Request) {
	var pendingTasks, pendingOllama int64
	if s.sizer != nil {
		pendingTasks, _ = s.sizer.Len(r.Context(), queue.QueueTasksKey)
		pendingOllama, _ = s.sizer.Len(r.Context(), queue.QueueOllamaTasksKey)
	}

	var workers any = []any{}
	if s.pool != nil {
		workers = s.pool.Stats().Workers
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"pending_tasks":  pendingTasks,
		"pending_ollama": pendingOllama,
		"workers":        workers,
	})
}
