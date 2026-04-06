package api

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"

	"github.com/amir-the-h/iran-watcher/internal/inference"
	"github.com/amir-the-h/iran-watcher/internal/model"
	"github.com/amir-the-h/iran-watcher/internal/store"
)

// handleListOllamaBackends returns all backends merged with live pool stats.
// GET /api/ollama/backends
func (s *Server) handleListOllamaBackends(w http.ResponseWriter, r *http.Request) {
	rows, err := s.store.ListOllamaBackends(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	var stats []inference.BackendStat
	if s.backendPool != nil {
		stats = s.backendPool.Stats()
	}
	statsByID := make(map[int64]inference.BackendStat, len(stats))
	for _, st := range stats {
		statsByID[st.ID] = st
	}
	out := make([]map[string]any, 0, len(rows))
	for _, b := range rows {
		st := statsByID[b.ID]
		out = append(out, map[string]any{
			"id":             b.ID,
			"name":           b.Name,
			"url":            b.URL,
			"enabled":        b.Enabled,
			"created_at":     b.CreatedAt,
			"healthy":        st.Healthy,
			"in_use":         st.InUse,
			"score":          st.Score,
			"request_count":  st.RequestCount,
			"last_active_at": st.LastActiveAt,
		})
	}
	writeJSON(w, http.StatusOK, out)
}

// handleCreateOllamaBackend creates a new backend and reloads the pool.
// POST /api/ollama/backends
func (s *Server) handleCreateOllamaBackend(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Name string `json:"name"`
		URL  string `json:"url"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON")
		return
	}
	if req.URL == "" {
		writeError(w, http.StatusBadRequest, "url is required")
		return
	}
	if req.Name == "" {
		req.Name = req.URL
	}
	b, err := s.store.CreateOllamaBackend(r.Context(), req.Name, req.URL)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	s.reloadBackendPool(r.Context())
	if s.backendPool != nil {
		s.broadcastEvent("ollama_backends", s.backendPool.Stats())
	}
	writeJSON(w, http.StatusCreated, b)
}

// handleUpdateOllamaBackend updates name and/or URL for a backend.
// PUT /api/ollama/backends/{id}
func (s *Server) handleUpdateOllamaBackend(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid id")
		return
	}
	var req struct {
		Name string `json:"name"`
		URL  string `json:"url"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON")
		return
	}
	existing, err := s.store.GetOllamaBackend(r.Context(), id)
	if err != nil {
		writeError(w, http.StatusNotFound, "backend not found")
		return
	}
	if req.Name == "" {
		req.Name = existing.Name
	}
	if req.URL == "" {
		req.URL = existing.URL
	}
	if err := s.store.UpdateOllamaBackend(r.Context(), id, req.Name, req.URL); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	s.reloadBackendPool(r.Context())
	if s.backendPool != nil {
		s.broadcastEvent("ollama_backends", s.backendPool.Stats())
	}
	b, err := s.store.GetOllamaBackend(r.Context(), id)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, b)
}

// handleDeleteOllamaBackend deletes a backend.
// DELETE /api/ollama/backends/{id}
func (s *Server) handleDeleteOllamaBackend(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid id")
		return
	}
	if err := s.store.DeleteOllamaBackend(r.Context(), id); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	s.reloadBackendPool(r.Context())
	if s.backendPool != nil {
		s.broadcastEvent("ollama_backends", s.backendPool.Stats())
	}
	w.WriteHeader(http.StatusNoContent)
}

// handleEnableOllamaBackend enables a backend.
// POST /api/ollama/backends/{id}/enable
func (s *Server) handleEnableOllamaBackend(w http.ResponseWriter, r *http.Request) {
	s.setOllamaBackendEnabled(w, r, true)
}

// handleDisableOllamaBackend disables a backend.
// POST /api/ollama/backends/{id}/disable
func (s *Server) handleDisableOllamaBackend(w http.ResponseWriter, r *http.Request) {
	s.setOllamaBackendEnabled(w, r, false)
}

