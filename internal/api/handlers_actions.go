package api

import (
	"context"
	"encoding/json"
	"net/http"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/amir-the-h/tidewatch/internal/inference"
	"github.com/amir-the-h/tidewatch/internal/model"
	"github.com/amir-the-h/tidewatch/internal/store"
)

func (s *Server) handleListActions(w http.ResponseWriter, r *http.Request) {
	filter := store.ActionFilter{}
	if v := r.URL.Query().Get("active"); v != "" {
		b := v == "true"
		filter.Active = &b
	}
	if v := r.URL.Query().Get("type"); v != "" {
		t := model.ActionType(v)
		filter.Type = &t
	}
	actions, err := s.store.ListActions(r.Context(), filter)
	if err != nil {
		s.logger.Error("api: list actions", "error", err)
		writeError(w, http.StatusInternalServerError, "failed to list actions")
		return
	}
	if actions == nil {
		actions = []*model.Action{}
	}
	writeJSON(w, http.StatusOK, actions)
}

func (s *Server) handleGetAction(w http.ResponseWriter, r *http.Request) {
	id, err := parseID(r)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid action ID")
		return
	}
	a, err := s.store.GetAction(r.Context(), id)
	if err != nil {
		writeError(w, http.StatusNotFound, "action not found")
		return
	}
	writeJSON(w, http.StatusOK, a)
}

func (s *Server) handleCreateAction(w http.ResponseWriter, r *http.Request) {
	var a model.Action
	if err := json.NewDecoder(r.Body).Decode(&a); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if a.Name == "" {
		writeError(w, http.StatusBadRequest, "name is required")
		return
	}
	if a.Type == "" {
		writeError(w, http.StatusBadRequest, "type is required")
		return
	}
	a.Active = true // new actions are active by default
	switch a.Type {
	case model.ActionTypeONNX, model.ActionTypeOllama, model.ActionTypeInternal, model.ActionTypeNotification:
		// valid
	default:
		writeError(w, http.StatusBadRequest, "invalid type: must be onnx, ollama, internal, or notification")
		return
	}
	// For Ollama actions: enforce usecase uniqueness and auto-fill system_prompt.
	if a.Type == model.ActionTypeOllama {
		var cfg map[string]any
		if len(a.Config) > 0 {
			_ = json.Unmarshal(a.Config, &cfg)
		}
		if cfg == nil {
			cfg = map[string]any{}
		}
		uc, _ := cfg["usecase"].(string)
		if uc != "" && uc != "custom" && uc != "chat" {
			existing, _ := s.store.ListActions(r.Context(), store.ActionFilter{})
			for _, ex := range existing {
				var exCfg map[string]any
				if len(ex.Config) > 0 {
					_ = json.Unmarshal(ex.Config, &exCfg)
				}
				if exCfg != nil {
					if exUC, _ := exCfg["usecase"].(string); exUC == uc {
						writeError(w, http.StatusConflict, "an action with usecase '"+uc+"' already exists (delete it first)")
						return
					}
				}
			}
		}
		if _, hasPrompt := cfg["system_prompt"]; !hasPrompt {
			switch uc {
			case "digest":
				cfg["system_prompt"] = inference.OllamaDigestSystem
			case "chat":
				cfg["system_prompt"] = inference.OllamaChatSystem
			}
		}
		if data, err := json.Marshal(cfg); err == nil {
			a.Config = data
		}
	}
	if err := s.store.SaveAction(r.Context(), &a); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if err := s.actionCache.Load(r.Context()); err != nil {
		s.logger.Warn("api: reload action cache after create", "error", err)
	}
	if list, err := s.store.ListActions(r.Context(), store.ActionFilter{}); err == nil {
		s.broadcastEvent("actions", list)
	}
	writeJSON(w, http.StatusCreated, a)
}

func (s *Server) handleUpdateAction(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid id")
		return
	}
	var req model.Action
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	a, err := s.store.GetAction(r.Context(), id)
	if err != nil {
		writeError(w, http.StatusNotFound, "action not found")
		return
	}
	if req.Name != "" {
		a.Name = req.Name
	}
	if req.Type != "" {
		a.Type = req.Type
	}
	if req.Config != nil {
		a.Config = req.Config
	}
	if err := s.store.UpdateAction(r.Context(), a); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if err := s.actionCache.Load(r.Context()); err != nil {
		s.logger.Warn("api: reload action cache after update", "error", err)
	}
	if list, err := s.store.ListActions(r.Context(), store.ActionFilter{}); err == nil {
		s.broadcastEvent("actions", list)
	}
	writeJSON(w, http.StatusOK, a)
}

