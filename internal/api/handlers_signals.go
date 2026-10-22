package api

import (
	"encoding/json"
	"net/http"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/amir-the-h/tidewatch/internal/model"
	"github.com/amir-the-h/tidewatch/internal/queue"
	"github.com/amir-the-h/tidewatch/internal/store"
	"github.com/amir-the-h/tidewatch/internal/worker"
)

func (s *Server) handleListSignals(w http.ResponseWriter, r *http.Request) {
	filter := store.SignalFilter{
		Page:    1,
		PerPage: 20,
	}

	q := r.URL.Query()

	if v := q.Get("rule_id"); v != "" {
		id, err := strconv.ParseInt(v, 10, 64)
		if err != nil {
			writeError(w, http.StatusBadRequest, "invalid rule_id")
			return
		}
		filter.RuleID = &id
	}
	if v := q.Get("source_id"); v != "" {
		id, err := strconv.ParseInt(v, 10, 64)
		if err != nil {
			writeError(w, http.StatusBadRequest, "invalid source_id")
			return
		}
		filter.SourceID = &id
	}
	filter.SourceType = q.Get("source_type")
	if v := q.Get("kind"); v != "" {
		kind := model.SignalKind(v)
		if kind != model.SignalKindPost && kind != model.SignalKindArticle {
			writeError(w, http.StatusBadRequest, "invalid kind (post or article)")
			return
		}
		filter.Kind = &kind
	}
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
		page, err := strconv.Atoi(v)
		if err != nil || page < 1 {
			writeError(w, http.StatusBadRequest, "invalid page")
			return
		}
		filter.Page = page
	}
	if v := q.Get("per_page"); v != "" {
		pp, err := strconv.Atoi(v)
		if err != nil || pp < 1 {
			writeError(w, http.StatusBadRequest, "invalid per_page")
			return
		}
		filter.PerPage = pp
	}

	signals, err := s.store.ListSignals(r.Context(), filter)
	if err != nil {
		s.logger.Error("api: list signals", "error", err)
		writeError(w, http.StatusInternalServerError, "failed to list signals")
		return
	}
	total, err := s.store.CountSignals(r.Context(), filter)
	if err != nil {
		s.logger.Error("api: count signals", "error", err)
		writeError(w, http.StatusInternalServerError, "failed to count signals")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"signals": signals, "total": total})
}

func (s *Server) handleGetSignal(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid signal id")
		return
	}

	signal, err := s.store.GetSignal(r.Context(), id)
	if err != nil {
		writeError(w, http.StatusNotFound, "signal not found")
		return
	}

	// Load action executions for this signal, with latest-per-action deduplication.
	execs, err := s.store.ListActionExecutions(r.Context(), store.ExecutionFilter{SignalID: &signal.ID})
	if err != nil {
		s.logger.Error("api: list executions for signal", "signal_id", id, "error", err)
		writeError(w, http.StatusInternalServerError, "failed to list executions")
		return
	}

	type execResult struct {
		ID         int64          `json:"id"`
		Status     string         `json:"status"`
		Error      string         `json:"error,omitempty"`
		ExecutedAt string         `json:"executed_at"`
		Metadata   map[string]any `json:"metadata"`
	}
	type actionResult struct {
		ActionID   int64        `json:"action_id"`
		ActionName string       `json:"action_name"`
		Executions []execResult `json:"executions"`
	}

	// Group all executions under their action, preserving insertion order.
	actionOrder := []int64{}
	actionMap := map[int64]*actionResult{}

	for _, e := range execs {
		if _, known := actionMap[e.ActionID]; !known {
			name := ""
			if a, err := s.store.GetAction(r.Context(), e.ActionID); err == nil {
				name = a.Name
			}
			actionOrder = append(actionOrder, e.ActionID)
			actionMap[e.ActionID] = &actionResult{
				ActionID:   e.ActionID,
				ActionName: name,
				Executions: []execResult{},
			}
		}

		rawMeta, _ := s.store.ListActionExecutionMetadata(r.Context(), e.ID)
		meta := make(map[string]any, len(rawMeta))
		for k, v := range rawMeta {
			var parsed any
			if json.Unmarshal([]byte(v), &parsed) == nil {
				meta[k] = parsed
			} else {
				meta[k] = v
			}
		}

		actionMap[e.ActionID].Executions = append(actionMap[e.ActionID].Executions, execResult{
			ID:         e.ID,
			Status:     e.Status,
			Error:      e.Error,
			ExecutedAt: e.ExecutedAt.UTC().Format(time.RFC3339),
			Metadata:   meta,
		})
	}

	actions := make([]actionResult, 0, len(actionOrder))
	for _, id := range actionOrder {
		actions = append(actions, *actionMap[id])
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"signal":  signal,
		"actions": actions,
	})
}

