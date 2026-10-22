// Auto-layout for the Pipeline builder: a layered left-to-right layout (dagre), so nodes are never
// placed by hand. Sizes come from Vue Flow's measured dimensions, falling back to the node type's
// fixed width (see the node components) before the first measurement.
import dagre from '@dagrejs/dagre'
import type { Edge, GraphNode } from '@vue-flow/core'

const fallbackSize: Record<string, { width: number; height: number }> = {
  start:     { width: 160, height: 64 },
  action:    { width: 240, height: 64 },
  pipeline:  { width: 240, height: 64 },
  condition: { width: 240, height: 88 },
}

// Pass edges sort above fail edges out of a condition, matching the handle order on the node.
const handleRank = (h: string | null | undefined) => (h === 'fail' ? 1 : 0)

/** Returns a position per node id. */
export function layoutGraph(nodes: GraphNode[], edges: Edge[]) {
  const g = new dagre.graphlib.Graph({ multigraph: true })
  g.setGraph({ rankdir: 'LR', nodesep: 32, ranksep: 96, marginx: 24, marginy: 24 })
  g.setDefaultEdgeLabel(() => ({}))

  for (const n of nodes) {
    const size = fallbackSize[n.type ?? 'action'] ?? fallbackSize.action
    g.setNode(n.id, {
      width:  n.dimensions.width  || size.width,
      height: n.dimensions.height || size.height,
    })
  }
  const sorted = [...edges].sort((a, b) => handleRank(a.sourceHandle) - handleRank(b.sourceHandle))
  for (const e of sorted) {
    if (g.hasNode(e.source) && g.hasNode(e.target)) g.setEdge(e.source, e.target, {}, e.id)
  }

  dagre.layout(g)

  const positions: Record<string, { x: number; y: number }> = {}
  for (const n of nodes) {
    const p = g.node(n.id)
    // dagre positions node centres; Vue Flow positions top-left corners.
    if (p) positions[n.id] = { x: Math.round(p.x - p.width / 2), y: Math.round(p.y - p.height / 2) }
  }
  return positions
}
