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

// PushInitialChainTasks dispatches tasks for the first-level action nodes in all active
// chains linked to triggerType. Called by the stream listener and search client when a
// new tweet arrives.
func PushInitialChainTasks(
	ctx context.Context,
	st store.Store,
	producer queue.Producer,
	triggerType string,
	ev queue.TweetEvent,
	logger *slog.Logger,
) error {
	links, err := st.ListActiveTriggerChainLinks(ctx, triggerType)
	if err != nil {
		return fmt.Errorf("chain: list active trigger links for %q: %w", triggerType, err)
	}

	if logger != nil {
		logger.Info("chain: initial dispatch",
			"trigger", triggerType,
			"tweet_id", ev.TweetID,
			"active_chain_links", len(links),
		)
	}

	if len(links) == 0 {
		if logger != nil {
			logger.Info("chain: no active chain links for trigger — tweet will not be processed",
				"trigger", triggerType,
				"tweet_id", ev.TweetID,
			)
		}
		return nil
	}

	eventPayload, err := json.Marshal(ev)
	if err != nil {
		return fmt.Errorf("chain: marshal tweet event: %w", err)
	}

	for _, link := range links {
		graph, err := st.GetChainGraph(ctx, link.ChainID)
		if err != nil {
			if logger != nil {
				logger.Warn("chain: load graph failed", "chain_id", link.ChainID, "error", err)
			}
			continue
		}

		if logger != nil {
			logger.Info("chain: walking graph",
				"chain_id", link.ChainID,
				"nodes", len(graph.Nodes),
				"edges", len(graph.Edges),
			)
		}

		startNode := findStartNode(graph)
		if startNode == nil {
			if logger != nil {
				logger.Warn("chain: no start node found — chain skipped", "chain_id", link.ChainID)
			}
			continue
		}

		actionNodes := directActionTargets(graph, startNode.ID)
		if logger != nil {
			logger.Info("chain: start node found",
				"chain_id", link.ChainID,
				"start_node_id", startNode.ID,
				"direct_action_nodes", len(actionNodes),
			)
		}

		// Push tasks for action nodes directly reachable from Start.
		// Condition nodes cannot be evaluated yet (no action outputs exist),
		// so we only follow edges that lead directly to action nodes.
		for _, actionNode := range actionNodes {
			if actionNode.ActionID == nil {
				if logger != nil {
					logger.Warn("chain: action node has no action_id — skipped",
						"chain_id", link.ChainID, "node_id", actionNode.ID)
				}
				continue
			}
			task := &queue.Task{
				TriggerType: triggerType,
				Event:       eventPayload,
				ActionID:    *actionNode.ActionID,
				ChainNodeID: actionNode.ID,
				ChainID:     link.ChainID,
				Context:     map[string]any{},
			}
			if pushErr := producer.PushTask(ctx, task); pushErr != nil {
				if logger != nil {
					logger.Warn("chain: push initial task failed",
						"chain_id", link.ChainID, "action_id", *actionNode.ActionID,
						"node_id", actionNode.ID, "error", pushErr)
				}
			} else if logger != nil {
				logger.Info("chain: queued initial task",
					"chain_id", link.ChainID,
					"action_id", *actionNode.ActionID,
					"node_id", actionNode.ID,
					"tweet_id", ev.TweetID,
				)
			}
		}
	}

	return nil
}

