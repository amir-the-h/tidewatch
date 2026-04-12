package worker

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strconv"
	"strings"

	"github.com/amir-the-h/iran-watcher/internal/inference"
	"github.com/amir-the-h/iran-watcher/internal/model"
	"github.com/amir-the-h/iran-watcher/internal/queue"
	"github.com/amir-the-h/iran-watcher/internal/store"
)

// IsOllamaAction reports whether the action identified by actionID uses an
// Ollama pipeline. Returns false when cache is nil or the action is not found.
func IsOllamaAction(cache *ActionCache, actionID int64) bool {
	if cache == nil {
		return false
	}
	entry := cache.Get(actionID)
	return entry != nil && entry.Pipeline.Type() == inference.PipelineTypeOllama
}

// PushInitialPipelineTasks dispatches tasks for all active pipelines linked to
// triggerType. Called by the stream listener and search client when a new tweet
// arrives. cache is used to skip tasks for disabled actions; pass nil to disable
// the check (tasks will still be dropped by the worker if the action is inactive).
func PushInitialPipelineTasks(
	ctx context.Context,
	st store.Store,
	producer queue.Producer,
	cache *ActionCache,
	triggerType string,
	ev queue.TweetEvent,
	logger *slog.Logger,
	sourcePipelineID int64,
) error {
	links, err := st.ListActiveTriggerPipelineLinks(ctx, triggerType, sourcePipelineID)
	if err != nil {
		return fmt.Errorf("pipeline: list active trigger links for %q: %w", triggerType, err)
	}

	if logger != nil {
		logger.Info("pipeline: initial dispatch",
			"trigger", triggerType,
			"tweet_id", ev.TweetID,
			"active_pipeline_links", len(links),
		)
	}

	if len(links) == 0 {
		if logger != nil {
			logger.Info("pipeline: no active pipeline links for trigger — tweet will not be processed",
				"trigger", triggerType,
				"tweet_id", ev.TweetID,
			)
		}
		return nil
	}

	eventPayload, err := json.Marshal(ev)
	if err != nil {
		return fmt.Errorf("pipeline: marshal tweet event: %w", err)
	}

	for _, link := range links {
		graph, err := st.GetPipelineGraph(ctx, link.PipelineID)
		if err != nil {
			if logger != nil {
				logger.Warn("pipeline: load graph failed", "pipeline_id", link.PipelineID, "error", err)
			}
			continue
		}

		if logger != nil {
			logger.Info("pipeline: walking graph",
				"pipeline_id", link.PipelineID,
				"nodes", len(graph.Nodes),
				"edges", len(graph.Edges),
			)
		}

		PushInitialTasksForGraph(ctx, st, producer, cache, graph, link.PipelineID, triggerType, eventPayload, ev.TweetID, logger)
	}

	return nil
}

// PushInitialTasksForGraph walks from the start node of an already-loaded graph
// and dispatches tasks for all reachable nodes (action and pipeline types).
// It is used by both the stream listener (via PushInitialPipelineTasks) and the
// manual backfill handler so they share identical dispatch logic.
// Returns the number of tasks successfully pushed.
func PushInitialTasksForGraph(
	ctx context.Context,
	st store.Store,
	producer queue.Producer,
	cache *ActionCache,
	graph *model.PipelineGraph,
	pipelineID int64,
	triggerType string,
	eventPayload []byte,
	tweetID string,
	logger *slog.Logger,
) int {
	startNode := findStartNode(graph)
	if startNode == nil {
		if logger != nil {
			logger.Warn("pipeline: no start node found — pipeline skipped", "pipeline_id", pipelineID)
		}
		return 0
	}

	if logger != nil {
		logger.Info("pipeline: start node found",
			"pipeline_id", pipelineID,
			"start_node_id", startNode.ID,
		)
	}

	// Build an initial context from the event so that conditions placed
	// directly after Start can be evaluated (e.g. rule_matches).
	// Also merge all stored execution metadata for this tweet so that
	// pipeline_finished-triggered pipelines can check detector/extractor
	// outputs in condition nodes during initial dispatch.
	var initCtx map[string]any
	{
		var ev queue.TweetEvent
		if err := json.Unmarshal(eventPayload, &ev); err == nil {
			initCtx = map[string]any{
				"rule_ids":     ev.RuleIDs,
				"tweet_id":     ev.TweetID,
				"tweet":        ev.Text,
				"tweet_author": ev.AuthorHandle,
			}
			if ev.TweetID != "" {
				if allMeta, err := st.MergeAllTweetExecutionMetadata(ctx, ev.TweetID); err == nil {
					for k, v := range allMeta {
						if _, exists := initCtx[k]; !exists {
							initCtx[k] = decodeMetaValue(v)
						}
					}
				}
			}
		} else {
			initCtx = map[string]any{}
		}
	}

	// Seed the tracking row to 0 BEFORE pushing any tasks. This prevents a race
	// where the initial dispatch loop takes time (e.g. loading sub-pipeline graphs
	// from NFS-backed SQLite) and workers complete tasks before InitPipelineRunTracking
	// is called — the old post-loop reset would overwrite those decrements, leaving
	// the counter permanently inflated and pipeline_finished never firing.
	// Each pushTrackedTask call below will increment from 0 as it enqueues.
	if tweetID != "" {
		if err := st.InitPipelineRunTracking(ctx, pipelineID, tweetID, 0, eventPayload); err != nil && logger != nil {
			logger.Warn("pipeline: init run tracking failed",
				"pipeline_id", pipelineID, "tweet_id", tweetID, "error", err)
		}
	}

	pushed := 0
	for _, edge := range outgoingEdges(graph, startNode.ID) {
		node := findNode(graph, edge.TargetNodeID)
		if node == nil {
			continue
		}
		pushed += dispatchInitialNode(ctx, st, producer, cache, graph, pipelineID,
			pipelineID, triggerType, eventPayload, tweetID, node, initCtx, 0, logger)
	}

	return pushed
}

