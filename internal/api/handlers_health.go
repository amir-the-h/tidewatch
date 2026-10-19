package api

import (
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/amir-the-h/tidewatch/internal/queue"
)

func (s *Server) handleFlushWorkerBuffer(w http.ResponseWriter, r *http.Request) {
	flusher, ok := s.pool.(workerBufferFlusher)
	if !ok {
		writeError(w, http.StatusServiceUnavailable, "worker buffer flush not available")
		return
	}
	n := flusher.Flush()
	writeJSON(w, http.StatusOK, map[string]any{"flushed": n})
}

func (s *Server) handleFlushPipelineBuffer(w http.ResponseWriter, r *http.Request) {
	flusher, ok := s.pool.(workerBufferFlusher)
	if !ok {
		writeError(w, http.StatusServiceUnavailable, "worker buffer flush not available")
		return
	}
	name := chi.URLParam(r, "pipeline")
	if name == "" {
		writeError(w, http.StatusBadRequest, "pipeline name required")
		return
	}
	n := flusher.FlushPipeline(name)
	writeJSON(w, http.StatusOK, map[string]any{"pipeline": name, "flushed": n})
}

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
		pending, _ := s.sizer.TotalTaskLen(r.Context())
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