// PushContinuationTasks is called by the worker after successfully running an action.
// It evaluates outgoing edges from task.ChainNodeID and pushes tasks for downstream
// action nodes whose conditions are satisfied.
func PushContinuationTasks(
	ctx context.Context,
	st store.Store,
	producer queue.Producer,
	task *queue.Task,
	mergedCtx map[string]any,
	logger *slog.Logger,
) {
	if task.ChainNodeID == 0 {
		if logger != nil {
			logger.Info("chain: continuation skipped — task has no chain_node_id (not a chain task)",
				"action_id", task.ActionID,
			)
		}
		return
	}

	chainID := task.ChainID
	if chainID == 0 {
		var err error
		chainID, err = st.GetChainIDForNode(ctx, task.ChainNodeID)
		if err != nil {
			if logger != nil {
				logger.Warn("chain: get chain id for node failed",
					"node_id", task.ChainNodeID, "error", err)
			}
			return
		}
	}

	graph, err := st.GetChainGraph(ctx, chainID)
	if err != nil {
		if logger != nil {
			logger.Warn("chain: load graph for continuation failed",
				"chain_id", chainID, "error", err)
		}
		return
	}

	edges := outgoingEdges(graph, task.ChainNodeID)
	if logger != nil {
		// Log context keys so we can see what the condition will evaluate against.
		ctxKeys := make([]string, 0, len(mergedCtx))
		for k := range mergedCtx {
			ctxKeys = append(ctxKeys, k)
		}
		logger.Info("chain: continuing from node",
			"chain_id", chainID,
			"completed_node_id", task.ChainNodeID,
			"action_id", task.ActionID,
			"outgoing_edges", len(edges),
			"context_keys", ctxKeys,
		)
	}

	if len(edges) == 0 {
		if logger != nil {
			logger.Info("chain: no outgoing edges — chain ends here",
				"chain_id", chainID, "node_id", task.ChainNodeID)
		}
		return
	}

	for _, edge := range edges {
		targetNode := findNode(graph, edge.TargetNodeID)
		if targetNode == nil {
			if logger != nil {
				logger.Warn("chain: target node not found in graph",
					"chain_id", chainID,
					"source_node_id", task.ChainNodeID,
					"target_node_id", edge.TargetNodeID,
				)
			}
			continue
		}

		if logger != nil {
			logger.Info("chain: evaluating edge",
				"chain_id", chainID,
				"source_node_id", edge.SourceNodeID,
				"target_node_id", edge.TargetNodeID,
				"target_type", targetNode.Type,
			)
		}

		switch targetNode.Type {
		case model.ChainNodeTypeAction:
			if targetNode.ActionID == nil {
				if logger != nil {
					logger.Warn("chain: action node has no action_id — skipped",
						"chain_id", chainID, "node_id", targetNode.ID)
				}
				continue
			}
			if logger != nil {
				logger.Info("chain: pushing downstream action task",
					"chain_id", chainID,
					"action_id", *targetNode.ActionID,
					"node_id", targetNode.ID,
				)
			}
			pushContinuationTask(ctx, producer, task, targetNode, mergedCtx, logger)

		case model.ChainNodeTypeCondition:
			if logger != nil {
				logger.Info("chain: evaluating condition node",
					"chain_id", chainID,
					"node_id", targetNode.ID,
					"conditions", len(targetNode.Conditions),
				)
			}
			passed := evaluateConditionsVerbose(targetNode.Conditions, mergedCtx, chainID, targetNode.ID, logger)
			if !passed {
				if logger != nil {
					logger.Info("chain: condition node FAILED — branch will not execute",
						"chain_id", chainID,
						"node_id", targetNode.ID,
					)
				}
				continue
			}
			if logger != nil {
				logger.Info("chain: condition node PASSED — following downstream edges",
					"chain_id", chainID,
					"node_id", targetNode.ID,
				)
			}
			// Conditions passed — push action nodes that follow this condition node.
			for _, condEdge := range outgoingEdges(graph, targetNode.ID) {
				nextNode := findNode(graph, condEdge.TargetNodeID)
				if nextNode == nil || nextNode.Type != model.ChainNodeTypeAction || nextNode.ActionID == nil {
					if logger != nil && nextNode != nil {
						logger.Warn("chain: unexpected node type after condition — expected action",
							"chain_id", chainID,
							"node_id", nextNode.ID,
							"type", nextNode.Type,
						)
					}
					continue
				}
				if logger != nil {
					logger.Info("chain: pushing downstream action task after condition",
						"chain_id", chainID,
						"action_id", *nextNode.ActionID,
						"node_id", nextNode.ID,
					)
				}
				pushContinuationTask(ctx, producer, task, nextNode, mergedCtx, logger)
			}
		}
	}
}

