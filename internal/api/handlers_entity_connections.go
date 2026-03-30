package api

import (
	"net/http"
)

// handleListEntityConnections GET /api/entity-connections
func (s *Server) handleListEntityConnections(w http.ResponseWriter, r *http.Request) {
	conns, err := s.store.ListEntityConnections(r.Context())
	if err != nil {
		s.logger.Error("api: list entity connections", "error", err)
		writeError(w, http.StatusInternalServerError, "failed to list entity connections")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"connections": conns})
}

// handleDeleteEntityConnections DELETE /api/entity-connections
func (s *Server) handleDeleteEntityConnections(w http.ResponseWriter, r *http.Request) {
	if err := s.store.DeleteAllEntityConnections(r.Context()); err != nil {
		s.logger.Error("api: delete entity connections", "error", err)
		writeError(w, http.StatusInternalServerError, "failed to delete entity connections")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
