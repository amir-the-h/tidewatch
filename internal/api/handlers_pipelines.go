package api

import (
	"context"
	"encoding/json"
	"io/fs"
	"net/http"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"github.com/go-chi/chi/v5"

	"github.com/amir-the-h/iran-watcher/internal/model"
	"github.com/amir-the-h/iran-watcher/internal/queue"
	"github.com/amir-the-h/iran-watcher/internal/store"
	"github.com/amir-the-h/iran-watcher/internal/worker"
)

// handleListPipelines lists all Pipeline (DAG workflow) records.
// GET /api/pipelines
func (s *Server) handleListPipelines(w http.ResponseWriter, r *http.Request) {
	pipelines, err := s.store.ListPipelines(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "list pipelines: "+err.Error())
		return
	}
	if pipelines == nil {
		pipelines = []*model.Pipeline{}
	}
	writeJSON(w, http.StatusOK, pipelines)
}

// handleGetPipeline returns a single Pipeline.
// GET /api/pipelines/{id}
func (s *Server) handleGetPipeline(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid id")
		return
	}
	p, err := s.store.GetPipeline(r.Context(), id)
	if err != nil {
		writeError(w, http.StatusNotFound, "pipeline not found")
		return
	}
	writeJSON(w, http.StatusOK, p)
}

// handleCreatePipeline creates a new Pipeline (DAG workflow).
// POST /api/pipelines
func (s *Server) handleCreatePipeline(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Name        string `json:"name"`
		Description string `json:"description"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid body: "+err.Error())
		return
	}
	if req.Name == "" {
		writeError(w, http.StatusBadRequest, "name is required")
		return
	}
	p := &model.Pipeline{Name: req.Name, Description: req.Description}
	if err := s.store.SavePipeline(r.Context(), p); err != nil {
		writeError(w, http.StatusInternalServerError, "save pipeline: "+err.Error())
		return
	}
	if list, err := s.store.ListPipelines(r.Context()); err == nil {
		s.broadcastEvent("pipelines", list)
	}
	writeJSON(w, http.StatusCreated, p)
}

// handleUpdatePipeline updates a Pipeline's name/description.
// PUT /api/pipelines/{id}
func (s *Server) handleUpdatePipeline(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid id")
		return
	}
	var req struct {
		Name        string `json:"name"`
		Description string `json:"description"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid body: "+err.Error())
		return
	}
	p, err := s.store.GetPipeline(r.Context(), id)
	if err != nil {
		writeError(w, http.StatusNotFound, "pipeline not found")
		return
	}
	if req.Name != "" {
		p.Name = req.Name
	}
	p.Description = req.Description
	if err := s.store.UpdatePipeline(r.Context(), p); err != nil {
		writeError(w, http.StatusInternalServerError, "update pipeline: "+err.Error())
		return
	}
	if list, err := s.store.ListPipelines(r.Context()); err == nil {
		s.broadcastEvent("pipelines", list)
	}
	writeJSON(w, http.StatusOK, p)
}

// handleDeletePipeline deletes a Pipeline.
// DELETE /api/pipelines/{id}
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
	if list, err := s.store.ListPipelines(r.Context()); err == nil {
		s.broadcastEvent("pipelines", list)
	}
	w.WriteHeader(http.StatusNoContent)
}

// handleActivatePipeline activates a Pipeline.
// POST /api/pipelines/{id}/activate
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
	if list, err := s.store.ListPipelines(r.Context()); err == nil {
		s.broadcastEvent("pipelines", list)
	}
	w.WriteHeader(http.StatusNoContent)
}

// handleDeactivatePipeline deactivates a Pipeline.
// POST /api/pipelines/{id}/deactivate
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
	if list, err := s.store.ListPipelines(r.Context()); err == nil {
		s.broadcastEvent("pipelines", list)
	}
	w.WriteHeader(http.StatusNoContent)
}

// handleGetPipelineGraph returns the DAG graph for a pipeline.
// GET /api/pipelines/{id}/graph
func (s *Server) handleGetPipelineGraph(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid id")
		return
	}
	graph, err := s.store.GetPipelineGraph(r.Context(), id)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, graph)
}

