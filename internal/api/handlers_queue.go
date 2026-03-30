package api

import (
	"net/http"
	"sort"

	"github.com/amir-the-h/iran-watcher/internal/queue"
	"github.com/amir-the-h/iran-watcher/internal/worker"
)

type queueEntry struct {
	ActionID     int64       `json:"action_id"`
	ActionName   string      `json:"action_name"`
	PipelineName string      `json:"pipeline_name"`
	PipelineType string      `json:"pipeline_type"`
	Pending      int64       `json:"pending"`
	Workers      []workerRef `json:"workers,omitempty"`
}

type workerRef struct {
	ID      int    `json:"id"`
	Backend string `json:"backend,omitempty"`
}

// buildWorkerRefs groups WorkerInfo entries by action name into a map of
// workerRef slices. Multiple workers on the same action are all included.
func buildWorkerRefs(workerInfos []worker.WorkerInfo) map[string][]workerRef {
	m := make(map[string][]workerRef)
	for _, wi := range workerInfos {
		m[wi.Action] = append(m[wi.Action], workerRef{ID: wi.ID, Backend: wi.Backend})
	}
	return m
}

func (s *Server) handleGetQueue(w http.ResponseWriter, r *http.Request) {
	if s.actionCache == nil {
		writeJSON(w, http.StatusOK, map[string]any{"queues": []queueEntry{}})
		return
	}

	entries := s.actionCache.All()

	workerByAction := make(map[string][]workerRef)
	if s.pool != nil {
		workerByAction = buildWorkerRefs(s.pool.Stats().ActiveWorkers)
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
		if refs, ok := workerByAction[e.Action.Name]; ok {
			qe.Workers = refs
		}
		result = append(result, qe)
	}

	// Sort: active rows (worker or pending) first, then by pending desc, then name.
	sort.Slice(result, func(i, j int) bool {
		ri, rj := result[i], result[j]
		iActive := len(ri.Workers) > 0 || ri.Pending > 0
		jActive := len(rj.Workers) > 0 || rj.Pending > 0
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
