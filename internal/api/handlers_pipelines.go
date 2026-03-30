package api

import (
	"encoding/json"
	"fmt"
	"io/fs"
	"net/http"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"github.com/go-chi/chi/v5"

	"github.com/amir-the-h/iran-watcher/internal/inference"
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
	if u := r.URL.Query().Get("usecase"); u != "" {
		filter.Usecase = &u
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
	switch p.Type {
	case model.PipelineTypeONNX, model.PipelineTypeOllama, model.PipelineTypeInternal, model.PipelineTypeNotification:
		// valid
	default:
		writeError(w, http.StatusBadRequest, "invalid type: must be onnx, ollama, internal, or notification")
		return
	}
	// For Ollama pipelines: enforce usecase uniqueness and auto-fill system_prompt.
	if p.Type == model.PipelineTypeOllama {
		var cfg map[string]any
		if len(p.Config) > 0 {
			_ = json.Unmarshal(p.Config, &cfg)
		}
		if cfg == nil {
			cfg = map[string]any{}
		}
		uc, _ := cfg["usecase"].(string)
		if uc != "" && uc != "custom" {
			existing, _ := s.store.ListPipelines(r.Context(), store.PipelineFilter{Usecase: &uc})
			if len(existing) > 0 {
				writeError(w, http.StatusConflict, "a pipeline with usecase '"+uc+"' already exists (delete it first)")
				return
			}
			switch uc {
			case "digest":
				cfg["system_prompt"] = inference.OllamaDigestSystem
			case "chat":
				cfg["system_prompt"] = inference.OllamaChatSystem
			}
		}
		if data, err := json.Marshal(cfg); err == nil {
			p.Config = data
		}
	}
	if err := s.store.SavePipeline(r.Context(), &p); err != nil {
		writeError(w, http.StatusInternalServerError, "save pipeline: "+err.Error())
		return
	}
	if list, err := s.store.ListPipelines(r.Context(), store.PipelineFilter{}); err == nil {
		s.broadcastEvent("pipelines", list)
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
	s.reloadCache(r.Context())
	if list, err := s.store.ListPipelines(r.Context(), store.PipelineFilter{}); err == nil {
		s.broadcastEvent("pipelines", list)
	}
	writeJSON(w, http.StatusOK, p)
}

func (s *Server) handleDeletePipeline(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid id")
		return
	}
	pipelineID := id
	actions, err := s.store.ListActions(r.Context(), store.ActionFilter{PipelineID: &pipelineID})
	if err == nil && len(actions) > 0 {
		writeError(w, http.StatusConflict, fmt.Sprintf("pipeline is used by %d action(s) — delete those actions first", len(actions)))
		return
	}
	if err := s.store.DeletePipeline(r.Context(), id); err != nil {
		writeError(w, http.StatusInternalServerError, "delete pipeline: "+err.Error())
		return
	}
	if list, err := s.store.ListPipelines(r.Context(), store.PipelineFilter{}); err == nil {
		s.broadcastEvent("pipelines", list)
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
	if list, err := s.store.ListPipelines(r.Context(), store.PipelineFilter{}); err == nil {
		s.broadcastEvent("pipelines", list)
	}
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
	if list, err := s.store.ListPipelines(r.Context(), store.PipelineFilter{}); err == nil {
		s.broadcastEvent("pipelines", list)
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handleListPipelineHandlers(w http.ResponseWriter, r *http.Request) {
	if s.pipelineRegistry == nil {
		writeJSON(w, http.StatusOK, []string{})
		return
	}
	handlers := s.pipelineRegistry.Handlers()
	writeJSON(w, http.StatusOK, handlers)
}

type modelFileInfo struct {
	Name string `json:"name"`
	Path string `json:"path"`
}

func (s *Server) handleListModels(w http.ResponseWriter, r *http.Request) {
	if s.modelsDir == "" {
		writeJSON(w, http.StatusOK, []modelFileInfo{})
		return
	}
	var files []modelFileInfo
	_ = filepath.WalkDir(s.modelsDir, func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return nil
		}
		if !strings.HasSuffix(d.Name(), ".onnx") || strings.HasPrefix(d.Name(), ".") {
			return nil
		}
		rel, _ := filepath.Rel(s.modelsDir, p)
		name := strings.TrimSuffix(rel, ".onnx")
		files = append(files, modelFileInfo{Name: name, Path: filepath.Join(s.modelsDir, rel)})
		return nil
	})
	if files == nil {
		files = []modelFileInfo{}
	}
	sort.Slice(files, func(i, j int) bool { return files[i].Name < files[j].Name })
	writeJSON(w, http.StatusOK, files)
}