// handleSavePipelineGraph replaces the full DAG graph for a pipeline.
// PUT /api/pipelines/{id}/graph
func (s *Server) handleSavePipelineGraph(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid id")
		return
	}
	var req model.PipelineGraphRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid body: "+err.Error())
		return
	}
	if hasCycle, err := detectPipelineCycle(r.Context(), s.store, id, req.Nodes); err != nil {
		writeError(w, http.StatusInternalServerError, "cycle check failed: "+err.Error())
		return
	} else if hasCycle {
		writeError(w, http.StatusBadRequest, "graph contains a cycle: a pipeline cannot (transitively) reference itself")
		return
	}
	graph, err := s.store.SavePipelineGraph(r.Context(), id, req.Nodes, req.Edges)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if list, err := s.store.ListPipelines(r.Context()); err == nil {
		s.broadcastEvent("pipelines", list)
	}
	writeJSON(w, http.StatusOK, graph)
}

// handleRunPipeline dispatches historical tweets through the pipeline's action nodes.
// Body: { "new_only": bool, "start_node_id": int64 (optional) }.
// When start_node_id is provided, dispatch begins from that specific node instead
// of the pipeline's Start node; the new_only exclusion set is also scoped to that node.
// POST /api/pipelines/{id}/run
func (s *Server) handleRunPipeline(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid id")
		return
	}

	var req struct {
		NewOnly     bool   `json:"new_only"`
		StartNodeID *int64 `json:"start_node_id"`
	}
	_ = json.NewDecoder(r.Body).Decode(&req)

	pipeline, err := s.store.GetPipeline(r.Context(), id)
	if err != nil {
		writeError(w, http.StatusNotFound, "pipeline not found")
		return
	}
	if !pipeline.Active {
		writeError(w, http.StatusUnprocessableEntity, "pipeline is not active")
		return
	}

	// Collect active trigger links for this pipeline.
	allLinks, err := s.store.ListTriggerPipelineLinks(r.Context())
	if err != nil {
		s.logger.Error("api: run pipeline: list trigger links", "pipeline_id", id, "error", err)
		writeError(w, http.StatusInternalServerError, "failed to list trigger links")
		return
	}
	var links []*model.TriggerPipelineLink
	for _, l := range allLinks {
		if l.PipelineID == id && l.Active {
			links = append(links, l)
		}
	}

	// If no direct trigger links exist, this pipeline may be a sub-pipeline
	// invoked from a parent. Walk all pipeline graphs to find parents that
	// reference this pipeline and inherit their trigger links (deduplicated by
	// trigger type, since only the type is used for tweet filtering).
	if len(links) == 0 {
		allPipelines, err := s.store.ListPipelines(r.Context())
		if err == nil {
			seenTriggers := make(map[string]struct{})
			for _, p := range allPipelines {
				if p.ID == id || !p.Active {
					continue
				}
				g, err := s.store.GetPipelineGraph(r.Context(), p.ID)
				if err != nil {
					continue
				}
				for _, node := range g.Nodes {
					if node.Type == model.PipelineNodeTypePipeline && node.SubPipelineID != nil && *node.SubPipelineID == id {
						for _, l := range allLinks {
							if l.PipelineID == p.ID && l.Active {
								if _, seen := seenTriggers[l.TriggerType]; !seen {
									seenTriggers[l.TriggerType] = struct{}{}
									links = append(links, l)
								}
							}
						}
						break
					}
				}
			}
		}
	}

	if len(links) == 0 {
		writeJSON(w, http.StatusAccepted, map[string]any{"queued": 0})
		return
	}

	// Load graph once for exclusion-set building and dispatch.
	graph, err := s.store.GetPipelineGraph(r.Context(), id)
	if err != nil {
		s.logger.Error("api: run pipeline: load graph", "pipeline_id", id, "error", err)
		writeError(w, http.StatusInternalServerError, "failed to load pipeline graph")
		return
	}

	// Validate start_node_id if provided.
	var startNode *model.PipelineNode
	if req.StartNodeID != nil {
		startNode = worker.FindNodeInGraph(graph, *req.StartNodeID)
		if startNode == nil {
			writeError(w, http.StatusBadRequest, "start_node_id not found in pipeline graph")
			return
		}
		if startNode.Type == model.PipelineNodeTypeStart || startNode.Type == model.PipelineNodeTypeCondition {
			writeError(w, http.StatusBadRequest, "start_node_id must be an action or pipeline node")
			return
		}
	}

	// Return 202 immediately — tweet iteration and task pushing happen in the
	// background so large backlogs don't time out the HTTP connection.
	writeJSON(w, http.StatusAccepted, map[string]any{"queued": 0, "async": true})

	capturedStartNode := startNode
	go func() {
		ctx := s.ctx
		newOnly := req.NewOnly

		// Build exclusion set once if new_only is requested.
		// When start_node_id is provided, scope the exclusion set to only the
		// action IDs reachable from that specific node; otherwise collect all
		// action IDs from the full graph and any referenced sub-pipeline graphs.
		var executedIDs map[string]struct{}
		if newOnly {
			executedIDs = make(map[string]struct{})
			var actionIDs []int64
			if capturedStartNode != nil {
				actionIDs = s.collectNodeActionIDs(ctx, capturedStartNode)
			} else {
				actionIDs = s.collectGraphActionIDs(ctx, graph)
			}
			for _, actionID := range actionIDs {
				aid := actionID
				execs, err := s.store.ListActionExecutions(ctx, store.ExecutionFilter{ActionID: &aid})
				if err != nil {
					s.logger.Warn("api: run pipeline: fetch executions", "action_id", aid, "error", err)
					continue
				}
				for _, e := range execs {
					executedIDs[e.TweetID] = struct{}{}
				}
			}
		}

		// Push tasks: for each active trigger link, select eligible tweets and dispatch.
		count := 0
		seenTweetIDs := make(map[string]struct{})

		for _, link := range links {
			filter := store.TweetFilter{Page: 1, PerPage: 5000}
			switch link.TriggerType {
			case "new_tweet", "new_categorization":
				// all tweets
			case "entity_matched":
				filter.HasEntityTrack = true
			default:
				continue
			}

			tweets, err := s.store.ListTweets(ctx, filter)
			if err != nil {
				s.logger.Error("api: run pipeline: list tweets", "trigger", link.TriggerType, "error", err)
				continue
			}

			for _, tweet := range tweets {
				if _, seen := seenTweetIDs[tweet.ID]; seen {
					continue
				}
				seenTweetIDs[tweet.ID] = struct{}{}

				if newOnly {
					if _, done := executedIDs[tweet.ID]; done {
						continue
					}
				}
				ruleIDs, _ := s.store.ListTweetRuleIDs(ctx, tweet.ID)
				evPayload, _ := json.Marshal(queue.TweetEvent{
					TweetID:      tweet.ID,
					Text:         tweet.Text,
					AuthorHandle: tweet.AuthorHandle,
					RuleIDs:      ruleIDs,
				})
				if capturedStartNode != nil {
					count += worker.PushTasksFromNode(
						ctx, s.store, s.producer, s.actionCache,
						graph, id, link.TriggerType, evPayload, tweet.ID, capturedStartNode, s.logger,
					)
				} else {
					count += worker.PushInitialTasksForGraph(
						ctx, s.store, s.producer, s.actionCache,
						graph, id, link.TriggerType, evPayload, tweet.ID, s.logger,
					)
				}
			}
		}

		s.logger.Info("api: run pipeline: background dispatch complete", "pipeline_id", id, "queued", count)
	}()
}