// PushTasksFromNode dispatches tasks starting from a specific node instead of
// the pipeline's Start node. Context from prior runs is always available at
// runtime via MergeAllTweetExecutionMetadata — the worker loads all stored
// action_execution_metadata for the tweet regardless of which node initiates the run.
func PushTasksFromNode(
	ctx context.Context,
	st store.Store,
	producer queue.Producer,
	cache *ActionCache,
	graph *model.PipelineGraph,
	pipelineID int64,
	triggerType string,
	eventPayload []byte,
	tweetID string,
	startNode *model.PipelineNode,
	logger *slog.Logger,
) int {
	var initCtx map[string]any
	{
		var ev queue.TweetEvent
		if err := json.Unmarshal(eventPayload, &ev); err == nil {
			initCtx = map[string]any{
				"rule_ids":     ev.RuleIDs,
				"tweet_id":     ev.TweetID,
				"tweet":        ev.Text,
				"tweet_author": ev.AuthorHandle,
			}
		} else {
			initCtx = map[string]any{}
		}
	}
	return dispatchInitialNode(ctx, st, producer, cache, graph, pipelineID,
		pipelineID, triggerType, eventPayload, tweetID, startNode, initCtx, 0, logger)
}

// FindNodeInGraph returns the node with the given ID, or nil if not found.
// Exported for use by the API handler when validating start_node_id.
func FindNodeInGraph(g *model.PipelineGraph, id int64) *model.PipelineNode {
	return findNode(g, id)
}

