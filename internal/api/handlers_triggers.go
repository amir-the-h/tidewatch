package api

import "net/http"

func (s *Server) handleListTriggers(w http.ResponseWriter, r *http.Request) {
	triggers := []map[string]string{
		{"type": "new_tweet", "description": "Fires when a tweet is received from the stream"},
		{"type": "new_categorization", "description": "Fires when an ONNX/Ollama pipeline action completes"},
		{"type": "entity_matched", "description": "Fires when an entity matcher action finds a match"},
		{"type": "chain_only", "description": "Only triggered by a chain from another action — never fires directly"},
		{"type": "digest_requested", "description": "Fires when a digest job is created via POST /api/digest; action must reference an Ollama pipeline"},
	}
	writeJSON(w, http.StatusOK, triggers)
}
