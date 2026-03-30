package api

import (
	"net/http"
	"strconv"
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

// handleGetEntityConnectionSubgraph GET /api/entity-connections/subgraph
func (s *Server) handleGetEntityConnectionSubgraph(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()

	entityIDStr := q.Get("entity_id")
	if entityIDStr == "" {
		writeError(w, http.StatusBadRequest, "entity_id is required")
		return
	}
	entityID, err := strconv.ParseInt(entityIDStr, 10, 64)
	if err != nil || entityID <= 0 {
		writeError(w, http.StatusBadRequest, "entity_id must be a positive integer")
		return
	}

	depth := 2
	if ds := q.Get("depth"); ds != "" {
		if d, err := strconv.Atoi(ds); err == nil {
			if d < 1 {
				d = 1
			}
			if d > 5 {
				d = 5
			}
			depth = d
		}
	}

	minWeight := 1
	if mws := q.Get("min_weight"); mws != "" {
		if mw, err := strconv.Atoi(mws); err == nil {
			if mw < 1 {
				mw = 1
			}
			minWeight = mw
		}
	}

	conns, err := s.store.GetEntityConnectionSubgraph(r.Context(), entityID, depth, minWeight)
	if err != nil {
		s.logger.Error("api: get entity connection subgraph", "error", err)
		writeError(w, http.StatusInternalServerError, "failed to get entity connection subgraph")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"connections": conns})
}