// dispatchInitialNode dispatches tasks for a node reachable from the Start node
// (directly or through a chain of condition nodes). Conditions are evaluated
// recursively so that Start→Condition→Condition→Pipeline works at any depth.
func dispatchInitialNode(
	ctx context.Context,
	st store.Store,
	producer queue.Producer,
	cache *ActionCache,
	graph *model.PipelineGraph,
	pipelineID int64,
	rootPipelineID int64,
	triggerType string,
	eventPayload []byte,
	tweetID string,
	node *model.PipelineNode,
	initCtx map[string]any,
	depth int,
	logger *slog.Logger,
) int {
	pushed := 0
	switch node.Type {
	case model.PipelineNodeTypeAction:
		if node.ActionID == nil {
			if logger != nil {
				logger.Warn("pipeline: action node has no action_id — skipped",
					"pipeline_id", pipelineID, "node_id", node.ID)
			}
			return 0
		}
		if cache != nil && cache.Get(*node.ActionID) == nil {
			if logger != nil {
				logger.Info("pipeline: skipping task for inactive action",
					"pipeline_id", pipelineID, "node_id", node.ID,
					"action_id", *node.ActionID)
			}
			return 0
		}
		task := &queue.Task{
			TriggerType:    triggerType,
			Event:          eventPayload,
			ActionID:       *node.ActionID,
			PipelineNodeID: node.ID,
			PipelineID:     pipelineID,
			RootPipelineID: rootPipelineID,
			Depth:          depth,
			Context:        map[string]any{},
			IsOllama:       IsOllamaAction(cache, *node.ActionID),
		}
		if pushTrackedTask(ctx, st, producer, task, logger) {
			pushed++
			if logger != nil {
				logger.Info("pipeline: queued initial task",
					"pipeline_id", pipelineID,
					"action_id", *node.ActionID,
					"node_id", node.ID,
					"tweet_id", tweetID,
				)
			}
		}

	case model.PipelineNodeTypePipeline:
		if node.SubPipelineID == nil {
			if logger != nil {
				logger.Warn("pipeline: pipeline node has no sub_pipeline_id — skipped",
					"pipeline_id", pipelineID, "node_id", node.ID)
			}
			return 0
		}
		subGraph, subErr := st.GetPipelineGraph(ctx, *node.SubPipelineID)
		if subErr != nil {
			if logger != nil {
				logger.Warn("pipeline: load sub-pipeline graph failed",
					"sub_pipeline_id", *node.SubPipelineID, "error", subErr)
			}
			return 0
		}
		subStartNode := findStartNode(subGraph)
		if subStartNode == nil {
			return 0
		}
		parentNodeID := node.ID
		for _, subActionNode := range resolveSubPipelineEntryNodes(subGraph, subStartNode.ID, initCtx, *node.SubPipelineID, logger) {
			if subActionNode.ActionID == nil {
				continue
			}
			if cache != nil && cache.Get(*subActionNode.ActionID) == nil {
				continue
			}
			entryNodeID := subActionNode.ID
			if insErr := st.InsertPipelineNodeExecution(ctx, parentNodeID, tweetID, entryNodeID); insErr != nil && logger != nil {
				logger.Warn("pipeline: insert pipeline node execution failed",
					"parent_node_id", parentNodeID, "entry_node_id", entryNodeID, "error", insErr)
			}
			subTask := &queue.Task{
				TriggerType:            triggerType,
				Event:                  eventPayload,
				ActionID:               *subActionNode.ActionID,
				PipelineNodeID:         subActionNode.ID,
				PipelineID:             *node.SubPipelineID,
				RootPipelineID:         rootPipelineID,
				ParentPipelineNodeID:   &parentNodeID,
				SubPipelineEntryNodeID: &entryNodeID,
				Depth:                  depth + 1,
				Context:                map[string]any{},
				IsOllama:               IsOllamaAction(cache, *subActionNode.ActionID),
			}
			if pushTrackedTask(ctx, st, producer, subTask, logger) {
				pushed++
				if logger != nil {
					logger.Info("pipeline: queued sub-pipeline initial task",
						"pipeline_id", pipelineID,
						"sub_pipeline_id", *node.SubPipelineID,
						"action_id", *subActionNode.ActionID,
						"node_id", subActionNode.ID,
						"tweet_id", tweetID,
					)
				}
			}
		}

	case model.PipelineNodeTypeCondition:
		passed := evaluateConditionsVerbose(node.Conditions, initCtx, pipelineID, node.ID, logger)
		wantHandle := "pass"
		if !passed {
			wantHandle = "fail"
		}
		if logger != nil {
			logger.Info("pipeline: condition node evaluated during initial dispatch",
				"pipeline_id", pipelineID, "node_id", node.ID,
				"passed", passed, "branch", wantHandle)
		}
		for _, condEdge := range outgoingEdges(graph, node.ID) {
			h := condEdge.SourceHandle
			if h == "" {
				h = "pass"
			}
			if h != wantHandle {
				continue
			}
			nextNode := findNode(graph, condEdge.TargetNodeID)
			if nextNode == nil {
				continue
			}
			// Recurse: handles Action, Pipeline, and nested Condition nodes.
			pushed += dispatchInitialNode(ctx, st, producer, cache, graph, pipelineID,
				rootPipelineID, triggerType, eventPayload, tweetID, nextNode, initCtx, depth, logger)
		}
	}
	return pushed
}