func (s *Server) handleDeleteAction(w http.ResponseWriter, r *http.Request) {
	id, err := parseID(r)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid action ID")
		return
	}

	if _, err := s.store.GetAction(r.Context(), id); err != nil {
		writeError(w, http.StatusNotFound, "action not found")
		return
	}

	if err := s.store.DeleteAction(r.Context(), id); err != nil {
		s.logger.Error("api: delete action", "action_id", id, "error", err)
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	if err := s.reloadActionCache(r.Context()); err != nil {
		s.logger.Error("api: delete action: reload cache", "error", err)
	}

	if list, err := s.store.ListActions(r.Context(), store.ActionFilter{}); err == nil {
		s.broadcastEvent("actions", list)
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handleActivateAction(w http.ResponseWriter, r *http.Request) {
	s.setActionActive(w, r, true)
}

func (s *Server) handleDeactivateAction(w http.ResponseWriter, r *http.Request) {
	s.setActionActive(w, r, false)
}

func (s *Server) setActionActive(w http.ResponseWriter, r *http.Request, active bool) {
	id, err := parseID(r)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid action ID")
		return
	}

	if _, err := s.store.GetAction(r.Context(), id); err != nil {
		writeError(w, http.StatusNotFound, "action not found")
		return
	}

	if err := s.store.SetActionActive(r.Context(), id, active); err != nil {
		s.logger.Error("api: set action active", "action_id", id, "active", active, "error", err)
		writeError(w, http.StatusInternalServerError, "failed to update action")
		return
	}

	if err := s.reloadActionCache(r.Context()); err != nil {
		s.logger.Error("api: set action active: reload cache", "action_id", id, "error", err)
	}

	a, err := s.store.GetAction(r.Context(), id)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to retrieve updated action")
		return
	}
	if list, err := s.store.ListActions(r.Context(), store.ActionFilter{}); err == nil {
		s.broadcastEvent("actions", list)
	}
	writeJSON(w, http.StatusOK, a)
}

func (s *Server) handleListActionExecutions(w http.ResponseWriter, r *http.Request) {
	id, err := parseID(r)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid action ID")
		return
	}

	filter := store.ExecutionFilter{ActionID: &id}
	applyExecutionQueryParams(r, &filter)

	execs, err := s.store.ListActionExecutions(r.Context(), filter)
	if err != nil {
		s.logger.Error("api: list action executions", "action_id", id, "error", err)
		writeError(w, http.StatusInternalServerError, "failed to list executions")
		return
	}
	writeJSON(w, http.StatusOK, execs)
}

func (s *Server) handleListAllExecutions(w http.ResponseWriter, r *http.Request) {
	filter := store.ExecutionFilter{}
	applyExecutionQueryParams(r, &filter)

	if v := r.URL.Query().Get("signal_id"); v != "" {
		if n, err := strconv.ParseInt(v, 10, 64); err == nil {
			filter.SignalID = &n
		}
	}
	if v := r.URL.Query().Get("action_id"); v != "" {
		if n, err := strconv.ParseInt(v, 10, 64); err == nil {
			filter.ActionID = &n
		}
	}
	if v := r.URL.Query().Get("status"); v != "" {
		filter.Status = &v
	}

	execs, err := s.store.ListActionExecutions(r.Context(), filter)
	if err != nil {
		s.logger.Error("api: list all executions", "error", err)
		writeError(w, http.StatusInternalServerError, "failed to list executions")
		return
	}
	writeJSON(w, http.StatusOK, execs)
}

func applyExecutionQueryParams(r *http.Request, filter *store.ExecutionFilter) {
	q := r.URL.Query()
	if v := q.Get("page"); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			filter.Page = n
		}
	}
	if v := q.Get("per_page"); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			filter.PerPage = n
		}
	}
	if v := q.Get("from"); v != "" {
		if t, err := time.Parse(time.RFC3339, v); err == nil {
			filter.From = &t
		}
	}
	if v := q.Get("to"); v != "" {
		if t, err := time.Parse(time.RFC3339, v); err == nil {
			filter.To = &t
		}
	}
}

// handleGetExecutionMetadata returns the metadata key-value pairs for a single execution.
func (s *Server) handleGetExecutionMetadata(w http.ResponseWriter, r *http.Request) {
	id, err := parseID(r)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid execution ID")
		return
	}
	meta, err := s.store.ListActionExecutionMetadata(r.Context(), id)
	if err != nil {
		s.logger.Error("api: get execution metadata", "execution_id", id, "error", err)
		writeError(w, http.StatusInternalServerError, "failed to get execution metadata")
		return
	}
	writeJSON(w, http.StatusOK, meta)
}

// reloadActionCache reloads the action cache from the store.
// Called after any action change.
func (s *Server) reloadActionCache(ctx context.Context) error {
	if s.actionCache == nil {
		return nil
	}
	return s.actionCache.Load(ctx)
}

// handleBulkDeleteActions DELETE /api/actions
func (s *Server) handleBulkDeleteActions(w http.ResponseWriter, r *http.Request) {
	var f store.BulkDeleteActionsFilter
	if err := json.NewDecoder(r.Body).Decode(&f); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	n, err := s.store.BulkDeleteActions(r.Context(), f)
	if err != nil {
		s.logger.Error("api: bulk delete actions", "error", err)
		writeError(w, http.StatusInternalServerError, "failed to bulk delete actions")
		return
	}
	writeJSON(w, http.StatusOK, map[string]int64{"deleted": n})
}
