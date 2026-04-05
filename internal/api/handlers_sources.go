package api

import (
	"encoding/json"
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"

	"github.com/amir-the-h/iran-watcher/internal/model"
	"github.com/amir-the-h/iran-watcher/internal/store"
)

func (s *Server) handleListSources(w http.ResponseWriter, r *http.Request) {
	filter := store.SourceFilter{}

	if t := r.URL.Query().Get("type"); t != "" {
		st := model.SourceType(t)
		filter.Type = &st
	}
	if c := r.URL.Query().Get("class"); c != "" {
		sc := model.SourceClass(c)
		filter.Class = &sc
	}
	if a := r.URL.Query().Get("active"); a != "" {
		active, err := strconv.ParseBool(a)
		if err != nil {
			writeError(w, http.StatusBadRequest, "invalid active parameter")
			return
		}
		filter.Active = &active
	}

	sources, err := s.store.ListSources(r.Context(), filter)
	if err != nil {
		s.logger.Error("api: list sources", "error", err)
		writeError(w, http.StatusInternalServerError, "failed to list sources")
		return
	}
	writeJSON(w, http.StatusOK, sources)
}

func (s *Server) handleCreateSource(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Name  string           `json:"name"`
		Type  model.SourceType `json:"type"`
		Value string           `json:"value"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if req.Type == "" {
		writeError(w, http.StatusBadRequest, "type is required")
		return
	}
	if req.Value == "" {
		writeError(w, http.StatusBadRequest, "value is required")
		return
	}

	info, ok := model.SourceTypeMap[req.Type]
	if !ok {
		writeError(w, http.StatusBadRequest, "unknown source type")
		return
	}

	if req.Name == "" {
		req.Name = req.Value
	}

	src := &model.Source{
		Name:     req.Name,
		Type:     req.Type,
		Class:    info.Class,
		Operator: info.Operator,
		Value:    req.Value,
		Active:   true,
	}

	if err := s.store.SaveSource(r.Context(), src); err != nil {
		s.logger.Error("api: save source", "error", err)
		writeError(w, http.StatusInternalServerError, "failed to save source")
		return
	}

	if list, err := s.store.ListSources(r.Context(), store.SourceFilter{}); err == nil {
		s.broadcastEvent("sources", list)
	}
	writeJSON(w, http.StatusCreated, src)
}

func (s *Server) handleUpdateSource(w http.ResponseWriter, r *http.Request) {
	idStr := chi.URLParam(r, "id")
	id, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid source ID")
		return
	}

	existing, err := s.store.GetSource(r.Context(), id)
	if err != nil {
		writeError(w, http.StatusNotFound, "source not found")
		return
	}

	var req struct {
		Name  string           `json:"name"`
		Type  model.SourceType `json:"type"`
		Value string           `json:"value"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	if req.Type != "" && req.Type != existing.Type {
		info, ok := model.SourceTypeMap[req.Type]
		if !ok {
			writeError(w, http.StatusBadRequest, "unknown source type")
			return
		}
		existing.Type = req.Type
		existing.Class = info.Class
		existing.Operator = info.Operator
	}
	if req.Name != "" {
		existing.Name = req.Name
	}
	if req.Value != "" {
		existing.Value = req.Value
	}

	if err := s.store.UpdateSource(r.Context(), existing); err != nil {
		s.logger.Error("api: update source", "source_id", id, "error", err)
		writeError(w, http.StatusInternalServerError, "failed to update source")
		return
	}

	if list, err := s.store.ListSources(r.Context(), store.SourceFilter{}); err == nil {
		s.broadcastEvent("sources", list)
	}
	writeJSON(w, http.StatusOK, existing)
}

func (s *Server) handleDeleteSource(w http.ResponseWriter, r *http.Request) {
	idStr := chi.URLParam(r, "id")
	id, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid source ID")
		return
	}

	if _, err := s.store.GetSource(r.Context(), id); err != nil {
		writeError(w, http.StatusNotFound, "source not found")
		return
	}

	count, err := s.store.RuleRefCount(r.Context(), id)
	if err != nil {
		s.logger.Error("api: rule ref count", "source_id", id, "error", err)
		writeError(w, http.StatusInternalServerError, "failed to check rule references")
		return
	}
	if count > 0 {
		writeError(w, http.StatusConflict, "source is referenced by one or more rules; remove those rules first")
		return
	}

	if err := s.store.DeleteSource(r.Context(), id); err != nil {
		s.logger.Error("api: delete source", "source_id", id, "error", err)
		writeError(w, http.StatusInternalServerError, "failed to delete source")
		return
	}

	if list, err := s.store.ListSources(r.Context(), store.SourceFilter{}); err == nil {
		s.broadcastEvent("sources", list)
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handleDeactivateSource(w http.ResponseWriter, r *http.Request) {
	idStr := chi.URLParam(r, "id")
	id, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid source ID")
		return
	}

	if _, err := s.store.GetSource(r.Context(), id); err != nil {
		writeError(w, http.StatusNotFound, "source not found")
		return
	}

	count, err := s.store.ActiveRuleRefCount(r.Context(), id)
	if err != nil {
		s.logger.Error("api: active rule ref count", "source_id", id, "error", err)
		writeError(w, http.StatusInternalServerError, "failed to check active rule references")
		return
	}
	if count > 0 {
		writeError(w, http.StatusConflict, "source is referenced by one or more active rules; deactivate those rules first")
		return
	}

	if err := s.store.SetSourceActive(r.Context(), id, false); err != nil {
		s.logger.Error("api: deactivate source", "source_id", id, "error", err)
		writeError(w, http.StatusInternalServerError, "failed to deactivate source")
		return
	}

	if list, err := s.store.ListSources(r.Context(), store.SourceFilter{}); err == nil {
		s.broadcastEvent("sources", list)
	}
	w.WriteHeader(http.StatusNoContent)
}

// handleBulkDeleteSources DELETE /api/sources
func (s *Server) handleBulkDeleteSources(w http.ResponseWriter, r *http.Request) {
	var f store.BulkDeleteSourcesFilter
	if err := json.NewDecoder(r.Body).Decode(&f); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	n, err := s.store.BulkDeleteSources(r.Context(), f)
	if err != nil {
		s.logger.Error("api: bulk delete sources", "error", err)
		writeError(w, http.StatusInternalServerError, "failed to bulk delete sources")
		return
	}
	writeJSON(w, http.StatusOK, map[string]int64{"deleted": n})
}
