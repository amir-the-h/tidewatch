// web/src/stores/sourceStatus.ts
import { defineStore } from 'pinia'
import { computed, ref } from 'vue'
import * as api from '../api/sourceStatus'
import type { SourceStatus } from '../api/sourceStatus'

export const useSourceStatusStore = defineStore('sourceStatus', () => {
  const statuses = ref<Record<number, SourceStatus>>({})

  const active  = computed(() => Object.values(statuses.value).filter(s => s.active))
  // A stream waiting to reconnect is failing too.
  const failing = computed(() => active.value.filter(s => s.state === 'error' || s.state === 'backoff'))

  async function fetch() {
    try {
      const list = await api.listSourceStatuses()
      statuses.value = Object.fromEntries(list.map(s => [s.source_id, s]))
    } catch { /* the summary stays as it was; WebSocket updates keep coming */ }
  }

  function set(s: SourceStatus) {
    statuses.value = { ...statuses.value, [s.source_id]: s }
  }

  return { statuses, active, failing, fetch, set }
})