// PushContinuationTasks is called by the worker after successfully running an action.
// It evaluates outgoing edges from task.PipelineNodeID and pushes tasks for downstream
// action nodes whose conditions are satisfied. cache is used to skip tasks for disabled
// actions; pass nil to disable the check.
func PushContinuationTasks(
	ctx context.Context,
	st store.Store,
	producer queue.Producer,
	cache *ActionCache,
	task *queue.Task,
	mergedCtx map[string]any,
	logger *slog.Logger,
) {
	if task.PipelineNodeID == 0 {
		if logger != nil {
			logger.Info("pipeline: continuation skipped — task has no pipeline_node_id (not a pipeline task)",
				"action_id", task.ActionID,
			)
		}
		return
	}

	pipelineID := task.PipelineID
	if pipelineID == 0 {
		var err error
		pipelineID, err = st.GetPipelineIDForNode(ctx, task.PipelineNodeID)
		if err != nil {
			if logger != nil {
				logger.Warn("pipeline: get pipeline id for node failed",
					"node_id", task.PipelineNodeID, "error", err)
			}
			return
		}
	}

	graph, err := st.GetPipelineGraph(ctx, pipelineID)
	if err != nil {
		if logger != nil {
			logger.Warn("pipeline: load graph for continuation failed",
				"pipeline_id", pipelineID, "error", err)
		}
		return
	}

	edges := outgoingEdges(graph, task.PipelineNodeID)
	if logger != nil {
		// Log context keys so we can see what the condition will evaluate against.
		ctxKeys := make([]string, 0, len(mergedCtx))
		for k := range mergedCtx {
			ctxKeys = append(ctxKeys, k)
		}
		logger.Info("pipeline: continuing from node",
			"pipeline_id", pipelineID,
			"completed_node_id", task.PipelineNodeID,
			"action_id", task.ActionID,
			"outgoing_edges", len(edges),
			"context_keys", ctxKeys,
		)
	}

	// signalSubPipelineLeafDone marks this branch's entry node as complete and,
	// if all branches of the sub-pipeline are done, resumes the parent pipeline.
	// It is called both when a node has no outgoing edges (structural terminal)
	// and when all outgoing branches were skipped (e.g. condition failed).
	signalSubPipelineLeafDone := func(tweetID string) {
		if task.ParentPipelineNodeID == nil || task.SubPipelineEntryNodeID == nil {
			return
		}
		allDone, err := st.MarkPipelineNodeLeafComplete(ctx, *task.ParentPipelineNodeID, tweetID, *task.SubPipelineEntryNodeID)
		if err != nil && logger != nil {
			logger.Warn("pipeline: mark leaf complete failed",
				"parent_node_id", *task.ParentPipelineNodeID,
				"entry_node_id", *task.SubPipelineEntryNodeID, "error", err)
		}
		if allDone {
			parentPipelineID, pidErr := st.GetPipelineIDForNode(ctx, *task.ParentPipelineNodeID)
			if pidErr != nil && logger != nil {
				logger.Warn("pipeline: get parent pipeline id failed", "error", pidErr)
			} else {
				freshCtx := copyContext(mergedCtx)
				parentTask := &queue.Task{
					TriggerType:    task.TriggerType,
					Event:          task.Event,
					PipelineNodeID: *task.ParentPipelineNodeID,
					PipelineID:     parentPipelineID,
					RootPipelineID: task.RootPipelineID,
					Depth:          task.Depth,
				}
				PushContinuationTasks(ctx, st, producer, cache, parentTask, freshCtx, logger)
			}
		}
	}

	if len(edges) == 0 {
		if logger != nil {
			logger.Info("pipeline: no outgoing edges — pipeline ends here",
				"pipeline_id", pipelineID, "node_id", task.PipelineNodeID)
		}
		signalSubPipelineLeafDone(extractTweetID(task))
		return
	}

	pushed := 0
	for _, edge := range edges {
		targetNode := findNode(graph, edge.TargetNodeID)
		if targetNode == nil {
			if logger != nil {
				logger.Warn("pipeline: target node not found in graph",
					"pipeline_id", pipelineID,
					"source_node_id", task.PipelineNodeID,
					"target_node_id", edge.TargetNodeID,
				)
			}
			continue
		}

		if logger != nil {
			logger.Info("pipeline: evaluating edge",
				"pipeline_id", pipelineID,
				"source_node_id", edge.SourceNodeID,
				"target_node_id", edge.TargetNodeID,
				"target_type", targetNode.Type,
			)
		}

		switch targetNode.Type {
		case model.PipelineNodeTypeAction:
			if targetNode.ActionID == nil {
				if logger != nil {
					logger.Warn("pipeline: action node has no action_id — skipped",
						"pipeline_id", pipelineID, "node_id", targetNode.ID)
				}
				continue
			}
			if cache != nil && cache.Get(*targetNode.ActionID) == nil {
				if logger != nil {
					logger.Info("pipeline: skipping downstream task for inactive action",
						"pipeline_id", pipelineID, "node_id", targetNode.ID,
						"action_id", *targetNode.ActionID)
				}
				continue
			}
			if logger != nil {
				logger.Info("pipeline: pushing downstream action task",
					"pipeline_id", pipelineID,
					"action_id", *targetNode.ActionID,
					"node_id", targetNode.ID,
				)
			}
			if pushContinuationTask(ctx, st, producer, cache, task, targetNode, mergedCtx, logger) {
				pushed++
			}

		case model.PipelineNodeTypeCondition:
			if logger != nil {
				logger.Info("pipeline: evaluating condition node",
					"pipeline_id", pipelineID,
					"node_id", targetNode.ID,
					"conditions", len(targetNode.Conditions),
				)
			}
			passed := evaluateConditionsVerbose(targetNode.Conditions, mergedCtx, pipelineID, targetNode.ID, logger)
			wantHandle := "pass"
			if !passed {
				wantHandle = "fail"
			}
			if logger != nil {
				logger.Info("pipeline: condition node evaluated — following branch",
					"pipeline_id", pipelineID,
					"node_id", targetNode.ID,
					"passed", passed,
					"branch", wantHandle,
				)
			}
			// Follow edges whose source handle matches the branch ("pass" or "fail").
			// Legacy edges with an empty source_handle are treated as "pass".
			for _, condEdge := range outgoingEdges(graph, targetNode.ID) {
				h := condEdge.SourceHandle
				if h == "" {
					h = "pass"
				}
				if h != wantHandle {
					continue
				}
				nextNode := findNode(graph, condEdge.TargetNodeID)
				if nextNode == nil {
					continue
				}
				switch nextNode.Type {
				case model.PipelineNodeTypeAction:
					if nextNode.ActionID == nil {
						if logger != nil {
							logger.Warn("pipeline: action node has no action_id after condition — skipped",
								"pipeline_id", pipelineID, "node_id", nextNode.ID)
						}
						continue
					}
					if cache != nil && cache.Get(*nextNode.ActionID) == nil {
						if logger != nil {
							logger.Info("pipeline: skipping downstream task for inactive action (post-condition)",
								"pipeline_id", pipelineID, "node_id", nextNode.ID,
								"action_id", *nextNode.ActionID)
						}
						continue
					}
					if logger != nil {
						logger.Info("pipeline: pushing downstream action task after condition",
							"pipeline_id", pipelineID,
							"action_id", *nextNode.ActionID,
							"node_id", nextNode.ID,
						)
					}
					if pushContinuationTask(ctx, st, producer, cache, task, nextNode, mergedCtx, logger) {
						pushed++
					}
				case model.PipelineNodeTypePipeline:
					if nextNode.SubPipelineID == nil {
						if logger != nil {
							logger.Warn("pipeline: pipeline node has no sub_pipeline_id after condition — skipped",
								"pipeline_id", pipelineID, "node_id", nextNode.ID)
						}
						continue
					}
					subGraph, subErr := st.GetPipelineGraph(ctx, *nextNode.SubPipelineID)
					if subErr != nil {
						if logger != nil {
							logger.Warn("pipeline: load sub-pipeline graph failed (post-condition)",
								"sub_pipeline_id", *nextNode.SubPipelineID, "error", subErr)
						}
						continue
					}
					subStartNode := findStartNode(subGraph)
					if subStartNode == nil {
						continue
					}
					parentNodeID := nextNode.ID
					tweetID := extractTweetID(task)
					for _, subActionNode := range resolveSubPipelineEntryNodes(subGraph, subStartNode.ID, mergedCtx, *nextNode.SubPipelineID, logger) {
						if subActionNode.ActionID == nil {
							continue
						}
						if cache != nil && cache.Get(*subActionNode.ActionID) == nil {
							continue
						}
						entryNodeID := subActionNode.ID
						if insErr := st.InsertPipelineNodeExecution(ctx, parentNodeID, tweetID, entryNodeID); insErr != nil && logger != nil {
							logger.Warn("pipeline: insert pipeline node execution failed (post-condition)",
								"parent_node_id", parentNodeID, "entry_node_id", entryNodeID, "error", insErr)
						}
						subTask := &queue.Task{
							TriggerType:            task.TriggerType,
							Event:                  task.Event,
							ActionID:               *subActionNode.ActionID,
							PipelineNodeID:         subActionNode.ID,
							PipelineID:             *nextNode.SubPipelineID,
							RootPipelineID:         task.RootPipelineID,
							ParentPipelineNodeID:   &parentNodeID,
							SubPipelineEntryNodeID: &entryNodeID,
							Depth:                  task.Depth + 1,
							Context:                copyContext(mergedCtx),
							IsOllama:               IsOllamaAction(cache, *subActionNode.ActionID),
						}
						if pushTrackedTask(ctx, st, producer, subTask, logger) {
							pushed++
						}
					}
				default:
					if logger != nil {
						logger.Warn("pipeline: unexpected node type after condition — skipped",
							"pipeline_id", pipelineID,
							"node_id", nextNode.ID,
							"type", nextNode.Type,
						)
					}
				}
			}

		case model.PipelineNodeTypePipeline:
			if targetNode.SubPipelineID == nil {
				if logger != nil {
					logger.Warn("pipeline: pipeline node has no sub_pipeline_id — skipped",
						"pipeline_id", pipelineID, "node_id", targetNode.ID)
				}
				continue
			}
			subGraph, err := st.GetPipelineGraph(ctx, *targetNode.SubPipelineID)
			if err != nil {
				if logger != nil {
					logger.Warn("pipeline: load sub-pipeline graph failed",
						"sub_pipeline_id", *targetNode.SubPipelineID, "error", err)
				}
				continue
			}
			subStartNode := findStartNode(subGraph)
			if subStartNode == nil {
				continue
			}
			parentNodeID := targetNode.ID
			tweetID := extractTweetID(task)
			for _, subActionNode := range resolveSubPipelineEntryNodes(subGraph, subStartNode.ID, mergedCtx, *targetNode.SubPipelineID, logger) {
				if subActionNode.ActionID == nil {
					continue
				}
				if cache != nil && cache.Get(*subActionNode.ActionID) == nil {
					continue
				}
				entryNodeID := subActionNode.ID
				if err := st.InsertPipelineNodeExecution(ctx, parentNodeID, tweetID, entryNodeID); err != nil && logger != nil {
					logger.Warn("pipeline: insert pipeline node execution failed",
						"parent_node_id", parentNodeID, "entry_node_id", entryNodeID, "error", err)
				}
				subTask := &queue.Task{
					TriggerType:            task.TriggerType,
					Event:                  task.Event,
					ActionID:               *subActionNode.ActionID,
					PipelineNodeID:         subActionNode.ID,
					PipelineID:             *targetNode.SubPipelineID,
					RootPipelineID:         task.RootPipelineID,
					ParentPipelineNodeID:   &parentNodeID,
					SubPipelineEntryNodeID: &entryNodeID,
					Depth:                  task.Depth + 1,
					Context:                copyContext(mergedCtx),
					IsOllama:               IsOllamaAction(cache, *subActionNode.ActionID),
				}
				if pushTrackedTask(ctx, st, producer, subTask, logger) {
					pushed++
				}
			}
		}
	}

	// If no tasks were dispatched from any branch (e.g. all conditions failed),
	// this task's branch is a dead end. Signal the sub-pipeline leaf as complete
	// so the parent pipeline can continue regardless of which branch was taken.
	if pushed == 0 {
		signalSubPipelineLeafDone(extractTweetID(task))
	}
}