// pushContinuationTask serialises and pushes a single downstream task.
func pushContinuationTask(
	ctx context.Context,
	producer queue.Producer,
	parent *queue.Task,
	node *model.ChainNode,
	mergedCtx map[string]any,
	logger *slog.Logger,
) {
	// Copy context so downstream tasks don't share a mutable map.
	downstream := make(map[string]any, len(mergedCtx))
	for k, v := range mergedCtx {
		downstream[k] = v
	}
	task := &queue.Task{
		TriggerType: parent.TriggerType,
		Event:       parent.Event,
		ActionID:    *node.ActionID,
		ChainNodeID: node.ID,
		ChainID:     parent.ChainID,
		Context:     downstream,
	}
	if err := producer.PushTask(ctx, task); err != nil && logger != nil {
		logger.Warn("chain: push continuation task failed",
			"action_id", *node.ActionID, "node_id", node.ID, "error", err)
	}
}

// evaluateConditionsVerbose logs each condition result individually, then returns
// whether all conditions passed.
func evaluateConditionsVerbose(
	conditions []model.ChainCondition,
	ctx map[string]any,
	chainID, nodeID int64,
	logger *slog.Logger,
) bool {
	if len(conditions) == 0 {
		if logger != nil {
			logger.Info("chain: condition node has no conditions — treated as pass",
				"chain_id", chainID, "node_id", nodeID)
		}
		return true
	}

	for _, c := range conditions {
		val, ok := ctx[c.Key]
		if !ok {
			if logger != nil {
				logger.Info("chain: condition FAILED — key not found in context",
					"chain_id", chainID, "node_id", nodeID,
					"key", c.Key, "op", c.Operator, "expected", c.Value,
				)
			}
			return false
		}
		actual := fmt.Sprintf("%v", val)
		result := evaluateCondition(c, actual)
		if logger != nil {
			logger.Info("chain: condition evaluated",
				"chain_id", chainID, "node_id", nodeID,
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

// evaluateConditions is the non-verbose variant used internally when no logger is needed.
func evaluateConditions(conditions []model.ChainCondition, ctx map[string]any) bool {
	return evaluateConditionsVerbose(conditions, ctx, 0, 0, nil)
}

func evaluateCondition(c model.ChainCondition, actual string) bool {
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

func findStartNode(g *model.ChainGraph) *model.ChainNode {
	for _, n := range g.Nodes {
		if n.Type == model.ChainNodeTypeStart {
			return n
		}
	}
	return nil
}

func findNode(g *model.ChainGraph, id int64) *model.ChainNode {
	for _, n := range g.Nodes {
		if n.ID == id {
			return n
		}
	}
	return nil
}

func outgoingEdges(g *model.ChainGraph, nodeID int64) []*model.ChainEdge {
	var out []*model.ChainEdge
	for _, e := range g.Edges {
		if e.SourceNodeID == nodeID {
			out = append(out, e)
		}
	}
	return out
}

// FirstLevelActionNodes returns the action nodes directly reachable from the
// chain's start node. Used by the run-chain handler to determine the initial
// dispatch targets without duplicating the graph-walk logic.
func FirstLevelActionNodes(g *model.ChainGraph) []*model.ChainNode {
	start := findStartNode(g)
	if start == nil {
		return nil
	}
	return directActionTargets(g, start.ID)
}

// directActionTargets returns action nodes reachable from nodeID in one hop
// (not through condition nodes).
func directActionTargets(g *model.ChainGraph, nodeID int64) []*model.ChainNode {
	var out []*model.ChainNode
	for _, e := range outgoingEdges(g, nodeID) {
		n := findNode(g, e.TargetNodeID)
		if n != nil && n.Type == model.ChainNodeTypeAction {
			out = append(out, n)
		}
	}
	return out
}
