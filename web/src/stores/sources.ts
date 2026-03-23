// web/src/stores/sources.ts
import { defineStore } from 'pinia'
import { ref } from 'vue'
import * as api from '../api/sources'
import type { Source, CreateSourceRequest } from '../api/sources'

export const useSourcesStore = defineStore('sources', () => {
  const sources = ref<Source[]>([])
  const loading = ref(false)
  const error   = ref<string | null>(null)

  async function fetchSources() {
    loading.value = true; error.value = null
    try { sources.value = await api.listSources() }
    catch (e: any) { error.value = e.message }
    finally { loading.value = false }
  }

  async function createSource(body: CreateSourceRequest) {
    const src = await api.createSource(body)
    sources.value.push(src)
    return src
  }

  async function deactivate(id: number) {
    await api.deactivateSource(id)
    const s = sources.value.find(s => s.id === id)
    if (s) s.active = false
  }

  async function remove(id: number) {
    await api.deleteSource(id)
    sources.value = sources.value.filter(s => s.id !== id)
  }

  return { sources, loading, error, fetchSources, createSource, deactivate, remove }
})
