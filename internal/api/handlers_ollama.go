package api

import (
	"encoding/json"
	"net/http"

	"github.com/amir-the-h/iran-watcher/internal/inference"
)

type pipelineBackends struct {
	Pipeline string                `json:"pipeline"`
	Backends []inference.BackendStat `json:"backends"`
}

func (s *Server) handleOllamaBackends(w http.ResponseWriter, r *http.Request) {
	all := s.registry.All()
	result := make([]pipelineBackends, 0, len(all))
	for _, p := range all {
		bs, ok := p.(inference.BackendStatter)
		if !ok {
			continue
		}
		result = append(result, pipelineBackends{
			Pipeline: p.Name(),
			Backends: bs.BackendStats(),
		})
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(result)
}