// pushTrackedTask pushes a task to the queue and, if the task has a RootPipelineID,
// increments the pipeline run tracking counter. Returns true on success.
func pushTrackedTask(
	ctx context.Context,
	st store.Store,
	producer queue.Producer,
	task *queue.Task,
	logger *slog.Logger,
) bool {
	// Increment the tracking counter BEFORE pushing to Redis so that no worker
	// can pop and complete the task before the counter reflects it. If the Redis
	// push subsequently fails the counter is left one too high (pipeline_finished
	// won't fire for that run), but push failures are transient and rare.
	if task.RootPipelineID != 0 {
		tweetID := extractTweetID(task)
		if err := st.IncrementPipelineRunTracking(ctx, task.RootPipelineID, tweetID); err != nil && logger != nil {
			logger.Warn("pipeline: increment run tracking failed",
				"root_pipeline_id", task.RootPipelineID, "tweet_id", tweetID, "error", err)
		}
	}
	if err := producer.PushTask(ctx, task); err != nil {
		if logger != nil {
			logger.Warn("pipeline: push task failed",
				"action_id", task.ActionID, "node_id", task.PipelineNodeID, "error", err)
		}
		return false
	}
	return true
}

// pushContinuationTask serialises and pushes a single downstream task.
// Returns true if the task was successfully enqueued.
func pushContinuationTask(
	ctx context.Context,
	st store.Store,
	producer queue.Producer,
	cache *ActionCache,
	parent *queue.Task,
	node *model.PipelineNode,
	mergedCtx map[string]any,
	logger *slog.Logger,
) bool {
	task := &queue.Task{
		TriggerType:            parent.TriggerType,
		Event:                  parent.Event,
		ActionID:               *node.ActionID,
		PipelineNodeID:         node.ID,
		PipelineID:             parent.PipelineID,
		RootPipelineID:         parent.RootPipelineID,
		ParentPipelineNodeID:   parent.ParentPipelineNodeID,
		SubPipelineEntryNodeID: parent.SubPipelineEntryNodeID,
		Depth:                  parent.Depth + 1,
		Context:                copyContext(mergedCtx),
		IsOllama:               IsOllamaAction(cache, *node.ActionID),
	}
	return pushTrackedTask(ctx, st, producer, task, logger)
}

