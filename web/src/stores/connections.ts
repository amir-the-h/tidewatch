import { ref, computed } from 'vue'
import { defineStore } from 'pinia'
import { listEntityConnections, deleteEntityConnections, type EntityConnection } from '../api/connections'

export const useConnectionsStore = defineStore('connections', () => {
  const connections = ref<EntityConnection[]>([])
  const loading = ref(false)

  async function fetchConnections() {
    loading.value = true
    try {
      const resp = await listEntityConnections()
      connections.value = resp.connections
    } finally {
      loading.value = false
    }
  }

  async function resetConnections() {
    await deleteEntityConnections()
    connections.value = []
  }

  // Builds vis-network nodes/edges for a given minimum weight threshold.
  const graphData = computed(() => (minWeight: number) => {
    const filtered = connections.value.filter(c => c.weight >= minWeight)

    // Collect unique node IDs from filtered edges only.
    const nodeMap = new Map<number, { id: number; label: string; type: string; totalWeight: number }>()
    for (const c of filtered) {
      for (const [id, name, type] of [[c.from_id, c.from_name, c.from_type], [c.to_id, c.to_name, c.to_type]] as [number, string, string][]) {
        const existing = nodeMap.get(id)
        if (existing) {
          existing.totalWeight += c.weight
        } else {
          nodeMap.set(id, { id, label: name, type, totalWeight: c.weight })
        }
      }
    }

    // Scale node size between 10 and 40 based on totalWeight.
    const weights = [...nodeMap.values()].map(n => n.totalWeight)
    const maxW = Math.max(...weights, 1)

    const nodes = [...nodeMap.values()].map(n => ({
      id: n.id,
      label: n.label,
      title: `${n.label} (${n.type}) — ${n.totalWeight} connections`,
      size: 10 + Math.round((n.totalWeight / maxW) * 30),
    }))

    const edges = filtered.map(c => ({
      from: c.from_id,
      to: c.to_id,
      value: c.weight,
      title: `Co-occurrences: ${c.weight}`,
    }))

    return { nodes, edges }
  })

  return { connections, loading, fetchConnections, resetConnections, graphData }
})
