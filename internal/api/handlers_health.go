package api

import (
	"net/http"
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

	writeJSON(w, http.StatusOK, map[string]any{
		"status": "ok",
		"stream": streamStatus,
		"models": modelNames,
	})
}