// handleListActionHandlers returns the registered internal action handler names.
// GET /api/actions/handlers
func (s *Server) handleListActionHandlers(w http.ResponseWriter, r *http.Request) {
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

// collectGraphActionIDs walks a pipeline graph and all referenced sub-pipeline
// graphs (one level deep per pipeline node) and returns the unique set of
// action IDs found. Used to build the new_only exclusion set correctly even
// when action nodes live inside sub-pipelines rather than the parent graph.
func (s *Server) collectGraphActionIDs(ctx context.Context, graph *model.PipelineGraph) []int64 {
	seen := make(map[int64]struct{})
	var ids []int64
	add := func(id int64) {
		if _, ok := seen[id]; !ok {
			seen[id] = struct{}{}
			ids = append(ids, id)
		}
	}
	for _, node := range graph.Nodes {
		if node.Type == model.PipelineNodeTypeAction && node.ActionID != nil {
			add(*node.ActionID)
		}
		if node.Type == model.PipelineNodeTypePipeline && node.SubPipelineID != nil {
			subGraph, err := s.store.GetPipelineGraph(ctx, *node.SubPipelineID)
			if err != nil {
				continue
			}
			for _, subNode := range subGraph.Nodes {
				if subNode.Type == model.PipelineNodeTypeAction && subNode.ActionID != nil {
					add(*subNode.ActionID)
				}
			}
		}
	}
	return ids
}

// collectNodeActionIDs returns the action IDs directly at the given node —
// not the full downstream sub-tree. For action nodes: the node's own action_id.
// For pipeline nodes: all action IDs found inside the referenced sub-pipeline
// graph (one level deep). Downstream successors of the named node are excluded
// by design: the exclusion set only covers what would be redundant for the
// selected start node.
// Used to scope the new_only exclusion set when start_node_id is provided.
func (s *Server) collectNodeActionIDs(ctx context.Context, node *model.PipelineNode) []int64 {
	seen := make(map[int64]struct{})
	var ids []int64
	add := func(id int64) {
		if _, ok := seen[id]; !ok {
			seen[id] = struct{}{}
			ids = append(ids, id)
		}
	}
	switch node.Type {
	case model.PipelineNodeTypeAction:
		if node.ActionID != nil {
			add(*node.ActionID)
		}
	case model.PipelineNodeTypePipeline:
		if node.SubPipelineID != nil {
			subGraph, err := s.store.GetPipelineGraph(ctx, *node.SubPipelineID)
			if err == nil {
				for _, subNode := range subGraph.Nodes {
					if subNode.Type == model.PipelineNodeTypeAction && subNode.ActionID != nil {
						add(*subNode.ActionID)
					}
				}
			}
		}
	}
	return ids
}

// handleListModels lists ONNX model files in the models directory.
// GET /api/models
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

// detectPipelineCycle performs a DFS through sub_pipeline_id references to detect
// transitive self-reference cycles. Returns (true, nil) if a cycle is found.
func detectPipelineCycle(ctx context.Context, st store.Store, pipelineID int64, nodes []*model.PipelineNodeRequest) (bool, error) {
	visited := make(map[int64]bool)
	var dfs func(id int64) (bool, error)
	dfs = func(id int64) (bool, error) {
		if id == pipelineID {
			return true, nil
		}
		if visited[id] {
			return false, nil
		}
		visited[id] = true
		g, err := st.GetPipelineGraph(ctx, id)
		if err != nil {
			return false, err
		}
		for _, n := range g.Nodes {
			if n.SubPipelineID != nil {
				if cycle, err := dfs(*n.SubPipelineID); cycle || err != nil {
					return cycle, err
				}
			}
		}
		return false, nil
	}

	for _, n := range nodes {
		if n.SubPipelineID != nil {
			if cycle, err := dfs(*n.SubPipelineID); cycle || err != nil {
				return cycle, err
			}
		}
	}
	return false, nil
}

// handleBulkDeletePipelines DELETE /api/pipelines
func (s *Server) handleBulkDeletePipelines(w http.ResponseWriter, r *http.Request) {
	var f store.BulkDeletePipelinesFilter
	if err := json.NewDecoder(r.Body).Decode(&f); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	n, err := s.store.BulkDeletePipelines(r.Context(), f)
	if err != nil {
		s.logger.Error("api: bulk delete pipelines", "error", err)
		writeError(w, http.StatusInternalServerError, "failed to bulk delete pipelines")
		return
	}
	writeJSON(w, http.StatusOK, map[string]int64{"deleted": n})
}
