// web/src/stores/queues.ts
import { defineStore } from 'pinia'
import { ref } from 'vue'
import type { WorkerInfo } from '../api/queue'

export type { WorkerInfo }

export const useQueuesStore = defineStore('queues', () => {
  const pendingTasks  = ref(0)
  const pendingOllama = ref(0)
  const workers       = ref<WorkerInfo[]>([])

  // Called on initial HTTP fetch (onMounted in QueueView).
  function setFromResponse(pt: number, po: number, w: WorkerInfo[]) {
    pendingTasks.value  = pt
    pendingOllama.value = po
    workers.value       = w
  }

  // Called on worker_stats WebSocket event.
  function setWorkers(w: WorkerInfo[]) {
    workers.value = w
  }

  return { pendingTasks, pendingOllama, workers, setFromResponse, setWorkers }
})
