// web/src/stores/queues.ts
import { defineStore } from 'pinia'
import { ref, computed } from 'vue'

export interface QueueEntry {
  action_id: number
  action_name: string
  pipeline_name: string
  pipeline_type: string
  pending: number
  workers: Array<{ id: number; backend: string }>
  throughput: number  // items/sec; 0 = not yet established
}

export interface WorkerStats {
  total: number
  active: number
  idle: number
  active_workers: Array<{ id: number; action: string; pipeline: string; backend: string }>
  action_throughput?: Record<number, number>
}

export const useQueuesStore = defineStore('queues', () => {
  const queues = ref<QueueEntry[]>([])
  const workerStats = ref<WorkerStats | null>(null)

  // Set the full list from the initial HTTP fetch (onMounted in QueueView).
  function setQueues(entries: any[]) {
    queues.value = entries.map(e => ({
      ...e,
      workers: (e.workers ?? []).map((w: any) => ({ id: w.id, backend: w.backend ?? '' })),
      throughput: e.throughput ?? 0,
    }))
  }

  // Update a single action's pending count from a WS queue_update event.
  function setQueueEntry(payload: { action_id: number; pending: number }) {
    const entry = queues.value.find(q => q.action_id === payload.action_id)
    if (entry) {
      entry.pending = payload.pending
    }
  }

  // Update pool-level stats and cross-update the workers + throughput fields on
  // each queue entry so both columns stay live without a full page refresh.
  function setWorkerStats(stats: WorkerStats) {
    workerStats.value = stats
    const activeWorkers = stats.active_workers ?? []
    const throughputMap = stats.action_throughput ?? {}
    for (const entry of queues.value) {
      entry.workers = activeWorkers
        .filter(w => w.action === entry.action_name)
        .map(w => ({ id: w.id, backend: w.backend ?? '' }))
      // Only update throughput if the WS event includes the map (avoids zeroing on old events).
      if (stats.action_throughput !== undefined) {
        entry.throughput = throughputMap[entry.action_id] ?? 0
      }
    }
  }

  const totalPending = computed(() =>
    queues.value.reduce((sum, q) => sum + q.pending, 0)
  )

  return { queues, workerStats, totalPending, setQueues, setQueueEntry, setWorkerStats }
})