// evaluateConditionsVerbose logs each condition result individually, then returns
// whether all conditions passed.
func evaluateConditionsVerbose(
	conditions []model.PipelineCondition,
	ctx map[string]any,
	pipelineID, nodeID int64,
	logger *slog.Logger,
) bool {
	if len(conditions) == 0 {
		if logger != nil {
			logger.Info("pipeline: condition node has no conditions — treated as pass",
				"pipeline_id", pipelineID, "node_id", nodeID)
		}
		return true
	}

	for _, c := range conditions {
		// rule_matches / rule_not_matches: c.Key is the rule ID (not a ctx key).
		op := strings.ToLower(c.Operator)
		if op == "rule_matches" || op == "rule_not_matches" {
			result := evalRuleMatch(ctx, c.Key, op == "rule_matches")
			if logger != nil {
				logger.Info("pipeline: rule condition evaluated",
					"pipeline_id", pipelineID, "node_id", nodeID,
					"rule_id", c.Key, "op", c.Operator, "passed", result,
				)
			}
			if !result {
				return false
			}
			continue
		}
		val, ok := ctx[c.Key]
		if !ok {
			if logger != nil {
				logger.Info("pipeline: condition FAILED — key not found in context",
					"pipeline_id", pipelineID, "node_id", nodeID,
					"key", c.Key, "op", c.Operator, "expected", c.Value,
				)
			}
			return false
		}
		actual := fmt.Sprintf("%v", val)
		result := evaluateCondition(c, actual)
		if logger != nil {
			logger.Info("pipeline: condition evaluated",
				"pipeline_id", pipelineID, "node_id", nodeID,
				"key", c.Key, "op", c.Operator,
				"expected", c.Value, "actual", actual,
				"passed", result,
			)
		}
		if !result {
			return false
		}
	}
	return true
}

