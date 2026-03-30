// web/src/stores/failures.ts
import { defineStore } from 'pinia'
import { ref } from 'vue'
import * as api from '../api/failures'
import type { FailedTweet } from '../api/failures'

export const useFailuresStore = defineStore('failures', () => {
  const failures = ref<FailedTweet[]>([])
  const total    = ref(0)
  const loading  = ref(false)
  const error    = ref<string | null>(null)
  const page     = ref(1)
  const perPage  = ref(50)

  async function fetch(p = 1, pp?: number) {
    page.value = p
    if (pp !== undefined) perPage.value = pp
    loading.value = true
    error.value = null
    try {
      const offset = (page.value - 1) * perPage.value
      const res = await api.listFailures(offset, perPage.value)
      failures.value = res.failures ?? []
      total.value    = res.total
    } catch (e: any) {
      error.value = e.message
    } finally {
      loading.value = false
    }
  }

  async function clear() {
    await api.clearFailures()
    failures.value = []
    total.value    = 0
  }

  function setTotal(n: number) {
    total.value = n
  }

  return { failures, total, loading, error, page, perPage, fetch, clear, setTotal }
})
