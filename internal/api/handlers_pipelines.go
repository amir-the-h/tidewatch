package api

import (
	"encoding/json"
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"

	"github.com/amir-the-h/iran-watcher/internal/model"
	"github.com/amir-the-h/iran-watcher/internal/store"
)

func (s *Server) handleListPipelines(w http.ResponseWriter, r *http.Request) {
	var filter store.PipelineFilter
	if t := r.URL.Query().Get("type"); t != "" {
		pt := model.PipelineType(t)
		filter.Type = &pt
	}
	if a := r.URL.Query().Get("active"); a != "" {
		b := a == "true"
		filter.Active = &b
	}
	pipelines, err := s.store.ListPipelines(r.Context(), filter)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "list pipelines: "+err.Error())
		return
	}
	writeJSON(w, http.StatusOK, pipelines)
}

func (s *Server) handleCreatePipeline(w http.ResponseWriter, r *http.Request) {
	var p model.Pipeline
	if err := json.NewDecoder(r.Body).Decode(&p); err != nil {
		writeError(w, http.StatusBadRequest, "invalid body: "+err.Error())
		return
	}
	if p.Name == "" || p.Type == "" {
		writeError(w, http.StatusBadRequest, "name and type are required")
		return
	}
	if err := s.store.SavePipeline(r.Context(), &p); err != nil {
		writeError(w, http.StatusInternalServerError, "save pipeline: "+err.Error())
		return
	}
	writeJSON(w, http.StatusCreated, p)
}

func (s *Server) handleUpdatePipeline(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid id")
		return
	}
	var p model.Pipeline
	if err := json.NewDecoder(r.Body).Decode(&p); err != nil {
		writeError(w, http.StatusBadRequest, "invalid body: "+err.Error())
		return
	}
	p.ID = id
	if err := s.store.UpdatePipeline(r.Context(), &p); err != nil {
		writeError(w, http.StatusInternalServerError, "update pipeline: "+err.Error())
		return
	}
	writeJSON(w, http.StatusOK, p)
}

func (s *Server) handleDeletePipeline(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid id")
		return
	}
	if err := s.store.DeletePipeline(r.Context(), id); err != nil {
		writeError(w, http.StatusInternalServerError, "delete pipeline: "+err.Error())
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handleActivatePipeline(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid id")
		return
	}
	if err := s.store.SetPipelineActive(r.Context(), id, true); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	s.reloadCache(r.Context())
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handleDeactivatePipeline(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid id")
		return
	}
	if err := s.store.SetPipelineActive(r.Context(), id, false); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	s.reloadCache(r.Context())
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handleListPipelineHandlers(w http.ResponseWriter, r *http.Request) {
	handlers := s.pipelineRegistry.Handlers()
	writeJSON(w, http.StatusOK, handlers)
}
