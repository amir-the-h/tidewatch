// web/src/stores/search.ts
import { defineStore } from 'pinia'
import { ref } from 'vue'
import * as api from '../api/search'
import type { SearchRunRequest, SearchRunResponse } from '../api/search'

export const useSearchStore = defineStore('search', () => {
  const loading = ref(false)
  const error   = ref<string | null>(null)
  const result  = ref<SearchRunResponse | null>(null)

  async function run(params: SearchRunRequest) {
    loading.value = true; error.value = null
    try {
      result.value = await api.runSearch(params)
    } catch (e: any) {
      error.value = e.message
      throw e
    } finally {
      loading.value = false
    }
  }

  return { loading, error, result, run }
})
