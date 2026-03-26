package api

import (
	"encoding/json"
	"net/http"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/amir-the-h/iran-watcher/internal/model"
	"github.com/amir-the-h/iran-watcher/internal/store"
)

// handleListTrackedEntities GET /api/tracked-entities
func (s *Server) handleListTrackedEntities(w http.ResponseWriter, r *http.Request) {
	filter := store.TrackedEntityFilter{}
	q := r.URL.Query()
	if v := q.Get("type"); v != "" {
		filter.Type = &v
	}
	if v := q.Get("active"); v != "" {
		b := v == "true" || v == "1"
		filter.Active = &b
	}

	entities, err := s.store.ListTrackedEntities(r.Context(), filter)
	if err != nil {
		s.logger.Error("api: list tracked entities", "error", err)
		writeError(w, http.StatusInternalServerError, "failed to list tracked entities")
		return
	}
	writeJSON(w, http.StatusOK, entities)
}

// handleCreateTrackedEntity POST /api/tracked-entities
func (s *Server) handleCreateTrackedEntity(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Name    string   `json:"name"`
		Type    string   `json:"type"`
		Aliases []string `json:"aliases"`
		Notes   string   `json:"notes"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON")
		return
	}
	if body.Name == "" {
		writeError(w, http.StatusBadRequest, "name is required")
		return
	}
	if body.Aliases == nil {
		body.Aliases = []string{}
	}

	e := &model.TrackedEntity{
		Name:    body.Name,
		Type:    body.Type,
		Aliases: body.Aliases,
		Notes:   body.Notes,
		Active:  true,
	}
	if err := s.store.SaveTrackedEntity(r.Context(), e); err != nil {
		s.logger.Error("api: create tracked entity", "error", err)
		writeError(w, http.StatusInternalServerError, "failed to create tracked entity")
		return
	}
	writeJSON(w, http.StatusCreated, e)
}

// handleGetTrackedEntity GET /api/tracked-entities/{id}
func (s *Server) handleGetTrackedEntity(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid id")
		return
	}
	e, err := s.store.GetTrackedEntity(r.Context(), id)
	if err != nil {
		s.logger.Error("api: get tracked entity", "id", id, "error", err)
		writeError(w, http.StatusNotFound, "tracked entity not found")
		return
	}
	writeJSON(w, http.StatusOK, e)
}

// handleUpdateTrackedEntity PUT /api/tracked-entities/{id}
func (s *Server) handleUpdateTrackedEntity(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid id")
		return
	}
	e, err := s.store.GetTrackedEntity(r.Context(), id)
	if err != nil {
		writeError(w, http.StatusNotFound, "tracked entity not found")
		return
	}

	var body struct {
		Name    string   `json:"name"`
		Type    string   `json:"type"`
		Aliases []string `json:"aliases"`
		Notes   string   `json:"notes"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON")
		return
	}
	if body.Name != "" {
		e.Name = body.Name
	}
	e.Type = body.Type
	if body.Aliases != nil {
		e.Aliases = body.Aliases
	}
	e.Notes = body.Notes

	if err := s.store.UpdateTrackedEntity(r.Context(), e); err != nil {
		s.logger.Error("api: update tracked entity", "id", id, "error", err)
		writeError(w, http.StatusInternalServerError, "failed to update tracked entity")
		return
	}
	writeJSON(w, http.StatusOK, e)
}

