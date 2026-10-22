import { ref, computed } from 'vue'
import { defineStore } from 'pinia'
import { getEntitySubgraph, deleteEntityConnections, type EntityConnection } from '../api/connections'

export const useConnectionsStore = defineStore('connections', () => {
  const subgraph = ref<EntityConnection[]>([])
  const loading = ref(false)

  async function fetchSubgraph(entityID: number, depth: number, minWeight: number) {
    loading.value = true
    try {
      const resp = await getEntitySubgraph(entityID, depth, minWeight)
      subgraph.value = resp.connections
    } finally {
      loading.value = false
    }
  }

  async function resetConnections() {
    await deleteEntityConnections()
    subgraph.value = []
  }

  // Builds vis-network nodes/edges from the current subgraph.
  const graphData = computed(() => () => {
    const nodeMap = new Map<number, { id: number; label: string; type: string; totalWeight: number }>()
    for (const c of subgraph.value) {
      for (const [id, name, type] of [[c.from_id, c.from_name, c.from_type], [c.to_id, c.to_name, c.to_type]] as [number, string, string][]) {
        const existing = nodeMap.get(id)
        if (existing) {
          existing.totalWeight += c.weight
        } else {
          nodeMap.set(id, { id, label: name, type, totalWeight: c.weight })
        }
      }
    }

    const weights = [...nodeMap.values()].map(n => n.totalWeight)
    const maxW = Math.max(...weights, 1)

    const nodes = [...nodeMap.values()].map(n => ({
      id: n.id,
      label: n.label,
      type: n.type,
      title: `${n.label} (${n.type}) — ${n.totalWeight} connections`,
      size: 10 + Math.round((n.totalWeight / maxW) * 30),
    }))

    const edges = subgraph.value.map(c => ({
      from: c.from_id,
      to: c.to_id,
      value: c.weight,
      title: `Co-occurrences: ${c.weight}`,
    }))

    return { nodes, edges }
  })

  return { subgraph, loading, fetchSubgraph, resetConnections, graphData }
})
