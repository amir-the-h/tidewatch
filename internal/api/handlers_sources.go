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

	src := &model.Source{
		Type:  req.Type,
		Value: req.Value,
	}

	if err := s.store.SaveSource(r.Context(), src); err != nil {
		s.logger.Error("api: save source", "error", err)
		writeError(w, http.StatusInternalServerError, "failed to save source")
		return
	}

	xRuleID, err := s.rules.AddRule(r.Context(), req.Value, req.Value)
	if err != nil {
		s.logger.Error("api: add rule", "source_id", src.ID, "error", err)
		writeError(w, http.StatusInternalServerError, "failed to add stream rule")
		return
	}

	if err := s.store.UpdateSourceXRuleID(r.Context(), src.ID, xRuleID); err != nil {
		s.logger.Error("api: update source xrule id", "source_id", src.ID, "error", err)
		writeError(w, http.StatusInternalServerError, "failed to update source rule ID")
		return
	}
	src.XRuleID = xRuleID

	writeJSON(w, http.StatusCreated, src)
}

func (s *Server) handleDeleteSource(w http.ResponseWriter, r *http.Request) {
	idStr := chi.URLParam(r, "id")
	id, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid source ID")
		return
	}

	src, err := s.store.GetSource(r.Context(), id)
	if err != nil {
		writeError(w, http.StatusNotFound, "source not found")
		return
	}

	if src.XRuleID != "" {
		if err := s.rules.DeleteRule(r.Context(), src.XRuleID); err != nil {
			s.logger.Error("api: delete rule", "source_id", id, "xrule_id", src.XRuleID, "error", err)
			writeError(w, http.StatusInternalServerError, "failed to delete stream rule")
			return
		}
	}

	if err := s.store.DeleteSource(r.Context(), id); err != nil {
		s.logger.Error("api: delete source", "source_id", id, "error", err)
		writeError(w, http.StatusInternalServerError, "failed to delete source")
		return
	}

	w.WriteHeader(http.StatusNoContent)
}