func (s *Server) handleDeleteSignals(w http.ResponseWriter, r *http.Request) {
	var req struct {
		IDs []int64 `json:"ids"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if len(req.IDs) == 0 {
		writeError(w, http.StatusBadRequest, "ids is required")
		return
	}
	n, err := s.store.DeleteSignals(r.Context(), req.IDs)
	if err != nil {
		s.logger.Error("api: delete signals", "error", err)
		writeError(w, http.StatusInternalServerError, "failed to delete signals")
		return
	}
	writeJSON(w, http.StatusOK, map[string]int64{"deleted": n})
}

func (s *Server) handleReprocessSignals(w http.ResponseWriter, r *http.Request) {
	var req struct {
		SignalIDs []int64 `json:"signal_ids"`
		ActionIDs []int64 `json:"action_ids"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	if len(req.SignalIDs) == 0 {
		writeError(w, http.StatusBadRequest, "signal_ids is required")
		return
	}

	var signals []*model.Signal
	for _, id := range req.SignalIDs {
		signal, err := s.store.GetSignal(r.Context(), id)
		if err != nil {
			s.logger.Warn("api: reprocess: signal not found", "signal_id", id, "error", err)
			continue
		}
		signals = append(signals, signal)
	}

	if len(signals) == 0 {
		writeError(w, http.StatusNotFound, "signal not found")
		return
	}

	count := 0
	for _, signal := range signals {
		ev := queue.SignalEvent{SignalID: signal.ID, Kind: string(signal.Kind)}

		if len(req.ActionIDs) == 0 {
			// Pipeline dispatch — mirrors what ingest does for a new signal.
			if err := worker.PushInitialPipelineTasks(r.Context(), s.store, s.producer, s.actionCache, "new_signal", ev, s.logger, 0); err != nil {
				s.logger.Error("api: reprocess pipeline dispatch", "signal_id", signal.ID, "error", err)
				continue
			}
			count++
		} else {
			// Targeted dispatch: caller explicitly named which actions to run
			// (bypass chain routing — useful for debugging a specific action).
			evBytes, _ := json.Marshal(ev)
			pushed := false
			for _, actionID := range req.ActionIDs {
				task := &queue.Task{
					TriggerType: "new_signal",
					Event:       evBytes,
					ActionID:    actionID,
					Context:     map[string]any{},
				}
				if err := s.producer.PushTask(r.Context(), task); err != nil {
					s.logger.Error("api: reprocess push", "signal_id", signal.ID, "action_id", actionID, "error", err)
				} else {
					pushed = true
				}
			}
			if pushed {
				count++
			}
		}
	}

	writeJSON(w, http.StatusOK, map[string]int{"requeued": count})
}

func (s *Server) handleDeleteNoiseSignals(w http.ResponseWriter, r *http.Request) {
	n, err := s.store.DeleteNoiseSignals(r.Context())
	if err != nil {
		s.logger.Error("api: delete noise signals", "error", err)
		writeError(w, http.StatusInternalServerError, "failed to delete noise signals")
		return
	}
	writeJSON(w, http.StatusOK, map[string]int64{"deleted": n})
}

func (s *Server) handleDeleteExecutions(w http.ResponseWriter, r *http.Request) {
	var req struct {
		SignalIDs  []int64 `json:"signal_ids"`
		ActionIDs  []int64 `json:"action_ids"`
		PipelineID *int64  `json:"pipeline_id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	actionIDs := req.ActionIDs
	if req.PipelineID != nil {
		graph, err := s.store.GetPipelineGraph(r.Context(), *req.PipelineID)
		if err != nil {
			s.logger.Error("api: delete executions: resolve pipeline graph", "pipeline_id", *req.PipelineID, "error", err)
			writeError(w, http.StatusInternalServerError, "failed to resolve pipeline actions")
			return
		}
		for _, n := range graph.Nodes {
			if n.ActionID != nil {
				actionIDs = append(actionIDs, *n.ActionID)
			}
		}
	}

	n, err := s.store.DeleteActionExecutions(r.Context(), req.SignalIDs, actionIDs)
	if err != nil {
		s.logger.Error("api: delete executions", "error", err)
		writeError(w, http.StatusInternalServerError, "failed to delete executions")
		return
	}
	writeJSON(w, http.StatusOK, map[string]int64{"deleted": n})
}
