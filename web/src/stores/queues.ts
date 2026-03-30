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
}

export interface WorkerStats {
  total: number
  active: number
  idle: number
  active_workers: Array<{ id: number; action: string; pipeline: string; backend: string }>
}

export const useQueuesStore = defineStore('queues', () => {
  const queues = ref<QueueEntry[]>([])
  const workerStats = ref<WorkerStats | null>(null)

  // Set the full list from the initial HTTP fetch (onMounted in QueueView).
  function setQueues(entries: QueueEntry[]) {
    queues.value = entries.map(e => ({
      ...e,
      workers: (e.workers ?? []).map(w => ({ id: w.id, backend: w.backend ?? '' })),
    }))
  }

  // Update a single action's pending count from a WS queue_update event.
  function setQueueEntry(payload: { action_id: number; pending: number }) {
    const entry = queues.value.find(q => q.action_id === payload.action_id)
    if (entry) {
      entry.pending = payload.pending
    }
  }

  // Update pool-level stats and cross-update the workers field on each queue
  // entry so the Worker column stays live without a full page refresh.
  function setWorkerStats(stats: WorkerStats) {
    workerStats.value = stats
    const activeWorkers = stats.active_workers ?? []
    for (const entry of queues.value) {
      entry.workers = activeWorkers
        .filter(w => w.action === entry.action_name)
        .map(w => ({ id: w.id, backend: w.backend ?? '' }))
    }
  }

  const totalPending = computed(() =>
    queues.value.reduce((sum, q) => sum + q.pending, 0)
  )

  return { queues, workerStats, totalPending, setQueues, setQueueEntry, setWorkerStats }
})