func (s *Server) setOllamaBackendEnabled(w http.ResponseWriter, r *http.Request, enabled bool) {
	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid id")
		return
	}
	if err := s.store.SetOllamaBackendEnabled(r.Context(), id, enabled); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	s.reloadBackendPool(r.Context())
	if s.backendPool != nil {
		s.broadcastEvent("ollama_backends", s.backendPool.Stats())
	}
	w.WriteHeader(http.StatusNoContent)
}

// handleListActionBackends returns an action's assigned Ollama backends in priority order.
// GET /api/actions/{id}/backends
func (s *Server) handleListActionBackends(w http.ResponseWriter, r *http.Request) {
	actionID, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid id")
		return
	}
	abs, err := s.store.ListActionBackends(r.Context(), actionID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, abs)
}

// handleSetActionBackends replaces an action's full backend assignment list.
// PUT /api/actions/{id}/backends
func (s *Server) handleSetActionBackends(w http.ResponseWriter, r *http.Request) {
	actionID, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid id")
		return
	}
	type backendItem struct {
		BackendID int64 `json:"backend_id"`
		Priority  int   `json:"priority"`
	}
	var req []backendItem
	bodyBytes, err := io.ReadAll(r.Body)
	if err != nil {
		writeError(w, http.StatusBadRequest, "failed to read body")
		return
	}
	// Accept either a raw array [...] or a wrapped object {"backends": [...]}.
	if len(bodyBytes) > 0 && bodyBytes[0] == '{' {
		var wrapped struct {
			Backends []backendItem `json:"backends"`
		}
		if err := json.Unmarshal(bodyBytes, &wrapped); err != nil {
			writeError(w, http.StatusBadRequest, "invalid JSON")
			return
		}
		req = wrapped.Backends
	} else {
		if err := json.Unmarshal(bodyBytes, &req); err != nil {
			writeError(w, http.StatusBadRequest, "invalid JSON")
			return
		}
	}
	assignments := make([]model.ActionBackend, len(req))
	for i, item := range req {
		assignments[i] = model.ActionBackend{ActionID: actionID, BackendID: item.BackendID, Priority: item.Priority}
	}
	if err := s.store.SetActionBackends(r.Context(), actionID, assignments); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	s.reloadCache(r.Context())
	abs, _ := s.store.ListActionBackends(r.Context(), actionID)
	writeJSON(w, http.StatusOK, abs)
}

// handleRemoveActionBackend removes one backend assignment from an action.
// DELETE /api/actions/{id}/backends/{backend_id}
func (s *Server) handleRemoveActionBackend(w http.ResponseWriter, r *http.Request) {
	actionID, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid id")
		return
	}
	backendID, err := strconv.ParseInt(chi.URLParam(r, "backend_id"), 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid backend_id")
		return
	}
	if err := s.store.RemoveActionBackend(r.Context(), actionID, backendID); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	s.reloadCache(r.Context())
	w.WriteHeader(http.StatusNoContent)
}

// reloadBackendPool reloads BackendPool from DB then reloads ActionCache.
func (s *Server) reloadBackendPool(ctx context.Context) {
	if s.backendPool == nil {
		return
	}
	backends, err := s.store.ListOllamaBackends(ctx)
	if err != nil {
		s.logger.Error("api: reload backend pool: list backends", "error", err)
		return
	}
	records := make([]inference.BackendRecord, len(backends))
	for i, b := range backends {
		records[i] = inference.BackendRecord{ID: b.ID, Name: b.Name, URL: b.URL, Enabled: b.Enabled}
	}
	s.backendPool.Load(records)
	s.reloadCache(ctx)
}

// handleBulkDeleteOllamaBackends DELETE /api/ollama/backends
func (s *Server) handleBulkDeleteOllamaBackends(w http.ResponseWriter, r *http.Request) {
	var f store.BulkDeleteOllamaBackendsFilter
	if err := json.NewDecoder(r.Body).Decode(&f); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	n, err := s.store.BulkDeleteOllamaBackends(r.Context(), f)
	if err != nil {
		s.logger.Error("api: bulk delete ollama backends", "error", err)
		writeError(w, http.StatusInternalServerError, "failed to bulk delete ollama backends")
		return
	}
	writeJSON(w, http.StatusOK, map[string]int64{"deleted": n})
}
