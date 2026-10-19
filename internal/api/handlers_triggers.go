package api

import (
	"encoding/json"
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"

	"github.com/amir-the-h/tidewatch/internal/model"
	"github.com/amir-the-h/tidewatch/internal/store"
)

func (s *Server) handleListTriggers(w http.ResponseWriter, r *http.Request) {
	triggers := []map[string]string{
		{"type": "new_tweet",          "description": "Fires when a tweet is received from the stream"},
		{"type": "new_categorization", "description": "Fires when an ONNX/Ollama action completes"},
		{"type": "entity_matched",     "description": "Fires when an entity matcher action finds a match"},
		{"type": "digest_requested",   "description": "Fires when a digest job is created via POST /api/digest"},
		{"type": "server_started",     "description": "Fires once when the server process starts"},
		{"type": "stream_connected",   "description": "Fires when the X stream connects"},
		{"type": "digest_completed",   "description": "Fires when a digest job finishes"},
		{"type": "pipeline_finished",  "description": "Fires when all branches of a pipeline complete for a tweet"},
	}
	writeJSON(w, http.StatusOK, triggers)
}

func (s *Server) handleListTriggerPipelineLinks(w http.ResponseWriter, r *http.Request) {
	links, err := s.store.ListTriggerPipelineLinks(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if links == nil {
		links = []*model.TriggerPipelineLink{}
	}
	writeJSON(w, http.StatusOK, links)
}

func (s *Server) handleCreateTriggerPipelineLink(w http.ResponseWriter, r *http.Request) {
	var req struct {
		TriggerType      string `json:"trigger_type"`
		PipelineID       int64  `json:"pipeline_id"`
		SourcePipelineID *int64 `json:"source_pipeline_id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid body")
		return
	}
	if req.TriggerType == "" || req.PipelineID == 0 {
		writeError(w, http.StatusBadRequest, "trigger_type and pipeline_id are required")
		return
	}
	if req.TriggerType == "pipeline_finished" && (req.SourcePipelineID == nil || *req.SourcePipelineID == 0) {
		writeError(w, http.StatusBadRequest, "source_pipeline_id is required for pipeline_finished trigger")
		return
	}
	l := &model.TriggerPipelineLink{
		TriggerType:      req.TriggerType,
		PipelineID:       req.PipelineID,
		SourcePipelineID: req.SourcePipelineID,
		Active:           true,
	}
	if err := s.store.SaveTriggerPipelineLink(r.Context(), l); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if list, err := s.store.ListTriggerPipelineLinks(r.Context()); err == nil {
		s.broadcastEvent("trigger_links", list)
	}
	writeJSON(w, http.StatusCreated, l)
}

func (s *Server) handleDeleteTriggerPipelineLink(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid id")
		return
	}
	if err := s.store.DeleteTriggerPipelineLink(r.Context(), id); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handleActivateTriggerPipelineLink(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid id")
		return
	}
	if err := s.store.SetTriggerPipelineLinkActive(r.Context(), id, true); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if list, err := s.store.ListTriggerPipelineLinks(r.Context()); err == nil {
		s.broadcastEvent("trigger_links", list)
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handleDeactivateTriggerPipelineLink(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid id")
		return
	}
	if err := s.store.SetTriggerPipelineLinkActive(r.Context(), id, false); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if list, err := s.store.ListTriggerPipelineLinks(r.Context()); err == nil {
		s.broadcastEvent("trigger_links", list)
	}
	w.WriteHeader(http.StatusNoContent)
}

// handleBulkDeleteTriggerPipelineLinks DELETE /api/triggers/links
func (s *Server) handleBulkDeleteTriggerPipelineLinks(w http.ResponseWriter, r *http.Request) {
	var f store.BulkDeleteTriggerPipelineLinksFilter
	if err := json.NewDecoder(r.Body).Decode(&f); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	n, err := s.store.BulkDeleteTriggerPipelineLinks(r.Context(), f)
	if err != nil {
		s.logger.Error("api: bulk delete trigger pipeline links", "error", err)
		writeError(w, http.StatusInternalServerError, "failed to bulk delete trigger pipeline links")
		return
	}
	writeJSON(w, http.StatusOK, map[string]int64{"deleted": n})
}
