package api

import (
	"encoding/json"
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"

	"github.com/amir-the-h/iran-watcher/internal/model"
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
		TriggerType string `json:"trigger_type"`
		PipelineID  int64  `json:"pipeline_id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid body")
		return
	}
	if req.TriggerType == "" || req.PipelineID == 0 {
		writeError(w, http.StatusBadRequest, "trigger_type and pipeline_id are required")
		return
	}
	l := &model.TriggerPipelineLink{TriggerType: req.TriggerType, PipelineID: req.PipelineID}
	if err := s.store.SaveTriggerPipelineLink(r.Context(), l); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
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
	w.WriteHeader(http.StatusNoContent)
}
