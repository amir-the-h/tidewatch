package api

import (
	"net/http"

	"github.com/amir-the-h/iran-watcher/internal/queue"
)

func (s *Server) handleGetHealth(w http.ResponseWriter, r *http.Request) {
	modelNames := []string{}

	if s.registry != nil {
		pipelines := s.registry.All()
		for _, p := range pipelines {
			modelNames = append(modelNames, p.Name())
		}
	}

	streamStatus := "disconnected"
	if s.stream != nil && s.stream.Connected() {
		streamStatus = "connected"
	}

	resp := map[string]any{
		"status": "ok",
		"stream": streamStatus,
		"models": modelNames,
	}

	if s.sizer != nil {
		pending, _ := s.sizer.Len(r.Context(), queue.QueueRaw)
		failed, _ := s.sizer.Len(r.Context(), queue.QueueFailed)
		resp["queue"] = map[string]any{
			"pending": pending,
			"failed":  failed,
		}
	}

	if s.pool != nil {
		stats := s.pool.Stats()
		resp["workers"] = stats
	}

	writeJSON(w, http.StatusOK, resp)
}