func evaluateCondition(c model.PipelineCondition, actual string) bool {
	switch strings.ToLower(c.Operator) {
	case "eq", "==":
		return actual == c.Value
	case "neq", "!=":
		return actual != c.Value
	case "contains":
		return strings.Contains(actual, c.Value)
	case "not_contains":
		return !strings.Contains(actual, c.Value)
	case "gt", ">":
		a, aErr := strconv.ParseFloat(actual, 64)
		b, bErr := strconv.ParseFloat(c.Value, 64)
		return aErr == nil && bErr == nil && a > b
	case "gte", ">=":
		a, aErr := strconv.ParseFloat(actual, 64)
		b, bErr := strconv.ParseFloat(c.Value, 64)
		return aErr == nil && bErr == nil && a >= b
	case "lt", "<":
		a, aErr := strconv.ParseFloat(actual, 64)
		b, bErr := strconv.ParseFloat(c.Value, 64)
		return aErr == nil && bErr == nil && a < b
	case "lte", "<=":
		a, aErr := strconv.ParseFloat(actual, 64)
		b, bErr := strconv.ParseFloat(c.Value, 64)
		return aErr == nil && bErr == nil && a <= b
	case "exists":
		return true // key already confirmed present above
	case "not_exists":
		return false // key is present, so not_exists fails
	default:
		return actual == c.Value
	}
}

// evalRuleMatch checks whether the rule ID stored in ruleKey (as a decimal string)
// is present in ctx["rule_ids"] ([]int64). If wantMatch is false it negates the result.
// Returns false when rule_ids is absent or ruleKey cannot be parsed.
func evalRuleMatch(ctx map[string]any, ruleKey string, wantMatch bool) bool {
	targetID, err := strconv.ParseInt(ruleKey, 10, 64)
	if err != nil {
		return false
	}
	ids, _ := ctx["rule_ids"].([]int64)
	for _, id := range ids {
		if id == targetID {
			return wantMatch
		}
	}
	return !wantMatch
}

// ── graph helpers ─────────────────────────────────────────────────────────────