// handleDeleteTrackedEntity DELETE /api/tracked-entities/{id}
func (s *Server) handleDeleteTrackedEntity(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid id")
		return
	}
	if err := s.store.DeleteTrackedEntity(r.Context(), id); err != nil {
		s.logger.Error("api: delete tracked entity", "id", id, "error", err)
		writeError(w, http.StatusInternalServerError, "failed to delete tracked entity")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// handleActivateTrackedEntity POST /api/tracked-entities/{id}/activate
func (s *Server) handleActivateTrackedEntity(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid id")
		return
	}
	if err := s.store.SetTrackedEntityActive(r.Context(), id, true); err != nil {
		s.logger.Error("api: activate tracked entity", "id", id, "error", err)
		writeError(w, http.StatusInternalServerError, "failed to activate tracked entity")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// handleDeactivateTrackedEntity POST /api/tracked-entities/{id}/deactivate
func (s *Server) handleDeactivateTrackedEntity(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid id")
		return
	}
	if err := s.store.SetTrackedEntityActive(r.Context(), id, false); err != nil {
		s.logger.Error("api: deactivate tracked entity", "id", id, "error", err)
		writeError(w, http.StatusInternalServerError, "failed to deactivate tracked entity")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// handleListEntityAliasSuggestions GET /api/tracked-entities/{id}/alias-suggestions
func (s *Server) handleListEntityAliasSuggestions(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid id")
		return
	}
	suggestions, err := s.store.ListNERSuggestionsForEntity(r.Context(), id)
	if err != nil {
		s.logger.Error("api: list entity alias suggestions", "id", id, "error", err)
		writeError(w, http.StatusInternalServerError, "failed to list alias suggestions")
		return
	}
	if suggestions == nil {
		suggestions = []string{}
	}
	writeJSON(w, http.StatusOK, suggestions)
}

// handleSearchAliasSuggestions GET /api/alias-suggestions?name={name}&type={type}
func (s *Server) handleSearchAliasSuggestions(w http.ResponseWriter, r *http.Request) {
	name := r.URL.Query().Get("name")
	if name == "" {
		writeJSON(w, http.StatusOK, []string{})
		return
	}
	entityType := r.URL.Query().Get("type")
	suggestions, err := s.store.SearchNERSuggestions(r.Context(), name, entityType)
	if err != nil {
		s.logger.Error("api: search alias suggestions", "name", name, "error", err)
		writeError(w, http.StatusInternalServerError, "failed to search alias suggestions")
		return
	}
	if suggestions == nil {
		suggestions = []string{}
	}
	writeJSON(w, http.StatusOK, suggestions)
}

// handleListEntityTracks GET /api/tracked-entities/{id}/tracks
func (s *Server) handleListEntityTracks(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid id")
		return
	}

	filter := store.EntityTrackFilter{EntityID: &id}
	q := r.URL.Query()

	if v := q.Get("from"); v != "" {
		t, err := time.Parse(time.RFC3339, v)
		if err != nil {
			writeError(w, http.StatusBadRequest, "invalid from (use RFC3339)")
			return
		}
		filter.From = &t
	}
	if v := q.Get("to"); v != "" {
		t, err := time.Parse(time.RFC3339, v)
		if err != nil {
			writeError(w, http.StatusBadRequest, "invalid to (use RFC3339)")
			return
		}
		filter.To = &t
	}
	if v := q.Get("page"); v != "" {
		if p, err := strconv.Atoi(v); err == nil && p > 0 {
			filter.Page = p
		}
	} else {
		filter.Page = 1
	}
	if v := q.Get("per_page"); v != "" {
		if p, err := strconv.Atoi(v); err == nil && p > 0 {
			filter.PerPage = p
		}
	} else {
		filter.PerPage = 50
	}

	tracks, err := s.store.ListEntityTracks(r.Context(), filter)
	if err != nil {
		s.logger.Error("api: list entity tracks", "entity_id", id, "error", err)
		writeError(w, http.StatusInternalServerError, "failed to list entity tracks")
		return
	}

	total, err := s.store.CountEntityTracks(r.Context(), store.EntityTrackFilter{EntityID: &id})
	if err != nil {
		s.logger.Error("api: count entity tracks", "entity_id", id, "error", err)
		writeError(w, http.StatusInternalServerError, "failed to count entity tracks")
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"tracks":     tracks,
		"total":      total,
		"page":       filter.Page,
		"per_page":   filter.PerPage,
	})
}
