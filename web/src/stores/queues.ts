// web/src/stores/queues.ts
import { defineStore } from 'pinia'
import { ref } from 'vue'
import type { WorkerInfo } from '../api/queue'

export type { WorkerInfo }

export const useQueuesStore = defineStore('queues', () => {
  const pending = ref(0)
  const workers = ref<WorkerInfo[]>([])

  // Called on initial HTTP fetch (onMounted in QueueView).
  function setFromResponse(p: number, w: WorkerInfo[]) {
    pending.value = p
    workers.value = w
  }

  // Called on queue_update WebSocket event.
  function setQueuePending(p: number) {
    pending.value = p
  }

  // Called on worker_stats WebSocket event.
  function setWorkers(w: WorkerInfo[]) {
    workers.value = w
  }

  return { pending, workers, setFromResponse, setQueuePending, setWorkers }
})
