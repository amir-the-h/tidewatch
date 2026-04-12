// web/src/stores/queues.ts
import { defineStore } from 'pinia'
import { ref, computed } from 'vue'
import { getQueue, type WorkerInfo } from '../api/queue'

export type { WorkerInfo }

interface PendingSnapshot {
  ts:     number
  tasks:  number
  ollama: number
}

const HISTORY_WINDOW_MS = 60_000
const MIN_WINDOW_MS     = 5_000

export const useQueuesStore = defineStore('queues', () => {
  const pendingTasks   = ref(0)
  const pendingOllama  = ref(0)
  const workers        = ref<WorkerInfo[]>([])
  const pendingHistory = ref<PendingSnapshot[]>([])

  function appendSnapshot(tasks: number, ollama: number) {
    const now = Date.now()
    pendingHistory.value.push({ ts: now, tasks, ollama })
    const cutoff = now - HISTORY_WINDOW_MS
    pendingHistory.value = pendingHistory.value.filter(s => s.ts >= cutoff)
  }

  // Called on initial HTTP fetch (onMounted in QueueView).
  function setFromResponse(pt: number, po: number, w: WorkerInfo[]) {
    pendingTasks.value  = pt
    pendingOllama.value = po
    workers.value       = w
    appendSnapshot(pt, po)
  }

  // Called on worker_stats WebSocket event.
  function setWorkers(w: WorkerInfo[]) {
    workers.value = w
  }

  // Called on queue_update WebSocket event to refresh global pending counts.
  async function fetchCounts() {
    try {
      const data = await getQueue()
      pendingTasks.value  = data.pending_tasks
      pendingOllama.value = data.pending_ollama
      appendSnapshot(data.pending_tasks, data.pending_ollama)
    } catch { /* ignore */ }
  }

  function computeEta(current: number, getVal: (s: PendingSnapshot) => number): number | null {
    const h = pendingHistory.value
    if (h.length < 2) return null
    const oldest = h[0]
    const newest = h[h.length - 1]
    const windowMs = newest.ts - oldest.ts
    if (windowMs < MIN_WINDOW_MS) return null
    if (current <= 0) return null
    const delta = getVal(oldest) - getVal(newest)
    if (delta <= 0) return null                        // growing or flat
    const ratePerMs = delta / windowMs
    return current / ratePerMs
  }

  const etaTasks  = computed(() => computeEta(pendingTasks.value,  s => s.tasks))
  const etaOllama = computed(() => computeEta(pendingOllama.value, s => s.ollama))

  return {
    pendingTasks, pendingOllama, workers,
    etaTasks, etaOllama,
    setFromResponse, setWorkers, fetchCounts,
  }
})
