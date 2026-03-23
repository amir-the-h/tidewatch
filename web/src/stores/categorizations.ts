// web/src/stores/categorizations.ts
import { defineStore } from 'pinia'
import { ref } from 'vue'
import * as api from '../api/categorizations'
import type { Categorization, CategorizationFilter } from '../api/categorizations'

export const useCategorizationsStore = defineStore('categorizations', () => {
  const items   = ref<Categorization[]>([])
  const loading = ref(false)
  const error   = ref<string | null>(null)

  async function fetchCategorizations(filter: CategorizationFilter = {}) {
    loading.value = true; error.value = null
    try { items.value = await api.listCategorizations(filter) }
    catch (e: any) { error.value = e.message }
    finally { loading.value = false }
  }

  return { items, loading, error, fetchCategorizations }
})
