package api

import (
	"encoding/json"
	"net/http"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"

	"github.com/go-chi/chi/v5"

	"github.com/amir-the-h/iran-watcher/internal/inference"
	"github.com/amir-the-h/iran-watcher/internal/model"
)

// repoIDPattern validates HuggingFace repo IDs: org/model format with safe characters only.
var repoIDPattern = regexp.MustCompile(`^[a-zA-Z0-9_.-]+/[a-zA-Z0-9_.-]+$`)

func (s *Server) handleListModels(w http.ResponseWriter, r *http.Request) {
	models, err := s.store.ListModels(r.Context())
	if err != nil {
		s.logger.Error("api: list models", "error", err)
		writeError(w, http.StatusInternalServerError, "failed to list models")
		return
	}
	writeJSON(w, http.StatusOK, models)
}

func (s *Server) handleCreateModel(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Name    string          `json:"name"`
		Version string          `json:"version"`
		Path    string          `json:"path"`
		Type    model.ModelType `json:"type"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if req.Name == "" {
		writeError(w, http.StatusBadRequest, "name is required")
		return
	}
	if req.Path == "" {
		writeError(w, http.StatusBadRequest, "path is required")
		return
	}

	m := &model.Model{
		Name:    req.Name,
		Version: req.Version,
		Path:    req.Path,
		Type:    req.Type,
	}

	if err := s.store.SaveModel(r.Context(), m); err != nil {
		s.logger.Error("api: save model", "error", err)
		writeError(w, http.StatusInternalServerError, "failed to save model")
		return
	}

	writeJSON(w, http.StatusCreated, m)
}

func (s *Server) handleDeleteModel(w http.ResponseWriter, r *http.Request) {
	idStr := chi.URLParam(r, "id")
	id, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid model ID")
		return
	}

	if _, err := s.store.GetModel(r.Context(), id); err != nil {
		writeError(w, http.StatusNotFound, "model not found")
		return
	}

	if err := s.store.DeleteModel(r.Context(), id); err != nil {
		s.logger.Error("api: delete model", "model_id", id, "error", err)
		writeError(w, http.StatusInternalServerError, "failed to delete model")
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handleActivateModel(w http.ResponseWriter, r *http.Request) {
	s.setModelActive(w, r, true)
}

func (s *Server) handleDeactivateModel(w http.ResponseWriter, r *http.Request) {
	s.setModelActive(w, r, false)
}

func (s *Server) setModelActive(w http.ResponseWriter, r *http.Request, active bool) {
	idStr := chi.URLParam(r, "id")
	id, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid model ID")
		return
	}

	if _, err := s.store.GetModel(r.Context(), id); err != nil {
		writeError(w, http.StatusNotFound, "model not found")
		return
	}

	if err := s.store.SetModelActive(r.Context(), id, active); err != nil {
		s.logger.Error("api: set model active", "model_id", id, "active", active, "error", err)
		writeError(w, http.StatusInternalServerError, "failed to update model")
		return
	}

	// Reload registry with current active models.
	if s.registry != nil {
		models, listErr := s.store.ListModels(r.Context())
		if listErr != nil {
			s.logger.Error("api: set model active: list models", "model_id", id, "error", listErr)
			writeError(w, http.StatusInternalServerError, "failed to list models for registry reload")
			return
		}
		activeModels := filterActive(models)
		if reloadErr := s.registry.Reload(r.Context(), activeModels, func(m *model.Model) (inference.Pipeline, error) {
			p := inference.NewONNXPipeline(m.Name)
			return p, p.Load(m.Path)
		}); reloadErr != nil {
			s.logger.Error("api: set model active: registry reload", "model_id", id, "error", reloadErr)
			writeError(w, http.StatusInternalServerError, "failed to reload model registry")
			return
		}
	}

	m, err := s.store.GetModel(r.Context(), id)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to retrieve updated model")
		return
	}
	writeJSON(w, http.StatusOK, m)
}

// filterActive returns only models with Active == true.
func filterActive(models []*model.Model) []*model.Model {
	out := make([]*model.Model, 0, len(models))
	for _, m := range models {
		if m.Active {
			out = append(out, m)
		}
	}
	return out
}

func (s *Server) handleDownloadModel(w http.ResponseWriter, r *http.Request) {
	var req struct {
		RepoID string `json:"repo_id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	if !repoIDPattern.MatchString(req.RepoID) {
		writeError(w, http.StatusBadRequest, "invalid repo_id: must match org/model with safe characters only")
		return
	}

	localDir := filepath.Join("./models", req.RepoID)
	cmd := exec.CommandContext(
		r.Context(),
		"hf", "download", req.RepoID,
		"--include", "*.onnx",
		"--local-dir", localDir,
	)

	out, err := cmd.CombinedOutput()
	if err != nil {
		s.logger.Error("api: hf download", "repo_id", req.RepoID, "output", string(out), "error", err)
		writeError(w, http.StatusInternalServerError, "model download failed")
		return
	}

	writeJSON(w, http.StatusOK, map[string]string{
		"repo_id":   req.RepoID,
		"local_dir": localDir,
		"status":    "downloaded",
	})
}
