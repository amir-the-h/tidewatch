package worker

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strconv"
	"strings"

	"github.com/amir-the-h/iran-watcher/internal/model"
	"github.com/amir-the-h/iran-watcher/internal/queue"
	"github.com/amir-the-h/iran-watcher/internal/store"
)

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
) error {
	links, err := st.ListActiveTriggerPipelineLinks(ctx, triggerType)
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

	pushed := 0

	// Walk all edges from Start and dispatch based on node type.
	// This mirrors PushContinuationTasks so the builder can place any
	// node type directly after Start, not only action nodes.
	for _, edge := range outgoingEdges(graph, startNode.ID) {
		node := findNode(graph, edge.TargetNodeID)
		if node == nil {
			continue
		}
		switch node.Type {
		case model.PipelineNodeTypeAction:
			if node.ActionID == nil {
				if logger != nil {
					logger.Warn("pipeline: action node has no action_id — skipped",
						"pipeline_id", pipelineID, "node_id", node.ID)
				}
				continue
			}
			if cache != nil && cache.Get(*node.ActionID) == nil {
				if logger != nil {
					logger.Info("pipeline: skipping task for inactive action",
						"pipeline_id", pipelineID, "node_id", node.ID,
						"action_id", *node.ActionID)
				}
				continue
			}
			task := &queue.Task{
				TriggerType:    triggerType,
				Event:          eventPayload,
				ActionID:       *node.ActionID,
				PipelineNodeID: node.ID,
				PipelineID:     pipelineID,
				Context:        map[string]any{},
			}
			if pushErr := producer.PushTask(ctx, task); pushErr != nil {
				if logger != nil {
					logger.Warn("pipeline: push initial task failed",
						"pipeline_id", pipelineID, "action_id", *node.ActionID,
						"node_id", node.ID, "error", pushErr)
				}
			} else {
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
				continue
			}
			subGraph, subErr := st.GetPipelineGraph(ctx, *node.SubPipelineID)
			if subErr != nil {
				if logger != nil {
					logger.Warn("pipeline: load sub-pipeline graph failed",
						"sub_pipeline_id", *node.SubPipelineID, "error", subErr)
				}
				continue
			}
			terminalIDs := findTerminalNodeIDs(subGraph)
			if len(terminalIDs) == 0 {
				if logger != nil {
					logger.Warn("pipeline: sub-pipeline has no terminal nodes — skipped",
						"sub_pipeline_id", *node.SubPipelineID)
				}
				continue
			}
			for _, leafID := range terminalIDs {
				if insErr := st.InsertPipelineNodeExecution(ctx, node.ID, tweetID, leafID); insErr != nil && logger != nil {
					logger.Warn("pipeline: insert pipeline node execution failed",
						"parent_node_id", node.ID, "leaf_node_id", leafID, "error", insErr)
				}
			}
			subStartNode := findStartNode(subGraph)
			if subStartNode == nil {
				continue
			}
			parentNodeID := node.ID
			for _, subActionNode := range directActionTargets(subGraph, subStartNode.ID) {
				if subActionNode.ActionID == nil {
					continue
				}
				if cache != nil && cache.Get(*subActionNode.ActionID) == nil {
					continue
				}
				subTask := &queue.Task{
					TriggerType:          triggerType,
					Event:                eventPayload,
					ActionID:             *subActionNode.ActionID,
					PipelineNodeID:       subActionNode.ID,
					PipelineID:           *node.SubPipelineID,
					ParentPipelineNodeID: &parentNodeID,
					Context:              map[string]any{},
				}
				if pushErr := producer.PushTask(ctx, subTask); pushErr != nil && logger != nil {
					logger.Warn("pipeline: push sub-pipeline initial task failed",
						"action_id", *subActionNode.ActionID, "error", pushErr)
				} else {
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
			if logger != nil {
				logger.Warn("pipeline: condition node directly after start — cannot evaluate without prior outputs, skipped",
					"pipeline_id", pipelineID, "node_id", node.ID)
			}
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

	if len(edges) == 0 {
		if logger != nil {
			logger.Info("pipeline: no outgoing edges — pipeline ends here",
				"pipeline_id", pipelineID, "node_id", task.PipelineNodeID)
		}
		// If this task is part of a sub-pipeline, check if all leaves are done.
		if task.ParentPipelineNodeID != nil {
			tweetID := extractTweetID(task)
			allDone, err := st.MarkPipelineNodeLeafComplete(ctx, *task.ParentPipelineNodeID, tweetID, task.PipelineNodeID)
			if err != nil && logger != nil {
				logger.Warn("pipeline: mark leaf complete failed",
					"parent_node_id", *task.ParentPipelineNodeID,
					"leaf_node_id", task.PipelineNodeID, "error", err)
			}
			if allDone {
				parentPipelineID, pidErr := st.GetPipelineIDForNode(ctx, *task.ParentPipelineNodeID)
				if pidErr != nil && logger != nil {
					logger.Warn("pipeline: get parent pipeline id failed", "error", pidErr)
				} else {
					freshCtx := copyContext(mergedCtx)
					// parentTask is a synthetic task representing the pipeline-type node
					// that spawned this sub-pipeline. ActionID is 0 because pipeline nodes
					// are not actions; it is only used for log context inside
					// PushContinuationTasks.
					parentTask := &queue.Task{
						TriggerType:    task.TriggerType,
						Event:          task.Event,
						PipelineNodeID: *task.ParentPipelineNodeID,
						PipelineID:     parentPipelineID,
					}
					PushContinuationTasks(ctx, st, producer, cache, parentTask, freshCtx, logger)
				}
			}
		}
		return
	}

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
			pushContinuationTask(ctx, producer, task, targetNode, mergedCtx, logger)

		case model.PipelineNodeTypeCondition:
			if logger != nil {
				logger.Info("pipeline: evaluating condition node",
					"pipeline_id", pipelineID,
					"node_id", targetNode.ID,
					"conditions", len(targetNode.Conditions),
				)
			}
			passed := evaluateConditionsVerbose(targetNode.Conditions, mergedCtx, pipelineID, targetNode.ID, logger)
			if !passed {
				if logger != nil {
					logger.Info("pipeline: condition node FAILED — branch will not execute",
						"pipeline_id", pipelineID,
						"node_id", targetNode.ID,
					)
				}
				continue
			}
			if logger != nil {
				logger.Info("pipeline: condition node PASSED — following downstream edges",
					"pipeline_id", pipelineID,
					"node_id", targetNode.ID,
				)
			}
			// Conditions passed — push action/pipeline nodes that follow this condition node.
			for _, condEdge := range outgoingEdges(graph, targetNode.ID) {
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
					pushContinuationTask(ctx, producer, task, nextNode, mergedCtx, logger)
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
					terminalIDs := findTerminalNodeIDs(subGraph)
					if len(terminalIDs) == 0 {
						if logger != nil {
							logger.Warn("pipeline: sub-pipeline has no terminal nodes (post-condition) — skipped",
								"sub_pipeline_id", *nextNode.SubPipelineID)
						}
						continue
					}
					for _, leafID := range terminalIDs {
						if insErr := st.InsertPipelineNodeExecution(ctx, nextNode.ID, extractTweetID(task), leafID); insErr != nil && logger != nil {
							logger.Warn("pipeline: insert pipeline node execution failed (post-condition)",
								"parent_node_id", nextNode.ID, "leaf_node_id", leafID, "error", insErr)
						}
					}
					subStartNode := findStartNode(subGraph)
					if subStartNode == nil {
						continue
					}
					parentNodeID := nextNode.ID
					for _, subActionNode := range directActionTargets(subGraph, subStartNode.ID) {
						if subActionNode.ActionID == nil {
							continue
						}
						if cache != nil && cache.Get(*subActionNode.ActionID) == nil {
							continue
						}
						downstream := copyContext(mergedCtx)
						subTask := &queue.Task{
							TriggerType:          task.TriggerType,
							Event:                task.Event,
							ActionID:             *subActionNode.ActionID,
							PipelineNodeID:       subActionNode.ID,
							PipelineID:           *nextNode.SubPipelineID,
							ParentPipelineNodeID: &parentNodeID,
							Context:              downstream,
						}
						if pushErr := producer.PushTask(ctx, subTask); pushErr != nil && logger != nil {
							logger.Warn("pipeline: push sub-pipeline initial task failed (post-condition)",
								"action_id", *subActionNode.ActionID, "error", pushErr)
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
			terminalIDs := findTerminalNodeIDs(subGraph)
			if len(terminalIDs) == 0 {
				if logger != nil {
					logger.Warn("pipeline: sub-pipeline has no terminal nodes — skipped",
						"sub_pipeline_id", *targetNode.SubPipelineID)
				}
				continue
			}
			for _, leafID := range terminalIDs {
				if err := st.InsertPipelineNodeExecution(ctx, targetNode.ID, extractTweetID(task), leafID); err != nil && logger != nil {
					logger.Warn("pipeline: insert pipeline node execution failed",
						"parent_node_id", targetNode.ID, "leaf_node_id", leafID, "error", err)
				}
			}
			subStartNode := findStartNode(subGraph)
			if subStartNode == nil {
				continue
			}
			parentNodeID := targetNode.ID
			for _, subActionNode := range directActionTargets(subGraph, subStartNode.ID) {
				if subActionNode.ActionID == nil {
					continue
				}
				if cache != nil && cache.Get(*subActionNode.ActionID) == nil {
					continue
				}
				downstream := copyContext(mergedCtx)
				subTask := &queue.Task{
					TriggerType:          task.TriggerType,
					Event:                task.Event,
					ActionID:             *subActionNode.ActionID,
					PipelineNodeID:       subActionNode.ID,
					PipelineID:           *targetNode.SubPipelineID,
					ParentPipelineNodeID: &parentNodeID,
					Context:              downstream,
				}
				if err := producer.PushTask(ctx, subTask); err != nil && logger != nil {
					logger.Warn("pipeline: push sub-pipeline initial task failed",
						"action_id", *subActionNode.ActionID, "error", err)
				}
			}
		}
	}
}

// pushContinuationTask serialises and pushes a single downstream task.
func pushContinuationTask(
	ctx context.Context,
	producer queue.Producer,
	parent *queue.Task,
	node *model.PipelineNode,
	mergedCtx map[string]any,
	logger *slog.Logger,
) {
	task := &queue.Task{
		TriggerType:          parent.TriggerType,
		Event:                parent.Event,
		ActionID:             *node.ActionID,
		PipelineNodeID:       node.ID,
		PipelineID:           parent.PipelineID,
		ParentPipelineNodeID: parent.ParentPipelineNodeID,
		Context:              copyContext(mergedCtx),
	}
	if err := producer.PushTask(ctx, task); err != nil && logger != nil {
		logger.Warn("pipeline: push continuation task failed",
			"action_id", *node.ActionID, "node_id", node.ID, "error", err)
	}
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
