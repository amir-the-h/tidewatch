package api

import (
	"net/http"
	"sort"

	"github.com/amir-the-h/iran-watcher/internal/queue"
)

type queueEntry struct {
	ActionID     int64      `json:"action_id"`
	ActionName   string     `json:"action_name"`
	PipelineName string     `json:"pipeline_name"`
	PipelineType string     `json:"pipeline_type"`
	Pending      int64      `json:"pending"`
	Worker       *workerRef `json:"worker,omitempty"`
}

type workerRef struct {
	ID      int    `json:"id"`
	Backend string `json:"backend,omitempty"`
}

func (s *Server) handleGetQueue(w http.ResponseWriter, r *http.Request) {
	if s.actionCache == nil {
		writeJSON(w, http.StatusOK, map[string]any{"queues": []queueEntry{}})
		return
	}

	entries := s.actionCache.All()

	// Build worker lookup: action name → workerRef.
	workerByAction := make(map[string]workerRef)
	if s.pool != nil {
		for _, wi := range s.pool.Stats().ActiveWorkers {
			workerByAction[wi.Action] = workerRef{ID: wi.ID, Backend: wi.Backend}
		}
	}

	result := make([]queueEntry, 0, len(entries))
	for _, e := range entries {
		var pending int64
		if s.sizer != nil {
			pending, _ = s.sizer.Len(r.Context(), queue.QueueTasksKey(e.Action.ID))
		}
		qe := queueEntry{
			ActionID:     e.Action.ID,
			ActionName:   e.Action.Name,
			PipelineName: e.Pipeline.Name(),
			PipelineType: string(e.Pipeline.Type()),
			Pending:      pending,
		}
		if ref, ok := workerByAction[e.Action.Name]; ok {
			qe.Worker = &ref
		}
		result = append(result, qe)
	}

	// Sort: rows with activity (worker assigned or pending > 0) first,
	// then by pending desc, then alphabetically by action name.
	sort.Slice(result, func(i, j int) bool {
		ri, rj := result[i], result[j]
		iActive := ri.Worker != nil || ri.Pending > 0
		jActive := rj.Worker != nil || rj.Pending > 0
		if iActive != jActive {
			return iActive
		}
		if ri.Pending != rj.Pending {
			return ri.Pending > rj.Pending
		}
		return ri.ActionName < rj.ActionName
	})

	writeJSON(w, http.StatusOK, map[string]any{"queues": result})
}