func findStartNode(g *model.PipelineGraph) *model.PipelineNode {
	for _, n := range g.Nodes {
		if n.Type == model.PipelineNodeTypeStart {
			return n
		}
	}
	return nil
}

func findNode(g *model.PipelineGraph, id int64) *model.PipelineNode {
	for _, n := range g.Nodes {
		if n.ID == id {
			return n
		}
	}
	return nil
}

func outgoingEdges(g *model.PipelineGraph, nodeID int64) []*model.PipelineEdge {
	var out []*model.PipelineEdge
	for _, e := range g.Edges {
		if e.SourceNodeID == nodeID {
			out = append(out, e)
		}
	}
	return out
}

// FirstLevelPipelineNodes returns the action nodes directly reachable from the
// pipeline's start node. Used by the run-pipeline handler to determine the initial
// dispatch targets without duplicating the graph-walk logic.
func FirstLevelPipelineNodes(g *model.PipelineGraph) []*model.PipelineNode {
	start := findStartNode(g)
	if start == nil {
		return nil
	}
	return directActionTargets(g, start.ID)
}

// directActionTargets returns action nodes reachable from nodeID in one hop
// (not through condition nodes).
func directActionTargets(g *model.PipelineGraph, nodeID int64) []*model.PipelineNode {
	var out []*model.PipelineNode
	for _, e := range outgoingEdges(g, nodeID) {
		n := findNode(g, e.TargetNodeID)
		if n != nil && n.Type == model.PipelineNodeTypeAction {
			out = append(out, n)
		}
	}
	return out
}

// resolveSubPipelineEntryNodes returns the first reachable action nodes from the
// outgoing edges of fromNodeID, traversing through condition nodes by evaluating
// them against condCtx. Unlike directActionTargets, this correctly handles
// sub-pipelines that begin with condition nodes (e.g. a sequential check chain).
func resolveSubPipelineEntryNodes(g *model.PipelineGraph, fromNodeID int64, condCtx map[string]any, subPipelineID int64, logger *slog.Logger) []*model.PipelineNode {
	var out []*model.PipelineNode
	for _, e := range outgoingEdges(g, fromNodeID) {
		n := findNode(g, e.TargetNodeID)
		if n == nil {
			continue
		}
		out = append(out, resolveEntryFromNode(g, n, condCtx, subPipelineID, logger)...)
	}
	return out
}

// resolveEntryFromNode returns the first reachable action nodes starting from node,
// traversing through condition nodes by evaluating them against condCtx.
func resolveEntryFromNode(g *model.PipelineGraph, node *model.PipelineNode, condCtx map[string]any, pipelineID int64, logger *slog.Logger) []*model.PipelineNode {
	switch node.Type {
	case model.PipelineNodeTypeAction:
		return []*model.PipelineNode{node}
	case model.PipelineNodeTypeCondition:
		passed := evaluateConditionsVerbose(node.Conditions, condCtx, pipelineID, node.ID, logger)
		wantHandle := "pass"
		if !passed {
			wantHandle = "fail"
		}
		var out []*model.PipelineNode
		for _, condEdge := range outgoingEdges(g, node.ID) {
			h := condEdge.SourceHandle
			if h == "" {
				h = "pass"
			}
			if h != wantHandle {
				continue
			}
			next := findNode(g, condEdge.TargetNodeID)
			if next == nil {
				continue
			}
			out = append(out, resolveEntryFromNode(g, next, condCtx, pipelineID, logger)...)
		}
		return out
	}
	return nil
}

// findTerminalNodeIDs returns IDs of nodes with no outgoing edges (excluding start nodes).
func findTerminalNodeIDs(g *model.PipelineGraph) []int64 {
	hasOutgoing := make(map[int64]bool)
	for _, e := range g.Edges {
		hasOutgoing[e.SourceNodeID] = true
	}
	var out []int64
	for _, n := range g.Nodes {
		if n.Type != model.PipelineNodeTypeStart && !hasOutgoing[n.ID] {
			out = append(out, n.ID)
		}
	}
	return out
}

// extractTweetID unmarshals the TweetEvent from task.Event to get the tweet ID.
func extractTweetID(task *queue.Task) string {
	var ev queue.TweetEvent
	_ = json.Unmarshal(task.Event, &ev)
	return ev.TweetID
}

// copyContext returns a shallow copy of a context map.
func copyContext(src map[string]any) map[string]any {
	dst := make(map[string]any, len(src))
	for k, v := range src {
		dst[k] = v
	}
	return dst
}
