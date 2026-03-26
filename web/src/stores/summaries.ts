import { defineStore } from 'pinia'
import { ref } from 'vue'
import * as api from '../api/summaries'
import type { SummaryItem, SummaryFilters } from '../api/summaries'

export const useSummariesStore = defineStore('summaries', () => {
  const summaries = ref<SummaryItem[]>([])
  const total     = ref(0)
  const loading   = ref(false)
  const running   = ref(false)
  const error     = ref<string | null>(null)
  const page      = ref(1)
  const perPage   = ref(50)
  const filters   = ref<SummaryFilters>({})

  async function fetch(p = 1, pp?: number) {
    page.value = p
    if (pp !== undefined) perPage.value = pp
    loading.value = true
    error.value = null
    try {
      const res = await api.listSummaries(filters.value, page.value, perPage.value)
      summaries.value = res.summaries ?? []
      total.value     = res.total
    } catch (e: any) {
      error.value = e.message
    } finally {
      loading.value = false
    }
  }

  async function run(): Promise<number> {
    running.value = true
    error.value = null
    try {
      const res = await api.runSummaries(filters.value)
      return res.queued
    } catch (e: any) {
      error.value = e.message
      throw e
    } finally {
      running.value = false
    }
  }

  return { summaries, total, loading, running, error, page, perPage, filters, fetch, run }
})
