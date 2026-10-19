package api

import (
	"net/http"

	"github.com/amir-the-h/tidewatch/internal/queue"
	"github.com/amir-the-h/tidewatch/internal/worker"
)

func (s *Server) handleGetQueue(w http.ResponseWriter, r *http.Request) {
	var pendingTasks, pendingOllama int64
	if s.sizer != nil {
		total, _ := s.sizer.TotalTaskLen(r.Context())
		// Compute pending_ollama by summing LLen for all Ollama action queues.
		if s.actionCache != nil {
			for _, entry := range s.actionCache.All() {
				if worker.IsOllamaAction(s.actionCache, entry.Action.ID) {
					n, _ := s.sizer.Len(r.Context(), queue.QueueTasksKey(entry.Action.ID))
					pendingOllama += n
				}
			}
		}
		// pending_tasks shows only non-Ollama tasks so the two counts are
		// complementary (non-Ollama + Ollama = total), matching the old UX.
		pendingTasks = total - pendingOllama
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
