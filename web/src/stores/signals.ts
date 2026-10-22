// web/src/stores/signals.ts
import { defineStore } from 'pinia'
import { ref } from 'vue'
import * as api from '../api/signals'
import type { Signal, SignalDetail, SignalListFilter } from '../api/signals'


export const useSignalsStore = defineStore('signals', () => {
  const signals  = ref<Signal[]>([])
  const total    = ref(0)
  const loading  = ref(false)
  const error    = ref<string | null>(null)
  const page     = ref(1)
  const perPage  = ref(20)

  // Holds the last-used filter so page changes can re-apply it
  let lastFilter: SignalListFilter = {}

  async function fetchSignals(filter: SignalListFilter = {}) {
    lastFilter = filter
    page.value = filter.page ?? 1
    loading.value = true; error.value = null
    try {
      const res = await api.listSignals({ page: page.value, per_page: perPage.value, ...filter })
      signals.value = res.signals
      total.value   = res.total
    }
    catch (e: any) { error.value = e.message }
    finally { loading.value = false }
  }

  async function goToPage(p: number, pp?: number) {
    await fetchSignals({ ...lastFilter, page: p, ...(pp !== undefined ? { per_page: pp } : {}) })
  }

  async function getSignal(id: number): Promise<SignalDetail> {
    return api.getSignal(id)
  }

  async function reprocess(body: { signal_ids?: number[]; action_ids?: number[] }) {
    return api.reprocessSignals(body)
  }

  async function remove(ids: number[]) {
    const result = await api.deleteSignals(ids)
    signals.value = signals.value.filter(s => !ids.includes(s.id))
    return result
  }

  async function deleteNoise() {
    const result = await api.deleteNoiseSignals()
    await fetchSignals()
    return result
  }

  function prependSignal(signal: Signal) {
    total.value++
    if (page.value === 1 && (!lastFilter.kind || lastFilter.kind === signal.kind)) {
      signals.value = [signal, ...signals.value].slice(0, perPage.value)
    }
  }

  return { signals, total, loading, error, page, perPage, fetchSignals, goToPage, getSignal, reprocess, remove, deleteNoise, prependSignal }
})
