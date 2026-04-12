package api

import (
	"net/http"

	"github.com/amir-the-h/iran-watcher/internal/queue"
	"github.com/amir-the-h/iran-watcher/internal/worker"
)

func (s *Server) handleGetQueue(w http.ResponseWriter, r *http.Request) {
	var pendingTasks, pendingOllama int64
	if s.sizer != nil {
		pendingTasks, _ = s.sizer.TotalTaskLen(r.Context())
		// Compute pending_ollama by summing LLen for all Ollama action queues.
		if s.actionCache != nil {
			for _, entry := range s.actionCache.All() {
				if worker.IsOllamaAction(s.actionCache, entry.Action.ID) {
					n, _ := s.sizer.Len(r.Context(), queue.QueueTasksKey(entry.Action.ID))
					pendingOllama += n
				}
			}
		}
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
