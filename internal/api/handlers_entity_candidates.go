package api

import (
	"encoding/json"
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"

	"github.com/amir-the-h/iran-watcher/internal/model"
	"github.com/amir-the-h/iran-watcher/internal/store"
)

// handleListEntityCandidates GET /api/entity-candidates
func (s *Server) handleListEntityCandidates(w http.ResponseWriter, r *http.Request) {
	filter := store.EntityCandidateFilter{Page: 1, PerPage: 50}
	q := r.URL.Query()

	if v := q.Get("status"); v != "" {
		filter.Status = &v
	}
	if v := q.Get("type"); v != "" {
		filter.Type = &v
	}
	if v := q.Get("page"); v != "" {
		if p, err := strconv.Atoi(v); err == nil && p > 0 {
			filter.Page = p
		}
	}
	if v := q.Get("per_page"); v != "" {
		if p, err := strconv.Atoi(v); err == nil && p > 0 {
			filter.PerPage = p
		}
	}

	candidates, err := s.store.ListEntityCandidates(r.Context(), filter)
	if err != nil {
		s.logger.Error("api: list entity candidates", "error", err)
		writeError(w, http.StatusInternalServerError, "failed to list entity candidates")
		return
	}

	total, err := s.store.CountEntityCandidates(r.Context(), store.EntityCandidateFilter{
		Status: filter.Status,
		Type:   filter.Type,
	})
	if err != nil {
		s.logger.Error("api: count entity candidates", "error", err)
		writeError(w, http.StatusInternalServerError, "failed to count entity candidates")
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"candidates": candidates,
		"total":      total,
		"page":       filter.Page,
		"per_page":   filter.PerPage,
	})
}

// handleGetEntityCandidate GET /api/entity-candidates/{id}
func (s *Server) handleGetEntityCandidate(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid id")
		return
	}
	c, err := s.store.GetEntityCandidate(r.Context(), id)
	if err != nil {
		s.logger.Error("api: get entity candidate", "id", id, "error", err)
		writeError(w, http.StatusNotFound, "entity candidate not found")
		return
	}
	writeJSON(w, http.StatusOK, c)
}

// handlePromoteEntityCandidate POST /api/entity-candidates/{id}/promote
func (s *Server) handlePromoteEntityCandidate(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid id")
		return
	}
	c, err := s.store.GetEntityCandidate(r.Context(), id)
	if err != nil {
		writeError(w, http.StatusNotFound, "entity candidate not found")
		return
	}
	if c.Status != "pending" {
		writeError(w, http.StatusBadRequest, "candidate is not pending")
		return
	}

	e := &model.TrackedEntity{
		Name:    c.Word,
		Type:    c.Type,
		Aliases: []string{},
		Active:  true,
	}
	if err := s.store.SaveTrackedEntity(r.Context(), e); err != nil {
		s.logger.Error("api: promote candidate: save entity", "id", id, "error", err)
		writeError(w, http.StatusInternalServerError, "failed to create tracked entity (name may already exist)")
		return
	}
	if err := s.store.UpdateEntityCandidateStatus(r.Context(), id, "promoted", &e.ID, false); err != nil {
		s.logger.Error("api: promote candidate: update status", "id", id, "error", err)
		writeError(w, http.StatusInternalServerError, "failed to update candidate status")
		return
	}
	writeJSON(w, http.StatusOK, e)
}

// handleRejectEntityCandidate POST /api/entity-candidates/{id}/reject
func (s *Server) handleRejectEntityCandidate(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid id")
		return
	}
	if err := s.store.UpdateEntityCandidateStatus(r.Context(), id, "rejected", nil, false); err != nil {
		s.logger.Error("api: reject candidate", "id", id, "error", err)
		writeError(w, http.StatusInternalServerError, "failed to reject candidate")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// handleMergeEntityCandidate POST /api/entity-candidates/{id}/merge
// Body: {"entity_id": N}
func (s *Server) handleMergeEntityCandidate(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid id")
		return
	}
	var body struct {
		EntityID int64 `json:"entity_id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON")
		return
	}
	if body.EntityID == 0 {
		writeError(w, http.StatusBadRequest, "entity_id is required")
		return
	}

	candidate, err := s.store.GetEntityCandidate(r.Context(), id)
	if err != nil {
		writeError(w, http.StatusNotFound, "entity candidate not found")
		return
	}

	target, err := s.store.GetTrackedEntity(r.Context(), body.EntityID)
	if err != nil {
		writeError(w, http.StatusNotFound, "target tracked entity not found")
		return
	}

	// Append candidate word as alias if not already present.
	alreadyAlias := false
	for _, a := range target.Aliases {
		if a == candidate.Word {
			alreadyAlias = true
			break
		}
	}
	if !alreadyAlias {
		target.Aliases = append(target.Aliases, candidate.Word)
		if err := s.store.UpdateTrackedEntity(r.Context(), target); err != nil {
			s.logger.Error("api: merge candidate: update entity", "entity_id", body.EntityID, "error", err)
			writeError(w, http.StatusInternalServerError, "failed to update entity aliases")
			return
		}
	}

	if err := s.store.UpdateEntityCandidateStatus(r.Context(), id, "promoted", &body.EntityID, false); err != nil {
		s.logger.Error("api: merge candidate: update status", "id", id, "error", err)
		writeError(w, http.StatusInternalServerError, "failed to update candidate status")
		return
	}

	writeJSON(w, http.StatusOK, target)
}

// handleDeleteEntityCandidate DELETE /api/entity-candidates/{id}
func (s *Server) handleDeleteEntityCandidate(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid id")
		return
	}
	if err := s.store.DeleteEntityCandidate(r.Context(), id); err != nil {
		s.logger.Error("api: delete entity candidate", "id", id, "error", err)
		writeError(w, http.StatusInternalServerError, "failed to delete entity candidate")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
